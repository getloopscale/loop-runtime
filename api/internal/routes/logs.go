// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"crypto/subtle"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/getloopscale/loopzero/api/internal/domain"
	"github.com/getloopscale/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// isDebugAuthorized matches the admin endpoint pattern (admin_pin_cell,
// admin_evict): LOOPZERO_ADMIN_API=true env gates the feature, and the
// X-Service-Auth header must match LOOPZERO_ADMIN_SERVICE_AUTH_SECRET to
// authorize the call. Dashboard user JWTs CANNOT enable debug — only
// operator service-to-service callers (CLI with admin token, runbook
// scripts) carry the secret.
//
// Plan J-17 Layer 3 (2026-06-06) gate for ?mode=debug on /logs.
func isDebugAuthorized(r *http.Request) bool {
	if os.Getenv("LOOPZERO_ADMIN_API") != "true" {
		return false
	}
	expected := os.Getenv("LOOPZERO_ADMIN_SERVICE_AUTH_SECRET")
	if expected == "" {
		return false
	}
	got := strings.TrimSpace(r.Header.Get("X-Service-Auth"))
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

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
		if !isDebugAuthorized(r) {
			writeLoopZeroError(w, loopzeroerrors.ErrForbidden)
			return
		}
		opts.Debug = true
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
