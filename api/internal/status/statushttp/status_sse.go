// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statushttp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/httpio"

	"github.com/go-chi/chi/v5"

	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	"github.com/getloopscale/loop-runtime/api/internal/sse"
	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// T2.1 of plan 2026-06-15-j18-t1.3.b-sse-pg-notify-backend v1.1.
//
// StatusSSEHandler exposes a project-scoped SSE stream that surfaces
// `signal.changed` events emitted by ReleaseStoreSqlc.AppendSideEffect
// (T1.1) so the dashboard `useStatusStream` consumer (T3.1) renders
// realtime signal pill updates.
//
// Wire: GET /api/v1/projects/{name}/signals/stream?env=<env>[&since=<id>]
// Returns: text/event-stream emitting `event: signal.changed\ndata: <json>\nid: <int>\n\n`
//
// EC absorptions:
//   - EC-2: only signal.changed events of the requested env (the store query filters both)
//   - EC-5: Flusher comma-ok form (500 on missing, NOT panic)
//   - EC-7: cross-tenant returns 404 NOT_FOUND (existence-leak-prevention)
//   - EC-9: Last-Event-ID dedup by id (replay > lastID; client SSE native handles reconnect)

// StatusSSEEventStore is the one read StatusSSEHandler needs: the
// signal.changed events of ONE environment of a project (satisfied by
// *store.PostgresEventStore). The environment is part of the query because a
// client subscribed to ?env=staging must never receive production signals.
type StatusSSEEventStore interface {
	GetSignalsAfter(ctx context.Context, tenantID, projectName, environment string, afterID int64) ([]sse.SSEEvent, error)
}

// StatusSSEHandler handles GET /api/v1/projects/{name}/signals/stream.
type StatusSSEHandler struct {
	projects   StatusProjectStore
	eventStore StatusSSEEventStore
	// pollInterval controls how often the handler re-queries GetSignalsAfter
	// when no events arrived since the last poll. Default 2s. Test-tunable.
	pollInterval time.Duration
	// maxDuration caps the handler's loop lifetime so a single client
	// can't hold a connection open forever. Default 30min. Test-tunable.
	maxDuration time.Duration
	// heartbeatInterval keeps idle proxies from closing the connection.
	heartbeatInterval time.Duration
	gate              *sse.Gate
	logger            *slog.Logger
}

// StatusSSEHandlerOpts configures NewStatusSSEHandler with test-tunable fields.
type StatusSSEHandlerOpts struct {
	PollInterval time.Duration // default 2s
	MaxDuration  time.Duration // default 30min
	Logger       *slog.Logger  // default slog.Default()
}

// NewStatusSSEHandler creates a new StatusSSEHandler.
//
// projects MUST be non-nil (handler enforces project ownership via GetByName).
// eventStore MUST be non-nil (handler reads events to forward to client).
// Panics on nil for either dep to surface mis-wired startups loud + fast,
// per EC-1 contract pattern from T1.1.
func NewStatusSSEHandler(projects StatusProjectStore, eventStore StatusSSEEventStore, opts StatusSSEHandlerOpts) *StatusSSEHandler {
	if projects == nil {
		panic("StatusSSEHandler requires non-nil StatusProjectStore")
	}
	if eventStore == nil {
		panic("StatusSSEHandler requires non-nil StatusSSEEventStore")
	}
	poll := opts.PollInterval
	if poll == 0 {
		poll = 2 * time.Second
	}
	maxDur := opts.MaxDuration
	if maxDur == 0 {
		maxDur = 30 * time.Minute
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &StatusSSEHandler{
		projects:          projects,
		eventStore:        eventStore,
		pollInterval:      poll,
		maxDuration:       maxDur,
		heartbeatInterval: 15 * time.Second,
		gate:              sse.NewGate(0),
		logger:            logger,
	}
}

// Get handles GET /api/v1/projects/{name}/signals/stream?env=<env>.
//
// Lifecycle:
//  1. Auth + URL param validation (env required per memory feedback_j19_runtime_logs_stream_missing_env_param)
//  2. Project ownership check (cross-tenant 404 per existence-leak-prevention)
//  3. Flusher comma-ok cast (500 if missing, NOT panic)
//  4. Parse Last-Event-ID header for replay anchor
//  5. Loop (sse.Loop): GetSignalsAfter for THIS env + emit, with heartbeats
//     and bounded retry
//  6. Exit on ctx cancellation (client disconnect) OR maxDuration timeout
func (h *StatusSSEHandler) Get(w http.ResponseWriter, r *http.Request) {
	uc := auth.GetUserContext(r.Context())
	if uc == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("project name is required"))
		return
	}

	// EC absorption: env query param is REQUIRED per memory
	// feedback_j19_runtime_logs_stream_missing_env_param. Fail fast 400.
	env := r.URL.Query().Get("env")
	if env == "" {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("env query parameter is required"))
		return
	}

	// EC-7 absorption: project ownership check. Cross-tenant → 404 NOT 403
	// (existence-leak-prevention). Same pattern as status.go + metrics_proxy.go.
	project, err := h.projects.GetByName(r.Context(), uc.TenantID, name)
	if err != nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if project == nil {
		httpio.WriteLoopZeroError(w, loopzeroerrors.NewProjectNotFound(name))
		return
	}

	leave, ok := h.gate.Enter()
	if !ok {
		sse.WriteTooManyStreams(w)
		return
	}
	defer leave()

	// EC-5 absorption: a writer that cannot flush (some CDN wrappers) is an
	// honest 500, never a panic.
	stream, err := sse.Open(w)
	if err != nil {
		httpio.WriteJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "sse_flusher_unavailable",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.maxDuration)
	defer cancel()

	// EC-9: events with id <= lastID are never re-sent.
	lastID := parseSinceID(r)
	sendNew := func(ctx context.Context) (bool, error) {
		events, err := h.eventStore.GetSignalsAfter(ctx, uc.TenantID, name, env, lastID)
		if err != nil {
			return false, fmt.Errorf("reading signals after %d: %w", lastID, err)
		}
		for _, ev := range events {
			// Named `event: signal.changed` so the dashboard's
			// addEventListener receives it.
			if err := stream.SendEvent(ev); err != nil {
				return true, err
			}
			lastID = ev.ID
		}
		return false, nil
	}
	if _, err := sendNew(ctx); err != nil {
		h.logger.Warn("status_sse: initial replay failed", "err", err, "tenant_id", uc.TenantID, "project", name)
		return
	}
	loop := sse.Loop{Interval: h.pollInterval, Heartbeat: h.heartbeatInterval}
	if err := loop.Run(ctx, stream, sendNew); err != nil {
		h.logger.Warn("status_sse: stream closed", "err", err, "tenant_id", uc.TenantID, "project", name)
		return
	}
	h.logger.Info("status_sse: client disconnected or maxDuration reached", "tenant_id", uc.TenantID, "project", name)
}

// parseSinceID extracts the resume anchor from Last-Event-ID header OR
// ?since= query parameter (in that priority order). Returns 0 when neither
// is present OR the value is unparseable.
func parseSinceID(r *http.Request) int64 {
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			return id
		}
	}
	if v := r.URL.Query().Get("since"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			return id
		}
	}
	return 0
}
