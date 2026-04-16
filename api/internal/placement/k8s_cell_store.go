// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package placement

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// ClusterCredentialGVR is the GroupVersionResource for ClusterCredential CRDs.
var ClusterCredentialGVR = schema.GroupVersionResource{
	Group:    "platform.loopzero.io",
	Version:  "v1",
	Resource: "clustercredentials",
}

// K8sCellStore reads CellConfigs from ClusterCredential CRDs on the management cluster.
// Each ClusterCredential with status.connected=true becomes a Cell.
type K8sCellStore struct {
	client    dynamic.Interface
	namespace string // namespace where ClusterCredentials live
}

// NewK8sCellStore creates a CellStore backed by the K8s API.
// Pass nil restConfig to use in-cluster config.
func NewK8sCellStore(restConfig *rest.Config, namespace string) (*K8sCellStore, error) {
	client, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("k8s cell store: create dynamic client: %w", err)
	}
	return &K8sCellStore{
		client:    client,
		namespace: namespace,
	}, nil
}

// LoadCells reads all ClusterCredential CRDs and returns connected cells as CellConfigs.
func (s *K8sCellStore) LoadCells(ctx context.Context) ([]CellConfig, error) {
	list, err := s.client.Resource(ClusterCredentialGVR).Namespace(s.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("k8s cell store: list ClusterCredentials: %w", err)
	}

	var cells []CellConfig
	for _, item := range list.Items {
		cell, ok := extractCellConfig(item)
		if ok {
			cells = append(cells, cell)
		}
	}
	return cells, nil
}

func extractCellConfig(obj unstructured.Unstructured) (CellConfig, bool) {
	// Check status.connected
	connected, found, _ := unstructured.NestedBool(obj.Object, "status", "connected")
	if !found || !connected {
		return CellConfig{}, false
	}

	// Extract spec fields
	clusterName, _, _ := unstructured.NestedString(obj.Object, "spec", "clusterName")
	apiServer, _, _ := unstructured.NestedString(obj.Object, "spec", "apiServer")

	// Extract labels for cell metadata
	labels := obj.GetLabels()
	cellID := labels["loopzero.io/cell-id"]
	if cellID == "" {
		cellID = clusterName // fallback: use cluster name as cell ID
	}

	ingressDomain := labels["loopzero.io/ingress-domain"]

	// Extract ArgoCD scope
	scopeData, _, _ := unstructured.NestedMap(obj.Object, "spec", "scope")
	argocdEnabled := false
	if scopeData != nil {
		if v, ok := scopeData["argoCD"]; ok {
			argocdEnabled, _ = v.(bool)
		}
	}

	_ = apiServer
	_ = argocdEnabled
	_ = json.Marshal // prevent unused import

	return CellConfig{
		CellID:        cellID,
		IngressDomain: ingressDomain,
	}, true
}
