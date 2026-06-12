// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Plan v1.2 T1.2 (cutover-deep-review-hardening): integration test that
// POST /api/v1/source/upload returns HTTP 400 with code
// INVALID_TENANT_OR_PROJECT when the source store rejects tenant/project IDs
// that fail DNS-1123 validation. Proves errors.Is wiring in
// internal/routes/source.go is intact end-to-end through the chi router.

package unit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/loopzero/loopzero/api/internal/routes"
	"github.com/loopzero/loopzero/api/internal/server"
	"github.com/loopzero/loopzero/api/internal/source"
)

// stubInvalidProjectSourceStore mimics what GiteaSourceStore returns when
// sourceRepoName rejects the supplied tenant/project IDs.
type stubInvalidProjectSourceStore struct{}

func (s *stubInvalidProjectSourceStore) PushBundle(_ context.Context, _, projectID string, _ io.Reader) (source.PushResult, error) {
	return source.PushResult{}, fmt.Errorf("gitea source: %w: projectID must match DNS-1123 (got %q)",
		source.ErrInvalidTenantOrProject, projectID)
}

func newValidSourceUploadReq(t *testing.T, projectName string) *http.Request {
	t.Helper()
	_ = projectName // hardcoded "services-bundle" downstream — irrelevant to T1.2 wire test
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)

	servicesJSON := `{
		"version": 1,
		"services": [
			{
				"name": "agent",
				"runtime": "python",
				"port": 8001,
				"proxy": "/api/agent",
				"dev": "uvicorn main:app",
				"start": "uvicorn main:app --workers 4",
				"healthcheck": "/health",
				"cors": false,
				"passSetCookie": false
			}
		]
	}`
	if err := w.WriteField("services_json", servicesJSON); err != nil {
		t.Fatal(err)
	}
	part, err := w.CreateFormFile("bundle", "bundle.git")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("PACK fake bundle bytes")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/source/upload", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer test-token")
	return req
}

// TestSourceUpload_PathTraversalTenant_Returns400 — Plan v1.2 T1.2.
// When the source store rejects tenant/project IDs that fail DNS-1123,
// the route MUST return HTTP 400 with INVALID_TENANT_OR_PROJECT code so
// the user sees an actionable validation error (not a generic 500).
func TestSourceUpload_PathTraversalTenant_Returns400(t *testing.T) {
	t.Setenv("LOOPZERO_ADMIN_API", "true")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	cfg := server.Config{
		Port:                 "0",
		Logger:               logger,
		DB:                   &stubDBHealthChecker{},
		TokenValidator:       &stubTokenValidator{},
		MemberStore:          &stubMemberStore{},
		ProjectStore:         &stubProjectStore{},
		TenantCRMgr:          &stubTenantCRManager{},
		SourceStore:          &stubInvalidProjectSourceStore{},
		IdempotencyStore:     &stubIdempotencyStore{},
		EventStore:           &stubEventStore{},
		BuildStoreReader:     &stubBuildReader{},
		BootstrapStore:       &stubBootstrapStore{},
		BootstrapSecret:      "test-bootstrap-secret",
		BuildHandler:         routes.NewBuildHandler(nil, logger),
		LogsHandler:          routes.NewLogsHandler(nil, nil),
		RollbackHandler:      routes.NewRollbackHandler(nil, nil, "test.local"),
		StatusHandler:        routes.NewStatusHandler(nil, nil, nil, "test.local", ""),
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

	router := server.NewRouter(cfg)
	// Project name that would survive services.json regex validation but be
	// rejected at the source store boundary — proves the route-level guard
	// fires regardless of upstream validators.
	req := newValidSourceUploadReq(t, "proj-with-traversal")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (INVALID_TENANT_OR_PROJECT), got %d body=%s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not JSON: %v", err)
	}
	code, _ := body["code"].(string)
	if code != "INVALID_TENANT_OR_PROJECT" {
		t.Errorf("expected code INVALID_TENANT_OR_PROJECT, got %q (body=%v)", code, body)
	}
}
