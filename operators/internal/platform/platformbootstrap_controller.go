// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package platform

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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/conditions"
	"github.com/loopzero/loopzero/operators/internal/constants"
	"github.com/loopzero/loopzero/operators/internal/gitea"
	"github.com/loopzero/loopzero/operators/internal/httpclient"
	"github.com/loopzero/loopzero/operators/internal/shared"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// platformBootstrapRequeueDelay uses the centralized constant.
const platformBootstrapRequeueDelay = constants.RequeuePlatformBootstrapDelay

// platformBootstrapSM is the PlatformBootstrap lifecycle state machine (spec section 5).
// Phase transitions go through this machine -- direct assignment is forbidden.
var platformBootstrapSM = statemachine.NewPlatformBootstrapMachine()

// ArgoCDBootstrapClient abstracts ArgoCD HTTP operations for testability.
// ARCH-OP-001: Mirrors the Gitea BootstrapClient pattern -- controllers depend
// on this interface instead of constructing HTTP requests inline.
type ArgoCDBootstrapClient interface {
	// CheckReachable verifies ArgoCD is reachable (GET /api/version).
	CheckReachable(ctx context.Context) error
	// GenerateApiKey creates a non-expiring apiKey for the loopzero-api account.
	GenerateApiKey(ctx context.Context, adminPassword string) (string, error)
}

// PlatformBootstrapReconciler reconciles a PlatformBootstrap object.
// It configures Gitea, ArgoCD, and seeds the database via the API,
// delegating external calls to typed clients (no inline HTTP).
type PlatformBootstrapReconciler struct {
	client.Client
	HTTPClient         *http.Client
	GiteaClientFunc    func(baseURL string) gitea.BootstrapClient    // factory, set at init; nil uses default
	ArgoCDClientFunc   func(baseURL string) ArgoCDBootstrapClient    // factory, set at init; nil uses default (inline HTTP)
}

// Reconcile handles the reconciliation loop for PlatformBootstrap resources.
func (r *PlatformBootstrapReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
	ctx, span := shared.StartReconcileSpan(ctx, "bootstrap", req.Name, req.Namespace)
	defer span.End()
	start := time.Now()
	defer func() {
		if retErr != nil {
			shared.RecordError(span, retErr)
		} else {
			shared.SetSuccess(span)
		}
		shared.ReconcileOutcome("bootstrap", start, retErr)
	}()

	var pb loopzerov1.PlatformBootstrap
	if err := r.Get(ctx, req.NamespacedName, &pb); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion -- clean up platform-tokens secret if present.
	if !pb.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&pb, constants.FinalizerPlatformBootstrap) {
			logger := log.FromContext(ctx)
			logger.Info("cleaning up PlatformBootstrap resources")
			// Delete the platform-tokens secret if managed by this controller.
			tokenSecret := &corev1.Secret{}
			tokenSecretKey := client.ObjectKey{Name: "platform-tokens", Namespace: pb.Namespace}
			if getErr := r.Get(ctx, tokenSecretKey, tokenSecret); getErr != nil {
				if !errors.IsNotFound(getErr) {
					logger.Error(getErr, "failed to get platform-tokens secret during cleanup")
				}
			} else if tokenSecret.Labels[constants.LabelManagedBy] == "platformbootstrap-controller" {
				if err := r.Delete(ctx, tokenSecret); err != nil && !errors.IsNotFound(err) {
					logger.Error(err, "failed to delete platform-tokens secret during cleanup")
				}
			} else {
				logger.Info("skipping platform-tokens delete -- not managed by this controller")
			}
			controllerutil.RemoveFinalizer(&pb, constants.FinalizerPlatformBootstrap)
			if err := r.Update(ctx, &pb); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(&pb, constants.FinalizerPlatformBootstrap) {
		controllerutil.AddFinalizer(&pb, constants.FinalizerPlatformBootstrap)
		if err := r.Update(ctx, &pb); err != nil {
			return ctrl.Result{}, err
		}
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
		maxRetries = constants.DefaultMaxRetries
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
		return r.transitionToFailed(ctx, &pb, fmt.Sprintf("unknown phase %q -- cannot reconcile", pb.Status.Phase))
	}
}

// transitionToFailed transitions the CR to Failed phase via the state machine.
// Uses TryTransition because pb.Status.Phase comes from CRD status (external source).
func (r *PlatformBootstrapReconciler) transitionToFailed(ctx context.Context, pb *loopzerov1.PlatformBootstrap, message string) (ctrl.Result, error) {
	next, err := platformBootstrapSM.TryTransition(statemachine.State(pb.Status.Phase), statemachine.PlatformBootstrapRetryExhaust)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("transition to failed: %w", err)
	}
	pb.Status.Phase = loopzerov1.PlatformBootstrapPhase(next)
	pb.Status.Message = message
	now := metav1.Now()
	pb.Status.LastTransitionTime = &now
	if err := r.Status().Update(ctx, pb); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// transitionToNextPhase transitions the CR to the next phase via the state machine.
// Uses TryTransition because pb.Status.Phase comes from CRD status (external source).
func (r *PlatformBootstrapReconciler) transitionToNextPhase(ctx context.Context, pb *loopzerov1.PlatformBootstrap, event statemachine.Event, message string) (ctrl.Result, error) {
	next, err := platformBootstrapSM.TryTransition(statemachine.State(pb.Status.Phase), event)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("transition to next phase: %w", err)
	}
	pb.Status.Phase = loopzerov1.PlatformBootstrapPhase(next)
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
// ARCH-005: Default client comes from centralized httpclient package.
func (r *PlatformBootstrapReconciler) httpClient() *http.Client {
	if r.HTTPClient != nil {
		return r.HTTPClient
	}
	return httpclient.NewWithTimeout(constants.HTTPTimeoutExternal)
}

// giteaClient returns a BootstrapClient for the given Gitea base URL.
// Uses the injected factory if set, otherwise creates a real APIClient.
func (r *PlatformBootstrapReconciler) giteaClient(baseURL string) gitea.BootstrapClient {
	if r.GiteaClientFunc != nil {
		return r.GiteaClientFunc(baseURL)
	}
	return gitea.NewAPIClient(baseURL, r.httpClient())
}

// argoCDClient returns an ArgoCDBootstrapClient for the given ArgoCD base URL.
// ARCH-OP-001: Uses the injected factory if set (for tests), otherwise creates
// a real HTTP client that talks to the ArgoCD API.
func (r *PlatformBootstrapReconciler) argoCDClient(baseURL string) ArgoCDBootstrapClient {
	if r.ArgoCDClientFunc != nil {
		return r.ArgoCDClientFunc(baseURL)
	}
	return &argoCDHTTPClient{baseURL: baseURL, client: r.httpClient()}
}

// getSecret reads a Secret by name in the same namespace as the CR.
func (r *PlatformBootstrapReconciler) getSecret(ctx context.Context, name, namespace string) (*corev1.Secret, error) {
	logger := log.FromContext(ctx)
	var secret corev1.Secret
	err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, &secret)
	if err != nil {
		logger.Error(err, "failed to read secret", "secret", name, "namespace", namespace)
		return nil, err
	}
	return &secret, nil
}

// secretString safely extracts a string value from a Secret's Data map.
// Returns an error if the key is missing or the value is empty.
func secretString(secret *corev1.Secret, key string) (string, error) {
	if secret.Data == nil {
		return "", fmt.Errorf("secret %s/%s has nil data", secret.Namespace, secret.Name)
	}
	val, ok := secret.Data[key]
	if !ok {
		return "", fmt.Errorf("secret %s/%s missing key %q", secret.Namespace, secret.Name, key)
	}
	s := string(val)
	if s == "" {
		return "", fmt.Errorf("secret %s/%s key %q is empty", secret.Namespace, secret.Name, key)
	}
	return s, nil
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
