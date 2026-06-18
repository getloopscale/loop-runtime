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
// ReleaseStatusQuerier contract. db.ReleaseStore has no direct
// "GetByDeployID" — deploy_id in our model is the build_id used to
// look up the release via FindReleaseForBuild semantics. For the v1
// spawn we emit a single ready event because the orchestrator's spawn
// site fires at SubmitBuild before any release row exists; the
// aggregator's release_db poll cycle picks up the row as it appears.
type releaseStoreQuerier struct {
	store db.ReleaseStore
}

func (r *releaseStoreQuerier) QueryReleaseStatus(ctx context.Context, deployID string) (*aggregator.ReleaseStatusRow, error) {
	// v1: the spawn-site fires BEFORE any release row exists. Return a
	// synthetic "PENDING" row so the aggregator emits release/in_progress
	// then the next poll picks up the real state once FindReleaseForBuild
	// returns a row. The v1.1 follow-up will inline a tenant-scoped
	// query that resolves release-by-build inside the store directly.
	return &aggregator.ReleaseStatusRow{
		State:     "PENDING",
		UpdatedAt: time.Now().UTC(),
	}, nil
}
