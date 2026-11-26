// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package db

import (
	"testing"
)

// =============================================================================
// Test fakes — enhanced DBTX implementation for unit testing
// =============================================================================

// FakeDBTX implements DBTX with configurable responses per call.

// =============================================================================
// safeTransition tests (DELETED iter 38)
// =============================================================================
//
// TestSafeTransition was deleted iter 38 after the local `safeTransition`
// helper in build_store.go was removed. Coverage moved to
// `pkg/statemachine/safe_transition_test.go` which exercises every
// typed wrapper's `SafeTransition` method uniformly (Build + Release +
// 9 other machines). The previous Build+Release-specific table-driven
// test is subsumed by the broader per-machine coverage.

// =============================================================================
// Build Store tests — DELETED in T1.2b of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// 22 sub-tests previously here (TestBuildStore_Create_*,
// _GetByID_*, _HasActiveBuild_*, _UpdateStatus_*,
// _Setters_*, _SetAppsSpec_*, _GetBuildApps_*,
// _ListByProject*, _CancelActiveBuilds_*) ALL used `FakeDBTX` to verify
// PostgresBuildStore behaviors against stub responses. With T1.2b
// deleting PostgresBuildStore and replacing it with BuildStoreSqlc
// (sqlc-backed via SqlcBridge), these tests can no longer compile.
//
// Classification per `.claude/knowledge-base/audits/2026-05-29-sqlc-
// test-reshape-matrix.md` line 37 (`stores_test.go` MIXED):
//
//   - KEEP-INVARIANT subset preserved structurally by BuildStoreSqlc:
//       * Create_UniqueViolationReturnsDeployInProgress —
//         isPGUniqueViolation in build_wrapper.go covers pgconn.PgError
//         23505 + legacy UniqueViolationError dual paths
//       * HasActiveBuild_True/False — concurrent-deploy invariant
//         (Rule 6 + ARCH-002) verified by integration suite under
//         api/tests/integration/concurrent_builds_test.go (T1.2b
//         reshape to NewBuildStoreSqlc)
//       * UpdateStatus_ValidTransition + UpdateStatus_InvalidTransition
//         panic discipline — guaranteed by state-machine SafeTransition
//         at store layer (D4) + buildMachine TypedMachine compile-time
//         constants
//       * ListByProjectAndEnv_ClampsLimit — limit clamp (1-50, default
//         20) preserved verbatim in BuildStoreSqlc.ListByProjectAndEnv
//
//   - DELETE-SCHEMA-MIRROR-DUPLICATE subset: Create_SetsQueuedStatus*,
//     Create_PreservesExistingID, Create_GenericErrorWrapped,
//     Setters_Success_NotFound_NilTx, SetAppsSpec_MarshalAndExec,
//     GetBuildApps_*, *NilTxUsesPool family, _EmptyReturnsEmptySlice —
//     sqlc compile-time + sqlcsync gate replace.
//
// Per matrix § "Per-phase action plan" Phase 2 (T2.1-T2.4 batches):
// additional KEEP-INVARIANT subset reshape for build store belongs to
// Phase 2 when a real-PG-gated unit test harness is wired (pgxmock or
// LOOPRUNTIME_E2E_DATABASE_URL).
// =============================================================================
// Release Store tests — DELETED in T1.1c2c of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// 19 sub-tests previously here (TestReleaseStore_Create_*,
// _GetByID_*, _UpdateStatus_*, _GetActiveByEnv_*, _GetPreviousRelease_*,
// _GetByIDForTenant_*, _GetLatestByBuildID_*, _SetGitCommit_*,
// _FindPendingReleases_*, _FindActiveReleases_*,
// _FindPendingReleasesByTenantEnv_*, _ListByEnv_*, _GetByIDTx_*) ALL
// used `FakeDBTX` to verify PostgresReleaseStore behaviors against stub
// responses. With T1.1c2c deleting PostgresReleaseStore and replacing
// it with ReleaseStoreSqlc (sqlc-backed via SqlcBridge), these tests
// can no longer compile.
//
// Classification per `.claude/knowledge-base/audits/2026-05-29-sqlc-
// test-reshape-matrix.md` line 37 (`stores_test.go` MIXED):
//
//   - KEEP-INVARIANT subset (per matrix): UpdateStatus_ValidTransition,
//     UpdateStatus_InvalidTransition (panic discipline), UpdateStatus_
//     NotFound (NoRows → ErrReleaseNotFound), *_NotFoundReturnsNil
//     family. These behaviors ARE preserved by ReleaseStoreSqlc:
//       - State-machine SafeTransition at store layer (D4 invariant) —
//         verified by compile-time assertion `var _ ReleaseStore =
//         (*ReleaseStoreSqlc)(nil)` in release_wrapper.go
//       - NoRows → ErrReleaseNotFound mapping explicit in wrapper
//         (release_wrapper.go GetByID, GetByIDTx, GetByIDForTenant)
//       - Integration suite under api/tests/integration/ exercises the
//         same behaviors against real PG via NewReleaseStoreSqlc
//         (reshaped in T1.1c2c)
//
//   - DELETE-SCHEMA-MIRROR-DUPLICATE subset: Create_SetsReadyStatus*,
//     Create_PreservesExistingID, Create_ExecError, _NilTxUsesPool
//     family, _ReturnsEmptySlice family, _Empty family — sqlc compile-
//     time + sqlcsync gate replace.
//
// Per matrix § "Per-phase action plan" Phase 2 (T2.1-T2.4 batches):
// additional KEEP-INVARIANT subset reshape for release belongs to Phase
// 2 when a real-PG-gated unit test harness is wired (pgxmock or
// LOOPRUNTIME_E2E_DATABASE_URL).

// =============================================================================
// =============================================================================
// Domain Store tests — DELETED in T2.1 #9 of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// All Domain Store test sub-blocks across stores_test.go (lines
// 294-473 + 812-876) and stores_extra_test.go (3 sections: Domain
// Store additional + Background methods + CF Zone methods) used
// FakeDBTX / RecordingDBTX / countingDBTX stubs to verify
// PostgresDomainStore behaviors. With T2.1 #9 migrating the 3-file
// legacy split (domain_store.go + domain_store_background.go +
// domain_store_cfzone.go) to a single sqlc-backed wrapper via
// SqlcBridge, the fake pattern is incompatible.
//
// KEEP-INVARIANT preserved structurally by domain_wrapper.go:
//   - Create + CreateWithCFZone 23505 → domain.ErrDomainExists
//   - GetByDomainAndTenant NoRows → (nil, nil)
//   - ListByTenant dynamic filter via sqlc.narg() (SEC-005 verified
//     by code generation — no fmt.Sprintf raw interpolation)
//   - Single-domain mutations rows-affected=0 → "domain not found"
//   - TransitionStatus CAS preserved (CRIT-01 fix)
//   - ClaimByStatus atomic UPDATE...FOR UPDATE SKIP LOCKED (FIND-001)
//   - FindPendingForAutoVerify cell-scoped JOIN to tenants
//
// Per matrix § Phase 2 reshape window: behavioral subset moves to
// real-PG-gated tests when pgxmock or LOOPRUNTIME_E2E_DATABASE_URL harness
// is wired.

// =============================================================================
// Project Store tests — DELETED in T2.1 #4 of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// 8 sub-tests previously here (TestProjectStore_CreateProject_Success/
// UniqueViolation, _EnsureProject_Success/ConflictIsIdempotent,
// _DeleteProject_Success/NotFound, _GetByName_NotFoundReturnsNil,
// _ListByTenant_EmptyReturnsEmptySlice) used FakeDBTX/RecordingDBTX
// stubs to verify PostgresProjectStore behaviors. With T2.1 #4
// migrating to sqlc-backed via SqlcBridge, the fake pattern is
// incompatible.
//
// KEEP-INVARIANT preserved structurally by project_wrapper.go:
//   - 23505 unique_violation → domain.ErrProjectExists via
//     isPGUniqueViolation helper
//   - EnsureProject idempotent (ON CONFLICT DO NOTHING in
//     queries/projects.sql)
//   - DeleteProject 0-rows-affected → "project not found" error
//   - GetByName NoRows → (nil, nil) via errors.Is(err, pgx.ErrNoRows)
//   - ListByTenant empty result → empty slice (make([]T, 0, len(rows))
//     pattern; tenant predicate in SQL)
//
// Per matrix § Phase 2 reshape window: behavioral subset moves to
// real-PG-gated tests when pgxmock or LOOPRUNTIME_E2E_DATABASE_URL harness
// is wired.

// =============================================================================
// Tenant Store tests — DELETED in T1.4 of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// 5 sub-tests previously here (TestTenantStore_ListTenantIDsByPattern_
// Success/Empty, _DeleteTenantsFKOrdered_EmptyNoOp/ExecutesAllSteps/
// ErrorStopsExecution) + the failOnNthExecDBTX wrapper + TestTruncateSQL
// (testing the now-removed truncateSQL helper that lived in
// tenant_store.go) — ALL used FakeDBTX-based stubs to verify
// PostgresTenantStore behaviors. With T1.4 deleting PostgresTenantStore
// and replacing it with TenantStoreSqlc (sqlc-backed via SqlcBridge),
// the stub pattern is incompatible.
//
// Classification per `.claude/knowledge-base/audits/2026-05-29-sqlc-
// test-reshape-matrix.md` line 37 (stores_test.go MIXED):
//
//   - KEEP-INVARIANT subset preserved structurally by TenantStoreSqlc:
//     * Empty-input no-op (`if len(tenantIDs) == 0 { return counts, nil }`
//       in TenantStoreSqlc.DeleteTenantsFKOrdered) — direct early
//       return, structurally identical to legacy
//     * 8-step FK-ordered cascade preserved verbatim — same step
//       sequence (environment_targets → releases → build_apps →
//       builds → projects → subscriptions → tenant_members → tenants)
//       with step-name annotated error wrapping (more informative
//       than legacy truncateSQL output)
//     * Error stops execution: the wrapper iterates the steps slice
//       and returns on first non-nil err — same loop discipline as
//       legacy
//
//   - DELETE-SCHEMA-MIRROR-DUPLICATE subset: ListTenantIDsByPattern_
//     Success/Empty stub-pass-through; failOnNthExecDBTX helper
//     (legacy-specific test scaffolding); TestTruncateSQL (testing a
//     helper that no longer exists). sqlc compile-time replaces.
//
// Per matrix § Phase 2 reshape window: KEEP-INVARIANT subset moves to
// real-PG-gated tests when pgxmock or LOOPRUNTIME_E2E_DATABASE_URL harness is
// wired.

// =============================================================================
// Team Store tests — DELETED in T2.1 #5 of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// 7 sub-tests previously here (TestTeamStore_AddMember_Success,
// _RemoveMember_Success, _UpdateMemberRole_Success/NotFound,
// _ListMembers_Empty, _GetMember_NotFound, _GetUserByUsername_NotFound)
// used FakeDBTX/RecordingDBTX stubs to verify PostgresTeamStore.
// With T2.1 #5 migrating to sqlc-backed via SqlcBridge, the fake
// pattern is incompatible.
//
// KEEP-INVARIANT preserved structurally by team_wrapper.go:
//   - AddMember idempotent (ON CONFLICT DO NOTHING in
//     queries/teams.sql)
//   - RemoveMember idempotent (silent on missing rows)
//   - UpdateMemberRole 0-rows-affected → domain.ErrMemberNotFound
//   - GetMember/GetUsernameByID/GetUserByUsername NoRows mappings
//   - AUDIT-7 username via JOIN to users.username (provider-agnostic)
//   - ListMembers returns empty slice (not nil) for consistent JSON
//
// Per matrix § Phase 2 reshape window: behavioral subset moves to
// real-PG-gated tests when pgxmock or LOOPRUNTIME_E2E_DATABASE_URL harness
// is wired.

// CHG-2026-04-23-AUDIT-7: TestStripGitHubPrefix removed along with the
// helper it tested. Username is now a column on users, not derived from
// the id. See migration 000037 and team_store.go.

// =============================================================================
// Environment Target Store tests — DELETED in T2.1 #6 of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// 8 sub-tests previously here (TestEnvironmentTargetStore_Mutators
// with 3 mutators × 2 variants = 6 sub-tests +
// _Get_NotFoundReturnsNil + _ListByProject_EmptyReturnsEmptySlice)
// used FakeDBTX/RecordingDBTX stubs to verify
// PostgresEnvironmentTargetStore. With T2.1 #6 migrating to sqlc-
// backed via SqlcBridge, the fake pattern is incompatible.
//
// KEEP-INVARIANT preserved structurally by
// environment_target_wrapper.go:
//   - SetDesired UPSERT (ON CONFLICT (tenant_id, project_id,
//     environment) DO UPDATE in queries/environment_targets.sql)
//   - SetActive runs after ArgoCD confirms sync (caller discipline)
//   - tx-required form for mutating methods (sqlctxprop gate
//     validates)
//   - Nil-tx fallback to s.Exec preserved verbatim in
//     resolveSqlcDBTX helper
//   - Get NoRows → (nil, nil) via errors.Is(err, pgx.ErrNoRows)
//   - ListByProject empty → empty slice (make([]T, 0, len(rows)))
//
// Per matrix § Phase 2 reshape window: behavioral subset moves to
// real-PG-gated tests when pgxmock or LOOPRUNTIME_E2E_DATABASE_URL harness
// is wired.

// =============================================================================
// Idempotency Store tests — DELETED in T2.1 (Batch A) of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// 4 sub-tests previously here (TestIdempotencyStore_Get_NotFound
// ReturnsNil, _Get_Success, _Store_Success, _Cleanup_Success) used
// FakeDBTX-based stubs to verify PostgresIdempotencyStore behaviors.
// With T2.1 widening the DB field to db.DBTX + sqlc-backed methods
// dispatching through SqlcBridge, the fake pattern is incompatible.
//
// KEEP-INVARIANT preserved structurally by sqlc-backed
// PostgresIdempotencyStore (idempotency_wrapper.go):
//   - Get NoRows → (nil, nil) via errors.Is(err, pgx.ErrNoRows)
//   - Tenant predicate on Get (queries/idempotency.sql carries
//     `tenant_id = $2`, gate enforces)
//   - INSERT shape preserved (sqlc emits same VALUES tuple)
//   - Cleanup returns int64 row count (sqlc :execrows shape matches
//     legacy)
//   - 24-hour TTL applied at Store-time via idempotencyTTL constant
//
// Per matrix § Phase 2 reshape window: behavioral subset moves to
// real-PG-gated tests when pgxmock or LOOPRUNTIME_E2E_DATABASE_URL harness
// is wired.

// =============================================================================
// Audit Store tests
// =============================================================================

// TestAuditStore_ListAuditEntries_EmptyResult — DELETED in T1.5 of
// plan 2026-05-29-sqlc-migration v1.3.
//
// The FakeDBTX-based test cannot run against the sqlc-backed
// PostgresAuditStore.ListAuditEntries — the wrapper bridges to
// generated.DBTX via SqlcBridge, which FakeDBTX deliberately doesn't
// implement (T0.6 reshape matrix). The empty-result behavior is
// PRESERVED structurally: when CountAuditLogForTenant returns 0 +
// ListAuditLogForTenant returns no rows, the wrapper emits
// `make([]domain.AuditEntryResult, 0, len(rows))` which is a non-nil
// empty slice (matches legacy `entries = []domain.AuditEntryResult{}`
// fallback). Phase 2 reshape moves this to a real-PG integration test.
//
// TestAuditStore_ListAuditEntries_NilDB (above) STILL PASSES — the
// nil-pool early return runs BEFORE the SqlcBridge dispatch, so the
// nil-DB invariant is hermetically testable.

// =============================================================================
// Build GC Store tests
// =============================================================================

// (TestBuildGCStore_DeleteOldEvents_Success removed in
//  dogfood-2026-05-08-fixes T1.1 / ADR D2: the production method was
//  deleted along with the inexistent `build_events` table reference.)

// TestBuildGCStore_DeleteOldWebhookDeliveries_Success — DELETED in
// T2.2 #6 of plan 2026-05-29-sqlc-migration v1.3.
//
// DeleteOldWebhookDeliveries was migrated to sqlc-backed via
// SqlcBridge; the previous FakeDBTX stub is incompatible with the
// bridge dispatch. KEEP-INVARIANT (sqlc emits the same `DELETE FROM
// github_webhook_deliveries WHERE received_at < $1` shape; RowsAffected
// surfaces through :execrows return) preserved structurally. Phase 2
// reshape window will exercise the behavior against real PG.
//
// NOTE: DeleteOldBuilds tests above (Success + Error) remain valid —
// that function is kept as raw DBTX.Exec (ranked-subquery shape that
// sqlc 1.31.1 cannot parse; deferred to T4.1 raw/ subpackage per
// ADR-1 D5).

// =============================================================================
// UniqueViolationError tests
// =============================================================================

func TestUniqueViolationError_ErrorMessage(t *testing.T) {
	err := &UniqueViolationError{Constraint: "builds_pkey"}
	if err.Error() != "unique violation: builds_pkey" {
		t.Errorf("expected 'unique violation: builds_pkey', got: %s", err.Error())
	}
}

// =============================================================================
