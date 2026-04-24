// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/conditions"
	"github.com/loopzero/loopzero/operators/internal/constants"
)

const (
	clusterCredentialFinalizer = constants.FinalizerClusterCredential
)

// ClusterCredentialReconciler reconciles a ClusterCredential object.
type ClusterCredentialReconciler struct {
	client.Client
	ArgoCDNamespace string
}

// Reconcile handles the reconciliation loop for ClusterCredential resources.
func (r *ClusterCredentialReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
	ctx, span := startReconcileSpan(ctx, "clustercredential", req.Name, req.Namespace)
	defer span.End()
	start := time.Now()
	defer func() {
		if retErr != nil {
			recordError(span, retErr)
		} else {
			setSuccess(span)
		}
		reconcileOutcome("clustercredential", start, retErr)
	}()

	logger := log.FromContext(ctx)

	var cr loopzerov1.ClusterCredential
	if err := r.Get(ctx, req.NamespacedName, &cr); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !cr.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &cr)
	}

	if !controllerutil.ContainsFinalizer(&cr, clusterCredentialFinalizer) {
		controllerutil.AddFinalizer(&cr, clusterCredentialFinalizer)
		if err := r.Update(ctx, &cr); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Get(ctx, req.NamespacedName, &cr); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Step 1: Resolve kubeconfig from the referenced Secret.
	kubeconfigData, result, err := r.resolveKubeconfig(ctx, &cr)
	if err != nil || kubeconfigData == nil {
		return result, err
	}

	// Step 2: Validate cluster connectivity.
	connected, result, err := r.checkConnectivity(ctx, &cr, kubeconfigData)
	if err != nil || !connected {
		return result, err
	}

	// Step 3: Create/Update ArgoCD cluster secret.
	if cr.Spec.Scope.ArgoCD {
		if err := r.reconcileArgoCDClusterSecret(ctx, &cr, kubeconfigData); err != nil {
			return ctrl.Result{}, err
		}
	}

	// MEET-053 D-03: Record observed generation for change detection.
	cr.Status.ObservedGeneration = cr.Generation

	if statusErr := r.Status().Update(ctx, &cr); statusErr != nil {
		return ctrl.Result{}, statusErr
	}

	logger.Info("reconciled cluster credential", "cluster", cr.Spec.ClusterName, "connected", cr.Status.Connected)
	return ctrl.Result{RequeueAfter: constants.RequeuePeriodicCheck}, nil
}

// resolveKubeconfig reads and parses the kubeconfig from the referenced Secret.
// Returns (nil, result, nil) if the kubeconfig is not yet available (status updated, requeue set).
func (r *ClusterCredentialReconciler) resolveKubeconfig(ctx context.Context, cr *loopzerov1.ClusterCredential) ([]byte, ctrl.Result, error) {
	logger := log.FromContext(ctx)

	secretKey := client.ObjectKey{
		Name:      cr.Spec.SecretRef.Name,
		Namespace: cr.Spec.SecretRef.Namespace,
	}
	var secret corev1.Secret
	if err := r.Get(ctx, secretKey, &secret); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("referenced secret not found", "secret", secretKey)
			cr.Status.Connected = false
			conditions.SetCondition(&cr.Status.Conditions, conditions.TypeSecretNotFound,
				metav1.ConditionTrue, "SecretMissing",
				fmt.Sprintf("Secret %s/%s not found", secretKey.Namespace, secretKey.Name),
				cr.Generation)
			if statusErr := r.Status().Update(ctx, cr); statusErr != nil {
				return nil, ctrl.Result{}, statusErr
			}
			return nil, ctrl.Result{RequeueAfter: constants.RequeueSecretNotFound}, nil
		}
		return nil, ctrl.Result{}, fmt.Errorf("failed to get secret %s/%s: %w", secretKey.Namespace, secretKey.Name, err)
	}

	conditions.SetCondition(&cr.Status.Conditions, conditions.TypeSecretNotFound,
		metav1.ConditionFalse, "SecretFound",
		fmt.Sprintf("Secret %s/%s exists", secretKey.Namespace, secretKey.Name),
		cr.Generation)

	kubeconfigData, ok := secret.Data[constants.KeyKubeconfig]
	if !ok {
		cr.Status.Connected = false
		conditions.SetCondition(&cr.Status.Conditions, conditions.TypeConnected,
			metav1.ConditionFalse, "KubeconfigKeyMissing",
			"Secret does not contain 'kubeconfig' key",
			cr.Generation)
		if statusErr := r.Status().Update(ctx, cr); statusErr != nil {
			return nil, ctrl.Result{}, statusErr
		}
		return nil, ctrl.Result{RequeueAfter: constants.RequeuePeriodicCheck}, nil
	}

	return kubeconfigData, ctrl.Result{}, nil
}

// checkConnectivity validates that the cluster is reachable via /healthz.
// Returns (false, result, nil) if unreachable (status updated, requeue set).
func (r *ClusterCredentialReconciler) checkConnectivity(ctx context.Context, cr *loopzerov1.ClusterCredential, kubeconfigData []byte) (bool, ctrl.Result, error) {
	ctx, span := startChildSpan(ctx, "clustercredential.connectivity.check")
	defer span.End()
	logger := log.FromContext(ctx)

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		cr.Status.Connected = false
		conditions.SetCondition(&cr.Status.Conditions, conditions.TypeConnected,
			metav1.ConditionFalse, "InvalidKubeconfig",
			fmt.Sprintf("Failed to parse kubeconfig: %v", err),
			cr.Generation)
		if statusErr := r.Status().Update(ctx, cr); statusErr != nil {
			return false, ctrl.Result{}, statusErr
		}
		return false, ctrl.Result{RequeueAfter: constants.RequeuePeriodicCheck}, nil
	}

	restConfig.Timeout = constants.ConnectivityTimeout
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		cr.Status.Connected = false
		conditions.SetCondition(&cr.Status.Conditions, conditions.TypeConnected,
			metav1.ConditionFalse, "ClientCreationFailed",
			fmt.Sprintf("Failed to create Kubernetes client: %v", err),
			cr.Generation)
		if statusErr := r.Status().Update(ctx, cr); statusErr != nil {
			return false, ctrl.Result{}, statusErr
		}
		return false, ctrl.Result{RequeueAfter: constants.RequeuePeriodicCheck}, nil
	}

	healthzCtx, healthzCancel := context.WithTimeout(ctx, constants.ConnectivityTimeout)
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
		conditions.SetCondition(&cr.Status.Conditions, conditions.TypeConnected,
			metav1.ConditionFalse, reason, msg, cr.Generation)
		now := metav1.Now()
		cr.Status.LastCheckedAt = &now
		if statusErr := r.Status().Update(ctx, cr); statusErr != nil {
			return false, ctrl.Result{}, statusErr
		}
		return false, ctrl.Result{RequeueAfter: constants.RequeuePeriodicCheck}, nil
	}

	logger.Info("cluster connectivity check succeeded", "cluster", cr.Spec.ClusterName)
	cr.Status.Connected = true
	now := metav1.Now()
	cr.Status.LastCheckedAt = &now
	conditions.SetCondition(&cr.Status.Conditions, conditions.TypeConnected,
		metav1.ConditionTrue, "Reachable",
		fmt.Sprintf("Cluster %s is reachable", cr.Spec.ClusterName),
		cr.Generation)

	return true, ctrl.Result{}, nil
}

// argoCDClusterConfig is the JSON structure ArgoCD expects in the cluster secret's "config" field.
// Contains bearer token for auth and TLS config. The full kubeconfig is NOT stored —
// only the minimal credentials ArgoCD needs.
type argoCDClusterConfig struct {
	BearerToken     string          `json:"bearerToken,omitempty"`
	TLSClientConfig argoCDTLSConfig `json:"tlsClientConfig"`
}

type argoCDTLSConfig struct {
	Insecure bool   `json:"insecure"`
	CAData   string `json:"caData,omitempty"`
}

// reconcileArgoCDClusterSecret creates or updates the ArgoCD cluster secret.
// Stores only the bearer token + CA certificate in the "config" field (ArgoCD's
// preferred auth method). The raw kubeconfig is NOT stored to minimize credential
// exposure — a compromised ArgoCD namespace only leaks a scoped bearer token.
func (r *ClusterCredentialReconciler) reconcileArgoCDClusterSecret(ctx context.Context, cr *loopzerov1.ClusterCredential, kubeconfigData []byte) error {
	secretName := constants.ArgoCDClusterSecretName(cr.Spec.ClusterName)

	// MEET-053 A-02: Extract credentials via clientcmd (replaces naive string parser).
	// Properly handles bearer tokens, client certs, and CA data from any valid
	// kubeconfig format (including exec-based auth used by DOKS default kubeconfigs).
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		return fmt.Errorf("parse kubeconfig for ArgoCD credentials: %w", err)
	}

	token := restConfig.BearerToken
	caData := ""
	if len(restConfig.CAData) > 0 {
		caData = string(restConfig.CAData)
	}

	// Fail-fast if no authentication mechanism found.
	if token == "" && len(restConfig.CertData) == 0 {
		return fmt.Errorf("kubeconfig has no bearer token or client certificate — exec-based auth not supported for ArgoCD cluster secrets")
	}

	argoConfig := argoCDClusterConfig{
		BearerToken: token,
		TLSClientConfig: argoCDTLSConfig{
			Insecure: cr.Spec.TLS.Insecure,
			CAData:   caData,
		},
	}
	configJSON, err := json.Marshal(argoConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal ArgoCD config: %w", err)
	}

	argoSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: r.ArgoCDNamespace,
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, argoSecret, func() error {
		if argoSecret.Labels == nil {
			argoSecret.Labels = make(map[string]string)
		}
		argoSecret.Labels[constants.ArgoCDSecretTypeLabel] = constants.ArgoCDSecretTypeCluster
		argoSecret.Labels[constants.LabelManagedBy] = constants.ManagedByClusterCredentialController
		argoSecret.Labels[constants.LabelClusterCredential] = cr.Name

		argoSecret.StringData = map[string]string{
			"name":   cr.Spec.ClusterName,
			"server": cr.Spec.APIServer,
			"config": string(configJSON),
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("argocd cluster secret %s/%s: %w", r.ArgoCDNamespace, secretName, err)
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

	if cr.Spec.Scope.ArgoCD {
		argoSecretName := constants.ArgoCDClusterSecretName(cr.Spec.ClusterName)
		argoSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      argoSecretName,
				Namespace: r.ArgoCDNamespace,
			},
		}
		if err := r.Delete(ctx, argoSecret); err != nil && !errors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("failed to delete ArgoCD cluster secret %s/%s: %w",
				r.ArgoCDNamespace, argoSecretName, err)
		}
		logger.Info("deleted ArgoCD cluster secret", "secret", argoSecretName)
	}

	controllerutil.RemoveFinalizer(cr, clusterCredentialFinalizer)
	if err := r.Update(ctx, cr); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("cluster credential deletion complete", "cluster", cr.Spec.ClusterName)
	return ctrl.Result{}, nil
}

// SetupClusterCredentialController registers the ClusterCredentialController with the manager.
// MEET-053 D-02: Watches Secrets so kubeconfig rotation triggers immediate reconciliation
// instead of waiting for the 5-minute periodic requeue.
func SetupClusterCredentialController(mgr ctrl.Manager, argoCDNamespace string) error {
	if argoCDNamespace == "" {
		argoCDNamespace = constants.DefaultArgoCDNamespace
	}
	reconciler := &ClusterCredentialReconciler{
		Client:          mgr.GetClient(),
		ArgoCDNamespace: argoCDNamespace,
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopzerov1.ClusterCredential{}).
		// D-02: Watch Secrets — when a kubeconfig Secret is updated (e.g.,
		// rotated by Pulumi), find the ClusterCredentials that reference it
		// and requeue them immediately.
		Watches(&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(reconciler.findCredentialsForSecret),
		).
		Named("clustercredential").
		Complete(reconciler)
}

// findCredentialsForSecret maps a Secret to the ClusterCredentials that reference it.
// MEET-053 D-02: Enables immediate reconciliation on kubeconfig Secret rotation.
func (r *ClusterCredentialReconciler) findCredentialsForSecret(ctx context.Context, obj client.Object) []ctrl.Request {
	secret := obj.(*corev1.Secret)

	// List all ClusterCredentials and find those referencing this Secret.
	var credList loopzerov1.ClusterCredentialList
	if err := r.List(ctx, &credList); err != nil {
		return nil
	}

	var requests []ctrl.Request
	for _, cred := range credList.Items {
		if cred.Spec.SecretRef.Name == secret.Name &&
			cred.Spec.SecretRef.Namespace == secret.Namespace {
			requests = append(requests, ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(&cred),
			})
		}
	}
	return requests
}
