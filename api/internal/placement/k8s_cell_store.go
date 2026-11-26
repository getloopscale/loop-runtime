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

// ClusterCredentialGVR is the GroupVersionResource for ClusterCredential CRDs.
var ClusterCredentialGVR = schema.GroupVersionResource{
	Group:    "runtime.getloopscale.com",
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
// Pass nil restConfig to use in-cluster config (for pods running on
// the mgmt cluster reading ClusterCredential CRDs from themselves).
func NewK8sCellStore(restConfig *rest.Config, namespace string) (*K8sCellStore, error) {
	if restConfig == nil {
		var err error
		restConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("k8s cell store: in-cluster config: %w", err)
		}
	}
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

// extractCellConfig converts an unstructured ClusterCredential CR to CellConfig.
// MEET-053 A-01: Reads all fields from spec (not labels). Returns false if
// the CR is not connected or has no cellID.
func extractCellConfig(obj unstructured.Unstructured) (CellConfig, bool) {
	// Only include connected cells.
	connected, found, _ := unstructured.NestedBool(obj.Object, "status", "connected")
	if !found || !connected {
		return CellConfig{}, false
	}

	// CellID from spec.cellID (not from labels — MEET-053 fix B9).
	cellID, _, _ := unstructured.NestedString(obj.Object, "spec", "cellID")
	if cellID == "" {
		return CellConfig{}, false
	}

	// Connection details from spec.
	apiServer, _, _ := unstructured.NestedString(obj.Object, "spec", "apiServer")

	// Kubeconfig Secret reference from spec.secretRef.
	secretName, _, _ := unstructured.NestedString(obj.Object, "spec", "secretRef", "name")
	secretNS, _, _ := unstructured.NestedString(obj.Object, "spec", "secretRef", "namespace")

	// Ingress domain from label (operational metadata, not spec).
	ingressDomain := obj.GetLabels()["runtime.getloopscale.com/ingress-domain"]

	return CellConfig{
		CellID:             cellID,
		APIServer:          apiServer,
		IngressDomain:      ingressDomain,
		SecretRefName:      secretName,
		SecretRefNamespace: secretNS,
	}, true
}
