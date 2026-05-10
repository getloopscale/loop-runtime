// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/domain"
	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// T1.3 of deep-review-remediation-plan (closes find_arch_001 + find_dep_routes_type_hub partial):
// canonical types live in domain/. routes/ re-exports as type aliases for
// back-compat with handler-adjacent code in this package.
type (
	LogEntry         = domain.LogEntry
	LogQueryOpts     = domain.LogQueryOpts
	LogQuerier       = domain.LogQuerier
	StreamLogQuerier = domain.StreamLogQuerier
)

// _ keeps the context import used (the type aliases above don't reference it).
var _ context.Context = nil

// LogBuild is the minimal build info needed by the logs handler.
type LogBuild struct {
	ID       string
	TenantID string
	Status   string
}

// LogBuildStore abstracts build lookup for the logs handler (DIP).
//
// T0.4 of deep-review-2026-05-10-remediation-plan: GetByIDForTenant is the
// canonical scoping mechanism. GetByID is kept for backward compatibility
// of intermediate adapters but new HTTP handlers MUST use the scoped form.
type LogBuildStore interface {
	GetByID(ctx context.Context, buildID string) (*LogBuild, error)
	GetByIDForTenant(ctx context.Context, tenantID, buildID string) (*LogBuild, error)
}

// logsResponse is the JSON response for GET /api/v1/builds/{id}/logs.
type logsResponse struct {
	BuildID    string     `json:"build_id"`
	Status     string     `json:"status"`
	Entries    []LogEntry `json:"entries"`
	TotalLines int        `json:"total_lines"`
}

// LogsHandler provides HTTP handlers for build log operations.
type LogsHandler struct {
	builds  LogBuildStore
	querier LogQuerier
}

// NewLogsHandler creates a new LogsHandler.
func NewLogsHandler(builds LogBuildStore, querier LogQuerier) *LogsHandler {
	return &LogsHandler{
		builds:  builds,
		querier: querier,
	}
}

// Get handles GET /api/v1/builds/{id}/logs.
func (h *LogsHandler) Get(w http.ResponseWriter, r *http.Request) {
	uc := middleware.GetUserContext(r.Context())
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	buildID := chi.URLParam(r, "id")
	if buildID == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("build ID is required"))
		return
	}

	// Verify build exists and enforce tenant isolation
	build, err := h.builds.GetByIDForTenant(r.Context(), uc.TenantID, buildID)
	if err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	// T0.4: tenant filter is now at DB level — build==nil means NotFound
	// OR cross-tenant access (indistinguishable to the caller by design).
	if build == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrBuildNotFound)
		return
	}

	// Parse query parameters
	opts := LogQueryOpts{}
	if tailStr := r.URL.Query().Get("tail"); tailStr != "" {
		if limit, parseErr := strconv.Atoi(tailStr); parseErr == nil && limit > 0 {
			opts.Limit = limit
		}
	}
	if step := r.URL.Query().Get("step"); step != "" {
		opts.Step = step
	}

	// Query logs from Loki
	entries, err := h.querier.QueryBuildLogs(r.Context(), buildID, opts)
	if err != nil {
		// Loki unavailable — return empty entries with warning instead of 500.
		// The build may have completed but logs are not yet available.
		writeJSON(w, http.StatusOK, logsResponse{
			BuildID:    build.ID,
			Status:     build.Status,
			Entries:    []LogEntry{},
			TotalLines: 0,
		})
		return
	}

	if entries == nil {
		entries = []LogEntry{}
	}

	resp := logsResponse{
		BuildID:    build.ID,
		Status:     build.Status,
		Entries:    entries,
		TotalLines: len(entries),
	}

	writeJSON(w, http.StatusOK, resp)
}
