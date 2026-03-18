package unit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/routes"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// --- Fake BuildOrchestrator ---

type fakeBuildOrchestrator struct {
	submitResp *routes.SubmitBuildResult
	submitErr  error

	// Track calls
	lastSubmitReq *routes.SubmitBuildInput

	// GetBuild
	getBuild    *routes.BuildStatusResponse
	getBuildErr error

	// OnBuildCompleted
	onCompletedErr   error
	onCompletedCalls int
	lastCallbackReq  *routes.BuildCallbackInput
	lastCallbackID   string

	// For duplicate detection
	buildTerminal bool

	// CancelBuild
	cancelErr          error
	lastCancelBuildID  string
}

func (f *fakeBuildOrchestrator) SubmitBuild(ctx context.Context, req routes.SubmitBuildInput) (*routes.SubmitBuildResult, error) {
	f.lastSubmitReq = &req
	if f.submitErr != nil {
		return nil, f.submitErr
	}
	return f.submitResp, nil
}

func (f *fakeBuildOrchestrator) GetBuildStatus(ctx context.Context, buildID string) (*routes.BuildStatusResponse, error) {
	if f.getBuildErr != nil {
		return nil, f.getBuildErr
	}
	return f.getBuild, nil
}

func (f *fakeBuildOrchestrator) OnBuildCompleted(ctx context.Context, buildID string, result routes.BuildCallbackInput) error {
	f.onCompletedCalls++
	f.lastCallbackID = buildID
	f.lastCallbackReq = &result
	if f.onCompletedErr != nil {
		return f.onCompletedErr
	}
	return nil
}

func (f *fakeBuildOrchestrator) CancelBuild(ctx context.Context, buildID string) error {
	f.lastCancelBuildID = buildID
	return f.cancelErr
}

func (f *fakeBuildOrchestrator) IsBuildTerminal(ctx context.Context, buildID string) (bool, error) {
	if f.getBuildErr != nil {
		return false, f.getBuildErr
	}
	return f.buildTerminal, nil
}

// --- Test helpers ---

func newBuildHandler(orch *fakeBuildOrchestrator, callbackSecret string) *routes.BuildHandler {
	return routes.NewBuildHandler(orch, callbackSecret, slog.Default(), context.Background())
}

func buildRouter(handler *routes.BuildHandler) *chi.Mux {
	r := chi.NewRouter()
	r.Post("/api/v1/builds", handler.Create)
	r.Get("/api/v1/builds/{id}", handler.Get)
	r.Post("/api/v1/builds/{id}/cancel", handler.Cancel)
	r.Post("/api/v1/builds/{id}/callback", handler.Callback)
	return r
}

// --- Tests ---

func TestBuildsRoute(t *testing.T) {
	t.Run("POST /builds happy path returns 202 with build_id and stream_url", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{
			submitResp: &routes.SubmitBuildResult{
				BuildID:   "build-abcd1234",
				StreamURL: "/api/v1/builds/build-abcd1234/stream",
			},
		}
		handler := newBuildHandler(orch, "test-secret")

		body := `{"project_id":"proj-xxx","environment":"production","apps":[{"name":"api","type":"server"},{"name":"worker","type":"worker"}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body)).
			WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
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

		body := `{"project_id":"proj-xxx","environment":"production"}`
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
			WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
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

		body := `{"project_id":"proj-xxx","environment":"production"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body)).
			WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
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

	t.Run("POST /builds environment uppercase returns 400", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{}
		handler := newBuildHandler(orch, "test-secret")

		body := `{"project_id":"proj-xxx","environment":"PRODUCTION"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body)).
			WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
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
		msg, _ := resp["message"].(string)
		if msg != "Environment names must be lowercase." {
			t.Errorf("expected 'Environment names must be lowercase.', got %q", msg)
		}
	})

	t.Run("POST /builds environment valid lowercase returns 202", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{
			submitResp: &routes.SubmitBuildResult{
				BuildID:   "build-abcd1234",
				StreamURL: "/api/v1/builds/build-abcd1234/stream",
			},
		}
		handler := newBuildHandler(orch, "test-secret")

		body := `{"project_id":"proj-xxx","environment":"staging"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body)).
			WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
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
			WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
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
			WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
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
			submitResp: &routes.SubmitBuildResult{
				BuildID:   "build-abcd1234",
				StreamURL: "/api/v1/builds/build-abcd1234/stream",
			},
		}
		handler := newBuildHandler(orch, "test-secret")

		body := `{"project_id":"proj-xxx","environment":"production","commit_sha":"abc123def456","apps":[{"name":"api"}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body)).
			WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
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

		body := `{"project_id":"proj-xxx","environment":"production"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body)).
			WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
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
			getBuild: &routes.BuildStatusResponse{
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
			WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
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
			WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestBuildsCallbackRoute(t *testing.T) {
	t.Run("POST /builds/:id/callback happy path returns 202", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{
			getBuild: &routes.BuildStatusResponse{
				BuildID: "build-abcd1234",
				Status:  "RUNNING",
			},
		}
		handler := newBuildHandler(orch, "test-callback-secret")
		router := buildRouter(handler)

		body := `{"status":"succeeded","apps":[{"name":"api","image_digest":"sha256:abc123","url":"https://api.example.com","duration_ms":45000}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/build-abcd1234/callback", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-callback-secret")
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert
		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /builds/:id/callback invalid auth returns 401", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{}
		handler := newBuildHandler(orch, "test-callback-secret")
		router := buildRouter(handler)

		body := `{"status":"succeeded","apps":[{"name":"api","image_digest":"sha256:abc123","url":"https://api.example.com","duration_ms":45000}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/build-abcd1234/callback", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer wrong-secret")
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /builds/:id/callback missing auth returns 401", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{}
		handler := newBuildHandler(orch, "test-callback-secret")
		router := buildRouter(handler)

		body := `{"status":"succeeded","apps":[{"name":"api","image_digest":"sha256:abc123","url":"https://api.example.com","duration_ms":45000}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/build-abcd1234/callback", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /builds/:id/callback build not found returns 404", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{
			getBuildErr: loopzeroerrors.ErrBuildNotFound,
		}
		handler := newBuildHandler(orch, "test-callback-secret")
		router := buildRouter(handler)

		body := `{"status":"succeeded","apps":[{"name":"api","image_digest":"sha256:abc123","url":"https://api.example.com","duration_ms":45000}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/build-nonexistent/callback", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-callback-secret")
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /builds/:id/callback empty apps with succeeded returns 400", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{
			getBuild: &routes.BuildStatusResponse{
				BuildID: "build-abcd1234",
				Status:  "RUNNING",
			},
		}
		handler := newBuildHandler(orch, "test-callback-secret")
		router := buildRouter(handler)

		body := `{"status":"succeeded","apps":[]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/build-abcd1234/callback", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-callback-secret")
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		msg, _ := resp["message"].(string)
		if !strings.Contains(msg, "at least one app") {
			t.Errorf("expected message about at least one app, got %q", msg)
		}
	})

	t.Run("POST /builds/:id/callback duplicate returns 200 no-op", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{
			buildTerminal: true,
			getBuild: &routes.BuildStatusResponse{
				BuildID: "build-abcd1234",
				Status:  "SUCCEEDED",
			},
		}
		handler := newBuildHandler(orch, "test-callback-secret")
		router := buildRouter(handler)

		body := `{"status":"succeeded","apps":[{"name":"api","image_digest":"sha256:abc123","url":"https://api.example.com","duration_ms":45000}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/build-abcd1234/callback", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-callback-secret")
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify OnBuildCompleted was NOT called
		if orch.onCompletedCalls != 0 {
			t.Errorf("expected OnBuildCompleted to not be called for duplicate, got %d calls", orch.onCompletedCalls)
		}
	})

	t.Run("POST /builds/:id/callback unknown status falls back to failed with 202", func(t *testing.T) {
		// Arrange — unknown statuses are treated as "failed" instead of returning 400.
		// The callback endpoint must never leave builds stuck in RUNNING. (F-01)
		orch := &fakeBuildOrchestrator{}
		handler := newBuildHandler(orch, "test-callback-secret")
		router := buildRouter(handler)

		body := `{"status":"unknown","apps":[{"name":"api","image_digest":"sha256:abc123","url":"https://api.example.com","duration_ms":45000}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/build-abcd1234/callback", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-callback-secret")
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert — 202 accepted, not 400
		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
		}

		// Wait for goroutine
		deadline := time.After(2 * time.Second)
		for {
			if orch.onCompletedCalls > 0 {
				break
			}
			select {
			case <-deadline:
				t.Fatal("expected OnBuildCompleted to be called for unknown status")
			default:
				time.Sleep(10 * time.Millisecond)
			}
		}

		if orch.lastCallbackReq.Status != "failed" {
			t.Errorf("expected fallback status 'failed', got %q", orch.lastCallbackReq.Status)
		}
		// Verify error message captures the ORIGINAL invalid status, not "failed"
		if !strings.Contains(orch.lastCallbackReq.Error, "unknown") {
			t.Errorf("error message should contain original status 'unknown', got %q", orch.lastCallbackReq.Error)
		}
	})

	t.Run("POST /builds/:id/callback orchestrator error is logged not silenced", func(t *testing.T) {
		// Arrange
		var mu sync.Mutex
		var loggedMessages []string

		// Custom handler that captures log records
		captureHandler := slog.NewTextHandler(&logWriter{
			mu:       &mu,
			messages: &loggedMessages,
		}, &slog.HandlerOptions{Level: slog.LevelError})
		logger := slog.New(captureHandler)

		orch := &fakeBuildOrchestrator{
			onCompletedErr: errors.New("gitops push failed"),
		}
		handler := routes.NewBuildHandler(orch, "test-callback-secret", logger, context.Background())
		router := buildRouter(handler)

		body := `{"status":"succeeded","apps":[{"name":"api","image_digest":"sha256:abc123","url":"https://api.example.com","duration_ms":45000}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/build-abcd1234/callback", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-callback-secret")
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert — response is still 202 (fire-and-forget)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
		}

		// Wait for goroutine to complete and log the error
		deadline := time.After(2 * time.Second)
		for {
			mu.Lock()
			found := false
			for _, msg := range loggedMessages {
				if strings.Contains(msg, "build completion failed") && strings.Contains(msg, "gitops push failed") {
					found = true
					break
				}
			}
			mu.Unlock()
			if found {
				break
			}
			select {
			case <-deadline:
				mu.Lock()
				t.Fatalf("expected error to be logged with 'build completion failed', got logs: %v", loggedMessages)
				mu.Unlock()
			default:
				time.Sleep(10 * time.Millisecond)
			}
		}
	})

	t.Run("POST /builds/:id/callback malformed JSON falls back to failed with 202", func(t *testing.T) {
		// Arrange — callback endpoint must NEVER return 400 for malformed payloads.
		// If the workflow terminated and sent a callback, the build must transition
		// to a terminal state regardless of payload quality. (F-01 fix)
		orch := &fakeBuildOrchestrator{}
		handler := newBuildHandler(orch, "test-callback-secret")
		router := buildRouter(handler)

		body := `{invalid json`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/build-abcd1234/callback", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-callback-secret")
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert — 202 accepted, not 400
		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202 (fallback to failed), got %d: %s", rec.Code, rec.Body.String())
		}

		// Wait for goroutine to call OnBuildCompleted
		deadline := time.After(2 * time.Second)
		for {
			if orch.onCompletedCalls > 0 {
				break
			}
			select {
			case <-deadline:
				t.Fatal("expected OnBuildCompleted to be called for malformed callback")
			default:
				time.Sleep(10 * time.Millisecond)
			}
		}

		// The fallback should send status=failed with a descriptive error
		if orch.lastCallbackReq.Status != "failed" {
			t.Errorf("expected fallback status 'failed', got %q", orch.lastCallbackReq.Status)
		}
		if orch.lastCallbackReq.Error == "" {
			t.Error("expected fallback error message to be non-empty")
		}
	})

	t.Run("POST /builds/:id/callback truncated Argo JSON falls back to failed with 202", func(t *testing.T) {
		// Arrange — this reproduces the exact payload that Argo sends when
		// {{workflow.failures}} contains nested escaped JSON that gets truncated.
		// The real payload observed in production:
		orch := &fakeBuildOrchestrator{}
		handler := newBuildHandler(orch, "test-callback-secret")
		router := buildRouter(handler)

		// Simulates the truncated Argo callback payload (key chars cut mid-string)
		body := `{"status":"failed","error":"Build pipeline failed: \"[{\"displayName\":\"validate-workspace\",\"message\":\"\",\"templateName\":\"build-pipeline\",\"phase\":\"Failed\",\"podName\":\"loopzero-build-build-e49111ce-sjwxf\",\"finishedAt\":\"2026-0","apps":[{"name":"e2e-project","image_digest":"k3d-loopzero-registry:5000/acme/e2e-project:2f62fcf8de347b28227b4df773bfa509515a9f02"}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/build-abcd1234/callback", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-callback-secret")
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert — must accept, not reject
		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202 for truncated Argo payload, got %d: %s", rec.Code, rec.Body.String())
		}

		// Wait for goroutine
		deadline := time.After(2 * time.Second)
		for {
			if orch.onCompletedCalls > 0 {
				break
			}
			select {
			case <-deadline:
				t.Fatal("expected OnBuildCompleted to be called for truncated Argo payload")
			default:
				time.Sleep(10 * time.Millisecond)
			}
		}

		if orch.lastCallbackReq.Status != "failed" {
			t.Errorf("expected status 'failed', got %q", orch.lastCallbackReq.Status)
		}
	})

	t.Run("POST /builds/:id/callback empty body falls back to failed with 202", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{}
		handler := newBuildHandler(orch, "test-callback-secret")
		router := buildRouter(handler)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/build-abcd1234/callback", bytes.NewBufferString(""))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-callback-secret")
		rec := httptest.NewRecorder()

		// Act
		router.ServeHTTP(rec, req)

		// Assert
		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202 for empty body, got %d: %s", rec.Code, rec.Body.String())
		}

		deadline := time.After(2 * time.Second)
		for {
			if orch.onCompletedCalls > 0 {
				break
			}
			select {
			case <-deadline:
				t.Fatal("expected OnBuildCompleted to be called for empty body callback")
			default:
				time.Sleep(10 * time.Millisecond)
			}
		}

		if orch.lastCallbackReq.Status != "failed" {
			t.Errorf("expected fallback status 'failed', got %q", orch.lastCallbackReq.Status)
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

		body := `{"project_id":"proj-xxx","environment":"production"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body)).
			WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
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

		body := `{"project_id":"proj-xxx","environment":"PRODUCTION"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body)).
			WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
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
				submitResp: &routes.SubmitBuildResult{
					BuildID:   "build-abcd1234",
					StreamURL: "/api/v1/builds/build-abcd1234/stream",
				},
			}
			handler := newBuildHandler(orch, "test-secret")

			body := `{"project_id":"proj-xxx","environment":"` + env + `"}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body)).
				WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
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

			body := `{"project_id":"proj-xxx","environment":"` + env + `"}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", bytes.NewBufferString(body)).
				WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.Create(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for env %q, got %d: %s", env, rec.Code, rec.Body.String())
			}
		})
	}
}

// logWriter captures written log output for test assertions.
type logWriter struct {
	mu       *sync.Mutex
	messages *[]string
}

func (w *logWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	*w.messages = append(*w.messages, string(p))
	return len(p), nil
}

// Verify that the fake implements the interface (compile-time check)
var _ routes.BuildOrchestratorService = (*fakeBuildOrchestrator)(nil)

func TestBuildsCancelRoute(t *testing.T) {
	t.Run("POST /builds/:id/cancel running build returns 202", func(t *testing.T) {
		// Arrange
		orch := &fakeBuildOrchestrator{}
		handler := newBuildHandler(orch, "test-secret")
		router := buildRouter(handler)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/build-abcd1234/cancel", nil).
			WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
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
				"Build already completed.",
				409,
			),
		}
		handler := newBuildHandler(orch, "test-secret")
		router := buildRouter(handler)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/builds/build-done/cancel", nil).
			WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
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
			WithContext(makeAuthCtx("github-lucas", "acme", "owner"))
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
