// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db"
)

// TestPgxTxDBTX_ImplementsSqlcBridge — regression test for the LIVE
// dogfood panic surfaced 2026-05-29 cleanroom retry:
//
//   "build_wrapper: DBTX *adapters.PgxTxDBTX does not implement
//    SqlcBridge — sqlc-backed stores require PgxAdapter /
//    PgxTxAdapter or pgxmock per T0.6 reshape matrix"
//
// Pre-fix-949aae1c, the lock-side adapter satisfied db.DBTX but
// NOT db.SqlcBridge. The sqlc-backed wrappers' bridge() helper
// does a strict type-assertion `bridge, ok := tx.(SqlcBridge)`;
// when the assertion failed, the wrapper panicked with the typed
// error.
//
// This test ensures:
//   1. PgxTxDBTX implements db.DBTX (the surface the wrapper field
//      expects)
//   2. PgxTxDBTX implements db.SqlcBridge (the bridge contract for
//      dispatching into sqlc-backed wrappers inside the
//      withEnvironmentLock-held tx)
//
// If a future contributor adds a new pgx.Tx wrapper, they should
// add the equivalent assertions here OR in the new wrapper's _test.go.
func TestPgxTxDBTX_ImplementsSqlcBridge(t *testing.T) {
	var _ db.DBTX = (*PgxTxDBTX)(nil)       // compile-time
	var _ db.SqlcBridge = (*PgxTxDBTX)(nil) // compile-time

	// Runtime check that the assertions actually hold at type-assertion
	// time (this is what the bridge() helper in sqlc-backed wrappers
	// does — see api/internal/db/build_wrapper.go gen() helper).
	var dbtx db.DBTX = &PgxTxDBTX{}
	if _, ok := dbtx.(db.SqlcBridge); !ok {
		t.Fatal("PgxTxDBTX must satisfy db.SqlcBridge at runtime type-assertion (the wrapper bridge() helper depends on this)")
	}
}
