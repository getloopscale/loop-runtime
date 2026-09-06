// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/routes/routestest"
)

// --- Fake AttestationStore ---

type fakeAttestationStore struct {
	info *AttestationInfo
	err  error
}

func (f *fakeAttestationStore) GetBuildAttestation(_ context.Context, _ string) (*AttestationInfo, error) {
	return f.info, f.err
}

// --- Tests ---

func TestBuildAttestation_Get_Found(t *testing.T) {
	store := &fakeAttestationStore{
		info: &AttestationInfo{
			BuildID:     "bld-abc123",
			ImageDigest: "sha256:abc",
			Signed:      true,
			SignedAt:    "2026-01-01T00:00:00Z",
		},
	}
	h := NewBuildAttestationHandler(store, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-abc123/attestation", nil)
	req = routestest.WithChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp AttestationInfo
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.BuildID != "bld-abc123" {
		t.Fatalf("want build_id bld-abc123, got %s", resp.BuildID)
	}
	if !resp.Signed {
		t.Fatal("want signed=true")
	}
}

func TestBuildAttestation_Get_NotFound(t *testing.T) {
	store := &fakeAttestationStore{
		err: errors.New("not found"),
	}
	h := NewBuildAttestationHandler(store, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-nonexistent/attestation", nil)
	req = routestest.WithChiParam(req, "id", "bld-nonexistent")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuildAttestation_Get_MissingID(t *testing.T) {
	h := NewBuildAttestationHandler(&fakeAttestationStore{}, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds//attestation", nil)
	req = routestest.WithChiParam(req, "id", "")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}
