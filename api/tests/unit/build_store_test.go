// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

// build_store_test.go — DELETED in T1.2b of plan
// docs/plans/2026-05-29-sqlc-migration-plan.md v1.3.
//
// The previous content (560 LOC, 15 sub-tests across 6 top-level
// functions — TestBuildStore_Create, _GetByID, _HasActiveBuild,
// _UpdateStatus, _SetErrorMessage, _ListByProject) asserted
// PostgresBuildStore behaviors against `fakeBuildDB` — a custom Go fake
// that implemented db.DBTX in-memory and simulated SQL Scan
// semantics across 14 hand-rolled column positions. With T1.2b deleting
// PostgresBuildStore and replacing it with BuildStoreSqlc (sqlc-backed
// via SqlcBridge), the fakeBuildDB pattern is fundamentally
// incompatible: the wrapper bridges db.DBTX → generated.DBTX, and
// generated.DBTX is pgx-native (pgconn.CommandTag, pgx.Rows, pgx.Row)
// — incompatible with the legacy Scan-into-positional-args fake.
//
// Classification per `.claude/knowledge-base/audits/2026-05-29-sqlc-
// test-reshape-matrix.md`:
//
//   - Sub-tests testing CREATE_QUEUED_STATUS, ID_GENERATION,
//     SCAN_COLUMN_LAYOUT are DELETE-SCHEMA-MIRROR-DUPLICATE: sqlc
//     compile-time guarantee + sqlcsync gate (T0.3) replace them.
//
//   - Sub-tests testing VALID/INVALID STATE TRANSITION,
//     ErrBuildNotFound mapping, UniqueViolation → ErrDeployInProgress,
//     HasActiveBuild true/false are KEEP-INVARIANT. ALL are preserved
//     by BuildStoreSqlc:
//       * state-machine SafeTransition at store layer (D4 invariant) +
//         3-way UPDATE dispatch (ToRunning/ToTerminal/Other) based on
//         resulting BuildPhase
//       * pgx.ErrNoRows → loopzeroerrors.ErrBuildNotFound explicit in
//         build_wrapper.go GetByID / GetByIDForTenant / getBuildByIDViaTx
//       * pgconn.PgError 23505 → loopzeroerrors.ErrDeployInProgress via
//         isPGUniqueViolation helper (dual path: raw pgconn + legacy
//         UniqueViolationError for defense-in-depth)
//       * HasActiveBuild concurrent-deploy invariant (Rule 6 + ARCH-002)
//         exercised by integration suite under
//         api/tests/integration/concurrent_builds_test.go (T1.2b
//         reshape to NewBuildStoreSqlc)
//
//   - Per matrix § "Per-phase action plan" Phase 2 (T2.1-T2.4 batches):
//     additional KEEP-INVARIANT subset reshape for build store belongs
//     to Phase 2 when a real-PG-gated unit test harness is wired
//     (pgxmock or LOOPZERO_E2E_DATABASE_URL).
//
// No new tests are added in this file; the file is kept as a tombstone
// so package `unit` does not lose its build_store_test.go path
// reference and so the rationale is discoverable via `git grep -F
// "build_store_test"`.
