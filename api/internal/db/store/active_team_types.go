// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package store holds the sqlc-backed status-mutation Store adapters (the only sanctioned build/release status path — MA-7). M9 Step 5.
package store

import "github.com/getloopscale/loop-runtime/api/internal/db"

// active_team_types.go — public types for explicit user-driven
// active-team selection (migration 000018, users.active_tenant_id
// column) that survive the T2.1 (Batch A) deletion of the legacy
// hand-rolled `active_team_store.go`. The 1 prod call site
// (api/cmd/setup_server.go:220) uses the constructor — preserved
// verbatim below.

// PostgresActiveTeamStore persists explicit user-driven active-team
// selection in the users.active_tenant_id column. Plan T2.5 D8.
//
// Semantics:
//   - GetActiveTenant: returns ("", nil) when active_tenant_id is
//     NULL (user never set explicit selection — fall back to JWT
//     middleware heuristic).
//   - SetActiveTenant: UPDATE users SET active_tenant_id = $1 WHERE
//     id = $2. FK enforces tenant exists; ON DELETE SET NULL handles
//     deletion.
//   - ClearActiveTenant: UPDATE users SET active_tenant_id = NULL
//     (EC-4 self-heal when /me detects the user is no longer a
//     member).
//
// T2.1 Batch A (plan 2026-05-29-sqlc-migration v1.3): DB field type
// preserved (db.DBTX — same as legacy); methods (active_team_wrapper.go)
// dispatch through SqlcBridge to sqlc-generated queries.
type PostgresActiveTeamStore struct {
	DB db.DBTX
}

// NewPostgresActiveTeamStore constructs a PostgresActiveTeamStore.
// Preserved verbatim from the legacy file so the 1 prod call site
// (api/cmd/setup_server.go:220) compiles unchanged.
func NewPostgresActiveTeamStore(db db.DBTX) *PostgresActiveTeamStore {
	return &PostgresActiveTeamStore{DB: db}
}
