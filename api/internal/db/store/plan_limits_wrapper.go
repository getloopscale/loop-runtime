// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/pkg/enforcement"
)

// PlanLimitsStore is the SQL adapter of enforcement.LimitsStore: it reads a
// tenant's plan from `subscriptions` and the (hard, soft) caps from
// `plan_limits`. It also answers GetAccountPlanByTenant, the optional
// capability PlanLimiter uses for the per-Account project cap.
//
// It MUST be given a cross-tenant connection — the background pool, running as
// loopzero_worker. `subscriptions` and `tenants` are under fail-closed Row Level
// Security (migration 000062), and the plan lookups carry no tenant scope: over
// the request pool (loopzero_api) every lookup sees no row, so every tenant reads
// as Starter. Scoping the session to the tenant would not be enough either: an
// Account has ONE subscription (UNIQUE(account_id)), held by one of its
// tenants, so the Account plan of any sibling tenant is another tenant's row.
// Measured against the migrated schema: a "pro" Account read "" through
// loopzero_api and "pro" through loopzero_worker, for both lookups.
type PlanLimitsStore struct {
	DB db.DBTX
}

// NewPlanLimitsStore builds the adapter over a cross-tenant connection.
func NewPlanLimitsStore(crossTenant db.DBTX) *PlanLimitsStore {
	return &PlanLimitsStore{DB: crossTenant}
}

var _ enforcement.LimitsStore = (*PlanLimitsStore)(nil)

// GetTenantPlan returns the plan of the tenant's active subscription, or
// ("", nil) when it has none — the onboarding state, which
// domain.ResolvePlanTier maps to Starter.
func (s *PlanLimitsStore) GetTenantPlan(ctx context.Context, tenantID string) (string, error) {
	return s.plan(ctx, "tenant plan", tenantID,
		"SELECT plan FROM subscriptions WHERE tenant_id = $1 AND status IN ('active', 'trialing') LIMIT 1")
}

// GetAccountPlanByTenant resolves the plan of the tenant's Account (M4; one
// subscription per Account since M2 — UNIQUE(account_id)), joining through
// tenants.account_id (NOT NULL since M3 000044). Like GetTenantPlan, an
// Account with no active subscription answers ("", nil): the Starter cap,
// never an outage.
func (s *PlanLimitsStore) GetAccountPlanByTenant(ctx context.Context, tenantID string) (string, error) {
	return s.plan(ctx, "account plan", tenantID,
		`SELECT plan FROM subscriptions
		 WHERE account_id = (SELECT account_id FROM tenants WHERE id = $1)
		   AND status IN ('active', 'trialing') LIMIT 1`)
}

func (s *PlanLimitsStore) plan(ctx context.Context, what, tenantID, query string) (string, error) {
	if s == nil || s.DB == nil {
		return "", fmt.Errorf("plan limits store: no database configured")
	}
	var plan string
	err := s.DB.QueryRow(ctx, query, tenantID).Scan(&plan)
	if errors.Is(err, db.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("plan limits store: %s for tenant %s: %w", what, tenantID, err)
	}
	return plan, nil
}

// GetLimits returns the (hard, soft) caps of a plan for a metric: a NULL
// hard_limit is -1 (unlimited), a NULL soft_limit is 0 (no soft cap), and a
// missing row is (0, 0, nil) — the sentinel PlanLimiter denies on.
func (s *PlanLimitsStore) GetLimits(ctx context.Context, plan, metric string) (int, int, error) {
	if s == nil || s.DB == nil {
		return 0, 0, fmt.Errorf("plan limits store: no database configured")
	}
	var hard, soft *int64
	err := s.DB.QueryRow(ctx,
		"SELECT hard_limit, soft_limit FROM plan_limits WHERE plan = $1 AND metric = $2",
		plan, metric,
	).Scan(&hard, &soft)
	if errors.Is(err, db.ErrNoRows) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("plan limits store: %s limit of plan %s: %w", metric, plan, err)
	}
	hardVal := -1
	if hard != nil {
		hardVal = int(*hard)
	}
	softVal := 0
	if soft != nil {
		softVal = int(*soft)
	}
	return hardVal, softVal, nil
}
