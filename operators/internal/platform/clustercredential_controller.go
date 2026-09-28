// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package platform

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"k8s.io/client-go/rest"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/conditions"
	"github.com/getloopscale/loopzero/operators/internal/constants"
	"github.com/getloopscale/loopzero/operators/internal/metrics"
	"github.com/getloopscale/loopzero/operators/internal/reconcileloop"
	"github.com/getloopscale/loopzero/pkg/correlation"
)

const (
	clusterCredentialFinalizer = constants.FinalizerClusterCredential
)

// ClusterCredentialReconciler reconciles a ClusterCredential object.
//
// ADR-0026 D4 / plan T4.1: kubebuilder:rbac markers.
//
// +kubebuilder:rbac:groups=loopzero.io,resources=clustercredentials,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=loopzero.io,resources=clustercredentials/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=loopzero.io,resources=clustercredentials/finalizers,verbs=update
// +kubebuilder:rbac:groups="",namespace=loopzero-system,resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",namespace=argocd,resources=secrets,verbs=get;list;watch;create;update;delete
type ClusterCredentialReconciler struct {
	client.Client
	ArgoCDNamespace string

	// CAFetcher reads the live cluster CA from the target cluster's
	// kube-root-ca.crt ConfigMap on every reconcile (plan
	// 2026-05-25-live-ca-fetch-clustercredential, T1.1 + T2.1).
	// MUST be non-nil — wired by SetupClusterCredentialController to
	// newKubeRootCAFetcher() by default. Tests inject fakes.
	CAFetcher caFetcher
}

// clusterCredentialDetailTypes are the per-step conditions that
// SetReadinessSummary classifies for kstatus: TypeConnected, plus
// TypeArgoCDSecretSynced when the credential feeds ArgoCD — a cell ArgoCD
// cannot deploy to is not Ready. TypeSecretNotFound has inverted polarity
// (True = bad) per ADR-0025 condition #3 and is deliberately excluded.
func clusterCredentialDetailTypes(cr *loopzerov1.ClusterCredential) []string {
	if cr.Spec.Scope.ArgoCD {
		return []string{conditions.TypeConnected, conditions.TypeArgoCDSecretSynced}
	}
	return []string{conditions.TypeConnected}
}

// Reconcile handles the reconciliation loop for ClusterCredential resources.
func (r *ClusterCredentialReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
	ctx, span := reconcileloop.StartReconcileSpan(ctx, "clustercredential", req.Name, req.Namespace)
	defer span.End()
	start := time.Now()
	defer func() {
		if retErr != nil {
			reconcileloop.RecordError(span, retErr)
		} else {
			reconcileloop.SetSuccess(span)
		}
		reconcileloop.ReconcileOutcome("clustercredential", start, retErr)
	}()

	logger := log.FromContext(ctx)

	var cr loopzerov1.ClusterCredential
	if err := r.Get(ctx, req.NamespacedName, &cr); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// T1.1 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap: propagate
	// correlation-id from CR annotation into reconcile context (no-op when absent).
	ctx = correlation.WithID(ctx, cr.GetAnnotations()["loopzero.io/correlation-id"])

	if !cr.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &cr)
	}

	if err := reconcileloop.EnsureFinalizer(ctx, r.Client, &cr, clusterCredentialFinalizer, req.NamespacedName); err != nil {
		return ctrl.Result{}, err
	}

	// Step 1: Resolve kubeconfig from the referenced Secret.
	// The base snapshot for every status write below.
	//
	// AFTER EnsureFinalizer above, which writes metadata and bumps
	// resourceVersion: a patch derived from an earlier snapshot carries the stale
	// version and becomes an optimistic lock frozen at snapshot time — a WIDER
	// conflict window than the refetch this contract removes. Measured as a real
	// 409 in slice-18.
	//
	// And before the first .Status mutation below, because a snapshot taken after
	// one diffs that field against itself: the patch would silently omit the very
	// change it exists to persist.
	//
	// The deletion path returns above and writes no status, so unlike the preview
	// controller it needs no base of its own.
	base := cr.DeepCopy()

	kubeconfigData, result, err := r.resolveKubeconfig(ctx, &cr, base)
	if err != nil || kubeconfigData == nil {
		return result, err
	}

	// Step 2 (fix-all-platform-issues-plan T1.2 / drift-baseline.md port #1):
	// ALWAYS reconcile ArgoCD cluster secret to mirror the kubeconfig Secret
	// token, BEFORE connectivity check. Previously the early-return on
	// connectivity=false (Step 3 below) skipped this sync, leaving the ArgoCD
	// secret with a stale token even after heal scripts (H12/H15) refreshed
	// the kubeconfig.
	// Effect: when operator action rotates the token in cell-X-kubeconfig,
	// the controller propagates it to <cell>-argocd-cluster on the next
	// reconcile (event-driven via the Secret watcher in
	// SetupClusterCredentialController), independent of whether the
	// destination cluster is currently reachable.
	syncErr := r.syncArgoCDClusterSecret(ctx, &cr, kubeconfigData)

	// Step 3: Validate cluster connectivity.
	connected, result, err := r.checkConnectivity(ctx, &cr, base, kubeconfigData)
	if err != nil || !connected {
		return result, err
	}

	// MEET-053 D-03: Record observed generation for change detection.
	cr.Status.ObservedGeneration = cr.Generation

	if statusErr := r.patchStatus(ctx, &cr, base); statusErr != nil {
		return ctrl.Result{}, statusErr
	}
	if syncErr != nil {
		// Connected, but ArgoCD cannot deploy to this cell: requeue with
		// backoff until the cluster Secret syncs.
		return ctrl.Result{}, syncErr
	}

	logger.Info("reconciled cluster credential", "cluster", cr.Spec.ClusterName, "connected", cr.Status.Connected)
	// AP-4 fix per ADR-0023 plan T5.3: drop the periodic-check requeue on
	// the SUCCESS path. SoT is the CR + Secret watch. Authoritative refresh:
	//   - kubeconfig Secret edits → Watches(&corev1.Secret{}, ...) fires
	//     immediate reconcile.
	//   - K8sCellStore polls every 60s and reads CR.spec.cellID; cell
	//     activation propagates within one poll cycle.
	// Removing the periodic poll on success drops ~1 reconcile/min/CR of
	// background load; failure paths below still requeue (RequeueSecretNotFound,
	// RequeuePeriodicCheck) as healing fallback.
	return ctrl.Result{}, nil
}

// syncArgoCDClusterSecret mirrors the credential into the ArgoCD cluster
// Secret when the credential feeds ArgoCD, and records the outcome on the
// ArgoCDSecretSynced condition. It runs before the connectivity check so a
// rotated token reaches ArgoCD even while the cell is unreachable
// (fix-all-platform-issues-plan T1.2). The error is returned so the caller
// can requeue; the connectivity check still runs, because it is independent.
func (r *ClusterCredentialReconciler) syncArgoCDClusterSecret(ctx context.Context, cr *loopzerov1.ClusterCredential, kubeconfigData []byte) error {
	if !cr.Spec.Scope.ArgoCD {
		return nil
	}
	if err := r.reconcileArgoCDClusterSecret(ctx, cr, kubeconfigData); err != nil {
		log.FromContext(ctx).Error(err, "argocd cluster secret sync failed", "cluster", cr.Spec.ClusterName)
		conditions.SetCondition(&cr.Status.Conditions, conditions.TypeArgoCDSecretSynced,
			metav1.ConditionFalse, "SyncFailed", err.Error(), cr.Generation)
		return fmt.Errorf("argocd cluster secret for %s: %w", cr.Spec.ClusterName, err)
	}
	conditions.SetCondition(&cr.Status.Conditions, conditions.TypeArgoCDSecretSynced,
		metav1.ConditionTrue, "Synced", "ArgoCD cluster Secret mirrors this credential", cr.Generation)
	return nil
}

// resolveKubeconfig reads and parses the kubeconfig from the referenced Secret.
// Returns (nil, result, nil) if the kubeconfig is not yet available (status updated, requeue set).
func (r *ClusterCredentialReconciler) resolveKubeconfig(ctx context.Context, cr, base *loopzerov1.ClusterCredential) ([]byte, ctrl.Result, error) {
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
			if statusErr := r.patchStatus(ctx, cr, base); statusErr != nil {
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
		if statusErr := r.patchStatus(ctx, cr, base); statusErr != nil {
			return nil, ctrl.Result{}, statusErr
		}
		return nil, ctrl.Result{RequeueAfter: constants.RequeuePeriodicCheck}, nil
	}

	return kubeconfigData, ctrl.Result{}, nil
}

// checkConnectivity validates that the cluster is reachable via /healthz.
// Returns (false, result, nil) if unreachable (status updated, requeue set).
func (r *ClusterCredentialReconciler) checkConnectivity(ctx context.Context, cr, base *loopzerov1.ClusterCredential, kubeconfigData []byte) (bool, ctrl.Result, error) {
	ctx, span := reconcileloop.StartChildSpan(ctx, "clustercredential.connectivity.check")
	defer span.End()
	logger := log.FromContext(ctx)

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		return r.markDisconnected(ctx, cr, base, "InvalidKubeconfig",
			fmt.Sprintf("Failed to parse kubeconfig: %v", err))
	}

	restConfig.Timeout = constants.ConnectivityTimeout
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return r.markDisconnected(ctx, cr, base, "ClientCreationFailed",
			fmt.Sprintf("Failed to create Kubernetes client: %v", err))
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
		if statusErr := r.patchStatus(ctx, cr, base); statusErr != nil {
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

// markDisconnected centralizes the "cluster is unreachable" failure handling
// in checkConnectivity: sets Status.Connected=false + sets the TypeConnected
// condition False with the given reason/msg + updates status + returns the
// canonical (false, RequeueAfter, nil) tuple. Phase 3 helper: collapses
// 2 inline 11-line copies (kubeconfig-parse + client-create error paths).
// The third "Unreachable / healthz fail" path is NOT consolidated here
// because it additionally writes Status.LastCheckedAt.
func (r *ClusterCredentialReconciler) markDisconnected(ctx context.Context, cr, base *loopzerov1.ClusterCredential, reason, msg string) (bool, ctrl.Result, error) {
	cr.Status.Connected = false
	conditions.SetCondition(&cr.Status.Conditions, conditions.TypeConnected,
		metav1.ConditionFalse, reason, msg, cr.Generation)
	if statusErr := r.patchStatus(ctx, cr, base); statusErr != nil {
		return false, ctrl.Result{}, statusErr
	}
	return false, ctrl.Result{RequeueAfter: constants.RequeuePeriodicCheck}, nil
}

// argoCDClusterConfig is the JSON structure ArgoCD expects in the cluster secret's "config" field.
// Contains bearer token for auth and TLS config. The full kubeconfig is NOT stored --
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
// exposure -- a compromised ArgoCD namespace only leaks a scoped bearer token.
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

	// Fail-fast if no authentication mechanism found.
	if token == "" && len(restConfig.CertData) == 0 {
		return fmt.Errorf("kubeconfig has no bearer token or client certificate -- exec-based auth not supported for ArgoCD cluster secrets")
	}

	// T4.6 / SEC-005: emit structured WARN when a ClusterCredential opts in
	// to TLS bypass. Lets SREs find every cluster operating without TLS
	// verification via Loki search on tls_insecure=true.
	if cr.Spec.TLS.Insecure {
		log.FromContext(ctx).Info("cluster credential reconciling with TLS verification disabled",
			"cluster", cr.Spec.ClusterName,
			"resource", cr.Name,
			"namespace", cr.Namespace,
			"tls_insecure", true,
			"audit", "tls_bypass_active",
			"finding_ref", "SEC-005",
		)
	}

	// Op B (plan 2026-05-25-live-ca-fetch-clustercredential, T2.1):
	// resolve caData via live fetch from target cluster's kube-root-ca.crt
	// ConfigMap. Falls back to kubeconfig snapshot on error. Skips fetch
	// when insecure=true (ArgoCD ignores caData anyway — saves a network
	// call and avoids noisy Warn in dev clusters).
	//
	// CAData format: ArgoCD expects base64-encoded PEM (NOT raw PEM bytes)
	// per CHG-002b 2026-04-27 — encoding happens HERE after we have either
	// the live PEM or the snapshot bytes.
	caData, caSource := r.resolveCAData(ctx, cr, restConfig)
	log.FromContext(ctx).V(1).Info("argocd cluster secret CA resolved",
		"cluster", cr.Spec.ClusterName, "ca_source", caSource)

	argoConfig := argoCDClusterConfig{
		BearerToken: token,
		TLSClientConfig: argoCDTLSConfig{
			Insecure: cr.Spec.TLS.Insecure,
			CAData:   caData,
		},
	}
	// G117 expected: ArgoCD's cluster-credential Secret REQUIRES the
	// bearer token to be serialized into its `data.config` field
	// (per ArgoCD docs: <https://argo-cd.readthedocs.io/en/stable/operator-manual/declarative-setup/#clusters>).
	// The marshaled JSON is then written into a K8s Secret (encrypted
	// at rest via etcd encryption + sealed-secret if configured); it
	// is NEVER logged or returned over an HTTP response. The G117
	// heuristic "field named BearerToken is marshaled" is correct in
	// the abstract but this is exactly the legitimate flow it would
	// fire on. Suppress with audit context.
	configJSON, err := json.Marshal(argoConfig) //nolint:gosec // G117 expected: ArgoCD cluster-credential Secret serialization (audit SEC-005)
	if err != nil {
		return fmt.Errorf("failed to marshal ArgoCD config: %w", err)
	}

	argoSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: r.ArgoCDNamespace,
		},
	}

	// The Secret has a second writer (ArgoCD itself, or an overlapping
	// reconcile), so a write can lose the optimistic-concurrency race.
	// CreateOrUpdate re-reads on each attempt; the CA is not fetched again.
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, argoSecret, func() error {
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
		return err
	})
	if err != nil {
		return fmt.Errorf("argocd cluster secret %s/%s: %w", r.ArgoCDNamespace, secretName, err)
	}

	return nil
}

// resolveCAData implements Op B (plan 2026-05-25-live-ca-fetch-clustercredential, T2.1):
// fetches the live cluster CA from the target cluster's kube-root-ca.crt
// ConfigMap, falls back to the kubeconfig snapshot on error, or short-circuits
// when CR opts into insecure mode (Op A precedence per ADR D4).
//
// Returns (base64-encoded PEM, source-tag). source-tag is one of:
//   - "live"             → fresh kube-root-ca.crt fetched
//   - "fallback"         → fetch failed; snapshot caData used (degraded)
//   - "skipped-insecure" → cr.Spec.TLS.Insecure=true; caData is "" (ArgoCD ignores)
//
// Always non-nil-safe; counter incremented per call for SRE visibility.
// 5s fetch budget keeps reconcile under its 30s deadline even when fetch hangs.
//
// ADR D2 (graceful fallback): never returns error — degrades to existing
// behavior. SRE detects degradation via loopzero_clustercredential_ca_fetch_total{result="fallback"}.
func (r *ClusterCredentialReconciler) resolveCAData(
	ctx context.Context,
	cr *loopzerov1.ClusterCredential,
	restConfig *rest.Config,
) (caDataB64, source string) {
	// D4: Op A precedence — short-circuit when insecure. ArgoCD ignores
	// caData when insecure=true, so the fetch would waste a network call
	// and produce noisy Warn logs in dev clusters where RBAC may legitimately
	// block. Counter labeled "skipped-insecure" so SRE Grafana can pivot.
	if cr.Spec.TLS.Insecure {
		metrics.ClusterCredentialCAFetchTotal.
			WithLabelValues(cr.Spec.ClusterName, "skipped-insecure").Inc()
		return "", "skipped-insecure"
	}

	// D1: try live fetch with 5s budget (reconcile budget = 30s).
	fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if r.CAFetcher == nil {
		// Defensive: should not happen — SetupClusterCredentialController
		// always wires newKubeRootCAFetcher. Log + fall through to snapshot.
		log.FromContext(ctx).Error(nil, "Op B disabled: CAFetcher is nil — using snapshot caData",
			"cluster", cr.Spec.ClusterName,
			"audit", "ca_fetcher_unwired")
	} else {
		livePEM, fetchErr := r.CAFetcher.FetchClusterCA(fetchCtx, restConfig)
		if fetchErr == nil {
			metrics.ClusterCredentialCAFetchTotal.
				WithLabelValues(cr.Spec.ClusterName, "success").Inc()
			return base64.StdEncoding.EncodeToString(livePEM), "live"
		}
		// D2: graceful fallback. Log structured + metric for SRE visibility.
		log.FromContext(ctx).Info("Op B live CA fetch failed; falling back to kubeconfig snapshot",
			"cluster", cr.Spec.ClusterName,
			"credential", cr.Name,
			"error", fetchErr.Error(),
			"audit", "ca_fetch_fallback",
			"audit_insecure_bootstrap", "true",
		)
	}
	metrics.ClusterCredentialCAFetchTotal.
		WithLabelValues(cr.Spec.ClusterName, "fallback").Inc()

	// Encode snapshot per CHG-002b 2026-04-27 contract (base64 PEM).
	if len(restConfig.CAData) > 0 {
		return base64.StdEncoding.EncodeToString(restConfig.CAData), "fallback"
	}
	return "", "fallback"
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
		CAFetcher:       newKubeRootCAFetcher(),
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopzerov1.ClusterCredential{}, reconcileloop.ForOption()).
		// D-02: Watch Secrets -- when a kubeconfig Secret is updated (e.g.,
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
		// A mapper cannot return an error; the lost enqueue is recovered by the
		// next event or resync, and this log is the only trace of it.
		log.FromContext(ctx).Error(err, "list ClusterCredentials to map a Secret event; enqueue dropped",
			"secret", client.ObjectKeyFromObject(secret).String())
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

// patchStatus is the single status writer of this controller. It folds the
// kstatus readiness summary in as the last mutation (ADR-0025 D3) and persists
// with a merge patch off base, so every write carries a Ready condition that
// agrees with the conditions written beside it.
func (r *ClusterCredentialReconciler) patchStatus(ctx context.Context, cr, base *loopzerov1.ClusterCredential) error {
	conditions.SetReadinessSummaryViaPkg(&cr.Status.Conditions, cr.Generation, clusterCredentialDetailTypes(cr))
	return r.Status().Patch(ctx, cr, client.MergeFrom(base))
}
