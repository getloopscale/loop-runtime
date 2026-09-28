// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

// rollout_events.go — argo-rollouts-progressive-delivery-plan T4.1.
//
// rollout.aborted (TERMINAL) is emitted when a canary release fails: it
// replaces deploy.completed in the abort path, the stream closes, and the CLI
// exits with code 3 (deploy failure).
//
// Schema (rollout.aborted.data):
//   {
//     "reason": "analysis-failed",  // "analysis-failed" | "pod-unhealthy" | "user-aborted"
//     "step": 3,
//     "weight": 20,
//     "message": "success-rate dropped below 0.90 threshold"
//   }
//
// The plan also specified a non-terminal rollout.step progress event, and the
// CLI renders one if it arrives, but nothing in the API observes canary step
// progression, so nothing emits it. Its helper had no caller and was removed;
// emitting rollout.step needs a producer that watches Rollout steps first.

// RolloutAbortedEvent is the JSON payload of an SSE rollout.aborted event.
type RolloutAbortedEvent struct {
	Reason  string `json:"reason"`
	Step    int    `json:"step"`
	Weight  int    `json:"weight"`
	Message string `json:"message"`
}

// EmitRolloutAborted emits a TERMINAL rollout.aborted event. Causes the SSE
// stream to close (registered as terminal in stream_handler.go). CLI exits
// with code 3 (deploy failure).
func EmitRolloutAborted(
	ctx context.Context,
	logger *slog.Logger,
	eventStore ReleaseActivationEventStore,
	buildID string,
	ev RolloutAbortedEvent,
) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal rollout.aborted: %w", err)
	}
	if _, err := eventStore.Append(ctx, buildID, "rollout.aborted", data); err != nil {
		if logger != nil {
			logger.Error("failed to emit rollout.aborted",
				"build_id", buildID,
				"reason", ev.Reason,
				"error", err)
		}
		return fmt.Errorf("append rollout.aborted: %w", err)
	}
	return nil
}
