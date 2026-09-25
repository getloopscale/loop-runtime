// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package platform

import (
	"context"
	"crypto/sha256"
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

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/conditions"
	"github.com/getloopscale/loopzero/operators/internal/constants"
	"github.com/getloopscale/loopzero/operators/internal/gitea"
	"github.com/getloopscale/loopzero/operators/internal/httpclient"
	"github.com/getloopscale/loopzero/operators/internal/reconcileloop"
	"github.com/getloopscale/loopzero/pkg/correlation"
)

const (
	repoBindingFinalizer = constants.FinalizerGitRepositoryBinding
)

// GitRepositoryBindingReconciler reconciles a GitRepositoryBinding object.
// It manages the lifecycle of ArgoCD repository Secrets based on the binding spec.
//
// ADR-0026 D4 / plan T4.1: kubebuilder:rbac markers.
//
// +kubebuilder:rbac:groups=loopzero.io,resources=gitrepositorybindings,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=loopzero.io,resources=gitrepositorybindings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=loopzero.io,resources=gitrepositorybindings/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
type GitRepositoryBindingReconciler struct {
	client.Client
	HTTPClient *http.Client
}

// gitRepositoryBindingDetailTypes per ADR-0025 D3.
var gitRepositoryBindingDetailTypes = []string{
	conditions.TypeCredentialsResolved,
	conditions.TypeRepositoryRegistered,
	conditions.TypeRepositoryReachable,
	conditions.TypeConsumerHealthy,
}

// Reconcile handles the reconciliation loop for GitRepositoryBinding resources.
func (r *GitRepositoryBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
	ctx, span := reconcileloop.StartReconcileSpan(ctx, "gitrepobinding", req.Name, req.Namespace)
	defer span.End()
	start := time.Now()
	defer func() {
		if retErr != nil {
			reconcileloop.RecordError(span, retErr)
		} else {
			reconcileloop.SetSuccess(span)
		}
		reconcileloop.ReconcileOutcome("gitrepobinding", start, retErr)
	}()

	logger := log.FromContext(ctx)

	var binding loopzerov1.GitRepositoryBinding
	if err := r.Get(ctx, req.NamespacedName, &binding); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// T1.1 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap: propagate
	// correlation-id from CR annotation into reconcile context (no-op when absent).
	ctx = correlation.WithID(ctx, binding.GetAnnotations()["loopzero.io/correlation-id"])

	// Handle deletion with finalizer pattern.
	if !binding.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &binding)
	}

	// Add finalizer if not present. Re-fetches in place to avoid stale resourceVersion.
	if err := reconcileloop.EnsureFinalizer(ctx, r.Client, &binding, repoBindingFinalizer, req.NamespacedName); err != nil {
		return ctrl.Result{}, err
	}

	// The base snapshot for every status write below.
	//
	// AFTER EnsureFinalizer above, which writes metadata and bumps
	// resourceVersion: a patch derived from an earlier snapshot carries the stale
	// version and becomes an optimistic lock frozen at snapshot time — a WIDER
	// conflict window than the refetch this contract removes. Measured as a real
	// 409 in slice-18.
	//
	// And before the first .Status mutation: the helpers below SET conditions on
	// `binding` without persisting them, so a snapshot taken after any of them
	// would diff those conditions against themselves and the patch would silently
	// omit the very findings it exists to record.
	//
	// The deletion path returns above and writes no status, so unlike the preview
	// controller it needs no base of its own. And no helper signature changes:
	// all three writes live in Reconcile, which is where `base` is in scope.
	base := binding.DeepCopy()

	gen := binding.Generation

	// Step 1: Resolve credential from the referenced Secret.
	token, err := r.resolveCredential(ctx, &binding, gen)
	if err != nil {
		if statusErr := r.patchStatus(ctx, &binding, base); statusErr != nil {
			logger.Error(statusErr, "failed to update binding status after credential resolve error")
		}
		return ctrl.Result{RequeueAfter: constants.RequeueRepoBindingOnError}, nil
	}

	// Step 2: Compute credential hash.
	hash := computeCredentialHash(token)

	// Step 3: Ensure ArgoCD Secret exists with correct data.
	if err := r.ensureArgoCDSecret(ctx, &binding, token, hash, gen); err != nil {
		if statusErr := r.patchStatus(ctx, &binding, base); statusErr != nil {
			logger.Error(statusErr, "failed to update binding status after ArgoCD secret error")
		}
		return ctrl.Result{RequeueAfter: constants.RequeueRepoBindingOnError}, nil
	}

	// Step 4: Update credential hash in status.
	binding.Status.CredentialHash = hash

	// Step 5: Check repository reachability (non-blocking -- the binding is functional
	// even if the repo is temporarily unreachable). Result is reflected in the
	// RepositoryReachable condition for observability.
	r.checkReachability(ctx, &binding, token, gen)

	// Step 6: Set ConsumerHealthy condition (ArgoCD Secret exists and is up to date).
	conditions.SetCondition(&binding.Status.Conditions, conditions.TypeConsumerHealthy,
		metav1.ConditionTrue, conditions.ReasonReady,
		"Consumer Secret is up to date",
		gen)

	// Step 7: Update observed generation.
	binding.Status.ObservedGeneration = gen

	if statusErr := r.patchStatus(ctx, &binding, base); statusErr != nil {
		return ctrl.Result{}, statusErr
	}

	logger.Info("reconciled git repository binding",
		"repoURL", binding.Spec.RepoURL,
		"credentialHash", hash)

	return ctrl.Result{RequeueAfter: constants.RequeueRepoBindingInterval}, nil
}

// resolveCredential reads the token from the referenced Secret.
// Sets CredentialsResolved condition on the binding status.
func (r *GitRepositoryBindingReconciler) resolveCredential(ctx context.Context, binding *loopzerov1.GitRepositoryBinding, gen int64) (string, error) {
	ref := binding.Spec.CredentialSecretRef
	secretKey := client.ObjectKey{
		Name:      ref.Name,
		Namespace: ref.Namespace,
	}

	var secret corev1.Secret
	if err := r.Get(ctx, secretKey, &secret); err != nil {
		if errors.IsNotFound(err) {
			conditions.SetCondition(&binding.Status.Conditions, conditions.TypeCredentialsResolved,
				metav1.ConditionFalse, conditions.ReasonNotFound,
				fmt.Sprintf("Secret %s/%s not found", ref.Namespace, ref.Name),
				gen)
			return "", err
		}
		conditions.SetCondition(&binding.Status.Conditions, conditions.TypeCredentialsResolved,
			metav1.ConditionFalse, conditions.ReasonFailed,
			fmt.Sprintf("Failed to get Secret %s/%s: %v", ref.Namespace, ref.Name, err),
			gen)
		return "", err
	}

	tokenBytes, ok := secret.Data[ref.Key]
	if !ok {
		return failCredentialsResolved(binding, gen,
			fmt.Sprintf("Secret %s/%s does not contain key %q", ref.Namespace, ref.Name, ref.Key),
			fmt.Errorf("secret %s/%s missing key %q", ref.Namespace, ref.Name, ref.Key))
	}

	token := string(tokenBytes)
	if token == "" {
		return failCredentialsResolved(binding, gen,
			fmt.Sprintf("Secret %s/%s key %q is empty", ref.Namespace, ref.Name, ref.Key),
			fmt.Errorf("secret %s/%s key %q is empty", ref.Namespace, ref.Name, ref.Key))
	}

	conditions.SetCondition(&binding.Status.Conditions, conditions.TypeCredentialsResolved,
		metav1.ConditionTrue, conditions.ReasonReady,
		fmt.Sprintf("Credential resolved from Secret %s/%s", ref.Namespace, ref.Name),
		gen)

	return token, nil
}

// failCredentialsResolved is the canonical "secret-data malformed" failure
// handler for resolveCredential. Sets the CredentialsResolved condition
// False with ReasonFailed + msg + gen, and returns the (zero-token, err)
// tuple expected by the caller. Phase 3 helper: collapses 2 inline 7-line
// copies (missing key + empty value) in gitrepositorybinding_controller.go.
func failCredentialsResolved(binding *loopzerov1.GitRepositoryBinding, gen int64, msg string, err error) (string, error) {
	conditions.SetCondition(&binding.Status.Conditions, conditions.TypeCredentialsResolved,
		metav1.ConditionFalse, conditions.ReasonFailed, msg, gen)
	return "", err
}

// computeCredentialHash returns the full sha256 hex hash of the token.
func computeCredentialHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", h)
}

// ensureArgoCDSecret creates or updates the ArgoCD repository Secret in the consumer namespace.
func (r *GitRepositoryBindingReconciler) ensureArgoCDSecret(ctx context.Context, binding *loopzerov1.GitRepositoryBinding, token, hash string, gen int64) error {
	secretName := constants.ArgoCDRepoSecretName(binding.Name)
	consumerNs := binding.Spec.Consumer.Namespace

	argoSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: consumerNs,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, argoSecret, func() error {
		if argoSecret.Labels == nil {
			argoSecret.Labels = make(map[string]string)
		}
		argoSecret.Labels[constants.ArgoCDSecretTypeLabel] = constants.ArgoCDSecretTypeRepository
		argoSecret.Labels[constants.LabelManagedBy] = constants.ManagedByGitRepoBindingController

		if argoSecret.Annotations == nil {
			argoSecret.Annotations = make(map[string]string)
		}
		argoSecret.Annotations[constants.AnnotationCredentialHash] = hash

		argoSecret.StringData = map[string]string{
			"type":     "git",
			"url":      binding.Spec.RepoURL,
			"username": constants.DefaultArgoCDRepoUsername,
			"password": token,
		}
		return nil
	})
	if err != nil {
		conditions.SetCondition(&binding.Status.Conditions, conditions.TypeRepositoryRegistered,
			metav1.ConditionFalse, conditions.ReasonFailed,
			fmt.Sprintf("Failed to create/update ArgoCD Secret %s/%s: %v", consumerNs, secretName, err),
			gen)
		return err
	}

	conditions.SetCondition(&binding.Status.Conditions, conditions.TypeRepositoryRegistered,
		metav1.ConditionTrue, conditions.ReasonReady,
		fmt.Sprintf("ArgoCD repository Secret %s/%s is up to date", consumerNs, secretName),
		gen)

	return nil
}

// checkReachability performs an HTTP GET to the Gitea API to verify the repository is reachable.
func (r *GitRepositoryBindingReconciler) checkReachability(ctx context.Context, binding *loopzerov1.GitRepositoryBinding, token string, gen int64) {
	logger := log.FromContext(ctx)

	apiURL, err := gitea.RepoAPIURL(binding.Spec.RepoURL)
	if err != nil {
		logger.Info("could not parse repo URL for reachability check", "repoURL", binding.Spec.RepoURL, "error", err)
		conditions.SetCondition(&binding.Status.Conditions, conditions.TypeRepositoryReachable,
			metav1.ConditionFalse, conditions.ReasonFailed,
			fmt.Sprintf("Cannot parse repo URL: %v", err),
			gen)
		return
	}

	httpClient := r.HTTPClient
	if httpClient == nil {
		httpClient = httpclient.NewWithTimeout(constants.ReachabilityTimeout)
	}

	reqCtx, cancel := context.WithTimeout(ctx, constants.ReachabilityTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, apiURL, nil)
	if err != nil {
		conditions.SetCondition(&binding.Status.Conditions, conditions.TypeRepositoryReachable,
			metav1.ConditionFalse, conditions.ReasonFailed,
			fmt.Sprintf("Failed to create HTTP request: %v", err),
			gen)
		return
	}
	req.Header.Set("Authorization", "token "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		logger.Info("repository reachability check failed", "repoURL", binding.Spec.RepoURL, "error", err)
		conditions.SetCondition(&binding.Status.Conditions, conditions.TypeRepositoryReachable,
			metav1.ConditionFalse, conditions.ReasonUnreachable,
			fmt.Sprintf("Repository unreachable: %v", err),
			gen)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		conditions.SetCondition(&binding.Status.Conditions, conditions.TypeRepositoryReachable,
			metav1.ConditionFalse, conditions.ReasonFailed,
			fmt.Sprintf("Repository returned HTTP %d", resp.StatusCode),
			gen)
		return
	}

	conditions.SetCondition(&binding.Status.Conditions, conditions.TypeRepositoryReachable,
		metav1.ConditionTrue, conditions.ReasonReady,
		"Repository is reachable",
		gen)
}

// reconcileDelete handles cleanup when a GitRepositoryBinding is being deleted.
func (r *GitRepositoryBindingReconciler) reconcileDelete(ctx context.Context, binding *loopzerov1.GitRepositoryBinding) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(binding, repoBindingFinalizer) {
		return ctrl.Result{}, nil
	}

	logger.Info("cleaning up git repository binding", "repoURL", binding.Spec.RepoURL)

	// Delete the ArgoCD repository Secret.
	secretName := constants.ArgoCDRepoSecretName(binding.Name)
	consumerNs := binding.Spec.Consumer.Namespace

	argoSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: consumerNs,
		},
	}
	if err := r.Delete(ctx, argoSecret); err != nil && !errors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("failed to delete ArgoCD repository Secret %s/%s: %w",
			consumerNs, secretName, err)
	}
	logger.Info("deleted ArgoCD repository secret", "secret", secretName, "namespace", consumerNs)

	// Remove finalizer.
	controllerutil.RemoveFinalizer(binding, repoBindingFinalizer)
	if err := r.Update(ctx, binding); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("git repository binding deletion complete", "repoURL", binding.Spec.RepoURL)
	return ctrl.Result{}, nil
}

// SetupGitRepositoryBindingController registers the GitRepositoryBindingController with the manager.
func SetupGitRepositoryBindingController(mgr ctrl.Manager, httpClient *http.Client) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopzerov1.GitRepositoryBinding{}, reconcileloop.ForOption()).
		Named("gitrepositorybinding").
		Complete(&GitRepositoryBindingReconciler{
			Client:     mgr.GetClient(),
			HTTPClient: httpClient,
		})
}

// patchStatus is the single status writer of this controller. It folds the
// kstatus readiness summary in as the last mutation (ADR-0025 D3) and persists
// with a merge patch off base, so every write carries a Ready condition that
// agrees with the conditions written beside it.
func (r *GitRepositoryBindingReconciler) patchStatus(ctx context.Context, binding, base *loopzerov1.GitRepositoryBinding) error {
	conditions.SetReadinessSummaryViaPkg(&binding.Status.Conditions, binding.Generation, gitRepositoryBindingDetailTypes)
	return r.Status().Patch(ctx, binding, client.MergeFrom(base))
}
