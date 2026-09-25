// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package logs serves the build log endpoints: the paged read and the streaming
// tail.
//
// Twenty-second family to leave the flat `routes` package (B-080, slice-39),
// extracted alongside `status` — both are read surfaces over a build, and both
// come in a one-shot and a streaming variant.
package logs

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/getloopscale/loopzero/api/internal/httpio"

	"github.com/go-chi/chi/v5"

	auth "github.com/getloopscale/loopzero/api/internal/middleware/auth"
	"github.com/getloopscale/loopzero/api/internal/ports"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// parseSinceParam converts a `?since=<value>` query parameter into a
// number of seconds. Accepts:
//   - Pure integer ("3600") — interpreted as seconds.
//   - Go duration ("24h", "30m", "45s") — parsed via time.ParseDuration.
//   - Day shorthand ("7d", "30d") — normalized to 24h*N internally.
//
// Returns 0 on parse failure OR negative duration (caller treats 0 as
// "no override" and falls back to the default 7-day window).
//
// Beta Logs Gate Criterion 7 — the runbook documents this contract so
// operators can write `loopzero logs --since 7d` without learning Go's
// idiosyncratic duration syntax (which lacks "d").
var daySuffixRegex = regexp.MustCompile(`^(\d+)d$`)

func parseSinceParam(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	// Pure integer → seconds
	if secs, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if secs <= 0 {
			return 0
		}
		return secs
	}
	// Day shorthand: "Nd" → "N*24h"
	if m := daySuffixRegex.FindStringSubmatch(raw); m != nil {
		if days, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			if days <= 0 {
				return 0
			}
			return days * 86400
		}
	}
	// Go duration syntax
	if d, err := time.ParseDuration(raw); err == nil {
		if d <= 0 {
			return 0
		}
		return int64(d.Seconds())
	}
	return 0
}

// T1.3 of deep-review-remediation-plan (closes find_arch_001 + find_dep_routes_type_hub partial):
// canonical types live in domain/. routes/ re-exports as type aliases for
// back-compat with handler-adjacent code in this package.
type (
	LogEntry         = ports.LogEntry
	LogQueryOpts     = ports.LogQueryOpts
	LogQuerier       = ports.LogQuerier
	StreamLogQuerier = ports.StreamLogQuerier
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

// errLogsUnavailable answers a read whose log backend could not be reached.
var errLogsUnavailable = loopzeroerrors.NewLoopZeroError("LOGS_UNAVAILABLE",
	"Build logs are temporarily unavailable. Try again in a moment.")

// LogsHandler provides HTTP handlers for build log operations.
type LogsHandler struct {
	builds  LogBuildStore
	querier LogQuerier
	// debugAuthorized decides whether a request may use ?mode=debug (plan
	// J-17 Layer 3). Nil refuses everyone: dashboard user JWTs never enable
	// debug, only an operator the router vouches for.
	debugAuthorized func(*http.Request) bool
}

// NewLogsHandler creates a new LogsHandler.
func NewLogsHandler(builds LogBuildStore, querier LogQuerier) *LogsHandler {
	return &LogsHandler{
		builds:  builds,
		querier: querier,
	}
}

// WithDebugAuthorizer returns a copy of the handler that admits ?mode=debug
// for the requests authorized reports true for.
func (h *LogsHandler) WithDebugAuthorizer(authorized func(*http.Request) bool) *LogsHandler {
	withDebug := *h
	withDebug.debugAuthorized = authorized
	return &withDebug
}

// Get handles GET /api/v1/builds/{id}/logs.
func (h *LogsHandler) Get(w http.ResponseWriter, r *http.Request) {
	uc := auth.GetUserContext(r.Context())
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	buildID := chi.URLParam(r, "id")
	if buildID == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("build ID is required"))
		return
	}

	// Verify build exists and enforce tenant isolation
	build, err := h.builds.GetByIDForTenant(r.Context(), uc.TenantID, buildID)
	if err != nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	// T0.4: tenant filter is now at DB level — build==nil means NotFound
	// OR cross-tenant access (indistinguishable to the caller by design).
	if build == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrBuildNotFound)
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
	// Beta Logs Gate Criterion 7 — `since` query param. Accepts both
	// raw seconds ("3600") and Go duration syntax ("24h", "7d" via "168h").
	// "d" is normalized to "24h" before time.ParseDuration so the
	// operator runbook can document "--since 7d" without users having
	// to know Go's `time.ParseDuration` quirks (Go doesn't natively
	// support "d" — minutes/hours only).
	if sinceStr := r.URL.Query().Get("since"); sinceStr != "" {
		if secs := parseSinceParam(sinceStr); secs > 0 {
			opts.SinceSeconds = secs
		}
	}

	// Plan J-17 Layer 3 (2026-06-06): operator-only debug mode bypasses
	// the user-facing filter. `?mode=debug` returns every container line
	// including raw main-container stdout. Gated by the same LOOPZERO_ADMIN_API
	// + X-Service-Auth pattern used by admin endpoints — a dashboard user
	// JWT cannot enable it.
	if r.URL.Query().Get("mode") == "debug" {
		if h.debugAuthorized == nil || !h.debugAuthorized(r) {
			httpio.WriteLoopZeroError(w, loopzeroerrors.ErrForbidden)
			return
		}
		opts.Debug = true
	}

	// Query logs from Loki
	entries, err := h.querier.QueryBuildLogs(r.Context(), buildID, opts)
	if err != nil {
		// The log backend is unreachable. That is not "no logs": answering
		// 200 with no entries would tell the caller the logs are gone.
		slog.ErrorContext(r.Context(), "build logs query failed", "build_id", buildID, "error", err)
		httpio.WriteLoopZeroError(w, errLogsUnavailable)
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

	httpio.WriteJSON(w, http.StatusOK, resp)
}
