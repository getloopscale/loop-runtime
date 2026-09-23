// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// LimitsStore is the read-only port PlanLimiter uses to look up a tenant's
// plan and the (hard, soft) limit pair for a given metric (ADR-0033).
//
// Implementations MUST:
//
//   - Return ("", nil) from GetTenantPlan when the tenant has no active
//     subscription. That is the normal state of a tenant that just signed up,
//     not a platform fault: domain.ResolvePlanTier maps "" to Starter. Any
//     other failure (transport, partial read) MUST be a non-nil error.
//
//   - Return (-1, _, nil) for hardLimit when the metric has no
//     enforced cap for the plan (NULL hard_limit row).
//
//   - Return (0, _, nil) — or sql.ErrNoRows — when the (plan, metric) row
//     is missing; the limiter denies the request (fail closed).
type LimitsStore interface {
	// GetTenantPlan returns the raw plan column (e.g., "starter", "pro")
	// of the tenant's active subscription, or ("", nil) when none exists.
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
	// No row is a tenant with no subscription yet — the onboarding state, not
	// an error (see the LimitsStore contract).
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return plan, nil
}

// GetAccountPlanByTenant resolves the plan of the tenant's Account (M4; one
// subscription per Account since M2 — UNIQUE(account_id)), joining through
// tenants.account_id (NOT NULL since M3 000044). Like GetTenantPlan, an
// Account with no active subscription answers ("", nil): the Starter cap,
// never an outage.
func (s *sqlLimitsStore) GetAccountPlanByTenant(ctx context.Context, tenantID string) (string, error) {
	if s == nil || s.db == nil {
		return "", fmt.Errorf("nil sql limits store")
	}
	var plan string
	err := s.db.QueryRowContext(ctx,
		`SELECT plan FROM subscriptions
		 WHERE account_id = (SELECT account_id FROM tenants WHERE id = $1)
		   AND status IN ('active', 'trialing') LIMIT 1`,
		tenantID,
	).Scan(&plan)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
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
