// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

import "github.com/getloopscale/loop-runtime/api/internal/db"

// membership_types.go — public struct for the user → tenant_members
// membership lookup that survives the T2.1 (Batch A #8a) deletion
// of the legacy `membership_store.go`. The 1 prod call site uses
// the struct-literal `&db.PostgresMembershipStore{DB: poolCfg}` —
// preserved.

// PostgresMembershipStore retrieves tenant memberships from
// PostgreSQL. Implements middleware.MembershipStore (DIP).
//
// T2.1 Batch A #8a (plan 2026-05-29-sqlc-migration v1.3): DB field
// type widened from QueryExecutor to db.DBTX so the sqlc dispatch
// can bridge through db.SqlcBridge. The prod call site passes a pool
// adapter that satisfies both.
type PostgresMembershipStore struct {
	DB db.DBTX
}
