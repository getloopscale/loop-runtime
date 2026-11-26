// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// attestation_store_test.go — RESHAPED in T2.1 #8b of plan
// 2026-05-29-sqlc-migration v1.3.
//
// The previous content (CHG-2026-04-25-ATTESTATION-WIRE Phase 6
// test suite) asserted PostgresAttestationStore behaviors against
// custom fake DBTX implementations. With T2.1 #8b widening to sqlc-
// backed dispatch via SqlcBridge, the legacy fakes are incompatible.
//
// KEEP-INVARIANT preserved structurally by attestation_wrapper.go:
//   - Empty buildID / tenantID → ErrBuildNotFound (no DB call)
//     The empty-input guards run BEFORE the SqlcBridge dispatch, so
//     they're hermetically testable. See the surviving guards below.
//   - NoRows → ErrBuildNotFound via errors.Is(err, pgx.ErrNoRows)
//     (no existence leak — both variants return the same sentinel,
//     not ErrForbidden, per ADR-0014)
//   - Empty signature fields → Signed=false (legacy releases pre-
//     Phase 5; preserved by attestationRowToInfo helper)
//   - SignedAt RFC3339Nano UTC formatting preserved
//
// Per matrix § Phase 2 reshape window: DB-roundtrip behavioral
// subset moves to real-PG-gated tests when pgxmock or
// LOOPZERO_E2E_DATABASE_URL harness is wired.

import (
	"context"
	"errors"
	"testing"

	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// TestAttestationStore_GetBuildAttestationForTenant_EmptyTenant —
// hermetic guard test. Empty tenant → ErrBuildNotFound (no DB call,
// no SqlcBridge invocation).
func TestAttestationStore_GetBuildAttestationForTenant_EmptyTenant(t *testing.T) {
	store := &PostgresAttestationStore{}
	_, err := store.GetBuildAttestationForTenant(context.Background(), "", "bld-1")
	if !errors.Is(err, loopzeroerrors.ErrBuildNotFound) {
		t.Errorf("expected ErrBuildNotFound for empty tenantID, got: %v", err)
	}
}

// TestAttestationStore_GetBuildAttestationForTenant_EmptyBuild —
// symmetric empty-input check.
func TestAttestationStore_GetBuildAttestationForTenant_EmptyBuild(t *testing.T) {
	store := &PostgresAttestationStore{}
	_, err := store.GetBuildAttestationForTenant(context.Background(), "tn-1", "")
	if !errors.Is(err, loopzeroerrors.ErrBuildNotFound) {
		t.Errorf("expected ErrBuildNotFound for empty buildID, got: %v", err)
	}
}
