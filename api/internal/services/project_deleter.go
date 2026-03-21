package services

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/routes"
)

// ProjectDeleterDeps holds all dependencies for ProjectDeleter.
type ProjectDeleterDeps struct {
	BuildStore     db.BuildStore
	EnvTargetStore db.EnvironmentTargetStore
	ProjectStore   ProjectDeleteProjectStore
	Locker         EnvironmentLocker
	GitOps         GitOpsPusher
}

// ProjectDeleteProjectStore defines the project store operations needed by ProjectDeleter.
// Separate from routes.ProjectStore to avoid circular dependency.
type ProjectDeleteProjectStore interface {
	DeleteProject(ctx context.Context, tenantID, projectID string) error
}

// ProjectDeleter orchestrates project deletion across all layers.
type ProjectDeleter struct {
	buildStore     db.BuildStore
	envTargetStore db.EnvironmentTargetStore
	projectStore   ProjectDeleteProjectStore
	locker         EnvironmentLocker
	gitops         GitOpsPusher
}

// NewProjectDeleter creates a new ProjectDeleter with all dependencies injected.
func NewProjectDeleter(deps ProjectDeleterDeps) *ProjectDeleter {
	return &ProjectDeleter{
		buildStore:     deps.BuildStore,
		envTargetStore: deps.EnvTargetStore,
		projectStore:   deps.ProjectStore,
		locker:         deps.Locker,
		gitops:         deps.GitOps,
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

	// Phase 1 (Lock): Cancel active builds + delete environment target
	err := d.locker.WithLock(ctx, req.TenantID, req.ProjectID, env, func(tx db.DBTX) error {
		// Cancel active builds inside the lock (Rule 6)
		cancelled, err := d.buildStore.CancelActiveBuilds(ctx, tx, req.TenantID, req.ProjectID, env)
		if err != nil {
			return fmt.Errorf("cancel active builds: %w", err)
		}
		cancelledBuilds = cancelled

		// Delete environment target
		if err := d.envTargetStore.DeleteTarget(ctx, tx, req.TenantID, req.ProjectID, env); err != nil {
			return fmt.Errorf("delete environment target: %w", err)
		}

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("project delete: lock phase: %w", err)
	}

	// Phase 2 (no lock): Remove from GitOps repo — I/O outside the lock (BLOCKER-2 pattern)
	removeCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	_, gitErr := d.gitops.Remove(removeCtx, GitOpsRemoveOpts{
		TenantID:      req.TenantID,
		ProjectID:     req.ProjectID,
		Environment:   env,
		CorrelationID: req.CorrelationID,
	})
	if gitErr != nil {
		slog.Error("gitops remove failed (DB already cleaned, manual cleanup may be needed)",
			"project_id", req.ProjectID, "environment", env, "error", gitErr)
		// Don't fail the whole operation — DB state is already clean
		// ArgoCD will eventually prune when the directory is cleaned up
	}

	return cancelledBuilds, nil
}
