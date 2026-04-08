// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package enforcement

import "time"

// CountRows returns a CountFn that executes a SQL query to count rows.
// The query must have one $1 parameter (tenant_id) and return a single integer.
func CountRows(limiter *PlanLimiter, query string) func(tenantID string) int {
	return func(tenantID string) int {
		if limiter == nil || limiter.db == nil {
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
		if limiter == nil || limiter.db == nil {
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

