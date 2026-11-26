// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package timelinehttp — deploy timeline SSE handler (T4.2 of Phase D plan).
//
// Built on the shared stream primitive (api/internal/sse/stream.go):
//   - One event scope: deploy_id (vs build_id + project for J-17 stream)
//   - Terminal events: deploy_timeline.live.completed | *.failed | *.canceled
//   - Tenant filter at store layer (EC-3 / ADR D8 defense-in-depth)
//   - 2s default poll, a flush per event (proxies buffer until EOF
//     otherwise), heartbeats and bounded retry from sse.Loop
//   - 30min cap per ADR D6 (orphan stream guard)
//
// Package timeline serves GET /api/v1/deploys/{deployID}/timeline: the ordered
// events of one deploy.
//
// Twenty-fourth family to leave the flat `routes` package (B-080, slice-40).
//
// It registers its route through a helper (RegisterDeployTimelineRoute) rather
// than by a literal in router.go, and moving that helper broke the spec⟺router
// conformance test — which scanned the routes directory SHALLOWLY and stopped
// seeing the file that spells the path. The route was registered the whole time;
// the scan had stopped reaching it. Fixed by walking the tree.
package timelinehttp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/getloopscale/loop-runtime/api/internal/sse"
	"github.com/getloopscale/loop-runtime/api/internal/timeline"
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
type DeployTimelineEvent = timeline.DeployTimelineEvent

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
var ErrDeployNotFound = timeline.ErrDeployNotFound

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
	gate         *sse.Gate
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

// NewDeployTimelineHandler constructs the handler with sane defaults.
func NewDeployTimelineHandler(store DeployTimelineEventStore, opts ...DTOption) *DeployTimelineHandler {
	h := &DeployTimelineHandler{
		store:        store,
		pollInterval: 2 * time.Second,
		maxDuration:  30 * time.Minute,
		gate:         sse.NewGate(0),
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

	leave, ok := h.gate.Enter()
	if !ok {
		sse.WriteTooManyStreams(w)
		return
	}
	defer leave()

	stream, err := sse.Open(w)
	if err != nil {
		http.Error(w, `{"error":"streaming unsupported"}`, http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.maxDuration)
	defer cancel()

	// Clients reconnecting resume after the last id they saw.
	afterID := sse.LastEventID(r)
	sendNew := func(ctx context.Context) (bool, error) {
		events, err := h.store.GetAfterForDeploy(ctx, tenantID, deployID, afterID)
		if err != nil {
			return false, fmt.Errorf("reading timeline after %d: %w", afterID, err)
		}
		for _, ev := range events {
			// Always advance, so skipped shared-table rows are not re-read.
			afterID = ev.ID
			// The table is shared with build.step.* / build.started: the
			// dashboard only understands deploy_timeline.*.
			if !strings.HasPrefix(ev.EventType, deployTimelineEventTypePrefix) {
				continue
			}
			if err := stream.Send(strconv.FormatInt(ev.ID, 10), ev.EventType, ev.Data); err != nil {
				return true, err
			}
			if deployTimelineTerminalEvents[ev.EventType] {
				return true, nil
			}
		}
		return false, nil
	}

	if done, err := sendNew(ctx); err != nil || done {
		if err != nil {
			h.logger.Warn("initial poll error", "err", err)
		}
		return
	}
	loop := sse.Loop{Interval: h.pollInterval, Heartbeat: deployTimelineHeartbeat}
	if err := loop.Run(ctx, stream, sendNew); err != nil {
		h.logger.Warn("timeline stream closed", "err", err, "deploy_id", deployID)
	}
}

// deployTimelineHeartbeat keeps idle proxies from closing a quiet timeline.
const deployTimelineHeartbeat = 15 * time.Second
