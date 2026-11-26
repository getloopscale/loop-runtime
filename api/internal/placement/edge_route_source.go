// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package placement

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// ExposedRouteGVR is the GroupVersionResource for ExposedRoute CRDs.
var ExposedRouteGVR = schema.GroupVersionResource{
	Group:    "runtime.getloopscale.com",
	Version:  "v1",
	Resource: "exposedroutes",
}

// labelTenant is the label the app operator stamps on every ExposedRoute
// (operators/internal/app/application_controller.go).
const labelTenant = "runtime.getloopscale.com/tenant"

// K8sEdgeRouteSource lists ExposedRoutes on the management cluster, where the app
// operator keeps them for every cell.
type K8sEdgeRouteSource struct {
	client    dynamic.Interface
	namespace string
}

// NewK8sEdgeRouteSource builds a source. A nil restConfig uses the in-cluster config.
func NewK8sEdgeRouteSource(restConfig *rest.Config, namespace string) (*K8sEdgeRouteSource, error) {
	if restConfig == nil {
		var err error
		restConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("k8s edge route source: in-cluster config: %w", err)
		}
	}
	client, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("k8s edge route source: create dynamic client: %w", err)
	}
	return &K8sEdgeRouteSource{client: client, namespace: namespace}, nil
}

// ListEdgeRoutes returns every route that names both a hostname and a tenant.
func (s *K8sEdgeRouteSource) ListEdgeRoutes(ctx context.Context) ([]EdgeRoute, error) {
	list, err := s.client.Resource(ExposedRouteGVR).Namespace(s.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("k8s edge route source: list ExposedRoutes: %w", err)
	}
	routes := make([]EdgeRoute, 0, len(list.Items))
	for _, item := range list.Items {
		host, _, _ := unstructured.NestedString(item.Object, "spec", "hostname")
		tenant := item.GetLabels()[labelTenant]
		if host == "" || tenant == "" {
			continue
		}
		routes = append(routes, EdgeRoute{Hostname: host, TenantID: tenant})
	}
	return routes, nil
}
