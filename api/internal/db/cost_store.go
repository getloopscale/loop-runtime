// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/loopzero/loopzero/pkg/enforcement"
)

// T1.9 production wiring (Wave 1 of MEET-2026-05-12).
//
// SQLCostStore implements `enforcement.CostStore` by reading the
// `cost_counters` table (migration 000017). Used by the production
// CapChecker wired in cmd/setup_server.go.
//
// Fail-open semantics (matching the CostStore interface):
//   - Row missing (sql.ErrNoRows) → returns (zero, false, nil). The
//     CapChecker interprets this as "no usage yet this period" = allow.
//   - DB error → returns (zero, false, err). The CapChecker treats
//     this as fail-open (degraded aggregator must never block traffic).

// SQLCostStore reads cost_counters rows from PostgreSQL.
type SQLCostStore struct {
	db *sql.DB
}

// NewSQLCostStore constructs a CostStore backed by *sql.DB.
//
// Nil-safe: if db is nil (boot-time, tests), the returned store still
// works — every GetCounter returns (zero, false, error) which the
// CapChecker treats as fail-open.
func NewSQLCostStore(db *sql.DB) *SQLCostStore {
	return &SQLCostStore{db: db}
}

// GetCounter implements enforcement.CostStore.
//
// Reads the active counter for (tenant, metric) where now() is within
// the period. The composite PK on (tenant_id, metric, period_start)
// makes this an O(log n) lookup.
func (s *SQLCostStore) GetCounter(ctx context.Context, tenantID string, metric enforcement.CostMetric) (enforcement.CostCounter, bool, error) {
	if s == nil || s.db == nil {
		return enforcement.CostCounter{}, false, errors.New("nil SQLCostStore")
	}

	var (
		plan        string
		periodStart int64
		periodEnd   int64
		value       int64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT plan_tier,
		       EXTRACT(EPOCH FROM period_start)::bigint,
		       EXTRACT(EPOCH FROM period_end)::bigint,
		       value
		FROM cost_counters
		WHERE tenant_id = $1
		  AND metric = $2
		  AND now() >= period_start
		  AND now() <  period_end
	`, tenantID, string(metric)).Scan(&plan, &periodStart, &periodEnd, &value)

	if errors.Is(err, sql.ErrNoRows) {
		// No counter row yet this period — caller treats as 0 usage.
		return enforcement.CostCounter{}, false, nil
	}
	if err != nil {
		return enforcement.CostCounter{}, false, err
	}

	return enforcement.CostCounter{
		TenantID:    tenantID,
		Plan:        enforcement.Plan(plan),
		Metric:      metric,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		Value:       value,
	}, true, nil
}

// PlanReader adapts the existing enforcement.LimitsStore.GetTenantPlan
// (returns string) into middleware.PlanReader (returns enforcement.Plan).
//
// Reuses the same SQL query (`SELECT plan FROM subscriptions ...`) as
// PlanLimiter — no duplicated logic. The conversion is a 1:1 typed
// cast since plan strings are the canonical enforcement.Plan values.
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
func (p *PlanReader) GetTenantPlan(ctx context.Context, tenantID string) (enforcement.Plan, error) {
	if p == nil || p.store == nil {
		return enforcement.PlanStarter, errors.New("nil PlanReader")
	}
	plan, err := p.store.GetTenantPlan(ctx, tenantID)
	if err != nil {
		return enforcement.PlanStarter, err
	}
	if plan == "" {
		return enforcement.PlanStarter, nil
	}
	return enforcement.Plan(plan), nil
}
