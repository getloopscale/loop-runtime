// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/builds"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
	"github.com/getloopscale/loop-runtime/api/internal/timeline/aggregator"
)

// makeDeployTimelineSpawn returns the canonical spawner callback wired
// into BuildOrchestratorDeps. Per plan deploy-timeline-canonical-5-phases
// T5.1 the v1 production set includes ONLY the release_db source — the
// other 4 adapters (loki_markers, argocd_app, loopruntimeapp_informer,
// status_url_probe) land in the v1.1 follow-up per Open Questions.
//
// The release_db source alone delivers ~60% of the canonical timeline
// signal (release transitions: PENDING → ACTIVE → ROLLED_BACK), enough
// for the v1 dashboard hero card while the remaining adapters mature.
func makeDeployTimelineSpawn(eventStore *store.PostgresEventStore) builds.DeployTimelineSpawner {
	if eventStore == nil {
		return nil
	}
	return func(ctx context.Context, deployID string) {
		sink := aggregator.NewSSEEventSink(eventStore)
		// v2 querier: subscribe to sse_events (the canonical orchestrator
		// already emits build.started/build.succeeded/deploy.delivering/
		// deploy.verifying/deploy.live/deploy.completed) and translate
		// each into FSM-strict 5-phase transitions. Replaces the v1
		// wall-clock ladder that emitted live.completed prematurely.
		reader := aggregator.NewReleaseDBReader(&sseTimelineQuerier{eventStore: eventStore})
		aggregator.SpawnFor(ctx, deployID, sink, []aggregator.SourceReader{reader})
		slog.Default().Info("deploy_timeline aggregator spawned",
			"deploy_id", deployID, "sources", 1)
	}
}

// sseTimelineQuerier satisfies the aggregator's ReleaseStatusQuerier by
// reading the deploy's own sse_events stream: each real orchestrator event
// (build.started, build.succeeded, deploy.delivering, ...) is translated by
// eventTypeToCanonicalTransitions into the in-order 5-phase transitions it
// implies, queued, and handed out one per query. It reads no release rows.
//
// Why events and not release rows: the release-row path
// (FindReleaseForBuild) needs a tenant_id the spawn site does not carry, and
// guessing release-phase states ahead of the row made the FSM jump phases —
// LIVE-caught 2026-06-18 as 119 DEGRADED events per build. An earlier
// version stood in for the missing signal with a wall-clock ladder; the event
// subscription replaced it.
type sseTimelineQuerier struct {
	eventStore timelineEventReader
	mu         sync.Mutex
	pending    []timelineTransition
	seen       map[string]bool
	afterID    int64
}

// eventTypeToCanonicalTransitions maps a real orchestrator event into
// the canonical 5-phase FSM transitions it implies. Each event can
// trigger 1-3 transitions because some real events compress multiple
// FSM phases (e.g., build.succeeded means Build was done AND Sign ran
// AND Sign is done — the canonical orchestrator doesn't emit a
// separate "sign started" event because sign happens inline at
// build completion).
//
// Returned state names are mapped by releaseStateProjection
// (api/internal/timeline/aggregator/adapter_release_db.go) to the canonical
// (phase, status) FSM tuple.
func eventTypeToCanonicalTransitions(eventType string) []string {
	switch eventType {
	case "build.started":
		return []string{"BUILD_IN_PROGRESS"}
	case "build.succeeded":
		// Build done. Sign happens inline as part of the canonical
		// build pipeline (cosign-sign + sbom-app steps fire during
		// the build workflow — surfaced by the J-17 stream). For the
		// canonical 5-phase timeline we collapse: BUILD_COMPLETED
		// implies Sign immediately starts and immediately completes.
		return []string{"BUILD_COMPLETED", "SIGN_IN_PROGRESS", "SIGN_COMPLETED"}
	case "build.failed":
		// Canonical build-phase terminal failure. Sponsor LIVE-caught
		// 2026-06-18: previously returned [] on the false premise that
		// "terminal failure surfaces via existing build SSE" — the
		// deploy timeline hero ONLY consumes the aggregator, so the
		// empty mapping froze the hero in BUILD_IN_PROGRESS while the
		// DB had status=FAILED for minutes (visually "Deploying /
		// Awaiting events" forever). BUILD_FAILED projects to phase=
		// build, status=failed, terminal=true so the hero renders the
		// failed step + the cascading downstream phases stay pending.
		return []string{"BUILD_FAILED"}
	case "build.cancelled":
		// Same canonical fix as build.failed but for user/operator
		// cancellation. Phase=build, status=canceled, terminal=true.
		return []string{"BUILD_CANCELLED"}
	case "deploy.delivering":
		// Manifests are being generated + pushed to gitops. This is
		// the canonical Release phase.
		return []string{"PENDING"} // → release/in_progress
	case "deploy.verifying":
		// Release done; the deploy is now in flight (ArgoCD sync +
		// pod up). This is the canonical Deploy phase.
		return []string{"ACTIVE", "DEPLOY_IN_PROGRESS"}
	case "deploy.live":
		// Pods are up + healthy. Live phase begins probing the URL.
		return []string{"DEPLOY_COMPLETED", "LIVE_IN_PROGRESS"}
	case "deploy.completed":
		// Live URL responds 200. Canonical terminal.
		return []string{"LIVE_COMPLETED"}
	case "deploy.sync_failed":
		// The sync did not confirm the release. Without this the deploy
		// phase stayed in progress for good, next to a release in
		// SYNC_FAILED. The sync can fail before deploy.verifying announced
		// the deploy phase, so the phase is opened first; a state already
		// emitted is skipped by the querier.
		return []string{"ACTIVE", "DEPLOY_IN_PROGRESS", "DEPLOY_FAILED"}
	}
	return nil
}

// timelineEventReader reads the events of one deploy in the order they were
// written.
type timelineEventReader interface {
	GetAfter(ctx context.Context, buildID string, afterID int64) ([]ports.SSEEvent, error)
}

// timelineTransition is one state waiting to be handed to the aggregator, with
// the reason a failure carries.
type timelineTransition struct {
	state, reason string
}

// timelineFailureReason reads the reason of the event behind a failed state.
// Other states carry none, and data that is not the expected object gives "".
func timelineFailureReason(state string, data []byte) string {
	if state != "DEPLOY_FAILED" {
		return ""
	}
	var payload struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return ""
	}
	return payload.Reason
}

// next hands out the oldest pending transition as a release status row.
func (r *sseTimelineQuerier) next() *aggregator.ReleaseStatusRow {
	transition := r.pending[0]
	r.pending = r.pending[1:]
	return &aggregator.ReleaseStatusRow{
		State:     transition.state,
		Reason:    transition.reason,
		UpdatedAt: time.Now().UTC(),
	}
}

func (r *sseTimelineQuerier) QueryReleaseStatus(ctx context.Context, deployID string) (*aggregator.ReleaseStatusRow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen == nil {
		r.seen = make(map[string]bool)
	}

	// Drain any pending transition first so the FSM walks one rung per
	// poll tick (matches the strict in-order FSM contract).
	if len(r.pending) > 0 {
		return r.next(), nil
	}

	// No pending → poll sse_events for new events since the last seen
	// id. The canonical orchestrator already emits build.started /
	// build.succeeded / deploy.delivering / deploy.verifying /
	// deploy.live / deploy.completed — translate each into the
	// canonical 5-phase FSM transitions it implies.
	events, err := r.eventStore.GetAfter(ctx, deployID, r.afterID)
	if err != nil {
		return nil, err
	}
	for _, ev := range events {
		if ev.ID > r.afterID {
			r.afterID = ev.ID
		}
		for _, st := range eventTypeToCanonicalTransitions(ev.EventType) {
			if !r.seen[st] {
				r.seen[st] = true
				r.pending = append(r.pending, timelineTransition{state: st, reason: timelineFailureReason(st, ev.Data)})
			}
		}
	}
	if len(r.pending) > 0 {
		return r.next(), nil
	}
	return nil, nil
}
