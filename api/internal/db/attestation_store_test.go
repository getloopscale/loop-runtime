// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"errors"
	"testing"
	"time"

	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// TestAttestationStore_GetBuildAttestation tests CHG-2026-04-25-ATTESTATION-WIRE Phase 6.
// Uses the test fakeDBTX harness to avoid PG dependency.

// fakeAttestationDBTX is a minimal DBTX that returns a preset row
// or no-rows error.
type fakeAttestationDBTX struct {
	queryRow func(ctx context.Context, sql string, args ...any) Row
}

func (f *fakeAttestationDBTX) Exec(_ context.Context, _ string, _ ...any) (ExecResult, error) {
	return ExecResult{}, nil
}
func (f *fakeAttestationDBTX) Query(_ context.Context, _ string, _ ...any) (Rows, error) {
	return nil, nil
}
func (f *fakeAttestationDBTX) QueryRow(ctx context.Context, sql string, args ...any) Row {
	if f.queryRow != nil {
		return f.queryRow(ctx, sql, args...)
	}
	return emptyRow{}
}

// AC-6.1: a release with all 3 columns populated returns Signed=true
// and full AttestationInfo.
func TestAttestationStore_SignedRelease_ReturnsFullInfo(t *testing.T) {
	signedAt := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	digest := "sha256:abc"
	sig := "registry/api:v1@sha256:abc"

	dbtx := &fakeAttestationDBTX{
		queryRow: func(_ context.Context, _ string, _ ...any) Row {
			return &fakeRowWithData{values: []any{&digest, &sig, &signedAt}}
		},
	}

	store := NewPostgresAttestationStore(dbtx)
	info, err := store.GetBuildAttestation(context.Background(), "build-1")
	if err != nil {
		t.Fatalf("GetBuildAttestation: %v", err)
	}
	if !info.Signed {
		t.Error("expected Signed=true")
	}
	if info.ImageDigest != digest {
		t.Errorf("ImageDigest: got %q, want %q", info.ImageDigest, digest)
	}
	if info.ImageSignature != sig {
		t.Errorf("ImageSignature: got %q", info.ImageSignature)
	}
	if info.SignedAt == "" {
		t.Error("SignedAt should be populated")
	}
	if info.BuildID != "build-1" {
		t.Errorf("BuildID: got %q", info.BuildID)
	}
}

// AC-6.2: a release with NULL signature columns (legacy / pre-CHG)
// returns Signed=false with empty fields.
func TestAttestationStore_LegacyRelease_ReturnsSignedFalse(t *testing.T) {
	dbtx := &fakeAttestationDBTX{
		queryRow: func(_ context.Context, _ string, _ ...any) Row {
			// All three pointers are nil — Scan() leaves the dest nil pointers
			return &fakeRowWithData{values: []any{
				(*string)(nil),
				(*string)(nil),
				(*time.Time)(nil),
			}}
		},
	}

	store := NewPostgresAttestationStore(dbtx)
	info, err := store.GetBuildAttestation(context.Background(), "build-legacy")
	if err != nil {
		t.Fatalf("GetBuildAttestation: %v", err)
	}
	if info.Signed {
		t.Error("expected Signed=false for legacy release with NULL columns")
	}
	if info.ImageDigest != "" {
		t.Errorf("ImageDigest should be empty: got %q", info.ImageDigest)
	}
	if info.ImageSignature != "" {
		t.Errorf("ImageSignature should be empty: got %q", info.ImageSignature)
	}
	if info.SignedAt != "" {
		t.Errorf("SignedAt should be empty: got %q", info.SignedAt)
	}
}

// AC-6.3: an unknown build returns ErrBuildNotFound (mapped to 404 by handler).
func TestAttestationStore_UnknownBuild_ReturnsErrBuildNotFound(t *testing.T) {
	dbtx := &fakeAttestationDBTX{
		queryRow: func(_ context.Context, _ string, _ ...any) Row {
			return emptyRow{}
		},
	}

	store := NewPostgresAttestationStore(dbtx)
	_, err := store.GetBuildAttestation(context.Background(), "build-nonexistent")
	if !errors.Is(err, loopzeroerrors.ErrBuildNotFound) {
		t.Errorf("expected ErrBuildNotFound, got %v", err)
	}
}

// Defensive: empty buildID returns ErrBuildNotFound (no SQL injection
// surface, no panic).
func TestAttestationStore_EmptyBuildID_ReturnsErrBuildNotFound(t *testing.T) {
	store := NewPostgresAttestationStore(&fakeAttestationDBTX{})
	_, err := store.GetBuildAttestation(context.Background(), "")
	if !errors.Is(err, loopzeroerrors.ErrBuildNotFound) {
		t.Errorf("expected ErrBuildNotFound for empty build ID, got %v", err)
	}
}

// AC-6.4 (cross-tenant 404): the tenant-scoped lookup returns
// ErrBuildNotFound when the build doesn't belong to the requesting tenant.
// The store treats "no row" identically whether the build doesn't exist
// or belongs to another tenant — by design (no existence leak).
func TestAttestationStore_GetBuildAttestationForTenant_CrossTenant_NotFound(t *testing.T) {
	// Empty result — same response shape as cross-tenant lookup miss.
	dbtx := &fakeAttestationDBTX{
		queryRow: func(_ context.Context, _ string, _ ...any) Row {
			return emptyRow{}
		},
	}

	store := NewPostgresAttestationStore(dbtx)
	_, err := store.GetBuildAttestationForTenant(context.Background(), "tn-other", "build-1")
	if !errors.Is(err, loopzeroerrors.ErrBuildNotFound) {
		t.Errorf("expected ErrBuildNotFound (no leak), got %v", err)
	}
}

// AC-6.4 cont: empty tenant ID returns ErrBuildNotFound without
// hitting the DB (defensive).
func TestAttestationStore_GetBuildAttestationForTenant_EmptyTenant(t *testing.T) {
	store := NewPostgresAttestationStore(&fakeAttestationDBTX{})
	_, err := store.GetBuildAttestationForTenant(context.Background(), "", "build-1")
	if !errors.Is(err, loopzeroerrors.ErrBuildNotFound) {
		t.Errorf("expected ErrBuildNotFound for empty tenant, got %v", err)
	}
}
