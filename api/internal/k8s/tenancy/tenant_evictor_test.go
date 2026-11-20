// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenancy

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/getloopscale/loopzero/pkg/naming"
)

const (
	testMgmtNS  = "loopzero-system"
	testBuildNS = "argo"
)

var listKinds = map[schema.GroupVersionResource]string{
	argoWorkflowGVR: "WorkflowList",
	{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}:   "ApplicationList",
	{Group: "loopzero.io", Version: "v1", Resource: "exposedroutes"}:        "ExposedRouteList",
	{Group: "loopzero.io", Version: "v1", Resource: "loopzeroapplications"}: "LoopZeroApplicationList",
	{Group: "loopzero.io", Version: "v1", Resource: "environments"}:         "EnvironmentList",
	{Group: "loopzero.io", Version: "v1", Resource: "tenants"}:              "TenantList",
}

func labelled(apiVersion, kind, ns, name, tenant string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion(apiVersion)
	u.SetKind(kind)
	u.SetNamespace(ns)
	u.SetName(name)
	u.SetLabels(map[string]string{naming.LabelTenant: tenant})
	u.SetFinalizers([]string{"loopzero.io/finalizer"})
	return u
}

func tenantCR(name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion("loopzero.io/v1")
	u.SetKind("Tenant")
	u.SetNamespace(testMgmtNS)
	u.SetName(name)
	return u
}

func environmentCR(name, tenant string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion("loopzero.io/v1")
	u.SetKind("Environment")
	u.SetNamespace(testMgmtNS)
	u.SetName(name)
	_ = unstructured.SetNestedField(u.Object, tenant, "spec", "tenantRef", "name")
	return u
}

// objectsFor seeds one resource of every kind the evictor purges, all owned
// by tenantID and all named with the tenant ID as a prefix.
func objectsFor(tenantID string) []runtime.Object {
	return []runtime.Object{
		labelled("argoproj.io/v1alpha1", "Workflow", testBuildNS, tenantID+"-build-1", tenantID),
		labelled("argoproj.io/v1alpha1", "Application", testMgmtNS, tenantID+"-web-production", tenantID),
		labelled("loopzero.io/v1", "ExposedRoute", testMgmtNS, tenantID+"-web-route", tenantID),
		labelled("loopzero.io/v1", "LoopZeroApplication", testMgmtNS, tenantID+"-web-production", tenantID),
		environmentCR(tenantID+"-web-production", tenantID),
		tenantCR(tenantID),
	}
}

func terminatingNamespace(name, tenant string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{naming.LabelTenant: tenant}},
		Spec:       corev1.NamespaceSpec{Finalizers: []corev1.FinalizerName{corev1.FinalizerKubernetes}},
		Status:     corev1.NamespaceStatus{Phase: corev1.NamespaceTerminating},
	}
}

type evictorFixture struct {
	evictor *TenantK8sEvictor
	mgmt    *dynamicfake.FakeDynamicClient
	build   *dynamicfake.FakeDynamicClient
	runtime *kubefake.Clientset
}

func newEvictorFixture(tenants ...string) evictorFixture {
	var objs []runtime.Object
	var namespaces []runtime.Object
	for _, id := range tenants {
		objs = append(objs, objectsFor(id)...)
		namespaces = append(namespaces, terminatingNamespace("loopzero-"+id+"-web-production", id))
	}
	scheme := runtime.NewScheme()
	mgmt := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, listKinds, objs...)
	build := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, listKinds, objs...)
	rt := kubefake.NewClientset(namespaces...)
	return evictorFixture{
		evictor: newTenantK8sEvictorFromClients(evictorClients{
			mgmt: mgmt, build: build, runtime: rt,
			mgmtNamespace: testMgmtNS, buildNamespace: testBuildNS,
		}),
		mgmt: mgmt, build: build, runtime: rt,
	}
}

func remainingNames(t *testing.T, f evictorFixture) []string {
	t.Helper()
	ctx := context.Background()
	var names []string
	for gvr := range listKinds {
		client := f.mgmt
		ns := testMgmtNS
		if gvr == argoWorkflowGVR {
			client, ns = f.build, testBuildNS
		}
		list, err := client.Resource(gvr).Namespace(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			t.Fatalf("list %s: %v", gvr.Resource, err)
		}
		for _, item := range list.Items {
			names = append(names, gvr.Resource+"/"+item.GetName())
		}
	}
	return names
}

func TestEvictionLeavesTenantsWhoseIDExtendsTheEvictedOne(t *testing.T) {
	// Arrange: "tn-a" is evicted; "tn-a-b" and "tn-ab" are live tenants whose
	// IDs start with "tn-a" — a name-prefix match would purge them too.
	f := newEvictorFixture("tn-a", "tn-a-b", "tn-ab")

	// Act
	if _, err := f.evictor.EvictTenantsFromClusters(context.Background(), []string{"tn-a"}); err != nil {
		t.Fatalf("EvictTenantsFromClusters: %v", err)
	}

	// Assert: every resource of the other two tenants survives, none of tn-a's.
	remaining := remainingNames(t, f)
	for _, name := range remaining {
		base := name[strings.Index(name, "/")+1:]
		if !strings.HasPrefix(base, "tn-a-b") && !strings.HasPrefix(base, "tn-ab") {
			t.Errorf("resource of evicted tenant tn-a survived: %s", name)
		}
	}
	if want := 2 * len(listKinds); len(remaining) != want {
		t.Errorf("remaining resources = %d (%v), want %d — another tenant's resources were purged", len(remaining), remaining, want)
	}
}

func TestEvictionFinalizesOnlyTheEvictedTenantsNamespaces(t *testing.T) {
	// Arrange
	f := newEvictorFixture("tn-a", "tn-ab")
	var finalized []string
	// The fake clientset models the finalize subresource as a "create" action.
	f.runtime.PrependReactor("create", "namespaces", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "finalize" {
			return false, nil, nil
		}
		ns := action.(k8stesting.CreateAction).GetObject().(*corev1.Namespace)
		finalized = append(finalized, ns.Name)
		return true, ns, nil
	})

	// Act
	counts, err := f.evictor.EvictTenantsFromClusters(context.Background(), []string{"tn-a"})

	// Assert
	if err != nil {
		t.Fatalf("EvictTenantsFromClusters: %v", err)
	}
	if len(finalized) != 1 || finalized[0] != "loopzero-tn-a-web-production" {
		t.Fatalf("finalized namespaces = %v, want only loopzero-tn-a-web-production", finalized)
	}
	if counts.NamespacesFinalized != 1 {
		t.Errorf("NamespacesFinalized = %d, want 1", counts.NamespacesFinalized)
	}
}

func TestEvictionReportsAResourceItCouldNotDelete(t *testing.T) {
	// Arrange: the API server refuses to delete one workflow.
	f := newEvictorFixture("tn-a")
	f.build.PrependReactor("delete", "workflows", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("admission webhook denied the request")
	})

	// Act
	counts, err := f.evictor.EvictTenantsFromClusters(context.Background(), []string{"tn-a"})

	// Assert: the failure is returned, named, and the rest of the purge still ran.
	if err == nil {
		t.Fatal("EvictTenantsFromClusters returned nil although a workflow could not be deleted")
	}
	if !strings.Contains(err.Error(), "tn-a-build-1") {
		t.Errorf("error %q does not name the resource that failed", err)
	}
	if counts.ArgoWorkflowsDeleted != 0 {
		t.Errorf("ArgoWorkflowsDeleted = %d, want 0", counts.ArgoWorkflowsDeleted)
	}
	if counts.MgmtCRsDeleted != 5 {
		t.Errorf("MgmtCRsDeleted = %d, want 5 (a failure on one resource must not stop the others)", counts.MgmtCRsDeleted)
	}
}

func TestOwnerOfReadsTheIdentityEachKindCarries(t *testing.T) {
	tests := []struct {
		name string
		gvr  schema.GroupVersionResource
		obj  *unstructured.Unstructured
		want string
	}{
		{"tenant CR is owned by its name", tenantsGVR, tenantCR("tn-a"), "tn-a"},
		{"environment CR is owned by spec.tenantRef", environmentsGVR, environmentCR("tn-a-web-prod", "tn-a"), "tn-a"},
		{"labelled kinds are owned by loopzero.io/tenant", argoWorkflowGVR, labelled("argoproj.io/v1alpha1", "Workflow", testBuildNS, "anything", "tn-z"), "tn-z"},
		{"unlabelled resource has no owner", argoWorkflowGVR, tenantCR("tn-a"), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ownerOf(tc.gvr, tc.obj); got != tc.want {
				t.Fatalf("ownerOf = %q, want %q", got, tc.want)
			}
		})
	}
}
