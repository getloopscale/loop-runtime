// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package services implements business logic orchestration.
package services

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// StartupReconciler runs once on API boot to recover orphaned builds.
//
// When the API crashes during a deploy, builds can be stuck in RUNNING or QUEUED
// with no process watching them. The reaper will eventually clean them up (30min),
// but the reconciler recovers them immediately on restart.
//
// For each non-terminal build it checks the Argo Workflow external state:
//   - Workflow completed (Succeeded/Failed) → finalize the build
//   - Workflow not found (cleaned up by TTL) → mark FAILED
//   - Workflow still active → reattach (do nothing, the callback will arrive)
//
// Spec §12.1: "API restart reconciliation"
type StartupReconciler struct {
	buildStore db.BuildStore
	workflow   WorkflowSubmitter
	locker     EnvironmentLocker
	logger     *slog.Logger
}

// NewStartupReconciler creates a new StartupReconciler.
func NewStartupReconciler(buildStore db.BuildStore, workflow WorkflowSubmitter, locker EnvironmentLocker, logger *slog.Logger) *StartupReconciler {
	return &StartupReconciler{
		buildStore: buildStore,
		workflow:   workflow,
		locker:     locker,
		logger:     logger,
	}
}

// Reconcile checks all non-terminal builds and reconciles their state
// against the external workflow system. Returns the number of builds reconciled.
func (r *StartupReconciler) Reconcile(ctx context.Context) int {
	r.logger.Info("startup reconciler: checking for orphaned builds")

	builds, err := r.findActiveBuilds(ctx)
	if err != nil {
		r.logger.Error("startup reconciler: failed to find active builds", "error", err)
		return 0
	}

	if len(builds) == 0 {
		r.logger.Info("startup reconciler: no orphaned builds found")
		return 0
	}

	r.logger.Info("startup reconciler: found active builds to reconcile", "count", len(builds))

	reconciled := 0
	for _, build := range builds {
		action := r.reconcileBuild(ctx, &build)
		if action != "" {
			r.logger.Info("startup reconciler: reconciled build",
				"build_id", build.ID,
				"status", build.Status,
				"workflow_id", build.WorkflowID,
				"action", action)
			reconciled++
		}
	}

	r.logger.Info("startup reconciler: complete", "reconciled", reconciled, "total_active", len(builds))
	return reconciled
}

// findActiveBuilds returns all builds in QUEUED or RUNNING state.
func (r *StartupReconciler) findActiveBuilds(ctx context.Context) ([]db.Build, error) {
	return r.buildStore.FindActiveBuilds(ctx)
}

// reconcileBuild checks a single build against external workflow state and reconciles.
// Returns a description of the action taken, or "" if no action was needed.
func (r *StartupReconciler) reconcileBuild(ctx context.Context, build *db.Build) string {
	// If no workflow ID, the build never made it to Argo — mark as FAILED
	if build.WorkflowID == "" {
		return r.failBuild(ctx, build, "Build had no workflow ID (API crashed before submission)")
	}

	// Check external workflow state
	status, err := r.workflow.GetStatus(ctx, "build-"+build.TenantID, build.WorkflowID)
	if err != nil {
		r.logger.Error("startup reconciler: failed to check workflow status",
			"build_id", build.ID, "workflow_id", build.WorkflowID, "error", err)
		return ""
	}

	if status == nil {
		// Workflow not found — cleaned up by TTL or never existed
		return r.failBuild(ctx, build, "Build not found (expired or never started). Redeploy to retry.")
	}

	switch status.Phase {
	case "Succeeded":
		// Workflow completed successfully but callback was lost.
		// We can't complete the full activation here (no callback payload),
		// so mark as FAILED with a clear message.
		return r.failBuild(ctx, build, "Build completed but result was lost during service restart. Redeploy to retry.")
	case "Failed", "Error":
		return r.failBuild(ctx, build, fmt.Sprintf("Build %s", strings.ToLower(status.Phase)))
	case "Running", "Pending":
		// Workflow is still active — the callback will arrive when it finishes.
		// No action needed — just log the reattach.
		r.logger.Info("startup reconciler: reattaching to active workflow",
			"build_id", build.ID, "workflow_id", build.WorkflowID, "phase", status.Phase)
		return "reattach"
	default:
		r.logger.Warn("startup reconciler: unknown workflow phase",
			"build_id", build.ID, "workflow_id", build.WorkflowID, "phase", status.Phase)
		return ""
	}
}

// failBuild transitions a build to FAILED inside the environment lock.
func (r *StartupReconciler) failBuild(ctx context.Context, build *db.Build, reason string) string {
	err := r.locker.WithLock(ctx, build.TenantID, build.ProjectID, build.Environment, func(tx db.DBTX) error {
		// Re-fetch inside lock — state may have changed
		current, err := r.buildStore.GetByID(ctx, build.ID)
		if err != nil {
			return err
		}
		if statemachine.IsBuildTerminal(statemachine.State(current.Status)) {
			return nil // Already finalized
		}

		// Transition QUEUED → RUNNING first if needed (state machine requires it)
		if current.Status == string(statemachine.BuildQueued) {
			if _, err := r.buildStore.UpdateStatus(ctx, tx, build.ID, string(statemachine.BuildStarted)); err != nil {
				return fmt.Errorf("transition to RUNNING: %w", err)
			}
		}

		// Transition RUNNING → FAILED
		if _, err := r.buildStore.UpdateStatus(ctx, tx, build.ID, string(statemachine.BuildFailedEvent)); err != nil {
			return fmt.Errorf("transition to FAILED: %w", err)
		}

		// Set error message for user visibility
		return r.buildStore.SetErrorMessage(ctx, tx, build.ID, reason)
	})

	if err != nil {
		r.logger.Error("startup reconciler: failed to transition build",
			"build_id", build.ID, "error", err)
		return ""
	}

	return "failed: " + reason
}
