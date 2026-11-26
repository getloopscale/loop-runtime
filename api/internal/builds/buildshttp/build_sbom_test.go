// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package buildshttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

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

type fakeSBOMReader struct {
	objects map[string]string
	err     error
	opened  []string
}

func (f *fakeSBOMReader) Open(_ context.Context, bucket, key string) (io.ReadCloser, error) {
	f.opened = append(f.opened, bucket+"/"+key)
	if f.err != nil {
		return nil, f.err
	}
	content, ok := f.objects[key]
	if !ok {
		return nil, errors.New("no such key")
	}
	return io.NopCloser(strings.NewReader(content)), nil
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

func TestBuildSBOMHandler_ReturnsAllAppsWithDownloadPath(t *testing.T) {
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
	h := NewBuildSBOMHandler(store, &fakeSBOMReader{}, "loopzero-static-assets", slog.Default())

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
	// The link is a path of this API: no storage address reaches the browser (#104).
	if got, want := resp.Apps[0].DownloadPath, "/api/v1/builds/build-1/sbom/api/download"; got != want {
		t.Errorf("download_path = %q, want %q", got, want)
	}
	if strings.Contains(w.Body.String(), "download_url") {
		t.Errorf("the response still carries download_url: %s", w.Body.String())
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
	h := NewBuildSBOMHandler(store, &fakeSBOMReader{}, "bucket", slog.Default())

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
	h := NewBuildSBOMHandler(store, &fakeSBOMReader{}, "bucket", slog.Default())

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
	h := NewBuildSBOMHandler(&fakeSBOMStore{}, &fakeSBOMReader{}, "bucket", slog.Default())

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
	h := NewBuildSBOMHandler(&fakeSBOMStore{}, &fakeSBOMReader{}, "bucket", slog.Default())

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
	h := NewBuildSBOMHandler(store, &fakeSBOMReader{}, "bucket", slog.Default())

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
	h := NewBuildSBOMHandler(store, &fakeSBOMReader{}, "bucket", slog.Default())

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

func TestBuildSBOMHandler_WithoutAnObjectStoreOmitsTheDownloadPath(t *testing.T) {
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
	if resp.Apps[0].DownloadPath != "" {
		t.Errorf("download_path = %q, want none without an object store", resp.Apps[0].DownloadPath)
	}
	if resp.Apps[0].ComponentCount != 5 {
		t.Errorf("ComponentCount = %d", resp.Apps[0].ComponentCount)
	}
}

// =============================================================================
// Download
// =============================================================================

func downloadSBOM(h *BuildSBOMHandler, tenantID, buildID, app string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/"+buildID+"/sbom/"+app+"/download", nil)
	if tenantID != "" {
		req = withTenantContext(req, tenantID)
	}
	router := chi.NewRouter()
	router.Get("/api/v1/builds/{id}/sbom/{app}/download", h.Download)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

var twoAppSBOMs = []AppSBOMInfo{
	{AppName: "api", SBOMS3Key: "sbom/tn-1/proj/abc/api.cdx.json", SBOMFormat: "cyclonedx-json"},
	{AppName: "worker", SBOMS3Key: "sbom/tn-1/proj/abc/worker.cdx.json", SBOMFormat: "cyclonedx-json"},
	{AppName: "legacy"},
}

func TestBuildSBOMDownload_StreamsTheObjectOfThatApp(t *testing.T) {
	reader := &fakeSBOMReader{objects: map[string]string{
		"sbom/tn-1/proj/abc/api.cdx.json":    `{"bomFormat":"CycloneDX","app":"api"}`,
		"sbom/tn-1/proj/abc/worker.cdx.json": `{"bomFormat":"CycloneDX","app":"worker"}`,
	}}
	h := NewBuildSBOMHandler(&fakeSBOMStore{infos: twoAppSBOMs}, reader, "builds", slog.Default())

	w := downloadSBOM(h, "tn-1", "build-1", "worker")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != `{"bomFormat":"CycloneDX","app":"worker"}` {
		t.Errorf("body = %s", got)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got, want := w.Header().Get("Content-Disposition"), `attachment; filename=build-1-worker.cdx.json`; got != want {
		t.Errorf("Content-Disposition = %q, want %q", got, want)
	}
	if len(reader.opened) != 1 || reader.opened[0] != "builds/sbom/tn-1/proj/abc/worker.cdx.json" {
		t.Errorf("opened = %v", reader.opened)
	}
}

func TestBuildSBOMDownload_Refusals(t *testing.T) {
	objects := map[string]string{"sbom/tn-1/proj/abc/api.cdx.json": "{}"}
	cases := []struct {
		name   string
		store  *fakeSBOMStore
		reader SBOMObjectReader
		tenant string
		app    string
		want   int
	}{
		{"an app the build does not have", &fakeSBOMStore{infos: twoAppSBOMs}, &fakeSBOMReader{objects: objects}, "tn-1", "ghost", http.StatusNotFound},
		{"an app with no SBOM", &fakeSBOMStore{infos: twoAppSBOMs}, &fakeSBOMReader{objects: objects}, "tn-1", "legacy", http.StatusNotFound},
		{"a build of another tenant", &fakeSBOMStore{err: loopzeroerrors.ErrBuildNotFound}, &fakeSBOMReader{objects: objects}, "tn-2", "api", http.StatusNotFound},
		{"no session", &fakeSBOMStore{infos: twoAppSBOMs}, &fakeSBOMReader{objects: objects}, "", "api", http.StatusUnauthorized},
		{"no object store in this boot", &fakeSBOMStore{infos: twoAppSBOMs}, nil, "tn-1", "api", http.StatusNotFound},
		{"the object store fails", &fakeSBOMStore{infos: twoAppSBOMs}, &fakeSBOMReader{err: errors.New("s3 unreachable")}, "tn-1", "api", http.StatusBadGateway},
		{"the lookup fails", &fakeSBOMStore{err: errors.New("db down")}, &fakeSBOMReader{objects: objects}, "tn-1", "api", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewBuildSBOMHandler(tc.store, tc.reader, "builds", slog.Default())
			w := downloadSBOM(h, tc.tenant, "build-1", tc.app)
			if w.Code != tc.want {
				t.Errorf("status = %d, want %d (body=%s)", w.Code, tc.want, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "s3 unreachable") || strings.Contains(w.Body.String(), "db down") {
				t.Errorf("the cause leaked to the client: %s", w.Body.String())
			}
		})
	}
}
