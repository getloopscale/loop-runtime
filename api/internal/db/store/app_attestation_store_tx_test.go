// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

import (
	"context"
	"strings"
	"testing"
)

// KEEP-INVARIANT per T0.6 reshape matrix line 27 (
// app_attestation_store_tx_test.go category KEEP-INVARIANT):
// "Tx-threading discipline (`SaveAppAttestationsTx_RequiresNonNilTx`).
// Maps directly to D3 + sqlctxprop gate invariant."
//
// Reshaped in T1.3b of plan 2026-05-29-sqlc-migration v1.3 from
// PostgresAppAttestationStore to AppAttestationStoreSqlc. The nil-tx
// guard is testable WITHOUT a real PG connection (the guard fires
// before any DBTX dispatch), so the test runs hermetically against the
// sqlc-backed wrapper.

// TestSaveAppAttestationsTx_RequiresNonNilTx asserts that calling with
// a nil tx returns a clear error (defensive guard against silent
// fallback to the pool). Plan T2.4 RED #1; T1.3b reshape.
func TestSaveAppAttestationsTx_RequiresNonNilTx(t *testing.T) {
	// pool is nil because the nil-tx guard fires BEFORE the wrapper
	// dispatches to s.gen(tx) — so we don't need a SqlcBridge-capable
	// pool to exercise this invariant.
	store := &AppAttestationStoreSqlc{}
	err := store.SaveAppAttestationsTx(context.Background(), nil, "rel-1", "ten-1", []AppAttestation{
		{AppName: "api", ImageDigest: "sha256:abcd"},
	})
	if err == nil {
		t.Fatalf("expected error for nil tx, got nil")
	}
	if !strings.Contains(err.Error(), "non-nil tx") {
		t.Errorf("expected 'non-nil tx' in error, got %q", err.Error())
	}
}

// TestSaveAppAttestationsTx_ExposesPublicAPI is a compile-time guard:
// asserts the SaveAppAttestationsTx method is part of the
// AppAttestationStore interface. If someone removes the method or
// changes the signature, this test fails to compile. Plan T2.4 RED #2;
// T1.3b reshape — the compile-time assertion in
// app_attestation_wrapper.go (`var _ AppAttestationStore =
// (*AppAttestationStoreSqlc)(nil)`) makes this test technically
// redundant but we keep it for symmetry with the legacy guard and to
// catch interface-shape drift early in any future refactor.
func TestSaveAppAttestationsTx_ExposesPublicAPI(t *testing.T) {
	var _ AppAttestationStore = &AppAttestationStoreSqlc{}
	// Reference the method directly to surface signature drift.
	store := &AppAttestationStoreSqlc{}
	_ = store.SaveAppAttestationsTx
}
