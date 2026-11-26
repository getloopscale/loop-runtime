// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package projects

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/pkg/naming"
)

// WorkflowCanceller cancels Argo Workflows by ID.
// Used by ProjectDeleter to stop running builds during project deletion.
type WorkflowCanceller interface {
	Cancel(ctx context.Context, namespace, workflowID string) error
}

// ProjectDeleterDeps holds all dependencies for ProjectDeleter.
type ProjectDeleterDeps struct {
	BuildStore        db.BuildStore
	EnvTargetStore    store.EnvironmentTargetStore
	ReleaseStore      ProjectDeleteReleaseStore // nil-safe: when nil, skips release cascade-cancel (legacy callers)
	ProjectStore      ProjectDeleteProjectStore
	ScalingStore      ProjectDeleteScalingStore // nil-safe: when nil, skips app_scaling_config cascade
	Locker            deploycontracts.EnvironmentLocker
	GitOps            deploycontracts.GitOpsPusher
	WorkflowCanceller WorkflowCanceller                  // nil-safe: skips workflow cancel if not set
	Sync              deploycontracts.SyncRecoveryClient // nil-safe: skips ArgoCD sync trigger if not set
}

// ProjectDeleteProjectStore defines the project store operations needed by ProjectDeleter.
// Narrow store interface — separate from the broader project store consumed by
// the routes layer (ISP).
type ProjectDeleteProjectStore interface {
	DeleteProject(ctx context.Context, tenantID, projectID string) error
}

// ProjectDeleteReleaseStore is the release-store operation project deletion
// needs: cancelling every non-terminal release of an environment (ISP).
type ProjectDeleteReleaseStore interface {
	CancelAllByEnv(ctx context.Context, tx db.DBTX, tenantID, projectID, env string) (int, error)
}

// ProjectDeleteScalingStore is the scaling-store operation project deletion
// needs: removing the project's app_scaling_config rows (ISP).
type ProjectDeleteScalingStore interface {
	DeleteByProject(ctx context.Context, tx db.DBTX, tenantID, projectID string) error
}

// ProjectDeleter orchestrates project deletion across all layers.
type ProjectDeleter struct {
	buildStore        db.BuildStore
	envTargetStore    store.EnvironmentTargetStore
	releaseStore      ProjectDeleteReleaseStore
	projectStore      ProjectDeleteProjectStore
	scalingStore      ProjectDeleteScalingStore
	locker            deploycontracts.EnvironmentLocker
	gitops            deploycontracts.GitOpsPusher
	workflowCanceller WorkflowCanceller
	sync              deploycontracts.SyncRecoveryClient
}

// NewProjectDeleter creates a new ProjectDeleter with all dependencies injected.
func NewProjectDeleter(deps ProjectDeleterDeps) *ProjectDeleter {
	return &ProjectDeleter{
		buildStore:        deps.BuildStore,
		envTargetStore:    deps.EnvTargetStore,
		releaseStore:      deps.ReleaseStore,
		projectStore:      deps.ProjectStore,
		scalingStore:      deps.ScalingStore,
		locker:            deps.Locker,
		gitops:            deps.GitOps,
		workflowCanceller: deps.WorkflowCanceller,
		sync:              deps.Sync,
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
func (d *ProjectDeleter) Delete(ctx context.Context, req ProjectDeleteInput) (*ProjectDeleteOutput, error) {
	result := &ProjectDeleteOutput{
		ProjectName:         req.ProjectName,
		DeletedEnvironments: []string{},
	}

	// CLAUDE.md "Project name vs project ID": builds and environment_targets store
	// the project name (from loopzero.yaml), not the UUID from the projects table.
	// All queries against these tables must use the name.
	projectNameForQueries := req.ProjectName
	if projectNameForQueries == "" {
		projectNameForQueries = req.ProjectID
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
		targets, err := d.envTargetStore.ListByProject(ctx, req.TenantID, projectNameForQueries)
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
			_, gitErr := d.gitops.Remove(removeCtx, deploycontracts.GitOpsRemoveOpts{
				TenantID:      req.TenantID,
				ProjectID:     gitopsID,
				Environment:   "production",
				CorrelationID: req.CorrelationID,
			})
			if gitErr != nil {
				slog.Warn("gitops fallback remove failed (directory may not exist in gitops repo)",
					"project_id", req.ProjectID, "environment", "production", "error", gitErr)
			} else {
				result.DeletedEnvironments = append(result.DeletedEnvironments, "production")
			}
			// Always trigger ArgoCD sync after fallback — even if gitops.Remove
			// returned error, there may be orphaned K8s resources to prune.
			// finding-2026-05-26-dogfood-cleanup-prove-death-race: if a previous
			// broken Rollout left the app's operationState=Failed, the first
			// TriggerSync will hit ErrSyncOperationInProgress; terminate stuck
			// op + retry once so the prune actually executes.
			if d.sync != nil {
				argoAppName, nameErr := naming.ArgoAppName(req.TenantID, gitopsID, "production")
				if nameErr != nil {
					return nil, fmt.Errorf("project delete: fallback sync: %w", nameErr)
				}
				syncCtx, syncCancel := context.WithTimeout(ctx, 30*time.Second)
				defer syncCancel()
				syncErr := deploycontracts.TriggerSyncWithRecovery(syncCtx, d.sync, argoAppName, fallbackPruneSyncPolicy, slog.Default())
				if syncErr != nil {
					slog.Warn("argocd sync after fallback delete failed",
						"app", argoAppName, "error", syncErr)
				} else {
					slog.Info("argocd sync triggered after fallback gitops remove",
						"app", argoAppName, "project", req.ProjectName)
				}
			}
		}

		// Cascade: remove all app_scaling_config rows for this project so a
		// future project of the same name does not inherit replicas/HPA state
		// from its predecessor (which would surface as REPLICAS_EXCEED_PLAN
		// on a clean redeploy). nil-safe — legacy callers without ScalingStore
		// keep their existing behavior.
		if d.scalingStore != nil {
			scalingProjectID := projectNameForQueries
			if err := d.scalingStore.DeleteByProject(ctx, nil, req.TenantID, scalingProjectID); err != nil {
				slog.Warn("failed to delete scaling configs (non-fatal, may leave orphan rows)",
					"project_id", scalingProjectID, "error", err)
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
func (d *ProjectDeleter) deleteEnvironment(ctx context.Context, req ProjectDeleteInput, env string) (int, error) {
	var cancelledBuilds int
	var workflowIDs []string

	// CLAUDE.md "Project name vs project ID": builds store the project name
	// (from loopzero.yaml) as project_id, not the UUID from the projects table.
	// The advisory lock hash and CancelActiveBuilds query must use the same
	// value that the deploy flow uses — the project name.
	buildProjectID := req.ProjectName
	if buildProjectID == "" {
		buildProjectID = req.ProjectID // fallback to UUID if name unavailable
	}

	// Phase 1 (Lock): Cancel active builds + cascade-cancel releases + delete environment target
	err := d.locker.WithLock(ctx, req.TenantID, buildProjectID, env, func(tx db.DBTX) error {
		// Cancel active builds inside the lock (Rule 6)
		cancelled, wfIDs, err := d.buildStore.CancelActiveBuilds(ctx, tx, req.TenantID, buildProjectID, env)
		if err != nil {
			return fmt.Errorf("cancel active builds: %w", err)
		}
		cancelledBuilds = cancelled
		workflowIDs = wfIDs

		// Cascade-cancel non-terminal releases. Without this, ACTIVE/PENDING rows
		// linger in DB after the workload is gone — promote/rollback would resolve
		// to those zombie rows on the next deploy and use stale git commits.
		if d.releaseStore != nil {
			cancelledReleases, releaseErr := d.releaseStore.CancelAllByEnv(ctx, tx, req.TenantID, buildProjectID, env)
			if releaseErr != nil {
				return fmt.Errorf("cancel releases cascade: %w", releaseErr)
			}
			if cancelledReleases > 0 {
				slog.Info("project delete: cancelled releases via state-machine cascade",
					"tenant_id", req.TenantID, "project", buildProjectID, "environment", env,
					"cancelled", cancelledReleases)
			}
		}

		// Delete environment target (uses project name, not UUID — same as builds)
		if err := d.envTargetStore.DeleteTarget(ctx, tx, req.TenantID, buildProjectID, env); err != nil {
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
	_, gitErr := d.gitops.Remove(removeCtx, deploycontracts.GitOpsRemoveOpts{
		TenantID:      req.TenantID,
		ProjectID:     gitopsProjectID,
		Environment:   env,
		CorrelationID: req.CorrelationID,
	})
	if gitErr != nil {
		// FATAL: if manifests aren't removed from gitops, resources remain on the cluster
		// indefinitely. Failing here forces the user to retry instead of silently leaving
		// orphaned deployments that degrade ArgoCD Application health.
		return 0, fmt.Errorf("project delete: gitops remove failed for %s/%s (DB already cleaned, retry to remove manifests): %w",
			env, gitopsProjectID, gitErr)
	}
	if d.sync != nil {
		// Trigger ArgoCD sync with prune to remove K8s resources after gitops manifests
		// were deleted. Without this, manual-sync ArgoCD won't detect the removal.
		//
		// Plan 2026-05-19-drill-skills-rigor-restoration Bug D fix:
		// Previously this was `slog.Warn` (silent degradation) — caller received 200 OK
		// even when sync failed (e.g., DEPLOY_SERVICE_AUTH_FAILED), leaving zombie K8s
		// resources that drove ongoing infra bill until manual cleanup. Per CLAUDE.md
		// "gitops.Remove failure is FATAL" principle: sync failure is equally FATAL
		// because manifests-removed-from-Git + resources-still-running is identically
		// broken state (ArgoCD detect drift, can't reconcile).
		//
		// Fix: pruneSyncPolicy retries 3 times (1s, then 3s) to ride out transient
		// auth/network blips and terminates a stuck operation left by a broken
		// Rollout (finding-2026-05-26-dogfood-cleanup-prove-death-race); if still
		// failing, return error → caller emits 5xx + user sees "delete partially
		// failed, retry".
		argoAppName, nameErr := naming.ArgoAppName(req.TenantID, gitopsProjectID, env)
		if nameErr != nil {
			return cancelledBuilds, fmt.Errorf("project delete: prune sync: %w", nameErr)
		}
		syncCtx, syncCancel := context.WithTimeout(ctx, 30*time.Second)
		defer syncCancel()

		lastErr := deploycontracts.TriggerSyncWithRecovery(syncCtx, d.sync, argoAppName, pruneSyncPolicy, slog.Default())
		if lastErr == nil {
			slog.Info("argocd sync triggered after gitops remove",
				"app", argoAppName, "project", req.ProjectName, "environment", env)
		}
		if lastErr != nil {
			return cancelledBuilds, fmt.Errorf("project delete: argocd sync failed after 3 attempts for %s (gitops manifests removed but K8s resources remain — retry delete or sync ArgoCD app %s manually): %w",
				argoAppName, argoAppName, lastErr)
		}
	}

	return cancelledBuilds, nil
}

// pruneSyncPolicy drives the prune sync after a project's manifests leave GitOps.
// Every stuck failure terminates the operation immediately: the application is
// being emptied, so there is no peer sync worth waiting for. Three attempts with
// a 1s-then-3s backoff ride out a transient auth or network blip.
var pruneSyncPolicy = deploycontracts.SyncRecoveryPolicy{
	MaxAttempts:         3,
	MaxStuckAttempts:    3,
	TerminateAfterStuck: 1,
	Backoff: func(failures int, _ bool) time.Duration {
		backoff := time.Second
		for i := 1; i < failures; i++ {
			backoff *= 3
		}
		return backoff
	},
}

// fallbackPruneSyncPolicy is the best-effort variant used when no environment
// target exists: one attempt, plus one retry after terminating a stuck operation.
var fallbackPruneSyncPolicy = deploycontracts.SyncRecoveryPolicy{
	MaxAttempts:         1,
	MaxStuckAttempts:    2,
	TerminateAfterStuck: 1,
	Backoff:             func(int, bool) time.Duration { return time.Second },
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
