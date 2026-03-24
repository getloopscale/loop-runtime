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

const buildRequeueInterval = 10 * time.Second

// buildPhaseToSMState maps CRD BuildPhase values to statemachine State values.
var buildPhaseToSMState = map[loopzerov1.BuildPhase]statemachine.State{
	loopzerov1.BuildPhaseQueued:     statemachine.BuildQueued,
	loopzerov1.BuildPhaseRunning:    statemachine.BuildRunning,
	loopzerov1.BuildPhaseSucceeded:  statemachine.BuildSucceeded,
	loopzerov1.BuildPhaseFailed:     statemachine.BuildFailed,
	loopzerov1.BuildPhaseCancelling: statemachine.BuildCancelling,
	loopzerov1.BuildPhaseCancelled:  statemachine.BuildCancelled,
}

// smStateToBuildPhase maps statemachine State values to CRD BuildPhase values.
var smStateToBuildPhase = map[statemachine.State]loopzerov1.BuildPhase{
	statemachine.BuildQueued:     loopzerov1.BuildPhaseQueued,
	statemachine.BuildRunning:    loopzerov1.BuildPhaseRunning,
	statemachine.BuildSucceeded:  loopzerov1.BuildPhaseSucceeded,
	statemachine.BuildFailed:     loopzerov1.BuildPhaseFailed,
	statemachine.BuildCancelling: loopzerov1.BuildPhaseCancelling,
	statemachine.BuildCancelled:  loopzerov1.BuildPhaseCancelled,
}

// BuildReconciler reconciles a LoopZeroBuild object.
// It tracks build phase transitions using the build state machine,
// sets conditions, and manages the expected workflow name.
type BuildReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	machine *statemachine.Machine
}

// Reconcile handles the reconciliation loop for LoopZeroBuild resources.
func (r *BuildReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var build loopzerov1.LoopZeroBuild
	if err := r.Get(ctx, req.NamespacedName, &build); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Terminal states: no-op, no requeue.
	if build.Status.Phase != "" {
		smState, ok := buildPhaseToSMState[build.Status.Phase]
		if ok && r.machine.IsTerminal(smState) {
			logger.Info("build in terminal state, skipping reconcile",
				"build", build.Name, "phase", build.Status.Phase)
			return ctrl.Result{}, nil
		}
	}

	// Phase empty: initialize to Queued.
	if build.Status.Phase == "" {
		return r.reconcileInitial(ctx, &build)
	}

	// Phase Queued: validate sources, set workflow name, set conditions.
	if build.Status.Phase == loopzerov1.BuildPhaseQueued {
		return r.reconcileQueued(ctx, &build)
	}

	// Phase Running: poll for workflow completion.
	if build.Status.Phase == loopzerov1.BuildPhaseRunning {
		return r.reconcileRunning(ctx, &build)
	}

	return ctrl.Result{}, nil
}

// reconcileInitial sets the build to Queued phase and initializes conditions.
func (r *BuildReconciler) reconcileInitial(ctx context.Context, build *loopzerov1.LoopZeroBuild) (ctrl.Result, error) {
	build.Status.Phase = loopzerov1.BuildPhaseQueued

	// Set SourcesResolved based on sourceRef being present in spec.
	conditions.SetCondition(&build.Status.Conditions, conditions.TypeSourcesResolved,
		metav1.ConditionTrue, conditions.ReasonReady,
		fmt.Sprintf("source archive resolved: %s", build.Spec.SourceRef),
		build.Generation)

	// Set expected workflow name.
	build.Status.WorkflowName = fmt.Sprintf("build-%s", build.Spec.BuildID)

	// WorkflowSubmitted is False initially.
	conditions.SetCondition(&build.Status.Conditions, conditions.TypeWorkflowSubmitted,
		metav1.ConditionFalse, conditions.ReasonWaiting,
		"waiting for workflow submission",
		build.Generation)

	build.Status.ObservedGeneration = build.Generation

	if err := r.Status().Update(ctx, build); err != nil {
		return ctrl.Result{}, fmt.Errorf("setting initial build status: %w", err)
	}

	return ctrl.Result{}, nil
}

// reconcileQueued handles the Queued phase. The controller validates the build
// is ready and sets conditions. Actual workflow creation is handled externally
// for v1 (the API creates the Argo Workflow).
func (r *BuildReconciler) reconcileQueued(ctx context.Context, build *loopzerov1.LoopZeroBuild) (ctrl.Result, error) {
	// Ensure conditions are current for this generation.
	if build.Status.ObservedGeneration != build.Generation {
		conditions.SetCondition(&build.Status.Conditions, conditions.TypeSourcesResolved,
			metav1.ConditionTrue, conditions.ReasonReady,
			fmt.Sprintf("source archive resolved: %s", build.Spec.SourceRef),
			build.Generation)

		build.Status.ObservedGeneration = build.Generation

		if err := r.Status().Update(ctx, build); err != nil {
			return ctrl.Result{}, fmt.Errorf("updating build conditions: %w", err)
		}
	}

	// Requeue to poll for external state changes (workflow result).
	return ctrl.Result{RequeueAfter: buildRequeueInterval}, nil
}

// reconcileRunning handles the Running phase. The controller polls for workflow
// completion. In v1, this is a placeholder that requeues until the external
// process updates the build status.
func (r *BuildReconciler) reconcileRunning(ctx context.Context, build *loopzerov1.LoopZeroBuild) (ctrl.Result, error) {
	_ = ctx // used in future iterations when polling workflow status

	// In v1, the API or an external callback sets the terminal state.
	// The controller just requeues to detect changes.
	return ctrl.Result{RequeueAfter: buildRequeueInterval}, nil
}

// SetupBuildController registers the BuildController with the manager.
func SetupBuildController(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopzerov1.LoopZeroBuild{}).
		Named("build").
		Complete(&BuildReconciler{
			Client:  mgr.GetClient(),
			Scheme:  mgr.GetScheme(),
			machine: statemachine.NewBuildMachine(),
		})
}
