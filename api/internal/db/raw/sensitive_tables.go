// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package raw is the escape hatch for sqlc-unparseable SQL queries
// in the LoopZero API db layer. See README.md for scope (≤5% of all
// queries), forbidden tables, and contribution rules.
//
// T4.1 of plan docs/plans/2026-05-29-sqlc-migration-plan.md v1.3.
//
// The package emits one symbol per query, named after the legacy
// store method (e.g., DeleteOldBuilds). All raw queries dispatch
// through db.DBTX (the same testable abstraction the sqlc-backed
// wrappers use) so the same test doubles work uniformly.
package raw

// SensitiveTables is the committed exclusion list of tables that
// raw queries CANNOT touch. Enforced by the pkg/quality/tenantpredicate/
// gate which scans every .go file under this package for references
// to these table names.
//
// Rationale per table:
//   - secrets: AES-256-GCM encryption boundary lives in the
//     wrapper (SEC-001); any raw access bypasses encryption.
//   - audit_log: M-3 audit-callsite-diff invariant — uniform shape
//     across all 6 prod call sites; raw drift breaks audit trail.
//   - users/tenants/tenant_members/subscriptions: bootstrap chain
//     has TOCTOU race fixed via MEET-053 D-01 SERIALIZABLE-via-
//     TxFromContext; raw INSERT would miss the serializer.
//   - tenants_pending_deletion/tenant_tombstones: T1.4 lifecycle
//     invariants must dispatch through TenantDeletionStoreSqlc.
//   - cells: placement primitives — admin override path goes
//     through PostgresCellStore.PinTenantToCell with 4-step
//     pre-flight (existence + status + capacity + atomic update).
//
// Changes to this list require T2 governance approval — the
// security-paired and serializer-paired invariants are platform-
// security-engineer + tenant-guardian signoff territory.
var SensitiveTables = []string{
	"secrets",
	"audit_log",
	"users",
	"tenants",
	"tenant_members",
	"subscriptions",
	"tenants_pending_deletion",
	"tenant_tombstones",
	"cells",
}

// IsSensitive returns true if the given table name is on the
// SensitiveTables list (case-insensitive).
func IsSensitive(table string) bool {
	for _, t := range SensitiveTables {
		if eqIgnoreCase(t, table) {
			return true
		}
	}
	return false
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
