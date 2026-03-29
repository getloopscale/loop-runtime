// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/routes"
)

// WorkflowCanceller cancels Argo Workflows by ID.
// Used by ProjectDeleter to stop running builds during project deletion.
type WorkflowCanceller interface {
	Cancel(ctx context.Context, namespace, workflowID string) error
}

// ProjectDeleterDeps holds all dependencies for ProjectDeleter.
type ProjectDeleterDeps struct {
	BuildStore        db.BuildStore
	EnvTargetStore    db.EnvironmentTargetStore
	ProjectStore      ProjectDeleteProjectStore
	Locker            EnvironmentLocker
	GitOps            GitOpsPusher
	WorkflowCanceller WorkflowCanceller // nil-safe: skips workflow cancel if not set
}

// ProjectDeleteProjectStore defines the project store operations needed by ProjectDeleter.
// Separate from routes.ProjectStore to avoid circular dependency.
type ProjectDeleteProjectStore interface {
	DeleteProject(ctx context.Context, tenantID, projectID string) error
}

// ProjectDeleter orchestrates project deletion across all layers.
type ProjectDeleter struct {
	buildStore        db.BuildStore
	envTargetStore    db.EnvironmentTargetStore
	projectStore      ProjectDeleteProjectStore
	locker            EnvironmentLocker
	gitops            GitOpsPusher
	workflowCanceller WorkflowCanceller
}

// NewProjectDeleter creates a new ProjectDeleter with all dependencies injected.
func NewProjectDeleter(deps ProjectDeleterDeps) *ProjectDeleter {
	return &ProjectDeleter{
		buildStore:        deps.BuildStore,
		envTargetStore:    deps.EnvTargetStore,
		projectStore:      deps.ProjectStore,
		locker:            deps.Locker,
		gitops:            deps.GitOps,
		workflowCanceller: deps.WorkflowCanceller,
	}
}

// Delete removes a project from one or all environments.
//
// Flow for single environment:
//  1. Lock(tenant, project, env)
//  2. CancelActiveBuilds
//  3. DeleteTarget (environment_targets row)
//  4. Unlock
//  5. GitOps Remove (I/O outside lock — BLOCKER-2 pattern)
//
// Flow for all environments:
//  1. ListByProject -> get all envs
//  2. For each env: single-env flow
//  3. If all envs removed: delete project from projects table
func (d *ProjectDeleter) Delete(ctx context.Context, req routes.ProjectDeleteInput) (*routes.ProjectDeleteOutput, error) {
	result := &routes.ProjectDeleteOutput{
		ProjectName:         req.ProjectName,
		DeletedEnvironments: []string{},
	}

	if req.Environment != "" {
		// Single environment delete
		cancelled, err := d.deleteEnvironment(ctx, req, req.Environment)
		if err != nil {
			return nil, err
		}
		result.CancelledBuilds += cancelled
		result.DeletedEnvironments = append(result.DeletedEnvironments, req.Environment)
	} else {
		// All environments
		targets, err := d.envTargetStore.ListByProject(ctx, req.TenantID, req.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("project delete: list environments: %w", err)
		}

		for _, target := range targets {
			cancelled, err := d.deleteEnvironment(ctx, req, target.Environment)
			if err != nil {
				return nil, fmt.Errorf("project delete: env %s: %w", target.Environment, err)
			}
			result.CancelledBuilds += cancelled
			result.DeletedEnvironments = append(result.DeletedEnvironments, target.Environment)
		}

		// Fallback: if no environment_targets rows exist but the project was deployed,
		// manifests may still be in the GitOps repo (e.g. after DB reset, or if SetDesired
		// was never called). Remove from the default "production" environment as safety net.
		if len(targets) == 0 {
			slog.Warn("no environment_targets found for project — attempting gitops cleanup for production",
				"project_id", req.ProjectID, "tenant_id", req.TenantID, "correlation_id", req.CorrelationID)
			removeCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			gitopsID := req.ProjectName
			if gitopsID == "" {
				gitopsID = req.ProjectID
			}
			_, gitErr := d.gitops.Remove(removeCtx, GitOpsRemoveOpts{
				TenantID:      req.TenantID,
				ProjectID:     gitopsID,
				Environment:   "production",
				CorrelationID: req.CorrelationID,
			})
			if gitErr != nil {
				slog.Warn("gitops fallback remove failed (may not exist)",
					"project_id", req.ProjectID, "environment", "production", "error", gitErr)
			} else {
				result.DeletedEnvironments = append(result.DeletedEnvironments, "production")
			}
		}

		// All environments removed — delete the project record
		if err := d.projectStore.DeleteProject(ctx, req.TenantID, req.ProjectID); err != nil {
			slog.Warn("failed to delete project record (non-fatal, environments already cleaned)",
				"project_id", req.ProjectID, "error", err)
		}
	}

	return result, nil
}

// deleteEnvironment deletes a single environment for a project.
// Returns the number of cancelled builds.
func (d *ProjectDeleter) deleteEnvironment(ctx context.Context, req routes.ProjectDeleteInput, env string) (int, error) {
	var cancelledBuilds int
	var workflowIDs []string

	// Phase 1 (Lock): Cancel active builds + delete environment target
	err := d.locker.WithLock(ctx, req.TenantID, req.ProjectID, env, func(tx db.DBTX) error {
		// Cancel active builds inside the lock (Rule 6)
		cancelled, wfIDs, err := d.buildStore.CancelActiveBuilds(ctx, tx, req.TenantID, req.ProjectID, env)
		if err != nil {
			return fmt.Errorf("cancel active builds: %w", err)
		}
		cancelledBuilds = cancelled
		workflowIDs = wfIDs

		// Delete environment target
		if err := d.envTargetStore.DeleteTarget(ctx, tx, req.TenantID, req.ProjectID, env); err != nil {
			return fmt.Errorf("delete environment target: %w", err)
		}

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("project delete: lock phase: %w", err)
	}

	// Phase 2 (no lock): Cancel Argo Workflows + remove from GitOps — I/O outside the lock
	d.cancelWorkflows(ctx, workflowIDs, req.ProjectID, env)

	removeCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// Use ProjectName for gitops path — the CLI sends the project name from
	// loopzero.yaml as project_id in the build request, so the gitops repo path is
	// {environment}/{projectName}/manifests.yaml (not {environment}/{projectID}/).
	gitopsProjectID := req.ProjectName
	if gitopsProjectID == "" {
		gitopsProjectID = req.ProjectID // fallback if name not available
	}
	_, gitErr := d.gitops.Remove(removeCtx, GitOpsRemoveOpts{
		TenantID:      req.TenantID,
		ProjectID:     gitopsProjectID,
		Environment:   env,
		CorrelationID: req.CorrelationID,
	})
	if gitErr != nil {
		slog.Error("gitops remove failed (DB already cleaned, manual cleanup may be needed)",
			"project_id", req.ProjectID, "project_name", req.ProjectName, "environment", env, "error", gitErr)
	}

	return cancelledBuilds, nil
}

// cancelWorkflows cancels Argo Workflows for cancelled builds (best-effort).
// Workflow cancel is I/O to the build cluster, so it runs outside the advisory lock.
// Failures are logged but do not block the delete operation.
func (d *ProjectDeleter) cancelWorkflows(ctx context.Context, workflowIDs []string, projectID, env string) {
	if d.workflowCanceller == nil || len(workflowIDs) == 0 {
		return
	}
	cancelCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	for _, wfID := range workflowIDs {
		if err := d.workflowCanceller.Cancel(cancelCtx, "argo", wfID); err != nil {
			slog.Warn("workflow cancel failed (best-effort)",
				"workflow_id", wfID, "project_id", projectID, "environment", env, "error", err)
		} else {
			slog.Info("cancelled workflow during project delete",
				"workflow_id", wfID, "project_id", projectID, "environment", env)
		}
	}
}
