// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"fmt"
)

// PostgresTenantStore provides direct tenant-level queries used by admin
// primitives such as bulk eviction (T-02 in docs/plans/50k-immediate-focus.md).
//
// Unlike the high-traffic stores (build, release, project), this store is only
// exercised by operator-level endpoints that are feature-gated behind
// LOOPZERO_ADMIN_API. Queries here intentionally bypass tenant scoping — the caller
// is already an operator and needs to read/write across tenants.
type PostgresTenantStore struct {
	DB DBTX
}

// ListTenantIDsByPattern returns tenant IDs matching a SQL LIKE pattern.
//
// Used by the AdminEvictHandler to resolve patterns like "tn-lt-l3-%" into a
// concrete set of IDs before enqueueing eviction. The pattern is applied
// directly to the tenants.id column with ESCAPE '\' to allow literal _/%
// characters if the caller needs them.
//
// Security: the caller is responsible for rejecting dangerous free-text
// patterns ('%', 'tn-%') BEFORE calling this — see admin_evict.go validation.
// This method does NOT validate the pattern; it trusts the handler layer.
//
// Results are sorted ASC for determinism. Limited to 10_000 rows to prevent
// pathological patterns from materializing the entire tenants table into
// memory. Patterns that need more than 10_000 matches should be split.
func (s *PostgresTenantStore) ListTenantIDsByPattern(ctx context.Context, pattern string) ([]string, error) {
	rows, err := s.DB.Query(ctx,
		`SELECT id FROM tenants WHERE id LIKE $1 ESCAPE '\' ORDER BY id ASC LIMIT 10000`,
		pattern,
	)
	if err != nil {
		return nil, fmt.Errorf("tenant store: list by pattern: %w", err)
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("tenant store: scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tenant store: rows error: %w", err)
	}
	return ids, nil
}

// TenantDeletionCounts reports how many rows were removed per table during a
// single call to DeleteTenantsFKOrdered. Exposed for auditing and for the
// AdminEvictHandler response; the bash equivalent (tools/admin-evict-tenants.sh)
// only printed "remaining=0" to stdout.
type TenantDeletionCounts struct {
	EnvironmentTargets int64 `json:"environment_targets"`
	Releases           int64 `json:"releases"`
	BuildApps          int64 `json:"build_apps"`
	Builds             int64 `json:"builds"`
	Projects           int64 `json:"projects"`
	Subscriptions      int64 `json:"subscriptions"`
	TenantMembers      int64 `json:"tenant_members"`
	Tenants            int64 `json:"tenants"`
}

// DeleteTenantsFKOrdered removes all rows associated with tenantIDs across the
// 8 tables touched by loadtest cleanup, in FK-safe order. The caller supplies
// the concrete list of IDs (resolved earlier via ListTenantIDsByPattern) so
// that pattern matching races cannot accidentally widen the blast radius.
//
// The method expects a transaction (DBTX): the caller is responsible for
// BEGIN/COMMIT. This makes the whole cleanup atomic — either every row for
// every tenant in the list is gone, or none of them are.
//
// FK order (validated by tools/admin-evict-tenants.sh step 5):
//
//   environment_targets → releases → build_apps → builds → projects →
//   subscriptions → tenant_members → tenants
//
// Each child table is purged before its parent. Releases/build_apps reference
// builds (not tenants directly), so they use a sub-SELECT against builds.
//
// Notes:
//   - If tenantIDs is empty, this is a no-op returning zero counts.
//   - The queries use `= ANY($1::text[])` which is pgx-idiomatic and safer
//     than building an IN-list manually.
//   - Audit log rows are retained intentionally: eviction is itself auditable.
func (s *PostgresTenantStore) DeleteTenantsFKOrdered(ctx context.Context, tenantIDs []string) (TenantDeletionCounts, error) {
	var counts TenantDeletionCounts
	if len(tenantIDs) == 0 {
		return counts, nil
	}

	type step struct {
		sql   string
		tally *int64
	}
	steps := []step{
		{`DELETE FROM environment_targets WHERE tenant_id = ANY($1::text[])`, &counts.EnvironmentTargets},
		{`DELETE FROM releases WHERE build_id IN (SELECT id FROM builds WHERE tenant_id = ANY($1::text[]))`, &counts.Releases},
		{`DELETE FROM build_apps WHERE build_id IN (SELECT id FROM builds WHERE tenant_id = ANY($1::text[]))`, &counts.BuildApps},
		{`DELETE FROM builds WHERE tenant_id = ANY($1::text[])`, &counts.Builds},
		{`DELETE FROM projects WHERE tenant_id = ANY($1::text[])`, &counts.Projects},
		{`DELETE FROM subscriptions WHERE tenant_id = ANY($1::text[])`, &counts.Subscriptions},
		{`DELETE FROM tenant_members WHERE tenant_id = ANY($1::text[])`, &counts.TenantMembers},
		{`DELETE FROM tenants WHERE id = ANY($1::text[])`, &counts.Tenants},
	}

	for i, st := range steps {
		res, err := s.DB.Exec(ctx, st.sql, tenantIDs)
		if err != nil {
			return counts, fmt.Errorf("tenant store: step %d (%s): %w", i+1, truncateSQL(st.sql), err)
		}
		*st.tally = res.RowsAffected
	}
	return counts, nil
}

// truncateSQL trims a SQL query string for error context without leaking
// the full body. Keeps error messages compact and safe.
func truncateSQL(s string) string {
	const max = 40
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
