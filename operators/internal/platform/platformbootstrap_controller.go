// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package platform implements the PlatformBootstrap +
// GitRepositoryBinding + ClusterCredential controllers (operator
// binary `loopruntime-platform-operator` per ADR-0023). Owns the control-
// plane reconciliation: bootstraps Gitea + ArgoCD on a fresh
// cluster, registers per-tenant Git repositories, and manages the
// multi-cell ClusterCredential CRs that DynamicRouter resolves.
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

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/conditions"
	"github.com/getloopscale/loop-runtime/operators/internal/constants"
	"github.com/getloopscale/loop-runtime/operators/internal/gitea"
	"github.com/getloopscale/loop-runtime/operators/internal/httpclient"
	"github.com/getloopscale/loop-runtime/operators/internal/reconcileloop"
	"github.com/getloopscale/loop-runtime/pkg/correlation"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// platformBootstrapRequeueDelay uses the centralized constant.
const platformBootstrapRequeueDelay = constants.RequeuePlatformBootstrapDelay

// platformBootstrapSM is the PlatformBootstrap lifecycle state machine (spec section 5).
// Phase transitions go through this machine -- direct assignment is forbidden.
var platformBootstrapSM = statemachine.NewPlatformBootstrapTypedMachine()

// ArgoCDBootstrapClient abstracts ArgoCD HTTP operations for testability.
// ARCH-OP-001: Mirrors the Gitea BootstrapClient pattern -- controllers depend
// on this interface instead of constructing HTTP requests inline.
type ArgoCDBootstrapClient interface {
	// CheckReachable verifies ArgoCD is reachable (GET /api/version).
	CheckReachable(ctx context.Context) error
	// GenerateAPIKey creates a non-expiring apiKey for the loopruntime-api account.
	GenerateAPIKey(ctx context.Context, adminPassword string) (string, error)
}

// PlatformBootstrapReconciler reconciles a PlatformBootstrap object.
// It configures Gitea, ArgoCD, and seeds the database via the API,
// delegating external calls to typed clients (no inline HTTP).
//
// ADR-0026 D4 / plan T4.1: kubebuilder:rbac markers.
//
// +kubebuilder:rbac:groups=runtime.getloopscale.com,resources=platformbootstraps,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=runtime.getloopscale.com,resources=platformbootstraps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=runtime.getloopscale.com,resources=platformbootstraps/finalizers,verbs=update
// +kubebuilder:rbac:groups="",namespace=loopruntime-system,resources=secrets,verbs=get;list;watch;update
// +kubebuilder:rbac:groups="",namespace=loopruntime-system,resources=secrets,resourceNames=platform-tokens,verbs=delete
// +kubebuilder:rbac:groups="",namespace=argocd,resources=secrets,verbs=get;list;watch;create;update
type PlatformBootstrapReconciler struct {
	client.Client
	HTTPClient       *http.Client
	GiteaClientFunc  func(baseURL string) gitea.BootstrapClient // factory, set at init; nil uses default
	ArgoCDClientFunc func(baseURL string) ArgoCDBootstrapClient // factory, set at init; nil uses default (inline HTTP)
}

// platformBootstrapDetailTypes per ADR-0025 D3.
var platformBootstrapDetailTypes = []string{
	conditions.TypeGiteaReady,
	conditions.TypeArgoCDReady,
	conditions.TypeDatabaseSeeded,
	conditions.TypeBootstrapComplete,
}

// Reconcile handles the reconciliation loop for PlatformBootstrap resources.
func (r *PlatformBootstrapReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
	ctx, span := reconcileloop.StartReconcileSpan(ctx, "bootstrap", req.Name, req.Namespace)
	defer span.End()
	start := time.Now()
	defer func() {
		if retErr != nil {
			reconcileloop.RecordError(span, retErr)
		} else {
			reconcileloop.SetSuccess(span)
		}
		reconcileloop.ReconcileOutcome("bootstrap", start, retErr)
	}()

	var pb loopruntimev1.PlatformBootstrap
	if err := r.Get(ctx, req.NamespacedName, &pb); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// T1.1 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap: propagate
	// correlation-id from CR annotation into reconcile context (no-op when absent).
	ctx = correlation.WithID(ctx, pb.GetAnnotations()["runtime.getloopscale.com/correlation-id"])

	if !pb.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.reconcileDelete(ctx, &pb)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(&pb, constants.FinalizerPlatformBootstrap) {
		controllerutil.AddFinalizer(&pb, constants.FinalizerPlatformBootstrap)
		if err := r.Update(ctx, &pb); err != nil {
			return ctrl.Result{}, err
		}
	}

	// The base snapshot for every status write below.
	//
	// AFTER the finalizer Update above, because that write bumps resourceVersion
	// and a patch derived from an earlier snapshot carries the stale one —
	// turning the patch into an optimistic lock frozen at snapshot time, which is
	// a WIDER conflict window than the refetch this contract removes. Measured as
	// a real 409 in slice-18.
	//
	// The deletion path does not reach here: it returns above, and writes no
	// status at all — so unlike the preview controller it needs no base of its own.
	base := pb.DeepCopy()

	// Terminal phases: do not requeue.
	if platformBootstrapSM.IsTerminal(statemachine.PlatformBootstrapPhase(pb.Status.Phase)) {
		return ctrl.Result{}, nil
	}

	// Empty -> Pending, then Pending -> ConfiguringGitea: one Start event per
	// reconcile, each persisted before the next.
	if pb.Status.Phase == "" || pb.Status.Phase == loopruntimev1.PlatformBootstrapPhasePending {
		return r.startPhase(ctx, &pb, base)
	}

	// Resolve max retries with default.
	maxRetries := pb.Spec.MaxRetries
	if maxRetries <= 0 {
		maxRetries = constants.DefaultMaxRetries
	}

	// Phase dispatch.
	switch pb.Status.Phase {
	case loopruntimev1.PlatformBootstrapPhaseConfiguringGitea:
		return r.reconcileGiteaPhase(ctx, &pb, base, maxRetries)

	case loopruntimev1.PlatformBootstrapPhaseConfiguringArgoCD:
		return r.reconcileArgoCDPhase(ctx, &pb, base, maxRetries)

	case loopruntimev1.PlatformBootstrapPhaseWaitingForAPI:
		return r.reconcileAPIPhase(ctx, &pb, base, maxRetries)

	default:
		return r.transitionToFailed(ctx, &pb, base, fmt.Sprintf("unknown phase %q -- cannot reconcile", pb.Status.Phase))
	}
}

// reconcileDelete removes the platform token Secret this controller manages and
// only then releases the finalizer. A Secret it did not create is left alone.
// Any failure other than NotFound keeps the finalizer, so the delete is retried
// rather than leaving the Gitea admin token and ArgoCD apiKey behind.
func (r *PlatformBootstrapReconciler) reconcileDelete(ctx context.Context, pb *loopruntimev1.PlatformBootstrap) error {
	if !controllerutil.ContainsFinalizer(pb, constants.FinalizerPlatformBootstrap) {
		return nil
	}
	if err := r.deleteManagedTokenSecret(ctx, pb.Namespace); err != nil {
		return err
	}
	controllerutil.RemoveFinalizer(pb, constants.FinalizerPlatformBootstrap)
	return r.Update(ctx, pb)
}

// deleteManagedTokenSecret deletes constants.SecretPlatformTokens when this
// controller manages it.
func (r *PlatformBootstrapReconciler) deleteManagedTokenSecret(ctx context.Context, namespace string) error {
	secret := &corev1.Secret{}
	key := client.ObjectKey{Name: constants.SecretPlatformTokens, Namespace: namespace}
	if err := r.Get(ctx, key, secret); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get %s during cleanup: %w", key, err)
	}
	if secret.Labels[constants.LabelManagedBy] != constants.ManagedByPlatformBootstrapController {
		log.FromContext(ctx).Info("leaving platform token Secret in place: not managed by this controller",
			"secret", key.String())
		return nil
	}
	if err := r.Delete(ctx, secret); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete %s during cleanup: %w", key, err)
	}
	return nil
}

// startPhase applies one Start event (Empty -> Pending, Pending ->
// ConfiguringGitea), persists it and requeues.
func (r *PlatformBootstrapReconciler) startPhase(ctx context.Context, pb, base *loopruntimev1.PlatformBootstrap) (ctrl.Result, error) {
	next, err := platformBootstrapSM.TryTransition(statemachine.PlatformBootstrapPhase(pb.Status.Phase), statemachine.PBPEventStart)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("start bootstrap: %w", err)
	}
	pb.Status.Phase = loopruntimev1.PlatformBootstrapPhase(next)
	pb.Status.RetryCount = 0
	now := metav1.Now()
	pb.Status.LastTransitionTime = &now
	if err := r.patchStatus(ctx, pb, base); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{Requeue: true}, nil
}

// transitionToFailed transitions the CR to Failed phase via the state machine
// and records message on BootstrapComplete=False (Reason=Failed), which the
// readiness summary classifies as Stalled. Uses TryTransition because
// pb.Status.Phase comes from CRD status (external source).
func (r *PlatformBootstrapReconciler) transitionToFailed(ctx context.Context, pb, base *loopruntimev1.PlatformBootstrap, message string) (ctrl.Result, error) {
	next, err := platformBootstrapSM.TryTransition(statemachine.PlatformBootstrapPhase(pb.Status.Phase), statemachine.PBPEventRetryExhaust)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("transition to failed: %w", err)
	}
	pb.Status.Phase = loopruntimev1.PlatformBootstrapPhase(next)
	r.setCondition(pb, conditions.TypeBootstrapComplete, metav1.ConditionFalse, conditions.ReasonFailed, message)
	now := metav1.Now()
	pb.Status.LastTransitionTime = &now
	if err := r.patchStatus(ctx, pb, base); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// transitionToNextPhase transitions the CR to the next phase via the state machine.
// Uses TryTransition because pb.Status.Phase comes from CRD status (external source).
func (r *PlatformBootstrapReconciler) transitionToNextPhase(ctx context.Context, pb, base *loopruntimev1.PlatformBootstrap, event statemachine.PlatformBootstrapPhaseEvent) (ctrl.Result, error) {
	next, err := platformBootstrapSM.TryTransition(statemachine.PlatformBootstrapPhase(pb.Status.Phase), event)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("transition to next phase: %w", err)
	}
	pb.Status.Phase = loopruntimev1.PlatformBootstrapPhase(next)
	pb.Status.RetryCount = 0
	now := metav1.Now()
	pb.Status.LastTransitionTime = &now
	if err := r.patchStatus(ctx, pb, base); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{Requeue: true}, nil
}

// requeueWithRetry increments the retry count and requeues. If max retries exceeded,
// transitions to Failed.
func (r *PlatformBootstrapReconciler) requeueWithRetry(ctx context.Context, pb, base *loopruntimev1.PlatformBootstrap, maxRetries int, message string) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	pb.Status.RetryCount++
	logger.Info("phase retry", "phase", pb.Status.Phase, "retryCount", pb.Status.RetryCount, "maxRetries", maxRetries, "reason", message)

	if pb.Status.RetryCount >= maxRetries {
		return r.transitionToFailed(ctx, pb, base, fmt.Sprintf("max retries (%d) exhausted: %s", maxRetries, message))
	}

	if err := r.patchStatus(ctx, pb, base); err != nil {
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
		For(&loopruntimev1.PlatformBootstrap{}, reconcileloop.ForOption()).
		Named("platformbootstrap").
		Complete(&PlatformBootstrapReconciler{
			Client:     mgr.GetClient(),
			HTTPClient: httpClient,
		})
}

// setConditionAndUpdateStatus is a helper that sets a condition and updates status.
func (r *PlatformBootstrapReconciler) setCondition(pb *loopruntimev1.PlatformBootstrap, condType string, status metav1.ConditionStatus, reason, message string) {
	conditions.SetCondition(&pb.Status.Conditions, condType, status, reason, message, pb.Generation)
}

// patchStatus is the single status writer of this controller. It folds the
// kstatus readiness summary in as the last mutation (ADR-0025 D3) and persists
// with a merge patch off base, so every write carries a Ready condition that
// agrees with the conditions written beside it.
func (r *PlatformBootstrapReconciler) patchStatus(ctx context.Context, pb, base *loopruntimev1.PlatformBootstrap) error {
	conditions.SetReadinessSummaryViaPkg(&pb.Status.Conditions, pb.Generation, platformBootstrapDetailTypes)
	return r.Status().Patch(ctx, pb, client.MergeFrom(base))
}
