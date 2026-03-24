package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"strings"
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
)

const (
	repoBindingFinalizer       = "platform.loopzero.io/repo-binding-cleanup"
	repoBindingRequeueInterval = 5 * time.Minute
	repoBindingRequeueOnError  = 30 * time.Second
	reachabilityTimeout        = 10 * time.Second
)

// GitRepositoryBindingReconciler reconciles a GitRepositoryBinding object.
// It manages the lifecycle of ArgoCD repository Secrets based on the binding spec.
type GitRepositoryBindingReconciler struct {
	client.Client
	HTTPClient *http.Client
}

// Reconcile handles the reconciliation loop for GitRepositoryBinding resources.
func (r *GitRepositoryBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var binding loopzerov1.GitRepositoryBinding
	if err := r.Get(ctx, req.NamespacedName, &binding); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion with finalizer pattern.
	if !binding.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &binding)
	}

	// Add finalizer if not present.
	if !controllerutil.ContainsFinalizer(&binding, repoBindingFinalizer) {
		controllerutil.AddFinalizer(&binding, repoBindingFinalizer)
		if err := r.Update(ctx, &binding); err != nil {
			return ctrl.Result{}, err
		}
		// Re-fetch after update to avoid stale resource version.
		if err := r.Get(ctx, req.NamespacedName, &binding); err != nil {
			return ctrl.Result{}, err
		}
	}

	gen := binding.Generation

	// Step 1: Resolve credential from the referenced Secret.
	token, err := r.resolveCredential(ctx, &binding, gen)
	if err != nil {
		if statusErr := r.Status().Update(ctx, &binding); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{RequeueAfter: repoBindingRequeueOnError}, nil
	}

	// Step 2: Compute credential hash.
	hash := computeCredentialHash(token)

	// Step 3: Ensure ArgoCD Secret exists with correct data.
	if err := r.ensureArgoCDSecret(ctx, &binding, token, hash, gen); err != nil {
		if statusErr := r.Status().Update(ctx, &binding); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{RequeueAfter: repoBindingRequeueOnError}, nil
	}

	// Step 4: Update credential hash in status.
	binding.Status.CredentialHash = hash

	// Step 5: Check repository reachability.
	r.checkReachability(ctx, &binding, token, gen)

	// Step 6: Set ConsumerHealthy condition (ArgoCD Secret exists and is up to date).
	conditions.SetCondition(&binding.Status.Conditions, conditions.TypeConsumerHealthy,
		metav1.ConditionTrue, conditions.ReasonReady,
		"Consumer Secret is up to date",
		gen)

	// Step 7: Update observed generation.
	binding.Status.ObservedGeneration = gen

	if statusErr := r.Status().Update(ctx, &binding); statusErr != nil {
		return ctrl.Result{}, statusErr
	}

	logger.Info("reconciled git repository binding",
		"repoURL", binding.Spec.RepoURL,
		"credentialHash", hash)

	return ctrl.Result{RequeueAfter: repoBindingRequeueInterval}, nil
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
		conditions.SetCondition(&binding.Status.Conditions, conditions.TypeCredentialsResolved,
			metav1.ConditionFalse, conditions.ReasonFailed,
			fmt.Sprintf("Secret %s/%s does not contain key %q", ref.Namespace, ref.Name, ref.Key),
			gen)
		return "", fmt.Errorf("secret %s/%s missing key %q", ref.Namespace, ref.Name, ref.Key)
	}

	token := string(tokenBytes)
	if token == "" {
		conditions.SetCondition(&binding.Status.Conditions, conditions.TypeCredentialsResolved,
			metav1.ConditionFalse, conditions.ReasonFailed,
			fmt.Sprintf("Secret %s/%s key %q is empty", ref.Namespace, ref.Name, ref.Key),
			gen)
		return "", fmt.Errorf("secret %s/%s key %q is empty", ref.Namespace, ref.Name, ref.Key)
	}

	conditions.SetCondition(&binding.Status.Conditions, conditions.TypeCredentialsResolved,
		metav1.ConditionTrue, conditions.ReasonReady,
		fmt.Sprintf("Credential resolved from Secret %s/%s", ref.Namespace, ref.Name),
		gen)

	return token, nil
}

// computeCredentialHash returns the first 12 characters of the sha256 hex hash of the token.
func computeCredentialHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", h)[:12]
}

// argocdSecretName returns the deterministic name for the ArgoCD repository Secret.
// TODO: Use pkg/naming function when available. For now, inline construction.
func argocdSecretName(bindingName string) string {
	return fmt.Sprintf("repo-%s-gitops", bindingName)
}

// ensureArgoCDSecret creates or updates the ArgoCD repository Secret in the consumer namespace.
func (r *GitRepositoryBindingReconciler) ensureArgoCDSecret(ctx context.Context, binding *loopzerov1.GitRepositoryBinding, token, hash string, gen int64) error {
	secretName := argocdSecretName(binding.Name)
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
		argoSecret.Labels["argocd.argoproj.io/secret-type"] = "repository"
		argoSecret.Labels["loopzero.io/managed-by"] = "gitrepositorybinding-controller"

		if argoSecret.Annotations == nil {
			argoSecret.Annotations = make(map[string]string)
		}
		argoSecret.Annotations["platform.loopzero.io/credential-hash"] = hash

		argoSecret.StringData = map[string]string{
			"type":     "git",
			"url":      binding.Spec.RepoURL,
			"username": "loopzero-platform",
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

	apiURL, err := repoAPIURL(binding.Spec.RepoURL)
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
		httpClient = &http.Client{Timeout: reachabilityTimeout}
	}

	reqCtx, cancel := context.WithTimeout(ctx, reachabilityTimeout)
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
	defer resp.Body.Close()

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

// repoAPIURL converts a Git repo URL like http://host/owner/repo.git to
// the Gitea API URL: http://host/api/v1/repos/owner/repo
func repoAPIURL(repoURL string) (string, error) {
	parsed, err := url.Parse(repoURL)
	if err != nil {
		return "", fmt.Errorf("invalid repo URL %q: %w", repoURL, err)
	}

	path := strings.TrimSuffix(parsed.Path, ".git")
	path = strings.TrimPrefix(path, "/")

	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("repo URL %q: expected path format owner/repo", repoURL)
	}

	apiURL := fmt.Sprintf("%s://%s/api/v1/repos/%s/%s", parsed.Scheme, parsed.Host, parts[0], parts[1])
	return apiURL, nil
}

// reconcileDelete handles cleanup when a GitRepositoryBinding is being deleted.
func (r *GitRepositoryBindingReconciler) reconcileDelete(ctx context.Context, binding *loopzerov1.GitRepositoryBinding) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(binding, repoBindingFinalizer) {
		return ctrl.Result{}, nil
	}

	logger.Info("cleaning up git repository binding", "repoURL", binding.Spec.RepoURL)

	// Delete the ArgoCD repository Secret.
	secretName := argocdSecretName(binding.Name)
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
		For(&loopzerov1.GitRepositoryBinding{}).
		Named("gitrepositorybinding").
		Complete(&GitRepositoryBindingReconciler{
			Client:     mgr.GetClient(),
			HTTPClient: httpClient,
		})
}
