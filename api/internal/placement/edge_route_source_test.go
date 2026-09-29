// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package placement

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func exposedRoute(ns, name, host, tenant string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion("loopzero.io/v1")
	u.SetKind("ExposedRoute")
	u.SetNamespace(ns)
	u.SetName(name)
	if tenant != "" {
		u.SetLabels(map[string]string{"loopzero.io/tenant": tenant})
	}
	_ = unstructured.SetNestedField(u.Object, host, "spec", "hostname")
	return u
}

func newFakeRouteSource(objs ...runtime.Object) *K8sEdgeRouteSource {
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{ExposedRouteGVR: "ExposedRouteList"}, objs...)
	return &K8sEdgeRouteSource{client: client, namespace: "loopzero-system"}
}

// Each ExposedRoute contributes its hostname and the tenant label the app operator
// stamps on it; that pair is all the edge index needs.
func TestK8sEdgeRouteSourceListsHostnameAndTenant(t *testing.T) {
	src := newFakeRouteSource(
		exposedRoute("loopzero-system", "a", "tn-a-p-web-production.loopzero.local", "tn-a"),
	)
	routes, err := src.ListEdgeRoutes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0] != (EdgeRoute{Hostname: "tn-a-p-web-production.loopzero.local", TenantID: "tn-a"}) {
		t.Errorf("routes = %+v", routes)
	}
}

// A route with no tenant label or no hostname cannot be attributed to a placement,
// so it is left out rather than routed by guess.
func TestK8sEdgeRouteSourceSkipsARouteItCannotAttribute(t *testing.T) {
	src := newFakeRouteSource(
		exposedRoute("loopzero-system", "no-tenant", "x.loopzero.local", ""),
		exposedRoute("loopzero-system", "no-host", "", "tn-a"),
	)
	routes, err := src.ListEdgeRoutes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 0 {
		t.Errorf("routes = %+v, want none", routes)
	}
}

// Routes are read from the namespace the control plane keeps them in, not cluster-wide.
func TestK8sEdgeRouteSourceReadsOnlyItsNamespace(t *testing.T) {
	src := newFakeRouteSource(
		exposedRoute("elsewhere", "b", "tn-b-p-web-production.loopzero.local", "tn-b"),
	)
	routes, err := src.ListEdgeRoutes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 0 {
		t.Errorf("routes = %+v, want none from another namespace", routes)
	}
}
