// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Plan v1.2 T6.1 (cutover-deep-review-hardening) — multipart per-field
// limits + field-order tests for POST /api/v1/source/upload.

package server_test

import (
	"bytes"
	"encoding/json"
	"github.com/getloopscale/loopzero/api/internal/routes/argocd"
	"github.com/getloopscale/loopzero/api/internal/routes/builds"
	"github.com/getloopscale/loopzero/api/internal/routes/environments"
	"github.com/getloopscale/loopzero/api/internal/routes/logs"
	"github.com/getloopscale/loopzero/api/internal/routes/rollback"
	"github.com/getloopscale/loopzero/api/internal/routes/status"
	"github.com/getloopscale/loopzero/api/internal/routes/teams"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/repositories/repositorieshttp"
	"github.com/getloopscale/loopzero/api/internal/routes/audit"
	"github.com/getloopscale/loopzero/api/internal/routes/domains"
	"github.com/getloopscale/loopzero/api/internal/server"
)

// postUploadThroughServer sends the multipart body to a real listening
// server, so the handler sees the request exactly as production traffic
// arrives (streamed, no GetBody).
func postUploadThroughServer(t *testing.T, h http.Handler, body *bytes.Buffer, contentType string) (int, []byte) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/source/upload", io.NopCloser(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, respBody
}

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
		EventStore:           &stubEventStore{},
		BuildStoreReader:     &stubBuildReader{},
		BootstrapStore:       &stubBootstrapStore{},
		BootstrapSecret:      "test-bootstrap-secret",
		BuildHandler:         builds.NewBuildHandler(nil, logger),
		LogsHandler:          logs.NewLogsHandler(nil, nil),
		RollbackHandler:      rollback.NewRollbackHandler(nil, nil, "test.local"),
		StatusHandler:        status.NewStatusHandler(nil, nil, nil, "test.local", "", ""),
		BuildListHandler:     builds.NewBuildListHandler(nil),
		EnvironmentHandler:   environments.NewEnvironmentHandler(nil),
		TeamHandler:          teams.NewTeamHandler(nil),
		DomainHandler:        domains.NewDomainHandler(nil, nil, nil, "test.local"),
		RepositoryHandler:    repositorieshttp.NewRepositoryHandler(nil),
		AuditHandler:         audit.NewAuditHandler(nil),
		ArgoCallbackHandler:  argocd.NewArgoCallbackHandler(&stubReleaseActivator{}, logger, "test-secret"),
		LogStreamHandler:     logs.NewLogStreamHandler(nil, nil, logs.LogStreamHandlerOpts{}),
		GitHubWebhookHandler: &stubGitHubWebhookHandler{},
		TenantDeletionStore:  &stubTenantDeletionStore{},
		Guards:               server.RouteGuards{IdempotencyStore: &stubIdempotencyStore{}},
		Admin:                server.AdminConfig{APIEnabled: true, Evictor: &stubTenantEvictor{}},
	}
}

// EC-4 — services_json field exceeding 256 KB is rejected with HTTP 413 +
// code SERVICES_JSON_TOO_LARGE.
func TestSourceUpload_ServicesJSONExceeds256KB_Returns413(t *testing.T) {
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
	// Through a real server: net/http never gives a server-side request a
	// GetBody, so a test that sets one by hand certifies a path production
	// traffic cannot reach.
	code, respBody := postUploadThroughServer(t, router, body, w.FormDataContentType())

	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 MULTIPART_FIELD_ORDER, got %d body=%s", code, respBody)
	}
	var resp map[string]any
	if err := json.Unmarshal(respBody, &resp); err != nil {
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
	code, respBody := postUploadThroughServer(t, router, body, w.FormDataContentType())

	if code == http.StatusBadRequest {
		var resp map[string]any
		_ = json.Unmarshal(respBody, &resp)
		if c, _ := resp["code"].(string); c == "MULTIPART_FIELD_ORDER" {
			t.Fatalf("expected order gate to pass, got MULTIPART_FIELD_ORDER body=%s", respBody)
		}
	}
}
