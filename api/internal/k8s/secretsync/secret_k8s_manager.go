// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package secretsync owns the Kubernetes half of secret delivery: the K8s Secret
// written on the management cluster and the ExternalSecret CR written on the
// runtime cluster for ESO to reconcile.
//
// One of the three homes the B-058 split gave the responsibilities that used to
// share api/internal/k8s because they shared client-go. It is deliberately the K8s
// half only — the value's encryption lives in pkg/vault and api/internal/db/store.
package secretsync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/pkg/domain"
	"github.com/getloopscale/loopzero/pkg/naming"
)

// SecretK8sManager creates K8s Secrets on mgmt cluster and ExternalSecret CRs
// on the workload (runtime) cluster for ESO to sync.
type SecretK8sManager struct {
	mgmtClient     kubernetes.Interface
	runtimeDynamic dynamic.Interface
	mgmtNamespace  string
	// tenantCells and runtimeCellID keep writes on the one cluster this
	// manager can reach: runtimeDynamic serves runtimeCellID only.
	tenantCells   TenantCellLookup
	runtimeCellID string
}

// TenantCellLookup reads the cell a tenant was placed on (tenants.cell_id).
type TenantCellLookup interface {
	GetTenantCellID(ctx context.Context, tenantID string) (string, error)
}

// ErrTenantCellNotServed is returned when a tenant lives on a cell whose
// runtime cluster this manager has no client for. Writing its ExternalSecret
// to the cluster it does have would create an object in a cluster that does
// not host the tenant's namespace — the secret would never reach the app.
var ErrTenantCellNotServed = errors.New("secret sync: the tenant's cell is not served by this API instance")

// requireServedCell refuses before any cluster is touched when the tenant is
// placed on a cell other than the one runtimeDynamic points at.
func (m *SecretK8sManager) requireServedCell(ctx context.Context, tenantID string) error {
	cellID, err := m.tenantCells.GetTenantCellID(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("secret sync: read cell of tenant %s: %w", tenantID, err)
	}
	if cellID != m.runtimeCellID {
		return fmt.Errorf("%w: tenant %s is on %q, this instance writes to %q", ErrTenantCellNotServed, tenantID, cellID, m.runtimeCellID)
	}
	return nil
}

var externalSecretGVR = schema.GroupVersionResource{
	Group:    "external-secrets.io",
	Version:  "v1beta1",
	Resource: "externalsecrets",
}

// NewSecretK8sManager creates a manager that can create K8s Secrets on mgmt
// and ExternalSecret CRs on runtime cluster.
//
// CHG-2026-04-30-DOGFOOD-LOCAL-HEAL NEW-15 follow-up: prefer in-cluster
// rest config for the mgmt client so loopzero-api SA RBAC applies (Secret
// read/write granted via clusterrolebinding/loopzero-api). The previous
// out-of-cluster path used a kubeconfig with kube-system:default SA token
// which doesn't have those permissions, producing 403 on every loopzero
// secret set call.
func NewSecretK8sManager(mgmtKubeconfig, mgmtContext, runtimeKubeconfig, runtimeContext, runtimeCellID string, tenantCells TenantCellLookup) (*SecretK8sManager, error) {
	if tenantCells == nil || runtimeCellID == "" {
		return nil, errors.New("secret k8s manager: the tenant cell lookup and the runtime cell id are required")
	}
	// Mgmt cluster — typed client for Secret creation. In-cluster preferred.
	var mgmtConfig *rest.Config
	if inCfg, inErr := rest.InClusterConfig(); inErr == nil {
		mgmtConfig = inCfg
	} else {
		var err error
		mgmtConfig, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: mgmtKubeconfig},
			&clientcmd.ConfigOverrides{CurrentContext: mgmtContext},
		).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("secret k8s manager: mgmt config: %w", err)
		}
	}
	mgmtClient, err := kubernetes.NewForConfig(mgmtConfig)
	if err != nil {
		return nil, fmt.Errorf("secret k8s manager: mgmt client: %w", err)
	}

	// Runtime cluster — dynamic client for ExternalSecret CRs
	runtimeConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: runtimeKubeconfig},
		&clientcmd.ConfigOverrides{CurrentContext: runtimeContext},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("secret k8s manager: runtime config: %w", err)
	}
	runtimeDynamic, err := dynamic.NewForConfig(runtimeConfig)
	if err != nil {
		return nil, fmt.Errorf("secret k8s manager: runtime dynamic: %w", err)
	}

	return &SecretK8sManager{
		mgmtClient:     mgmtClient,
		runtimeDynamic: runtimeDynamic,
		mgmtNamespace:  "loopzero-system",
		tenantCells:    tenantCells,
		runtimeCellID:  runtimeCellID,
	}, nil
}

// k8sName sanitizes a name for K8s: lowercase, replace underscores with hyphens.
func k8sName(parts ...string) string {
	name := strings.ToLower(strings.Join(parts, "-"))
	return strings.ReplaceAll(name, "_", "-")
}

// secretName generates a K8s-compatible name for a secret.
func secretName(tenantID, app, name string) string {
	return k8sName("secret", tenantID, app, name)
}

// targetSecretName returns the materialized K8s Secret name that ESO will
// create from the ExternalSecret CR. When `app` is empty (tenant-wide
// secret), the app segment is omitted so the name does not end on a
// trailing hyphen, which would violate the K8s DNS-1123 label grammar
// `[a-z0-9]([-a-z0-9]*[a-z0-9])?`. naming.UserSecretName is shared with the
// renderer's envFrom, so the name written here is the name mounted there.
func targetSecretName(tenantID, projectID, env, app string) string {
	return naming.UserSecretName(tenantID, projectID, env, app)
}

// externalSecretName generates a K8s-compatible name for an ExternalSecret CR.
func externalSecretName(app, name string) string {
	return k8sName("es", app, name)
}

// buildSecretLabels emits the canonical loopzero.io/* label map for the K8s
// Secret AND the ExternalSecret CR. T4.1 of plan
// 2026-06-10-j22-layer2-code-review-fixes: omit `loopzero.io/app` when app
// is empty (tenant-wide secret) — an empty label-value field is legal
// per K8s spec but breaks selectors like `loopzero.io/app=foo` that match
// the empty label too. Centralizing the label construction guarantees
// the Secret and the ExternalSecret CR carry identical labels — the
// previous duplicated map literals at two call sites were a DRY
// violation flagged by /loop-code-review.
func buildSecretLabels(tenantID, projectID, app string) map[string]string {
	labels := map[string]string{
		"loopzero.io/tenant":     tenantID,
		"loopzero.io/project":    projectID,
		"loopzero.io/managed-by": "loopzero-api",
	}
	if app != "" {
		labels["loopzero.io/app"] = app
	}
	return labels
}

// tenantNamespaceFor resolves the canonical M2 namespace via the
// EnvRef Value Object (3-segment `loopzero-<tenant>-<project>-<env>`).
//
// T1.3.d of plan 2026-06-11-code-review-remediation v1.2 migrated this
// helper from pkg/naming.Namespace to pkg/domain.EnvRef.Namespace()
// per ADR D2. The two-step construction (NewEnvRef + .Namespace())
// adds stronger typing: empty-field defense fires at construction,
// not at the formula. T4.1 + ADR D5 absorption (plan 2026-06-10-
// j22-layer2-code-review-fixes): the helper returns `(string, error)`
// because the canonical method validates DNS-1123 conformance and the
// combined-name 63-char budget upstream. On error we wrap-and-propagate
// per ADR D5 — silent ignore would mask upstream validation gaps that
// feedback_post_feature_live_regression_workflow_2026_06_09 teaches us
// to surface honestly.
func tenantNamespaceFor(tenantID, projectID, env string) (string, error) {
	ref, refErr := domain.NewEnvRef(tenantID, projectID, env)
	if refErr != nil {
		return "", fmt.Errorf("k8s secret: EnvRef: %w", refErr)
	}
	ns, err := ref.Namespace()
	if err != nil {
		return "", fmt.Errorf("k8s secret: namespace: %w", err)
	}
	return ns, nil
}

// CreateSecret creates a K8s Secret on the mgmt cluster and an ExternalSecret CR
// on the runtime cluster for ESO to sync the value to the workload namespace.
func (m *SecretK8sManager) CreateSecret(ctx context.Context, tenantID, projectID, env, app, name, value string) error {
	if err := m.requireServedCell(ctx, tenantID); err != nil {
		// B-177: the row is already persisted when CreateSecret runs, so this
		// exit counts like every other one.
		metrics.SecretExternalSecretTotal.WithLabelValues("fail").Inc()
		return err
	}
	// T4.1 (plan 2026-06-10-j22-layer2-code-review-fixes): rename local var to
	// `secretObjName` so it no longer shadows the package-level `k8sName` func
	// declared 30 LOC above. Originally LIVE-caught by /loop-code-review as a
	// MEDIUM naming-collision finding.
	secretObjName := secretName(tenantID, app, name)

	// 1. Create/update K8s Secret on mgmt cluster
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretObjName,
			Namespace: m.mgmtNamespace,
			Labels:    buildSecretLabels(tenantID, projectID, app),
		},
		StringData: map[string]string{
			"value": value,
		},
	}

	_, err := m.mgmtClient.CoreV1().Secrets(m.mgmtNamespace).Get(ctx, secretObjName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			_, err = m.mgmtClient.CoreV1().Secrets(m.mgmtNamespace).Create(ctx, secret, metav1.CreateOptions{})
		}
	} else {
		_, err = m.mgmtClient.CoreV1().Secrets(m.mgmtNamespace).Update(ctx, secret, metav1.UpdateOptions{})
	}
	if err != nil {
		// B-177: count this exit. By the time CreateSecret runs, the caller in
		// routes/secrets has already persisted the row via store.Set, so
		// returning here leaves a row in `secrets` behind. The `result="fail"`
		// series is the observable form of that — see the comment on
		// metrics.SecretExternalSecretTotal for why every exit of this
		// function has to move it.
		metrics.SecretExternalSecretTotal.WithLabelValues("fail").Inc()
		return fmt.Errorf("create k8s secret on mgmt: %w", err)
	}

	// 2. Create/update ExternalSecret CR on runtime cluster.
	// Canonical M2 namespace: loopzero-{tenantID}-{projectID}-{env}
	// (3-segment per `pkg/naming.Namespace`). The earlier draft used the
	// M1 legacy 2-segment `loopzero-{tenantID}-{env}` which the TenantController
	// stopped creating in M2 (plan 2026-06-09-m2-operator-canonical-fix).
	// LIVE-caught 2026-06-10 J-22 walkthrough: secret POST returned
	// status=created (PG persist OK with vault:v1: prefix) but
	// sync_status=failed because the ExternalSecret targeted
	// `loopzero-{tenantID}-{env}` which does not exist post-M2 cutover.
	// T4.1 + ADR D5: invoke the canonical `naming.Namespace` helper via the
	// `tenantNamespaceFor(...)` wrapper above. Errors surface (never silent).
	tenantNS, err := tenantNamespaceFor(tenantID, projectID, env)
	if err != nil {
		// B-177: same reason as the mgmt-write exit above — the row is already
		// persisted when this return is taken.
		metrics.SecretExternalSecretTotal.WithLabelValues("fail").Inc()
		return err
	}
	esName := externalSecretName(app, name)
	esLabels := buildSecretLabels(tenantID, projectID, app)
	esLabelsAny := make(map[string]interface{}, len(esLabels))
	for k, v := range esLabels {
		esLabelsAny[k] = v
	}

	externalSecret := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "external-secrets.io/v1beta1",
			"kind":       "ExternalSecret",
			"metadata": map[string]interface{}{
				"name":      esName,
				"namespace": tenantNS,
				"labels":    esLabelsAny,
			},
			"spec": map[string]interface{}{
				"refreshInterval": "30s",
				"secretStoreRef": map[string]interface{}{
					// Plan v1.2 D3 + plan 2026-06-02-openbao-adoption T4.1:
					// `loopzero-broker` is the canonical ESO ClusterSecretStore for
					// user secrets. provider.webhook calls loopzero-api's
					// /internal/v1/secret-fetch/{tenant_id}/{secret_name}
					// endpoint with a Bearer token; loopzero-api validates the
					// auth, enforces tenant↔cell binding (EC-07), decrypts
					// the vault:v1: ciphertext via OpenBao transit, and
					// returns plaintext_b64. ESO base64-decodes into the
					// materialized K8s Secret. LIVE-validated 2026-06-10
					// J-22 walkthrough (ClusterSecretStore Ready=True +
					// in-cluster probe HTTP 200 + plaintext round-trip).
					"name": "loopzero-broker",
					"kind": "ClusterSecretStore",
				},
				"target": map[string]interface{}{
					// Must match envFrom secretRef in manifests/renderer.go.
					// When app is empty (tenant-wide secret), omit the trailing
					// segment so the K8s resource name stays inside the
					// `[a-z0-9]([-a-z0-9]*[a-z0-9])?` grammar. Trailing hyphen
					// LIVE-caught 2026-06-10 J-22 walkthrough.
					"name":           targetSecretName(tenantID, projectID, env, app),
					"creationPolicy": "Owner",
				},
				"data": []interface{}{
					map[string]interface{}{
						"secretKey": name,
						"remoteRef": map[string]interface{}{
							// loopzero-broker webhook URL substitutes 2 variables into
							// the path: `.remoteRef.key` (tenant_id) +
							// `.remoteRef.property` (composite `secretName.projectID.env`).
							// T2.1 + ADR D2 of plan 2026-06-10-j22-layer2-code-review-fixes
							// (closes HIGH #15 scope-drop): project + environment thread
							// through to broker SQL composite-key scope.
							//
							// 2026-06-11 LIVE walkthrough catch — ESO v1beta1 CRD strict
							// decoder REJECTS spec.data[].remoteRef.metadata as unknown
							// field. So the encoding moves into the `property` field
							// using `.` as separator (EC-08 regex `^[A-Z][A-Z0-9_]*$`
							// forbids `.` in secret names → safe non-ambiguous delimiter).
							// Broker parses property → split on `.` → 3 parts.
							//
							// Slashes are also problematic: ESO URL-encodes `/` inside a
							// SINGLE value (e.g., `tn-X/SECRET` → `tn-X%2FSECRET`) which
							// chi rejects as 404. Dot delimiter survives URL encoding +
							// chi routing.
							"key":      tenantID,
							"property": fmt.Sprintf("%s.%s.%s", name, projectID, env),
						},
					},
				},
			},
		},
	}

	existing, err := m.runtimeDynamic.Resource(externalSecretGVR).Namespace(tenantNS).Get(ctx, esName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			_, err = m.runtimeDynamic.Resource(externalSecretGVR).Namespace(tenantNS).Create(ctx, externalSecret, metav1.CreateOptions{})
		}
	} else {
		// V9 dogfood 2026-05-29 cleanroom-fresh retry surfaced this bug:
		// K8s Update is optimistic-concurrency by resourceVersion. The
		// freshly-built `externalSecret` unstructured object has no
		// resourceVersion → K8s rejects the Update with:
		//   "metadata.resourceVersion: Invalid value: 0x0: must be
		//    specified for an update"
		// Fix: carry the existing object's resourceVersion forward so
		// the Update commits against the same row the Get observed.
		// This was THE V9-blocker — 13 dogfood specs failed on this
		// path. Sponsor goal-directive 2026-05-29 unblocks via this fix.
		externalSecret.SetResourceVersion(existing.GetResourceVersion())
		_, err = m.runtimeDynamic.Resource(externalSecretGVR).Namespace(tenantNS).Update(ctx, externalSecret, metav1.UpdateOptions{})
		// T4.2 + EC-4 absorption (plan 2026-06-10-j22-layer2-code-review-fixes):
		// IsConflict can arise when ESO controller mutates the CR between
		// our Get and our Update (label refresh, status block edit). The
		// canonical recovery is to re-Get + carry the fresh resourceVersion
		// once. Bounded to a SINGLE retry — under persistent conflict the
		// caller (routes/secrets.go) records sync_status=failed and the
		// dashboard surfaces the error. Unbounded retry would DoS the K8s
		// API on a stuck conflict; EC-4 explicitly guards against that.
		if err != nil && k8serrors.IsConflict(err) {
			refreshed, refreshErr := m.runtimeDynamic.Resource(externalSecretGVR).Namespace(tenantNS).Get(ctx, esName, metav1.GetOptions{})
			if refreshErr == nil {
				externalSecret.SetResourceVersion(refreshed.GetResourceVersion())
				_, err = m.runtimeDynamic.Resource(externalSecretGVR).Namespace(tenantNS).Update(ctx, externalSecret, metav1.UpdateOptions{})
			}
		}
	}
	if err != nil {
		// dogfood-2026-05-08-fixes T2.3: emit fail metric so silent skip
		// can be observed via Prometheus. Secret VALUE is NEVER logged.
		metrics.SecretExternalSecretTotal.WithLabelValues("fail").Inc()
		slog.Error("ExternalSecret create/update failed",
			"namespace", tenantNS,
			"name", esName,
			"tenant", tenantID,
			"app", app,
			"secret_name", name,
			"error", err)
		return fmt.Errorf("create external secret on runtime: %w", err)
	}

	// dogfood-2026-05-08-fixes T2.3: emit success metric + structured log.
	// Secret VALUE NEVER appears in metric label or log field — only the
	// secret NAME (e.g. "API_KEY") is logged for traceability.
	metrics.SecretExternalSecretTotal.WithLabelValues("success").Inc()
	slog.Info("ExternalSecret created",
		"namespace", tenantNS,
		"name", esName,
		"tenant", tenantID,
		"app", app,
		"secret_name", name)
	return nil
}

// DeleteSecret removes the K8s Secret from mgmt and ExternalSecret CR from runtime.
// projectID is the canonical M2 namespace segment (per `pkg/naming.Namespace`).
// LIVE-caught 2026-06-10 J-22 walkthrough: signature extended with projectID
// so the delete path mirrors CreateSecret's M2 canonical 3-segment namespace.
func (m *SecretK8sManager) DeleteSecret(ctx context.Context, tenantID, projectID, env, app, name string) error {
	if err := m.requireServedCell(ctx, tenantID); err != nil {
		return err
	}
	// T4.1: rename local var to avoid shadowing the package-level `k8sName` func.
	secretObjName := secretName(tenantID, app, name)

	// 1. Delete K8s Secret from mgmt
	err := m.mgmtClient.CoreV1().Secrets(m.mgmtNamespace).Delete(ctx, secretObjName, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("delete k8s secret from mgmt: %w", err)
	}

	// 2. Delete ExternalSecret CR from runtime (M2 canonical 3-segment ns
	//    via `naming.Namespace` — T4.1 + ADR D5 absorption).
	tenantNS, err := tenantNamespaceFor(tenantID, projectID, env)
	if err != nil {
		return err
	}
	esName := externalSecretName(app, name)
	err = m.runtimeDynamic.Resource(externalSecretGVR).Namespace(tenantNS).Delete(ctx, esName, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("delete external secret from runtime: %w", err)
	}

	return nil
}
