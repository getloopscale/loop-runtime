// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import "time"

// CountRows returns a CountFn that executes a SQL query to count rows.
// The query must have one $1 parameter (tenant_id) and return a single integer.
func CountRows(limiter *PlanLimiter, query string) func(tenantID string) int {
	return func(tenantID string) int {
		if limiter == nil || !limiter.hasStore() || limiter.db == nil {
			return 0
		}
		var count int
		if err := limiter.db.QueryRow(query, tenantID).Scan(&count); err != nil {
			return 0
		}
		return count
	}
}

// CountBuildsThisMonth returns a CountFn that counts builds created this month.
func CountBuildsThisMonth(limiter *PlanLimiter) func(tenantID string) int {
	return func(tenantID string) int {
		if limiter == nil || !limiter.hasStore() || limiter.db == nil {
			return 0
		}
		now := time.Now().UTC()
		periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		var count int
		if err := limiter.db.QueryRow(
			"SELECT COUNT(*) FROM builds WHERE tenant_id = $1 AND created_at >= $2",
			tenantID, periodStart,
		).Scan(&count); err != nil {
			return 0
		}
		return count
	}
}

// CountPermanentEnvironments returns a CountFn that counts ONLY
// permanent environments for a tenant. D5 of plan
// 2026-06-07-preview-as-category-single-slot-plan.md (Bug A fix).
//
// Preview environments live under the single-slot model
// (preview_slot table) and MUST NOT be counted against
// plan_limits.environments. Before migration 000027, the enforcement
// middleware ran `SELECT COUNT(*) FROM environment_targets WHERE
// tenant_id = $1` which mixed both kinds — a tenant with one
// permanent prod env + 2 preview-pr-N rows hit "current=3, max=3"
// even on Pro plan.
//
// M2 v1.2 (migration 000032, plan 2026-06-07-runtime-envs-per-project-m2):
// the `environments` cap is now interpreted per-(project, env) tuple.
// Pre-M2, the DISTINCT on `environment` made sense because the legacy
// path stored an env name once per tenant (no project segment). Post-M2,
// the PK of environment_targets is the full (tenant_id, project_id,
// environment) tuple — each unique (project, env) is one row, so a
// plain COUNT(*) is the natural per-tuple count.
//
// Example: a tenant with (web, staging), (web, prod), (api, staging),
// (api, prod) hits COUNT(*)=4 post-M2 (DISTINCT environment would have
// said 2 — that was the bug we're fixing). The enforcement middleware
// compares this aggregate against plan_limits.environments.hard_limit
// times projects.hard_limit (the ADR D4 ceiling); the per-project cap
// is enforced separately at the create-time path in routes/.
func CountPermanentEnvironments(limiter *PlanLimiter) func(tenantID string) int {
	return func(tenantID string) int {
		if limiter == nil || !limiter.hasStore() || limiter.db == nil {
			return 0
		}
		var count int
		if err := limiter.db.QueryRow(
			// M2 v1.2 T7.2: COUNT(*) counts (project, env) tuples per tenant.
			// PK guarantees uniqueness — no DISTINCT needed. Filter kind=permanent
			// remains (previews live in preview_slot, not env_targets).
			"SELECT COUNT(*) FROM environment_targets WHERE tenant_id = $1 AND kind = 'permanent'",
			tenantID,
		).Scan(&count); err != nil {
			return 0
		}
		return count
	}
}
