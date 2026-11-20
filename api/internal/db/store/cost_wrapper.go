// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// cost_wrapper.go — sqlc-backed implementation of SQLCostStore.
// T2.2 (Batch B #5) of plan
// 2026-05-29-sqlc-migration-plan v1.3.
//
// T1.9 production wiring (Wave 1 of MEET-2026-05-12). Previous
// cost_store.go used *sql.DB (database/sql legacy) for cost_counters
// reads. The wrapper now uses DBTX (pgx-based) via SqlcBridge for
// uniform dispatch with the rest of the migrated stores.
//
// PlanReader adapter preserved verbatim — no SQL handwritten, only
// delegates to enforcement.LimitsStore.
//
// Constructor + struct moved here from cost_store.go; SQLCostStore
// remains the public name to preserve 2 prod call sites
// (api/cmd/setup_server.go:129, :179).

import (
	"context"
	"errors"

	"github.com/getloopscale/loopzero/api/internal/db"

	"github.com/jackc/pgx/v5"

	planpkg "github.com/getloopscale/contracts/plan"
	"github.com/getloopscale/loopzero/api/internal/db/generated"
	"github.com/getloopscale/loopzero/pkg/enforcement"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// SQLCostStore reads cost_counters rows from PostgreSQL via sqlc.
//
// T2.2 Batch B #5: field type widened from *sql.DB → DBTX (pgx-based,
// shared with the rest of the migrated stores).
type SQLCostStore struct {
	db db.DBTX
}

// NewSQLCostStore constructs a CostStore backed by a DBTX (typically
// the *PgxAdapter created in cmd/setup_database.go).
//
// Nil-safe: if db is nil (boot-time, tests), the returned store still
// works — every GetCounter returns (zero, false, error) which the
// CapChecker treats as fail-open.
func NewSQLCostStore(db db.DBTX) *SQLCostStore {
	return &SQLCostStore{db: db}
}

// GetCounter implements enforcement.CostStore.
//
// Reads the active counter for (tenant, metric) where now() is within
// the period via sqlc-generated GetActiveCostCounter. The composite
// PK on (tenant_id, metric, period_start) makes this an O(log n)
// lookup.
//
// Error semantics (preserved from legacy):
//   - pgx.ErrNoRows → (zero, false, nil) (CapChecker = "no usage" = allow)
//   - other err     → (zero, false, err) (CapChecker fails-open)
func (s *SQLCostStore) GetCounter(ctx context.Context, tenantID string, metric planpkg.CostMetric) (planpkg.CostCounter, bool, error) {
	if s == nil || s.db == nil {
		// Audit gap M02 #13 fix: typed LoopZeroError instead of generic errors.New.
		// `nil SQLCostStore` indicates the cost-enforcement middleware was
		// wired without a backing store — a programming error caught at the
		// first request rather than at boot.
		return planpkg.CostCounter{}, false, loopzeroerrors.ErrInternalError.Wrap("nil SQLCostStore")
	}

	gdb := db.Generated(s.db)
	q := generated.New()
	row, err := q.GetActiveCostCounter(ctx, gdb, generated.GetActiveCostCounterParams{
		TenantID: tenantID,
		Metric:   string(metric),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// No counter row yet this period — caller treats as 0 usage.
		return planpkg.CostCounter{}, false, nil
	}
	if err != nil {
		return planpkg.CostCounter{}, false, err
	}

	return planpkg.CostCounter{
		TenantID:    tenantID,
		Plan:        planpkg.Plan(row.PlanTier),
		Metric:      metric,
		PeriodStart: row.PeriodStartEpoch,
		PeriodEnd:   row.PeriodEndEpoch,
		Value:       row.Value,
	}, true, nil
}

// PlanReader adapts the existing enforcement.LimitsStore.GetTenantPlan
// (returns string) into middleware.PlanReader (returns planpkg.Plan).
//
// Reuses the same SQL query (`SELECT plan FROM subscriptions ...`) as
// PlanLimiter — no duplicated logic. The conversion is a 1:1 typed
// cast since plan strings are the canonical planpkg.Plan values.
//
// T2.2 Batch B #5: preserved verbatim (no SQL handwritten — only
// delegates to LimitsStore which lives in pkg/enforcement/).
type PlanReader struct {
	store enforcement.LimitsStore
}

// NewPlanReader wraps a LimitsStore for use by CostEnforcer middleware.
func NewPlanReader(store enforcement.LimitsStore) *PlanReader {
	return &PlanReader{store: store}
}

// GetTenantPlan implements middleware.PlanReader.
//
// On error from the underlying store, returns ("starter", err). The
// middleware fails-open on plan lookup error, so the conservative
// starter default would only matter if the middleware were configured
// fail-closed — currently it logs + skips enforcement.
func (p *PlanReader) GetTenantPlan(ctx context.Context, tenantID string) (planpkg.Plan, error) {
	if p == nil || p.store == nil {
		// Audit gap M02 #13 fix: typed LoopZeroError instead of generic errors.New.
		return planpkg.PlanStarter, loopzeroerrors.ErrInternalError.Wrap("nil PlanReader")
	}
	plan, err := p.store.GetTenantPlan(ctx, tenantID)
	if err != nil {
		return planpkg.PlanStarter, err
	}
	if plan == "" {
		return planpkg.PlanStarter, nil
	}
	return planpkg.Plan(plan), nil
}
