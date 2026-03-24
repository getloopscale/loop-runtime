package controller

import (
	"context"
	"fmt"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/conditions"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

const (
	platformBootstrapRequeueDelay = 5 * time.Second
)

// platformBootstrapSM is the PlatformBootstrap lifecycle state machine (spec section 5).
// Phase transitions go through this machine -- direct assignment is forbidden.
var platformBootstrapSM = statemachine.NewPlatformBootstrapMachine()

// PlatformBootstrapReconciler reconciles a PlatformBootstrap object.
// It configures Gitea, ArgoCD, and seeds the database via the API,
// using HTTP calls only (no pods/exec).
type PlatformBootstrapReconciler struct {
	client.Client
	HTTPClient *http.Client
}

// Reconcile handles the reconciliation loop for PlatformBootstrap resources.
func (r *PlatformBootstrapReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var pb loopzerov1.PlatformBootstrap
	if err := r.Get(ctx, req.NamespacedName, &pb); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Terminal phases: do not requeue.
	if platformBootstrapSM.IsTerminal(statemachine.State(pb.Status.Phase)) {
		return ctrl.Result{}, nil
	}

	// Initialize phase via state machine if empty.
	if pb.Status.Phase == "" {
		pb.Status.Phase = loopzerov1.PlatformBootstrapPhase(
			platformBootstrapSM.Transition(statemachine.PlatformBootstrapEmpty, statemachine.PlatformBootstrapStart),
		)
		pb.Status.Message = "Bootstrap initialized"
		now := metav1.Now()
		pb.Status.LastTransitionTime = &now
		if err := r.Status().Update(ctx, &pb); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Transition from Pending to ConfiguringGitea.
	if pb.Status.Phase == loopzerov1.PlatformBootstrapPhasePending {
		pb.Status.Phase = loopzerov1.PlatformBootstrapPhase(
			platformBootstrapSM.Transition(statemachine.PlatformBootstrapPending, statemachine.PlatformBootstrapStart),
		)
		pb.Status.Message = "Starting Gitea configuration"
		pb.Status.RetryCount = 0
		now := metav1.Now()
		pb.Status.LastTransitionTime = &now
		if err := r.Status().Update(ctx, &pb); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Resolve max retries with default.
	maxRetries := pb.Spec.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 10
	}

	// Phase dispatch.
	switch pb.Status.Phase {
	case loopzerov1.PlatformBootstrapPhaseConfiguringGitea:
		return r.reconcileGiteaPhase(ctx, &pb, maxRetries)

	case loopzerov1.PlatformBootstrapPhaseConfiguringArgoCD:
		return r.reconcileArgoCDPhase(ctx, &pb, maxRetries)

	case loopzerov1.PlatformBootstrapPhaseWaitingForAPI:
		return r.reconcileAPIPhase(ctx, &pb, maxRetries)

	default:
		logger.Info("unknown phase, ignoring", "phase", pb.Status.Phase)
		return ctrl.Result{}, nil
	}
}

// transitionToFailed transitions the CR to Failed phase via the state machine.
func (r *PlatformBootstrapReconciler) transitionToFailed(ctx context.Context, pb *loopzerov1.PlatformBootstrap, message string) (ctrl.Result, error) {
	pb.Status.Phase = loopzerov1.PlatformBootstrapPhase(
		platformBootstrapSM.Transition(statemachine.State(pb.Status.Phase), statemachine.PlatformBootstrapRetryExhaust),
	)
	pb.Status.Message = message
	now := metav1.Now()
	pb.Status.LastTransitionTime = &now
	if err := r.Status().Update(ctx, pb); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// transitionToNextPhase transitions the CR to the next phase via the state machine.
func (r *PlatformBootstrapReconciler) transitionToNextPhase(ctx context.Context, pb *loopzerov1.PlatformBootstrap, event statemachine.Event, message string) (ctrl.Result, error) {
	pb.Status.Phase = loopzerov1.PlatformBootstrapPhase(
		platformBootstrapSM.Transition(statemachine.State(pb.Status.Phase), event),
	)
	pb.Status.Message = message
	pb.Status.RetryCount = 0
	now := metav1.Now()
	pb.Status.LastTransitionTime = &now
	if err := r.Status().Update(ctx, pb); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{Requeue: true}, nil
}

// requeueWithRetry increments the retry count and requeues. If max retries exceeded,
// transitions to Failed.
func (r *PlatformBootstrapReconciler) requeueWithRetry(ctx context.Context, pb *loopzerov1.PlatformBootstrap, maxRetries int, message string) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	pb.Status.RetryCount++
	pb.Status.Message = message
	logger.Info("phase retry", "phase", pb.Status.Phase, "retryCount", pb.Status.RetryCount, "maxRetries", maxRetries, "reason", message)

	if pb.Status.RetryCount >= maxRetries {
		return r.transitionToFailed(ctx, pb, fmt.Sprintf("max retries (%d) exhausted: %s", maxRetries, message))
	}

	if err := r.Status().Update(ctx, pb); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: platformBootstrapRequeueDelay}, nil
}

// httpClient returns the HTTP client, using a default if none was injected.
func (r *PlatformBootstrapReconciler) httpClient() *http.Client {
	if r.HTTPClient != nil {
		return r.HTTPClient
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// getSecret reads a Secret by name in the same namespace as the CR.
func (r *PlatformBootstrapReconciler) getSecret(ctx context.Context, name, namespace string) (*corev1.Secret, error) {
	var secret corev1.Secret
	err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, &secret)
	if err != nil {
		return nil, err
	}
	return &secret, nil
}

// updateSecretData updates or creates a key in a Secret's data.
func (r *PlatformBootstrapReconciler) updateSecretData(ctx context.Context, secret *corev1.Secret, key, value string) error {
	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	secret.Data[key] = []byte(value)
	return r.Update(ctx, secret)
}

// SetupPlatformBootstrapController registers the PlatformBootstrapController with the manager.
func SetupPlatformBootstrapController(mgr ctrl.Manager, httpClient *http.Client) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopzerov1.PlatformBootstrap{}).
		Named("platformbootstrap").
		Complete(&PlatformBootstrapReconciler{
			Client:     mgr.GetClient(),
			HTTPClient: httpClient,
		})
}

// setConditionAndUpdateStatus is a helper that sets a condition and updates status.
func (r *PlatformBootstrapReconciler) setCondition(pb *loopzerov1.PlatformBootstrap, condType string, status metav1.ConditionStatus, reason, message string) {
	conditions.SetCondition(&pb.Status.Conditions, condType, status, reason, message, pb.Generation)
}
