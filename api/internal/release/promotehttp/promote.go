// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package promotehttp serves the promotion endpoint: moving a release from one
// environment to the next.
//
// Nineteenth family to leave the flat `routes` package (B-080, slice-38),
// extracted alongside `rollback` — the two are the release lifecycle's forward
// and backward moves, and they share nothing but that symmetry.
//
// It declares its own `EnvironmentEnsurer`, the third package to do so: a Go
// interface is structural, so each consumer naming the one method it calls is
// cheaper and more honest than a shared package nobody owns.
package promotehttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/httpio"
	"github.com/getloopscale/loop-runtime/api/internal/ports"

	"github.com/go-chi/chi/v5"

	"github.com/getloopscale/loop-runtime/api/internal/correlation"
	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// PromoteService abstracts the promotion orchestrator for the handler (DIP).
type PromoteService interface {
	Promote(ctx context.Context, req PromoteInput) (*PromoteOutput, error)
}

// PromoteInput is the input for the promote service from the handler.
type PromoteInput struct {
	TenantID      string
	ProjectID     string
	ProjectName   string
	FromEnv       string
	ToEnv         string
	CorrelationID string
	UserID        string
}

// PromoteOutput is the result from the promote service.
type PromoteOutput struct {
	PromotionID     string
	SourceReleaseID string
	NewReleaseID    string
	FromEnv         string
	ToEnv           string
	URL             string
}

// PromoteProjectStore abstracts project lookup for promote (DIP).
type PromoteProjectStore interface {
	GetByName(ctx context.Context, tenantID, name string) (*ports.ProjectResult, error)
}

// promoteRequest is the JSON body for POST /api/v1/projects/{name}/promote.
type promoteRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// promoteResponse is the JSON response for a successful promotion.
type promoteResponse struct {
	PromotionID   string `json:"promotion_id"`
	Status        string `json:"status"`
	SourceRelease string `json:"source_release"`
	NewRelease    string `json:"new_release"`
	FromEnv       string `json:"from"`
	ToEnv         string `json:"to"`
	URL           string `json:"url,omitempty"`
}

// EnvironmentEnsurer creates the environment a promotion targets.
//
// Declared here rather than imported from the builds family that also declares
// it: a Go interface is structural, so the consumer naming exactly the method it
// calls is both idiomatic and the DIP shape rules/architecture.md asks for. Same
// decision the projects family took one slice earlier, for the same reason — the
// dependency is on ONE method, not on a package (B-080, slice-37).
type EnvironmentEnsurer interface {
	EnsureEnvironment(ctx context.Context, tenantID, project, envName string) error
}

// PromoteHandler provides HTTP handlers for promote operations.
type PromoteHandler struct {
	projects   PromoteProjectStore
	service    PromoteService
	baseDomain string
	// envEnsurer closes Red Team loop-console#158 — the promote path was
	// missing the m37/#37 EnsureEnvironment retrofit that builds.go had.
	// Without it, promoting to a not-yet-existing production env leaves
	// the Tenant CR spec.environments[] short of the (project, env)
	// tuple → AppProject destinations never extend → ArgoCD sync fails
	// with "namespace not permitted" → release ends in SYNC_FAILED and
	// the CLI reports success with a URL that never serves (404).
	envEnsurer EnvironmentEnsurer
}

// NewPromoteHandler creates a new PromoteHandler.
func NewPromoteHandler(projects PromoteProjectStore, service PromoteService, baseDomain string) *PromoteHandler {
	return &PromoteHandler{
		projects:   projects,
		service:    service,
		baseDomain: baseDomain,
	}
}

// WithEnvironmentEnsurer wires an EnvironmentEnsurer so the promote handler
// auto-creates the target (production/staging/…) Environment CR before
// attempting the release-target creation. Nil ensurer (default) preserves
// legacy behavior (target env must pre-exist). Mirrors
// BuildHandler.WithEnvironmentEnsurer. Closes Red Team loop-console#158.
func (h *PromoteHandler) WithEnvironmentEnsurer(e EnvironmentEnsurer) *PromoteHandler {
	h.envEnsurer = e
	return h
}

// Create handles POST /api/v1/projects/{name}/promote.
func (h *PromoteHandler) Create(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrUnauthorized)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrMissingField.Wrap("project name is required"))
		return
	}

	// Parse request body
	var req promoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrMissingField.Wrap("invalid request body"))
		return
	}

	if req.From == "" {
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrMissingField.Wrap("'from' environment is required"))
		return
	}
	if req.To == "" {
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrMissingField.Wrap("'to' environment is required"))
		return
	}

	// Look up project
	project, err := h.projects.GetByName(ctx, uc.TenantID, name)
	if err != nil {
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrInternalError)
		return
	}
	if project == nil {
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.NewProjectNotFound(name))
		return
	}

	// T6.1 of plan 2026-06-01-p3-infra-observability-test-debt — use the
	// canonical `Correlation-ID` reader with backward-compat fallback to
	// `X-Correlation-ID` so existing CLI clients keep working through
	// one release cycle (helper emits a deprecation WARN on legacy hits).
	correlationID := correlation.Read(r)

	// Red Team loop-console#158: auto-create the target env before the release
	// row is created. Symptomatic pre-fix state: promote → SetDesired created
	// the env_target row + release row, but the Tenant CR spec.environments[]
	// was missing (project, ToEnv) → AppProject destinations never extended
	// to include the target namespace → ArgoCD Application `tn-<tenant>-<env>`
	// stays OutOfSync/Missing → release → SYNC_FAILED → CLI still reports
	// success with a URL that 404s. Mirror of the m37/#37 EnsureEnvironment
	// retrofit already applied to the builds path (builds.go:363-370).
	//
	// Nil ensurer preserves legacy behavior (env must pre-exist). Idempotent
	// on already-existing envs. Failure surfaces as a typed error → JSON.
	if h.envEnsurer != nil {
		if ensureErr := h.envEnsurer.EnsureEnvironment(ctx, uc.TenantID, name, req.To); ensureErr != nil {
			var loopRuntimeErr *loopruntimeerrors.LoopRuntimeError
			if errors.As(ensureErr, &loopRuntimeErr) {
				httpio.WriteLoopRuntimeError(w, loopRuntimeErr)
				return
			}
			slog.Error("promote: EnsureEnvironment failed",
				"error", ensureErr.Error(), "tenant_id", uc.TenantID,
				"project", name, "to", req.To,
			)
			httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrInternalError)
			return
		}
	}

	// Execute promotion
	result, err := h.service.Promote(ctx, PromoteInput{
		TenantID:      uc.TenantID,
		ProjectID:     name, // Deploy uses project name as project_id in builds/releases
		ProjectName:   name,
		FromEnv:       req.From,
		ToEnv:         req.To,
		CorrelationID: correlationID,
		UserID:        uc.UserID,
	})
	if err != nil {
		var loopRuntimeErr *loopruntimeerrors.LoopRuntimeError
		if errors.As(err, &loopRuntimeErr) {
			httpio.WriteLoopRuntimeError(w, loopRuntimeErr)
			return
		}
		// Mirror mapBuildError at builds.go:402 — log unexpected errors for diagnosis.
		// Without this, 500 responses are opaque and E2E validation is impossible.
		slog.Error("unexpected error in promote handler",
			"error", err.Error(),
			"type", fmt.Sprintf("%T", err),
			"tenant_id", uc.TenantID,
			"project", name,
			"from", req.From,
			"to", req.To,
		)
		httpio.WriteLoopRuntimeError(w, loopruntimeerrors.ErrInternalError)
		return
	}

	resp := promoteResponse{
		PromotionID:   result.PromotionID,
		Status:        "promoting",
		SourceRelease: result.SourceReleaseID,
		NewRelease:    result.NewReleaseID,
		FromEnv:       result.FromEnv,
		ToEnv:         result.ToEnv,
		URL:           result.URL,
	}

	httpio.WriteJSON(w, http.StatusAccepted, resp)
}
