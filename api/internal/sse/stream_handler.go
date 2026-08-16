// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package sse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/api/internal/middleware"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
	errorshttp "github.com/getloopscale/loopzero/pkg/errors/http"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// BuildInfo holds the minimal build information needed by the stream handler.
// This avoids importing the db package (which would create an import cycle).
type BuildInfo struct {
	ID       string
	TenantID string
	Status   string
}

// BuildStoreReader is a minimal interface for reading build status.
// Separated from the full BuildStore to respect ISP.
// Returns BuildInfo to avoid coupling with the db package.
type BuildStoreReader interface {
	GetByID(ctx context.Context, id string) (*BuildInfo, error)
}

// streamTerminalEvents are event types that signal the stream should close.
// These are events after which no more meaningful events will arrive.
// - deploy.completed / deploy.sync_failed / deploy.git_push_failed: Phase 2 finished
// - build.failed / build.cancelled: no deploy will follow
// - rollout.aborted: argo-rollouts-progressive-delivery-plan T4.1 — canary
//   analysis failed and Argo Rollouts auto-aborted. Replaces deploy.completed
//   in the abort path. Note: rollout.step is NON-terminal — stream stays open
//   while canary progresses through pause/setWeight steps.
var streamTerminalEvents = map[string]bool{
	"deploy.completed":       true,
	"deploy.sync_failed":     true,
	"deploy.git_push_failed": true,
	"build.failed":           true,
	"build.cancelled":        true,
	"rollout.aborted":        true,
}

// isStreamTerminalEvent returns true if the event type signals the stream should close.
func isStreamTerminalEvent(eventType string) bool {
	return streamTerminalEvents[eventType]
}

// containsStreamTerminalEvent checks whether a slice of events contains at least
// one stream-terminal event.
func containsStreamTerminalEvent(events []SSEEvent) bool {
	for i := len(events) - 1; i >= 0; i-- {
		if isStreamTerminalEvent(events[i].EventType) {
			return true
		}
	}
	return false
}

// defaultSafetyTimeout is how long the stream waits after the build becomes terminal
// before closing, even if no deploy events arrived. Covers edge cases where Phase 2
// never starts (e.g., callback failure).
const defaultSafetyTimeout = 5 * time.Minute

// defaultMaxConcurrentStreams is the maximum number of concurrent SSE connections
// the server will accept. Prevents resource exhaustion from runaway clients.
const defaultMaxConcurrentStreams = 200

// maxPollRetries is the number of consecutive transient DB errors tolerated
// before closing the stream. Prevents infinite loops on persistent failures.
const maxPollRetries = 3

// StreamOption configures StreamHandler behavior (used for testing).
type StreamOption func(*StreamHandler)

// WithHeartbeatInterval overrides the default heartbeat interval.
func WithHeartbeatInterval(d time.Duration) StreamOption {
	return func(h *StreamHandler) {
		h.heartbeatInterval = d
	}
}

// WithPollInterval overrides the default poll interval.
func WithPollInterval(d time.Duration) StreamOption {
	return func(h *StreamHandler) {
		h.pollInterval = d
	}
}

// WithSafetyTimeout overrides the default safety timeout for builds that are terminal
// but no deploy events have arrived.
func WithSafetyTimeout(d time.Duration) StreamOption {
	return func(h *StreamHandler) {
		h.safetyTimeout = d
	}
}

// WithLogger sets a structured logger for the handler.
func WithLogger(l *slog.Logger) StreamOption {
	return func(h *StreamHandler) {
		h.logger = l
	}
}

// WithMaxConcurrentStreams overrides the maximum number of simultaneous SSE connections.
func WithMaxConcurrentStreams(n int64) StreamOption {
	return func(h *StreamHandler) {
		h.maxConcurrentStreams = n
	}
}

// StreamHandler handles GET /api/v1/builds/:id/stream.
// It streams SSE events for a build, supporting reconnection via Last-Event-ID.
//
// The stream closes when a "stream-terminal" event has been delivered, NOT when the
// build reaches a terminal state. This is because deploy events (Phase 2) arrive AFTER
// the build is marked SUCCEEDED. Closing on build-terminal would miss deploy events.
//
// Security: Verifies tenant ownership — a user can only stream builds owned by their tenant.
type StreamHandler struct {
	eventStore           EventStore
	buildStore           BuildStoreReader
	logger               *slog.Logger
	heartbeatInterval    time.Duration
	pollInterval         time.Duration
	safetyTimeout        time.Duration
	maxConcurrentStreams int64
	activeStreams         atomic.Int64
}

// NewStreamHandler creates a StreamHandler with the given dependencies.
func NewStreamHandler(eventStore EventStore, buildStore BuildStoreReader, opts ...StreamOption) *StreamHandler {
	h := &StreamHandler{
		eventStore:           eventStore,
		buildStore:           buildStore,
		logger:               slog.Default(),
		heartbeatInterval:    15 * time.Second,
		pollInterval:         1 * time.Second,
		safetyTimeout:        defaultSafetyTimeout,
		maxConcurrentStreams: defaultMaxConcurrentStreams,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// ActiveStreams returns the current number of active SSE connections.
func (h *StreamHandler) ActiveStreams() int64 {
	return h.activeStreams.Load()
}

// ServeHTTP handles the SSE stream request.
func (h *StreamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	buildID := chi.URLParam(r, "id")
	if buildID == "" {
		http.Error(w, `{"code":"MISSING_PARAM","message":"build id is required"}`, http.StatusBadRequest)
		return
	}

	// SEC-1: Verify tenant ownership — return 404 (not 403) to avoid leaking build existence.
	// Per ADR-0030 + T2.3 closure: emit RFC 9457 problem+json (Stage C migration).
	uc := middleware.GetUserContext(r.Context())
	if uc == nil {
		errorshttp.WriteJSON(w, r, loopzeroerrors.ErrUnauthorized)
		return
	}

	// Verify build exists
	build, err := h.buildStore.GetByID(r.Context(), buildID)
	if err != nil {
		if errors.Is(err, loopzeroerrors.ErrBuildNotFound) {
			errorshttp.WriteJSON(w, r, loopzeroerrors.ErrBuildNotFound)
			return
		}
		h.logger.Error("stream handler: failed to fetch build",
			"build_id", buildID,
			"error", err,
		)
		errorshttp.WriteJSON(w, r, loopzeroerrors.NewLoopZeroError("INTERNAL_ERROR", "failed to fetch build", 0))
		return
	}

	// SEC-1: Tenant isolation — user can only stream their own builds.
	if build.TenantID != uc.TenantID {
		errorshttp.WriteJSON(w, r, loopzeroerrors.ErrBuildNotFound)
		return
	}

	// BLOCKER-2: Enforce max concurrent SSE connections to prevent resource exhaustion.
	current := h.activeStreams.Add(1)
	if current > h.maxConcurrentStreams {
		h.activeStreams.Add(-1)
		h.logger.Warn("stream: max concurrent streams reached, rejecting",
			"build_id", buildID, "active", current-1, "max", h.maxConcurrentStreams)
		http.Error(w, `{"code":"TOO_MANY_STREAMS","message":"Too many active streams. Try again shortly."}`, http.StatusServiceUnavailable)
		return
	}
	defer h.activeStreams.Add(-1)

	// BLOCKER-1: Track active connections in Prometheus.
	metrics.SSEConnectionsActive.Inc()
	defer metrics.SSEConnectionsActive.Dec()

	// Check if ResponseWriter supports flushing (required for SSE)
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"code":"INTERNAL_ERROR","message":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Prevent proxy buffering (Nginx, CF)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Determine starting event ID from Last-Event-ID header
	var lastEventID int64
	if leID := r.Header.Get("Last-Event-ID"); leID != "" {
		parsed, parseErr := strconv.ParseInt(leID, 10, 64)
		if parseErr == nil {
			lastEventID = parsed
		}
	}

	// Use a context that detects client disconnection
	ctx := r.Context()

	// Replay existing events (with or without Last-Event-ID)
	var replayStart int64
	if lastEventID > 0 {
		replayStart = lastEventID
	}

	events, getErr := h.eventStore.GetAfter(ctx, buildID, replayStart)
	if getErr != nil {
		h.logger.Error("stream: GetAfter failed (replay)",
			"build_id", buildID, "last_event_id", replayStart, "error", getErr)
		return
	}

	// MOD-3: Handle journal expiration when reconnecting with Last-Event-ID.
	// Only emit JOURNAL_EXPIRED when:
	// 1. Client sent Last-Event-ID (reconnecting)
	// 2. No events with id > Last-Event-ID exist
	// 3. Events DO exist for this build (but with lower IDs — they were pruned)
	// This prevents false positives when the build simply hasn't produced events yet.
	if lastEventID > 0 && len(events) == 0 {
		// Check if there are ANY events for this build (even with lower IDs).
		// If there are none at all, this is not journal expiration — the build
		// may just be starting. Only if events exist with ID <= lastEventID
		// (meaning the journal was pruned) do we emit JOURNAL_EXPIRED.
		allEvents, _ := h.eventStore.GetAfter(ctx, buildID, 0)
		if len(allEvents) > 0 {
			// Events exist but none with ID > lastEventID — journal was pruned.
			if writeErr := writeSSEEvent(w, flusher, "0", "JOURNAL_EXPIRED", json.RawMessage(`{"reason":"Requested events are no longer available","action":"reconnect_without_last_event_id"}`)); writeErr != nil {
				return
			}
			// If build is terminal and journal is gone, close the stream.
			if statemachine.IsBuildPhaseTerminal(statemachine.BuildPhase(build.Status)) {
				return
			}
		}
		// If no events exist at all, this is a fresh build — fall through to poll loop.
		// Reset lastEventID so the poll loop catches events from the beginning.
		lastEventID = 0
	}

	// Send replayed events
	for _, ev := range events {
		if writeErr := writeSSEEvent(w, flusher, fmt.Sprintf("%d", ev.ID), ev.EventType, ev.Data); writeErr != nil {
			return
		}
		lastEventID = ev.ID
	}

	// If a stream-terminal event was delivered during replay, close immediately.
	if containsStreamTerminalEvent(events) {
		return
	}

	h.logger.Info("stream: entering poll loop",
		"build_id", buildID, "last_event_id", lastEventID, "build_status", build.Status)

	// A-02 fix: Poll at a slower interval (fallback). The event store sends
	// pg_notify('sse_events', buildID) on each Append. The notification
	// channel notifies us to poll immediately. This reduces DB QPS from
	// 200 (one per stream per second) to ~N (one per actual event).
	// The pollInterval serves as a fallback in case NOTIFY is missed.
	fallbackInterval := h.pollInterval * 5 // 5s fallback instead of 1s constant
	if fallbackInterval < 3*time.Second {
		fallbackInterval = 3 * time.Second
	}
	pollTicker := time.NewTicker(fallbackInterval)
	heartbeatTicker := time.NewTicker(h.heartbeatInterval)
	defer pollTicker.Stop()
	defer heartbeatTicker.Stop()

	// Track when the build first became terminal, for the safety timeout.
	// If it is already terminal (SUCCEEDED waiting for deploy events), start the clock now.
	var buildTerminalSince time.Time
	if statemachine.IsBuildPhaseTerminal(statemachine.BuildPhase(build.Status)) {
		buildTerminalSince = time.Now()
	}

	// LEVE-3: Track whether we need to re-fetch build status.
	// Only re-fetch when the build is not yet terminal (status can still change).
	buildIsTerminal := !buildTerminalSince.IsZero()

	// MOD-4: Track consecutive poll errors for retry logic.
	var consecutivePollErrors int

	for {
		select {
		case <-ctx.Done():
			// Client disconnected
			h.logger.Info("stream: client disconnected",
				"build_id", buildID, "reason", ctx.Err())
			return
		case <-heartbeatTicker.C:
			_, writeErr := fmt.Fprintf(w, ":heartbeat\n\n")
			if writeErr != nil {
				h.logger.Info("stream: heartbeat write failed (client gone)",
					"build_id", buildID, "error", writeErr)
				return
			}
			flusher.Flush()
		case <-pollTicker.C:
			// LEVE-3: Only re-check build status when it hasn't reached terminal yet.
			if !buildIsTerminal {
				build, err = h.buildStore.GetByID(ctx, buildID)
				if err != nil {
					if ctx.Err() != nil {
						h.logger.Info("stream: poll cancelled (client gone)",
							"build_id", buildID)
						return
					}
					// MOD-4: Retry transient DB errors instead of closing immediately.
					consecutivePollErrors++
					h.logger.Warn("stream: GetByID failed in poll loop, retrying",
						"build_id", buildID, "error", err,
						"attempt", consecutivePollErrors, "max", maxPollRetries)
					if consecutivePollErrors >= maxPollRetries {
						h.logger.Error("stream: GetByID failed after max retries, closing",
							"build_id", buildID, "error", err)
						return
					}
					continue
				}
				consecutivePollErrors = 0

				// Track when the build first became terminal (for safety timeout).
				if statemachine.IsBuildPhaseTerminal(statemachine.BuildPhase(build.Status)) {
					buildTerminalSince = time.Now()
					buildIsTerminal = true
				}
			}

			// Poll for new events
			newEvents, getErr := h.eventStore.GetAfter(ctx, buildID, lastEventID)
			if getErr != nil {
				if ctx.Err() != nil {
					h.logger.Info("stream: poll cancelled (client gone)",
						"build_id", buildID)
					return
				}
				// MOD-4: Retry transient DB errors instead of closing immediately.
				consecutivePollErrors++
				h.logger.Warn("stream: GetAfter failed in poll loop, retrying",
					"build_id", buildID, "error", getErr,
					"attempt", consecutivePollErrors, "max", maxPollRetries)
				if consecutivePollErrors >= maxPollRetries {
					h.logger.Error("stream: GetAfter failed after max retries, closing",
						"build_id", buildID, "error", getErr)
					return
				}
				continue
			}
			consecutivePollErrors = 0

			for _, ev := range newEvents {
				if writeErr := writeSSEEvent(w, flusher, fmt.Sprintf("%d", ev.ID), ev.EventType, ev.Data); writeErr != nil {
					return
				}
				lastEventID = ev.ID
			}

			// If a stream-terminal event was delivered, close the stream.
			if containsStreamTerminalEvent(newEvents) {
				h.logger.Info("stream: terminal event delivered, closing",
					"build_id", buildID, "status", build.Status)
				return
			}

			// Safety timeout: if build has been terminal for too long without a
			// stream-terminal event, close with a diagnostic event. This covers
			// edge cases where Phase 2 never starts (e.g., callback failure).
			if !buildTerminalSince.IsZero() && time.Since(buildTerminalSince) > h.safetyTimeout {
				h.logger.Warn("stream: safety timeout — build terminal but no deploy events arrived",
					"build_id", buildID, "status", build.Status,
					"terminal_since", buildTerminalSince)
				_ = writeSSEEvent(w, flusher, "0", "stream.timeout", json.RawMessage(
					`{"reason":"Build completed but deploy events were not received within the safety timeout","action":"check_build_status"}`))
				return
			}
		}
	}
}

// =============================================================================
// ProjectStreamHandler — Plan J-17 T2.3
// =============================================================================
//
// Handles GET /api/v1/projects/{name}/builds/stream. Unlike the build-level
// StreamHandler (which closes on a stream-terminal event), this handler stays
// open for the lifetime of the connection: a project list is a perpetual
// subscription. Stream-terminal events are NOT honored — when a build closes
// here, the next build for the same project keeps the stream alive.
//
// SECURITY: tenant ownership is enforced inside EventStore.GetAfterForProject
// itself. We additionally require an authenticated UserContext so anonymous
// callers never reach the store (defense-in-depth, mirrors SEC-1 above).

// ProjectStreamHandler streams every event for every build of a project.
type ProjectStreamHandler struct {
	eventStore           EventStore
	logger               *slog.Logger
	heartbeatInterval    time.Duration
	pollInterval         time.Duration
	maxConcurrentStreams int64
	activeStreams        atomic.Int64
}

// ProjectStreamOption configures ProjectStreamHandler (used for testing).
type ProjectStreamOption func(*ProjectStreamHandler)

// WithProjectHeartbeatInterval overrides the default heartbeat interval.
func WithProjectHeartbeatInterval(d time.Duration) ProjectStreamOption {
	return func(h *ProjectStreamHandler) { h.heartbeatInterval = d }
}

// WithProjectPollInterval overrides the default poll interval.
func WithProjectPollInterval(d time.Duration) ProjectStreamOption {
	return func(h *ProjectStreamHandler) { h.pollInterval = d }
}

// WithProjectLogger sets a structured logger.
func WithProjectLogger(l *slog.Logger) ProjectStreamOption {
	return func(h *ProjectStreamHandler) { h.logger = l }
}

// WithProjectMaxConcurrentStreams overrides the maximum concurrent connections.
func WithProjectMaxConcurrentStreams(n int64) ProjectStreamOption {
	return func(h *ProjectStreamHandler) { h.maxConcurrentStreams = n }
}

// NewProjectStreamHandler constructs the handler with sensible defaults.
func NewProjectStreamHandler(eventStore EventStore, opts ...ProjectStreamOption) *ProjectStreamHandler {
	h := &ProjectStreamHandler{
		eventStore:           eventStore,
		logger:               slog.Default(),
		heartbeatInterval:    15 * time.Second,
		pollInterval:         1 * time.Second,
		maxConcurrentStreams: defaultMaxConcurrentStreams,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// ServeHTTP handles the SSE request for a project-level stream.
func (h *ProjectStreamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	projectName := chi.URLParam(r, "name")
	if projectName == "" {
		http.Error(w, `{"code":"MISSING_PARAM","message":"project name is required"}`, http.StatusBadRequest)
		return
	}

	uc := middleware.GetUserContext(r.Context())
	if uc == nil {
		errorshttp.WriteJSON(w, r, loopzeroerrors.ErrUnauthorized)
		return
	}

	current := h.activeStreams.Add(1)
	if current > h.maxConcurrentStreams {
		h.activeStreams.Add(-1)
		h.logger.Warn("project stream: max concurrent streams reached, rejecting",
			"project", projectName, "active", current-1, "max", h.maxConcurrentStreams)
		http.Error(w, `{"code":"TOO_MANY_STREAMS","message":"Too many active streams. Try again shortly."}`, http.StatusServiceUnavailable)
		return
	}
	defer h.activeStreams.Add(-1)

	metrics.SSEConnectionsActive.Inc()
	defer metrics.SSEConnectionsActive.Dec()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"code":"INTERNAL_ERROR","message":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	var lastEventID int64
	if leID := r.Header.Get("Last-Event-ID"); leID != "" {
		if parsed, parseErr := strconv.ParseInt(leID, 10, 64); parseErr == nil {
			lastEventID = parsed
		}
	}

	ctx := r.Context()

	events, getErr := h.eventStore.GetAfterForProject(ctx, uc.TenantID, projectName, lastEventID)
	if getErr != nil {
		h.logger.Error("project stream: GetAfterForProject failed (replay)",
			"project", projectName, "last_event_id", lastEventID, "error", getErr)
		return
	}
	for _, ev := range events {
		if writeErr := writeSSEEvent(w, flusher, fmt.Sprintf("%d", ev.ID), ev.EventType, ev.Data); writeErr != nil {
			return
		}
		lastEventID = ev.ID
	}

	fallbackInterval := h.pollInterval * 5
	if fallbackInterval < 3*time.Second {
		fallbackInterval = 3 * time.Second
	}
	pollTicker := time.NewTicker(fallbackInterval)
	heartbeatTicker := time.NewTicker(h.heartbeatInterval)
	defer pollTicker.Stop()
	defer heartbeatTicker.Stop()

	var consecutivePollErrors int
	for {
		select {
		case <-ctx.Done():
			h.logger.Info("project stream: client disconnected",
				"project", projectName, "reason", ctx.Err())
			return
		case <-heartbeatTicker.C:
			if _, writeErr := fmt.Fprintf(w, ":heartbeat\n\n"); writeErr != nil {
				h.logger.Info("project stream: heartbeat write failed (client gone)",
					"project", projectName, "error", writeErr)
				return
			}
			flusher.Flush()
		case <-pollTicker.C:
			newEvents, pollErr := h.eventStore.GetAfterForProject(ctx, uc.TenantID, projectName, lastEventID)
			if pollErr != nil {
				if ctx.Err() != nil {
					return
				}
				consecutivePollErrors++
				h.logger.Warn("project stream: poll failed, retrying",
					"project", projectName, "error", pollErr,
					"attempt", consecutivePollErrors, "max", maxPollRetries)
				if consecutivePollErrors >= maxPollRetries {
					h.logger.Error("project stream: max retries exhausted, closing",
						"project", projectName, "error", pollErr)
					_ = writeSSEEvent(w, flusher, "0", "stream.error", json.RawMessage(
						`{"reason":"upstream poll failed repeatedly","action":"reconnect"}`))
					return
				}
				continue
			}
			consecutivePollErrors = 0

			for _, ev := range newEvents {
				if writeErr := writeSSEEvent(w, flusher, fmt.Sprintf("%d", ev.ID), ev.EventType, ev.Data); writeErr != nil {
					return
				}
				lastEventID = ev.ID
			}
		}
	}
}

// writeSSEEvent writes a single SSE event in the standard format.
// Returns an error if the write fails (e.g., client disconnected).
func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, id string, eventType string, data json.RawMessage) error {
	_, err := fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", id, eventType, string(data))
	if err != nil {
		return err
	}
	flusher.Flush()
	// BLOCKER-1: Track emitted events in Prometheus.
	metrics.SSEEventsEmitted.WithLabelValues(eventType).Inc()
	return nil
}
