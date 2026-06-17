// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routes

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/getloopscale/loopzero/api/internal/middleware"
	"github.com/getloopscale/loopzero/api/internal/sse"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
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
//   - EC-2: filters event_type=signal.changed (skips J-17 deploy events in shared sse_events table)
//   - EC-5: Flusher comma-ok form (500 on missing, NOT panic)
//   - EC-7: cross-tenant returns 404 NOT_FOUND (existence-leak-prevention)
//   - EC-9: Last-Event-ID dedup by id (replay > lastID; client SSE native handles reconnect)

// StatusSSEEventStore is the minimal interface StatusSSEHandler needs from
// sse.EventStore. ISP — handler depends on what it actually uses.
type StatusSSEEventStore interface {
	GetAfterForProject(ctx context.Context, tenantID string, projectName string, afterID int64) ([]sse.SSEEvent, error)
}

// StatusSSEHandler handles GET /api/v1/projects/{name}/signals/stream.
type StatusSSEHandler struct {
	projects   StatusProjectStore
	eventStore StatusSSEEventStore
	// pollInterval controls how often the handler re-queries GetAfterForProject
	// when no events arrived since the last poll. Default 2s. Test-tunable.
	pollInterval time.Duration
	// maxDuration caps the handler's loop lifetime so a single client
	// can't hold a connection open forever. Default 30min. Test-tunable.
	maxDuration time.Duration
	logger      *slog.Logger
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
		projects:     projects,
		eventStore:   eventStore,
		pollInterval: poll,
		maxDuration:  maxDur,
		logger:       logger,
	}
}

// Get handles GET /api/v1/projects/{name}/signals/stream?env=<env>.
//
// Lifecycle:
//  1. Auth + URL param validation (env required per memory feedback_j19_runtime_logs_stream_missing_env_param)
//  2. Project ownership check (cross-tenant 404 per existence-leak-prevention)
//  3. Flusher comma-ok cast (500 if missing, NOT panic)
//  4. Parse Last-Event-ID header for replay anchor
//  5. Loop: GetAfterForProject + filter event_type=signal.changed + emit + flush
//  6. Exit on ctx cancellation (client disconnect) OR maxDuration timeout
func (h *StatusSSEHandler) Get(w http.ResponseWriter, r *http.Request) {
	uc := middleware.GetUserContext(r.Context())
	if uc == nil {
		writeLoopZeroError(w, loopzeroerrors.ErrUnauthorized)
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("project name is required"))
		return
	}

	// EC absorption: env query param is REQUIRED per memory
	// feedback_j19_runtime_logs_stream_missing_env_param. Fail fast 400.
	env := r.URL.Query().Get("env")
	if env == "" {
		writeLoopZeroError(w, loopzeroerrors.ErrMissingField.Wrap("env query parameter is required"))
		return
	}

	// EC-7 absorption: project ownership check. Cross-tenant → 404 NOT 403
	// (existence-leak-prevention). Same pattern as status.go + metrics_proxy.go.
	project, err := h.projects.GetByName(r.Context(), uc.TenantID, name)
	if err != nil {
		writeLoopZeroError(w, loopzeroerrors.ErrInternalError)
		return
	}
	if project == nil {
		writeLoopZeroError(w, loopzeroerrors.NewProjectNotFound(name))
		return
	}

	// EC-5 absorption: Flusher cast with comma-ok form. Production gateways
	// (CDN/CloudFront) sometimes wrap the writer; if Flusher unavailable,
	// return 500 honest — NOT panic per memory
	// feedback_loopzero_cloud_sse_proxy_buffers_until_eof.
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "sse_flusher_unavailable",
		})
		return
	}

	// Parse Last-Event-ID header (browser-native EventSource sends this on
	// reconnect). Falls back to ?since= query param for clients that can't
	// set headers. EC-9 dedup: events with id <= lastID are skipped.
	lastID := parseSinceID(r)

	// SSE headers — must precede first Flush.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering
	w.WriteHeader(http.StatusOK)
	// Initial flush sends headers immediately so client knows connection is open.
	flusher.Flush()

	ctx, cancel := context.WithTimeout(r.Context(), h.maxDuration)
	defer cancel()

	ticker := time.NewTicker(h.pollInterval)
	defer ticker.Stop()

	// Replay events with id > lastID, then poll for new ones.
	if err := h.emitPending(ctx, w, flusher, uc.TenantID, name, &lastID); err != nil {
		h.logger.Warn("status_sse: initial replay failed",
			"err", err, "tenant_id", uc.TenantID, "project", name)
		return
	}

	for {
		select {
		case <-ctx.Done():
			h.logger.Info("status_sse: client disconnected or maxDuration reached",
				"tenant_id", uc.TenantID, "project", name)
			return
		case <-ticker.C:
			if err := h.emitPending(ctx, w, flusher, uc.TenantID, name, &lastID); err != nil {
				h.logger.Warn("status_sse: poll failed; closing stream",
					"err", err, "tenant_id", uc.TenantID, "project", name)
				return
			}
		}
	}
}

// emitPending queries GetAfterForProject + filters event_type=signal.changed +
// emits each as SSE wire format + advances lastID.
func (h *StatusSSEHandler) emitPending(
	ctx context.Context,
	w http.ResponseWriter,
	flusher http.Flusher,
	tenantID, projectName string,
	lastID *int64,
) error {
	events, err := h.eventStore.GetAfterForProject(ctx, tenantID, projectName, *lastID)
	if err != nil {
		return fmt.Errorf("event store query: %w", err)
	}
	for _, ev := range events {
		// EC-2 absorption: skip non-signal.changed events (sse_events table is
		// shared with J-17 deploy events; downstream useStatusStream consumer
		// only handles signal.changed; emit only what the consumer expects).
		if ev.EventType != "signal.changed" {
			// Advance lastID anyway so we don't re-query the skipped event.
			if ev.ID > *lastID {
				*lastID = ev.ID
			}
			continue
		}
		// SSE wire format per memory
		// feedback_eventsource_named_events_vs_onmessage_2026_06_13: emit as
		// named `event: signal.changed` so frontend addEventListener works.
		fmt.Fprintf(w, "event: signal.changed\ndata: %s\nid: %d\n\n", string(ev.Data), ev.ID)
		flusher.Flush()
		if ev.ID > *lastID {
			*lastID = ev.ID
		}
	}
	return nil
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
