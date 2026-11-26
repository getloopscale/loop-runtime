// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/events"
	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// CancelBuild cancels a running or queued build.
//
// All state mutations happen INSIDE the environment lock (Rule 6).
//
// Steps:
//  1. Get build by ID -- verify it exists and get lock coordinates
//  2. Acquire environment lock on (tenant, project, environment)
//  3. Re-fetch build inside lock (state may have changed)
//  4. If build is terminal -> return ErrBuildAlreadyCompleted
//  5. Transition to CANCELLING (if RUNNING) or CANCELLED (if QUEUED) via state machine
//  6. Cancel the Argo Workflow via WorkflowSubmitter.Cancel
//  7. If was RUNNING (now CANCELLING), confirm cancellation -> CANCELLED
//  8. Emit SSE build.cancelled event
func (o *BuildOrchestrator) CancelBuild(ctx context.Context, buildID string) error {
	// Step 1: get build to determine lock coordinates (tenant, project, env)
	build, err := o.buildStore.GetByID(ctx, buildID)
	if err != nil {
		return fmt.Errorf("cancel build: get build: %w", err)
	}

	// Check if already terminal before acquiring lock (fast path, no race risk)
	if statemachine.IsBuildPhaseTerminal(statemachine.BuildPhase(build.Status)) {
		return loopzeroerrors.ErrBuildAlreadyCompleted
	}

	// Step 2: acquire environment lock -- all state mutations inside (Rule 6)
	return o.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		// Step 3: re-fetch inside lock (state may have changed between pre-check and lock acquisition)
		build, err = o.buildStore.GetByID(ctx, buildID)
		if err != nil {
			return fmt.Errorf("cancel build: re-fetch build: %w", err)
		}

		// Step 4: re-check terminal state inside lock
		if statemachine.IsBuildPhaseTerminal(statemachine.BuildPhase(build.Status)) {
			return loopzeroerrors.ErrBuildAlreadyCompleted
		}

		// Step 5: transition via state machine: BuildCancelRequested
		// QUEUED -> CANCELLED (direct), RUNNING -> CANCELLING
		updatedBuild, err := o.buildStore.UpdateStatus(ctx, tx, buildID, string(statemachine.BPEventCancelRequested))
		if err != nil {
			return fmt.Errorf("cancel build: transition cancel requested: %w", err)
		}

		// Step 6: cancel the workflow (best-effort -- workflow may already be done)
		if build.WorkflowID != "" {
			if cancelErr := o.workflow.Cancel(ctx, "argo", build.WorkflowID); cancelErr != nil {
				slog.Warn("workflow cancel failed (best-effort)", "workflow_id", build.WorkflowID, "error", cancelErr)
			}
		}

		// Step 7: if transitioned to CANCELLING (was RUNNING), confirm cancellation
		if statemachine.BuildPhase(updatedBuild.Status) == statemachine.BPhCancelling {
			if _, err := o.buildStore.UpdateStatus(ctx, tx, buildID, string(statemachine.BPEventCancelConfirmed)); err != nil {
				return fmt.Errorf("cancel build: confirm cancellation: %w", err)
			}
		}

		// Step 8: emit SSE build.cancelled
		eventData := events.SafeJSONMarshal(map[string]string{
			"build_id": buildID,
			"status":   "cancelled",
		})
		if _, err := o.eventStore.Append(ctx, buildID, "build.cancelled", eventData); err != nil {
			return fmt.Errorf("cancel build: emit SSE event: %w", err)
		}

		return nil
	})
}
