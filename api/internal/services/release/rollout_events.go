// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

// rollout_events.go — argo-rollouts-progressive-delivery-plan T4.1.
//
// Helpers for emitting SSE events during canary rollout progression.
//
// Event taxonomy:
//   - rollout.step  (NON-TERMINAL): emitted when canary advances to a new
//     step. Stream stays open. CLI shows progress.
//   - rollout.aborted (TERMINAL): emitted when Argo Rollouts auto-aborts a
//     canary (analysis failed or bad pod health). Replaces deploy.completed
//     in the abort path. Stream closes; CLI shows error and exits with
//     code 3 (deploy failure).
//
// Schema (rollout.step.data):
//   {
//     "weight": 20,           // current canary traffic weight (0-100)
//     "step": 3,              // 1-indexed current step
//     "total_steps": 5,       // total step count from canary spec
//     "status": "analyzing",  // "progressing" | "paused" | "analyzing"
//     "analysis_status": "running"  // optional: "running"|"successful"|"failed"
//   }
//
// Schema (rollout.aborted.data):
//   {
//     "reason": "analysis-failed",  // "analysis-failed" | "pod-unhealthy" | "user-aborted"
//     "step": 3,
//     "weight": 20,
//     "message": "success-rate dropped below 0.90 threshold"
//   }

// RolloutStepEvent is the JSON payload of an SSE rollout.step event.
type RolloutStepEvent struct {
	Weight         int    `json:"weight"`
	Step           int    `json:"step"`
	TotalSteps     int    `json:"total_steps"`
	Status         string `json:"status"`
	AnalysisStatus string `json:"analysis_status,omitempty"`
}

// RolloutAbortedEvent is the JSON payload of an SSE rollout.aborted event.
type RolloutAbortedEvent struct {
	Reason  string `json:"reason"`
	Step    int    `json:"step"`
	Weight  int    `json:"weight"`
	Message string `json:"message"`
}

// EmitRolloutStep emits a non-terminal rollout.step event. Idempotent:
// repeated calls with the same weight/step/status produce duplicate events
// — callers (release convergence loop) should deduplicate by polling
// only on transitions.
func EmitRolloutStep(
	ctx context.Context,
	logger *slog.Logger,
	eventStore ReleaseActivationEventStore,
	buildID string,
	ev RolloutStepEvent,
) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal rollout.step: %w", err)
	}
	if _, err := eventStore.Append(ctx, buildID, "rollout.step", data); err != nil {
		if logger != nil {
			logger.Error("failed to emit rollout.step",
				"build_id", buildID,
				"weight", ev.Weight,
				"step", ev.Step,
				"error", err)
		}
		return fmt.Errorf("append rollout.step: %w", err)
	}
	return nil
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
