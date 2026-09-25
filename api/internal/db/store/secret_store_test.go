// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store_test

// =============================================================================
// PostgresSecretStore tests — DELETED in T2.2 #4 of plan
// 2026-05-29-sqlc-migration v1.3
// =============================================================================
//
// 5 stub-class subtests previously here under
// TestPostgresSecretStore_ListNames (returns_secrets_matching_,
// returns_empty_slice_when_no_secrets_found, returns_error_when_query_fails,
// returns_error_when_row_scan_fails, returns_error_when_rows_iteration_
// fails) used fakeQueryExecutorForSecrets / fakeRowsImpl stubs to verify
// PostgresSecretStore.ListNames behaviors. With T2.2 #4 migrating to
// sqlc-backed via SqlcBridge, the QueryExecutor stub interface is no
// longer the dispatch path — the wrapper returns a typed error when
// the DBTX doesn't satisfy SqlcBridge.
//
// Classification per `.claude/knowledge-base/audits/2026-05-29-sqlc-
// test-reshape-matrix.md`:
//   - KEEP-INVARIANT preserved structurally by SecretStoreSqlc:
//     * Tenant + project + environment scoped ListNames (sqlc query
//       takes (tenant_id, project_id, environment) composite scope,
//       ORDER BY name)
//     * SEC-001 defense-in-depth: ListSecretNames query NEVER selects
//       the encrypted_value column
//     * Empty result → non-nil empty slice (wrapper preallocates with
//       `make([]domain.SecretMetadata, 0, len(rows))`)
//     * Query errors wrap with "secret_store: list names:" prefix
//
// Per matrix § Phase 2 reshape window: behavioral subset moves to
// real-PG-gated tests when pgxmock or LOOPZERO_E2E_DATABASE_URL harness
// is wired.

// The compile-time assertion that PostgresSecretStore satisfies the secret
// port lives with the port, in api/internal/secrets: the persistence kernel
// does not import the feature that declares the contract.
