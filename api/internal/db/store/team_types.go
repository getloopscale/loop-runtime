// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import "github.com/getloopscale/loop-runtime/api/internal/db"

// team_types.go — public struct for the team membership domain that
// survives the T2.1 (Batch A #5) deletion of the legacy
// `team_store.go`. The 1 prod call site
// (api/cmd/setup_server.go:215) uses the struct-literal
// `&db.PostgresTeamStore{DB: dbAdapter}` shape — preserved here.

// PostgresTeamStore handles team member CRUD operations. Implements
// domain.TeamStore (DIP).
//
// T2.1 Batch A #5 (plan 2026-05-29-sqlc-migration v1.3): DB field
// type preserved (db.DBTX); methods (team_wrapper.go) dispatch
// through SqlcBridge to sqlc-generated queries.
type PostgresTeamStore struct {
	DB db.DBTX
}
