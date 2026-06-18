// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package routes — deploy timeline SSE handler (T4.2 of Phase D plan).
//
// Mirrors api/internal/sse/stream_handler.go shape with simplifications:
//   - One event scope: deploy_id (vs build_id + project for J-17 stream)
//   - Terminal events: deploy_timeline.live.completed | *.failed | *.canceled
//   - Tenant filter at store layer (EC-3 / ADR D8 defense-in-depth)
//   - 2s default poll + Flusher per chunk (SSE buffer trap from
//     feedback_loopzero_cloud_sse_proxy_buffers_until_eof.md)
//   - 30min cap per ADR D6 (orphan stream guard)
package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// deployTimelineEventTypePrefix is the canonical filter — the underlying
// sse_events table is shared with the J-17 build.step.* stream, so the
// handler MUST skip every event whose type does not begin with this
// prefix. LIVE caught 2026-06-18: a real build's stream returned
// build.started + build.step.* events that the dashboard would render
// with the wrong shape.
const deployTimelineEventTypePrefix = "deploy_timeline."

// DeployTimelineEvent is the canonical SSE-event row consumed by the
// timeline handler. ID is monotonic per (tenant, deploy). EventType
// follows convention "deploy_timeline.<phase>.<status>".
type DeployTimelineEvent struct {
	ID        int64           `json:"id"`
	EventType string          `json:"event_type"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
}

// DeployTimelineEventStore is the read-side contract the handler uses.
// Write side (Aggregator.AppendDeployTimelineEvent) lives in the
// aggregator package via EventSink; both back onto the same Postgres
// table via T5.1 wiring.
type DeployTimelineEventStore interface {
	// DeployTenantID resolves the canonical tenant_id for a deploy.
	// Returns ErrDeployNotFound when the deploy does not exist (used
	// by the handler to emit 404 without leaking existence).
	DeployTenantID(ctx context.Context, deployID string) (string, error)
	// GetAfterForDeploy returns events with id > afterID for the
	// (tenant, deploy) pair. MUST enforce tenant filter in SQL —
	// defense-in-depth even though the handler also checks (ADR D8).
	GetAfterForDeploy(ctx context.Context, tenantID, deployID string, afterID int64) ([]DeployTimelineEvent, error)
}

// ErrDeployNotFound is the sentinel for unknown deploy_id OR
// tenant-mismatched deploy_id. Handler renders 404 in both cases to
// avoid leaking deploy existence to non-owners.
var ErrDeployNotFound = errors.New("deploy timeline: not found")

// Terminal event-type set per ADR D1 — stream closes after observing one.
var deployTimelineTerminalEvents = map[string]bool{
	"deploy_timeline.live.completed":   true,
	"deploy_timeline.build.failed":     true,
	"deploy_timeline.sign.failed":      true,
	"deploy_timeline.release.failed":   true,
	"deploy_timeline.deploy.failed":    true,
	"deploy_timeline.live.failed":      true,
	"deploy_timeline.build.canceled":   true,
	"deploy_timeline.sign.canceled":    true,
	"deploy_timeline.release.canceled": true,
	"deploy_timeline.deploy.canceled":  true,
}

// deployTimelineTenantCtxKey is the context key for the resolved
// tenant_id (set by the auth middleware in production; injected by
// tests directly).
type ctxKey int

const (
	deployTimelineTenantCtxKey ctxKey = iota
)

// DeployTimelineHandler streams canonical timeline SSE events.
type DeployTimelineHandler struct {
	store        DeployTimelineEventStore
	pollInterval time.Duration
	maxDuration  time.Duration
	logger       *slog.Logger
}

// DTOption configures the handler.
type DTOption func(*DeployTimelineHandler)

// WithDTPollInterval overrides the poll interval (default 2s per plan T4.2).
func WithDTPollInterval(d time.Duration) DTOption {
	return func(h *DeployTimelineHandler) { h.pollInterval = d }
}

// WithDTMaxDuration overrides the orphan-stream cap (default 30min per ADR D6).
func WithDTMaxDuration(d time.Duration) DTOption {
	return func(h *DeployTimelineHandler) { h.maxDuration = d }
}

// WithDTLogger sets a structured logger.
func WithDTLogger(l *slog.Logger) DTOption {
	return func(h *DeployTimelineHandler) { h.logger = l }
}

// NewDeployTimelineHandler constructs the handler with sane defaults.
func NewDeployTimelineHandler(store DeployTimelineEventStore, opts ...DTOption) *DeployTimelineHandler {
	h := &DeployTimelineHandler{
		store:        store,
		pollInterval: 2 * time.Second,
		maxDuration:  30 * time.Minute,
		logger:       slog.Default().With("component", "deploy_timeline_sse"),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Stream is the chi.HandlerFunc wired at /api/v1/deploys/{deployID}/timeline.
func (h *DeployTimelineHandler) Stream(w http.ResponseWriter, r *http.Request) {
	deployID := chi.URLParam(r, "deployID")
	if deployID == "" {
		http.Error(w, `{"error":"missing deploy_id"}`, http.StatusBadRequest)
		return
	}
	tenantID, ok := r.Context().Value(deployTimelineTenantCtxKey).(string)
	if !ok || tenantID == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	ownerTenant, err := h.store.DeployTenantID(r.Context(), deployID)
	if err != nil {
		if errors.Is(err, ErrDeployNotFound) {
			http.Error(w, `{"error":"deploy not found"}`, http.StatusNotFound)
			return
		}
		h.logger.Error("DeployTenantID lookup", "err", err, "deploy_id", deployID)
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	if ownerTenant != tenantID {
		// Tenant mismatch → 404 (never 403; avoids leaking existence).
		http.Error(w, `{"error":"deploy not found"}`, http.StatusNotFound)
		return
	}

	// Last-Event-ID dedup — clients reconnecting resume from last seen ID.
	afterID := int64(0)
	if hdr := r.Header.Get("Last-Event-ID"); hdr != "" {
		if id, perr := strconv.ParseInt(hdr, 10, 64); perr == nil && id > 0 {
			afterID = id
		}
	}

	// SSE headers — Content-Type + no-buffer + flusher init.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, fok := w.(http.Flusher)
	if !fok {
		http.Error(w, `{"error":"streaming unsupported"}`, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx, cancel := context.WithTimeout(r.Context(), h.maxDuration)
	defer cancel()
	ticker := time.NewTicker(h.pollInterval)
	defer ticker.Stop()

	// Drain immediately + on every tick.
	pollAndEmit := func() (terminal bool, err error) {
		events, perr := h.store.GetAfterForDeploy(ctx, tenantID, deployID, afterID)
		if perr != nil {
			return false, perr
		}
		for _, ev := range events {
			// Always advance afterID so the sse_events scan does not
			// re-stream skipped rows on the next poll tick.
			afterID = ev.ID
			// Filter shared-table siblings (build.step.*, build.started)
			// so the dashboard only sees canonical deploy_timeline.*.
			if !strings.HasPrefix(ev.EventType, deployTimelineEventTypePrefix) {
				continue
			}
			if _, werr := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.ID, ev.EventType, string(ev.Data)); werr != nil {
				return false, werr
			}
			flusher.Flush()
			if deployTimelineTerminalEvents[ev.EventType] {
				return true, nil
			}
		}
		return false, nil
	}

	if term, perr := pollAndEmit(); perr != nil {
		h.logger.Warn("initial poll error", "err", perr)
		return
	} else if term {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			term, perr := pollAndEmit()
			if perr != nil {
				h.logger.Warn("poll error", "err", perr)
				return
			}
			if term {
				return
			}
		}
	}
}
