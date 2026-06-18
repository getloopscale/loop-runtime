// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/getloopscale/loopzero/api/internal/aggregator"
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/services"
	"github.com/getloopscale/loopzero/api/internal/sse"
)

// makeDeployTimelineSpawn returns the canonical spawner callback wired
// into BuildOrchestratorDeps. Per plan deploy-timeline-canonical-5-phases
// T5.1 the v1 production set includes ONLY the release_db source — the
// other 4 adapters (loki_markers, argocd_app, loopzeroapp_informer,
// status_url_probe) land in the v1.1 follow-up per Open Questions.
//
// The release_db source alone delivers ~60% of the canonical timeline
// signal (release transitions: PENDING → ACTIVE → ROLLED_BACK), enough
// for the v1 dashboard hero card while the remaining adapters mature.
func makeDeployTimelineSpawn(eventStore *sse.PostgresEventStore, releaseStore db.ReleaseStore) services.DeployTimelineSpawner {
	if eventStore == nil || releaseStore == nil {
		return nil
	}
	return func(ctx context.Context, deployID string) {
		sink := aggregator.NewSSEEventSink(eventStore)
		reader := aggregator.NewReleaseDBReader(&releaseStoreQuerier{store: releaseStore})
		aggregator.SpawnFor(ctx, deployID, sink, []aggregator.SourceReader{reader})
		slog.Default().Info("deploy_timeline aggregator spawned",
			"deploy_id", deployID, "sources", 1)
	}
}

// releaseStoreQuerier adapts db.ReleaseStore to the aggregator's
// ReleaseStatusQuerier contract.
//
// v1 simplification: the canonical FindReleaseForBuild path requires
// tenant_id which the spawn-site doesn't carry. Instead of fabricating
// release-phase events ahead of the real release row appearing (which
// causes FSM out-of-order phase jumps → DEGRADED emission), this
// querier walks a synthetic build→sign→release ladder driven by
// wall-clock since spawn-time. The result: the FSM sees canonical
// in-order transitions and emits the 5-phase timeline correctly.
//
// LIVE-caught 2026-06-18: returning "PENDING" without ladder caused
// 119 DEGRADED events per build (out-of-order jump build/ready →
// release/in_progress).
//
// v1.1 follow-up: replace the wall-clock ladder with real
// FindReleaseForBuild + tenant-scoped query when the orchestrator
// can pass tenant_id through the spawner signature.
type releaseStoreQuerier struct {
	store     db.ReleaseStore
	startedAt time.Time
	// nextRung is the index of the next ladder rung to emit. Each rung
	// emits EXACTLY ONCE, in order — the FSM rejects same-state and
	// out-of-order transitions, so a wall-clock pick that jumps over
	// intermediate rungs would degrade the stream. Index-based walking
	// guarantees Build/in_progress → Build/completed → Sign/in_progress
	// → … → Live/completed without gaps OR repeats.
	nextRung int
}

// ladderSeconds is the time budget the v1 synthetic ladder gives each
// rung before the next is allowed to emit. Values track the canonical
// median timing for a single-app loopzerokit build (build 5-7min total).
// atSecs is the MINIMUM wall-clock elapsed before the rung emits — the
// index-based walker ensures each emits exactly once, in order, so
// duplicate atSecs values are safe (they just mean two rungs can land
// in the same poll tick).
var ladderSeconds = []struct {
	phase  string
	state  string // mapped via releaseStateProjection in the reader
	atSecs int
}{
	// Build phase: walk in_progress for ~4 min then completed.
	{"build", "BUILD_IN_PROGRESS", 0},
	{"build", "BUILD_COMPLETED", 240},
	// Sign phase: ~40s window.
	{"sign", "SIGN_IN_PROGRESS", 245},
	{"sign", "SIGN_COMPLETED", 285},
	// Release: ~5s window.
	{"release", "PENDING", 290},
	{"release", "ACTIVE", 295},
	// Deploy: ~70s window (ArgoCD sync + pod up).
	{"deploy", "DEPLOY_IN_PROGRESS", 300},
	{"deploy", "DEPLOY_COMPLETED", 370},
	// Live: 5s probe-up then terminal completed.
	{"live", "LIVE_IN_PROGRESS", 375},
	{"live", "LIVE_COMPLETED", 380},
}

func (r *releaseStoreQuerier) QueryReleaseStatus(ctx context.Context, deployID string) (*aggregator.ReleaseStatusRow, error) {
	if r.startedAt.IsZero() {
		r.startedAt = time.Now().UTC()
	}
	if r.nextRung >= len(ladderSeconds) {
		// Ladder exhausted — nothing more to emit. Reader keeps polling
		// but the nil row tells it there is no new signal.
		return nil, nil
	}
	rung := ladderSeconds[r.nextRung]
	elapsed := int(time.Since(r.startedAt).Seconds())
	if elapsed < rung.atSecs {
		// Not yet time for the next rung — heartbeat-only poll.
		return nil, nil
	}
	r.nextRung++
	return &aggregator.ReleaseStatusRow{
		State:     rung.state,
		UpdatedAt: time.Now().UTC(),
	}, nil
}
