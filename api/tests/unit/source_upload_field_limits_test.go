// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Plan v1.2 T6.1 (cutover-deep-review-hardening) — multipart per-field
// limits + field-order tests for POST /api/v1/source/upload.

package unit

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/routes"
	"github.com/getloopscale/loopzero/api/internal/server"
)

func ioNopCloser(r io.Reader) io.ReadCloser { return io.NopCloser(r) }

func t61BaseCfg(logger *slog.Logger) server.Config {
	return server.Config{
		Port:                 "0",
		Logger:               logger,
		DB:                   &stubDBHealthChecker{},
		TokenValidator:       &stubTokenValidator{},
		MemberStore:          &stubMemberStore{},
		ProjectStore:         &stubProjectStore{},
		TenantCRMgr:          &stubTenantCRManager{},
		SourceStore:          &stubSourceStore{},
		IdempotencyStore:     &stubIdempotencyStore{},
		EventStore:           &stubEventStore{},
		BuildStoreReader:     &stubBuildReader{},
		BootstrapStore:       &stubBootstrapStore{},
		BootstrapSecret:      "test-bootstrap-secret",
		BuildHandler:         routes.NewBuildHandler(nil, logger),
		LogsHandler:          routes.NewLogsHandler(nil, nil),
		RollbackHandler:      routes.NewRollbackHandler(nil, nil, "test.local"),
		StatusHandler:        routes.NewStatusHandler(nil, nil, nil, "test.local", "", ""),
		BuildListHandler:     routes.NewBuildListHandler(nil),
		EnvironmentHandler:   routes.NewEnvironmentHandler(nil),
		TeamHandler:          routes.NewTeamHandler(nil),
		DomainHandler:        routes.NewDomainHandler(nil, nil, nil, "test.local"),
		RepositoryHandler:    routes.NewRepositoryHandler(nil),
		AuditHandler:         routes.NewAuditHandler(nil),
		ArgoCallbackHandler:  routes.NewArgoCallbackHandler(&stubReleaseActivator{}, logger, "test-secret"),
		LogStreamHandler:     routes.NewLogStreamHandler(nil, nil, routes.LogStreamHandlerOpts{}),
		GitHubWebhookHandler: &stubGitHubWebhookHandler{},
		TenantDeletionStore:  &stubTenantDeletionStore{},
		AdminEvictor:         &stubTenantEvictor{},
	}
}

// EC-4 — services_json field exceeding 256 KB is rejected with HTTP 413 +
// code SERVICES_JSON_TOO_LARGE.
func TestSourceUpload_ServicesJSONExceeds256KB_Returns413(t *testing.T) {
	t.Setenv("LOOPZERO_ADMIN_API", "true")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	router := server.NewRouter(t61BaseCfg(logger))

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	huge := strings.Repeat("x", 300*1024) // 300 KB — exceeds the 256 KB cap
	if err := w.WriteField("services_json", huge); err != nil {
		t.Fatal(err)
	}
	part, err := w.CreateFormFile("bundle", "bundle.git")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("PACK")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/source/upload", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer test-token")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if code, _ := resp["code"].(string); code != "SERVICES_JSON_TOO_LARGE" {
		t.Errorf("expected code SERVICES_JSON_TOO_LARGE, got %q", code)
	}
}

// EC-5 — when the multipart body sends bundle BEFORE services_json the API
// MUST reject with HTTP 400 + code MULTIPART_FIELD_ORDER.
func TestSourceUpload_BundleFirstFieldOrder_Returns400(t *testing.T) {
	t.Setenv("LOOPZERO_ADMIN_API", "true")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	router := server.NewRouter(t61BaseCfg(logger))

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	// Intentionally wrong order: bundle first.
	part, err := w.CreateFormFile("bundle", "bundle.git")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("PACK")); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteField("services_json", `{"version":1,"services":[]}`); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/source/upload", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer test-token")
	// Mark the body as repeatable so peekFirstMultipartFieldName can clone it.
	bodyBytes := append([]byte{}, body.Bytes()...)
	req.GetBody = func() (io.ReadCloser, error) {
		return ioNopCloser(bytes.NewReader(bodyBytes)), nil
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 MULTIPART_FIELD_ORDER, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if code, _ := resp["code"].(string); code != "MULTIPART_FIELD_ORDER" {
		t.Errorf("expected code MULTIPART_FIELD_ORDER, got %q", code)
	}
}

// EC-5 (negative control) — services_json first + bundle second is accepted.
// The downstream validation may still fail (invalid bundle/services contents)
// but the order gate MUST NOT trigger.
func TestSourceUpload_ServicesJSONFirstFieldOrder_NotRejectedByOrderGate(t *testing.T) {
	t.Setenv("LOOPZERO_ADMIN_API", "true")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	router := server.NewRouter(t61BaseCfg(logger))

	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	if err := w.WriteField("services_json", `{"version":1,"services":[]}`); err != nil {
		t.Fatal(err)
	}
	part, err := w.CreateFormFile("bundle", "bundle.git")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("PACK")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/source/upload", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer test-token")
	bodyBytes := append([]byte{}, body.Bytes()...)
	req.GetBody = func() (io.ReadCloser, error) {
		return ioNopCloser(bytes.NewReader(bodyBytes)), nil
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code == http.StatusBadRequest {
		var resp map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if code, _ := resp["code"].(string); code == "MULTIPART_FIELD_ORDER" {
			t.Fatalf("expected order gate to pass, got MULTIPART_FIELD_ORDER body=%s", rec.Body.String())
		}
	}
}
