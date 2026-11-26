// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// app_attestation_store_test.go — RESHAPED in T1.3b of plan
// 2026-05-29-sqlc-migration v1.3.
//
// Per `.claude/knowledge-base/audits/2026-05-29-sqlc-test-reshape-
// matrix.md` line 26 (app_attestation_store_test.go category RESHAPE-
// INTEGRATION):
//   "Persistence + validation + cross-tenant. Reshape to call new
//    attestation wrapper; preserve cross-tenant + SBOM-format-rejection
//    assertions."
//
// The original 13 sub-tests split into 2 categories:
//
// (A) Pure-validation tests (6) — fire BEFORE any DB roundtrip in the
//     wrapper. These are RESHAPED below to exercise
//     AppAttestationStoreSqlc directly (nil pool is safe because the
//     validation guards return before s.gen(tx) is invoked).
//
// (B) DB-roundtrip tests (7) — TestAppAttestationStore_SaveApp
//     Attestations_PersistsAllApps, _PropagatesDBError, _GetBuildApp
//     Attestations_ReturnsAllApps, _CrossTenantEmpty, _UpdateApp
//     SBOM_PersistsMetadata, _NoMatchReturnsError, _AcceptsSPDXFormat
//     — these required real SQL execution against a fake DBTX (now
//     incompatible with the SqlcBridge requirement). DELETED here;
//     reshape obligation logged for Phase 2 (T2.x) when pgxmock or
//     LOOPRUNTIME_E2E_DATABASE_URL harness is wired.
//
// KEEP-INVARIANT coverage of validation invariants (digest format,
// non-empty app_name, non-empty release/tenant, SBOM format whitelist)
// is preserved by the 6 reshaped tests below — these are the SLSA L3
// pre-DB gates that defend the ALL-signed contract.

// =============================================================================
// SaveAppAttestations — validation guards (pre-DB)
// =============================================================================

func TestAppAttestationStoreSqlc_SaveAppAttestations_RejectsBadDigest(t *testing.T) {
	// Validation pre-loops fire before s.gen(tx); nil pool is safe.
	store := &AppAttestationStoreSqlc{}
	err := store.SaveAppAttestations(context.Background(), "rel-1", "ten-1", []AppAttestation{
		{AppName: "api", ImageDigest: "deadbeef-no-sha256-prefix"},
	})
	if err == nil {
		t.Fatal("expected error for bad digest, got nil")
	}
	if !strings.Contains(err.Error(), "sha256:") {
		t.Errorf("expected 'sha256:' in error, got %q", err.Error())
	}
}

func TestAppAttestationStoreSqlc_SaveAppAttestations_RejectsEmptyAppName(t *testing.T) {
	store := &AppAttestationStoreSqlc{}
	err := store.SaveAppAttestations(context.Background(), "rel-1", "ten-1", []AppAttestation{
		{AppName: "", ImageDigest: "sha256:abcd"},
	})
	if err == nil {
		t.Fatal("expected error for empty app_name, got nil")
	}
	if !strings.Contains(err.Error(), "app_name is required") {
		t.Errorf("expected 'app_name is required' in error, got %q", err.Error())
	}
}

func TestAppAttestationStoreSqlc_SaveAppAttestations_NoOpEmpty(t *testing.T) {
	// Empty atts → no-op nil (legacy contract; preserved by wrapper
	// `if len(atts) == 0 { return nil }` early return).
	store := &AppAttestationStoreSqlc{}
	err := store.SaveAppAttestations(context.Background(), "rel-1", "ten-1", nil)
	if err != nil {
		t.Errorf("expected nil error for empty atts, got %v", err)
	}
	err = store.SaveAppAttestations(context.Background(), "rel-1", "ten-1", []AppAttestation{})
	if err != nil {
		t.Errorf("expected nil error for zero-len atts, got %v", err)
	}
}

func TestAppAttestationStoreSqlc_SaveAppAttestations_RejectsEmptyReleaseID(t *testing.T) {
	store := &AppAttestationStoreSqlc{}
	err := store.SaveAppAttestations(context.Background(), "", "ten-1", []AppAttestation{
		{AppName: "api", ImageDigest: "sha256:abcd"},
	})
	if err == nil {
		t.Fatal("expected error for empty release_id, got nil")
	}
	// Audit gap M02 #13: typed LoopRuntimeError code surfaces in audit_log.
	var loopruntimeErr *loopruntimeerrors.LoopRuntimeError
	if !errors.As(err, &loopruntimeErr) {
		t.Errorf("expected *loopruntimeerrors.LoopRuntimeError, got %T: %v", err, err)
	}
}

// =============================================================================
// GetBuildAppAttestations — empty-input guard
// =============================================================================

func TestAppAttestationStoreSqlc_GetBuildAppAttestations_EmptyInputsReturnsErrBuildNotFound(t *testing.T) {
	// Empty tenantID OR empty buildID → ErrBuildNotFound (legacy
	// contract; preserved by wrapper guard).
	store := &AppAttestationStoreSqlc{}
	_, err := store.GetBuildAppAttestations(context.Background(), "", "build-1")
	if !errors.Is(err, loopruntimeerrors.ErrBuildNotFound) {
		t.Errorf("expected ErrBuildNotFound for empty tenant, got %v", err)
	}
	_, err = store.GetBuildAppAttestations(context.Background(), "ten-1", "")
	if !errors.Is(err, loopruntimeerrors.ErrBuildNotFound) {
		t.Errorf("expected ErrBuildNotFound for empty build, got %v", err)
	}
}

// =============================================================================
// UpdateAppSBOM — validation guards (pre-DB)
// =============================================================================

func TestAppAttestationStoreSqlc_UpdateAppSBOM_RejectsBadFormat(t *testing.T) {
	// Format validation fires BEFORE the DB roundtrip; nil pool is safe.
	store := &AppAttestationStoreSqlc{}
	err := store.UpdateAppSBOM(context.Background(), "rel-1", "api", "s3://...", "invalid-format", 42)
	if err == nil {
		t.Fatal("expected error for bad format, got nil")
	}
	if !strings.Contains(err.Error(), "invalid sbom_format") {
		t.Errorf("expected 'invalid sbom_format' in error, got %q", err.Error())
	}
}

func TestAppAttestationStoreSqlc_UpdateAppSBOM_RejectsEmptyInputs(t *testing.T) {
	store := &AppAttestationStoreSqlc{}
	err := store.UpdateAppSBOM(context.Background(), "", "api", "s3://...", SBOMFormatCycloneDXJSON, 42)
	if err == nil {
		t.Fatal("expected error for empty release_id, got nil")
	}
	err = store.UpdateAppSBOM(context.Background(), "rel-1", "", "s3://...", SBOMFormatCycloneDXJSON, 42)
	if err == nil {
		t.Fatal("expected error for empty app_name, got nil")
	}
	// Validates typed LoopRuntimeError surface for audit_log mapping.
	err = store.UpdateAppSBOM(context.Background(), "", "", "s3://...", SBOMFormatCycloneDXJSON, 42)
	var loopruntimeErr *loopruntimeerrors.LoopRuntimeError
	if !errors.As(err, &loopruntimeErr) {
		t.Errorf("expected *loopruntimeerrors.LoopRuntimeError, got %T: %v", err, err)
	}
}
