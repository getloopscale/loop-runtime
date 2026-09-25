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
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/routestest"

	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// =============================================================================
// Fakes
// =============================================================================

type fakeSBOMStore struct {
	infos []AppSBOMInfo
	err   error
}

func (f *fakeSBOMStore) GetBuildAppSBOMs(_ context.Context, _, _ string) ([]AppSBOMInfo, error) {
	return f.infos, f.err
}

type fakeSBOMSigner struct {
	called bool
	url    string
	err    error
}

func (f *fakeSBOMSigner) Presign(_ context.Context, _, key string, _ time.Duration) (string, error) {
	f.called = true
	if f.err != nil {
		return "", f.err
	}
	if f.url != "" {
		return f.url, nil
	}
	return "https://signed.example.com/" + key + "?sig=abc", nil
}

// withTenantContext injects a UserContext so the handler treats the
// request as authenticated.
func withTenantContext(req *http.Request, tenantID string) *http.Request {
	ctx := context.WithValue(req.Context(),
		auth.UserContextKeyPublic(),
		&auth.UserContext{
			UserID:   "user-1",
			Email:    "u@example.com",
			TenantID: tenantID,
			Role:     "owner",
		},
	)
	return req.WithContext(ctx)
}

// =============================================================================
// Happy path
// =============================================================================

func TestBuildSBOMHandler_ReturnsAllAppsWithDownloadURL(t *testing.T) {
	store := &fakeSBOMStore{
		infos: []AppSBOMInfo{
			{
				AppName:            "api",
				SBOMS3Key:          "sbom/tn-1/proj/abc/api.cdx.json",
				SBOMFormat:         "cyclonedx-json",
				SBOMComponentCount: 142,
				SBOMGeneratedAt:    "2026-04-26T10:30:00Z",
				Signed:             true,
			},
			{
				AppName:            "worker",
				SBOMS3Key:          "sbom/tn-1/proj/abc/worker.cdx.json",
				SBOMFormat:         "cyclonedx-json",
				SBOMComponentCount: 87,
				SBOMGeneratedAt:    "2026-04-26T10:30:00Z",
				Signed:             true,
			},
		},
	}
	signer := &fakeSBOMSigner{}
	h := NewBuildSBOMHandler(store, signer, "loopzero-static-assets", slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/build-1/sbom", nil)
	req = routestest.WithChiParam(req, "id", "build-1")
	req = withTenantContext(req, "tn-1")
	w := httptest.NewRecorder()

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp BuildSBOMResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.BuildID != "build-1" {
		t.Errorf("BuildID = %q, want build-1", resp.BuildID)
	}
	if len(resp.Apps) != 2 {
		t.Fatalf("want 2 apps, got %d", len(resp.Apps))
	}
	if resp.Apps[0].DownloadURL == "" {
		t.Error("download_url must be populated when signer is configured")
	}
	if !strings.Contains(resp.Apps[0].DownloadURL, "signed.example.com") {
		t.Errorf("unexpected download URL: %s", resp.Apps[0].DownloadURL)
	}
	if resp.Apps[0].ComponentCount != 142 {
		t.Errorf("ComponentCount = %d, want 142", resp.Apps[0].ComponentCount)
	}
}

// =============================================================================
// Error paths
// =============================================================================

func TestBuildSBOMHandler_NoBuildReturns404(t *testing.T) {
	store := &fakeSBOMStore{err: loopzeroerrors.ErrBuildNotFound}
	h := NewBuildSBOMHandler(store, &fakeSBOMSigner{}, "bucket", slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/missing/sbom", nil)
	req = routestest.WithChiParam(req, "id", "missing")
	req = withTenantContext(req, "tn-1")
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestBuildSBOMHandler_CrossTenantReturns404(t *testing.T) {
	// Cross-tenant: store returns empty (no leak). Handler should map empty
	// → 404, not 200 with empty array.
	store := &fakeSBOMStore{infos: nil}
	h := NewBuildSBOMHandler(store, &fakeSBOMSigner{}, "bucket", slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/build-1/sbom", nil)
	req = routestest.WithChiParam(req, "id", "build-1")
	req = withTenantContext(req, "tn-other")
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 for cross-tenant, got %d", w.Code)
	}
}

func TestBuildSBOMHandler_NoBuildID_Returns400(t *testing.T) {
	h := NewBuildSBOMHandler(&fakeSBOMStore{}, &fakeSBOMSigner{}, "bucket", slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds//sbom", nil)
	req = routestest.WithChiParam(req, "id", "")
	req = withTenantContext(req, "tn-1")
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestBuildSBOMHandler_Unauthenticated_Returns401(t *testing.T) {
	h := NewBuildSBOMHandler(&fakeSBOMStore{}, &fakeSBOMSigner{}, "bucket", slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/build-1/sbom", nil)
	req = routestest.WithChiParam(req, "id", "build-1")
	// no withTenantContext — request is anonymous
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestBuildSBOMHandler_StoreError_Returns500(t *testing.T) {
	store := &fakeSBOMStore{err: errors.New("postgres exploded")}
	h := NewBuildSBOMHandler(store, &fakeSBOMSigner{}, "bucket", slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/build-1/sbom", nil)
	req = routestest.WithChiParam(req, "id", "build-1")
	req = withTenantContext(req, "tn-1")
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
}

// Apps without an SBOM (legacy releases, signing-only) are filtered out.
// The endpoint returns only apps with SBOM data.
func TestBuildSBOMHandler_FiltersAppsWithoutSBOM(t *testing.T) {
	store := &fakeSBOMStore{
		infos: []AppSBOMInfo{
			{AppName: "api", SBOMS3Key: "key-api", SBOMFormat: "cyclonedx-json", SBOMComponentCount: 10, Signed: true},
			{AppName: "legacy", SBOMS3Key: "", Signed: true}, // no SBOM
		},
	}
	signer := &fakeSBOMSigner{url: "https://signed.example/key"}
	h := NewBuildSBOMHandler(store, signer, "bucket", slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/build-1/sbom", nil)
	req = routestest.WithChiParam(req, "id", "build-1")
	req = withTenantContext(req, "tn-1")
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp BuildSBOMResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Apps) != 1 {
		t.Fatalf("expected 1 app (filtered), got %d", len(resp.Apps))
	}
	if resp.Apps[0].AppName != "api" {
		t.Errorf("only api should be present, got %q", resp.Apps[0].AppName)
	}
}

// When signer is nil (no S3 configured), metadata is still served with
// download_url omitted (graceful degradation).
func TestBuildSBOMHandler_NilSigner_OmitsDownloadURL(t *testing.T) {
	store := &fakeSBOMStore{
		infos: []AppSBOMInfo{
			{AppName: "api", SBOMS3Key: "k", SBOMFormat: "cyclonedx-json", SBOMComponentCount: 5, Signed: true},
		},
	}
	h := NewBuildSBOMHandler(store, nil, "", slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/build-1/sbom", nil)
	req = routestest.WithChiParam(req, "id", "build-1")
	req = withTenantContext(req, "tn-1")
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp BuildSBOMResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Apps) != 1 {
		t.Fatalf("want 1 app, got %d", len(resp.Apps))
	}
	if resp.Apps[0].DownloadURL != "" {
		t.Errorf("DownloadURL should be empty when signer is nil, got %q", resp.Apps[0].DownloadURL)
	}
	if resp.Apps[0].ComponentCount != 5 {
		t.Errorf("ComponentCount = %d", resp.Apps[0].ComponentCount)
	}
}

// Presign failure must NOT crash the handler — the metadata is still returned.
func TestBuildSBOMHandler_PresignFails_ReturnsMetadataWithoutURL(t *testing.T) {
	store := &fakeSBOMStore{
		infos: []AppSBOMInfo{
			{AppName: "api", SBOMS3Key: "k", SBOMFormat: "cyclonedx-json", SBOMComponentCount: 5, Signed: true},
		},
	}
	signer := &fakeSBOMSigner{err: errors.New("aws unreachable")}
	h := NewBuildSBOMHandler(store, signer, "bucket", slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/build-1/sbom", nil)
	req = routestest.WithChiParam(req, "id", "build-1")
	req = withTenantContext(req, "tn-1")
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 (graceful degradation), got %d", w.Code)
	}
	var resp BuildSBOMResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Apps) != 1 {
		t.Fatalf("want 1 app, got %d", len(resp.Apps))
	}
	if resp.Apps[0].DownloadURL != "" {
		t.Error("DownloadURL must be empty when presign failed")
	}
}
