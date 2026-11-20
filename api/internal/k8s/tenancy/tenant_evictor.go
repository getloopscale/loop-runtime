// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenancy

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/getloopscale/loopzero/api/internal/k8s"
	"github.com/getloopscale/loopzero/pkg/naming"
)

// TenantK8sEvictor performs the K8s cluster teardown portion of bulk tenant
// eviction (T-02 phase 2). It replaces steps 1-4 of tools/admin-evict-tenants.sh:
//
//	step 1: delete Argo workflows in the build cluster
//	step 2: delete mgmt CRs (tenants, environments, loopzeroapplications,
//	        exposedroutes, applications.argoproj.io)
//	step 3: force-null finalizers on stuck mgmt CRs (3-pass retry)
//	step 4: force-finalize the tenant's runtime namespaces
//
// Step 5 (DB cleanup) remains in db.PostgresTenantStore.DeleteTenantsFKOrdered.
//
// A resource is in scope ONLY when the identity it carries names one of the
// evicted tenants exactly (see ownerOf). Tenant IDs are free-form and may be
// hyphen-extensions of each other ("tn-a", "tn-a-b"), so a resource name is
// never used as evidence of ownership: every operation here is forced and
// irreversible, and a prefix match would purge another customer.
type TenantK8sEvictor struct {
	mgmtDynamic        dynamic.Interface
	buildDynamic       dynamic.Interface
	runtimeTyped       kubernetes.Interface
	mgmtNamespace      string
	buildNamespace     string
	finalizerPassDelay time.Duration
}

// Resource types this evictor knows how to purge. Listed in deletion order:
// ArgoCD Applications first (they reference the other CRs), then the loopzero.io
// CRs.
var (
	argoApplicationsGVR     = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}
	exposedRoutesGVR        = schema.GroupVersionResource{Group: "loopzero.io", Version: "v1", Resource: "exposedroutes"}
	loopzeroApplicationsGVR = schema.GroupVersionResource{Group: "loopzero.io", Version: "v1", Resource: "loopzeroapplications"}
	environmentsGVR         = schema.GroupVersionResource{Group: "loopzero.io", Version: "v1", Resource: "environments"}
	tenantsGVR              = schema.GroupVersionResource{Group: "loopzero.io", Version: "v1", Resource: "tenants"}
	argoWorkflowGVR         = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "workflows"}
)

var mgmtCRTypes = []schema.GroupVersionResource{
	argoApplicationsGVR, exposedRoutesGVR, loopzeroApplicationsGVR, environmentsGVR, tenantsGVR,
}

const (
	defaultBuildNamespace     = "argo"
	defaultFinalizerPassDelay = 3 * time.Second
	finalizerPasses           = 3
)

// ClusterAccess names one cluster: the kubeconfig file and the context in it.
// An empty Kubeconfig means "use in-cluster config".
type ClusterAccess struct {
	Kubeconfig string
	Context    string
}

// EvictorConfig is the evictor's cluster wiring. Namespaces default to the
// control-plane namespace (mgmt) and "argo" (build) when empty.
type EvictorConfig struct {
	Mgmt, Build, Runtime ClusterAccess
	MgmtNamespace        string
	BuildNamespace       string
}

// evictorClients is the resolved form of EvictorConfig; tests build it from
// fakes.
type evictorClients struct {
	mgmt, build    dynamic.Interface
	runtime        kubernetes.Interface
	mgmtNamespace  string
	buildNamespace string
}

// NewTenantK8sEvictor builds an evictor with distinct clients per cluster.
func NewTenantK8sEvictor(cfg EvictorConfig) (*TenantK8sEvictor, error) {
	mgmtCfg, err := k8s.ExplicitRestConfig(cfg.Mgmt.Kubeconfig, cfg.Mgmt.Context)
	if err != nil {
		return nil, fmt.Errorf("mgmt client: %w", err)
	}
	mgmtDyn, err := dynamic.NewForConfig(mgmtCfg)
	if err != nil {
		return nil, fmt.Errorf("mgmt client: %w", err)
	}
	buildCfg, err := k8s.ExplicitRestConfig(cfg.Build.Kubeconfig, cfg.Build.Context)
	if err != nil {
		return nil, fmt.Errorf("build client: %w", err)
	}
	buildDyn, err := dynamic.NewForConfig(buildCfg)
	if err != nil {
		return nil, fmt.Errorf("build client: %w", err)
	}
	runtimeCfg, err := k8s.ExplicitRestConfig(cfg.Runtime.Kubeconfig, cfg.Runtime.Context)
	if err != nil {
		return nil, fmt.Errorf("runtime client: %w", err)
	}
	runtimeTyped, err := kubernetes.NewForConfig(runtimeCfg)
	if err != nil {
		return nil, fmt.Errorf("runtime client: %w", err)
	}
	e := newTenantK8sEvictorFromClients(evictorClients{
		mgmt: mgmtDyn, build: buildDyn, runtime: runtimeTyped,
		mgmtNamespace: cfg.MgmtNamespace, buildNamespace: cfg.BuildNamespace,
	})
	e.finalizerPassDelay = defaultFinalizerPassDelay
	return e, nil
}

func newTenantK8sEvictorFromClients(c evictorClients) *TenantK8sEvictor {
	mgmtNS := c.mgmtNamespace
	if mgmtNS == "" {
		mgmtNS = k8s.ControlPlaneNamespace
	}
	buildNS := c.buildNamespace
	if buildNS == "" {
		buildNS = defaultBuildNamespace
	}
	return &TenantK8sEvictor{
		mgmtDynamic:    c.mgmt,
		buildDynamic:   c.build,
		runtimeTyped:   c.runtime,
		mgmtNamespace:  mgmtNS,
		buildNamespace: buildNS,
	}
}

// K8sEvictionCounts records what was purged per cluster. All fields are
// "attempted" counts — delete+force-null operations are idempotent, so a
// non-zero count doesn't imply anything was actually present beforehand.
type K8sEvictionCounts struct {
	ArgoWorkflowsDeleted int `json:"argo_workflows_deleted"`
	MgmtCRsDeleted       int `json:"mgmt_crs_deleted"`
	FinalizersCleared    int `json:"finalizers_cleared"`
	NamespacesFinalized  int `json:"namespaces_finalized"`
}

// EvictTenantsFromClusters purges K8s resources owned by the given tenant IDs.
//
// A failure on one resource does not stop the purge of the others — stuck
// resources must not block cleanup of the rest, and every operation is
// idempotent — but every such failure is collected and returned, naming the
// resource. A nil error therefore means every in-scope resource was handled;
// the caller must not proceed to DB cleanup otherwise.
//
// Order:
//  1. Delete Argo workflows in build cluster (grace-period=0)
//  2. Delete mgmt CRs (triggers operator reconcileDelete)
//  3. Force-null finalizers (3 passes) for stuck CRs
//  4. Force-finalize the tenants' runtime namespaces stuck Terminating
func (e *TenantK8sEvictor) EvictTenantsFromClusters(ctx context.Context, tenantIDs []string) (K8sEvictionCounts, error) {
	var counts K8sEvictionCounts
	if len(tenantIDs) == 0 {
		return counts, nil
	}
	scope := newTenantSet(tenantIDs)
	var failures []error

	wfCount, errs, err := e.deleteOwnedResources(ctx, e.buildDynamic, argoWorkflowGVR, e.buildNamespace, scope)
	if err != nil {
		return counts, fmt.Errorf("step 1 argo wf: %w", err)
	}
	counts.ArgoWorkflowsDeleted = wfCount
	failures = append(failures, errs...)

	for _, gvr := range mgmtCRTypes {
		n, errs, err := e.deleteOwnedResources(ctx, e.mgmtDynamic, gvr, e.mgmtNamespace, scope)
		if err != nil {
			return counts, fmt.Errorf("step 2 %s: %w", gvr.Resource, err)
		}
		counts.MgmtCRsDeleted += n
		failures = append(failures, errs...)
	}

	cleared, errs, err := e.clearFinalizersInPasses(ctx, scope)
	counts.FinalizersCleared = cleared
	if err != nil {
		return counts, err
	}
	failures = append(failures, errs...)

	nsCount, errs, err := e.forceFinalizeRuntimeNamespaces(ctx, scope)
	if err != nil {
		return counts, fmt.Errorf("step 4 namespaces: %w", err)
	}
	counts.NamespacesFinalized = nsCount
	failures = append(failures, errs...)

	if len(failures) > 0 {
		return counts, fmt.Errorf("tenant eviction incomplete (%d resource operations failed): %w",
			len(failures), errors.Join(failures...))
	}
	return counts, nil
}

// clearFinalizersInPasses runs forceNullFinalizers over every mgmt CR type
// finalizerPasses times. Only the last pass's per-resource failures are
// reported: an earlier failure the next pass cleared is not a failure.
func (e *TenantK8sEvictor) clearFinalizersInPasses(ctx context.Context, scope tenantSet) (int, []error, error) {
	cleared := 0
	var lastPass []error
	for pass := 1; pass <= finalizerPasses; pass++ {
		lastPass = nil
		for _, gvr := range mgmtCRTypes {
			n, errs, err := e.forceNullFinalizers(ctx, e.mgmtDynamic, gvr, e.mgmtNamespace, scope)
			if err != nil {
				return cleared, nil, fmt.Errorf("step 3 pass %d %s: %w", pass, gvr.Resource, err)
			}
			cleared += n
			lastPass = append(lastPass, errs...)
		}
		if pass < finalizerPasses {
			if err := e.waitBetweenPasses(ctx); err != nil {
				return cleared, nil, err
			}
		}
	}
	return cleared, lastPass, nil
}

func (e *TenantK8sEvictor) waitBetweenPasses(ctx context.Context) error {
	if e.finalizerPassDelay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(e.finalizerPassDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// tenantSet is the exact set of tenant IDs being evicted.
type tenantSet map[string]struct{}

func newTenantSet(ids []string) tenantSet {
	s := make(tenantSet, len(ids))
	for _, id := range ids {
		if id != "" {
			s[id] = struct{}{}
		}
	}
	return s
}

func (s tenantSet) owns(tenantID string) bool {
	if tenantID == "" {
		return false
	}
	_, ok := s[tenantID]
	return ok
}

// ownerOf returns the tenant ID a resource belongs to, read from the identity
// the platform writes on it: the Tenant CR is named after its tenant, an
// Environment carries spec.tenantRef.name, and every other kind carries the
// loopzero.io/tenant label. It returns "" when the resource carries no identity —
// such a resource is never evicted.
func ownerOf(gvr schema.GroupVersionResource, obj *unstructured.Unstructured) string {
	switch gvr {
	case tenantsGVR:
		return obj.GetName()
	case environmentsGVR:
		ref, _, _ := unstructured.NestedString(obj.Object, "spec", "tenantRef", "name")
		return ref
	default:
		return obj.GetLabels()[naming.LabelTenant]
	}
}

func resourceRef(gvr schema.GroupVersionResource, ns, name string) string {
	return fmt.Sprintf("%s %s/%s", gvr.Resource, ns, name)
}

// ownedResources lists gvr in ns and keeps the items owned by a tenant in
// scope. A missing resource type (CRD not installed) is an empty list.
func ownedResources(
	ctx context.Context, client dynamic.Interface, gvr schema.GroupVersionResource, ns string, scope tenantSet,
) ([]unstructured.Unstructured, error) {
	list, err := client.Resource(gvr).Namespace(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list %s: %w", gvr.Resource, err)
	}
	var owned []unstructured.Unstructured
	for _, item := range list.Items {
		if scope.owns(ownerOf(gvr, &item)) {
			owned = append(owned, item)
		}
	}
	return owned, nil
}

// deleteOwnedResources deletes each in-scope resource with grace=0. It returns
// the number of delete calls that succeeded and one error per resource that
// could not be deleted; the third return is a failure to list at all.
func (e *TenantK8sEvictor) deleteOwnedResources(
	ctx context.Context, client dynamic.Interface, gvr schema.GroupVersionResource, ns string, scope tenantSet,
) (int, []error, error) {
	owned, err := ownedResources(ctx, client, gvr, ns, scope)
	if err != nil {
		return 0, nil, err
	}
	// Ownership, declared: this is the ONE path where the api removes an ArgoCD
	// Application, and it is not spec authorship — it deletes the whole resource on
	// administrative eviction, for tenants whose operator may not be reconciling at
	// all. The operator owns `.spec` and never sees this path. ADR
	// docs/decisions.md (B-050).
	//
	// Shared ownership was considered and rejected there: declaring it would authorise
	// spec writes from the api, which do not exist today and must not start.
	zero := int64(0)
	policy := metav1.DeletePropagationBackground
	deleted := 0
	var failures []error
	for _, item := range owned {
		err := client.Resource(gvr).Namespace(ns).Delete(ctx, item.GetName(), metav1.DeleteOptions{
			GracePeriodSeconds: &zero,
			PropagationPolicy:  &policy,
		})
		if err != nil && !k8serrors.IsNotFound(err) {
			failures = append(failures, fmt.Errorf("delete %s: %w", resourceRef(gvr, ns, item.GetName()), err))
			continue
		}
		deleted++
	}
	return deleted, failures, nil
}

// forceNullFinalizers patches finalizers to null on in-scope resources that
// still carry any. Uses a JSON-merge-patch rather than strategic-merge because
// unstructured CRDs don't have strategic-merge schema registered.
func (e *TenantK8sEvictor) forceNullFinalizers(
	ctx context.Context, client dynamic.Interface, gvr schema.GroupVersionResource, ns string, scope tenantSet,
) (int, []error, error) {
	owned, err := ownedResources(ctx, client, gvr, ns, scope)
	if err != nil {
		return 0, nil, fmt.Errorf("finalizer clear: %w", err)
	}
	cleared := 0
	var failures []error
	patch := []byte(`{"metadata":{"finalizers":null}}`)
	for _, item := range owned {
		if len(item.GetFinalizers()) == 0 {
			continue
		}
		_, err := client.Resource(gvr).Namespace(ns).Patch(ctx, item.GetName(), types.MergePatchType, patch, metav1.PatchOptions{})
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				failures = append(failures, fmt.Errorf("clear finalizers on %s: %w", resourceRef(gvr, ns, item.GetName()), err))
			}
			continue
		}
		cleared++
	}
	return cleared, failures, nil
}

// forceFinalizeRuntimeNamespaces is the runtime cluster equivalent of
// `kubectl replace --raw /api/v1/namespaces/{name}/finalize`. Stuck-Terminating
// namespaces don't release until their .spec.finalizers is emptied via the
// finalize subresource — this cannot be done through the normal Patch/Update
// API. Namespaces are selected by the loopzero.io/tenant label the environment
// controller writes on them.
func (e *TenantK8sEvictor) forceFinalizeRuntimeNamespaces(ctx context.Context, scope tenantSet) (int, []error, error) {
	list, err := e.runtimeTyped.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, nil, fmt.Errorf("list namespaces: %w", err)
	}
	finalized := 0
	var failures []error
	for i := range list.Items {
		ns := &list.Items[i]
		if !scope.owns(ns.Labels[naming.LabelTenant]) {
			continue
		}
		if ns.Status.Phase != corev1.NamespaceTerminating && len(ns.Spec.Finalizers) == 0 {
			continue
		}
		ns.Spec.Finalizers = nil
		if _, err := e.runtimeTyped.CoreV1().Namespaces().Finalize(ctx, ns, metav1.UpdateOptions{}); err != nil {
			if !k8serrors.IsNotFound(err) {
				failures = append(failures, fmt.Errorf("finalize namespace %s: %w", ns.Name, err))
			}
			continue
		}
		finalized++
	}
	return finalized, failures, nil
}
