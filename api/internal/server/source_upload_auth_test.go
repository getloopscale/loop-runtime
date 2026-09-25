// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Plan v1.2 T1.1 (cutover-deep-review-hardening): defensive regression tests
// asserting that POST /api/v1/source/upload requires JWT Bearer authentication
// via the auth middleware — NOT just handler self-check.
//
// Context: Deep review 2026-06-05 raised C1 "endpoint outside auth group".
// Architecture re-audit T0.1 (2026-06-06) confirmed FALSE POSITIVE — the
// route is registered via `registerProtectedRoutes(r, ...)` which is called
// inside `r.Group(func(r chi.Router) { r.Use(auth.Auth) ... })` and
// chi router scope inheritance correctly applies auth. This test file
// EXPLICITLY proves the protection so any future regression (e.g., moving
// registration outside the protected closure) breaks CI immediately.

package server_test

import (
	"bytes"
	"context"
	"github.com/getloopscale/loopzero/api/internal/environments/environmentshttp"
	"github.com/getloopscale/loopzero/api/internal/logs/logshttp"
	"github.com/getloopscale/loopzero/api/internal/release/argocdhttp"
	"github.com/getloopscale/loopzero/api/internal/release/rollbackhttp"
	"github.com/getloopscale/loopzero/api/internal/routes/builds"
	"github.com/getloopscale/loopzero/api/internal/status/statushttp"
	"github.com/getloopscale/loopzero/api/internal/teams/teamshttp"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/audit/audithttp"
	"github.com/getloopscale/loopzero/api/internal/domains/domainshttp"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	"github.com/getloopscale/loopzero/api/internal/repositories/repositorieshttp"
	"github.com/getloopscale/loopzero/api/internal/server"
)

// newSourceUploadMultipartReq builds a minimal valid-shape multipart request
// (services_json + bundle). Content is throwaway since the handler will
// reject at auth boundary before parsing.
func newSourceUploadMultipartReq(t *testing.T, auth string) *http.Request {
	t.Helper()
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	if err := w.WriteField("services_json", `{"version":1,"services":[]}`); err != nil {
		t.Fatal(err)
	}
	part, err := w.CreateFormFile("bundle", "bundle.git")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("fake bundle")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/source/upload", body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	return req
}

// TestSourceUpload_Unauthenticated_Returns401 — EC-baseline of Plan T1.1.
// POST without Authorization header must return 401 BEFORE reaching the
// handler. Proves the auth middleware is in the chain.
func TestSourceUpload_Unauthenticated_Returns401(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	cfg := server.Config{
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
		LogsHandler:          logshttp.NewLogsHandler(nil, nil),
		RollbackHandler:      rollbackhttp.NewRollbackHandler(nil, nil, "test.local"),
		StatusHandler:        statushttp.NewStatusHandler(nil, nil, nil, "test.local", "", ""),
		BuildListHandler:     builds.NewBuildListHandler(nil),
		EnvironmentHandler:   environmentshttp.NewEnvironmentHandler(nil),
		TeamHandler:          teamshttp.NewTeamHandler(nil),
		DomainHandler:        domainshttp.NewDomainHandler(nil, nil, nil, "test.local"),
		RepositoryHandler:    repositorieshttp.NewRepositoryHandler(nil),
		AuditHandler:         audithttp.NewAuditHandler(nil),
		ArgoCallbackHandler:  argocdhttp.NewArgoCallbackHandler(&stubReleaseActivator{}, logger, "test-secret"),
		LogStreamHandler:     logshttp.NewLogStreamHandler(nil, nil, logshttp.LogStreamHandlerOpts{}),
		GitHubWebhookHandler: &stubGitHubWebhookHandler{},
		TenantDeletionStore:  &stubTenantDeletionStore{},
		Guards:               server.RouteGuards{IdempotencyStore: &stubIdempotencyStore{}},
		Admin:                server.AdminConfig{APIEnabled: true, Evictor: &stubTenantEvictor{}},
	}

	router := server.NewRouter(cfg)
	req := newSourceUploadMultipartReq(t, "")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestSourceUpload_InvalidBearerToken_Returns401 — EC-6 (v1.1) variant.
// POST with malformed Bearer token must return 401 via the auth middleware
// JWT validation, NOT via handler self-check.
func TestSourceUpload_InvalidBearerToken_Returns401(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Use a TokenValidator that rejects everything.
	rejectingValidator := &stubRejectingTokenValidator{}

	cfg := server.Config{
		Port:                 "0",
		Logger:               logger,
		DB:                   &stubDBHealthChecker{},
		TokenValidator:       rejectingValidator,
		MemberStore:          &stubMemberStore{},
		ProjectStore:         &stubProjectStore{},
		TenantCRMgr:          &stubTenantCRManager{},
		SourceStore:          &stubSourceStore{},
		EventStore:           &stubEventStore{},
		BuildStoreReader:     &stubBuildReader{},
		BootstrapStore:       &stubBootstrapStore{},
		BootstrapSecret:      "test-bootstrap-secret",
		BuildHandler:         builds.NewBuildHandler(nil, logger),
		LogsHandler:          logshttp.NewLogsHandler(nil, nil),
		RollbackHandler:      rollbackhttp.NewRollbackHandler(nil, nil, "test.local"),
		StatusHandler:        statushttp.NewStatusHandler(nil, nil, nil, "test.local", "", ""),
		BuildListHandler:     builds.NewBuildListHandler(nil),
		EnvironmentHandler:   environmentshttp.NewEnvironmentHandler(nil),
		TeamHandler:          teamshttp.NewTeamHandler(nil),
		DomainHandler:        domainshttp.NewDomainHandler(nil, nil, nil, "test.local"),
		RepositoryHandler:    repositorieshttp.NewRepositoryHandler(nil),
		AuditHandler:         audithttp.NewAuditHandler(nil),
		ArgoCallbackHandler:  argocdhttp.NewArgoCallbackHandler(&stubReleaseActivator{}, logger, "test-secret"),
		LogStreamHandler:     logshttp.NewLogStreamHandler(nil, nil, logshttp.LogStreamHandlerOpts{}),
		GitHubWebhookHandler: &stubGitHubWebhookHandler{},
		TenantDeletionStore:  &stubTenantDeletionStore{},
		Guards:               server.RouteGuards{IdempotencyStore: &stubIdempotencyStore{}},
		Admin:                server.AdminConfig{APIEnabled: true, Evictor: &stubTenantEvictor{}},
	}

	router := server.NewRouter(cfg)
	req := newSourceUploadMultipartReq(t, "Bearer invalid-malformed-token-xxx")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// The auth middleware either rejects with 401 (invalid token claim) OR 503
	// (validator service error). Both prove the middleware was invoked;
	// 200 / 400 would prove bypass (handler reached without auth).
	if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 401 or 503 via the auth middleware (proves middleware in chain), got %d: %s",
			rec.Code, rec.Body.String())
	}
}

// TestSourceUpload_XServiceAuthHeader_DoesNotBypass — EC-6 (v1.1).
// POST with X-Service-Auth header but NO Authorization Bearer must return
// 401. The auth middleware on /source/upload must NOT have any code path
// that accepts X-Service-Auth (those are for admin endpoints in
// /api/v1/admin/* with separate middleware).
func TestSourceUpload_XServiceAuthHeader_DoesNotBypass(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	cfg := server.Config{
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
		LogsHandler:          logshttp.NewLogsHandler(nil, nil),
		RollbackHandler:      rollbackhttp.NewRollbackHandler(nil, nil, "test.local"),
		StatusHandler:        statushttp.NewStatusHandler(nil, nil, nil, "test.local", "", ""),
		BuildListHandler:     builds.NewBuildListHandler(nil),
		EnvironmentHandler:   environmentshttp.NewEnvironmentHandler(nil),
		TeamHandler:          teamshttp.NewTeamHandler(nil),
		DomainHandler:        domainshttp.NewDomainHandler(nil, nil, nil, "test.local"),
		RepositoryHandler:    repositorieshttp.NewRepositoryHandler(nil),
		AuditHandler:         audithttp.NewAuditHandler(nil),
		ArgoCallbackHandler:  argocdhttp.NewArgoCallbackHandler(&stubReleaseActivator{}, logger, "test-secret"),
		LogStreamHandler:     logshttp.NewLogStreamHandler(nil, nil, logshttp.LogStreamHandlerOpts{}),
		GitHubWebhookHandler: &stubGitHubWebhookHandler{},
		TenantDeletionStore:  &stubTenantDeletionStore{},
		Guards:               server.RouteGuards{IdempotencyStore: &stubIdempotencyStore{}},
		Admin:                server.AdminConfig{APIEnabled: true, Evictor: &stubTenantEvictor{}},
	}

	router := server.NewRouter(cfg)
	req := newSourceUploadMultipartReq(t, "")
	req.Header.Set("X-Service-Auth", "fake-admin-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("X-Service-Auth header MUST NOT bypass user auth on /source/upload; got %d: %s",
			rec.Code, rec.Body.String())
	}
}

// stubRejectingTokenValidator rejects all tokens — for invalid-bearer tests.
type stubRejectingTokenValidator struct{}

func (s *stubRejectingTokenValidator) ValidateToken(_ context.Context, _ string) (*auth.TokenValidationResult, error) {
	return nil, &validationError{msg: "invalid token"}
}

type validationError struct{ msg string }

func (e *validationError) Error() string { return e.msg }
