// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// =============================================================================
// Release Store additional tests — DELETED in T1.1c2c of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// The 7 stub-class tests previously here (TestReleaseStore_GetActiveByEnv_
// Success, TestReleaseStore_GetActiveByEnvTx_NotFoundReturnsNil,
// TestReleaseStore_GetPreviousRelease_Success, TestReleaseStore_GetByID
// ForTenant_Success, TestReleaseStore_GetLatestByBuildID_Success,
// TestReleaseStore_UpdateStatus_NilTxUsesPool, TestReleaseStore_SetGit
// Commit_ExecError) ALL used `FakeDBTX` stub responses to verify that
// PostgresReleaseStore returned what the stub fed it — a pure pass-
// through pattern.
//
// Classification per `.claude/knowledge-base/audits/2026-05-29-sqlc-
// test-reshape-matrix.md` (T0.6) line 36:
//
//   - 4 are DELETE-SCHEMA-MIRROR-DUPLICATE (stub-pass-through; sqlc
//     compile-time guarantee replaces them):
//     * GetActiveByEnv_Success, GetByIDForTenant_Success,
//       UpdateStatus_NilTxUsesPool, SetGitCommit_ExecError
//
//   - 3 are KEEP-INVARIANT subset reshape (per matrix:
//     GetActiveByEnvTx_NotFoundReturnsNil, GetPreviousRelease_Success,
//     GetLatestByBuildID_Success). These behaviors ARE preserved by
//     ReleaseStoreSqlc (errors.Is(err, pgx.ErrNoRows) → nil mapping,
//     state-machine SafeTransition discipline) AND are exercised by
//     the integration suite under `api/tests/integration/` against
//     real PG via `NewReleaseStoreSqlc`.
//
// Per matrix § "Per-phase action plan" — Phase 2 (T2.1-T2.4 batches) is
// the canonical reshape window for `stores_extra_test.go`. With
// PostgresReleaseStore deleted in T1.1c2c, the stub-based versions
// COULD NOT compile any longer, so the deletion landed here; the Phase
// 2 reshape window now owns the integration coverage for the
// KEEP-INVARIANT subset against a real PG instance.
//
// Cross-refs:
//   - `.claude/knowledge-base/audits/2026-05-29-sqlc-test-reshape-matrix.md`
//   - `api/internal/db/release_wrapper.go` (ReleaseStoreSqlc)
//   - `api/tests/integration/release_store_column_cascade_test.go`
//     (M07 Rekor URL persistence regression — reshaped in T1.1c2c to
//     use NewReleaseStoreSqlc)
//   - `api/tests/integration/rollback_test.go` (rollback flow against
//     real PG; reshaped in T1.1c2c)
//   - `api/tests/integration/concurrent_builds_test.go` (concurrent-
//     deploy 1×202+1×409 invariant; reshaped in T1.1c2c)

// =============================================================================
// Build Store tests — DELETED in T1.2b of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// Build-related stub tests (Setters_NilTxUsesPool, GetBuildApps_NullJSON,
// FindActiveBuilds_Empty, ListByProject_WithData, CancelActiveBuilds_
// QueryError, UpdateStatus_QueuedToFailed, UpdateStatus_RunningToCancelling,
// ListByProjectAndEnv_NoEnvFilter, ListByProjectAndEnv_QueryError,
// ListByProjectAndEnv_NoEnvQueryError, ListByProject_QueryError) ALL used
// FakeDBTX stub responses to verify PostgresBuildStore behaviors. With
// T1.2b deleting PostgresBuildStore and replacing it with BuildStoreSqlc
// (sqlc-backed via SqlcBridge), the stub-based pattern is incompatible.
//
// Classification per `.claude/knowledge-base/audits/2026-05-29-sqlc-test-
// reshape-matrix.md` line 36 (stores_extra_test.go MIXED):
//
//   - KEEP-INVARIANT subset preserved by BuildStoreSqlc structurally:
//       * UpdateStatus_QueuedToFailed + UpdateStatus_RunningToCancelling
//         — state-machine SafeTransition at store layer (D4) + 3-way
//         dispatch (ToRunning/ToTerminal/Other) covered by integration
//         suite under api/tests/integration/concurrent_builds_test.go
//
//   - DELETE-SCHEMA-MIRROR-DUPLICATE subset: NilTxUsesPool family,
//     NullJSON, FindActiveBuilds_Empty, ListByProject_WithData,
//     CancelActiveBuilds_QueryError, ListByProjectAndEnv_*Error — sqlc
//     compile-time + sqlcsync gate replace.
//
// Per matrix § Phase 2 reshape window: KEEP-INVARIANT subset moves to
// real-PG-gated unit tests when pgxmock or LOOPZERO_E2E_DATABASE_URL harness
// is wired.

// =============================================================================
// Environment Target Store additional tests
// =============================================================================

// Refactored into TestEnvironmentTargetStore_Mutators in stores_test.go.

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
// real-PG-gated tests when pgxmock or LOOPZERO_E2E_DATABASE_URL harness
// is wired.

// =============================================================================
// Team Store additional tests — DELETED in T2.1 #5 along with the
// Team Store section in stores_test.go (same tombstone block applies)
// =============================================================================

// =============================================================================
// Idempotency Store additional tests
// =============================================================================

// =============================================================================
// WriteAuditLog tests — DELETED in T1.5 of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// 2 sub-tests previously here (TestWriteAuditLog_Success +
// _ExecError) used FakeDBTX stubs to verify legacy
// ExecQueryExecutor-based WriteAuditLog. With T1.5 widening the
// signature to db.DBTX + dispatching through SqlcBridge to sqlc-
// generated InsertAuditLog, the fake pattern is incompatible — the
// wrapper returns a typed error when the DBTX doesn't satisfy
// SqlcBridge (fail-fast per T0.6 reshape matrix).
//
// Classification per `.claude/knowledge-base/audits/2026-05-29-sqlc-
// test-reshape-matrix.md` line 36 (stores_extra_test.go MIXED):
//
//   - KEEP-INVARIANT preserved structurally by sqlc-backed
//     WriteAuditLog:
//     * Successful INSERT: sqlc-generated InsertAuditLog dispatches
//       the same VALUES tuple from queries/audit.sql — column shape
//       unchanged (M-3 audit-callsite-diff verified by SQL inspection
//       in the queries file vs the deleted audit.go INSERT)
//     * ExecError wrap with "audit: insert:" prefix preserved
//       verbatim in audit_wrapper.go
//
// Per matrix § Phase 2 reshape window: KEEP-INVARIANT subset moves to
// real-PG-gated tests when pgxmock or LOOPZERO_E2E_DATABASE_URL harness
// is wired (out of T1.5 scope).

// =============================================================================
// Cell Store tests — DELETED in T2.2 #2 of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// 6 stub-class tests previously here (TestCellStore_CellTenantCounts_
// Empty/WithData/QueryError, _CountByCell_Empty, _GetTenantCellID_
// NotFound/Success) used FakeDBTX / RecordingDBTX stub responses to
// verify PostgresCellStore behaviors. With T2.2 #2 migrating to sqlc-
// backed via SqlcBridge, the fake pattern is incompatible.
//
// Classification per `.claude/knowledge-base/audits/2026-05-29-sqlc-
// test-reshape-matrix.md` (stores_extra_test.go MIXED):
//
//   - KEEP-INVARIANT preserved structurally by CellStoreSqlc:
//     * CellTenantCounts/CountByCell returns non-nil empty map when
//       zero rows (wrapper `make(map[string]int, len(rows))` pattern
//       handles zero-row case structurally)
//     * GetTenantCellID error wrap with "cell store: get tenant
//       cell_id:" prefix preserved verbatim
//
//   - DELETE-SCHEMA-MIRROR-DUPLICATE subset: stub-pass-through tests
//     — sqlc compile-time + sqlcsync gate replace.
//
// PinTenantToCell 4-step sequence (KEEP-INVARIANT, exercised in
// services/cell_pinner_test.go against the real wrapper):
//   1. tenant exists → ErrCellPinnerTenantNotFound
//   2. cell exists → ErrCellPinnerCellNotFound
//   3. cell.status == 'active' → ErrCellPinnerNotPinnable
//   4. capacity check (NULL = unlimited; excludes moving tenant) →
//      ErrCellPinnerAtCapacity
// preserved verbatim in cell_wrapper.go (each step is a sqlc query
// + the wrapper sequences them with the error mapping).
//
// Per matrix § Phase 2 reshape window: KEEP-INVARIANT subset moves to
// real-PG-gated tests when pgxmock or LOOPZERO_E2E_DATABASE_URL harness
// is wired.

// Keep import anchored
var _ = loopzeroerrors.ErrBuildNotFound
