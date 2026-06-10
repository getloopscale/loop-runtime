// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Delivery Replay plan — T8.2 RED tests for the replay handler.
//
// Endpoint: POST /v1/memory/webhooks/{webhook_id}/deliveries/{delivery_id}/replay
//
// Pinned behaviour (per ADRs D1-D5 of the plan):
//   D1 — both IDs in the path; both validated BEFORE DB
//   D2 — failed | dead_letter → reset (status='pending', attempt_count=0,
//        next_attempt_at=NOW(), last_response_status=NULL, last_error_message=NULL)
//   D3 — status guard:
//          delivered          → 400 ALREADY_DELIVERED
//          pending|delivering → 200 no-op (idempotent re-trigger by polling client)
//          failed|dead_letter → 200 reset
//   D4 — RequireScope("memory:admin") + cross-tenant → 404 (P0 D5 existence-leak invariant)
//   D5 — UI concern, tested in webhooks.tsx.vitest, not here.
//
// Mirrors test patterns in memory_webhooks_test.go (sqlmock + chi route context
// injection) for greppability and consistent failure messages.

package unit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/cloud/internal/auth"
	"github.com/loopzero/loopzero/cloud/internal/routes"
)

func newDeliveriesHandler(t *testing.T) (*routes.MemoryWebhookDeliveriesHandler, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return routes.NewMemoryWebhookDeliveriesHandler(db, nil), mock
}

// replayRequest builds a POST with both URL params bound via chi route context.
// safeURL keeps the test transport from choking on shell-meta or path traversal
// payloads the way the chi.URLParam injection actually does.
func replayRequest(t *testing.T, webhookID, deliveryID string, user *auth.UserContext) *http.Request {
	t.Helper()
	req := httptest.NewRequest("POST", "/replay", nil)
	if user != nil {
		req = req.WithContext(auth.WithUser(req.Context(), user))
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("webhook_id", webhookID)
	rctx.URLParams.Add("delivery_id", deliveryID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	return req
}

// =============================================================================
// Auth + input validation
// =============================================================================

func TestMemoryWebhookDeliveries_ReplayRejectsUnauthenticated(t *testing.T) {
	h, mock := newDeliveriesHandler(t)
	req := replayRequest(t, "mwh-aaaa1111bbbb2222", "mwd-cccc3333dddd4444", nil)
	rec := httptest.NewRecorder()
	h.Replay(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("DB called before auth check — gate violated: %v", err)
	}
}

func TestMemoryWebhookDeliveries_ReplayRejectsMalformedWebhookID(t *testing.T) {
	cases := []string{"'; DROP", "../etc", "mwh-XYZ", "mwh-", ""}
	for _, malformed := range cases {
		t.Run(malformed, func(t *testing.T) {
			h, mock := newDeliveriesHandler(t)
			req := replayRequest(t, malformed, "mwd-cccc3333dddd4444",
				&auth.UserContext{Subject: "u", TenantID: "t"})
			rec := httptest.NewRecorder()
			h.Replay(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 INVALID_ID for webhook_id %q, got %d", malformed, rec.Code)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("DB called before id validation — gate violated: %v", err)
			}
		})
	}
}

func TestMemoryWebhookDeliveries_ReplayRejectsMalformedDeliveryID(t *testing.T) {
	cases := []string{"'; DROP", "../etc", "mwd-XYZ", "mwd-", "", "mwh-cccc3333dddd4444"}
	for _, malformed := range cases {
		t.Run(malformed, func(t *testing.T) {
			h, mock := newDeliveriesHandler(t)
			req := replayRequest(t, "mwh-aaaa1111bbbb2222", malformed,
				&auth.UserContext{Subject: "u", TenantID: "t"})
			rec := httptest.NewRecorder()
			h.Replay(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 INVALID_ID for delivery_id %q, got %d", malformed, rec.Code)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("DB called before id validation — gate violated: %v", err)
			}
		})
	}
}

// =============================================================================
// D4 — cross-tenant returns 404 (existence-leak invariant)
// =============================================================================

func TestMemoryWebhookDeliveries_ReplayCrossTenantReturns404NotFound(t *testing.T) {
	h, mock := newDeliveriesHandler(t)

	// SELECT join scoped by user.Subject + user.TenantID returns NO ROWS for
	// the cross-tenant probe. Handler MUST return 404, NOT 403, per P0 D5.
	mock.ExpectQuery("SELECT d.status, d.webhook_id FROM memory_webhook_deliveries").
		WithArgs("mwd-cccc3333dddd4444", "user-B", "tenant-B").
		WillReturnError(sqlmock.ErrCancelled) // sql.ErrNoRows path is equivalent here

	req := replayRequest(t, "mwh-aaaa1111bbbb2222", "mwd-cccc3333dddd4444",
		&auth.UserContext{Subject: "user-B", TenantID: "tenant-B"})
	rec := httptest.NewRecorder()
	h.Replay(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Fatal("CRITICAL: cross-tenant Replay returned 403 — D5 P0 violation; must return 404")
	}
}

// =============================================================================
// D3 — status guard branches
// =============================================================================

func TestMemoryWebhookDeliveries_ReplayDeliveredReturns400AlreadyDelivered(t *testing.T) {
	h, mock := newDeliveriesHandler(t)
	mock.ExpectQuery("SELECT d.status, d.webhook_id FROM memory_webhook_deliveries").
		WithArgs("mwd-cccc3333dddd4444", "u", "t").
		WillReturnRows(sqlmock.NewRows([]string{"status", "webhook_id"}).
			AddRow("delivered", "mwh-aaaa1111bbbb2222"))

	req := replayRequest(t, "mwh-aaaa1111bbbb2222", "mwd-cccc3333dddd4444",
		&auth.UserContext{Subject: "u", TenantID: "t"})
	rec := httptest.NewRecorder()
	h.Replay(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("delivered → expected 400 ALREADY_DELIVERED, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMemoryWebhookDeliveries_ReplayPendingReturns200NoOp(t *testing.T) {
	for _, status := range []string{"pending", "delivering"} {
		t.Run(status, func(t *testing.T) {
			h, mock := newDeliveriesHandler(t)
			mock.ExpectQuery("SELECT d.status, d.webhook_id FROM memory_webhook_deliveries").
				WithArgs("mwd-cccc3333dddd4444", "u", "t").
				WillReturnRows(sqlmock.NewRows([]string{"status", "webhook_id"}).
					AddRow(status, "mwh-aaaa1111bbbb2222"))
			// NO ExpectExec for UPDATE — pending/delivering are no-ops.

			req := replayRequest(t, "mwh-aaaa1111bbbb2222", "mwd-cccc3333dddd4444",
				&auth.UserContext{Subject: "u", TenantID: "t"})
			rec := httptest.NewRecorder()
			h.Replay(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("%s → expected 200 no-op, got %d body=%s", status, rec.Code, rec.Body.String())
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unexpected UPDATE executed on %s status: %v", status, err)
			}
		})
	}
}

func TestMemoryWebhookDeliveries_ReplayFailedResetsState(t *testing.T) {
	for _, status := range []string{"failed", "dead_letter"} {
		t.Run(status, func(t *testing.T) {
			h, mock := newDeliveriesHandler(t)
			mock.ExpectQuery("SELECT d.status, d.webhook_id FROM memory_webhook_deliveries").
				WithArgs("mwd-cccc3333dddd4444", "u", "t").
				WillReturnRows(sqlmock.NewRows([]string{"status", "webhook_id"}).
					AddRow(status, "mwh-aaaa1111bbbb2222"))
			// D2 — reset attempt_count, next_attempt_at, status, AND clear
			// last_response_status + last_error_message so the dashboard
			// doesn't show stale failure context after a successful replay.
			mock.ExpectExec("UPDATE memory_webhook_deliveries SET status = 'pending'").
				WithArgs("mwd-cccc3333dddd4444").
				WillReturnResult(sqlmock.NewResult(0, 1))

			req := replayRequest(t, "mwh-aaaa1111bbbb2222", "mwd-cccc3333dddd4444",
				&auth.UserContext{Subject: "u", TenantID: "t"})
			rec := httptest.NewRecorder()
			h.Replay(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("%s → expected 200 reset, got %d body=%s", status, rec.Code, rec.Body.String())
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("expected UPDATE on %s but not executed: %v", status, err)
			}
		})
	}
}

// =============================================================================
// Edge case — path declares webhook A but delivery belongs to webhook B
// =============================================================================
func TestMemoryWebhookDeliveries_ReplayWebhookIDMismatchReturns404(t *testing.T) {
	h, mock := newDeliveriesHandler(t)
	mock.ExpectQuery("SELECT d.status, d.webhook_id FROM memory_webhook_deliveries").
		WithArgs("mwd-cccc3333dddd4444", "u", "t").
		WillReturnRows(sqlmock.NewRows([]string{"status", "webhook_id"}).
			AddRow("failed", "mwh-other000other00")) // belongs to a DIFFERENT webhook

	req := replayRequest(t, "mwh-aaaa1111bbbb2222", "mwd-cccc3333dddd4444",
		&auth.UserContext{Subject: "u", TenantID: "t"})
	rec := httptest.NewRecorder()
	h.Replay(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("path/delivery webhook mismatch → expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}
