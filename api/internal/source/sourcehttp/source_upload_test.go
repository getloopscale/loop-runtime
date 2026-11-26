// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package sourcehttp_test

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
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/middleware/auth/authtest"
	"github.com/getloopscale/loop-runtime/api/internal/source"
	sourceroutes "github.com/getloopscale/loop-runtime/api/internal/source/sourcehttp"
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

	// services_json first, bundle second: the order is part of the wire
	// contract (docs/loopzerokit-loopzerocloud-seam.md) and the handler enforces it.
	if servicesJSON != "" {
		err := writer.WriteField("services_json", servicesJSON)
		if err != nil {
			t.Fatal(err)
		}
	}

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
		req = req.WithContext(authtest.Context("github-lucas", "acme", "owner"))
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

	t.Run("form without services_json returns 422 E-008", func(t *testing.T) {
		// Arrange -- a well-formed multipart body carrying no parts at all.
		// A body whose FIRST part is the bundle is a field-order violation
		// instead (400 MULTIPART_FIELD_ORDER, source_upload_field_limits_test):
		// the manifest must be validated before any bundle byte is read.
		store := newFakeSourceStore("abc123")
		handler := sourceroutes.NewSourceHandler(store)

		req := newMultipartRequest(t, nil, "")
		req = req.WithContext(authtest.Context("github-lucas", "acme", "owner"))
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

	t.Run("legacy loopzero_yaml is rejected with the E-009 migration hint", func(t *testing.T) {
		// B-184. This subtest replaces SIX that stood here asserting the legacy
		// loopzero.yaml flow (missing name -> 422, 0 apps -> 422, ambiguous app type ->
		// 422, worker type -> 200, server-without-port -> 422, frontend type ->
		// 200). Each opened with a BARE t.Skip and no guard --
		//
		//	t.Skip("legacy loopzero.yaml flow removed post Plan B v3.1 T-B.3 cutover")
		//	store := newFakeSourceStore("abc123")
		//	... eleven more statements ...
		//
		// -- so a skip marked each whole closure skipped and none of those bodies
		// had run since the T-B.3 cutover. Their reason was true: the flow is gone,
		// and they passed a YAML document into newMultipartRequest's servicesJSON
		// parameter, so they could not have passed if they had run.
		//
		// What replaced the flow had no test at all: the handler now REJECTS a
		// legacy loopzero_yaml field with an E-009 migration hint
		// (api/internal/source/sourcehttp/source.go, EC-B3-7). Six dead assertions
		// about a deleted behaviour become one live assertion about the behaviour
		// that deleted it.
		store := newFakeSourceStore("sha-legacy-reject")
		handler := sourceroutes.NewSourceHandler(store)

		// Arrange -- the legacy loopzero_yaml field in the manifest slot, then a
		// bundle, and NO services_json.
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		if err := writer.WriteField("loopzero_yaml", "apps:\n  api:\n    type: server\n    port: 3000\n"); err != nil {
			t.Fatal(err)
		}
		part, err := writer.CreateFormFile("bundle", "bundle.git")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte("fake-bundle")); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/source/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req = req.WithContext(authtest.Context("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		handler.Upload(rec, req)

		// Assert
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for legacy loopzero_yaml, got %d: %s", rec.Code, rec.Body.String())
		}
		var errResp map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
			t.Fatalf("failed to decode error response: %v", err)
		}
		if errResp["code"] != "VALIDATION_FAILED" {
			t.Errorf("expected code VALIDATION_FAILED, got %v", errResp["code"])
		}
		// The message must carry the migration hint, not just a generic refusal:
		// E-009 is what tells a caller on old LoopZeroKit what to do next. Asserting
		// only the 422 would pass against the E-008 "services.json is required"
		// branch, which is a different answer to a different mistake.
		msg, _ := errResp["message"].(string)
		for _, want := range []string{"E-009", "services.json", "LoopZeroKit"} {
			if !strings.Contains(msg, want) {
				t.Errorf("E-009 rejection message must mention %q; got %q", want, msg)
			}
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
		req = req.WithContext(authtest.Context("github-lucas", "acme", "owner"))
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

	t.Run("source store error returns 500", func(t *testing.T) {
		// Arrange
		store := newFakeSourceStore("abc123")
		store.Error = fmt.Errorf("gitea connection refused")
		handler := sourceroutes.NewSourceHandler(store)

		req := newMultipartRequest(t, []byte("fake-bundle"), validLoopZeroYAML)
		req = req.WithContext(authtest.Context("github-lucas", "acme", "owner"))
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
		req = req.WithContext(authtest.Context("github-lucas", "acme", "owner"))
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

}
