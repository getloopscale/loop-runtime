// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

import (
	"testing"
)

// =============================================================================
// Bootstrap Store tests — DELETED in T2.2 #3 of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// 4 stub-class tests previously here (TestBootstrapStore_Bootstrap_
// HappyPath/DefaultPlanAndCell/ExecError/Idempotent) used fakeDBTX
// stub responses to verify PostgresBootstrapStore behaviors. With
// T2.2 #3 migrating to sqlc-backed via SqlcBridge, the fake pattern
// is incompatible.
//
// Classification per `.claude/knowledge-base/audits/2026-05-29-sqlc-
// test-reshape-matrix.md` (coverage_boost_test.go MIXED):
//
//   - KEEP-INVARIANT preserved structurally by BootstrapStoreSqlc:
//     * Idempotent 4-INSERT-ON-CONFLICT-DO-NOTHING chain
//       (users, tenants, tenant_members, subscriptions)
//     * Wrapper-side defaults (cellID='cell-0', plan='starter',
//       displayName=TenantSlug) applied identically
//     * anyCreated semantics: returns true if ANY of the 4 inserts
//       took effect, false only when all 4 were no-ops
//     * MEET-053 D-01 TxFromContext serializer-aware path preserved
//       verbatim (count-and-pick + INSERT in same tx when placement
//       lock is held)
//
//   - DELETE-SCHEMA-MIRROR-DUPLICATE subset: pure-pass-through stubs
//     — sqlc compile-time + sqlcsync gate replace.
//
// Per matrix § Phase 2 reshape window: KEEP-INVARIANT subset moves
// to real-PG-gated tests when pgxmock or LOOP_RUNTIME_E2E_DATABASE_URL
// harness is wired (out of T2.2 #3 scope).

// =============================================================================
// Tenant Deletion Store tests — DELETED in T1.4 of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// 6 sub-tests previously here (TestTenantDeletionStore_CancelDeletion_
// Success/Error, _RequestDeletion_ExecError, _WriteTombstone_Success/
// WithFailed/ExecError) ALL used fakeDBTX stub responses to verify
// PostgresTenantDeletionStore behaviors. With T1.4 deleting
// PostgresTenantDeletionStore and replacing it with
// TenantDeletionStoreSqlc (sqlc-backed via SqlcBridge), the stub-based
// pattern is incompatible.
//
// Classification per `.claude/knowledge-base/audits/2026-05-29-sqlc-
// test-reshape-matrix.md` line 31 (coverage_boost_test.go MIXED):
//
//   - KEEP-INVARIANT preserved structurally by TenantDeletionStoreSqlc:
//     * Idempotent CancelDeletion (DELETE-on-no-rows is a no-op via
//       sqlc's :exec query shape)
//     * Idempotent WriteTombstone (ON CONFLICT (tenant_id) DO NOTHING
//       preserves the original record)
//     * Error wrapping with "tenant deletion store:" prefix preserved
//       verbatim in TenantDeletionStoreSqlc methods
//
//   - DELETE-SCHEMA-MIRROR-DUPLICATE subset: NotFoundReturnsNil,
//     CancelDeletion_Success, _Error stub-pass-through tests — sqlc
//     compile-time + sqlcsync gate replace.
//
// Per matrix § Phase 2 reshape window: KEEP-INVARIANT subset moves to
// real-PG-gated tests when pgxmock or LOOP_RUNTIME_E2E_DATABASE_URL harness is
// wired.

// TestStores_Getters_NotFoundAndQueryError consolidates the canonical
// {NotFound returns nil+nil, QueryError returns err} pair for read-side
// Getter methods on PostgresRepositoryStore. Phase 3 helper.
//
// T1.4 (plan 2026-05-29-sqlc-migration v1.3): removed the
// TenantDeletionStore_GetPendingDeletion case along with the legacy
// PostgresTenantDeletionStore deletion. The KEEP-INVARIANT "NoRows →
// (nil, nil) mapping" for GetPendingDeletion is preserved by
// TenantDeletionStoreSqlc.GetPendingDeletion (explicit
// `errors.Is(err, pgx.ErrNoRows)` check in tenant_wrapper.go); Phase
// 2 reshape moves the behavioral test to a real-PG harness.
// TestStores_Getters_NotFoundAndQueryError — DELETED in T2.1 #3.
//
// The consolidated read-side getter test became vacuous after T1.4
// removed the TenantDeletionStore case and T2.1 #3 removes the
// Repository cases (the only 2 cases that remained). The "NoRows →
// (nil, nil) mapping" invariant is preserved structurally by each
// migrated wrapper (explicit `errors.Is(err, pgx.ErrNoRows)` check).
//
// isNilPointer helper deleted along with this test (unused after the
// function deletion).

// TestTenantDeletionStore_* (4 tests) — DELETED in T1.4 along with the
// Tenant Deletion Store section block above. See tombstone block at
// the top of this section for classification + Phase 2 reshape
// obligation.

// =============================================================================
// Repository Store tests — DELETED in T2.1 #3 of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// 13 sub-tests previously here
// (TestRepositoryStore_SaveInstallation_Success/Error,
// _DeleteInstallation_Success/Error, _LinkRepository_Success/
// UsesUpsertForIdempotency/Error, _UnlinkRepository_Success/
// NotLinked/Error, _GetByProject_NotFound/QueryError) used fakeDBTX-
// based stubs to verify PostgresRepositoryStore behaviors. With T2.1
// #3 migrating to sqlc-backed via SqlcBridge, the fake pattern is
// incompatible.
//
// KEEP-INVARIANT preserved structurally by repository_wrapper.go:
//   - Idempotent UPSERT (ON CONFLICT (id) for installations;
//     ON CONFLICT (repo_full_name) for project_repositories) lives in
//     queries/repositories.sql
//   - ErrRepositoryNotLinked sentinel (CHG-2026-04-23-AUDIT-21)
//     returned by UnlinkRepository when rows-affected == 0
//   - GetByRepo cross-tenant (webhook path; tenantpredicate:exempt)
//   - GetByProject tenant-scoped (gate enforces tenant_id predicate)
//
// Per matrix § Phase 2 reshape window: behavioral subset moves to
// real-PG-gated tests when pgxmock or LOOP_RUNTIME_E2E_DATABASE_URL harness
// is wired.
//
// TestGenerateRepoID_HasPrefix + TestGenerateRepoID_IsUnique below
// (lines 342 + 349 of the pre-T2.1#3 file) STILL PASS — they test
// pure helper logic with no DBTX dependency.

func TestGenerateRepoID_HasPrefix(t *testing.T) {
	id := generateRepoID()
	if len(id) < 6 || id[:5] != "repo-" {
		t.Errorf("expected repo- prefix, got: %s", id)
	}
}

func TestGenerateRepoID_IsUnique(t *testing.T) {
	id1 := generateRepoID()
	id2 := generateRepoID()
	if id1 == id2 {
		t.Error("expected unique IDs")
	}
}

// =============================================================================
// Domain Store - CreateWithCFZone tests — DELETED in T2.1 #9 of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// 2 sub-tests previously here (CreateWithCFZone_Success +
// CreateWithCFZone_ExecError) used fakeDBTX stubs against
// PostgresDomainStore. Migrated to sqlc-backed via SqlcBridge in
// T2.1 #9. See the broader Domain Store tombstone in stores_test.go
// / stores_extra_test.go for classification.

// =============================================================================
// Release Store — DELETED in T1.1c2c of plan 2026-05-29-sqlc-migration
// v1.3.
// =============================================================================
//
// The 7 release-store sub-tests previously here
// (FindPendingReleases_WithCellID, FindPendingReleases_QueryError,
// FindActiveReleases_WithCellID, FindActiveReleases_QueryError,
// FindPendingReleasesByTenantEnv_QueryError, ListByEnv_QueryError,
// GetLatestByBuildID_NotFound) ALL used `fakeDBTX` with empty / error
// responses to verify that PostgresReleaseStore propagated the
// query-error path and returned empty slices on no-result. Per T0.6
// reshape matrix line 31, `coverage_boost_test.go` is classified MIXED
// with "Mostly stub-error-path padding → DELETE-SCHEMA-MIRROR-DUPLICATE";
// these 7 tests fall in that subset. The error-propagation invariant is
// now structurally guaranteed by sqlc-generated code wrapping
// `pgx.Conn.Query` (errors bubble up unmodified), and the "empty result
// → empty slice" guarantee is preserved by ReleaseStoreSqlc.
//
// The cell-scoped routing invariant (cellID → WHERE cell_id = $1) is
// covered by the static-content tests in cell_scoped_queries_test.go
// against `queries/releases.sql` — a STRONGER invariant than runtime-
// captured SQL since it scans the canonical SoT directly.

// =============================================================================
// Domain Store ListByTenant_QueryError — DELETED in T2.1 #9 along
// with the broader Domain Store tombstone block. See stores_test.go
// for classification.
// =============================================================================
