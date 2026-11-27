// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package enforcement checks plan limits before allowing mutations.
//
// PlanLimiter depends only on the LimitsStore port (ADR-0033); the plan-tier
// rule it applies is domain.ResolvePlanTier, so a tenant's plan means the same
// thing here as everywhere else in the engine.
package enforcement

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/getloopscale/loop-runtime/pkg/domain"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// PlanLimiter checks if a tenant's operation is within their plan limits.
//
// Every check fails CLOSED: when the plan or the limits cannot be read, the
// check returns a PLATFORM_DEGRADED error instead of a decision. A store
// outage is an infrastructure fault; treating it as "starter plan" or as
// "no limit configured" would turn it into an allow or a misleading 402.
type PlanLimiter struct {
	store  LimitsStore
	logger *slog.Logger
}

// NewPlanLimiter creates a limiter backed by the given LimitsStore. A nil
// store is the dev fall-through: every check allows (plan "dev").
func NewPlanLimiter(store LimitsStore, logger *slog.Logger) *PlanLimiter {
	return &PlanLimiter{store: store, logger: logger}
}

// LimitResult describes the outcome of a limit check.
type LimitResult struct {
	Allowed  bool   // Whether the operation is permitted
	Plan     string // Current plan name
	Metric   string // Which metric was checked
	Current  int    // Current usage count
	Limit    int    // Hard limit for the plan
	SoftOver bool   // True if soft limit exceeded (Team plan: allowed but overage)
}

// hasStore reports whether the limiter has a usable backing store.
// Used to short-circuit dev-mode fall-throughs (nil DB → nil store).
func (l *PlanLimiter) hasStore() bool {
	return l != nil && l.store != nil
}

// errLimitsUnavailable is the typed error every check returns when the plan
// or the limits cannot be read. The cause stays reachable through Unwrap, so
// callers can still tell a store outage from an unknown plan value
// (domain.ErrUnknownPlanTier).
func errLimitsUnavailable(tenantID, what string, cause error) error {
	return &loopruntimeerrors.LoopRuntimeError{
		Code:    loopruntimeerrors.ErrPlatformDegraded.Code,
		Message: "plan limits cannot be verified right now; retry shortly",
		Cause:   fmt.Errorf("%s for tenant %s: %w", what, tenantID, cause),
	}
}

// CheckLimit verifies if a tenant can perform an operation for the given metric.
func (l *PlanLimiter) CheckLimit(ctx context.Context, tenantID, metric string, currentCount int) (LimitResult, error) {
	if !l.hasStore() {
		return LimitResult{Allowed: true, Plan: "dev", Metric: metric}, nil
	}

	plan, err := l.tenantPlan(ctx, tenantID)
	if err != nil {
		return LimitResult{}, err
	}
	if plan == domain.PlanTierEnterprise {
		return LimitResult{Allowed: true, Plan: plan.String(), Metric: metric}, nil
	}

	hardLimit, softLimit, err := l.limits(ctx, tenantID, plan, metric)
	if err != nil {
		return LimitResult{}, err
	}
	result := LimitResult{Plan: plan.String(), Metric: metric, Current: currentCount, Limit: hardLimit}
	if hardLimit < 0 {
		return LimitResult{Allowed: true, Plan: plan.String(), Metric: metric}, nil
	}
	if hardLimit == 0 {
		// No limit row for a non-enterprise plan: deny rather than allow.
		l.logger.Warn("no limit configured (denying by default)",
			"tenant", tenantID, "plan", plan, "metric", metric,
		)
		return result, nil
	}

	if softLimit > 0 && currentCount >= softLimit {
		result.Allowed = true
		result.SoftOver = true
		l.logger.Info("soft limit exceeded (overage)",
			"tenant", tenantID, "plan", plan, "metric", metric,
			"current", currentCount, "soft_limit", softLimit,
		)
		return result, nil
	}

	if currentCount >= hardLimit {
		l.logger.Info("hard limit exceeded",
			"tenant", tenantID, "plan", plan, "metric", metric,
			"current", currentCount, "hard_limit", hardLimit,
		)
		return result, nil
	}

	result.Allowed = true
	return result, nil
}

// CheckValue verifies if a REQUESTED value (not accumulated usage) is within
// a tenant's plan cap for the given metric. Unlike CheckLimit, this answers
// "is the user allowed to request this exact value?" — used for per-resource
// intent checks such as replica count or secret size.
//
// B15 (CHG-2026-04-23-B15 / MEET-2026-04-23-059).
//
// Semantics:
//   - nil limiter / nil store         → Allowed (dev fall-through, same as CheckLimit)
//   - enterprise plan                 → Allowed (unlimited)
//   - hard_limit NULL (< 0)           → Allowed (unlimited for this plan)
//   - hard_limit == 0 (missing row)   → Denied (fail closed, logged)
//   - requested <= hard_limit         → Allowed
//   - requested > hard_limit          → Denied, LimitResult.Current == requested
//   - plan or limits unreadable       → PLATFORM_DEGRADED error
//
// Reports LimitResult.Current as the REQUESTED value (not current usage),
// so `FormatLimitError` surfaces it to the user.
func (l *PlanLimiter) CheckValue(ctx context.Context, tenantID, metric string, requested int) (LimitResult, error) {
	if !l.hasStore() {
		return LimitResult{Allowed: true, Plan: "dev", Metric: metric, Current: requested}, nil
	}

	plan, err := l.tenantPlan(ctx, tenantID)
	if err != nil {
		return LimitResult{}, err
	}
	if plan == domain.PlanTierEnterprise {
		return LimitResult{Allowed: true, Plan: plan.String(), Metric: metric, Current: requested}, nil
	}

	hardLimit, _, err := l.limits(ctx, tenantID, plan, metric)
	if err != nil {
		return LimitResult{}, err
	}
	result := LimitResult{Plan: plan.String(), Metric: metric, Current: requested, Limit: hardLimit}
	switch {
	case hardLimit < 0:
		result.Allowed = true
		result.Limit = 0
	case hardLimit == 0:
		l.logger.Warn("no value limit configured (denying by default)",
			"tenant", tenantID, "plan", plan, "metric", metric, "requested", requested,
		)
	case requested > hardLimit:
		l.logger.Info("requested value exceeds plan cap",
			"tenant", tenantID, "plan", plan, "metric", metric,
			"requested", requested, "hard_limit", hardLimit,
		)
	default:
		result.Allowed = true
	}
	return result, nil
}

// CheckProjectsValue answers "may this Account create one more project?" given
// its CURRENT project count. M4 (account-level tenancy, Phase 2): unlike
// CheckValue (tenant-scoped), it resolves the ACCOUNT plan + the per-Account
// `projects` cap. Allowed iff creating one more stays within the cap
// (current < hard); enterprise / unlimited (hard < 0) always allowed with
// Limit -1; a missing limit row fails closed. The COUNT is supplied by the
// caller (the atomic in-transaction count in the create path — T2.1).
func (l *PlanLimiter) CheckProjectsValue(ctx context.Context, tenantID string, current int) (LimitResult, error) {
	const metric = "projects"
	if !l.hasStore() {
		return LimitResult{Allowed: true, Plan: "dev", Metric: metric, Current: current}, nil
	}

	plan, err := l.accountPlan(ctx, tenantID)
	if err != nil {
		return LimitResult{}, err
	}
	unlimited := LimitResult{Allowed: true, Plan: plan.String(), Metric: metric, Current: current, Limit: -1}
	if plan == domain.PlanTierEnterprise {
		// Limit:-1 is the unambiguous "unlimited" sentinel the create path uses
		// to SKIP the advisory lock + count (EC-6).
		return unlimited, nil
	}

	hardLimit, _, err := l.limits(ctx, tenantID, plan, metric)
	if err != nil {
		return LimitResult{}, err
	}
	if hardLimit < 0 {
		return unlimited, nil
	}
	result := LimitResult{Plan: plan.String(), Metric: metric, Current: current, Limit: hardLimit}
	if hardLimit == 0 {
		l.logger.Warn("no projects limit configured (denying by default)",
			"tenant", tenantID, "plan", plan, "current", current)
		return result, nil
	}
	result.Allowed = current < hardLimit // creating one more makes count current+1 ≤ hard
	if !result.Allowed {
		l.logger.Info("account at project cap",
			"tenant", tenantID, "plan", plan, "current", current, "hard_limit", hardLimit)
	}
	return result, nil
}

// tenantPlan resolves the tenant's plan through domain.ResolvePlanTier — the
// single implementation of the plan-tier rule (ADR D4): a missing
// subscription is Starter, legacy "growth" is Pro, an unrecognised value and a
// store failure are errors.
func (l *PlanLimiter) tenantPlan(ctx context.Context, tenantID string) (domain.PlanTier, error) {
	plan, err := domain.ResolvePlanTier(ctx, l.store, tenantID)
	if err != nil {
		return domain.PlanTierUnknown, errLimitsUnavailable(tenantID, "resolve plan", err)
	}
	return plan, nil
}

// accountPlanResolver is the OPTIONAL capability (ISP) a LimitsStore may
// implement to resolve a tenant's ACCOUNT plan. Kept off the core LimitsStore
// interface so narrow test fakes are not forced to implement it.
type accountPlanResolver interface {
	GetAccountPlanByTenant(ctx context.Context, tenantID string) (string, error)
}

// accountPlanStore presents an accountPlanResolver as a domain.PlanTierStore,
// so the account plan goes through the same ResolvePlanTier rule as the
// tenant plan.
type accountPlanStore struct{ resolver accountPlanResolver }

func (s accountPlanStore) GetTenantPlan(ctx context.Context, tenantID string) (string, error) {
	return s.resolver.GetAccountPlanByTenant(ctx, tenantID)
}

// accountPlan resolves the plan of the tenant's Account (M4). A missing
// account subscription is Starter — the tightest cap, never unlimited. A
// store that does not implement accountPlanResolver degrades to the
// tenant-scoped plan (pre-M4 behavior).
func (l *PlanLimiter) accountPlan(ctx context.Context, tenantID string) (domain.PlanTier, error) {
	resolver, ok := l.store.(accountPlanResolver)
	if !ok {
		return l.tenantPlan(ctx, tenantID)
	}
	plan, err := domain.ResolvePlanTier(ctx, accountPlanStore{resolver}, tenantID)
	if err != nil {
		return domain.PlanTierUnknown, errLimitsUnavailable(tenantID, "resolve account plan", err)
	}
	return plan, nil
}

// limits reads the (hard, soft) pair for a plan and metric. A missing row is
// the (0, 0) sentinel the store returns and the checks deny on; any store
// error is returned as PLATFORM_DEGRADED.
func (l *PlanLimiter) limits(ctx context.Context, tenantID string, plan domain.PlanTier, metric string) (hardLimit, softLimit int, err error) {
	hard, soft, err := l.store.GetLimits(ctx, plan.String(), metric)
	if err != nil {
		return 0, 0, errLimitsUnavailable(tenantID, "read "+metric+" limit", err)
	}
	return hard, soft, nil
}
