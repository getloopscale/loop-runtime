// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/api/internal/events"
	"github.com/getloopscale/loop-runtime/api/internal/release"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// buildEventAppender appends an SSE event to a build's stream.
type buildEventAppender interface {
	Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
}

// buildFinalizer is the one implementation of taking a build to a terminal
// phase under its environment lock. The success skeleton used to be written
// three times (OnBuildCompleted, RetryReleaseActivation, completeBuild) and the
// failure skeleton three more (OnBuildCompleted, failBuild, the reaper), and
// the copies had drifted: the error message was fatal in two and not in the
// third, and build.failed was appended inside the lock in one and after it in
// the others.
type buildFinalizer struct {
	builds       db.BuildStore
	locker       deploycontracts.EnvironmentLocker
	sse          buildEventAppender
	deployEvents store.DeployEventStore // nil = the deploy.failed outbox emit is skipped
	logger       *slog.Logger
}

// buildFailure describes how a build fails.
type buildFailure struct {
	// reason is persisted as the build's error message and carried by the
	// build.failed event. Empty means the failure carried no message.
	reason string
	// route returns the machine events that take a build in phase to its
	// terminal state; it runs under the lock, on the freshly read phase.
	route func(phase statemachine.BuildPhase) ([]statemachine.BuildPhaseEvent, error)
	// stageDeployFailed stages deploy.failed on the outbox in the same
	// transaction. False for a cancellation, which is not a failed deploy.
	stageDeployFailed bool
}

// succeed moves a RELEASING build to SUCCEEDED. A build another worker already
// finalized is left alone.
func (f buildFinalizer) succeed(ctx context.Context, build *db.Build) error {
	return f.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		current, err := f.builds.GetByID(ctx, build.ID)
		if err != nil {
			return fmt.Errorf("re-fetch build: %w", err)
		}
		if statemachine.IsBuildPhaseTerminal(statemachine.BuildPhase(current.Status)) {
			return nil
		}
		if _, err := f.builds.UpdateStatus(ctx, tx, build.ID, string(statemachine.BPEventReleaseCompleted)); err != nil {
			return fmt.Errorf("transition to SUCCEEDED: %w", err)
		}
		return nil
	})
}

// fail drives a build to its terminal failure state, persists the reason and
// stages deploy.failed in the same transaction, then (only once that commits
// and only if this call made the transition) emits build.failed and calls the
// build-failed callback, which marks a push deploy's commit "failure" on GitHub.
// It reports whether it transitioned the build; a build already terminal is
// left alone.
func (f buildFinalizer) fail(ctx context.Context, build *db.Build, failure buildFailure) (bool, error) {
	transitioned := false
	var failed *db.Build
	err := f.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		current, err := f.builds.GetByID(ctx, build.ID)
		if err != nil {
			return fmt.Errorf("re-fetch build: %w", err)
		}
		phase := statemachine.BuildPhase(current.Status)
		if statemachine.IsBuildPhaseTerminal(phase) {
			return nil
		}
		route, err := failure.route(phase)
		if err != nil {
			return err
		}
		for _, event := range route {
			if _, err := f.builds.UpdateStatus(ctx, tx, build.ID, string(event)); err != nil {
				return fmt.Errorf("apply %s from %s: %w", event, phase, err)
			}
		}
		// F-04: a failed build must say why.
		if failure.reason != "" {
			if err := f.builds.SetErrorMessage(ctx, tx, build.ID, failure.reason); err != nil {
				return fmt.Errorf("set error message: %w", err)
			}
		}
		// J-16 slice 3 (MEET-2026-07-04-001): deploy.failed rides the same
		// transaction as the FAILED transition — atomic, at-least-once.
		if failure.stageDeployFailed {
			if err := stageDeployFailed(ctx, tx, f.deployEvents, current); err != nil {
				return fmt.Errorf("stage deploy.failed: %w", err)
			}
		}
		transitioned = true
		failed = current
		return nil
	})
	if err != nil {
		return false, err
	}
	if transitioned {
		f.emitBuildFailed(ctx, build.ID, failure.reason)
		release.NotifyBuildFailed(ctx, failed.TenantID, failed.ProjectID, failed.CommitSHA, failed.ID)
	}
	return transitioned, nil
}

// emitBuildFailed appends build.failed so the CLI learns of the failure without
// waiting for its deploy timeout. It runs after the transition committed; a
// failed append is logged, because the build's state is already durable.
func (f buildFinalizer) emitBuildFailed(ctx context.Context, buildID, reason string) {
	if f.sse == nil {
		return
	}
	payload := map[string]string{"build_id": buildID, "status": "failed"}
	if reason != "" {
		payload["error"] = reason
	}
	if _, err := f.sse.Append(ctx, buildID, "build.failed", events.SafeJSONMarshal(payload)); err != nil {
		f.logger.Error("build finalization: failed to emit build.failed",
			"build_id", buildID, "error", err)
	}
}

// routeToFailed returns the machine events that fail a build from phase: QUEUED
// must pass through RUNNING first (the machine has no QUEUED→FAILED edge for a
// build failure), RELEASING fails through its release-specific event.
func routeToFailed(phase statemachine.BuildPhase) ([]statemachine.BuildPhaseEvent, error) {
	switch phase {
	case statemachine.BPhQueued:
		return []statemachine.BuildPhaseEvent{statemachine.BPEventStarted, statemachine.BPEventFailed}, nil
	case statemachine.BPhRunning:
		return []statemachine.BuildPhaseEvent{statemachine.BPEventFailed}, nil
	case statemachine.BPhReleasing:
		return []statemachine.BuildPhaseEvent{statemachine.BPEventReleaseFailed}, nil
	default:
		return nil, fmt.Errorf("unexpected build state %q to fail", phase)
	}
}
