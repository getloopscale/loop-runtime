// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package enforcement checks plan limits before allowing mutations.
// Shared between cloud and engine — zero external dependencies at the
// API surface (database/sql is used only for the default SQL adapter).
//
// Per ADR-0033, PlanLimiter accepts a LimitsStore interface so callers
// that want to test with an in-memory fake, route through OPA, or
// otherwise decouple from the SQL backend can do so without rewriting
// pkg/enforcement.
//
// Backward compatibility: NewPlanLimiter(*sql.DB, logger) continues to
// work exactly as before — internally it wraps the *sql.DB in
// NewSQLLimitsStore. New consumers should prefer
// NewPlanLimiterWithStore for cleaner DIP.
package enforcement

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
)

// PlanLimiter checks if a tenant's operation is within their plan limits.
type PlanLimiter struct {
	store             LimitsStore
	db                *sql.DB // retained for the legacy API surface and tests
	logger            *slog.Logger
	OnFallbackStarter func() // Called when subscription row is missing (for metrics)
}

// NewPlanLimiter creates a limiter backed by a *sql.DB. This is the
// legacy entry point used by api/cmd/main.go; under the hood it wraps
// the DB in a SQL LimitsStore adapter so all internal lookups go
// through the interface.
func NewPlanLimiter(db *sql.DB, logger *slog.Logger) *PlanLimiter {
	var store LimitsStore
	if db != nil {
		store = NewSQLLimitsStore(db)
	}
	return &PlanLimiter{store: store, db: db, logger: logger}
}

// NewPlanLimiterWithStore creates a limiter backed by an arbitrary
// LimitsStore. Use this in tests with an in-memory fake or in
// alternate backends (OPA, file-based config) per ADR-0033.
func NewPlanLimiterWithStore(store LimitsStore, logger *slog.Logger) *PlanLimiter {
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

// CheckLimit verifies if a tenant can perform an operation for the given metric.
func (l *PlanLimiter) CheckLimit(tenantID, metric string, currentCount int) LimitResult {
	if !l.hasStore() {
		return LimitResult{Allowed: true, Plan: "dev", Metric: metric}
	}

	plan := l.getTenantPlan(tenantID)
	if plan == "enterprise" {
		return LimitResult{Allowed: true, Plan: plan, Metric: metric}
	}

	hardLimit, softLimit := l.getLimits(plan, metric)
	if hardLimit < 0 {
		return LimitResult{Allowed: true, Plan: plan, Metric: metric}
	}
	if hardLimit == 0 {
		// hardLimit==0 means no limit row found — fail closed for safety.
		// Enterprise plans are already handled above. A missing row for
		// a non-enterprise plan should deny, not silently allow.
		l.logger.Warn("no limit configured (denying by default)",
			"tenant", tenantID, "plan", plan, "metric", metric,
		)
		return LimitResult{
			Allowed: false,
			Plan:    plan,
			Metric:  metric,
			Current: currentCount,
			Limit:   0,
		}
	}

	result := LimitResult{
		Plan:    plan,
		Metric:  metric,
		Current: currentCount,
		Limit:   hardLimit,
	}

	if softLimit > 0 && currentCount >= softLimit {
		result.Allowed = true
		result.SoftOver = true
		l.logger.Info("soft limit exceeded (overage)",
			"tenant", tenantID, "plan", plan, "metric", metric,
			"current", currentCount, "soft_limit", softLimit,
		)
		return result
	}

	if currentCount >= hardLimit {
		result.Allowed = false
		l.logger.Info("hard limit exceeded",
			"tenant", tenantID, "plan", plan, "metric", metric,
			"current", currentCount, "hard_limit", hardLimit,
		)
		return result
	}

	result.Allowed = true
	return result
}

// CheckValue verifies if a REQUESTED value (not accumulated usage) is within
// a tenant's plan cap for the given metric. Unlike CheckLimit, this answers
// "is the user allowed to request this exact value?" — used for per-resource
// intent checks such as replica count or secret size.
//
// B15 (CHG-2026-04-23-B15 / MEET-2026-04-23-059).
//
// Semantics (matches MEET-059 Stage 2 test contract):
//   - nil limiter / nil DB            → Allowed (dev fall-through, same as CheckLimit)
//   - enterprise plan                 → Allowed (unlimited)
//   - hard_limit NULL (< 0)           → Allowed (unlimited for this plan)
//   - hard_limit == 0 (missing row)   → Denied (fail closed, logged)
//   - requested <= hard_limit         → Allowed
//   - requested > hard_limit          → Denied, LimitResult.Current == requested
//
// Reports LimitResult.Current as the REQUESTED value (not current usage),
// so `FormatLimitError` surfaces it to the user.
func (l *PlanLimiter) CheckValue(tenantID, metric string, requested int) LimitResult {
	if !l.hasStore() {
		return LimitResult{Allowed: true, Plan: "dev", Metric: metric, Current: requested}
	}

	plan := l.getTenantPlan(tenantID)
	if plan == "enterprise" {
		return LimitResult{Allowed: true, Plan: plan, Metric: metric, Current: requested}
	}

	hardLimit, _ := l.getLimits(plan, metric)
	if hardLimit < 0 {
		return LimitResult{Allowed: true, Plan: plan, Metric: metric, Current: requested}
	}
	if hardLimit == 0 {
		l.logger.Warn("no value limit configured (denying by default)",
			"tenant", tenantID, "plan", plan, "metric", metric, "requested", requested,
		)
		return LimitResult{
			Allowed: false,
			Plan:    plan,
			Metric:  metric,
			Current: requested,
			Limit:   0,
		}
	}

	result := LimitResult{
		Plan:    plan,
		Metric:  metric,
		Current: requested,
		Limit:   hardLimit,
	}
	if requested > hardLimit {
		result.Allowed = false
		l.logger.Info("requested value exceeds plan cap",
			"tenant", tenantID, "plan", plan, "metric", metric,
			"requested", requested, "hard_limit", hardLimit,
		)
		return result
	}
	result.Allowed = true
	return result
}

// GetBuildTimeout returns the build timeout for a tenant's plan (in seconds).
func (l *PlanLimiter) GetBuildTimeout(tenantID string) int {
	if !l.hasStore() {
		return 1800
	}
	plan := l.getTenantPlan(tenantID)
	hardLimit, _ := l.getLimits(plan, "build_timeout_seconds")
	if hardLimit <= 0 {
		return 1800
	}
	return hardLimit
}

// GetRateLimit returns the API rate limit for a tenant's plan (requests per minute).
func (l *PlanLimiter) GetRateLimit(tenantID string) int {
	if !l.hasStore() {
		return 300
	}
	plan := l.getTenantPlan(tenantID)
	hardLimit, _ := l.getLimits(plan, "api_requests_per_min")
	if hardLimit <= 0 {
		return 60
	}
	return hardLimit
}

func (l *PlanLimiter) getTenantPlan(tenantID string) string {
	plan, err := l.store.GetTenantPlan(context.Background(), tenantID)
	if err != nil || plan == "" {
		l.logger.Warn("subscription row missing — falling back to starter plan",
			"tenant", tenantID, "error", err)
		if l.OnFallbackStarter != nil {
			l.OnFallbackStarter()
		}
		return "starter"
	}
	return plan
}

func (l *PlanLimiter) getLimits(plan, metric string) (hardLimit, softLimit int) {
	hard, soft, err := l.store.GetLimits(context.Background(), plan, metric)
	if err != nil {
		// sql.ErrNoRows OR comparable not-found → 0/0 sentinel preserved
		// for fail-closed semantics. Other errors are also treated as
		// missing row (defensive — limiter still surfaces denial via the
		// limiter's "no limit configured" path).
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0
		}
		return 0, 0
	}
	return hard, soft
}

// FormatLimitError creates a user-facing error message for a plan limit violation.
func FormatLimitError(result LimitResult, upgradeURL string) map[string]interface{} {
	return map[string]interface{}{
		"code":        "PLAN_LIMIT_EXCEEDED",
		"message":     fmt.Sprintf("Plan limit reached: %s allows %d %s (current: %d). Upgrade your plan to continue.", displayPlan(result.Plan), result.Limit, displayMetric(result.Metric), result.Current),
		"plan":        result.Plan,
		"limit":       result.Metric,
		"current":     result.Current,
		"max":         result.Limit,
		"upgrade_url": upgradeURL,
	}
}

func displayPlan(plan string) string {
	switch plan {
	case "starter":
		return "Starter"
	case "pro":
		return "Pro"
	case "team":
		return "Team"
	default:
		return plan
	}
}

func displayMetric(metric string) string {
	switch metric {
	case "projects":
		return "projects"
	case "services_per_project":
		return "services per project"
	case "environments":
		return "environments"
	case "builds_per_month":
		return "builds per month"
	case "seats":
		return "team members"
	case "secrets_per_env":
		return "secrets per environment"
	default:
		return metric
	}
}
