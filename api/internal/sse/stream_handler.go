// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package sse

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
	errorshttp "github.com/getloopscale/loop-runtime/pkg/errors/http"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
	"github.com/go-chi/chi/v5"
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
//   - deploy.completed / deploy.sync_failed / deploy.git_push_failed: Phase 2 finished
//   - build.failed / build.cancelled: no deploy will follow
//   - rollout.aborted: argo-rollouts-progressive-delivery-plan T4.1 — canary
//     analysis failed and Argo Rollouts auto-aborted. Replaces deploy.completed
//     in the abort path. Note: rollout.step is NON-terminal — stream stays open
//     while canary progresses through pause/setWeight steps.
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
	gate                 *Gate
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
	h.gate = NewGate(h.maxConcurrentStreams)
	return h
}

// ActiveStreams returns the current number of active SSE connections.
func (h *StreamHandler) ActiveStreams() int64 {
	return h.gate.Active()
}

// pollCadence is how often a stream re-reads the event store: five poll
// intervals, never faster than 3s. Nothing LISTENs for Postgres
// notifications, so polling is the only delivery path and this cadence
// bounds its load (one query per open stream per tick).
func pollCadence(pollInterval time.Duration) time.Duration {
	if d := pollInterval * 5; d >= 3*time.Second {
		return d
	}
	return 3 * time.Second
}

// ServeHTTP handles the SSE stream request.
func (h *StreamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	build, ok := h.authorizedBuild(w, r)
	if !ok {
		return
	}
	// BLOCKER-2: cap concurrent SSE connections to prevent resource exhaustion.
	leave, ok := h.gate.Enter()
	if !ok {
		h.logger.Warn("stream: max concurrent streams reached, rejecting",
			"build_id", build.ID, "active", h.gate.Active(), "max", h.gate.Max())
		WriteTooManyStreams(w)
		return
	}
	defer leave()

	stream, err := Open(w)
	if err != nil {
		http.Error(w, `{"code":"INTERNAL_ERROR","message":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}
	session := &buildStream{h: h, stream: stream, build: build, lastEventID: LastEventID(r)}
	ctx := r.Context()
	if done, err := session.replay(ctx); done || err != nil {
		if err != nil {
			h.logger.Error("stream: replay failed", "build_id", build.ID, "error", err)
		}
		return
	}

	h.logger.Info("stream: entering poll loop",
		"build_id", build.ID, "last_event_id", session.lastEventID, "build_status", build.Status)
	loop := Loop{Interval: pollCadence(h.pollInterval), Heartbeat: h.heartbeatInterval}
	if err := loop.Run(ctx, stream, session.poll); err != nil {
		h.logger.Warn("stream: closed", "build_id", build.ID, "error", err)
	}
}

// authorizedBuild resolves the build and enforces tenant ownership, writing
// the error response itself when it answers false. SEC-1: a build owned by
// another tenant answers 404, not 403, so its existence is not disclosed.
func (h *StreamHandler) authorizedBuild(w http.ResponseWriter, r *http.Request) (*BuildInfo, bool) {
	buildID := chi.URLParam(r, "id")
	if buildID == "" {
		http.Error(w, `{"code":"MISSING_PARAM","message":"build id is required"}`, http.StatusBadRequest)
		return nil, false
	}
	uc := auth.GetUserContext(r.Context())
	if uc == nil {
		errorshttp.WriteJSON(w, r, loopruntimeerrors.ErrUnauthorized)
		return nil, false
	}
	build, err := h.buildStore.GetByID(r.Context(), buildID)
	if err != nil {
		if errors.Is(err, loopruntimeerrors.ErrBuildNotFound) {
			errorshttp.WriteJSON(w, r, loopruntimeerrors.ErrBuildNotFound)
			return nil, false
		}
		h.logger.Error("stream handler: failed to fetch build", "build_id", buildID, "error", err)
		errorshttp.WriteJSON(w, r, loopruntimeerrors.NewLoopRuntimeError("INTERNAL_ERROR", "failed to fetch build"))
		return nil, false
	}
	if build.TenantID != uc.TenantID {
		errorshttp.WriteJSON(w, r, loopruntimeerrors.ErrBuildNotFound)
		return nil, false
	}
	return build, true
}

// buildStream is one open build stream: the build-specific replay and
// terminal policy on top of the shared Loop.
//
// The stream closes when a stream-terminal EVENT has been delivered, not
// when the build reaches a terminal state: deploy events (Phase 2) arrive
// after the build is marked SUCCEEDED. A build terminal for longer than the
// safety timeout without such an event closes with stream.timeout.
type buildStream struct {
	h             *StreamHandler
	stream        *Stream
	build         *BuildInfo
	lastEventID   int64
	terminalSince time.Time
}

// replay sends the events after the resume anchor. It reports done when the
// stream must close right away: a terminal event was replayed, or the
// journal was pruned for a build that is already terminal.
func (b *buildStream) replay(ctx context.Context) (bool, error) {
	events, err := b.h.eventStore.GetAfter(ctx, b.build.ID, b.lastEventID)
	if err != nil {
		return true, fmt.Errorf("replay after %d: %w", b.lastEventID, err)
	}
	if b.lastEventID > 0 && len(events) == 0 {
		done, err := b.handleEmptyResume(ctx)
		if done || err != nil {
			return true, err
		}
	}
	if err := b.send(events); err != nil {
		return true, err
	}
	if containsStreamTerminalEvent(events) {
		return true, nil
	}
	if isBuildTerminal(b.build) {
		b.terminalSince = time.Now()
	}
	return false, nil
}

// handleEmptyResume (MOD-3): a client resumed with Last-Event-ID and nothing
// newer exists. If the build has events at all, the ones it asked for were
// pruned — say JOURNAL_EXPIRED, and close when the build is terminal. If it
// has none, it is a fresh build and the stream follows it from the start.
func (b *buildStream) handleEmptyResume(ctx context.Context) (bool, error) {
	all, err := b.h.eventStore.GetAfter(ctx, b.build.ID, 0)
	if err != nil {
		return true, fmt.Errorf("checking for a pruned journal: %w", err)
	}
	b.lastEventID = 0
	if len(all) == 0 {
		return false, nil
	}
	if err := b.stream.Send("0", "JOURNAL_EXPIRED",
		[]byte(`{"reason":"Requested events are no longer available","action":"reconnect_without_last_event_id"}`)); err != nil {
		return true, err
	}
	return isBuildTerminal(b.build), nil
}

// poll is one tick of the loop (a PollFunc).
func (b *buildStream) poll(ctx context.Context) (bool, error) {
	// LEVE-3: re-read the build only while it can still change.
	if b.terminalSince.IsZero() {
		build, err := b.h.buildStore.GetByID(ctx, b.build.ID)
		if err != nil {
			return false, fmt.Errorf("refreshing build status: %w", err)
		}
		b.build = build
		if isBuildTerminal(build) {
			b.terminalSince = time.Now()
		}
	}
	events, err := b.h.eventStore.GetAfter(ctx, b.build.ID, b.lastEventID)
	if err != nil {
		return false, fmt.Errorf("reading events after %d: %w", b.lastEventID, err)
	}
	if err := b.send(events); err != nil {
		return true, err
	}
	if containsStreamTerminalEvent(events) {
		b.h.logger.Info("stream: terminal event delivered, closing", "build_id", b.build.ID, "status", b.build.Status)
		return true, nil
	}
	if !b.terminalSince.IsZero() && time.Since(b.terminalSince) > b.h.safetyTimeout {
		b.h.logger.Warn("stream: safety timeout — build terminal but no deploy events arrived",
			"build_id", b.build.ID, "status", b.build.Status, "terminal_since", b.terminalSince)
		return true, b.stream.Send("0", "stream.timeout",
			[]byte(`{"reason":"Build completed but deploy events were not received within the safety timeout","action":"check_build_status"}`))
	}
	return false, nil
}

func (b *buildStream) send(events []SSEEvent) error {
	for _, ev := range events {
		if err := b.stream.SendEvent(ev); err != nil {
			return err
		}
		b.lastEventID = ev.ID
	}
	return nil
}

func isBuildTerminal(b *BuildInfo) bool {
	return statemachine.IsBuildPhaseTerminal(statemachine.BuildPhase(b.Status))
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
	gate                 *Gate
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
	h.gate = NewGate(h.maxConcurrentStreams)
	return h
}

// ServeHTTP handles the SSE request for a project-level stream.
func (h *ProjectStreamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	projectName := chi.URLParam(r, "name")
	if projectName == "" {
		http.Error(w, `{"code":"MISSING_PARAM","message":"project name is required"}`, http.StatusBadRequest)
		return
	}
	uc := auth.GetUserContext(r.Context())
	if uc == nil {
		errorshttp.WriteJSON(w, r, loopruntimeerrors.ErrUnauthorized)
		return
	}
	leave, ok := h.gate.Enter()
	if !ok {
		h.logger.Warn("project stream: max concurrent streams reached, rejecting",
			"project", projectName, "active", h.gate.Active(), "max", h.gate.Max())
		WriteTooManyStreams(w)
		return
	}
	defer leave()

	stream, err := Open(w)
	if err != nil {
		http.Error(w, `{"code":"INTERNAL_ERROR","message":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}
	ctx := r.Context()
	lastEventID := LastEventID(r)
	sendNew := func(ctx context.Context) (bool, error) {
		events, err := h.eventStore.GetAfterForProject(ctx, uc.TenantID, projectName, lastEventID)
		if err != nil {
			return false, fmt.Errorf("reading project events after %d: %w", lastEventID, err)
		}
		for _, ev := range events {
			if err := stream.SendEvent(ev); err != nil {
				return true, err
			}
			lastEventID = ev.ID
		}
		// A project stream is a perpetual subscription: stream-terminal
		// events of one build do not close it.
		return false, nil
	}
	if _, err := sendNew(ctx); err != nil {
		h.logger.Error("project stream: replay failed", "project", projectName, "error", err)
		return
	}
	loop := Loop{Interval: pollCadence(h.pollInterval), Heartbeat: h.heartbeatInterval}
	err = loop.Run(ctx, stream, sendNew)
	if errors.Is(err, ErrPollRetriesExhausted) {
		h.logger.Error("project stream: max retries exhausted, closing", "project", projectName, "error", err)
		_ = stream.Send("0", "stream.error", []byte(`{"reason":"upstream poll failed repeatedly","action":"reconnect"}`))
	}
}
