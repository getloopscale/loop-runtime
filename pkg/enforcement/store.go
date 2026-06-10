// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import (
	"context"
	"database/sql"
	"fmt"
)

// LimitsStore is the read-only interface PlanLimiter uses to look up a
// tenant's plan and the (hard, soft) limit pair for a given metric.
//
// Per ADR-0033 (DIP via LimitsStore interface): pkg/enforcement no longer
// hard-couples to *sql.DB at the type level. The default constructor
// NewPlanLimiter still accepts *sql.DB for backward compatibility, but
// callers that want to mock, test, or back the limiter with a different
// store (in-memory cache, OPA policy fetcher) can implement this
// interface and use NewPlanLimiterWithStore.
//
// Implementations MUST:
//
//   - Return ("starter", nil) when the subscription row is missing AND
//     the limiter's OnFallbackStarter callback (set on PlanLimiter) is
//     responsible for emitting the metric. Returning a non-nil error here
//     causes the limiter to log and fall back to "starter" as well, but
//     the OnFallbackStarter callback only fires when GetTenantPlan
//     returns an error or empty plan.
//
//   - Return (-1, _, nil) for hardLimit when the metric has no
//     enforced cap for the plan (NULL hard_limit row).
//
//   - Return (0, _, nil) for hardLimit when the (plan, metric) row is
//     missing — the limiter treats this as fail-closed (denies the
//     request) per ADR-0033 enumeration.
type LimitsStore interface {
	// GetTenantPlan returns the active plan name (e.g., "starter",
	// "pro") for the given tenant, or ("starter", error) if no
	// subscription row exists.
	GetTenantPlan(ctx context.Context, tenantID string) (string, error)

	// GetLimits returns the (hard, soft) limits for the given (plan,
	// metric) pair. Soft <= 0 means no soft cap; hard < 0 means
	// unlimited; hard == 0 means missing row (fail closed).
	GetLimits(ctx context.Context, plan, metric string) (hardLimit, softLimit int, err error)
}

// sqlLimitsStore is the canonical SQL adapter used internally when
// callers pass *sql.DB to NewPlanLimiter. Externalized so tests and
// future backends can implement LimitsStore directly.
type sqlLimitsStore struct {
	db *sql.DB
}

// NewSQLLimitsStore wraps a *sql.DB so it satisfies the LimitsStore
// interface. Callers in the api/operators modules can use this when
// constructing a PlanLimiter explicitly.
func NewSQLLimitsStore(db *sql.DB) LimitsStore {
	return &sqlLimitsStore{db: db}
}

// GetTenantPlan implements LimitsStore.
func (s *sqlLimitsStore) GetTenantPlan(ctx context.Context, tenantID string) (string, error) {
	if s == nil || s.db == nil {
		return "", fmt.Errorf("nil sql limits store")
	}
	var plan string
	err := s.db.QueryRowContext(ctx,
		"SELECT plan FROM subscriptions WHERE tenant_id = $1 AND status IN ('active', 'trialing') LIMIT 1",
		tenantID,
	).Scan(&plan)
	if err != nil {
		return "", err
	}
	return plan, nil
}

// GetLimits implements LimitsStore.
func (s *sqlLimitsStore) GetLimits(ctx context.Context, plan, metric string) (int, int, error) {
	if s == nil || s.db == nil {
		return 0, 0, fmt.Errorf("nil sql limits store")
	}
	var hard sql.NullInt64
	var soft sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		"SELECT hard_limit, soft_limit FROM plan_limits WHERE plan = $1 AND metric = $2",
		plan, metric,
	).Scan(&hard, &soft)
	if err != nil {
		return 0, 0, err
	}
	hardVal := -1
	if hard.Valid {
		hardVal = int(hard.Int64)
	}
	softVal := 0
	if soft.Valid {
		softVal = int(soft.Int64)
	}
	return hardVal, softVal, nil
}
