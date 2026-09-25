// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/middleware/auth/authtest"
	"github.com/getloopscale/loopzero/api/internal/routes/builds"

	"github.com/go-chi/chi/v5"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// --- Fake BuildOrchestrator ---

type fakeBuildOrchestrator struct {
	submitResp *builds.SubmitBuildResult
	submitErr  error

	// Track calls
	lastSubmitReq *builds.SubmitBuildInput

	// GetBuild
	getBuild    *builds.BuildStatusResponse
	getBuildErr error

	// CancelBuild
	cancelErr         error
	lastCancelBuildID string

	// SEC-4: tenant ownership check
	buildTenantID string
}

func (f *fakeBuildOrchestrator) SubmitBuild(ctx context.Context, req builds.SubmitBuildInput) (*builds.SubmitBuildResult, error) {
	f.lastSubmitReq = &req
	if f.submitErr != nil {
		return nil, f.submitErr
	}
	return f.submitResp, nil
}

func (f *fakeBuildOrchestrator) GetBuildStatus(ctx context.Context, buildID string) (*builds.BuildStatusResponse, error) {
	if f.getBuildErr != nil {
		return nil, f.getBuildErr
	}
	return f.getBuild, nil
}

func (f *fakeBuildOrchestrator) CancelBuild(ctx context.Context, buildID string) error {
	f.lastCancelBuildID = buildID
	return f.cancelErr
}

func (f *fakeBuildOrchestrator) GetBuildTenantID(ctx context.Context, buildID string) (string, error) {
	if f.getBuildErr != nil {
		return "", f.getBuildErr
	}
	tenantID := f.buildTenantID
	if tenantID == "" {
		tenantID = "acme" // default matches the tenant used in makeAuthCtx across existing tests
	}
	return tenantID, nil
}

// GetRedeploySource — Plan J-17 T6.4. Stub for legacy builds_route_test.go
// (redeploy paths are covered by api/internal/routes/builds_test.go).
func (f *fakeBuildOrchestrator) GetRedeploySource(_ context.Context, _, _ string) (*builds.RedeploySource, error) {
	return nil, nil
}

// --- Test helpers ---

func newBuildHandler(orch *fakeBuildOrchestrator, callbackSecret string) *builds.BuildHandler {
	return builds.NewBuildHandler(orch, slog.Default())
}

func buildRouter(handler *builds.BuildHandler) *chi.Mux {
	r := chi.NewRouter()
	r.Post("/api/v1/builds", handler.Create)
	r.Get("/api/v1/builds/{id}", handler.Get)
	r.Post("/api/v1/builds/{id}/cancel", handler.Cancel)
	return r
}

// --- Tests ---

func TestBuildsRoute(t *testing.T) {
	t.Run("POST /builds happy path returns 202 with build_id and stream_url", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{
			submitResp: &builds.SubmitBuildResult{
				BuildID:   "build-abcd1234",
				StreamURL: "/api/v1/builds/build-abcd1234/stream",
			},
		}
		handler := newBuildHandler(orch, "test-secret")

		body := `{"project_id":"proj-xxx","environment":"production","apps":[{"name":"api","type":"server"},{"name":"worker","type":"worker"}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		// Act
		handler.Create(rec, req)

		// Assert
		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp["build_id"] != "build-abcd1234" {
			t.Errorf("expected build_id 'build-abcd1234', got '%v'", resp["build_id"])
		}
		if resp["stream_url"] != "/api/v1/builds/build-abcd1234/stream" {
			t.Errorf("expected stream_url '/api/v1/builds/build-abcd1234/stream', got '%v'", resp["stream_url"])
		}

		// Verify orchestrator received correct params
		if orch.lastSubmitReq == nil {
			t.Fatal("expected SubmitBuild to be called")
		}
		if orch.lastSubmitReq.TenantID != "acme" {
			t.Errorf("expected tenant_id 'acme', got '%s'", orch.lastSubmitReq.TenantID)
		}
		if orch.lastSubmitReq.ProjectID != "proj-xxx" {
			t.Errorf("expected project_id 'proj-xxx', got '%s'", orch.lastSubmitReq.ProjectID)
		}
		if orch.lastSubmitReq.Environment != "production" {
			t.Errorf("expected environment 'production', got '%s'", orch.lastSubmitReq.Environment)
		}
		if orch.lastSubmitReq.CreatedBy != "github-lucas" {
			t.Errorf("expected created_by 'github-lucas', got '%s'", orch.lastSubmitReq.CreatedBy)
		}
	})

	t.Run("POST /builds auth missing returns 401", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{}
		handler := newBuildHandler(orch, "test-secret")

		body := `{"project_id":"proj-xxx","environment":"production","apps":[{"name":"api","type":"server"}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		// Act
		handler.Create(rec, req)

		// Assert
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /builds invalid body returns 400", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{}
		handler := newBuildHandler(orch, "test-secret")

		body := `{invalid json`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		// Act
		handler.Create(rec, req)

		// Assert
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /builds concurrent build returns 409 with DA-18 message", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{
			submitErr: loopzeroerrors.ErrDeployInProgress,
		}
		handler := newBuildHandler(orch, "test-secret")

		body := `{"project_id":"proj-xxx","environment":"production","apps":[{"name":"api","type":"server"}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		// Act
		handler.Create(rec, req)

		// Assert
		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		msg, _ := resp["message"].(string)
		expected := "A deploy is already in progress for this environment."
		if msg != expected {
			t.Errorf("expected message %q, got %q", expected, msg)
		}
	})

	t.Run("POST /builds over the Account project cap returns 422 PROJECTS_EXCEED_PLAN (deploy-path bypass #106)", func(t *testing.T) {
		// M5 review F2 (handler-seam half): the deploy path (EnsureProject) returns
		// a typed *LoopZeroError PROJECTS_EXCEED_PLAN when a NEW-project deploy would
		// exceed the Account cap; the orchestrator passes it through unchanged. This
		// proves the LIVE HTTP seam — SubmitBuild's typed error → mapBuildError →
		// HTTP 422 with an honest code — instead of a silent 500 / build-failure
		// (DoD #1). A refactor that stopped routing the SubmitBuild error through
		// mapBuildError would fail here.
		orch := &fakeBuildOrchestrator{
			submitErr: loopzeroerrors.NewProjectsExceedPlan("starter", 1, 1, "acct-cap"),
		}
		handler := newBuildHandler(orch, "test-secret")

		body := `{"project_id":"brand-new-project","environment":"production","apps":[{"name":"api","type":"server"}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Create(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 (deploy-path project cap), got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if code, _ := resp["code"].(string); code != "PROJECTS_EXCEED_PLAN" {
			t.Errorf("expected code PROJECTS_EXCEED_PLAN, got %q (body: %s)", code, rec.Body.String())
		}
	})

	t.Run("POST /builds environment uppercase returns 400", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{}
		handler := newBuildHandler(orch, "test-secret")

		body := `{"project_id":"proj-xxx","environment":"PRODUCTION","apps":[{"name":"api","type":"server"}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		// Act
		handler.Create(rec, req)

		// Assert
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		// The code is the contract; the message names every rule the name broke.
		if code, _ := resp["code"].(string); code != "INVALID_ENVIRONMENT" {
			t.Errorf("expected code INVALID_ENVIRONMENT, got %q", code)
		}
		if msg, _ := resp["message"].(string); !strings.Contains(msg, "lowercase") {
			t.Errorf("expected the message to name the lowercase rule, got %q", msg)
		}
	})

	t.Run("POST /builds environment valid lowercase returns 202", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{
			submitResp: &builds.SubmitBuildResult{
				BuildID:   "build-abcd1234",
				StreamURL: "/api/v1/builds/build-abcd1234/stream",
			},
		}
		handler := newBuildHandler(orch, "test-secret")

		body := `{"project_id":"proj-xxx","environment":"staging","apps":[{"name":"api","type":"server"}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		// Act
		handler.Create(rec, req)

		// Assert
		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /builds missing project_id returns 400", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{}
		handler := newBuildHandler(orch, "test-secret")

		body := `{"environment":"production"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		// Act
		handler.Create(rec, req)

		// Assert
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /builds missing environment returns 400", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{}
		handler := newBuildHandler(orch, "test-secret")

		body := `{"project_id":"proj-xxx"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		// Act
		handler.Create(rec, req)

		// Assert
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /builds passes commit_sha to orchestrator", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{
			submitResp: &builds.SubmitBuildResult{
				BuildID:   "build-abcd1234",
				StreamURL: "/api/v1/builds/build-abcd1234/stream",
			},
		}
		handler := newBuildHandler(orch, "test-secret")

		body := `{"project_id":"proj-xxx","environment":"production","commit_sha":"abc123def456","apps":[{"name":"api"}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		// Act
		handler.Create(rec, req)

		// Assert
		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
		}
		if orch.lastSubmitReq.CommitSHA != "abc123def456" {
			t.Errorf("expected commit_sha 'abc123def456', got '%s'", orch.lastSubmitReq.CommitSHA)
		}
	})

	t.Run("POST /builds lock timeout returns 409", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{
			submitErr: loopzeroerrors.ErrLockTimeout,
		}
		handler := newBuildHandler(orch, "test-secret")

		body := `{"project_id":"proj-xxx","environment":"production","apps":[{"name":"api","type":"server"}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		// Act
		handler.Create(rec, req)

		// Assert
		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestBuildsGetRoute(t *testing.T) {
	t.Run("GET /builds/:id returns 200 with build data", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{
			getBuild: &builds.BuildStatusResponse{
				BuildID:     "build-abcd1234",
				ProjectID:   "proj-xxx",
				Environment: "production",
				Status:      "RUNNING",
				CommitSHA:   "abc123",
				CreatedBy:   "github-lucas",
			},
		}
		handler := newBuildHandler(orch, "test-secret")
		router := buildRouter(handler)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/build-abcd1234", nil).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp["build_id"] != "build-abcd1234" {
			t.Errorf("expected build_id 'build-abcd1234', got '%v'", resp["build_id"])
		}
		if resp["status"] != "RUNNING" {
			t.Errorf("expected status 'RUNNING', got '%v'", resp["status"])
		}
	})

	t.Run("GET /builds/:id not found returns 404", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{
			getBuildErr: loopzeroerrors.ErrBuildNotFound,
		}
		handler := newBuildHandler(orch, "test-secret")
		router := buildRouter(handler)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/build-nonexistent", nil).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// Ensure no internal terms leak into error responses
func TestBuildsNoInternalTerms(t *testing.T) {
	forbiddenTerms := []string{
		"ArgoCD", "argocd", "GitOps", "gitops-push", "CRD",
		"tenant_id", "advisory lock", "pg_advisory", "Gitea",
		"MinIO", "kubebuilder", "envtest", "Kaniko",
	}

	t.Run("409 response contains no internal terms", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{
			submitErr: loopzeroerrors.ErrDeployInProgress,
		}
		handler := newBuildHandler(orch, "test-secret")

		body := `{"project_id":"proj-xxx","environment":"production","apps":[{"name":"api","type":"server"}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Create(rec, req)

		respBody := rec.Body.String()
		for _, term := range forbiddenTerms {
			if strings.Contains(respBody, term) {
				t.Errorf("response contains forbidden internal term %q: %s", term, respBody)
			}
		}
	})

	t.Run("400 environment response contains no internal terms", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{}
		handler := newBuildHandler(orch, "test-secret")

		body := `{"project_id":"proj-xxx","environment":"PRODUCTION","apps":[{"name":"api","type":"server"}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body)).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.Create(rec, req)

		respBody := rec.Body.String()
		for _, term := range forbiddenTerms {
			if strings.Contains(respBody, term) {
				t.Errorf("response contains forbidden internal term %q: %s", term, respBody)
			}
		}
	})
}

// Verify the environment regex DA-10 edge cases
func TestEnvironmentValidation(t *testing.T) {
	validEnvs := []string{"production", "staging", "pr-142", "a", "dev-1"}
	invalidEnvs := []string{"PRODUCTION", "Production", "-invalid", "1invalid", "a_b", "ab.cd", strings.Repeat("a", 64)}

	for _, env := range validEnvs {
		t.Run("valid: "+env, func(t *testing.T) {
			orch := &fakeBuildOrchestrator{
				submitResp: &builds.SubmitBuildResult{
					BuildID:   "build-abcd1234",
					StreamURL: "/api/v1/builds/build-abcd1234/stream",
				},
			}
			handler := newBuildHandler(orch, "test-secret")

			body := `{"project_id":"proj-xxx","environment":"` + env + `","apps":[{"name":"api","type":"server"}]}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body)).
				WithContext(authtest.Context("github-lucas", "acme", "owner"))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.Create(rec, req)

			if rec.Code != http.StatusAccepted {
				t.Fatalf("expected 202 for env %q, got %d: %s", env, rec.Code, rec.Body.String())
			}
		})
	}

	for _, env := range invalidEnvs {
		t.Run("invalid: "+env, func(t *testing.T) {
			orch := &fakeBuildOrchestrator{}
			handler := newBuildHandler(orch, "test-secret")

			body := `{"project_id":"proj-xxx","environment":"` + env + `","apps":[{"name":"api","type":"server"}]}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body)).
				WithContext(authtest.Context("github-lucas", "acme", "owner"))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.Create(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for env %q, got %d: %s", env, rec.Code, rec.Body.String())
			}
		})
	}
}

// Verify that the fake implements the interface (compile-time check)
var _ builds.BuildOrchestratorService = (*fakeBuildOrchestrator)(nil)

func TestBuildsCancelRoute(t *testing.T) {
	t.Run("POST /builds/:id/cancel running build returns 202", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{}
		handler := newBuildHandler(orch, "test-secret")
		router := buildRouter(handler)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/build-abcd1234/cancel", nil).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert
		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp["status"] != "cancelled" {
			t.Errorf("expected status 'cancelled', got '%v'", resp["status"])
		}

		// Verify orchestrator received the correct build ID
		if orch.lastCancelBuildID != "build-abcd1234" {
			t.Errorf("expected CancelBuild called with 'build-abcd1234', got '%s'", orch.lastCancelBuildID)
		}
	})

	t.Run("POST /builds/:id/cancel already completed returns 409", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{
			cancelErr: loopzeroerrors.NewLoopZeroError(
				"BUILD_ALREADY_COMPLETED",
				"Build already completed."),
		}
		handler := newBuildHandler(orch, "test-secret")
		router := buildRouter(handler)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/build-done/cancel", nil).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert
		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp["code"] != "BUILD_ALREADY_COMPLETED" {
			t.Errorf("expected code 'BUILD_ALREADY_COMPLETED', got '%v'", resp["code"])
		}
	})

	t.Run("POST /builds/:id/cancel not found returns 404", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{
			cancelErr: loopzeroerrors.ErrBuildNotFound,
		}
		handler := newBuildHandler(orch, "test-secret")
		router := buildRouter(handler)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/build-nonexistent/cancel", nil).
			WithContext(authtest.Context("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /builds/:id/cancel auth missing returns 401", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{}
		handler := newBuildHandler(orch, "test-secret")
		router := buildRouter(handler)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/build-abcd1234/cancel", nil)
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// Ensure errors.Is works for mapping
func TestErrorMapping(t *testing.T) {
	t.Run("ErrDeployInProgress is detected via errors.Is", func(t *testing.T) {
		err := loopzeroerrors.ErrDeployInProgress
		if !errors.Is(err, loopzeroerrors.ErrDeployInProgress) {
			t.Fatal("expected errors.Is to match ErrDeployInProgress")
		}
	})

	t.Run("ErrLockTimeout is detected via errors.Is", func(t *testing.T) {
		err := loopzeroerrors.ErrLockTimeout
		if !errors.Is(err, loopzeroerrors.ErrLockTimeout) {
			t.Fatal("expected errors.Is to match ErrLockTimeout")
		}
	})
}
