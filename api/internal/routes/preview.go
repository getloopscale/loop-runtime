// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/getloopscale/loopzero/api/internal/domain"
	"github.com/getloopscale/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// PreviewStore is the persistence interface used by the preview HTTP handler.
//
// Mirrors db.PreviewStore (read-only subset). The handler does not write —
// creation/updates flow through PreviewService (Sprint 5 / T1.1).
type PreviewStore interface {
	ListByTenant(ctx context.Context, tenantID string) ([]domain.Preview, error)
	ListByProject(ctx context.Context, tenantID, projectID string) ([]domain.Preview, error)
	GetByID(ctx context.Context, tenantID, previewID string) (*domain.Preview, error)
	RequestDeletion(ctx context.Context, tenantID, previewID string) error
}

// PreviewService + CreatePreviewRequest live in domain/ (T1.3 of
// deep-review-remediation-plan, closes find_arch_001 partial). Aliases keep
// existing handler call sites working without churn.
type (
	PreviewService       = domain.PreviewService
	CreatePreviewRequest = domain.CreatePreviewRequest
)

// PreviewCRPhaseLookup resolves the LIVE PreviewEnvironment CR phase
// (Creating/Active/Failed/Deleting/Deleted) per preview ID. Used to
// keep the dashboard /previews row in sync with the operator without
// requiring a PG writeback path.
//
// LIVE catch 2026-06-07 J-23 bug #5: operator brings CR to Active +
// namespace + URL, but PreviewStore.UpdateStatus is never called →
// dashboard shows perpetual "creating". Enrich-at-request via this
// lookup avoids adding a new reconciler.
type PreviewCRPhaseLookup interface {
	GetPhases(ctx context.Context, previewIDs []string) (map[string]string, error)
}

// PreviewHandler handles preview environment API endpoints.
type PreviewHandler struct {
	store       PreviewStore
	service     PreviewService
	logger      *slog.Logger
	phaseLookup PreviewCRPhaseLookup
}

// NewPreviewHandler creates a new PreviewHandler. service may be nil if
// only read/delete endpoints should be exposed.
func NewPreviewHandler(store PreviewStore, service PreviewService, logger *slog.Logger) *PreviewHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &PreviewHandler{store: store, service: service, logger: logger}
}

// WithPhaseLookup wires the live CR phase lookup so List enriches each
// preview row with the operator-observed phase. Fluent setter.
func (h *PreviewHandler) WithPhaseLookup(p PreviewCRPhaseLookup) *PreviewHandler {
	h.phaseLookup = p
	return h
}

// requestTimeout is applied to every preview HTTP handler. Generous enough
// for slow PG, tight enough that idle requests don't block forever.
const previewRequestTimeout = 30 * time.Second

// Auth + project URL parameter extraction. Returns (tenantID, projectName,
// ok). When ok=false, an error response has already been written.
func (h *PreviewHandler) authAndProject(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	uc := middleware.GetUserContext(r.Context())
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return "", "", false
	}
	project := chi.URLParam(r, "project")
	if strings.TrimSpace(project) == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("project is required"))
		return "", "", false
	}
	return uc.TenantID, project, true
}

// List returns all preview environments for the authenticated tenant
// scoped to the project in the URL.
//
// GET /api/v1/projects/{project}/previews
func (h *PreviewHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), previewRequestTimeout)
	defer cancel()

	tenantID, projectID, ok := h.authAndProject(w, r)
	if !ok {
		return
	}

	previews, err := h.store.ListByProject(ctx, tenantID, projectID)
	if err != nil {
		h.logger.Error("preview list failed",
			"error", err,
			"tenant", tenantID,
			"project", projectID,
		)
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	// LIVE catch 2026-06-07 J-23 bug #5: enrich each row with the live
	// CR phase (operator-observed). Tolerant: on lookup error, fall
	// through with DB status — dashboard renders honest stale.
	if h.phaseLookup != nil && len(previews) > 0 {
		ids := make([]string, 0, len(previews))
		for _, p := range previews {
			ids = append(ids, p.ID)
		}
		if phases, lerr := h.phaseLookup.GetPhases(ctx, ids); lerr == nil {
			for i, p := range previews {
				if live, ok := phases[p.ID]; ok && live != "" {
					previews[i].Status = strings.ToLower(live)
				}
			}
		} else {
			h.logger.Warn("preview phase lookup failed (using DB status)", "error", lerr)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"previews": previews,
		"count":    len(previews),
		"total":    len(previews), // legacy alias used by older CLI builds
	})
}

// Get returns a specific preview environment.
//
// GET /api/v1/projects/{project}/previews/{id}
func (h *PreviewHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), previewRequestTimeout)
	defer cancel()

	tenantID, projectID, ok := h.authAndProject(w, r)
	if !ok {
		return
	}

	previewID := chi.URLParam(r, "id")
	if strings.TrimSpace(previewID) == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("preview id is required"))
		return
	}

	preview, err := h.store.GetByID(ctx, tenantID, previewID)
	if err != nil {
		if errors.Is(err, loopzeroerrors.ErrPreviewNotFound) {
			writeLoopZeroError(w, loopzeroerrors.ErrPreviewNotFound)
			return
		}
		h.logger.Error("preview get failed", "error", err, "preview", previewID)
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	// Defense in depth: the store filters by tenant, but if a preview were
	// for a different project under the same tenant, return 404 — never
	// leak across project boundaries.
	if preview.ProjectID != projectID {
		writeLoopZeroError(w, loopzeroerrors.ErrPreviewNotFound)
		return
	}

	writeJSON(w, http.StatusOK, preview)
}

// Delete requests deletion of a preview environment (soft-delete).
// The PreviewController observes the change and tears down the namespace.
//
// DELETE /api/v1/projects/{project}/previews/{id}
func (h *PreviewHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), previewRequestTimeout)
	defer cancel()

	tenantID, projectID, ok := h.authAndProject(w, r)
	if !ok {
		return
	}

	previewID := chi.URLParam(r, "id")
	if strings.TrimSpace(previewID) == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("preview id is required"))
		return
	}

	// Confirm cross-project access is rejected.
	preview, err := h.store.GetByID(ctx, tenantID, previewID)
	if err != nil {
		if errors.Is(err, loopzeroerrors.ErrPreviewNotFound) {
			writeLoopZeroError(w, loopzeroerrors.ErrPreviewNotFound)
			return
		}
		h.logger.Error("preview lookup before delete failed", "error", err)
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if preview.ProjectID != projectID {
		writeLoopZeroError(w, loopzeroerrors.ErrPreviewNotFound)
		return
	}

	if err := h.store.RequestDeletion(ctx, tenantID, previewID); err != nil {
		if errors.Is(err, loopzeroerrors.ErrPreviewNotFound) {
			writeLoopZeroError(w, loopzeroerrors.ErrPreviewNotFound)
			return
		}
		h.logger.Error("preview delete failed", "error", err, "preview", previewID)
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"id":     previewID,
		"status": "deleting",
	})
}

// Create handles manual / webhook-driven preview creation.
//
// POST /api/v1/projects/{project}/previews
func (h *PreviewHandler) Create(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), previewRequestTimeout)
	defer cancel()

	tenantID, projectID, ok := h.authAndProject(w, r)
	if !ok {
		return
	}

	if h.service == nil {
		// No PreviewService wired — feature isn't fully online yet.
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError.Wrap("preview service not available"))
		return
	}

	var body CreatePreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInvalidRequest.Wrap("malformed JSON: "+err.Error()))
		return
	}
	// URL project takes precedence over body field (URL is canonical).
	body.ProjectID = projectID

	if body.PRNumber <= 0 {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("pr_number must be positive"))
		return
	}
	if strings.TrimSpace(body.Branch) == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("branch is required"))
		return
	}
	if strings.TrimSpace(body.CommitSHA) == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("commit_sha is required"))
		return
	}

	// Inject tenant from authenticated context — never trust client.
	body.SetTenantID(tenantID)

	preview, err := h.service.CreatePreview(ctx, body)
	if err != nil {
		var te *loopzeroerrors.LoopZeroError
		if errors.As(err, &te) {
			writeLoopZeroError(w, te)
			return
		}
		h.logger.Error("preview create failed", "error", err)
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}

	writeJSON(w, http.StatusAccepted, preview)
}

// TenantID + SetTenantID methods now live in domain/preview_request.go alongside
// the canonical CreatePreviewRequest type (T1.3).
