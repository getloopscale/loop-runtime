// Package rollbackhttp serves the rollback endpoint: returning an environment to a
// previous release.
//
// Twentieth family to leave the flat `routes` package (B-080, slice-38).
package rollbackhttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/getloopscale/loopzero/api/internal/httpio"
	"github.com/getloopscale/loopzero/api/internal/ports"

	"github.com/go-chi/chi/v5"

	"github.com/getloopscale/loopzero/api/internal/correlation"
	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// RollbackService abstracts the rollback orchestrator for the handler (DIP).
type RollbackService interface {
	Rollback(ctx context.Context, req RollbackInput) (*RollbackOutput, error)
}

// RollbackInput is the input for the rollback service from the handler.
type RollbackInput struct {
	TenantID      string
	ProjectID     string
	ProjectName   string
	Environment   string
	ToReleaseID   string
	CorrelationID string
	UserID        string
	BaseDomain    string
}

// RollbackOutput is the result from the rollback service.
type RollbackOutput struct {
	RolledBackReleaseID  string
	ReactivatedReleaseID string
	// RollbackID names the record of the rollback in the project's history.
	RollbackID  string
	Environment string
	URL         string
}

// RollbackProjectStore abstracts project lookup for rollback (DIP).
type RollbackProjectStore interface {
	GetByName(ctx context.Context, tenantID, name string) (*ports.ProjectResult, error)
}

// RollbackReleaseLookup resolves the canonical release for a build.
// Used to translate the dashboard sugar {build_id} into the canonical
// state-machine identifier {to_release_id}. Nil-safe: when nil, the
// handler rejects requests that only carry build_id with a typed
// error (caller must send to_release_id directly).
//
// Plan: deploy-timeline-canonical-5-phases LIVE follow-up — closes
// the dashboard rollback 400 caught against the LIVE Vercel UX flow.
type RollbackReleaseLookup interface {
	GetReleaseForBuild(ctx context.Context, tenantID, projectID, environment, buildID string) (releaseID string, err error)
}

// rollbackRequest is the JSON body for POST /api/v1/projects/{name}/rollback.
//
// Canonical contract: {environment, to_release_id?} per the Release
// state machine — rollback flips an ACTIVE release back. When
// to_release_id is omitted, the service rolls back to the prior
// ACTIVE release for the environment ("undo last deploy" sugar).
//
// Dashboard sugar (LIVE-caught 2026-06-18): build_id may be supplied
// instead of to_release_id. The handler resolves the release that
// was created from the named build and treats it as to_release_id.
// Lets the UI pass the user-facing build identifier without an
// extra lookup round-trip.
type rollbackRequest struct {
	Environment string `json:"environment"`
	ToReleaseID string `json:"to_release_id,omitempty"`
	BuildID     string `json:"build_id,omitempty"`
}

// rollbackResponse is the JSON response for a successful rollback.
// rollbackStatusInProgress is the status of an accepted rollback.
const rollbackStatusInProgress = "in_progress"

type rollbackResponse struct {
	Status             string `json:"status"`
	RollbackID         string `json:"rollback_id"`
	RolledBackRelease  string `json:"rolled_back_release"`
	ReactivatedRelease string `json:"reactivated_release"`
	Environment        string `json:"environment"`
	URL                string `json:"url"`
}

// RollbackHandler provides HTTP handlers for rollback operations.
type RollbackHandler struct {
	projects   RollbackProjectStore
	service    RollbackService
	releases   RollbackReleaseLookup // nil-safe: when nil, build_id sugar is rejected
	history    *RollbackHistoryHandler
	baseDomain string
}

// NewRollbackHandler creates a new RollbackHandler.
func NewRollbackHandler(projects RollbackProjectStore, service RollbackService, baseDomain string) *RollbackHandler {
	return &RollbackHandler{
		projects:   projects,
		service:    service,
		baseDomain: baseDomain,
	}
}

// WithReleaseLookup wires the build_id → release_id resolver. Returns
// the handler for fluent chaining at the wire-up site.
func (h *RollbackHandler) WithReleaseLookup(rl RollbackReleaseLookup) *RollbackHandler {
	h.releases = rl
	return h
}

// WithHistory wires the recorded rollbacks that List serves.
func (h *RollbackHandler) WithHistory(history RollbackHistory) *RollbackHandler {
	h.history = NewRollbackHistoryHandler(history)
	return h
}

// List handles GET /api/v1/projects/{name}/rollbacks.
func (h *RollbackHandler) List(w http.ResponseWriter, r *http.Request) {
	if h.history == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError.Wrap("rollback history not configured"))
		return
	}
	h.history.List(w, r)
}

// Create handles POST /api/v1/projects/{name}/rollback.
func (h *RollbackHandler) Create(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()

	uc := auth.GetUserContext(ctx)
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("project name is required"))
		return
	}

	// Parse request body
	var req rollbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("invalid request body"))
		return
	}

	env := req.Environment
	if env == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("environment is required"))
		return
	}

	// Resolve dashboard sugar: build_id → to_release_id.
	if req.BuildID != "" && req.ToReleaseID == "" {
		if h.releases == nil {
			httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError.Wrap("rollback by build_id not configured"))
			return
		}
		relID, lookupErr := h.releases.GetReleaseForBuild(ctx, uc.TenantID, name, env, req.BuildID)
		if lookupErr != nil {
			slog.Error("rollback: build_id lookup failed", "build_id", req.BuildID, "err", lookupErr)
			httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
			return
		}
		if relID == "" {
			httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("no release found for build_id"))
			return
		}
		req.ToReleaseID = relID
	}

	// Look up project
	project, err := h.projects.GetByName(ctx, uc.TenantID, name)
	if err != nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if project == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.NewProjectNotFound(name))
		return
	}

	// T6.1 of plan 2026-06-01-p3-infra-observability-test-debt — use the
	// canonical `Correlation-ID` reader with backward-compat fallback to
	// `X-Correlation-ID`. Helper emits a deprecation WARN on legacy hits
	// so operators can identify lagging CLI clients before the next
	// major drops the fallback.
	correlationID := correlation.Read(r)

	// Execute rollback
	result, err := h.service.Rollback(ctx, RollbackInput{
		TenantID:      uc.TenantID,
		ProjectID:     name, // Deploy uses project name as project_id in builds/releases
		ProjectName:   name,
		Environment:   env,
		ToReleaseID:   req.ToReleaseID,
		CorrelationID: correlationID,
		UserID:        uc.UserID,
		BaseDomain:    h.baseDomain,
	})
	if err != nil {
		var loopzeroErr *loopzeroerrors.LoopZeroError
		if errors.As(err, &loopzeroErr) {
			httpio.WriteLoopZeroError(w, loopzeroErr)
			return
		}
		// Mirror mapBuildError at builds.go:402 — log unexpected errors for diagnosis.
		slog.Error("unexpected error in rollback handler",
			"error", err.Error(),
			"type", fmt.Sprintf("%T", err),
			"tenant_id", uc.TenantID,
			"project", name,
			"env", env,
			"to_release", req.ToReleaseID,
		)
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	// 202: the target's manifests are pushed and the sync is triggered. The
	// cells roll the pods through canary steps for longer than a request
	// lives, so the release state settles afterwards (#118). A client learns
	// the outcome from GET /projects/{name}/status: active_release is
	// reactivated_release once the rollback completed, and stays
	// rolled_back_release when it failed.
	resp := rollbackResponse{
		Status:             rollbackStatusInProgress,
		RollbackID:         result.RollbackID,
		RolledBackRelease:  result.RolledBackReleaseID,
		ReactivatedRelease: result.ReactivatedReleaseID,
		Environment:        result.Environment,
		URL:                result.URL,
	}

	httpio.WriteJSON(w, http.StatusAccepted, resp)
}
