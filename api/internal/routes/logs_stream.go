// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/loopzero/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// StreamLogQuerier — alias to domain.StreamLogQuerier (T1.3 of
// deep-review-remediation-plan). The type is canonically defined in
// domain/logs.go; the alias was promoted to the same routes/logs.go file
// to keep existing handler call sites working without churn.

// LogStreamHandlerOpts configures the log streaming handler.
type LogStreamHandlerOpts struct {
	// PollInterval is how often to poll Loki for new log lines. Default: 2s.
	PollInterval time.Duration
	// MaxDuration is the maximum lifetime of a single stream. Default: 30min.
	MaxDuration time.Duration
	// Logger for structured logging. Default: slog.Default().
	Logger *slog.Logger
}

// LogStreamHandler provides SSE endpoints for streaming build and runtime logs.
type LogStreamHandler struct {
	builds       LogBuildStore
	querier      StreamLogQuerier
	pollInterval time.Duration
	maxDuration  time.Duration
	logger       *slog.Logger
}

// NewLogStreamHandler creates a new LogStreamHandler.
func NewLogStreamHandler(builds LogBuildStore, querier StreamLogQuerier, opts LogStreamHandlerOpts) *LogStreamHandler {
	pollInterval := opts.PollInterval
	if pollInterval == 0 {
		pollInterval = 2 * time.Second
	}
	maxDuration := opts.MaxDuration
	if maxDuration == 0 {
		maxDuration = 30 * time.Minute
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &LogStreamHandler{
		builds:       builds,
		querier:      querier,
		pollInterval: pollInterval,
		maxDuration:  maxDuration,
		logger:       logger,
	}
}

// sseLogEvent is the JSON payload for each SSE log event.
// Uses product-friendly field names (no internal terms).
type sseLogEvent struct {
	Timestamp string `json:"timestamp"`
	Line      string `json:"line"`
	Step      string `json:"step,omitempty"`
}

// StreamBuildLogs handles GET /api/v1/builds/{id}/logs/stream.
// Streams build logs as SSE events by polling Loki incrementally.
func (h *LogStreamHandler) StreamBuildLogs(w http.ResponseWriter, r *http.Request) {
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

	// Verify build exists and enforce tenant isolation.
	// T0.4 of deep-review-2026-05-10-remediation-plan: tenant filter
	// pushed to DB layer via GetByIDForTenant.
	build, err := h.builds.GetByIDForTenant(r.Context(), uc.TenantID, buildID)
	if err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if build == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrBuildNotFound)
		return
	}

	// Check for flusher support (required for SSE)
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"code":"INTERNAL_ERROR","message":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Determine starting timestamp from Last-Event-ID header (nanosecond timestamp)
	var sinceNano int64
	if leID := r.Header.Get("Last-Event-ID"); leID != "" {
		if parsed, parseErr := strconv.ParseInt(leID, 10, 64); parseErr == nil {
			sinceNano = parsed
		}
	}

	h.streamLoop(r.Context(), w, flusher, buildID, sinceNano, true)
}

// StreamRuntimeLogs handles GET /api/v1/projects/{name}/logs/stream?env=<env>[&app=<app>].
// Streams runtime logs from the workload namespace via Loki.
//
// Query params:
//   - env (required): environment name (e.g., "production")
//   - app (optional, Beta Logs Gate Criterion 3): filter to a single
//     app within the project. When omitted, returns logs from ALL
//     apps in the namespace (legacy behavior). The app label is
//     promoted by promtail's loopzero-pods scrape job from the pod's
//     loopzero.io/app label.
func (h *LogStreamHandler) StreamRuntimeLogs(w http.ResponseWriter, r *http.Request) {
	uc := middleware.GetUserContext(r.Context())
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	projectName := chi.URLParam(r, "name")
	if projectName == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("project name is required"))
		return
	}

	env := r.URL.Query().Get("env")
	if env == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("env query parameter is required"))
		return
	}

	// Beta Logs Gate Criterion 3: per-app filter. Empty = all apps in
	// the project namespace (backward compatible). Non-empty value
	// is passed through to Loki client which adds `app="<value>"`
	// to the LogQL selector.
	app := r.URL.Query().Get("app")

	// Check for flusher support
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"code":"INTERNAL_ERROR","message":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Determine starting timestamp from Last-Event-ID
	var sinceNano int64
	if leID := r.Header.Get("Last-Event-ID"); leID != "" {
		if parsed, parseErr := strconv.ParseInt(leID, 10, 64); parseErr == nil {
			sinceNano = parsed
		}
	}

	// For runtime logs, the "buildID" is actually a namespace identifier
	// that the Loki querier uses. App is appended as a 5th colon-
	// segment when provided so the loki client can split it without
	// growing the surface for empty-value handling — empty app field
	// means "no filter" downstream.
	runtimeKey := fmt.Sprintf("runtime:%s:%s:%s:%s", uc.TenantID, env, projectName, app)

	h.streamLoop(r.Context(), w, flusher, runtimeKey, sinceNano, false)
}

// streamLoop is the core SSE polling loop. It queries Loki at regular intervals
// and emits new log lines as SSE events.
//
// For build logs (isBuild=true), it stops when the build reaches a terminal state
// and no new logs arrive for two consecutive polls.
// For runtime logs, it streams until client disconnect or max duration.
func (h *LogStreamHandler) streamLoop(
	ctx context.Context,
	w http.ResponseWriter,
	flusher http.Flusher,
	queryKey string,
	sinceNano int64,
	isBuild bool,
) {
	deadline := time.After(h.maxDuration)
	pollTicker := time.NewTicker(h.pollInterval)
	defer pollTicker.Stop()

	// Track consecutive empty polls after build is terminal
	var emptyPollsAfterTerminal int
	const maxEmptyPollsAfterTerminal = 2

	for {
		select {
		case <-ctx.Done():
			h.logger.Info("log stream: client disconnected", "key", queryKey)
			return
		case <-deadline:
			h.logger.Info("log stream: max duration reached", "key", queryKey)
			// Beta Logs Gate Criterion 6 — enrich the timeout reason
			// with a "why + next" so the operator doesn't think the
			// stream died unexpectedly. 30 minutes is the platform
			// cap to prevent forgotten `loopzero logs --follow` sessions
			// from holding API resources indefinitely.
			h.writeStreamEvent(w, flusher, "stream.timeout",
				`{"reason":"Stream open for 30 minutes — platform cap to prevent forgotten sessions from holding resources. Rerun `+"`"+`loopzero logs --build <id> --follow`+"`"+` to resume from the latest position; the stream picks up where this one ended."}`)
			return
		case <-pollTicker.C:
			// Poll Loki for new logs
			entries, err := h.querier.QueryBuildLogsSince(ctx, queryKey, sinceNano, 500)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				h.logger.Warn("log stream: loki query failed", "key", queryKey, "error", err)
				// Continue polling -- Loki may recover
				continue
			}

			// Emit each log line as an SSE event
			for _, entry := range entries {
				ts := entry.Timestamp
				nsID := timestampToNano(ts)

				evt := sseLogEvent{
					Timestamp: ts,
					Line:      entry.Content,
					Step:      entry.Step,
				}

				data, _ := json.Marshal(evt)
				if writeErr := h.writeSSELogEvent(w, flusher, fmt.Sprintf("%d", nsID), data); writeErr != nil {
					return
				}

				// Advance cursor past this entry
				if nsID > sinceNano {
					sinceNano = nsID
				}
			}

			// For build logs: check if build is terminal and no more logs arriving.
			// Internal polling loop AFTER auth gate; safe to use GetByID since
			// the caller's tenant was already validated at handler entry.
			if isBuild {
				build, buildErr := h.builds.GetByID(ctx, queryKey)
				if buildErr != nil {
					if ctx.Err() != nil {
						return
					}
					continue
				}
				if build != nil && statemachine.IsBuildTerminal(statemachine.State(build.Status)) {
					if len(entries) == 0 {
						emptyPollsAfterTerminal++
					} else {
						emptyPollsAfterTerminal = 0
					}

					if emptyPollsAfterTerminal >= maxEmptyPollsAfterTerminal {
						h.logger.Info("log stream: build terminal, no more logs",
							"build_id", queryKey, "status", build.Status)
						h.writeStreamEvent(w, flusher, "stream.done",
							fmt.Sprintf(`{"status":"%s"}`, build.Status))
						return
					}
				}
			}
		}
	}
}

// writeSSELogEvent writes a single log SSE event.
func (h *LogStreamHandler) writeSSELogEvent(w http.ResponseWriter, flusher http.Flusher, id string, data []byte) error {
	_, err := fmt.Fprintf(w, "id: %s\nevent: log\ndata: %s\n\n", id, string(data))
	if err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// writeStreamEvent writes a control SSE event (non-log).
//
// G705 false positive: SSE stream goes to JSON-parsing clients (CLI,
// dashboard), not an HTML renderer; the `data` string is API-internal
// event payload, never raw user input. Caller controls.
func (h *LogStreamHandler) writeStreamEvent(w http.ResponseWriter, flusher http.Flusher, eventType string, data string) {
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data) //nolint:gosec // G705 false positive: SSE to JSON-parser clients
	flusher.Flush()
}

// timestampToNano converts an RFC3339 timestamp string to nanoseconds since epoch.
// Falls back to current time on parse failure.
func timestampToNano(ts string) int64 {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		// Try RFC3339Nano
		t, err = time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return time.Now().UnixNano()
		}
	}
	return t.UnixNano()
}
