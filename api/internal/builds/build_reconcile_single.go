// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package builds

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/dbtx"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// reconcileBuild reconciles a single build against its Argo Workflow state.
//
// MEET-2026-03-26-008: Also handles builds in RELEASING state. These are builds
// where Phase 1 (image build) completed but Phase 2 (release activation) was
// interrupted (pod crash, rollout, Gitea/ArgoCD failure). The reconcile loop
// retries Phase 2 automatically.
//
// The loop lists builds of every tenant through the cross-tenant pool, but the
// orchestrator and the callbacks it reaches work through the request pool,
// whose connections see only the tenant their context is scoped to (migration
// 000062). The build's own tenant is that scope: the loop acts for the tenant
// that submitted the build, and for no other.
func (r *BuildReconcileLoop) reconcileBuild(ctx context.Context, build *db.Build) {
	ctx = dbtx.WithTenantScope(ctx, build.TenantID)
	ctx, span := otel.Tracer("loop-runtime-api").Start(ctx, "loop-runtime.api.build.reconcile",
		trace.WithAttributes(
			attribute.String("loop-runtime.build_id", build.ID),
		))
	defer span.End()

	// RELEASING builds: Phase 1 done, Phase 2 needs retry (MEET-2026-03-26-008)
	if build.Status == string(statemachine.BPhReleasing) {
		r.retryReleaseActivation(ctx, build)
		return
	}

	// No workflow ID means the build was never submitted to Argo — OR the
	// SubmitBuild is still in progress on another pod (race condition between
	// lock release and SetWorkflowID). Grace period: skip builds created < 60s ago.
	// After 60s, the build is legitimately orphaned and should be failed.
	if build.WorkflowID == "" {
		age := time.Since(build.CreatedAt)
		if age < 60*time.Second {
			r.logger.Debug("reconcile loop: build has no workflow ID yet, skipping (created recently)",
				"build_id", build.ID, "age", age.String())
			return
		}
		r.failBuild(ctx, build, "Build had no workflow ID (API crashed before submission)")
		return
	}

	// Check external workflow state in the build cluster
	status, err := r.workflow.GetStatus(ctx, "argo", build.WorkflowID)
	if err != nil {
		r.logger.Error("reconcile loop: failed to check workflow status",
			"build_id", build.ID, "workflow_id", build.WorkflowID, "error", err)
		return
	}

	if status == nil {
		// Workflow not found — cleaned up by TTL or never existed
		r.failBuild(ctx, build, "Build not found (expired or never started). Redeploy to retry.")
		return
	}

	switch status.Phase {
	case "Succeeded":
		r.onWorkflowSucceeded(ctx, build, status)
	case "Failed", "Error":
		// Only treat Failed/Error as terminal when the workflow has actually
		// finished (FinishedAt set). Argo Workflows can transiently report
		// Failed for a workflow whose DAG still has onExit steps or
		// ContinueOn tasks in flight — those eventually resolve to Succeeded.
		// Reconciling too early marked builds FAILED even when the pipeline
		// went on to succeed (observed on a ContinueOn task).
		if status.FinishedAt == "" {
			r.logger.Debug("reconcile loop: workflow reports Failed but not yet finished (likely transient — onExit or ContinueOn pending)",
				"build_id", build.ID, "workflow_id", build.WorkflowID, "phase", status.Phase)
			return
		}
		r.failBuild(ctx, build, workflowFailureReason(status))
	case "Running", "Pending":
		// Workflow is still active — nothing to do
	default:
		r.logger.Warn("reconcile loop: unknown workflow phase",
			"build_id", build.ID, "workflow_id", build.WorkflowID, "phase", status.Phase)
	}
}

// workflowFailureReason is the sentence a build that failed in its workflow is
// recorded with. It names the step when the workflow tells which one failed.
// The cause itself is in the step's log: Argo keeps only the exit code.
func workflowFailureReason(status *deploycontracts.WorkflowStatus) string {
	if status.FailedStep == "" {
		return fmt.Sprintf("Build %s", strings.ToLower(status.Phase))
	}
	if status.FailedStepMessage == "" {
		return fmt.Sprintf("Build failed at step %q. The step's log has the cause.", status.FailedStep)
	}
	return fmt.Sprintf("Build failed at step %q: %s. The step's log has the cause.", status.FailedStep, status.FailedStepMessage)
}
