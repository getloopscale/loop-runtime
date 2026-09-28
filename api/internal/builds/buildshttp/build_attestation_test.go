// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package buildshttp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/routestest"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// --- Fake AttestationStore ---

// fakeAttestationStore models the production store faithfully: every
// attestation belongs to exactly one tenant, and the tenant-scoped read
// answers ErrBuildNotFound for a build owned by someone else.
type fakeAttestationStore struct {
	owner string // tenant that owns info
	info  *AttestationInfo
	err   error
}

func (f *fakeAttestationStore) GetBuildAttestationForTenant(_ context.Context, tenantID, _ string) (*AttestationInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.info == nil || tenantID != f.owner {
		return nil, loopzeroerrors.ErrBuildNotFound
	}
	return f.info, nil
}

func attestationRequest(buildID string, withUser bool) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/"+buildID+"/attestation", nil)
	req = routestest.WithChiParam(req, "id", buildID)
	if withUser {
		req = routestest.WithAuth(req, routestest.DefaultUserContext())
	}
	return req
}

// --- Tests ---

func TestBuildAttestation_Get_ReturnsAttestationOfCallersOwnBuild(t *testing.T) {
	store := &fakeAttestationStore{
		owner: routestest.DefaultUserContext().TenantID,
		info: &AttestationInfo{
			BuildID:     "bld-abc123",
			ImageDigest: "sha256:abc",
			Signed:      true,
			SignedAt:    "2026-01-01T00:00:00Z",
		},
	}
	h := NewBuildAttestationHandler(store, slog.Default())
	w := httptest.NewRecorder()

	h.Get(w, attestationRequest("bld-abc123", true))

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp AttestationInfo
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.BuildID != "bld-abc123" || !resp.Signed {
		t.Fatalf("want signed bld-abc123, got %+v", resp)
	}
}

func TestBuildAttestation_Get_HidesAnotherTenantsBuildAsNotFound(t *testing.T) {
	store := &fakeAttestationStore{
		owner: "tn-someone-else",
		info:  &AttestationInfo{BuildID: "bld-foreign", ImageDigest: "sha256:secret", Signed: true},
	}
	h := NewBuildAttestationHandler(store, slog.Default())
	w := httptest.NewRecorder()

	h.Get(w, attestationRequest("bld-foreign", true))

	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant read: want 404, got %d body=%s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); json.Valid([]byte(body)) && containsDigest(body) {
		t.Fatalf("cross-tenant read leaked the image digest: %s", body)
	}
}

func TestBuildAttestation_Get_RejectsUnauthenticatedCaller(t *testing.T) {
	store := &fakeAttestationStore{owner: "tn-test-123", info: &AttestationInfo{BuildID: "bld-abc"}}
	h := NewBuildAttestationHandler(store, slog.Default())
	w := httptest.NewRecorder()

	h.Get(w, attestationRequest("bld-abc", false))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuildAttestation_Get_MapsUnknownBuildToNotFound(t *testing.T) {
	h := NewBuildAttestationHandler(&fakeAttestationStore{owner: "tn-test-123"}, slog.Default())
	w := httptest.NewRecorder()

	h.Get(w, attestationRequest("bld-nonexistent", true))

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuildAttestation_Get_ReportsStoreFailureAsServerError(t *testing.T) {
	store := &fakeAttestationStore{err: errors.New("connection refused")}
	h := NewBuildAttestationHandler(store, slog.Default())
	w := httptest.NewRecorder()

	h.Get(w, attestationRequest("bld-abc", true))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("store outage must not read as a missing build: want 500, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuildAttestation_Get_RejectsMissingBuildID(t *testing.T) {
	h := NewBuildAttestationHandler(&fakeAttestationStore{}, slog.Default())
	w := httptest.NewRecorder()

	h.Get(w, attestationRequest("", true))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func containsDigest(body string) bool {
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		return false
	}
	_, ok := m["image_digest"]
	return ok
}
