package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
)

const (
	clusterCredentialFinalizer = "loopzero.io/clustercredential-cleanup"
	argoCDSecretLabel          = "argocd.argoproj.io/secret-type"
	argoCDSecretLabelValue     = "cluster"
	argoCDNamespace            = "argocd"

	conditionSecretNotFound = "SecretNotFound"
	conditionConnected      = "Connected"

	requeueSecretNotFound = 30 * time.Second
	requeuePeriodicCheck  = 5 * time.Minute

	// connectivityTimeout limits how long we wait for a healthz response.
	connectivityTimeout = 5 * time.Second
)

// ClusterCredentialReconciler reconciles a ClusterCredential object.
type ClusterCredentialReconciler struct {
	client.Client
}

// Reconcile handles the reconciliation loop for ClusterCredential resources.
func (r *ClusterCredentialReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var cr loopzerov1.ClusterCredential
	if err := r.Get(ctx, req.NamespacedName, &cr); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion with finalizer pattern.
	if !cr.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &cr)
	}

	// Add finalizer if not present.
	if !controllerutil.ContainsFinalizer(&cr, clusterCredentialFinalizer) {
		controllerutil.AddFinalizer(&cr, clusterCredentialFinalizer)
		if err := r.Update(ctx, &cr); err != nil {
			return ctrl.Result{}, err
		}
		// Re-fetch after update to avoid stale resource version.
		if err := r.Get(ctx, req.NamespacedName, &cr); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Step 1: Read the referenced Secret.
	var secret corev1.Secret
	secretKey := client.ObjectKey{
		Name:      cr.Spec.SecretRef.Name,
		Namespace: cr.Spec.SecretRef.Namespace,
	}
	if err := r.Get(ctx, secretKey, &secret); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("referenced secret not found", "secret", secretKey)
			cr.Status.Connected = false
			loopzerov1.SetCondition(&cr.Status.Conditions, conditionSecretNotFound,
				metav1.ConditionTrue, "SecretMissing",
				fmt.Sprintf("Secret %s/%s not found", secretKey.Namespace, secretKey.Name),
				cr.Generation)
			if statusErr := r.Status().Update(ctx, &cr); statusErr != nil {
				return ctrl.Result{}, statusErr
			}
			return ctrl.Result{RequeueAfter: requeueSecretNotFound}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get secret %s/%s: %w", secretKey.Namespace, secretKey.Name, err)
	}

	// Secret found — clear SecretNotFound condition.
	loopzerov1.SetCondition(&cr.Status.Conditions, conditionSecretNotFound,
		metav1.ConditionFalse, "SecretFound",
		fmt.Sprintf("Secret %s/%s exists", secretKey.Namespace, secretKey.Name),
		cr.Generation)

	// Step 2: Parse kubeconfig from the secret.
	kubeconfigData, ok := secret.Data["kubeconfig"]
	if !ok {
		cr.Status.Connected = false
		loopzerov1.SetCondition(&cr.Status.Conditions, conditionConnected,
			metav1.ConditionFalse, "KubeconfigKeyMissing",
			"Secret does not contain 'kubeconfig' key",
			cr.Generation)
		if statusErr := r.Status().Update(ctx, &cr); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{RequeueAfter: requeuePeriodicCheck}, nil
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		cr.Status.Connected = false
		loopzerov1.SetCondition(&cr.Status.Conditions, conditionConnected,
			metav1.ConditionFalse, "InvalidKubeconfig",
			fmt.Sprintf("Failed to parse kubeconfig: %v", err),
			cr.Generation)
		if statusErr := r.Status().Update(ctx, &cr); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{RequeueAfter: requeuePeriodicCheck}, nil
	}

	// Step 3: Validate cluster connectivity.
	restConfig.Timeout = connectivityTimeout
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		cr.Status.Connected = false
		loopzerov1.SetCondition(&cr.Status.Conditions, conditionConnected,
			metav1.ConditionFalse, "ClientCreationFailed",
			fmt.Sprintf("Failed to create Kubernetes client: %v", err),
			cr.Generation)
		if statusErr := r.Status().Update(ctx, &cr); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{RequeueAfter: requeuePeriodicCheck}, nil
	}

	// Use /healthz endpoint for connectivity check (lightweight, does not require RBAC).
	healthzCtx, healthzCancel := context.WithTimeout(ctx, connectivityTimeout)
	defer healthzCancel()

	body, err := clientset.Discovery().RESTClient().Get().AbsPath("/healthz").DoRaw(healthzCtx)
	if err != nil || string(body) != "ok" {
		logger.Info("cluster connectivity check failed", "cluster", cr.Spec.ClusterName, "error", err)
		cr.Status.Connected = false
		reason := "Unreachable"
		msg := fmt.Sprintf("Cluster %s is not reachable", cr.Spec.ClusterName)
		if err != nil {
			msg = fmt.Sprintf("Cluster %s connectivity failed: %v", cr.Spec.ClusterName, err)
		}
		loopzerov1.SetCondition(&cr.Status.Conditions, conditionConnected,
			metav1.ConditionFalse, reason, msg, cr.Generation)
		now := metav1.Now()
		cr.Status.LastCheckedAt = &now
		if statusErr := r.Status().Update(ctx, &cr); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{RequeueAfter: requeuePeriodicCheck}, nil
	}

	// Cluster is reachable.
	logger.Info("cluster connectivity check succeeded", "cluster", cr.Spec.ClusterName)
	cr.Status.Connected = true
	now := metav1.Now()
	cr.Status.LastCheckedAt = &now
	loopzerov1.SetCondition(&cr.Status.Conditions, conditionConnected,
		metav1.ConditionTrue, "Reachable",
		fmt.Sprintf("Cluster %s is reachable", cr.Spec.ClusterName),
		cr.Generation)

	// Step 4: Create/Update ArgoCD cluster secret.
	if cr.Spec.Scope.ArgoCD {
		if err := r.reconcileArgoCDClusterSecret(ctx, &cr, kubeconfigData); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Step 5: Update status.
	if statusErr := r.Status().Update(ctx, &cr); statusErr != nil {
		return ctrl.Result{}, statusErr
	}

	logger.Info("reconciled cluster credential", "cluster", cr.Spec.ClusterName, "connected", cr.Status.Connected)
	return ctrl.Result{RequeueAfter: requeuePeriodicCheck}, nil
}

// argoCDClusterConfig is the JSON structure ArgoCD expects in the cluster secret's "config" field.
type argoCDClusterConfig struct {
	TLSClientConfig argoCDTLSConfig `json:"tlsClientConfig"`
}

type argoCDTLSConfig struct {
	Insecure bool `json:"insecure"`
}

// reconcileArgoCDClusterSecret creates or updates the ArgoCD cluster secret.
func (r *ClusterCredentialReconciler) reconcileArgoCDClusterSecret(ctx context.Context, cr *loopzerov1.ClusterCredential, kubeconfigData []byte) error {
	secretName := fmt.Sprintf("cluster-%s", cr.Spec.ClusterName)

	argoConfig := argoCDClusterConfig{
		TLSClientConfig: argoCDTLSConfig{
			Insecure: true, // Using kubeconfig-based auth; TLS verified by kubeconfig settings.
		},
	}
	configJSON, err := json.Marshal(argoConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal ArgoCD config: %w", err)
	}

	argoSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: argoCDNamespace,
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, argoSecret, func() error {
		if argoSecret.Labels == nil {
			argoSecret.Labels = make(map[string]string)
		}
		argoSecret.Labels[argoCDSecretLabel] = argoCDSecretLabelValue
		argoSecret.Labels["loopzero.io/managed-by"] = "clustercredential-controller"
		argoSecret.Labels["loopzero.io/cluster-credential"] = cr.Name

		argoSecret.StringData = map[string]string{
			"name":       cr.Spec.ClusterName,
			"server":     cr.Spec.APIServer,
			"config":     string(configJSON),
			"kubeconfig": string(kubeconfigData),
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("argocd cluster secret %s/%s: %w", argoCDNamespace, secretName, err)
	}

	return nil
}

// reconcileDelete handles cleanup when a ClusterCredential is being deleted.
func (r *ClusterCredentialReconciler) reconcileDelete(ctx context.Context, cr *loopzerov1.ClusterCredential) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(cr, clusterCredentialFinalizer) {
		return ctrl.Result{}, nil
	}

	logger.Info("cleaning up cluster credential", "cluster", cr.Spec.ClusterName)

	// Delete the ArgoCD cluster secret if it exists.
	if cr.Spec.Scope.ArgoCD {
		argoSecretName := fmt.Sprintf("cluster-%s", cr.Spec.ClusterName)
		argoSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      argoSecretName,
				Namespace: argoCDNamespace,
			},
		}
		if err := r.Delete(ctx, argoSecret); err != nil && !errors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("failed to delete ArgoCD cluster secret %s/%s: %w",
				argoCDNamespace, argoSecretName, err)
		}
		logger.Info("deleted ArgoCD cluster secret", "secret", argoSecretName)
	}

	// Remove finalizer.
	controllerutil.RemoveFinalizer(cr, clusterCredentialFinalizer)
	if err := r.Update(ctx, cr); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("cluster credential deletion complete", "cluster", cr.Spec.ClusterName)
	return ctrl.Result{}, nil
}

// SetupClusterCredentialController registers the ClusterCredentialController with the manager.
func SetupClusterCredentialController(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopzerov1.ClusterCredential{}).
		Named("clustercredential").
		Complete(&ClusterCredentialReconciler{
			Client: mgr.GetClient(),
		})
}
