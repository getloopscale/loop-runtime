// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/loopzero/loopzero/cloud/internal/billing"
)

func newWebhookHandler(t *testing.T) (*billing.WebhookHandler, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	logger := slog.New(slog.NewTextHandler(httptest.NewRecorder().Body, nil))
	return billing.NewWebhookHandler(db, "whsec_test", billing.LoadPlanConfig(), logger), mock
}

func checkoutEvent(id string) billing.StripeEvent {
	return billing.StripeEvent{
		ID:   id,
		Type: "checkout.session.completed",
		Data: json.RawMessage(`{"object":{"customer":"cus_1","subscription":"sub_1","mode":"subscription"}}`),
	}
}

// T3.2 — checkout links the subscription WITHOUT setting plan (exactly two args:
// subscription id + customer; a 'pro' default would add a third arg and fail).
func TestProcessEvent_CheckoutDoesNotSetPlan(t *testing.T) {
	h, mock := newWebhookHandler(t)

	mock.ExpectQuery("SELECT EXISTS").WithArgs("evt_1").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectBegin() // T3.1 — business mutation + event record are atomic
	mock.ExpectExec("UPDATE subscriptions").
		WithArgs("sub_1", "cus_1"). // only 2 args — no plan column
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO webhook_events").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := h.ProcessEvent(checkoutEvent("evt_1")); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations (UPDATE likely still sets plan, or not atomic): %v", err)
	}
}

// T3.2 — when no subscription row matches the customer, ProcessEvent must return
// an error (so Stripe retries) instead of silently orphaning the plan.
func TestProcessEvent_CheckoutErrorsWhenNoRow(t *testing.T) {
	h, mock := newWebhookHandler(t)

	mock.ExpectQuery("SELECT EXISTS").WithArgs("evt_1").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE subscriptions").
		WithArgs("sub_1", "cus_1").
		WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected → handler errors
	mock.ExpectRollback() // T3.1 — mutation must be rolled back, not committed
	// Failure recorded OUTSIDE the rolled-back tx (status=failed), so the event
	// stays retryable; then the error is propagated.
	mock.ExpectExec("INSERT INTO webhook_events").
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := h.ProcessEvent(checkoutEvent("evt_1")); err == nil {
		t.Fatal("expected error when no subscription row matches the customer")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expected rollback + failure record: %v", err)
	}
}

// T3.3 — unhandled event types are recorded with status='skipped' for audit.
func TestProcessEvent_UnhandledTypeRecordedSkipped(t *testing.T) {
	h, mock := newWebhookHandler(t)
	event := billing.StripeEvent{
		ID:   "evt_2",
		Type: "customer.created",
		Data: json.RawMessage(`{"object":{}}`),
	}

	mock.ExpectQuery("SELECT EXISTS").WithArgs("evt_2").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec("INSERT INTO webhook_events").
		WithArgs("evt_2", "customer.created", "skipped", nil, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := h.ProcessEvent(event); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("skipped event not recorded as expected: %v", err)
	}
}

// T3.4 — a malformed (non-hex) v1 signature is rejected without panicking.
func TestVerifySignature_MalformedHexRejected(t *testing.T) {
	secret := "whsec_test"
	payload := []byte(`{"id":"evt"}`)
	ts := fmt.Sprintf("%d", time.Now().Unix())
	header := fmt.Sprintf("t=%s,v1=ZZZZ-not-valid-hex", ts)

	if err := billing.VerifySignature(payload, header, secret); err == nil {
		t.Error("malformed hex signature must be rejected")
	}
}

// T3.5 — metered usage reports must carry a stable Idempotency-Key so a retry or
// multi-replica re-run does not double-charge the customer.
func TestReportUsage_SendsStableIdempotencyKey(t *testing.T) {
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := billing.NewStripeClientWithURL("sk_test", srv.URL)
	if err := c.ReportUsage("si_123", 5, 1700000000); err != nil {
		t.Fatalf("ReportUsage: %v", err)
	}
	if err := c.ReportUsage("si_123", 5, 1700000000); err != nil { // retry
		t.Fatalf("ReportUsage retry: %v", err)
	}

	if len(keys) != 2 || keys[0] == "" {
		t.Fatalf("expected Idempotency-Key on each report, got %v", keys)
	}
	if keys[0] != keys[1] {
		t.Errorf("Idempotency-Key must be stable for the same (item, period): %q vs %q", keys[0], keys[1])
	}
}
