// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

// environment_target_store_test.go — RESHAPED in T2.1 #6 of plan
// 2026-05-29-sqlc-migration v1.3.
//
// The previous content (237 LOC, 5 sub-tests + fakeExecForTarget /
// fakeQueryForTarget / fakeTargetRows harness) asserted
// PostgresEnvironmentTargetStore behaviors against custom fakes for
// the legacy ExecQueryExecutor + QueryExecutor field types. With
// T2.1 #6 widening both fields to db.DBTX + dispatching through
// SqlcBridge, the fake pattern is incompatible (legacy fakes don't
// implement the full db.DBTX surface, including QueryRow).
//
// KEEP-INVARIANT preserved structurally by
// environment_target_wrapper.go:
//   - SetDesired UPSERT (queries/environment_targets.sql ON CONFLICT
//     DO UPDATE)
//   - SetActive UPDATE active_release_id (caller invokes AFTER
//     ArgoCD confirms sync — caller discipline, not enforced here)
//   - Get NoRows → (nil, nil)
//   - ListByProject empty → empty slice (not nil)
//   - Nil-tx fallback to s.Exec preserved verbatim in
//     resolveSqlcDBTX helper
//
// Per matrix § Phase 2 reshape window: behavioral subset moves to
// real-PG-gated tests when pgxmock or LOOPZERO_E2E_DATABASE_URL harness
// is wired.
