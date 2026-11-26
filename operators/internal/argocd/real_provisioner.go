// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package argocd

import (
	"context"
	"fmt"
	"log/slog"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"

	"github.com/getloopscale/loop-runtime/operators/internal/constants"
)

var applicationGVR = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "applications",
}

var appProjectGVR = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "appprojects",
}

// Compile-time interface compliance check.
var _ Provisioner = (*RealProvisioner)(nil)

// RealProvisioner manages ArgoCD Application CRs using the Kubernetes API.
//
// ARCH-OP-003: Uses dynamic/unstructured client intentionally to avoid pinning
// the argoproj.io CRD Go types as a dependency. The ArgoCD Application and
// AppProject schemas evolve independently of this operator, and a typed client
// would create a tight coupling requiring coordinated version bumps. The trade-off
// is no compile-time type safety for the CR spec — mitigated by integration tests.
type RealProvisioner struct {
	client    dynamic.Interface
	namespace string // ArgoCD namespace (typically "argocd")
}

// NewRealProvisioner creates a provisioner that manages real ArgoCD Application CRs.
// The dynamic client is injected rather than created internally (ARCH-003: DIP compliance).
// In cmd/main.go, create the client via dynamic.NewForConfig(mgr.GetConfig()) and pass it here.
func NewRealProvisioner(namespace string, client dynamic.Interface) *RealProvisioner {
	return &RealProvisioner{
		client:    client,
		namespace: namespace,
	}
}

// EnsureAppProject creates or updates an ArgoCD AppProject CR for tenant RBAC isolation (MOD-5).
//
// CHG-2026-04-30-DELETE-SINGLE-CELL: when CellID is non-empty AND not the
// "cell-0" sentinel, AppProject destinations use `name: <cellID>`
// (matches cluster Secret label) instead of `server: <URL>`. This keeps
// AppProject + Application destinations consistent — both resolve via
// ArgoCD's cluster name registry instead of pinning to an apiServer URL
// that may rotate (k3d) or be inaccurate (multi-cell).
func (p *RealProvisioner) EnsureAppProject(ctx context.Context, spec AppProjectSpec) error {
	// Build destination list scoped to tenant namespaces only.
	// Each destination restricts to the tenant's environment namespaces,
	// preventing cross-tenant deployment via ArgoCD.
	// SEC-007: Use explicit destination server instead of wildcard to prevent
	// cross-cluster deployment when multiple clusters are registered in ArgoCD.
	//
	// F-08 fix (2026-05-07 cleanroom dogfood): the previous cell-0
	// sentinel exclusion forced `server: <URL>` resolution, which broke
	// when the operator's WORKLOAD_CLUSTER_URL env (DNS hostname) didn't
	// match the cluster Secret's `data.server` (IP). Now ALL non-empty
	// cellIDs use name-based lookup. ClusterCredential.spec.clusterName
	// is required to equal cellID for cell-0 deploys (enforced
	// canonically by infra/scripts/bootstrap-cell-0-cc.sh).
	useName := spec.CellID != ""
	destServer := spec.DestinationServer
	if destServer == "" {
		destServer = "*"
	}

	destinations := make([]interface{}, 0, len(spec.DestinationNamespaces))
	for _, ns := range spec.DestinationNamespaces {
		dest := map[string]interface{}{"namespace": ns}
		if useName {
			dest["name"] = spec.CellID
		} else {
			dest["server"] = destServer
		}
		destinations = append(destinations, dest)
	}
	// SEC-007: No glob fallback. If no namespaces are provided, the AppProject
	// will have an empty destinations list, blocking all deployments until the
	// EnvironmentController creates the namespaces and the next reconcile
	// populates this list. This is fail-closed by design.
	if len(destinations) == 0 {
		slog.Warn("argocd provisioner: no destination namespaces for tenant, AppProject will block deploys until environments are reconciled",
			"tenant", spec.TenantID, "project", spec.Name)
	}

	proj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "argoproj.io/v1alpha1",
			"kind":       "AppProject",
			"metadata": map[string]interface{}{
				"name":      spec.Name,
				"namespace": p.namespace,
				"labels": map[string]interface{}{
					constants.LabelTenant:    spec.TenantID,
					constants.LabelManagedBy: constants.ManagedByTenantController,
				},
			},
			"spec": map[string]interface{}{
				"sourceRepos":  toInterfaceSlice(spec.SourceRepos),
				"destinations": destinations,
				"clusterResourceWhitelist": []interface{}{
					map[string]interface{}{
						"group": "",
						"kind":  "Namespace",
					},
				},
			},
		},
	}

	existing, err := p.client.Resource(appProjectGVR).Namespace(p.namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("argocd: get appproject %s: %w", spec.Name, err)
		}
		_, err = p.client.Resource(appProjectGVR).Namespace(p.namespace).Create(ctx, proj, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("argocd: create appproject %s: %w", spec.Name, err)
		}
		return nil
	}

	proj.SetResourceVersion(existing.GetResourceVersion())

	// Preserve what the ArgoCD controller owns (annotations, labels, finalizers, .status).
	mergeExistingState(existing, proj)

	_, err = p.client.Resource(appProjectGVR).Namespace(p.namespace).Update(ctx, proj, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("argocd: update appproject %s: %w", spec.Name, err)
	}
	return nil
}

// toInterfaceSlice converts a string slice to an interface slice for unstructured objects.
func toInterfaceSlice(ss []string) []interface{} {
	result := make([]interface{}, len(ss))
	for i, s := range ss {
		result[i] = s
	}
	return result
}

// buildApplicationObject constructs the ArgoCD Application unstructured object.
// Pure function (no side effects) — testable without a K8s client.
//
// Sync policy: manual sync only (automated: nil). The API controls sync timing
// via POST /api/v1/applications/{name}/sync. Auto-sync (selfHeal/prune) creates
// race conditions with the API's manual trigger. See MEET-2026-03-27-012.
//
// CreateNamespace: false — the TenantController creates namespaces, not ArgoCD.
func buildApplicationObject(spec ApplicationSpec) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "argoproj.io/v1alpha1",
			"kind":       "Application",
			"metadata": map[string]interface{}{
				"name": spec.Name,
				"labels": func() map[string]interface{} {
					// Sprint B B3 prep (code-005): emit runtime.getloopscale.com/environment label
					// when spec.Environment is non-empty so the label-based
					// parser path (future activator iteration) can disambiguate
					// hyphenated env names. Backward-compat: empty Environment
					// → label omitted, no behavior change.
					labels := map[string]interface{}{
						constants.LabelTenant:    spec.TenantID,
						constants.LabelManagedBy: constants.ManagedByTenantController,
					}
					if spec.Environment != "" {
						labels[constants.LabelEnvironment] = spec.Environment
					}
					return labels
				}(),
				"annotations": map[string]interface{}{
					// ArgoCD Notifications: subscribe to sync-completed trigger.
					// When Synced + Healthy, ArgoCD calls POST /internal/argocd-callback
					// on the API to activate the release (push-based, replaces polling).
					"notifications.argoproj.io/subscribe.on-sync-healthy.loop-runtime-api": "",
					// MEET-2026-04-22-058 (B12): skip-reconcile REMOVED. The annotation
					// coupled two writers (operator sets, API removes via broken PATCH)
					// and required Casbin RBAC scope that doesn't exist. Amplification
					// risk is bounded by manual sync + self-heal=false + 3m interval.
					// Revisit at >1k apps; see ADR on removal.
				},
				"finalizers": []interface{}{
					constants.ArgoCDResourcesFinalizer,
				},
			},
			"spec": map[string]interface{}{
				"project": spec.TenantID,
				"source": map[string]interface{}{
					"repoURL":        spec.RepoURL,
					"targetRevision": spec.TargetRevision,
					"path":           spec.Path,
					"directory": map[string]interface{}{
						"recurse": true,
					},
				},
				"destination": buildDestination(spec),
				"syncPolicy": map[string]interface{}{
					"automated": nil,
					"syncOptions": []interface{}{
						"CreateNamespace=false",
						"PrunePropagationPolicy=foreground",
						"PruneLast=true",
					},
				},
				// argo-rollouts-progressive-delivery-plan T2.1 — ArgoCD must
				// ignore fields modified at runtime by Argo Rollouts and the
				// scaler (HPA/KEDA), otherwise every reconcile would show
				// drift and OutOfSync state, blocking notifications and
				// potentially overwriting canary progress on auto-sync.
				"ignoreDifferences": []interface{}{
					// Rollout replicas — KEDA/HPA modifies at runtime.
					map[string]interface{}{
						"group": "argoproj.io",
						"kind":  "Rollout",
						"jsonPointers": []interface{}{
							"/spec/replicas",
						},
					},
					// Legacy Deployment replicas (D8 fallback path).
					map[string]interface{}{
						"group": "apps",
						"kind":  "Deployment",
						"jsonPointers": []interface{}{
							"/spec/replicas",
						},
					},
					// TraefikService weights — Argo Rollouts shifts at runtime
					// during canary steps. Both indices listed because the
					// renderer always emits stable+canary in fixed order.
					map[string]interface{}{
						"group": "traefik.io",
						"kind":  "TraefikService",
						"jsonPointers": []interface{}{
							"/spec/weighted/services/0/weight",
							"/spec/weighted/services/1/weight",
						},
					},
					// EC-13: Service selectors — Argo Rollouts injects
					// rollouts-pod-template-hash to scope selectors to
					// stable/canary revision. The renderer doesn't emit this
					// field; Rollouts adds it at runtime.
					map[string]interface{}{
						"group": "",
						"kind":  "Service",
						"jsonPointers": []interface{}{
							"/spec/selector/rollouts-pod-template-hash",
						},
					},
				},
			},
		},
	}
}

// EnsureApplication creates or updates an ArgoCD Application CR.
//
// T4.3 / CODE-005: the Update path is wrapped in retry.RetryOnConflict so
// concurrent reconciles updating the same Application do not surface 409
// Conflict errors to the controller queue. Each retry re-fetches the
// resource to obtain a fresh resourceVersion.
func (p *RealProvisioner) EnsureApplication(ctx context.Context, spec ApplicationSpec) error {
	desired := buildApplicationObject(spec)
	desired.Object["metadata"].(map[string]interface{})["namespace"] = p.namespace

	// Try to get existing application
	existing, err := p.client.Resource(applicationGVR).Namespace(p.namespace).Get(ctx, spec.Name, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("argocd: get application %s: %w", spec.Name, err)
		}
		// Resource does not exist — create it.
		_, err = p.client.Resource(applicationGVR).Namespace(p.namespace).Create(ctx, desired, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("argocd: create application %s: %w", spec.Name, err)
		}
		return nil
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Re-fetch on each attempt for a fresh resourceVersion.
		current, getErr := p.client.Resource(applicationGVR).Namespace(p.namespace).Get(ctx, spec.Name, metav1.GetOptions{})
		if getErr != nil {
			if errors.IsNotFound(getErr) {
				// Disappeared between Get and retry — treat as success
				// (caller will see eventual consistency on next reconcile).
				_ = existing
				return nil
			}
			return fmt.Errorf("argocd: refetch application %s: %w", spec.Name, getErr)
		}
		desired.SetResourceVersion(current.GetResourceVersion())
		mergeExistingState(current, desired)
		_, updateErr := p.client.Resource(applicationGVR).Namespace(p.namespace).Update(ctx, desired, metav1.UpdateOptions{})
		if updateErr != nil {
			return updateErr
		}
		return nil
	})
}

// mergeExistingState carries over the parts of an existing ArgoCD resource that are NOT
// ours to author, so the full-replace Update below does not erase them. What survives:
//
//   - annotations and labels — ArgoCD adds its own (notification subscriptions, tracking)
//   - finalizers — including resources-finalizer.argocd.argoproj.io, whose loss would
//     silently disable the cascade delete
//   - .status — written by the ArgoCD application-controller, never by us
//
// The .status clause exists because the CRD does NOT enable the `status` subresource
// (`kubectl get crd applications.argoproj.io -o jsonpath='{.spec.versions[*].subresources}'`
// returns `{}`), so `.status` is an ordinary field and a whole-object Update replaces it.
// Dropping it made sync/health flap to empty every few seconds, and every reader — including
// `loop-runtime platform status` — saw false drift (issue #271). If ArgoCD upstream ever enables the
// subresource, this clause becomes inert and can go.
//
// The .status copy runs BEFORE the metadata guard on purpose: preserving status must not
// depend on metadata being well-formed. Putting it after the guard would reproduce the very
// shape of the original defect — a function that promises to preserve N things and quietly
// preserves N-1.
func mergeExistingState(existing, desired *unstructured.Unstructured) {
	if st, ok := existing.Object["status"]; ok && st != nil {
		desired.Object["status"] = st
	}

	existingMeta, _ := existing.Object["metadata"].(map[string]interface{})
	desiredMeta, _ := desired.Object["metadata"].(map[string]interface{})
	if existingMeta == nil || desiredMeta == nil {
		return
	}

	// Merge annotations: keep existing, overlay ours
	if existingAnnotations, ok := existingMeta["annotations"].(map[string]interface{}); ok {
		merged := make(map[string]interface{}, len(existingAnnotations))
		for k, v := range existingAnnotations {
			merged[k] = v
		}
		if ourAnnotations, ok := desiredMeta["annotations"].(map[string]interface{}); ok {
			for k, v := range ourAnnotations {
				merged[k] = v
			}
		}
		// MEET-2026-04-22-058 (B12): actively strip legacy skip-reconcile annotation
		// from any pre-existing Application. Converges stale CRs on first reconcile.
		delete(merged, "argocd.argoproj.io/skip-reconcile")
		desiredMeta["annotations"] = merged
	}

	// Merge labels: keep existing, overlay ours
	if existingLabels, ok := existingMeta["labels"].(map[string]interface{}); ok {
		merged := make(map[string]interface{}, len(existingLabels))
		for k, v := range existingLabels {
			merged[k] = v
		}
		if ourLabels, ok := desiredMeta["labels"].(map[string]interface{}); ok {
			for k, v := range ourLabels {
				merged[k] = v
			}
		}
		desiredMeta["labels"] = merged
	}

	// Preserve existing finalizers
	if existingFinalizers, ok := existingMeta["finalizers"]; ok {
		desiredMeta["finalizers"] = existingFinalizers
	}
}

// DeleteApplications removes all ArgoCD Application CRs for a tenant.
func (p *RealProvisioner) DeleteApplications(ctx context.Context, tenantID string) error {
	list, err := p.client.Resource(applicationGVR).Namespace(p.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", constants.LabelTenant, tenantID),
	})
	if err != nil {
		return fmt.Errorf("argocd: list applications for tenant %s: %w", tenantID, err)
	}

	for _, app := range list.Items {
		if err := p.client.Resource(applicationGVR).Namespace(p.namespace).Delete(ctx, app.GetName(), metav1.DeleteOptions{}); err != nil {
			if errors.IsNotFound(err) {
				continue // Already deleted by concurrent reconcile — idempotent
			}
			return fmt.Errorf("argocd: delete application %s: %w", app.GetName(), err)
		}
	}

	return nil
}

// ListApplicationNames returns the metadata.name of every ArgoCD Application
// CR labeled with the given tenantID. #71 LIVE catch 2026-06-07: tenant_gitops
// uses this to compute the set of Apps to prune after the active
// PreviewEnvironment set shrinks (close-PR cleanup).
func (p *RealProvisioner) ListApplicationNames(ctx context.Context, tenantID string) ([]string, error) {
	list, err := p.client.Resource(applicationGVR).Namespace(p.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", constants.LabelTenant, tenantID),
	})
	if err != nil {
		return nil, fmt.Errorf("argocd: list applications for tenant %s: %w", tenantID, err)
	}
	names := make([]string, 0, len(list.Items))
	for _, app := range list.Items {
		names = append(names, app.GetName())
	}
	return names, nil
}

// DeleteApplication removes a SINGLE ArgoCD Application CR by name. Idempotent
// — NotFound is not an error (concurrent reconcile race). #71 LIVE catch
// 2026-06-07.
func (p *RealProvisioner) DeleteApplication(ctx context.Context, appName string) error {
	err := p.client.Resource(applicationGVR).Namespace(p.namespace).Delete(ctx, appName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("argocd: delete application %s: %w", appName, err)
	}
	return nil
}

// DeleteAppProject removes the ArgoCD AppProject CR for a tenant.
func (p *RealProvisioner) DeleteAppProject(ctx context.Context, tenantID string) error {
	err := p.client.Resource(appProjectGVR).Namespace(p.namespace).Delete(ctx, tenantID, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("argocd: delete appproject %s: %w", tenantID, err)
	}
	return nil
}

// buildDestination resolves the ArgoCD Application destination block.
//
// CHG-2026-04-30-DELETE-SINGLE-CELL: when CellID is non-empty AND not
// the legacy "cell-0" sentinel, ArgoCD destination uses `name:`
// pointing to the cluster Secret labeled `argocd.argoproj.io/secret-type=cluster`
// with `name=<cellID>` (eg. "cell-1-argocd-cluster" → name=cell-1).
// ArgoCD application-controller resolves this at sync time, picking up
// IP rotations / token refreshes without operator awareness.
//
// For "cell-0" or empty cellID we keep the explicit `server:` URL
// to preserve compatibility with the existing single-runtime-cluster
// installs whose argocd-cluster Secret may be named differently
// (eg. "runtime-argocd-cluster") OR who use the in-cluster API server
// directly.
//
// Vocabulary: the paragraph above uses "runtime" in two senses, on
// purpose. The hyphenated compound names a TOPOLOGY — an install that
// has exactly one runtime cluster. The quoted Secret name is DATA: a
// literal object name that exists on such installs, not prose, and
// rewording it here would make this comment describe a Secret that no
// install actually has. Neither sense is apimachinery's runtime package (k8s.io/apimachinery/pkg/runtime, NOT Go's stdlib runtime — the stdlib one has no Scheme type, which is what `*runtime.Scheme` in this tree proves), imported
// in this file as k8s.io/apimachinery/pkg/runtime/schema. The domain
// term stays "runtime"; the cluster name is frozen until AM8.
// See docs/decisions.md
// § Decision on the domain term (2026-09-06).
//
// SEC-007 invariant preserved: destination is explicit, never wildcard.
func buildDestination(spec ApplicationSpec) map[string]interface{} {
	dest := map[string]interface{}{
		"namespace": spec.DestinationNS,
	}
	// F-08 fix (2026-05-07): cell-0 is no longer a sentinel.
	// All non-empty cellIDs use name-based lookup. See EnsureAppProject
	// for full rationale.
	if spec.CellID != "" {
		dest["name"] = spec.CellID
	} else {
		dest["server"] = spec.DestinationURL
	}
	return dest
}
