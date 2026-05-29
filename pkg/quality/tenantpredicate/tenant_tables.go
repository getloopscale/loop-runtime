// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenantpredicate

// TenantOwnedTables is the canonical list of tables that REQUIRE a
// `tenant_id = $N` predicate (or equivalent JOIN-based filter) in every
// SELECT / UPDATE / DELETE, AND a `tenant_id` column in every INSERT.
//
// This list is SECURITY-CRITICAL. Reviewed + signed off by
// platform-security-engineer + tenant-guardian per M-1 of
// MEET-2026-05-29-001 (sqlc T3 governance). Changes require their
// re-sign-off + a new T2 meeting.
//
// Initial list derived from the 19 forward-only migrations under
// api/internal/db/migrations/ (commits up through 000020). Tables that
// exist but are NOT tenant-owned (e.g., `cells` operational metadata,
// `schema_migrations` framework) are deliberately excluded.
//
// The `tenants` table itself is a SPECIAL CASE: it owns the tenant
// identity, so `tenant_id = $N` would be redundant. Queries on `tenants`
// should use `id = $N` filter. The gate exempts queries on the `tenants`
// table from the tenant_id predicate check (handled via TenantSelfTable).
//
// References:
//   - docs/plans/2026-05-29-sqlc-migration-plan.md v1.2 § T0.5
//   - docs/decisions/2026-05-29-sqlc-adoption.md ADR-1 § Conflict 3
//     resolution + § 6 platform-security-engineer condition 1
//   - api/internal/db/schema.gen.sql (CI-rendered source of truth for
//     what tables actually exist)
var TenantOwnedTables = []string{
	"tenants",                  // self-table, exempted from tenant_id predicate (uses id = $N)
	"projects",
	"environments",
	"environment_targets",
	"builds",
	"releases",
	"release_app_attestations",
	"secrets",
	"audit_log",
	"scaling_configs",
	"app_scaling_config",
	"custom_domains",
	"idempotency_keys",
	"memberships",
	"tenant_members",
	"repositories",
	"preview_environments",
	"teams",
	"active_teams",
	"tenants_pending_deletion", // T1.4 add (PostgresTenantDeletionStore); replaces obsolete `tenant_deletion` stub
	"tenant_tombstones",        // T1.4 add (terminal-state audit record; keyed by tenant_id)
	"cost_counters",
	"users",                    // tenant-scoped via owner_id / active_tenant_id
}

// TenantSelfTable is the table that IS the tenant identity. Queries on
// this table cannot use `tenant_id = $N` (no such column); they use
// `id = $N` instead. Exempt from the tenant_id predicate check.
const TenantSelfTable = "tenants"

// IsTenantOwned returns true if the given table name (case-insensitive)
// is in the tenant-owned list.
func IsTenantOwned(table string) bool {
	for _, t := range TenantOwnedTables {
		if eqIgnoreCase(t, table) {
			return true
		}
	}
	return false
}

// IsTenantSelfTable returns true if the table is the `tenants` table
// itself.
func IsTenantSelfTable(table string) bool {
	return eqIgnoreCase(table, TenantSelfTable)
}

func eqIgnoreCase(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 32
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}
