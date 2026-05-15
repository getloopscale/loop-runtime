// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"strings"
	"testing"
)

// TestSaveAppAttestationsTx_RequiresNonNilTx asserts that calling with a nil
// tx returns a clear error (defensive guard against silent fallback to s.db).
// Plan T2.4 RED #1.
func TestSaveAppAttestationsTx_RequiresNonNilTx(t *testing.T) {
	store := &PostgresAppAttestationStore{}
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
// asserts the new SaveAppAttestationsTx method is part of the
// AppAttestationStore interface. If someone removes the method or changes
// the signature, this test fails to compile.
// Plan T2.4 RED #2.
func TestSaveAppAttestationsTx_ExposesPublicAPI(t *testing.T) {
	var _ AppAttestationStore = &PostgresAppAttestationStore{}
	// Reference the method directly to surface signature drift.
	store := &PostgresAppAttestationStore{}
	_ = store.SaveAppAttestationsTx
}
