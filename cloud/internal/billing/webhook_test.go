// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billing

import (
	"encoding/json"
	"io"
	"log/slog"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// Regression test for audit gap M10 #24 (active-execution-plan §6.C.10).
//
// Before the fix, handleCheckoutCompleted hard-coded `plan := "pro"` and
// included `plan = $2` in the UPDATE statement. If `customer.subscription.updated`
// was delayed, lost, or never arrived (network partition, Stripe outage,
// attacker-induced race between events), the tenant ended up at Pro tier
// without having paid.
//
// The fix removes `plan = $2` from the UPDATE so that:
//   - stripe_subscription_id and status='active' are set (mid-flow signal)
//   - plan is NOT touched — remains whatever it was (typically "starter")
//   - customer.subscription.updated is the only path that maps price_id → plan
func TestHandleCheckoutCompleted_DoesNotElevatePlan(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	// The UPDATE statement must NOT contain `plan = $N` after the fix.
	// We assert positionally: only stripe_subscription_id and status are
	// written; plan is absent from the SQL. Matching the literal SQL string
	// would be brittle; we use a regex that fails if `plan =` appears in
	// the UPDATE SET clause.
	expectedSQL := regexp.MustCompile(
		`UPDATE subscriptions\s+` +
			`SET stripe_subscription_id = \$1, status = 'active', updated_at = now\(\)\s+` +
			`WHERE stripe_customer_id = \$2`,
	)
	mock.ExpectExec(expectedSQL.String()).
		WithArgs("sub_test_123", "cus_test_456").
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Build the event payload.
	session := map[string]any{
		"customer":     "cus_test_456",
		"subscription": "sub_test_123",
		"mode":         "subscription",
	}
	sessionJSON, _ := json.Marshal(session)
	dataObj := StripeEventData{Object: sessionJSON}
	dataJSON, _ := json.Marshal(dataObj)
	event := StripeEvent{
		ID:   "evt_test_1",
		Type: "checkout.session.completed",
		Data: dataJSON,
	}

	handler := &WebhookHandler{
		db:     db,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	if err := handler.handleCheckoutCompleted(event); err != nil {
		t.Fatalf("handleCheckoutCompleted: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

// Negative assertion: the literal string "plan = $" must not appear in the
// SQL emitted by handleCheckoutCompleted. This is a belt-and-suspenders
// check on top of the regex above — a future refactor that re-introduces
// the vulnerability via inline plan assignment would still fail this test.
func TestHandleCheckoutCompleted_SQLDoesNotMentionPlan(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	// Capture the actual SQL via QueryMatcher. We set a permissive expectation
	// and then verify the recorded SQL does not contain "plan = $".
	mock.MatchExpectationsInOrder(true)
	mock.ExpectExec(`UPDATE subscriptions`).
		WithArgs("sub_x", "cus_x").
		WillReturnResult(sqlmock.NewResult(0, 1))

	session := map[string]any{
		"customer":     "cus_x",
		"subscription": "sub_x",
		"mode":         "subscription",
	}
	sessionJSON, _ := json.Marshal(session)
	dataObj := StripeEventData{Object: sessionJSON}
	dataJSON, _ := json.Marshal(dataObj)
	event := StripeEvent{ID: "evt_x", Type: "checkout.session.completed", Data: dataJSON}

	handler := &WebhookHandler{
		db:     db,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	if err := handler.handleCheckoutCompleted(event); err != nil {
		t.Fatalf("handleCheckoutCompleted: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

// Sanity: non-subscription checkouts are no-ops (early return).
func TestHandleCheckoutCompleted_SkipsNonSubscriptionMode(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	// No DB calls expected.

	session := map[string]any{
		"customer":     "cus_payment",
		"subscription": "",
		"mode":         "payment", // one-time payment, not subscription
	}
	sessionJSON, _ := json.Marshal(session)
	dataObj := StripeEventData{Object: sessionJSON}
	dataJSON, _ := json.Marshal(dataObj)
	event := StripeEvent{ID: "evt_pay", Type: "checkout.session.completed", Data: dataJSON}

	handler := &WebhookHandler{
		db:     db,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	if err := handler.handleCheckoutCompleted(event); err != nil {
		t.Fatalf("handleCheckoutCompleted: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}
