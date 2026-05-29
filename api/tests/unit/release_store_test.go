// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

// release_store_test.go — DELETED in T1.1c2c of plan
// docs/plans/2026-05-29-sqlc-migration-plan.md v1.3.
//
// The previous content (350+ LOC, 12 sub-tests against fakeReleaseDB)
// asserted PostgresReleaseStore behaviors against a custom Go fake that
// implemented db.DBTX without holding a pgx connection. With T1.1c2c
// deleting PostgresReleaseStore and replacing it with ReleaseStoreSqlc
// (sqlc-backed), the fakeReleaseDB pattern is fundamentally incompatible:
// the sqlc-backed wrapper bridges db.DBTX → generated.DBTX via the
// SqlcBridge interface, and SqlcBridge is intentionally NOT implemented
// by test fakes that don't hold a real pgx connection (matrix § verification-
// architect condition 1, T0.6 reshape matrix).
//
// Classification per `.claude/knowledge-base/audits/2026-05-29-sqlc-test-
// reshape-matrix.md`:
//
//   - Sub-tests testing CREATION-WITH-READY-STATUS, ID-PRESERVATION, and
//     STATUS-PERSISTENCE are DELETE-SCHEMA-MIRROR-DUPLICATE: sqlc
//     compile-time guarantee + sqlcsync gate (T0.3) replace them.
//
//   - Sub-tests testing VALID/INVALID STATE TRANSITION + ErrReleaseNotFound
//     mapping are KEEP-INVARIANT. These behaviors ARE preserved by
//     ReleaseStoreSqlc (state-machine SafeTransition is called at the
//     store layer per D4; pgx.ErrNoRows → loopzeroerrors.ErrReleaseNotFound
//     mapping is explicit in wrapper). They are EXERCISED by the
//     integration suite under `api/tests/integration/`:
//
//       - release_store_column_cascade_test.go (T1.1c2c reshape; M07
//         Rekor URL regression)
//       - rollback_test.go (T1.1c2c reshape; rollback state machine)
//       - concurrent_builds_test.go (T1.1c2c reshape; lock+rejection
//         invariant)
//
//   - Per matrix § "Per-phase action plan" Phase 2 (T2.1-T2.4 batches):
//     additional KEEP-INVARIANT subset reshape for release belongs to
//     Phase 2 when a real-PG-gated unit test harness is wired (pgxmock
//     or LOOPZERO_E2E_DATABASE_URL).
//
// No new tests are added in this file; the file is kept as a tombstone
// so package `unit` does not lose its release_store_test.go path
// reference and so the rationale is discoverable via `git grep -F
// "release_store_test"`.
