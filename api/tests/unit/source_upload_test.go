// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/buildplane/source"
	sourceroutes "github.com/getloopscale/loopzero/api/internal/routes/source"
)

// fakeSourceStore is a test-only SourceStore that stores bundles in memory.
// This lives in _test.go, NOT in production code.
type fakeSourceStore struct {
	Bundles   map[string][]byte
	CommitSHA string
	Error     error
}

func newFakeSourceStore(commitSHA string) *fakeSourceStore {
	return &fakeSourceStore{
		Bundles:   make(map[string][]byte),
		CommitSHA: commitSHA,
	}
}

func (f *fakeSourceStore) PushBundle(_ context.Context, tenantID, projectID string, bundle io.Reader) (source.PushResult, error) {
	if f.Error != nil {
		return source.PushResult{}, f.Error
	}
	data, err := io.ReadAll(bundle)
	if err != nil {
		return source.PushResult{}, fmt.Errorf("reading bundle: %w", err)
	}
	f.Bundles[tenantID+":"+projectID] = data
	return source.PushResult{SourceID: f.CommitSHA}, nil
}

// Plan B v3.1 T-B.3 (2026-06-05) — wire field renamed `loopzero_yaml` →
// `services_json`. Helpers updated to send the JSON payload as either
// text field or file upload (mirroring CLI behavior). Parameter names
// preserved for diff cleanliness; semantics now point at services.json.
func newMultipartRequest(t *testing.T, bundleContent []byte, servicesJSON string) *http.Request {
	t.Helper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	if bundleContent != nil {
		part, err := writer.CreateFormFile("bundle", "bundle.git")
		if err != nil {
			t.Fatal(err)
		}
		_, err = part.Write(bundleContent)
		if err != nil {
			t.Fatal(err)
		}
	}

	if servicesJSON != "" {
		err := writer.WriteField("services_json", servicesJSON)
		if err != nil {
			t.Fatal(err)
		}
	}

	err := writer.Close()
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/source/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

// Plan B v3.1 T-B.3 — file-upload helper updated for services_json wire field.
// Matches CLI behavior: `formData.append('services_json', blob, 'services.json')`.
func newMultipartRequestWithFileYAML(t *testing.T, bundleContent []byte, servicesJSON string) *http.Request {
	t.Helper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	if bundleContent != nil {
		part, err := writer.CreateFormFile("bundle", "bundle.git")
		if err != nil {
			t.Fatal(err)
		}
		_, err = part.Write(bundleContent)
		if err != nil {
			t.Fatal(err)
		}
	}

	if servicesJSON != "" {
		part, err := writer.CreateFormFile("services_json", "services.json")
		if err != nil {
			t.Fatal(err)
		}
		_, err = part.Write([]byte(servicesJSON))
		if err != nil {
			t.Fatal(err)
		}
	}

	err := writer.Close()
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/source/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func TestSourceUpload(t *testing.T) {
	// Plan B v3.1 T-B.3 — services.json equivalent of the legacy validLoopZeroYAML.
	// Two services: an HTTP server-shaped app + a worker-shaped app (mapped to
	// the services.json shape via runtime/port/proxy/healthcheck).
	validLoopZeroYAML := `{
  "version": 1,
  "services": [
    {
      "name": "api",
      "runtime": "node",
      "port": 3000,
      "proxy": "/api",
      "dev": "node dev.js",
      "start": "node prod.js",
      "healthcheck": "/health",
      "cors": false,
      "passSetCookie": false
    },
    {
      "name": "worker",
      "runtime": "node",
      "port": 4001,
      "proxy": "/worker",
      "dev": "node worker.js",
      "start": "node worker.js",
      "healthcheck": "/health",
      "cors": false,
      "passSetCookie": false
    }
  ]
}`

	t.Run("upload valid bundle returns 200 with commit_sha and apps list", func(t *testing.T) {
		// Arrange
		store := newFakeSourceStore("abc123def456")
		handler := sourceroutes.NewSourceHandler(store)

		req := newMultipartRequest(t, []byte("fake-bundle-data"), validLoopZeroYAML)
		req = req.WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		handler.Upload(rec, req)

		// Assert
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp sourceroutes.SourceUploadResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.CommitSHA != "abc123def456" {
			t.Errorf("expected commit_sha 'abc123def456', got '%s'", resp.CommitSHA)
		}

		expectedApps := []string{"api", "worker"}
		sort.Strings(resp.Apps)
		if len(resp.Apps) != len(expectedApps) {
			t.Fatalf("expected %d apps, got %d: %v", len(expectedApps), len(resp.Apps), resp.Apps)
		}
		for i, app := range resp.Apps {
			if app != expectedApps[i] {
				t.Errorf("expected app[%d] = '%s', got '%s'", i, expectedApps[i], app)
			}
		}

		// Plan B v3.1 T-B.3 — services.json is platform-neutral (no top-level
		// name field per ADR-0015). The validator now emits a stable
		// `"services-bundle"` placeholder name; production callers resolve the
		// real project name from the build context (project ID / git remote).
		// Test asserts the stable name appears in the storage key.
		if _, ok := store.Bundles["acme:services-bundle"]; !ok {
			t.Errorf("expected bundle to be stored under acme:services-bundle, got keys: %v", store.Bundles)
		}
	})

	t.Run("no loopzero_yaml returns 422", func(t *testing.T) {
		// Arrange
		store := newFakeSourceStore("abc123")
		handler := sourceroutes.NewSourceHandler(store)

		req := newMultipartRequest(t, []byte("fake-bundle"), "")
		req = req.WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		handler.Upload(rec, req)

		// Assert
		if rec.Code != 422 {
			t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
		}

		var errResp map[string]string
		_ = json.NewDecoder(rec.Body).Decode(&errResp)
		if errResp["code"] != "VALIDATION_FAILED" {
			t.Errorf("expected code VALIDATION_FAILED, got '%s'", errResp["code"])
		}
	})

	t.Run("invalid loopzero_yaml with missing name returns 422", func(t *testing.T) {
		// Plan B v3.1 T-B.3 — services.json has no top-level `name` field
		// (platform-neutral manifest per ADR-0015). Skipped: legacy behavior.
		t.Skip("legacy loopzero.yaml flow removed post Plan B v3.1 T-B.3 cutover")
		// Arrange
		store := newFakeSourceStore("abc123")
		handler := sourceroutes.NewSourceHandler(store)

		loopzeroYAML := `
apps:
  api:
    type: server
    port: 3000
`
		req := newMultipartRequest(t, []byte("fake-bundle"), loopzeroYAML)
		req = req.WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		handler.Upload(rec, req)

		// Assert
		if rec.Code != 422 {
			t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
		}

		var errResp map[string]string
		_ = json.NewDecoder(rec.Body).Decode(&errResp)
		if errResp["code"] != "VALIDATION_FAILED" {
			t.Errorf("expected code VALIDATION_FAILED, got '%s'", errResp["code"])
		}
	})

	t.Run("0 apps in loopzero_yaml returns 422 with no apps message", func(t *testing.T) {
		// Plan B v3.1 T-B.3 (2026-06-05) — skipped: this subtest validates the
		// legacy loopzero.yaml `0 apps` message which no longer exists. The
		// equivalent services.json validation (empty `services[]`) is covered
		// in `api/internal/buildplane/source/validator_services_test.go`.
		t.Skip("legacy loopzero.yaml flow removed post Plan B v3.1 T-B.3 cutover")
		// Arrange
		store := newFakeSourceStore("abc123")
		handler := sourceroutes.NewSourceHandler(store)

		loopzeroYAML := `
name: my-saas
apps: {}
`
		req := newMultipartRequest(t, []byte("fake-bundle"), loopzeroYAML)
		req = req.WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		handler.Upload(rec, req)

		// Assert
		if rec.Code != 422 {
			t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
		}

		var errResp map[string]string
		_ = json.NewDecoder(rec.Body).Decode(&errResp)
		if errResp["message"] != "No apps defined in loopzero.yaml" {
			t.Errorf("expected message 'No apps defined in loopzero.yaml', got '%s'", errResp["message"])
		}
	})

	t.Run("app type ambiguous (no port and no worker/frontend type) returns 422", func(t *testing.T) {
		// Plan B v3.1 T-B.3 — services.json schema requires `port` (minimum
		// 1024) on every service; ambiguity contract differs. Skipped: legacy.
		t.Skip("legacy loopzero.yaml flow removed post Plan B v3.1 T-B.3 cutover")
		// Arrange
		store := newFakeSourceStore("abc123")
		handler := sourceroutes.NewSourceHandler(store)

		loopzeroYAML := `
name: my-saas
apps:
  api:
    path: ./api
`
		req := newMultipartRequest(t, []byte("fake-bundle"), loopzeroYAML)
		req = req.WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		handler.Upload(rec, req)

		// Assert
		if rec.Code != 422 {
			t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
		}

		var errResp map[string]string
		_ = json.NewDecoder(rec.Body).Decode(&errResp)
		if errResp["code"] != "VALIDATION_FAILED" {
			t.Errorf("expected code VALIDATION_FAILED, got '%s'", errResp["code"])
		}
		// Should mention the app name and the issue
		if errResp["message"] == "" {
			t.Error("expected a specific error message about the ambiguous app type")
		}
	})

	t.Run("bundle too large returns 413", func(t *testing.T) {
		// Arrange
		store := newFakeSourceStore("abc123")
		handler := sourceroutes.NewSourceHandler(store)

		// Create a request with Content-Length exceeding 500MB
		// We don't actually create a 500MB body — we set Content-Length header
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		_ = writer.WriteField("loopzero_yaml", validLoopZeroYAML)
		part, _ := writer.CreateFormFile("bundle", "bundle.git")
		_, _ = part.Write([]byte("small"))
		_ = writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/source/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		// Simulate oversized content by setting Content-Length > 500MB
		req.ContentLength = 600 * 1024 * 1024
		req = req.WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		handler.Upload(rec, req)

		// Assert
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("expected 413, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("no auth returns 401", func(t *testing.T) {
		// Arrange
		store := newFakeSourceStore("abc123")
		handler := sourceroutes.NewSourceHandler(store)

		req := newMultipartRequest(t, []byte("fake-bundle"), validLoopZeroYAML)
		// No auth context set
		rec := httptest.NewRecorder()

		// Act
		handler.Upload(rec, req)

		// Assert
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("valid app with type worker (no port needed) returns 200", func(t *testing.T) {
		// Plan B v3.1 T-B.3 — services.json doesn't model worker type
		// (sidecar shape only). Worker semantics now expressed via dedicated
		// loopzerokit primitive (`defineJob`) outside the deploy bundle. Skipped.
		t.Skip("legacy loopzero.yaml worker-type flow removed post Plan B v3.1")
		// Arrange
		store := newFakeSourceStore("sha-worker-ok")
		handler := sourceroutes.NewSourceHandler(store)

		loopzeroYAML := `
name: my-saas
apps:
  background-job:
    type: worker
    path: ./jobs
`
		req := newMultipartRequest(t, []byte("fake-bundle"), loopzeroYAML)
		req = req.WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		handler.Upload(rec, req)

		// Assert
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp sourceroutes.SourceUploadResponse
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		if resp.CommitSHA != "sha-worker-ok" {
			t.Errorf("expected commit_sha 'sha-worker-ok', got '%s'", resp.CommitSHA)
		}
		if len(resp.Apps) != 1 || resp.Apps[0] != "background-job" {
			t.Errorf("expected apps ['background-job'], got %v", resp.Apps)
		}
	})

	t.Run("server type without port returns 422", func(t *testing.T) {
		// Plan B v3.1 T-B.3 — services.json schema enforces `port` as required
		// (1024-65535) at the JSON Schema layer. Validator tests in
		// `validator_services_test.go` cover the equivalent contract.
		t.Skip("legacy loopzero.yaml port-validation flow removed post Plan B v3.1")
		// Arrange
		store := newFakeSourceStore("abc123")
		handler := sourceroutes.NewSourceHandler(store)

		loopzeroYAML := `
name: my-saas
apps:
  api:
    type: server
    path: ./api
`
		req := newMultipartRequest(t, []byte("fake-bundle"), loopzeroYAML)
		req = req.WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		handler.Upload(rec, req)

		// Assert
		if rec.Code != 422 {
			t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
		}

		var errResp map[string]string
		_ = json.NewDecoder(rec.Body).Decode(&errResp)
		if errResp["code"] != "VALIDATION_FAILED" {
			t.Errorf("expected code VALIDATION_FAILED, got '%s'", errResp["code"])
		}
	})

	t.Run("source store error returns 500", func(t *testing.T) {
		// Arrange
		store := newFakeSourceStore("abc123")
		store.Error = fmt.Errorf("gitea connection refused")
		handler := sourceroutes.NewSourceHandler(store)

		req := newMultipartRequest(t, []byte("fake-bundle"), validLoopZeroYAML)
		req = req.WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		handler.Upload(rec, req)

		// Assert
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
		}

		var errResp map[string]string
		_ = json.NewDecoder(rec.Body).Decode(&errResp)
		if errResp["code"] != "SOURCE_UPLOAD_FAILED" {
			t.Errorf("expected code SOURCE_UPLOAD_FAILED, got '%s'", errResp["code"])
		}
	})

	t.Run("loopzero_yaml sent as file upload (CLI behavior) returns 200", func(t *testing.T) {
		// Arrange — the CLI sends loopzero_yaml as a file (Blob with filename),
		// not as a text field. The API must accept both forms.
		store := newFakeSourceStore("sha-file-yaml")
		handler := sourceroutes.NewSourceHandler(store)

		req := newMultipartRequestWithFileYAML(t, []byte("fake-bundle-data"), validLoopZeroYAML)
		req = req.WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		handler.Upload(rec, req)

		// Assert
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp sourceroutes.SourceUploadResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp.CommitSHA != "sha-file-yaml" {
			t.Errorf("expected commit_sha 'sha-file-yaml', got '%s'", resp.CommitSHA)
		}

		expectedApps := []string{"api", "worker"}
		sort.Strings(resp.Apps)
		if len(resp.Apps) != len(expectedApps) {
			t.Fatalf("expected %d apps, got %d: %v", len(expectedApps), len(resp.Apps), resp.Apps)
		}
		for i, app := range resp.Apps {
			if app != expectedApps[i] {
				t.Errorf("expected app[%d] = '%s', got '%s'", i, expectedApps[i], app)
			}
		}
	})

	t.Run("valid app with type frontend (no port needed) returns 200", func(t *testing.T) {
		// Plan B v3.1 T-B.3 — services.json doesn't model frontend type
		// (CDN-static frontends are loopzerokit-build emitted separately; the
		// services.json payload only models polyglot sidecars). Skipped.
		t.Skip("legacy loopzero.yaml frontend-type flow removed post Plan B v3.1")
		// Arrange
		store := newFakeSourceStore("sha-frontend-ok")
		handler := sourceroutes.NewSourceHandler(store)

		loopzeroYAML := `
name: my-saas
apps:
  web:
    type: frontend
    path: ./web
`
		req := newMultipartRequest(t, []byte("fake-bundle"), loopzeroYAML)
		req = req.WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		handler.Upload(rec, req)

		// Assert
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp sourceroutes.SourceUploadResponse
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		if len(resp.Apps) != 1 || resp.Apps[0] != "web" {
			t.Errorf("expected apps ['web'], got %v", resp.Apps)
		}
	})
}
