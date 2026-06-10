// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

// project_types.go — public struct for the projects domain that
// survives the T2.1 (Batch A #4) deletion of the legacy
// `project_store.go`. The 5 prod call sites use the struct-literal
// `&db.PostgresProjectStore{DB: dbAdapter}` shape — preserved here.

// PostgresProjectStore creates projects in PostgreSQL. Implements
// domain.ProjectStore (DIP).
//
// T2.1 Batch A #4 (plan 2026-05-29-sqlc-migration v1.3): DB field
// type preserved (db.DBTX); methods (project_wrapper.go) dispatch
// through SqlcBridge to sqlc-generated queries.
type PostgresProjectStore struct {
	DB DBTX
}
