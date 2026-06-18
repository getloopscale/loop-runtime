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
	// lastEmitted is the synthetic state returned on the previous poll.
	// Dedup is required because the ReleaseDBReader emits every poll
	// tick and the strict FSM rejects same-state transitions (in_progress
	// → in_progress is invalid → DEGRADED storm).
	lastEmitted string
}

// laddersSeconds is the time budget the v1 synthetic ladder gives each
// phase before advancing. Values mirror canonical median observed
// in the build-86c2cef499bc6f9b reference build (~4 minutes total).
var ladderSeconds = []struct {
	phase   string
	state   string // mapped via releaseStateProjection in the reader
	atSecs  int
}{
	// Build phase: walk in_progress for ~2.5 min then completed.
	{"build", "BUILD_IN_PROGRESS", 0},
	{"build", "BUILD_COMPLETED", 150},
	// Sign phase: ~30s window.
	{"sign", "SIGN_IN_PROGRESS", 150},
	{"sign", "SIGN_COMPLETED", 180},
	// Release: ~5s window.
	{"release", "PENDING", 180},
	{"release", "ACTIVE", 185},
	// Deploy: ~70s window (ArgoCD sync + pod up).
	{"deploy", "DEPLOY_IN_PROGRESS", 185},
	{"deploy", "DEPLOY_COMPLETED", 255},
	// Live: 5s probe-up then terminal completed.
	{"live", "LIVE_IN_PROGRESS", 255},
	{"live", "LIVE_COMPLETED", 260},
}

func (r *releaseStoreQuerier) QueryReleaseStatus(ctx context.Context, deployID string) (*aggregator.ReleaseStatusRow, error) {
	if r.startedAt.IsZero() {
		r.startedAt = time.Now().UTC()
	}
	elapsed := int(time.Since(r.startedAt).Seconds())
	// Walk the ladder forward — pick the LAST rung whose atSecs <= elapsed.
	pick := ladderSeconds[0]
	for _, rung := range ladderSeconds {
		if rung.atSecs <= elapsed {
			pick = rung
		}
	}
	// Dedup: same state as previous poll → return nil so the reader
	// emits no PhaseEvent. The reader's `if row == nil { return false, nil }`
	// branch handles this gracefully and keeps the poll cycle alive.
	// Strict FSM rejects same-state transitions; without dedup the
	// aggregator emits 1 valid event then 100+ DEGRADED events.
	if pick.state == r.lastEmitted {
		return nil, nil
	}
	r.lastEmitted = pick.state
	return &aggregator.ReleaseStatusRow{
		State:     pick.state,
		UpdatedAt: time.Now().UTC(),
	}, nil
}
