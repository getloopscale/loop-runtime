// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package aggregator

import (
	"context"
	"log/slog"

	"github.com/getloopscale/loopzero/api/internal/scrub"
)

// SpawnFor is the canonical spawn helper for the per-deploy aggregator
// goroutine. The orchestrator (BuildOrchestrator.SubmitBuild) calls this
// after a build transitions to RUNNING, passing the deploy_id, the
// canonical SSE event sink, and the configured set of source readers.
//
// The spawn is fire-and-forget — the goroutine owns its own lifecycle
// and exits when the FSM reaches a terminal state OR ctx is canceled.
// Callers that need to wait on completion should chain their own ctx
// cancel after observing the terminal event via the SSE handler.
//
// Plan T5.1 production wiring — this helper exists so the spawn-site
// integration is a single canonical call rather than ad-hoc goroutine
// scaffolding in every caller. Test stubs can replace the helper to
// inject hermetic readers.
func SpawnFor(ctx context.Context, deployID string, sink EventSink, readers []SourceReader) {
	if sink == nil || len(readers) == 0 {
		slog.Default().With("component", "deploy_timeline_aggregator", "deploy_id", deployID).
			Warn("SpawnFor called with nil sink or empty readers — aggregator not started")
		return
	}
	agg := New(deployID, sink, scrub.ScrubEventWithCount, readers)
	go agg.Run(ctx)
}
