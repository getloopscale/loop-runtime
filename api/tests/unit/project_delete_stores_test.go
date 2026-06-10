// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

// project_delete_stores_test.go — DELETED in T2.1 #6 of plan
// 2026-05-29-sqlc-migration v1.3.
//
// This file previously held two domain test suites against legacy
// fakes:
//
// (A) ProjectStore.GetByName / DeleteProject (6 sub-tests against
//     fakeDBTXForProject). DELETED in T2.1 #4; see commit cb670468
//     for the classification + Phase 2 reshape obligation.
//
// (B) EnvironmentTargetStore.ListByProject / DeleteTarget (4 sub-
//     tests against fakeQueryForTarget + fakeExecForTarget). DELETED
//     in T2.1 #6 along with the underlying
//     PostgresEnvironmentTargetStore migration to sqlc-backed
//     dispatch via SqlcBridge.
//
// (C) BuildStore.CancelActiveBuilds (3 sub-tests +
//     fakeBuildDBForCancel/fakeCancelRows). DELETED in T1.2b; see
//     commit 86d0fab8.
//
// Per matrix § Phase 2 reshape window: behavioral subset moves to
// real-PG-gated tests when pgxmock or LOOPZERO_E2E_DATABASE_URL harness
// is wired.
//
// No new tests are added in this file; the file is kept as a
// tombstone so the test path stays discoverable via `git grep
// "project_delete_stores_test"`.
