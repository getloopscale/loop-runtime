// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import (
	"log/slog"
	"os"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// TestCountPermanentEnvironments_PreviewsNotCounted is the canonical
// regression test for D5 of plan
// 2026-06-07-preview-as-category-single-slot-plan.md (Bug A fix).
//
// The bug it locks: before migration 000027 + the CountFn refactor,
// `SELECT COUNT(*) FROM environment_targets WHERE tenant_id = $1`
// counted preview-pr-N rows as if they were permanent environments.
// A tenant with 1 permanent prod env + 2 preview-pr-N rows hit
// "current=3, max=3" PLAN_LIMIT_EXCEEDED even on Pro plan.
//
// The contract verified here: the helper MUST filter by
// `kind = 'permanent'`. The query passed to sqlmock is the EXACT
// canonical string emitted by CountPermanentEnvironments — drift in
// either side causes the test to fail.
func TestCountPermanentEnvironments_PreviewsNotCounted(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	// Expect the EXACT canonical query (escaped for regex).
	// M2 v1.2 T7.2: COUNT(*) over (project, env) tuples (PK guarantees
	// uniqueness — DISTINCT no longer needed post-J-23/M2).
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM environment_targets WHERE tenant_id = \$1 AND kind = 'permanent'`).
		WithArgs("tn-test").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	limiter := &PlanLimiter{
		store:  &fakeLimitsStore{},
		db:     db,
		logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
	count := CountPermanentEnvironments(limiter)("tn-test")
	if count != 1 {
		t.Errorf("CountPermanentEnvironments returned %d, want 1", count)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestCountPermanentEnvironments_NilLimiter_ReturnsZero confirms the
// dev fall-through: a nil limiter MUST never panic and MUST return 0
// (so the policy CheckLimit short-circuits to "allowed" downstream).
func TestCountPermanentEnvironments_NilLimiter_ReturnsZero(t *testing.T) {
	count := CountPermanentEnvironments(nil)("tn-test")
	if count != 0 {
		t.Errorf("nil limiter: got %d, want 0", count)
	}
}

// TestCountPermanentEnvironments_NoStore_ReturnsZero confirms that a
// limiter with no LimitsStore (the legacy NewPlanLimiter(nil) path
// for dev mode) returns 0 without querying.
func TestCountPermanentEnvironments_NoStore_ReturnsZero(t *testing.T) {
	limiter := &PlanLimiter{db: nil, store: nil, logger: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	count := CountPermanentEnvironments(limiter)("tn-test")
	if count != 0 {
		t.Errorf("no store: got %d, want 0", count)
	}
}

// TestCountPermanentEnvironments_QueryError_ReturnsZero confirms
// fail-safe semantics: if the underlying SQL fails (e.g., schema
// drift, transient connection loss), the helper returns 0 so the
// policy layer evaluates as "no current usage" rather than crashing
// the request. This matches the established CountRows /
// CountBuildsThisMonth contract.
func TestCountPermanentEnvironments_QueryError_ReturnsZero(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(`SELECT COUNT\(DISTINCT environment\) FROM environment_targets`).
		WithArgs("tn-test").
		WillReturnError(sqlmock.ErrCancelled)

	limiter := &PlanLimiter{
		store:  &fakeLimitsStore{},
		db:     db,
		logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
	count := CountPermanentEnvironments(limiter)("tn-test")
	if count != 0 {
		t.Errorf("query error: got %d, want 0", count)
	}
}

// TestCountPermanentEnvironments_PerProjectEnvTuple_M2 — T7.2 RED:
// asserts COUNT(*) counts per-(project, env) tuple (not unique env
// names tenant-wide). Multi-project tenant with 5 distinct
// (project, env) tuples should return 5.
func TestCountPermanentEnvironments_PerProjectEnvTuple_M2(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	// 5 distinct (project, env) tuples:
	//   (web, production), (web, staging), (api, production),
	//   (api, staging), (api, qa)
	// Pre-M2 COUNT(DISTINCT environment) would have said 3 (prod, staging, qa).
	// Post-M2 COUNT(*) returns 5 — the true per-tuple count.
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM environment_targets WHERE tenant_id = \$1 AND kind = 'permanent'`).
		WithArgs("tn-m2-multi").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

	limiter := &PlanLimiter{
		store:  &fakeLimitsStore{},
		db:     db,
		logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
	count := CountPermanentEnvironments(limiter)("tn-m2-multi")
	if count != 5 {
		t.Errorf("M2 per-tuple count: got %d, want 5", count)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
