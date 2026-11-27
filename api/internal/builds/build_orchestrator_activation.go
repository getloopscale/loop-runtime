// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package builds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/api/internal/events"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// build_orchestrator_activation.go — M9 Step 6 P0: the two *BuildOrchestrator
// methods that were physically clustered in release_activator.go (a
// release-context file) are relocated here so release_activator.go becomes
// pure-release before the bounded-context package split. Go forbids methods on
// a type outside its package, so this de-clustering MUST precede the move.
// Still package services (in-place); the package split lands in later slices.

// emitActivationFailure emits a deploy.sync_failed SSE event when release activation
// fails after a successful build. This ensures the CLI gets notified instead of
// waiting for a timeout.
func (o *BuildOrchestrator) emitActivationFailure(ctx context.Context, buildID string, activationErr error) {
	failedData := events.SafeJSONMarshal(map[string]interface{}{
		"status": "sync_failed",
		"reason": "ACTIVATION_FAILED",
		"error":  activationErr.Error(),
	})
	if _, err := o.eventStore.Append(ctx, buildID, "deploy.sync_failed", failedData); err != nil {
		slog.Error("failed to emit activation failure event", "build_id", buildID, "error", err)
	}
}

// failOnUndeliveredSecrets fails a build whose activation stopped because the
// environment's secrets did not reach the cluster, and reports whether it did.
//
// Other activation failures leave the build in RELEASING for the reconcile
// loop. This one is not retried: delivery already waited for the secret store
// with a deadline of its own, a retry would end minutes later with a message
// about attempts, and the build's error has to name the store.
func (o *BuildOrchestrator) failOnUndeliveredSecrets(ctx context.Context, build *db.Build, activationErr error) bool {
	var undelivered *deploycontracts.SecretsNotDeliveredError
	if !errors.As(activationErr, &undelivered) {
		return false
	}
	slog.Error("deploy stopped: the environment's secrets did not reach the cluster",
		"build_id", build.ID, "tenant_id", build.TenantID, "project", build.ProjectID,
		"environment", build.Environment, "secret_store", undelivered.Store,
		"external_secret", undelivered.ExternalSecret, "error", activationErr)
	if _, err := o.finalizer().fail(ctx, build, buildFailure{
		reason: undelivered.Error(), route: routeToFailed, stageDeployFailed: true,
	}); err != nil {
		// Still RELEASING: the reconcile loop reaches this again.
		slog.Error("deploy stopped on undelivered secrets, but the build could not be failed",
			"build_id", build.ID, "error", err)
		return false
	}
	return true
}

// RetryRelease is the "retry without rebuild" mechanism (Step 6): the API reads the
// manifests stored in release.Artifacts and re-pushes them to the GitOps repo.
// No new build is triggered.
func (o *BuildOrchestrator) RetryRelease(ctx context.Context, releaseID string) error {
	release, err := o.releaseStore.GetByID(ctx, releaseID)
	if err != nil {
		return fmt.Errorf("retry release: get release: %w", err)
	}

	if release.Status != string(statemachine.RPhGitMirrorFailed) {
		return fmt.Errorf("retry release: release %s is in %s state, not GIT_MIRROR_FAILED — cannot retry", releaseID, release.Status)
	}

	var artifacts map[string]string
	if release.Artifacts == nil {
		return fmt.Errorf("retry release: release %s has no stored artifacts", releaseID)
	}
	if err := json.Unmarshal(release.Artifacts, &artifacts); err != nil {
		return fmt.Errorf("retry release: unmarshal artifacts: %w", err)
	}
	manifests, ok := artifacts["manifests"]
	if !ok || manifests == "" {
		return fmt.Errorf("retry release: release %s has no manifests in artifacts", releaseID)
	}

	// Push outside lock (consistent with BLOCKER-2 fix)
	pushCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	_, pushErr := o.gitops.Push(pushCtx, deploycontracts.GitOpsPushOpts{
		TenantID:      release.TenantID,
		ProjectID:     release.ProjectID,
		Environment:   release.Environment,
		Manifests:     manifests,
		BuildID:       release.BuildID,
		ReleaseID:     release.ID,
		CorrelationID: release.CorrelationID,
	})
	if pushErr != nil {
		return fmt.Errorf("retry release: gitops push failed: %w", pushErr)
	}

	// Record success inside lock
	return o.locker.WithLock(ctx, release.TenantID, release.ProjectID, release.Environment, func(tx db.DBTX) error {
		if _, err := o.releaseStore.UpdateStatus(ctx, tx, releaseID, string(statemachine.RPEventGitMirrorRetried)); err != nil {
			return fmt.Errorf("retry release: transition to PENDING: %w", err)
		}

		if err := o.envTargetStore.SetDesired(ctx, tx, release.TenantID, release.ProjectID, release.Environment, releaseID, release.CreatedBy, release.CorrelationID); err != nil {
			return fmt.Errorf("retry release: set desired: %w", err)
		}

		return nil
	})
}
