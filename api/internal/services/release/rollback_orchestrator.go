package release

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/services/deploycontracts"
	"github.com/getloopscale/loopzero/pkg/naming"
	"github.com/getloopscale/loopzero/pkg/statemachine"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// RollbackReleaseStore abstracts release persistence for the rollback orchestrator (DIP).
type RollbackReleaseStore interface {
	GetActiveByEnvTx(ctx context.Context, tx db.DBTX, tenantID, projectID, env string) (*db.Release, error)
	GetPreviousRelease(ctx context.Context, tenantID, projectID, env string) (*db.Release, error)
	GetByIDForTenant(ctx context.Context, tenantID, releaseID string) (*db.Release, error)
	UpdateStatus(ctx context.Context, tx db.DBTX, id string, event string) (*db.Release, error)
}

// RollbackEnvTargetStore abstracts environment target operations for rollback (DIP).
type RollbackEnvTargetStore interface {
	SetDesired(ctx context.Context, tx db.DBTX, tenantID, projectID, env, releaseID, updatedBy, correlationID string) error
	SetActive(ctx context.Context, tx db.DBTX, tenantID, projectID, env, releaseID string) error
}

// RollbackGitOpsReader abstracts manifest retrieval from git history (DIP).
type RollbackGitOpsReader interface {
	GetManifestsAtCommit(ctx context.Context, tenantID, projectID, env, commitSHA string) ([]byte, error)
}

// RollbackGitOpsPusher abstracts manifest push to gitops repo (DIP).
type RollbackGitOpsPusher interface {
	Push(ctx context.Context, opts deploycontracts.GitOpsPushOpts) (string, error)
}

// RollbackSyncClient abstracts ArgoCD sync operations (DIP).
type RollbackSyncClient interface {
	TriggerSync(ctx context.Context, appName string) error
	// WaitForSync blocks until the deployment infrastructure confirms
	// the gitops manifest has been applied AND the target workloads
	// are healthy (or until timeout fires). Required for the canonical
	// "DB safety invariant" — release state flip happens ONLY after
	// this returns nil. LIVE-caught 2026-06-18 sponsor mandate.
	WaitForSync(ctx context.Context, appName string, timeout time.Duration, deployedApps []string) error
}

// RollbackOrchestratorDeps contains all dependencies for the rollback orchestrator.
type RollbackOrchestratorDeps struct {
	Releases   RollbackReleaseStore
	EnvTargets RollbackEnvTargetStore
	GitReader  RollbackGitOpsReader
	GitPusher  RollbackGitOpsPusher
	Sync       RollbackSyncClient
	Locker     deploycontracts.EnvironmentLocker
	Delivery   deploycontracts.DeliveryActivator // MEET-009: polymorphic rollback delivery
	// ArgocdSyncTimeout bounds the post-trigger sync confirmation wait.
	// LIVE-caught 2026-06-18 sponsor mandate: DB state flip requires
	// sync verification (not just trigger). Zero defaults to 120s.
	ArgocdSyncTimeout time.Duration
	Logger            *slog.Logger
}

// RollbackRequest contains the inputs for a rollback operation.
type RollbackRequest struct {
	TenantID      string
	ProjectID     string
	ProjectName   string
	Environment   string
	ToReleaseID   string // optional — if empty, uses most recent SUPERSEDED/ROLLED_BACK
	CorrelationID string
	UserID        string
	BaseDomain    string
}

// RollbackResult contains the outcome of a rollback operation.
type RollbackResult struct {
	ReactivatedRelease *db.Release // release that was reactivated (target)
	RolledBackRelease  *db.Release // release that was rolled back (was ACTIVE)
	Environment        string
	URL                string
}

// RollbackOrchestrator coordinates the rollback workflow.
type RollbackOrchestrator struct {
	deps RollbackOrchestratorDeps
}

// NewRollbackOrchestrator creates a new RollbackOrchestrator.
func NewRollbackOrchestrator(deps RollbackOrchestratorDeps) *RollbackOrchestrator {
	return &RollbackOrchestrator{deps: deps}
}

// Rollback executes the rollback workflow:
// 1. Lock environment
// 2. Find current ACTIVE release and target release
// 3. Fetch old manifests from git
// 4. Push old manifests to gitops repo
// 5. Transition current release: ACTIVE → ROLLING_BACK → ROLLED_BACK
// 6. Reactivate target release: SUPERSEDED/ROLLED_BACK → ACTIVE
// 7. Update environment target pointers
func (o *RollbackOrchestrator) Rollback(ctx context.Context, req RollbackRequest) (*RollbackResult, error) {
	ctx, span := otel.Tracer("loopzero-api").Start(ctx, "loopzero.api.rollback.execute",
		trace.WithAttributes(
			attribute.String("loopzero.tenant", req.TenantID),
			attribute.String("loopzero.project", req.ProjectID),
			attribute.String("loopzero.environment", req.Environment),
		))
	defer span.End()

	logger := o.deps.Logger.With(
		"tenant_id", req.TenantID,
		"project_id", req.ProjectID,
		"environment", req.Environment,
		"correlation_id", req.CorrelationID,
	)

	// Phase 1 (INSIDE lock): validate and resolve target
	var currentRelease, targetRelease *db.Release
	err := o.deps.Locker.WithLock(ctx, req.TenantID, req.ProjectID, req.Environment, func(tx db.DBTX) error {
		// Find current ACTIVE release
		active, err := o.deps.Releases.GetActiveByEnvTx(ctx, tx, req.TenantID, req.ProjectID, req.Environment)
		if err != nil {
			return fmt.Errorf("rollback: get active release: %w", err)
		}
		if active == nil {
			return loopzeroerrors.ErrNoActiveRelease
		}
		currentRelease = active

		// Resolve target release
		if req.ToReleaseID != "" {
			target, err := o.deps.Releases.GetByIDForTenant(ctx, req.TenantID, req.ToReleaseID)
			if err != nil {
				return fmt.Errorf("rollback: get target release: %w", err)
			}
			if target == nil {
				return loopzeroerrors.ErrReleaseNotFound
			}
			targetRelease = target
		} else {
			target, err := o.deps.Releases.GetPreviousRelease(ctx, req.TenantID, req.ProjectID, req.Environment)
			if err != nil {
				return fmt.Errorf("rollback: get previous release: %w", err)
			}
			if target == nil {
				return loopzeroerrors.ErrNoPreviousRelease
			}
			targetRelease = target
		}

		// Transition current release: ACTIVE → ROLLING_BACK
		_, err = o.deps.Releases.UpdateStatus(ctx, tx, currentRelease.ID, string(statemachine.RPEventRollbackStarted))
		if err != nil {
			return fmt.Errorf("rollback: transition to ROLLING_BACK: %w", err)
		}

		return nil
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	logger.Info("rollback initiated",
		"current_release", currentRelease.ID,
		"target_release", targetRelease.ID,
	)

	// Phases 2-4 (OUTSIDE lock): delivery-specific rollback via DeliveryActivator.
	// For containers: fetch old manifests from git, push to gitea, trigger ArgoCD sync.
	// For static (future): push IngressRoute with old release-id path.
	// MEET-2026-03-26-009: polymorphic rollback via interface.
	if o.deps.Delivery != nil {
		if err := o.deps.Delivery.Rollback(ctx, deploycontracts.RollbackDeliveryRequest{
			TenantID:      req.TenantID,
			ProjectID:     req.ProjectID,
			ProjectName:   req.ProjectName,
			Environment:   req.Environment,
			TargetRelease: rollbackTargetFrom(targetRelease),
			CorrelationID: req.CorrelationID,
		}); err != nil {
			return nil, o.failRollback(ctx, req.TenantID, req.ProjectID, req.Environment, currentRelease.ID, fmt.Errorf("rollback: delivery failed: %w", err))
		}
	} else {
		// Legacy fallback: inline git fetch + push + sync (for backward compat in tests)
		if targetRelease.GitCommit == "" {
			return nil, o.failRollback(ctx, req.TenantID, req.ProjectID, req.Environment, currentRelease.ID, fmt.Errorf("target release has no git commit"))
		}
		manifests, err := o.deps.GitReader.GetManifestsAtCommit(ctx, req.TenantID, req.ProjectID, req.Environment, targetRelease.GitCommit)
		if err != nil {
			return nil, o.failRollback(ctx, req.TenantID, req.ProjectID, req.Environment, currentRelease.ID, fmt.Errorf("rollback: fetch manifests: %w", err))
		}
		_, err = o.deps.GitPusher.Push(ctx, deploycontracts.GitOpsPushOpts{
			TenantID: req.TenantID, ProjectID: req.ProjectID, Environment: req.Environment,
			Manifests: string(manifests), ReleaseID: targetRelease.ID, CorrelationID: req.CorrelationID,
		})
		if err != nil {
			return nil, o.failRollback(ctx, req.TenantID, req.ProjectID, req.Environment, currentRelease.ID, fmt.Errorf("rollback: push manifests: %w", err))
		}
		// MEET-2026-03-27-012 Phase 0.2 + 1.4 fixed a name that used the project in
		// place of the tenant. The canonical identity is tenant + project + env, via
		// naming.ArgoAppName — the project belongs in the name, just not alone (#271).
		// And do NOT swallow sync failure — a rollback that doesn't sync is a lie.
		appName, nameErr := naming.ArgoAppName(req.TenantID, req.ProjectID, req.Environment)
		if nameErr != nil {
			return nil, o.failRollback(ctx, req.TenantID, req.ProjectID, req.Environment, currentRelease.ID,
				fmt.Errorf("rollback: argocd application name: %w", nameErr))
		}
		if syncErr := o.deps.Sync.TriggerSync(ctx, appName); syncErr != nil {
			return nil, o.failRollback(ctx, req.TenantID, req.ProjectID, req.Environment, currentRelease.ID,
				fmt.Errorf("rollback: sync trigger failed for %s: %w", appName, syncErr))
		}
		// LIVE-caught 2026-06-18 sponsor mandate: "o banco deve ser
		// atualizado somente COM GARANTIA MAXIMA QUE O ROLLBACK
		// aconteceu". TriggerSync alone returns immediately + the
		// deployment infrastructure might fail mid-sync. Wait for
		// real confirmation before letting Phase 5 flip DB state.
		timeout := o.deps.ArgocdSyncTimeout
		if timeout == 0 {
			timeout = 120 * time.Second
		}
		if waitErr := o.deps.Sync.WaitForSync(ctx, appName, timeout, nil); waitErr != nil {
			return nil, o.failRollback(ctx, req.TenantID, req.ProjectID, req.Environment, currentRelease.ID,
				fmt.Errorf("rollback: sync wait failed for %s: %w", appName, waitErr))
		}
	}

	// Phase 5 (INSIDE lock): finalize — mark current as ROLLED_BACK, reactivate target.
	// Re-verify state inside lock to guard against concurrent deploys between Phase 1 and 5.
	err = o.deps.Locker.WithLock(ctx, req.TenantID, req.ProjectID, req.Environment, func(tx db.DBTX) error {
		// Re-fetch current release inside lock to verify it's still ROLLING_BACK.
		// A concurrent deploy that started between Phase 1 and Phase 5 could have
		// superseded this release, making the transition invalid.
		fresh, fetchErr := o.deps.Releases.GetByIDForTenant(ctx, req.TenantID, currentRelease.ID)
		if fetchErr != nil {
			return fmt.Errorf("rollback: re-fetch current release: %w", fetchErr)
		}
		if fresh == nil || fresh.Status != string(statemachine.RPhRollingBack) {
			return fmt.Errorf("rollback: release %s is no longer in ROLLING_BACK state (current: %s), aborting",
				currentRelease.ID, fresh.Status)
		}

		// Transition current: ROLLING_BACK → ROLLED_BACK
		_, err := o.deps.Releases.UpdateStatus(ctx, tx, currentRelease.ID, string(statemachine.RPEventRollbackCompleted))
		if err != nil {
			return fmt.Errorf("rollback: transition to ROLLED_BACK: %w", err)
		}

		// Reactivate target: SUPERSEDED/ROLLED_BACK → ACTIVE
		_, err = o.deps.Releases.UpdateStatus(ctx, tx, targetRelease.ID, string(statemachine.RPEventReactivated))
		if err != nil {
			return fmt.Errorf("rollback: reactivate target release: %w", err)
		}

		// Update environment target pointers
		if err := o.deps.EnvTargets.SetDesired(ctx, tx, req.TenantID, req.ProjectID, req.Environment, targetRelease.ID, req.UserID, req.CorrelationID); err != nil {
			return fmt.Errorf("rollback: set desired release: %w", err)
		}
		if err := o.deps.EnvTargets.SetActive(ctx, tx, req.TenantID, req.ProjectID, req.Environment, targetRelease.ID); err != nil {
			return fmt.Errorf("rollback: set active release: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	logger.Info("rollback completed",
		"rolled_back_release", currentRelease.ID,
		"reactivated_release", targetRelease.ID,
	)

	url := ""
	if req.BaseDomain != "" {
		url = fmt.Sprintf("https://%s-%s.%s", req.ProjectName, req.Environment, req.BaseDomain)
	}

	return &RollbackResult{
		ReactivatedRelease: targetRelease,
		RolledBackRelease:  currentRelease,
		Environment:        req.Environment,
		URL:                url,
	}, nil
}

// failRollback transitions the current release to ROLLBACK_FAILED state.
// REVIEW-FIX: Pass correct lock coordinates instead of empty strings to avoid
// global lock contention. Also log lock/transition errors instead of discarding.
func (o *RollbackOrchestrator) failRollback(ctx context.Context, tenant, project, env, releaseID string, cause error) error {
	// LIVE-caught 2026-06-18: when sync wait times out the caller's
	// ctx is already cancelled. Using it for the lock + UpdateStatus
	// fails immediately ("lock: begin transaction: context canceled")
	// → release stays in ROLLING_BACK forever → next rollback attempt
	// rejects with "no longer in ROLLING_BACK state" cascade.
	//
	// Fix: detach a fresh ctx with a bounded budget so failRollback
	// ALWAYS gets a chance to write ROLLBACK_FAILED, even when the
	// upstream HTTP request died. WithoutCancel propagates trace
	// context for observability while ignoring the parent's cancel.
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := o.deps.Locker.WithLock(cleanupCtx, tenant, project, env, func(tx db.DBTX) error {
		if _, err := o.deps.Releases.UpdateStatus(cleanupCtx, tx, releaseID, string(statemachine.RPEventRollbackFailed)); err != nil {
			return fmt.Errorf("failRollback: update status: %w", err)
		}
		return nil
	}); err != nil {
		slog.Error("failRollback: could not transition release to ROLLBACK_FAILED",
			"release_id", releaseID, "lock_error", err)
	}
	return cause
}
