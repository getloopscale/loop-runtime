package controller

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/conditions"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

const releaseRequeueInterval = 15 * time.Second

// releasePhaseToSMState maps CRD ReleasePhase values to statemachine State values.
var releasePhaseToSMState = map[loopzerov1.ReleasePhase]statemachine.State{
	loopzerov1.ReleasePhaseReady:           statemachine.ReleaseReady,
	loopzerov1.ReleasePhasePending:         statemachine.ReleasePending,
	loopzerov1.ReleasePhaseActive:          statemachine.ReleaseActive,
	loopzerov1.ReleasePhaseGitMirrorFailed: statemachine.ReleaseGitMirrorFailed,
	loopzerov1.ReleasePhaseSyncFailed:      statemachine.ReleaseSyncFailed,
	loopzerov1.ReleasePhaseDriftAlert:      statemachine.ReleaseDriftAlert,
	loopzerov1.ReleasePhaseSuperseded:      statemachine.ReleaseSuperseded,
	loopzerov1.ReleasePhaseRollingBack:     statemachine.ReleaseRollingBack,
	loopzerov1.ReleasePhaseRolledBack:      statemachine.ReleaseRolledBack,
	loopzerov1.ReleasePhaseRollbackFailed:  statemachine.ReleaseRollbackFailed,
	loopzerov1.ReleasePhaseCancelled:       statemachine.ReleaseCancelled,
}

// isReleaseStablePhase returns true if the phase is a state where the controller
// should not requeue (terminal states or states waiting for external events
// like Active, DriftAlert, RollbackFailed, GitMirrorFailed, SyncFailed).
func isReleaseStablePhase(phase loopzerov1.ReleasePhase) bool {
	switch phase {
	case loopzerov1.ReleasePhaseActive,
		loopzerov1.ReleasePhaseSuperseded,
		loopzerov1.ReleasePhaseRolledBack,
		loopzerov1.ReleasePhaseCancelled,
		loopzerov1.ReleasePhaseDriftAlert,
		loopzerov1.ReleasePhaseRollbackFailed,
		loopzerov1.ReleasePhaseGitMirrorFailed,
		loopzerov1.ReleasePhaseSyncFailed:
		return true
	}
	return false
}

// ReleaseReconciler reconciles a LoopZeroRelease object.
// It tracks release phase transitions using the release state machine,
// sets conditions, and monitors GitOps sync progress.
type ReleaseReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	machine *statemachine.Machine
}

// Reconcile handles the reconciliation loop for LoopZeroRelease resources.
func (r *ReleaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var release loopzerov1.LoopZeroRelease
	if err := r.Get(ctx, req.NamespacedName, &release); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Stable phases (terminal or waiting for external event): no-op, no requeue.
	if release.Status.Phase != "" && isReleaseStablePhase(release.Status.Phase) {
		logger.Info("release in stable state, skipping reconcile",
			"release", release.Name, "phase", release.Status.Phase)
		return ctrl.Result{}, nil
	}

	// Phase empty: initialize to Ready.
	if release.Status.Phase == "" {
		return r.reconcileInitial(ctx, &release)
	}

	// Phase Ready: waiting for GitOps push.
	if release.Status.Phase == loopzerov1.ReleasePhaseReady {
		return r.reconcileReady(ctx, &release)
	}

	// Phase Pending: waiting for ArgoCD sync.
	if release.Status.Phase == loopzerov1.ReleasePhasePending {
		return r.reconcilePending(ctx, &release)
	}

	return ctrl.Result{}, nil
}

// reconcileInitial sets the release to Ready phase and initializes conditions.
func (r *ReleaseReconciler) reconcileInitial(ctx context.Context, release *loopzerov1.LoopZeroRelease) (ctrl.Result, error) {
	release.Status.Phase = loopzerov1.ReleasePhaseReady

	// GitMirrorCompleted is False initially.
	conditions.SetCondition(&release.Status.Conditions, conditions.TypeGitMirrorCompleted,
		metav1.ConditionFalse, conditions.ReasonWaiting,
		"waiting for GitOps push to complete",
		release.Generation)

	// SyncCompleted is False initially.
	conditions.SetCondition(&release.Status.Conditions, conditions.TypeSyncCompleted,
		metav1.ConditionFalse, conditions.ReasonWaiting,
		"waiting for sync to complete",
		release.Generation)

	release.Status.ObservedGeneration = release.Generation

	if err := r.Status().Update(ctx, release); err != nil {
		return ctrl.Result{}, fmt.Errorf("setting initial release status: %w", err)
	}

	return ctrl.Result{}, nil
}

// reconcileReady handles the Ready phase. The release is waiting for the GitOps
// push to complete. In v1, the API performs the actual Gitea push.
func (r *ReleaseReconciler) reconcileReady(ctx context.Context, release *loopzerov1.LoopZeroRelease) (ctrl.Result, error) {
	// Ensure conditions are current for this generation.
	if release.Status.ObservedGeneration != release.Generation {
		conditions.SetCondition(&release.Status.Conditions, conditions.TypeGitMirrorCompleted,
			metav1.ConditionFalse, conditions.ReasonWaiting,
			"waiting for GitOps push to complete",
			release.Generation)

		release.Status.ObservedGeneration = release.Generation

		if err := r.Status().Update(ctx, release); err != nil {
			return ctrl.Result{}, fmt.Errorf("updating release conditions: %w", err)
		}
	}

	// Requeue to poll for external state changes (git mirror result).
	return ctrl.Result{RequeueAfter: releaseRequeueInterval}, nil
}

// reconcilePending handles the Pending phase. The release is waiting for ArgoCD
// sync to complete. In v1, the API monitors ArgoCD sync status.
func (r *ReleaseReconciler) reconcilePending(ctx context.Context, release *loopzerov1.LoopZeroRelease) (ctrl.Result, error) {
	_ = ctx // used in future iterations when polling ArgoCD status

	// In v1, the API or an external callback sets the terminal state.
	// The controller just requeues to detect changes.
	return ctrl.Result{RequeueAfter: releaseRequeueInterval}, nil
}

// SetupReleaseController registers the ReleaseController with the manager.
func SetupReleaseController(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopzerov1.LoopZeroRelease{}).
		Named("release").
		Complete(&ReleaseReconciler{
			Client:  mgr.GetClient(),
			Scheme:  mgr.GetScheme(),
			machine: statemachine.NewReleaseMachine(),
		})
}
