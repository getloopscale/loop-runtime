package routes

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// environmentRegex validates environment names (DA-10).
// Lowercase, starts with letter, alphanumeric + hyphens, max 63 chars.
var environmentRegex = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// AppInput describes a single app from the CLI request.
type AppInput struct {
	Name      string `json:"name"`
	Type      string `json:"type,omitempty"`      // "server", "worker", "frontend"
	Port      int    `json:"port,omitempty"`
	Path      string `json:"path,omitempty"`
	Framework string `json:"framework,omitempty"` // "express", "go", "fastapi", etc.
}

// SubmitBuildInput contains the inputs for submitting a new build via the route handler.
type SubmitBuildInput struct {
	TenantID      string
	ProjectID     string
	Environment   string
	CommitSHA     string
	Apps          []AppInput
	CreatedBy     string
	CorrelationID string
}

// SubmitBuildResult is returned after a build is successfully submitted.
type SubmitBuildResult struct {
	BuildID   string `json:"build_id"`
	StreamURL string `json:"stream_url"`
}

// BuildCallbackInput is the callback payload from the build system.
type BuildCallbackInput struct {
	Status    string             `json:"status"`
	Error     string             `json:"error,omitempty"`
	Apps      []BuildCallbackApp `json:"apps"`
	Manifests string             `json:"manifests,omitempty"` // raw K8s YAML from build pipeline
}

// BuildCallbackApp describes a built app in the callback payload.
type BuildCallbackApp struct {
	Name        string `json:"name"`
	ImageDigest string `json:"image_digest"`
	URL         string `json:"url"`
	Type        string `json:"type,omitempty"` // "server", "worker", "frontend"
	DurationMs  int    `json:"duration_ms"`
}

// BuildOrchestratorService defines the interface the route handler needs
// from the build orchestrator. Follows DIP — handler depends on abstraction,
// using route-local types to avoid import cycles with services/db packages.
type BuildOrchestratorService interface {
	SubmitBuild(ctx context.Context, req SubmitBuildInput) (*SubmitBuildResult, error)
	GetBuildStatus(ctx context.Context, buildID string) (*BuildStatusResponse, error)
	OnBuildCompleted(ctx context.Context, buildID string, result BuildCallbackInput) error
	IsBuildTerminal(ctx context.Context, buildID string) (bool, error)
	CancelBuild(ctx context.Context, buildID string) error
	// GetBuildTenantID returns the tenant that owns a build — used for authorization.
	GetBuildTenantID(ctx context.Context, buildID string) (string, error)
}

// BuildStatusResponse is the JSON response for GET /builds/:id.
type BuildStatusResponse struct {
	BuildID     string `json:"build_id"`
	ProjectID   string `json:"project_id"`
	Environment string `json:"environment"`
	Status      string `json:"status"`
	CommitSHA   string `json:"commit_sha"`
	CreatedBy   string `json:"created_by"`
}

// BuildHandler provides HTTP handlers for build operations.
type BuildHandler struct {
	orch           BuildOrchestratorService
	callbackSecret string
	logger         *slog.Logger
	shutdownCtx    context.Context    // cancelled on server shutdown; used by callback goroutine (A-05)
	callbackWg     sync.WaitGroup     // tracks in-flight callback goroutines (C-08)
}

// NewBuildHandler creates a new BuildHandler.
// shutdownCtx should be cancelled when the server begins graceful shutdown,
// so that background goroutines (e.g. callback processing) can drain.
func NewBuildHandler(orch BuildOrchestratorService, callbackSecret string, logger *slog.Logger, shutdownCtx context.Context) *BuildHandler {
	return &BuildHandler{
		orch:           orch,
		callbackSecret: callbackSecret,
		logger:         logger,
		shutdownCtx:    shutdownCtx,
	}
}

// WaitForCallbacks blocks until all in-flight callback goroutines complete
// or the timeout expires. Used during graceful shutdown (C-08).
func (h *BuildHandler) WaitForCallbacks(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		h.callbackWg.Wait()
		close(done)
	}()
	select {
	case <-done:
		h.logger.Info("all in-flight callbacks drained")
	case <-time.After(timeout):
		h.logger.Warn("timed out waiting for in-flight callbacks", "timeout", timeout)
	}
}

// createBuildRequest is the JSON request body for POST /api/v1/builds.
type createBuildRequest struct {
	ProjectID   string     `json:"project_id"`
	Environment string     `json:"environment"`
	CommitSHA   string     `json:"commit_sha,omitempty"`
	Apps        []AppInput `json:"apps,omitempty"`
}

// Create handles POST /api/v1/builds.
// Auth required. Idempotency-Key middleware applied externally.
func (h *BuildHandler) Create(w http.ResponseWriter, r *http.Request) {
	// C-02: Enforce request deadline so handlers don't hang indefinitely.
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()

	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	var req createBuildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInvalidRequest)
		return
	}

	// Validate required fields
	if req.ProjectID == "" {
		writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"MISSING_FIELD",
			"Project name is required. Check your loopzero.yaml.",
			400,
		))
		return
	}
	if req.Environment == "" {
		writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"MISSING_FIELD",
			"Environment is required",
			400,
		))
		return
	}

	// Environment validation (DA-10): lowercase only
	if !environmentRegex.MatchString(req.Environment) {
		writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
			"INVALID_ENVIRONMENT",
			"Environment names must be lowercase.",
			400,
		))
		return
	}

	resp, err := h.orch.SubmitBuild(ctx, SubmitBuildInput{
		TenantID:      uc.TenantID,
		ProjectID:     req.ProjectID,
		Environment:   req.Environment,
		CommitSHA:     req.CommitSHA,
		Apps:          req.Apps,
		CreatedBy:     uc.UserID,
		CorrelationID: r.Header.Get("Correlation-ID"), //nolint:contextcheck // header from original request
	})
	if err != nil {
		mapBuildError(w, err)
		return
	}

	writeJSON(w, http.StatusAccepted, resp)
}

// Get handles GET /api/v1/builds/:id.
// SEC-4: Validates that the authenticated user's tenant owns the build.
func (h *BuildHandler) Get(w http.ResponseWriter, r *http.Request) {
	// MOD-2: Enforce timeout on read-only handlers to prevent slowloris.
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	buildID := chi.URLParam(r, "id")

	// SEC-4: Verify tenant ownership before returning build data.
	// Returns 404 (not 403) to avoid leaking build existence across tenants.
	buildTenantID, err := h.orch.GetBuildTenantID(ctx, buildID)
	if err != nil {
		mapBuildError(w, err)
		return
	}
	if buildTenantID != uc.TenantID {
		writeLoopZeroError(w, loopzeroerrors.ErrBuildNotFound)
		return
	}

	resp, err := h.orch.GetBuildStatus(ctx, buildID)
	if err != nil {
		mapBuildError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// Cancel handles POST /api/v1/builds/:id/cancel.
// Validates auth and tenant ownership, delegates to orchestrator which checks
// terminal state, cancels the workflow, and transitions the build to CANCELLED.
func (h *BuildHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	// C-02: Enforce request deadline so handlers don't hang indefinitely.
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()

	uc := middleware.GetUserContext(ctx)
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	buildID := chi.URLParam(r, "id")

	// SEC-4: Verify tenant ownership before allowing cancel.
	// Returns 404 (not 403) to avoid leaking build existence across tenants.
	buildTenantID, err := h.orch.GetBuildTenantID(ctx, buildID)
	if err != nil {
		mapBuildError(w, err)
		return
	}
	if buildTenantID != uc.TenantID {
		writeLoopZeroError(w, loopzeroerrors.ErrBuildNotFound)
		return
	}

	if err := h.orch.CancelBuild(ctx, buildID); err != nil {
		mapBuildError(w, err)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status": "cancelled",
	})
}

// callbackRequest is the JSON request body for POST /api/v1/builds/:id/callback.
type callbackRequest struct {
	Status    string             `json:"status"`
	Error     string             `json:"error,omitempty"`
	Apps      []BuildCallbackApp `json:"apps"`
	Manifests string             `json:"manifests,omitempty"` // raw K8s YAML from build pipeline
}

// Callback handles POST /api/v1/builds/:id/callback (DA-15).
// Auth: shared secret (NOT OAuth). Returns 202 immediately, runs OnBuildCompleted in goroutine.
//
// RESILIENCE RULE: This endpoint MUST NEVER return 400 for malformed payloads.
// If the build system (Argo Workflows) terminated and sent a callback, the build
// must transition to a terminal state regardless of payload quality. A 400 here
// leaves the build stuck in RUNNING, blocking the entire environment. (F-01)
func (h *BuildHandler) Callback(w http.ResponseWriter, r *http.Request) {
	// Auth: validate shared secret — auth failures are the ONLY 4xx we return.
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	// SEC-1: Use constant-time comparison to prevent timing attacks on the callback secret.
	if subtle.ConstantTimeCompare([]byte(token), []byte(h.callbackSecret)) != 1 {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	buildID := chi.URLParam(r, "id")

	// A-06: Limit callback body to 10 MB to prevent DoS via oversized payloads.
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)

	// Read body as raw bytes first — never fail on read errors.
	rawBody, readErr := io.ReadAll(r.Body)
	if readErr != nil {
		h.logger.Warn("callback body read failed, treating as failed build",
			"build_id", buildID,
			"error", readErr,
		)
	}

	// Attempt normal JSON decode
	var req callbackRequest
	parsed := false
	if len(rawBody) > 0 {
		if err := json.Unmarshal(rawBody, &req); err != nil {
			// Log the raw payload for diagnostics, then fall back
			h.logger.Warn("callback JSON decode failed, falling back to failed status",
				"build_id", buildID,
				"error", err,
				"raw_body_len", len(rawBody),
				"raw_body_preview", truncate(string(rawBody), 500),
			)
			// Fallback: try to extract status via simple string search
			req = fallbackParseCallback(rawBody)
		} else {
			parsed = true
		}
	}

	// If body was empty or completely unparseable, default to failed
	if req.Status == "" {
		req.Status = "failed"
		if req.Error == "" {
			req.Error = "Build failed (callback payload was empty or unreadable)"
		}
	}

	// Validate status — only "succeeded" and "failed" are valid.
	// Unknown statuses fall back to "failed" instead of returning 400.
	if req.Status != "succeeded" && req.Status != "failed" {
		originalStatus := req.Status
		h.logger.Warn("callback has invalid status, treating as failed",
			"build_id", buildID,
			"status", originalStatus,
		)
		req.Status = "failed"
		if req.Error == "" {
			req.Error = "Build failed (callback reported unexpected status: " + originalStatus + ")"
		}
	}

	// If we fell back from a parse error, ensure error message is populated
	if !parsed && req.Error == "" {
		req.Error = "Build failed (callback payload malformed)"
	}

	// MOD-4: Check terminal state BEFORE apps validation — prevents returning 400
	// for duplicate callbacks with empty apps when build is already completed.
	isTerminal, err := h.orch.IsBuildTerminal(r.Context(), buildID)
	if err != nil {
		mapBuildError(w, err)
		return
	}
	if isTerminal {
		// Duplicate callback — build already in terminal state → 200 no-op
		writeJSON(w, http.StatusOK, map[string]string{
			"status": "already_completed",
		})
		return
	}

	// MOD-1: Succeeded + empty apps means the build system has a bug, but returning
	// 400 would leave the build stuck in RUNNING for 30 minutes (violates resilience rule).
	// Instead, treat it as a failed build so the environment is unblocked immediately.
	if req.Status == "succeeded" && len(req.Apps) == 0 {
		h.logger.Error("callback reported succeeded but provided no apps — treating as failed",
			"build_id", buildID,
		)
		req.Status = "failed"
		req.Error = "Build system reported success but provided no apps (build system bug)"
	}

	// Return 202 immediately (DA-15 EC-R)
	writeJSON(w, http.StatusAccepted, map[string]string{
		"status": "accepted",
	})

	// Run OnBuildCompleted in goroutine — do not block the callback response
	callbackResult := BuildCallbackInput{
		Status:    req.Status,
		Error:     req.Error,
		Apps:      req.Apps,
		Manifests: req.Manifests,
	}
	h.callbackWg.Add(1) // C-08: track in-flight callback for graceful shutdown
	go func() {
		defer h.callbackWg.Done()
		// A-05: Use shutdown-aware context so in-flight callbacks can drain
		// during graceful shutdown instead of being orphaned.
		ctx := h.shutdownCtx
		if err := h.orch.OnBuildCompleted(ctx, buildID, callbackResult); err != nil {
			h.logger.Error("build completion failed",
				"build_id", buildID,
				"error", err,
			)
		}
	}()
}

// fallbackParseCallback returns a failed status when JSON parsing fails.
// No fragile string extraction -- if the payload is malformed, we default to
// failed with a preview of the raw body for diagnostics.
func fallbackParseCallback(raw []byte) callbackRequest {
	return callbackRequest{
		Status: "failed",
		Error:  "Build callback payload was malformed (raw: " + truncate(string(raw), 200) + ")",
	}
}

// truncate returns s truncated to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// mapBuildError maps orchestrator errors to HTTP responses.
func mapBuildError(w http.ResponseWriter, err error) {
	var loopzeroErr *loopzeroerrors.LoopZeroError
	if errors.As(err, &loopzeroErr) {
		// DA-18: Override message for DEPLOY_IN_PROGRESS
		if errors.Is(err, loopzeroerrors.ErrDeployInProgress) {
			writeLoopZeroError(w, loopzeroerrors.NewLoopZeroError(
				"DEPLOY_IN_PROGRESS",
				"A deploy is already in progress for this environment.",
				409,
			))
			return
		}
		writeLoopZeroError(w, loopzeroErr)
		return
	}

	// Fallback for unexpected errors
	writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
}
