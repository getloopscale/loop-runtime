// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package enforcement checks plan limits before allowing mutations.
package enforcement

import (
	"database/sql"
	"fmt"
	"log/slog"
)

// PlanLimiter checks if a tenant's operation is within their plan limits.
type PlanLimiter struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewPlanLimiter creates a limiter backed by the plan_limits and usage tables.
func NewPlanLimiter(db *sql.DB, logger *slog.Logger) *PlanLimiter {
	return &PlanLimiter{db: db, logger: logger}
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

// CheckLimit verifies if a tenant can perform an operation for the given metric.
// Returns LimitResult indicating whether the operation is allowed.
//
// Logic:
//   - Enterprise: always allowed (bypass)
//   - Team with soft_limit: allowed but marked as overage when exceeded
//   - Starter/Pro: blocked (402) when hard_limit exceeded
func (l *PlanLimiter) CheckLimit(tenantID, metric string, currentCount int) LimitResult {
	if l.db == nil {
		// Dev mode without DB — allow everything
		return LimitResult{Allowed: true, Plan: "dev", Metric: metric}
	}

	// Get tenant's plan
	plan := l.getTenantPlan(tenantID)
	if plan == "enterprise" {
		return LimitResult{Allowed: true, Plan: plan, Metric: metric}
	}

	// Get limits for this plan + metric
	hardLimit, softLimit := l.getLimits(plan, metric)
	if hardLimit < 0 {
		// NULL = unlimited — allow
		return LimitResult{Allowed: true, Plan: plan, Metric: metric}
	}
	if hardLimit == 0 {
		// No limit configured or DB error — allow (fail-open for missing config)
		return LimitResult{Allowed: true, Plan: plan, Metric: metric}
	}

	result := LimitResult{
		Plan:    plan,
		Metric:  metric,
		Current: currentCount,
		Limit:   hardLimit,
	}

	// Check soft limit first (Team plan)
	if softLimit > 0 && currentCount >= softLimit {
		result.Allowed = true
		result.SoftOver = true
		l.logger.Info("soft limit exceeded (overage)",
			"tenant", tenantID, "plan", plan, "metric", metric,
			"current", currentCount, "soft_limit", softLimit,
		)
		return result
	}

	// Check hard limit
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

// GetBuildTimeout returns the build timeout for a tenant's plan (in seconds).
func (l *PlanLimiter) GetBuildTimeout(tenantID string) int {
	if l.db == nil {
		return 1800 // 30min default for dev
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
	if l.db == nil {
		return 300 // Default for dev
	}

	plan := l.getTenantPlan(tenantID)
	hardLimit, _ := l.getLimits(plan, "api_requests_per_min")
	if hardLimit <= 0 {
		return 60
	}
	return hardLimit
}

func (l *PlanLimiter) getTenantPlan(tenantID string) string {
	var plan string
	err := l.db.QueryRow(
		"SELECT plan FROM subscriptions WHERE tenant_id = $1 AND status IN ('active', 'trialing') LIMIT 1",
		tenantID,
	).Scan(&plan)
	if err != nil {
		l.logger.Debug("could not determine tenant plan, defaulting to starter", "tenant", tenantID, "error", err)
		return "starter"
	}
	return plan
}

// getLimits returns the hard and soft limits for a plan+metric.
// Returns (-1, -1) if no limit configured.
// Returns (0, 0) on DB error.
// NULL hard_limit = unlimited (returned as -1).
func (l *PlanLimiter) getLimits(plan, metric string) (hardLimit, softLimit int) {
	var hard sql.NullInt64
	var soft sql.NullInt64
	err := l.db.QueryRow(
		"SELECT hard_limit, soft_limit FROM plan_limits WHERE plan = $1 AND metric = $2",
		plan, metric,
	).Scan(&hard, &soft)
	if err != nil {
		return 0, 0
	}
	hardVal := -1 // NULL = unlimited
	if hard.Valid {
		hardVal = int(hard.Int64)
	}
	softVal := 0
	if soft.Valid {
		softVal = int(soft.Int64)
	}
	return hardVal, softVal
}

// FormatLimitError creates a user-facing error message for a plan limit violation.
func FormatLimitError(result LimitResult, upgradeURL string) map[string]interface{} {
	return map[string]interface{}{
		"code":        "PLAN_LIMIT_EXCEEDED",
		"error":       fmt.Sprintf("Plan limit reached: %s allows %d %s (current: %d). Upgrade your plan to continue.", displayPlan(result.Plan), result.Limit, displayMetric(result.Metric), result.Current),
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
