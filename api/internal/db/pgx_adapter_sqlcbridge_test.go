// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package db

import "testing"

// TestPgxAdapter_ImplementsSqlcBridge — mirror of the regression
// test in api/internal/adapters/pgx_adapter_sqlcbridge_test.go for
// the placement-side adapters.
//
// PgxAdapter (pool-backed) and PgxTxAdapter (placement
// SERIALIZABLE-via-TxFromContext per MEET-053 D-01) BOTH must
// satisfy db.DBTX + db.SqlcBridge. The bridge() helpers in the
// sqlc-backed wrappers dispatch via type-assertion; missing
// implementation = panic in the wrapper.
//
// Pre-this-test, the assertions held by accident — there was no
// regression guard. A future contributor refactoring either adapter
// could silently break the bridge contract; this test surfaces it
// at the package test gate.
//
// Companion to api/internal/adapters/pgx_adapter_sqlcbridge_test.go
// which covers the lock-side PgxTxDBTX adapter.
func TestPgxAdapter_ImplementsSqlcBridge(t *testing.T) {
	var _ DBTX = (*PgxAdapter)(nil)         // compile-time
	var _ SqlcBridge = (*PgxAdapter)(nil)   // compile-time
	var _ DBTX = (*PgxTxAdapter)(nil)       // compile-time
	var _ SqlcBridge = (*PgxTxAdapter)(nil) // compile-time

	// Runtime check — exactly what bridge() in build_wrapper.go does.
	var pa DBTX = &PgxAdapter{}
	if _, ok := pa.(SqlcBridge); !ok {
		t.Fatal("PgxAdapter must satisfy SqlcBridge at runtime type-assertion")
	}
	var pta DBTX = &PgxTxAdapter{}
	if _, ok := pta.(SqlcBridge); !ok {
		t.Fatal("PgxTxAdapter must satisfy SqlcBridge at runtime type-assertion")
	}
}
