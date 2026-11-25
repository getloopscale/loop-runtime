package release

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
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

// RollbackOrchestratorDeps contains all dependencies for the rollback orchestrator.
type RollbackOrchestratorDeps struct {
	Releases   RollbackReleaseStore
	EnvTargets RollbackEnvTargetStore
	Locker     deploycontracts.EnvironmentLocker
	// Delivery re-delivers the target release's artifacts and confirms the sync
	// before Phase 5 flips any release state. Required — it is the same
	// activator the build path uses, wired at the composition root.
	Delivery deploycontracts.DeliveryActivator
	// SavedScale delivers the environment's saved scaling configuration to the
	// reactivated release (scaling.ReleaseScaler). Optional: without it the
	// environment keeps the replica count of the rolled back manifests.
	SavedScale SavedScaleDeliverer
	// PublicDomain is the operator's public wildcard (LOOPZERO_PUBLIC_DOMAIN). The
	// rollback URL is composed on it, falling back to the request's BaseDomain
	// when it is unset — the same rule promotion and activation apply.
	PublicDomain string
	Logger       *slog.Logger
}

// SavedScaleDeliverer makes an environment's running workloads match its saved
// scaling configuration, and reports whether it delivered anything.
type SavedScaleDeliverer interface {
	DeliverSaved(ctx context.Context, tenantID, project, env string) (bool, error)
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

// errRollbackDeliveryNotWired is returned when the orchestrator was constructed
// without a DeliveryActivator — a wiring bug, refused before any release state
// changes rather than discovered as a nil-interface panic mid-rollback.
var errRollbackDeliveryNotWired = &loopzeroerrors.LoopZeroError{
	Code:    loopzeroerrors.ErrInternalError.Code,
	Message: "rollback orchestrator: no delivery activator wired; set RollbackOrchestratorDeps.Delivery at the composition root",
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
// 1. Lock environment; transition current ACTIVE release → ROLLING_BACK
// 2. Resolve the target release
// 3-4. Re-deliver the target's manifests through the DeliveryActivator and
//
//	wait for the sync to confirm them
//
// 5. Transition current release: ROLLING_BACK → ROLLED_BACK
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

	if o.deps.Delivery == nil {
		return nil, errRollbackDeliveryNotWired
	}

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

	// Phases 2-4 (OUTSIDE lock): re-deliver the target release's manifests and
	// wait for the sync to confirm them. The delivery activator returns only
	// after WaitForSync succeeds, which is what lets Phase 5 flip state safely.
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
		if fresh == nil {
			return fmt.Errorf("rollback: release %s disappeared before finalize: %w",
				currentRelease.ID, loopzeroerrors.ErrReleaseNotFound)
		}
		if fresh.Status != string(statemachine.RPhRollingBack) {
			return loopzeroerrors.NewInvalidTransition(fresh.Status, fmt.Sprintf(
				"%s: release %s is no longer in ROLLING_BACK state, aborting",
				statemachine.RPEventRollbackCompleted, currentRelease.ID))
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

	o.deliverSavedScale(ctx, logger, req)

	url := entryAppURL(req.TenantID, targetRelease.ProjectID, req.Environment, targetRelease.Apps,
		UserFacingDomain(o.deps.PublicDomain, req.BaseDomain))

	return &RollbackResult{
		ReactivatedRelease: targetRelease,
		RolledBackRelease:  currentRelease,
		Environment:        req.Environment,
		URL:                url,
	}, nil
}

// deliverSavedScale puts the environment's saved scaling configuration back
// over the rolled back manifests, which carry the replica count of the older
// release. It runs after the target is ACTIVE, because the scaler renders the
// active release. A failure does not fail the rollback: the older release is
// serving, and a repeated scale request delivers the config again.
func (o *RollbackOrchestrator) deliverSavedScale(ctx context.Context, logger *slog.Logger, req RollbackRequest) {
	if o.deps.SavedScale == nil {
		return
	}
	delivered, err := o.deps.SavedScale.DeliverSaved(ctx, req.TenantID, req.ProjectID, req.Environment)
	if err != nil {
		logger.Error("rollback: saved scaling config not delivered; the environment runs the replica count of the rolled back release",
			"error", err)
		return
	}
	if delivered {
		logger.Info("rollback: saved scaling config delivered")
	}
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
