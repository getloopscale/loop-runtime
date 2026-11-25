package release

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
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
	// AppendSideEffect records why a rollback failed on the release that
	// stays active.
	AppendSideEffect(ctx context.Context, tx db.DBTX, id string, patch map[string]any) error
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
	// CompletionTimeout bounds the completion of a rollback after its sync was
	// triggered: the wait for the sync and the state change that follows. It
	// runs apart from the request, which a rollout through canary steps
	// outlives. Zero means defaultRollbackCompletionTimeout.
	CompletionTimeout time.Duration
}

const (
	// defaultRollbackCompletionTimeout leaves room for the delivery's own sync
	// timeout (ARGOCD_SYNC_TIMEOUT, 5 minutes by default) and the state change.
	defaultRollbackCompletionTimeout = 15 * time.Minute
	// rollbackDeliveryStartTimeout bounds the push of the manifests and the
	// sync trigger, which the request waits for.
	rollbackDeliveryStartTimeout = 2 * time.Minute
	// rollbackStateTimeout bounds one state change made apart from the request.
	rollbackStateTimeout = 30 * time.Second
)

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
	// completions counts the rollbacks whose sync is still being awaited.
	completions sync.WaitGroup
}

// NewRollbackOrchestrator creates a new RollbackOrchestrator.
func NewRollbackOrchestrator(deps RollbackOrchestratorDeps) *RollbackOrchestrator {
	return &RollbackOrchestrator{deps: deps}
}

// Wait blocks until every rollback this orchestrator started has completed.
// A shutdown calls it so that no release is left ROLLING_BACK, and tests call
// it to observe the outcome.
func (o *RollbackOrchestrator) Wait() { o.completions.Wait() }

// Rollback starts a rollback and returns once the target's manifests are pushed
// and the sync is triggered. The rest runs apart from the caller's context:
//
//  1. Lock the environment; current ACTIVE release → ROLLING_BACK; resolve the
//     target. This is the only step the caller's context can cancel.
//  2. Re-deliver the target's manifests and trigger the sync.
//  3. In the background, with its own deadline: wait for the sync, then
//     current → ROLLED_BACK, target → ACTIVE, environment target → target.
//
// A release rolls its pods through canary steps, so step 3 outlives an HTTP
// request (#118). Whatever happens after step 1, the environment ends with
// one active release: the target when the cluster runs it, and the release
// rolled back from otherwise, with the failure recorded on it.
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

	currentRelease, targetRelease, err := o.beginRollback(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	logger.Info("rollback initiated",
		"current_release", currentRelease.ID,
		"target_release", targetRelease.ID,
	)

	// From here the rollback belongs to the environment, not to the request:
	// the current release is ROLLING_BACK, and only this code moves it on.
	// WithoutCancel keeps the trace and drops the caller's cancellation.
	work := context.WithoutCancel(ctx)
	run := rollbackRun{
		req: req, current: currentRelease, target: targetRelease, logger: logger,
		delivery: deploycontracts.RollbackDeliveryRequest{
			TenantID:      req.TenantID,
			ProjectID:     req.ProjectID,
			ProjectName:   req.ProjectName,
			Environment:   req.Environment,
			TargetRelease: rollbackTargetFrom(targetRelease),
			CorrelationID: req.CorrelationID,
		},
	}

	startCtx, cancelStart := context.WithTimeout(work, rollbackDeliveryStartTimeout)
	err = o.deps.Delivery.Rollback(startCtx, run.delivery)
	cancelStart()
	if err != nil {
		cause := fmt.Errorf("rollback: delivery failed: %w", err)
		o.keepCurrentActive(work, run, cause)
		span.RecordError(cause)
		span.SetStatus(codes.Error, cause.Error())
		return nil, cause
	}

	o.completions.Add(1)
	go func() {
		defer o.completions.Done()
		o.complete(work, run)
	}()

	url := entryAppURL(req.TenantID, targetRelease.ProjectID, req.Environment, targetRelease.Apps,
		UserFacingDomain(o.deps.PublicDomain, req.BaseDomain))

	return &RollbackResult{
		ReactivatedRelease: targetRelease,
		RolledBackRelease:  currentRelease,
		Environment:        req.Environment,
		URL:                url,
	}, nil
}

// rollbackRun is one rollback in flight: what was asked, the release being
// rolled back from and the one being reactivated.
type rollbackRun struct {
	req      RollbackRequest
	current  *db.Release
	target   *db.Release
	delivery deploycontracts.RollbackDeliveryRequest
	logger   *slog.Logger
}

// beginRollback resolves the target and moves the current ACTIVE release to
// ROLLING_BACK, inside the environment lock.
func (o *RollbackOrchestrator) beginRollback(ctx context.Context, req RollbackRequest) (current, target *db.Release, err error) {
	err = o.deps.Locker.WithLock(ctx, req.TenantID, req.ProjectID, req.Environment, func(tx db.DBTX) error {
		active, err := o.deps.Releases.GetActiveByEnvTx(ctx, tx, req.TenantID, req.ProjectID, req.Environment)
		if err != nil {
			return fmt.Errorf("rollback: get active release: %w", err)
		}
		if active == nil {
			return loopzeroerrors.ErrNoActiveRelease
		}
		current = active

		if req.ToReleaseID != "" {
			found, err := o.deps.Releases.GetByIDForTenant(ctx, req.TenantID, req.ToReleaseID)
			if err != nil {
				return fmt.Errorf("rollback: get target release: %w", err)
			}
			if found == nil {
				return loopzeroerrors.ErrReleaseNotFound
			}
			target = found
		} else {
			found, err := o.deps.Releases.GetPreviousRelease(ctx, req.TenantID, req.ProjectID, req.Environment)
			if err != nil {
				return fmt.Errorf("rollback: get previous release: %w", err)
			}
			if found == nil {
				return loopzeroerrors.ErrNoPreviousRelease
			}
			target = found
		}

		if _, err := o.deps.Releases.UpdateStatus(ctx, tx, current.ID, string(statemachine.RPEventRollbackStarted)); err != nil {
			return fmt.Errorf("rollback: transition to ROLLING_BACK: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return current, target, nil
}

// complete waits for the sync of a started rollback and settles the release
// state. It never leaves the environment without an active release: when the
// target cannot be activated, the release rolled back from is active again.
func (o *RollbackOrchestrator) complete(work context.Context, run rollbackRun) {
	timeout := o.deps.CompletionTimeout
	if timeout <= 0 {
		timeout = defaultRollbackCompletionTimeout
	}
	waitCtx, cancel := context.WithTimeout(work, timeout)
	waitErr := o.deps.Delivery.AwaitRollback(waitCtx, run.delivery)
	cancel()

	if waitErr != nil && !o.clusterRunsTheTarget(work, run, waitErr) {
		o.keepCurrentActive(work, run, fmt.Errorf("rollback: sync not confirmed: %w", waitErr))
		return
	}

	if err := o.activateTarget(work, run); err != nil {
		run.logger.Error("rollback: the sync was confirmed but the release state could not be finalized",
			"current_release", run.current.ID, "target_release", run.target.ID, "error", err)
		o.keepCurrentActive(work, run, err)
		return
	}

	run.logger.Info("rollback completed",
		"rolled_back_release", run.current.ID,
		"reactivated_release", run.target.ID,
	)

	scaleCtx, cancelScale := context.WithTimeout(work, rollbackDeliveryStartTimeout)
	defer cancelScale()
	o.deliverSavedScale(scaleCtx, run.logger, run.req)
}

// clusterRunsTheTarget asks, after a failed wait, whether the environment
// converged on the rolled back manifests all the same. A wait fails for
// reasons that say nothing about the cells: its deadline, or a lost
// connection to the sync API. An answer that cannot be had counts as no.
func (o *RollbackOrchestrator) clusterRunsTheTarget(work context.Context, run rollbackRun, waitErr error) bool {
	checkCtx, cancel := context.WithTimeout(work, rollbackStateTimeout)
	defer cancel()
	converged, err := o.deps.Delivery.RollbackConverged(checkCtx, run.delivery)
	if err != nil {
		run.logger.Error("rollback: the sync wait failed and the cluster state could not be read",
			"wait_error", waitErr, "error", err)
		return false
	}
	if converged {
		run.logger.Warn("rollback: the sync wait failed but the cluster runs the target; completing",
			"wait_error", waitErr, "target_release", run.target.ID)
	}
	return converged
}

// activateTarget is the state change of a confirmed rollback, inside the
// environment lock: current ROLLING_BACK → ROLLED_BACK, target → ACTIVE, and
// the environment target pointers.
func (o *RollbackOrchestrator) activateTarget(work context.Context, run rollbackRun) error {
	ctx, cancel := context.WithTimeout(work, rollbackStateTimeout)
	defer cancel()
	req := run.req
	return o.deps.Locker.WithLock(ctx, req.TenantID, req.ProjectID, req.Environment, func(tx db.DBTX) error {
		// A deploy that started while the sync was awaited may have moved the
		// current release on, which makes this transition invalid.
		fresh, fetchErr := o.deps.Releases.GetByIDForTenant(ctx, req.TenantID, run.current.ID)
		if fetchErr != nil {
			return fmt.Errorf("rollback: re-fetch current release: %w", fetchErr)
		}
		if fresh == nil {
			return fmt.Errorf("rollback: release %s disappeared before finalize: %w",
				run.current.ID, loopzeroerrors.ErrReleaseNotFound)
		}
		if fresh.Status != string(statemachine.RPhRollingBack) {
			return loopzeroerrors.NewInvalidTransition(fresh.Status, fmt.Sprintf(
				"%s: release %s is no longer in ROLLING_BACK state, aborting",
				statemachine.RPEventRollbackCompleted, run.current.ID))
		}

		if _, err := o.deps.Releases.UpdateStatus(ctx, tx, run.current.ID, string(statemachine.RPEventRollbackCompleted)); err != nil {
			return fmt.Errorf("rollback: transition to ROLLED_BACK: %w", err)
		}
		if _, err := o.deps.Releases.UpdateStatus(ctx, tx, run.target.ID, string(statemachine.RPEventReactivated)); err != nil {
			return fmt.Errorf("rollback: reactivate target release: %w", err)
		}
		if err := o.deps.EnvTargets.SetDesired(ctx, tx, req.TenantID, req.ProjectID, req.Environment, run.target.ID, req.UserID, req.CorrelationID); err != nil {
			return fmt.Errorf("rollback: set desired release: %w", err)
		}
		if err := o.deps.EnvTargets.SetActive(ctx, tx, req.TenantID, req.ProjectID, req.Environment, run.target.ID); err != nil {
			return fmt.Errorf("rollback: set active release: %w", err)
		}
		return nil
	})
}

// keepCurrentActive settles a rollback that did not happen: the release rolled
// back from is ACTIVE again and carries why, so the environment is never left
// without an active release. The state machine has no direct edge back, so the
// release passes through ROLLBACK_FAILED inside one transaction.
//
// It changes nothing when the release is no longer ROLLING_BACK: something
// else, a deploy for one, already moved the environment on.
func (o *RollbackOrchestrator) keepCurrentActive(work context.Context, run rollbackRun, cause error) {
	ctx, cancel := context.WithTimeout(work, rollbackStateTimeout)
	defer cancel()
	req := run.req
	err := o.deps.Locker.WithLock(ctx, req.TenantID, req.ProjectID, req.Environment, func(tx db.DBTX) error {
		fresh, err := o.deps.Releases.GetByIDForTenant(ctx, req.TenantID, run.current.ID)
		if err != nil {
			return fmt.Errorf("re-fetch current release: %w", err)
		}
		if fresh == nil || fresh.Status != string(statemachine.RPhRollingBack) {
			return nil
		}
		if _, err := o.deps.Releases.UpdateStatus(ctx, tx, run.current.ID, string(statemachine.RPEventRollbackFailed)); err != nil {
			return fmt.Errorf("transition to ROLLBACK_FAILED: %w", err)
		}
		if _, err := o.deps.Releases.UpdateStatus(ctx, tx, run.current.ID, string(statemachine.RPEventManualResolve)); err != nil {
			return fmt.Errorf("transition back to ACTIVE: %w", err)
		}
		return o.deps.Releases.AppendSideEffect(ctx, tx, run.current.ID, map[string]any{
			"rollback_failure": map[string]any{
				"target_release": run.target.ID,
				"reason":         rollbackFailureReason(cause),
				"at":             time.Now().UTC().Format(time.RFC3339),
			},
		})
	})
	if err != nil {
		run.logger.Error("rollback failed and the previous release could not be made active again; the environment has no active release until the next deploy",
			"current_release", run.current.ID, "target_release", run.target.ID, "cause", cause, "error", err)
		return
	}
	run.logger.Error("rollback failed; the previous release stays active",
		"current_release", run.current.ID, "target_release", run.target.ID, "error", cause)
}

// rollbackFailureReason is the cause as the delivery reported it, without the
// orchestrator's own prefixes.
func rollbackFailureReason(cause error) string {
	if inner := errors.Unwrap(cause); inner != nil {
		return inner.Error()
	}
	return cause.Error()
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
