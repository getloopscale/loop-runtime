// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/metrics"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
	"github.com/loopzero/loopzero/pkg/statemachine"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// SubmitBuild acquires the environment lock to reserve a build slot, then
// submits the workflow OUTSIDE the lock to avoid holding it during slow
// Argo API calls (REVIEW-BLOCKER-1 fix).
//
// Steps (spec SS10.1, steps 4-11):
//  1. Acquire environment lock
//  2. Check concurrent build (HasActiveBuild) -- INSIDE the lock
//  3. Create Build record (QUEUED) + persist apps spec -- INSIDE the lock
//  4. Release lock (transaction commits) -- build exists as QUEUED
//  5. Submit Argo Workflow (OUTSIDE the lock)
//  6. Persist workflow ID, transition QUEUED -> RUNNING, emit SSE
//
// If workflow submission fails (step 5), the build is immediately transitioned
// to FAILED so the environment is unblocked. The build reaper also covers this
// case (5min QUEUED timeout) as a safety net.
//
// Returns ErrDeployInProgress if another build is active.
// Returns ErrLockTimeout if the lock cannot be acquired within 5s.
func (o *BuildOrchestrator) SubmitBuild(ctx context.Context, req SubmitBuildRequest) (*SubmitBuildResponse, error) {
	// MEET-2026-03-30-020: distributed tracing (C3 naming convention)
	ctx, span := otel.Tracer("loopzero-api").Start(ctx, "loopzero.api.build.submit",
		trace.WithAttributes(
			attribute.String("loopzero.tenant", req.TenantID),
			attribute.String("loopzero.project", req.ProjectID),
			attribute.String("loopzero.environment", req.Environment),
		))
	defer span.End()

	// Phase 1 (INSIDE lock): reserve build slot as QUEUED
	var buildID string

	err := o.locker.WithLock(ctx, req.TenantID, req.ProjectID, req.Environment, func(tx db.DBTX) error {
		// Step 2: concurrent build check INSIDE the lock (Rule 6)
		hasActive, err := o.buildStore.HasActiveBuild(ctx, tx, req.TenantID, req.ProjectID, req.Environment)
		if err != nil {
			return fmt.Errorf("submit build: check active builds: %w", err)
		}
		if hasActive {
			return loopzeroerrors.ErrDeployInProgress
		}

		// Step 2.5: ensure project exists in projects table (idempotent).
		// After `loopzero project delete` + redeploy, the build succeeds but the
		// project doesn't appear in `loopzero project list` because it was hard-deleted.
		if o.projectStore != nil {
			if err := o.projectStore.EnsureProject(ctx, tx, req.TenantID, req.ProjectID, req.CreatedBy); err != nil {
				return fmt.Errorf("submit build: ensure project: %w", err)
			}
		}

		// Step 3: create Build (QUEUED)
		build := &db.Build{
			TenantID:      req.TenantID,
			ProjectID:     req.ProjectID,
			Environment:   req.Environment,
			CommitSHA:     req.CommitSHA,
			CreatedBy:     req.CreatedBy,
			CorrelationID: req.CorrelationID,
		}
		if err := o.buildStore.Create(ctx, tx, build); err != nil {
			return fmt.Errorf("submit build: create build: %w", err)
		}
		buildID = build.ID

		// C-01 fix: Persist app specs so the reconcile loop can generate
		// manifests and compute image references when the workflow completes.
		var appsSpec []db.BuildAppSpec
		for _, a := range req.Apps {
			spec := db.BuildAppSpec{
				Name:      a.Name,
				Type:      a.Type,
				Rendering: a.Rendering,
				Port:      a.Port,
				Secrets:   a.Secrets,
				Replicas:  a.Replicas,
			}
			if a.Autoscaling != nil {
				spec.Autoscaling = &db.BuildAppAutoscaling{
					MinReplicas: a.Autoscaling.MinReplicas,
					MaxReplicas: a.Autoscaling.MaxReplicas,
					TargetCPU:   a.Autoscaling.TargetCPU,
				}
			}
			appsSpec = append(appsSpec, spec)
		}
		if err := o.buildStore.SetAppsSpec(ctx, tx, build.ID, appsSpec); err != nil {
			return fmt.Errorf("submit build: set apps spec: %w", err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Phase 2 (OUTSIDE lock): submit workflow and activate build.
	// Lock is released -- Argo API latency no longer blocks other deploys.
	// If anything below fails, the build stays QUEUED briefly, then we
	// immediately transition it to FAILED so the env is unblocked.

	// Normalize AppSpecs into AppUnits and create a BuildPlan.
	// The BuildPlan resolves all paths, image tags, and strategies before
	// reaching the WorkflowBuilder -- no inference happens in the pipeline.
	var units []AppUnit
	for _, a := range req.Apps {
		unit, normErr := NormalizeApp(a.Name, a)
		if normErr != nil {
			// BRC-001 fix: fail the QUEUED build immediately so the environment
			// is unblocked. Previously, the build sat QUEUED until the reaper
			// fired (5min timeout), blocking all deploys to this environment.
			o.failQueuedBuild(ctx, buildID, fmt.Sprintf("app normalization failed: %v", normErr))
			return nil, fmt.Errorf("normalize app %q: %w", a.Name, normErr)
		}
		units = append(units, unit)
	}
	plan := CreateBuildPlan(units, req.TenantID, req.ProjectID, req.CommitSHA, o.registryURL)

	wfSpec := WorkflowSpec{
		BuildID:       buildID,
		TenantID:      req.TenantID,
		ProjectID:     req.ProjectID,
		Environment:   req.Environment,
		CommitSHA:     req.CommitSHA,
		NoCache:       req.NoCache,
		Apps:          req.Apps,
		Plan:          &plan,
		CorrelationID: req.CorrelationID,
		Namespace:     "argo",
	}
	workflowID, submitErr := o.workflow.Submit(ctx, wfSpec)
	if submitErr != nil {
		// Workflow submit failed -- immediately fail the build so the environment
		// is unblocked. The build reaper is a safety net (5min QUEUED timeout)
		// but immediate failure gives better UX.
		o.failQueuedBuild(ctx, buildID, fmt.Sprintf("Workflow submission failed: %v", submitErr))
		return nil, fmt.Errorf("submit build: submit workflow: %w", submitErr)
	}

	// Persist workflow ID (outside lock, tx=nil -> uses pool directly)
	if err := o.buildStore.SetWorkflowID(ctx, nil, buildID, workflowID); err != nil {
		slog.Error("submit build: failed to set workflow ID after submit",
			"build_id", buildID, "workflow_id", workflowID, "error", err)
		// Non-fatal: reconcile loop will find the workflow by label matching.
		// The build will still work, just cancel won't correlate by workflow ID.
	}

	// Transition QUEUED -> RUNNING
	if _, err := o.buildStore.UpdateStatus(ctx, nil, buildID, string(statemachine.BuildStarted)); err != nil {
		slog.Error("submit build: failed to transition to RUNNING",
			"build_id", buildID, "error", err)
		// Non-fatal: reconcile loop handles EC-N (QUEUED build with completed workflow).
	}

	// Emit SSE build.started -- include app names for CLI per-app rendering (F-01)
	appNames := make([]string, len(req.Apps))
	for i, a := range req.Apps {
		appNames[i] = a.Name
	}
	eventData := safeJSONMarshal(map[string]interface{}{
		"build_id": buildID,
		"status":   "running",
		"apps":     appNames,
	})
	if _, err := o.eventStore.Append(ctx, buildID, "build.started", eventData); err != nil {
		slog.Error("submit build: failed to emit SSE event",
			"build_id", buildID, "error", err)
	}

	metrics.DeploysTotal.WithLabelValues(req.TenantID, req.Environment, "submitted").Inc()
	metrics.BuildQueueDepth.Inc()

	span.SetAttributes(attribute.String("loopzero.build_id", buildID))

	streamURL := fmt.Sprintf("/api/v1/builds/%s/stream", buildID)
	return &SubmitBuildResponse{
		BuildID:   buildID,
		StreamURL: streamURL,
	}, nil
}

// failQueuedBuild transitions a QUEUED build to FAILED when workflow submission
// fails. This runs OUTSIDE the advisory lock using the connection pool directly.
// REVIEW-BLOCKER-1: ensures the environment is immediately unblocked on submit failure.
func (o *BuildOrchestrator) failQueuedBuild(ctx context.Context, buildID string, reason string) {
	if _, err := o.buildStore.UpdateStatus(ctx, nil, buildID, string(statemachine.BuildFailedEvent)); err != nil {
		slog.Error("submit build: failed to mark QUEUED build as FAILED",
			"build_id", buildID, "error", err)
		return
	}
	if err := o.buildStore.SetErrorMessage(ctx, nil, buildID, reason); err != nil {
		slog.Error("submit build: failed to set error message on failed build",
			"build_id", buildID, "error", err)
	}

	// Emit SSE build.failed so CLI gets notified
	eventData := safeJSONMarshal(map[string]string{
		"build_id": buildID,
		"status":   "failed",
		"error":    reason,
	})
	if _, err := o.eventStore.Append(ctx, buildID, "build.failed", eventData); err != nil {
		slog.Error("submit build: failed to emit build.failed SSE event",
			"build_id", buildID, "error", err)
	}
}
