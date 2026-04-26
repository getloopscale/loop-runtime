// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// =============================================================================
// SaveAppAttestations
// =============================================================================

func TestAppAttestationStore_SaveAppAttestations_PersistsAllApps(t *testing.T) {
	signedAt := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	sig := "registry/api:v1@sha256:abc"
	tx := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}

	store := NewPostgresAppAttestationStore(tx)
	atts := []AppAttestation{
		{AppName: "api", ImageDigest: "sha256:aaaa", ImageSignature: &sig, SignedAt: &signedAt},
		{AppName: "worker", ImageDigest: "sha256:bbbb", ImageSignature: &sig, SignedAt: &signedAt},
		{AppName: "frontend", ImageDigest: "sha256:cccc"},
	}

	err := store.SaveAppAttestations(context.Background(), "rel-1", "tn-1", atts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.execCalls != 3 {
		t.Errorf("expected 3 Exec calls (one per app), got %d", tx.execCalls)
	}
}

func TestAppAttestationStore_SaveAppAttestations_RejectsBadDigest(t *testing.T) {
	tx := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresAppAttestationStore(tx)

	atts := []AppAttestation{
		{AppName: "api", ImageDigest: "not-a-digest"},
	}
	err := store.SaveAppAttestations(context.Background(), "rel-1", "tn-1", atts)
	if err == nil {
		t.Fatal("expected error for invalid digest, got nil")
	}
	if !strings.Contains(err.Error(), "sha256:") {
		t.Errorf("error should mention sha256: prefix, got %v", err)
	}
	if tx.execCalls != 0 {
		t.Errorf("expected 0 Exec calls (validation rejected), got %d", tx.execCalls)
	}
}

func TestAppAttestationStore_SaveAppAttestations_RejectsEmptyAppName(t *testing.T) {
	tx := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresAppAttestationStore(tx)

	atts := []AppAttestation{
		{AppName: "", ImageDigest: "sha256:abc"},
	}
	err := store.SaveAppAttestations(context.Background(), "rel-1", "tn-1", atts)
	if err == nil {
		t.Fatal("expected error for empty app_name, got nil")
	}
	if tx.execCalls != 0 {
		t.Errorf("expected 0 Exec calls, got %d", tx.execCalls)
	}
}

func TestAppAttestationStore_SaveAppAttestations_NoOpEmpty(t *testing.T) {
	tx := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresAppAttestationStore(tx)

	err := store.SaveAppAttestations(context.Background(), "rel-1", "tn-1", nil)
	if err != nil {
		t.Errorf("expected nil error for empty atts, got %v", err)
	}
	if tx.execCalls != 0 {
		t.Errorf("expected 0 Exec calls, got %d", tx.execCalls)
	}
}

func TestAppAttestationStore_SaveAppAttestations_RejectsEmptyReleaseID(t *testing.T) {
	tx := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresAppAttestationStore(tx)

	atts := []AppAttestation{
		{AppName: "api", ImageDigest: "sha256:abc"},
	}
	err := store.SaveAppAttestations(context.Background(), "", "tn-1", atts)
	if err == nil {
		t.Fatal("expected error for empty release_id, got nil")
	}
	err = store.SaveAppAttestations(context.Background(), "rel-1", "", atts)
	if err == nil {
		t.Fatal("expected error for empty tenant_id, got nil")
	}
}

func TestAppAttestationStore_SaveAppAttestations_PropagatesDBError(t *testing.T) {
	tx := &fakeDBTX{execErr: errors.New("connection refused")}
	store := NewPostgresAppAttestationStore(tx)

	atts := []AppAttestation{
		{AppName: "api", ImageDigest: "sha256:abc"},
	}
	err := store.SaveAppAttestations(context.Background(), "rel-1", "tn-1", atts)
	if err == nil {
		t.Fatal("expected error to propagate")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("expected wrapped DB error, got %v", err)
	}
}

// =============================================================================
// GetBuildAppAttestations
// =============================================================================

func TestAppAttestationStore_GetBuildAppAttestations_ReturnsAllApps(t *testing.T) {
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	sig := "registry/api:v1@sha256:abc"
	s3Key := "sbom/tn-1/proj/abc/api.cdx.json"
	format := SBOMFormatCycloneDXJSON
	count := 142

	rows := &fakeRowsMulti{data: [][]any{
		{"api", "sha256:aaaa", &sig, &now, &s3Key, &format, &count, now},
		{"worker", "sha256:bbbb", &sig, &now, (*string)(nil), (*string)(nil), (*int)(nil), now},
	}}
	fake := &fakeDBTX{queryRows: rows}
	store := NewPostgresAppAttestationStore(fake)

	infos, err := store.GetBuildAppAttestations(context.Background(), "tn-1", "build-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("expected 2 attestations, got %d", len(infos))
	}
	if infos[0].AppName != "api" {
		t.Errorf("first app should be api, got %q", infos[0].AppName)
	}
	if !infos[0].Signed {
		t.Error("first app should be signed")
	}
	if infos[0].SBOMS3Key != s3Key {
		t.Errorf("SBOM key mismatch: %q", infos[0].SBOMS3Key)
	}
	if infos[0].SBOMComponentCount != 142 {
		t.Errorf("SBOM count mismatch: %d", infos[0].SBOMComponentCount)
	}
	if infos[1].AppName != "worker" {
		t.Errorf("second app should be worker, got %q", infos[1].AppName)
	}
	if infos[1].SBOMS3Key != "" {
		t.Errorf("worker SBOM should be empty, got %q", infos[1].SBOMS3Key)
	}
}

func TestAppAttestationStore_GetBuildAppAttestations_CrossTenantEmpty(t *testing.T) {
	rows := &fakeRowsMulti{} // empty
	fake := &fakeDBTX{queryRows: rows}
	store := NewPostgresAppAttestationStore(fake)

	infos, err := store.GetBuildAppAttestations(context.Background(), "tn-other", "build-1")
	if err != nil {
		t.Fatalf("expected nil error (no leak), got %v", err)
	}
	if len(infos) != 0 {
		t.Errorf("expected empty slice for cross-tenant, got %d rows", len(infos))
	}
}

func TestAppAttestationStore_GetBuildAppAttestations_EmptyInputs(t *testing.T) {
	store := NewPostgresAppAttestationStore(&fakeDBTX{})

	_, err := store.GetBuildAppAttestations(context.Background(), "", "build-1")
	if !errors.Is(err, loopzeroerrors.ErrBuildNotFound) {
		t.Errorf("expected ErrBuildNotFound for empty tenant, got %v", err)
	}
	_, err = store.GetBuildAppAttestations(context.Background(), "tn-1", "")
	if !errors.Is(err, loopzeroerrors.ErrBuildNotFound) {
		t.Errorf("expected ErrBuildNotFound for empty build, got %v", err)
	}
}

// =============================================================================
// UpdateAppSBOM
// =============================================================================

func TestAppAttestationStore_UpdateAppSBOM_PersistsMetadata(t *testing.T) {
	tx := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresAppAttestationStore(tx)

	err := store.UpdateAppSBOM(context.Background(), "rel-1", "api",
		"sbom/tn-1/proj/abc/api.cdx.json", SBOMFormatCycloneDXJSON, 142)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.execCalls != 1 {
		t.Errorf("expected 1 Exec call, got %d", tx.execCalls)
	}
}

func TestAppAttestationStore_UpdateAppSBOM_RejectsBadFormat(t *testing.T) {
	tx := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresAppAttestationStore(tx)

	err := store.UpdateAppSBOM(context.Background(), "rel-1", "api",
		"key", "bogus-format", 42)
	if err == nil {
		t.Fatal("expected error for invalid sbom_format")
	}
	if tx.execCalls != 0 {
		t.Errorf("expected 0 Exec calls, got %d", tx.execCalls)
	}
}

func TestAppAttestationStore_UpdateAppSBOM_NoMatchReturnsError(t *testing.T) {
	tx := &fakeDBTX{execResult: ExecResult{RowsAffected: 0}}
	store := NewPostgresAppAttestationStore(tx)

	err := store.UpdateAppSBOM(context.Background(), "rel-missing", "api",
		"key", SBOMFormatCycloneDXJSON, 1)
	if err == nil {
		t.Fatal("expected error when no row matched")
	}
}

func TestAppAttestationStore_UpdateAppSBOM_AcceptsSPDXFormat(t *testing.T) {
	tx := &fakeDBTX{execResult: ExecResult{RowsAffected: 1}}
	store := NewPostgresAppAttestationStore(tx)

	err := store.UpdateAppSBOM(context.Background(), "rel-1", "api",
		"key", SBOMFormatSPDXJSON, 1)
	if err != nil {
		t.Errorf("SPDX should be accepted, got %v", err)
	}
}
