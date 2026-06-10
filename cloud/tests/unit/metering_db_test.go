// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"github.com/loopzero/loopzero/cloud/internal/billing"
	"github.com/loopzero/loopzero/cloud/internal/metering"
)

// T4.2 — the reporter claims pending rows with FOR UPDATE SKIP LOCKED inside a
// transaction and marks them reported in the same tx, so two replicas never
// report the same usage (no duplicate billing).
func TestReportPending_LocksAndCommits(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer func() { _ = db.Close() }()

	stripeHits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		stripeHits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	stripe := billing.NewStripeClientWithURL("sk_test", srv.URL)

	mock.ExpectBegin()
	mock.ExpectQuery("FOR UPDATE OF ur SKIP LOCKED").
		WillReturnRows(sqlmock.NewRows(
			[]string{"id", "tenant_id", "metric", "quantity", "created_at", "stripe_subscription_item_id"}).
			AddRow("ur_1", "tn_1", "builds", 3, time.Unix(1700000000, 0), "si_1"))
	mock.ExpectExec("UPDATE usage_records SET reported_to_stripe = true").
		WithArgs("ur_1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	r := metering.NewReporter(db, stripe, slog.New(slog.DiscardHandler))
	if err := r.ReportPending(); err != nil {
		t.Fatalf("ReportPending: %v", err)
	}
	if stripeHits != 1 {
		t.Fatalf("expected exactly 1 Stripe report, got %d", stripeHits)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expected lock+mark+commit: %v", err)
	}
}
