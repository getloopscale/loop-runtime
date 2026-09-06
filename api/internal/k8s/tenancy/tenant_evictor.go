// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenancy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// TenantK8sEvictor performs the K8s cluster teardown portion of bulk tenant
// eviction (T-02 phase 2). It replaces steps 1-4 of tools/admin-evict-tenants.sh:
//
//	step 1: delete Argo workflows in the build cluster
//	step 2: delete mgmt CRs (tenants, environments, loopzeroapplications,
//	        exposedroutes, applications.argoproj.io)
//	step 3: force-null finalizers on stuck mgmt CRs (3-pass retry)
//	step 4: force-finalize runtime namespaces (loopzero-{tenantID}*)
//
// Step 5 (DB cleanup) remains in db.PostgresTenantStore.DeleteTenantsFKOrdered.
//
// The evictor is constructed with three kubeconfigs (mgmt, build, runtime)
// so it works both in-cluster (where mgmt pod mounts all three as Secret
// projections) and in local dev (where kubectl contexts differ).
type TenantK8sEvictor struct {
	mgmtDynamic    dynamic.Interface
	buildDynamic   dynamic.Interface
	runtimeTyped   kubernetes.Interface
	runtimeDynamic dynamic.Interface
	mgmtNamespace  string
	buildNamespace string
}

// Resource types this evictor knows how to purge. Listed in deletion order:
// ArgoCD Applications first (they reference the other CRs), then the loopzero.io
// CRs. applications.argoproj.io uses a different group, kept last.
var mgmtCRTypes = []schema.GroupVersionResource{
	{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"},
	{Group: "loopzero.io", Version: "v1", Resource: "exposedroutes"},
	{Group: "loopzero.io", Version: "v1", Resource: "loopzeroapplications"},
	{Group: "loopzero.io", Version: "v1", Resource: "environments"},
	{Group: "loopzero.io", Version: "v1", Resource: "tenants"},
}

var argoWorkflowGVR = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "workflows",
}

// NewTenantK8sEvictor builds an evictor with distinct clients per cluster.
// An empty kubeconfig means "use in-cluster config" — the mgmt pod mounts
// its own service account; build + runtime require projected kubeconfigs.
func NewTenantK8sEvictor(
	mgmtKubeconfig, mgmtContext string,
	buildKubeconfig, buildContext string,
	cell0Kubeconfig, runtimeContext string,
	mgmtNamespace, buildNamespace string,
) (*TenantK8sEvictor, error) {
	mgmtDyn, err := newDynamicClient(mgmtKubeconfig, mgmtContext)
	if err != nil {
		return nil, fmt.Errorf("mgmt client: %w", err)
	}
	buildDyn, err := newDynamicClient(buildKubeconfig, buildContext)
	if err != nil {
		return nil, fmt.Errorf("build client: %w", err)
	}
	runtimeTyped, runtimeDyn, err := newDualClient(cell0Kubeconfig, runtimeContext)
	if err != nil {
		return nil, fmt.Errorf("runtime client: %w", err)
	}
	if mgmtNamespace == "" {
		mgmtNamespace = "loopzero-system"
	}
	if buildNamespace == "" {
		buildNamespace = "argo"
	}
	return &TenantK8sEvictor{
		mgmtDynamic:    mgmtDyn,
		buildDynamic:   buildDyn,
		runtimeTyped:   runtimeTyped,
		runtimeDynamic: runtimeDyn,
		mgmtNamespace:  mgmtNamespace,
		buildNamespace: buildNamespace,
	}, nil
}

func newDynamicClient(kubeconfig, kubeContext string) (dynamic.Interface, error) {
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig},
		&clientcmd.ConfigOverrides{CurrentContext: kubeContext},
	).ClientConfig()
	if err != nil {
		return nil, err
	}
	return dynamic.NewForConfig(cfg)
}

func newDualClient(kubeconfig, kubeContext string) (kubernetes.Interface, dynamic.Interface, error) {
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig},
		&clientcmd.ConfigOverrides{CurrentContext: kubeContext},
	).ClientConfig()
	if err != nil {
		return nil, nil, err
	}
	typed, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, nil, err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, nil, err
	}
	return typed, dyn, nil
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

// EvictTenantsFromClusters purges K8s resources for the given tenant IDs.
// Mirrors steps 1-4 of tools/admin-evict-tenants.sh. The tenantIDs list is
// used as an exact-match prefix filter — all resources whose name has any
// tenantID as prefix are considered in scope.
//
// The method is idempotent and safe to re-run. Errors in individual resources
// are logged but do not abort the overall eviction (the bash script uses
// `|| true` liberally for the same reason — stuck resources shouldn't block
// cleanup of the rest).
//
// Order:
//  1. Delete Argo workflows in build cluster (grace-period=0)
//  2. Delete mgmt CRs (triggers operator reconcileDelete)
//  3. Force-null finalizers (3 passes, 3s between) for stuck CRs
//  4. Force-finalize runtime namespaces stuck Terminating
func (e *TenantK8sEvictor) EvictTenantsFromClusters(ctx context.Context, tenantIDs []string) (K8sEvictionCounts, error) {
	var counts K8sEvictionCounts
	if len(tenantIDs) == 0 {
		return counts, nil
	}

	// Step 1: Argo workflows (build cluster).
	wfCount, err := e.deleteMatchingResources(ctx, e.buildDynamic, argoWorkflowGVR, e.buildNamespace, tenantIDs)
	if err != nil {
		return counts, fmt.Errorf("step 1 argo wf: %w", err)
	}
	counts.ArgoWorkflowsDeleted = wfCount

	// Step 2: mgmt CRs.
	for _, gvr := range mgmtCRTypes {
		n, err := e.deleteMatchingResources(ctx, e.mgmtDynamic, gvr, e.mgmtNamespace, tenantIDs)
		if err != nil {
			return counts, fmt.Errorf("step 2 %s: %w", gvr.Resource, err)
		}
		counts.MgmtCRsDeleted += n
	}

	// Step 3: force-null finalizers (3 passes).
	for i := 0; i < 3; i++ {
		for _, gvr := range mgmtCRTypes {
			n, err := e.forceNullFinalizers(ctx, e.mgmtDynamic, gvr, e.mgmtNamespace, tenantIDs)
			if err != nil {
				return counts, fmt.Errorf("step 3 pass %d %s: %w", i+1, gvr.Resource, err)
			}
			counts.FinalizersCleared += n
		}
		if i < 2 {
			select {
			case <-time.After(3 * time.Second):
			case <-ctx.Done():
				return counts, ctx.Err()
			}
		}
	}

	// Step 4: force-finalize runtime namespaces.
	nsCount, err := e.forceFinalizeRuntimeNamespaces(ctx, tenantIDs)
	if err != nil {
		return counts, fmt.Errorf("step 4 namespaces: %w", err)
	}
	counts.NamespacesFinalized = nsCount

	return counts, nil
}

// hasTenantPrefix returns true if name starts with any tenantID prefix.
// Matches the bash script's `grep "$RES_PREFIX"` behavior (prefix match on
// resource name, where resource names typically embed the tenant ID).
func hasTenantPrefix(name string, tenantIDs []string) bool {
	for _, id := range tenantIDs {
		if strings.HasPrefix(name, id) {
			return true
		}
	}
	return false
}

// runtimeNamespaceMatches returns true if a namespace name matches the
// runtime convention `loopzero-{tenantID}...`. Runtime per-env namespaces are
// named after the tenant ID + environment, so a tenant prefix check works.
func runtimeNamespaceMatches(ns string, tenantIDs []string) bool {
	const prefix = "loopzero-"
	if !strings.HasPrefix(ns, prefix) {
		return false
	}
	return hasTenantPrefix(strings.TrimPrefix(ns, prefix), tenantIDs)
}

// deleteMatchingResources lists resources in ns, filters by tenant prefix,
// and deletes each with grace=0. Returns count of delete calls issued (not
// rows actually gone — K8s may keep finalizer-blocked objects around).
func (e *TenantK8sEvictor) deleteMatchingResources(
	ctx context.Context, client dynamic.Interface, gvr schema.GroupVersionResource, ns string, tenantIDs []string,
) (int, error) {
	list, err := client.Resource(gvr).Namespace(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("list %s: %w", gvr.Resource, err)
	}
	// Ownership, declared: this is the ONE path where the api removes an ArgoCD
	// Application, and it is not spec authorship — it deletes the whole resource on
	// administrative eviction, for tenants whose operator may not be reconciling at
	// all. The operator owns `.spec` and never sees this path. ADR
	// docs/wiki/decisions/2026-08-12-argocd-application-ownership.md (B-050).
	//
	// Shared ownership was considered and rejected there: declaring it would authorise
	// spec writes from the api, which do not exist today and must not start.
	zero := int64(0)
	policy := metav1.DeletePropagationBackground
	deleted := 0
	for _, item := range list.Items {
		name := item.GetName()
		if !hasTenantPrefix(name, tenantIDs) {
			continue
		}
		err := client.Resource(gvr).Namespace(ns).Delete(ctx, name, metav1.DeleteOptions{
			GracePeriodSeconds: &zero,
			PropagationPolicy:  &policy,
		})
		if err != nil && !k8serrors.IsNotFound(err) {
			// Don't abort — mirror bash's `|| true`. Finalizer-blocked
			// objects come back in the next pass via forceNullFinalizers.
			continue
		}
		deleted++
	}
	return deleted, nil
}

// forceNullFinalizers patches finalizers to [] on stuck resources. Uses a
// JSON-merge-patch rather than strategic-merge because unstructured CRDs
// don't have strategic-merge schema registered.
func (e *TenantK8sEvictor) forceNullFinalizers(
	ctx context.Context, client dynamic.Interface, gvr schema.GroupVersionResource, ns string, tenantIDs []string,
) (int, error) {
	list, err := client.Resource(gvr).Namespace(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("list for finalizer clear %s: %w", gvr.Resource, err)
	}
	cleared := 0
	patch := []byte(`{"metadata":{"finalizers":null}}`)
	for _, item := range list.Items {
		name := item.GetName()
		if !hasTenantPrefix(name, tenantIDs) {
			continue
		}
		finalizers := item.GetFinalizers()
		if len(finalizers) == 0 {
			continue
		}
		_, err := client.Resource(gvr).Namespace(ns).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
		if err == nil {
			cleared++
		}
	}
	return cleared, nil
}

// forceFinalizeRuntimeNamespaces is the runtime cluster equivalent of
// `kubectl replace --raw /api/v1/namespaces/{name}/finalize` from the bash
// script. Stuck-Terminating namespaces don't release until their .spec.finalizers
// is emptied via the finalize subresource — this cannot be done through the
// normal Patch/Update API.
func (e *TenantK8sEvictor) forceFinalizeRuntimeNamespaces(ctx context.Context, tenantIDs []string) (int, error) {
	list, err := e.runtimeTyped.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("list namespaces: %w", err)
	}
	finalized := 0
	for _, ns := range list.Items {
		if !runtimeNamespaceMatches(ns.Name, tenantIDs) {
			continue
		}
		if ns.Status.Phase != "Terminating" && len(ns.Spec.Finalizers) == 0 {
			// Namespace is either healthy or already done — skip.
			continue
		}
		// Fetch current state + clear spec.finalizers via the /finalize
		// subresource. Use a raw PUT so we hit the correct endpoint.
		current, err := e.runtimeTyped.CoreV1().Namespaces().Get(ctx, ns.Name, metav1.GetOptions{})
		if err != nil {
			continue
		}
		current.Spec.Finalizers = nil
		body, err := json.Marshal(current)
		if err != nil {
			continue
		}
		_, err = e.runtimeTyped.CoreV1().RESTClient().Put().
			AbsPath("/api/v1/namespaces", ns.Name, "finalize").
			Body(body).
			DoRaw(ctx)
		if err == nil {
			finalized++
		}
	}
	return finalized, nil
}
