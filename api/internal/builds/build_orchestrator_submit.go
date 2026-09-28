// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
	"github.com/getloopscale/loopzero/api/internal/events"
	"github.com/getloopscale/loopzero/api/internal/metrics"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
	"github.com/getloopscale/loopzero/pkg/statemachine"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
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
func (o *BuildOrchestrator) SubmitBuild(ctx context.Context, req deploycontracts.SubmitBuildRequest) (*deploycontracts.SubmitBuildResponse, error) {
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
			return wrapSubmitBuildError(StageCheckActiveBuilds, err)
		}
		if hasActive {
			return loopzeroerrors.ErrDeployInProgress
		}

		// Step 2.2: B40 — resolve scaling from app_scaling_config DB rows BEFORE
		// plan-cap enforcement. Per D1 (DB > loopzero.yaml when row exists), this
		// closes the hole MEET-2026-04-23-059 Constraint 6 — plan caps must
		// operate on the config that becomes K8s state, not on user-supplied
		// yaml. Resolve runs inside the tx scope (serializable isolation
		// guarantees no torn reads). Race condition with concurrent scale
		// upsert is acceptable per eventual consistency (T2.1 Deep Dives).
		resolvedApps, err := o.resolveScaling(ctx, req.TenantID, req.ProjectID, req.Environment, req.Apps)
		if err != nil {
			return wrapSubmitBuildError(StageResolveScaling, err)
		}
		req.Apps = resolvedApps

		// Step 2.3: B15 scaling validation + plan-cap enforcement.
		// This is the authoritative gate covering BOTH the CLI deploy path and
		// the GitHub App auto-deploy path — bypass here means a plan-cap
		// enforcement hole (MEET-2026-04-23-059 Constraint 6). Operates on
		// resolved AppSpec per B40 fix above.
		//
		// Sprint 6 /review CODE-003 LOW: enforceScaling returns a *LoopZeroError
		// directly (ErrScalingAmbiguous / ErrReplicasExceedPlan / etc.) by
		// design — bypassing wrapSubmitBuildError so mapBuildError dispatches
		// it via the *LoopZeroError ladder branch with its own code, not as
		// SubmitBuildError. The wrap helper EC-3 guard also short-circuits
		// any *LoopZeroError that reaches it (defense-in-depth).
		if err := enforceScaling(ctx, req.Apps, o.planCap, req.TenantID, o.kedaEnabled); err != nil {
			return err
		}

		// Step 2.5: ensure project exists in projects table (idempotent).
		// After `loopzero project delete` + redeploy, the build succeeds but the
		// project doesn't appear in `loopzero project list` because it was hard-deleted.
		if o.projectStore != nil {
			ensureCtx, ensureSpan := otel.Tracer("loopzero-api").Start(ctx, "loopzero.api.project.ensure",
				trace.WithAttributes(
					attribute.String("loopzero.tenant", req.TenantID),
					attribute.String("loopzero.project", req.ProjectID),
				))
			if err := o.projectStore.EnsureProject(ensureCtx, tx, req.TenantID, req.ProjectID, req.CreatedBy); err != nil {
				ensureSpan.RecordError(err)
				ensureSpan.SetStatus(codes.Error, err.Error())
				ensureSpan.End()
				return wrapSubmitBuildError(StageEnsureProject, err)
			}
			ensureSpan.End()
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
			return wrapSubmitBuildError(StageCreateBuild, err)
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
				Start:     a.Start,   // T0.2: persist declared start (canonical Dockerfile CMD)
				Path:      a.Path,    // T0.2: persist app path (monorepo target)
				Build:     a.Build,   // T0.2: persist build mode
				Runtime:   a.Runtime, // T0.1 (F-dom-2 wiring): persist services.json runtime
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
			return wrapSubmitBuildError(StageSetAppsSpec, err)
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
	var units []deploycontracts.AppUnit
	for _, a := range req.Apps {
		unit, normErr := deploycontracts.NormalizeApp(a.Name, a)
		if normErr != nil {
			// BRC-001 fix: fail the QUEUED build immediately so the environment
			// is unblocked. Previously, the build sat QUEUED until the reaper
			// fired (5min timeout), blocking all deploys to this environment.
			o.failQueuedBuild(ctx, buildID, req.TenantID, fmt.Sprintf("app normalization failed: %v", normErr))
			return nil, wrapSubmitBuildError(StageNormalizeApp, fmt.Errorf("normalize app %q: %w", a.Name, normErr))
		}
		units = append(units, unit)
	}
	plan := deploycontracts.CreateBuildPlan(units, req.TenantID, req.ProjectID, req.CommitSHA, o.registryURL)

	wfSpec := deploycontracts.WorkflowSpec{
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
		o.failQueuedBuild(ctx, buildID, req.TenantID, fmt.Sprintf("Workflow submission failed: %v", submitErr))
		return nil, wrapSubmitBuildError(StageSubmitWorkflow, submitErr)
	}

	// Persist workflow ID (outside lock, tx=nil -> uses pool directly)
	if err := o.buildStore.SetWorkflowID(ctx, nil, buildID, workflowID); err != nil {
		slog.Error("submit build: failed to set workflow ID after submit",
			"build_id", buildID, "workflow_id", workflowID, "error", err)
		// Non-fatal: reconcile loop will find the workflow by label matching.
		// The build will still work, just cancel won't correlate by workflow ID.
	}

	// Transition QUEUED -> RUNNING. Runs OUTSIDE the advisory lock, so use the
	// lock-free CAS variant (M9 5b, ADR 2026-07-03-m9-5b-nil-tx-status-atomicity):
	// a single conditional UPDATE (WHERE status=QUEUED) instead of a nil-tx
	// read-modify-write that could split fetch+update across two pooled
	// connections and clobber a concurrent lock-holder's terminal write.
	if b, err := o.buildStore.UpdateStatusIfCurrent(ctx, buildID, req.TenantID, statemachine.BPhQueued, string(statemachine.BPEventStarted)); err != nil {
		slog.Error("submit build: failed to transition to RUNNING",
			"build_id", buildID, "error", err)
		// Non-fatal: reconcile loop handles EC-N (QUEUED build with completed workflow).
	} else if b == nil {
		// Superseded: a concurrent lock-holder (reaper / reconcile / callback)
		// already advanced the build past QUEUED. The reconcile backstop owns it.
		slog.Debug("submit build: build advanced past QUEUED before RUNNING flip — reconcile backstop owns it",
			"build_id", buildID)
	}

	// Emit SSE build.started -- include app names for CLI per-app rendering (F-01)
	appNames := make([]string, len(req.Apps))
	for i, a := range req.Apps {
		appNames[i] = a.Name
	}
	eventData := events.SafeJSONMarshal(map[string]interface{}{
		"build_id": buildID,
		"status":   "running",
		"apps":     appNames,
	})
	if _, err := o.eventStore.Append(ctx, buildID, "build.started", eventData); err != nil {
		slog.Error("submit build: failed to emit SSE event",
			"build_id", buildID, "error", err)
	}

	// Plan deploy-timeline-canonical-5-phases T5.1 — spawn the per-deploy
	// 5-phase aggregator goroutine. Nil-safe: when no spawner is wired
	// (legacy / test configurations), submit behavior is identical to
	// before.
	//
	// LIVE-caught 2026-06-18: pass context.Background(), NOT the HTTP
	// request ctx. SubmitBuild returns 202 immediately; the request
	// handler completes and its ctx is canceled — the goroutine would
	// see ctx.Done() and exit before emitting any deploy_timeline.*
	// event. The aggregator owns its own lifecycle and exits via FSM
	// terminal-state OR explicit cancel from outside.
	if o.deployTimelineSpawn != nil {
		o.deployTimelineSpawn(context.Background(), buildID)
	}

	metrics.DeploysTotal.WithLabelValues(req.TenantID, req.Environment, "submitted").Inc()
	metrics.BuildQueueDepth.Inc()

	span.SetAttributes(attribute.String("loopzero.build_id", buildID))

	streamURL := fmt.Sprintf("/api/v1/builds/%s/stream", buildID)
	return &deploycontracts.SubmitBuildResponse{
		BuildID:   buildID,
		StreamURL: streamURL,
	}, nil
}

// enforceScaling rejects requests that set both `replicas` and `autoscaling`
// (ErrScalingAmbiguous) or whose requested per-service replica count exceeds
// the tenant's plan cap (ErrReplicasExceedPlan).
//
// B15 (CHG-2026-04-23-B15 / MEET-2026-04-23-059) Constraint 6: the check MUST
// live in SubmitBuild so both CLI and GitHub App auto-deploy paths are covered.
// A nil checker disables enforcement (dev / legacy callsites).
func enforceScaling(ctx context.Context, apps []deploycontracts.AppSpec, checker PlanCapChecker, tenantID string, kedaEnabled bool) error {
	for _, a := range apps {
		hasReplicas := a.Replicas > 0
		hasAutoscaling := a.Autoscaling != nil

		// KEDA gate: scale-to-zero (min=0) requires KEDA to be enabled on the cluster.
		if hasAutoscaling && a.Autoscaling.MinReplicas == 0 && a.Autoscaling.MaxReplicas > 0 && !kedaEnabled {
			return loopzeroerrors.NewLoopZeroError(
				"SCALE_TO_ZERO_UNAVAILABLE",
				fmt.Sprintf("App '%s': scale-to-zero is not available on this cluster. Use min >= 1 for autoscaling, or contact support to enable scale-to-zero.", a.Name))
		}

		if hasReplicas && hasAutoscaling {
			// B15: observe mutual-exclusion rejections under the same metric so
			// dashboards show a single plan-enforcement signal.
			metrics.PlanLimitViolations.WithLabelValues("scaling_ambiguous", "").Inc()
			// The HTTP status is resolved by errorshttp.Status from the code.
			return loopzeroerrors.NewLoopZeroError(
				loopzeroerrors.ErrScalingAmbiguous.Code,
				fmt.Sprintf("App '%s': %s", a.Name, loopzeroerrors.ErrScalingAmbiguous.Message))
		}
		if checker == nil {
			continue
		}
		// Determine the effective maximum replica count for cap checks.
		// autoscaling.max is the ceiling; replicas is a fixed count.
		var requested int
		switch {
		case hasAutoscaling:
			requested = a.Autoscaling.MaxReplicas
		case hasReplicas:
			requested = a.Replicas
		default:
			continue // default replicas=1, no plan cap interaction
		}
		if requested <= 0 {
			continue
		}
		allowed, plan, cap, err := checker.CheckReplicasValue(ctx, tenantID, requested)
		if err != nil {
			return err
		}
		if !allowed {
			// B15 MEET-059 action #9: emit counter so dashboards/alerts can
			// surface plan-limit friction without scraping audit logs.
			metrics.PlanLimitViolations.WithLabelValues("max_replicas_per_service", plan).Inc()
			return loopzeroerrors.NewReplicasExceedPlan(plan, cap, requested, a.Name)
		}
	}
	return nil
}

// failQueuedBuild transitions a QUEUED build to FAILED when workflow submission
// fails. This runs OUTSIDE the advisory lock — DELIBERATELY (REVIEW-BLOCKER-1:
// immediately unblock the environment on submit failure). It uses the lock-free
// CAS variant (M9 5b, ADR 2026-07-03-m9-5b-nil-tx-status-atomicity): a single
// conditional UPDATE (WHERE status=QUEUED) that is atomic without a tx and is a
// correct no-op if a concurrent lock-holder (e.g. the reaper) already terminated
// the build. tenantID is threaded from the caller (SubmitBuild's req.TenantID)
// to preserve the defense-in-depth tenant predicate.
//
// J-16 slice 3 (MEET-2026-07-04-001): this path deliberately does NOT stage a
// deploy.failed outbox event. A workflow-SUBMISSION failure is a platform
// infrastructure error (the build never ran) — a distinct class from
// deploy.failed (the build ran and failed), which the three build-EXECUTION-
// failure paths (complete / reconcile-retry / reaper) emit. The lock-free CAS
// here is intentional (ADR 2026-07-03-m9-5b); a transactional outbox emit would
// require a tx, reversing that ADR. Do NOT "fix" this into an emit — the
// exclusion is the ratified design, not a gap.
func (o *BuildOrchestrator) failQueuedBuild(ctx context.Context, buildID, tenantID, reason string) {
	if _, err := o.buildStore.UpdateStatusIfCurrent(ctx, buildID, tenantID, statemachine.BPhQueued, string(statemachine.BPEventFailed)); err != nil {
		slog.Error("submit build: failed to mark QUEUED build as FAILED",
			"build_id", buildID, "error", err)
		return
	}
	if err := o.buildStore.SetErrorMessage(ctx, nil, buildID, reason); err != nil {
		slog.Error("submit build: failed to set error message on failed build",
			"build_id", buildID, "error", err)
	}

	// Emit SSE build.failed so CLI gets notified
	eventData := events.SafeJSONMarshal(map[string]string{
		"build_id": buildID,
		"status":   "failed",
		"error":    reason,
	})
	if _, err := o.eventStore.Append(ctx, buildID, "build.failed", eventData); err != nil {
		slog.Error("submit build: failed to emit build.failed SSE event",
			"build_id", buildID, "error", err)
	}
}
