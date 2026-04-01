// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argocd

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var configMapGVR = schema.GroupVersionResource{
	Group:    "",
	Version:  "v1",
	Resource: "configmaps",
}

// ApplicationSetSpec defines the desired state of an ApplicationSet per Cell.
// ADR-0008 Phase 4 (P4-1).
type ApplicationSetSpec struct {
	// CellID identifies the Cell this ApplicationSet manages.
	CellID string

	// Namespace is where the ApplicationSet lives (typically "argocd").
	Namespace string

	// GiteaURL is the base URL for tenant GitOps repos.
	GiteaURL string
}

// TenantEntry represents a tenant in the Cell's tenant ConfigMap.
type TenantEntry struct {
	TenantID string `json:"tenantID" yaml:"tenantID"`
}

// UpdateCellTenantConfigMap adds or removes a tenant from the Cell's
// tenant list ConfigMap. The ApplicationSet reads this ConfigMap to
// generate Applications for each tenant-environment pair.
//
// ConfigMap name: cell-{cellID}-tenants
// Data key: "tenants" (YAML array of TenantEntry)
//
// ADR-0008 Phase 4 (P4-1).
func UpdateCellTenantConfigMap(
	ctx context.Context,
	client dynamic.Interface,
	namespace string,
	cellID string,
	tenantID string,
	add bool, // true = add tenant, false = remove tenant
) error {
	cmName := fmt.Sprintf("cell-%s-tenants", cellID)

	// Get current ConfigMap
	cm, err := client.Resource(configMapGVR).Namespace(namespace).Get(ctx, cmName, metav1.GetOptions{})
	if err != nil {
		if add {
			// Create new ConfigMap with this tenant
			return createTenantConfigMap(ctx, client, namespace, cmName, cellID, tenantID)
		}
		return nil // Remove from non-existent ConfigMap = no-op
	}

	// Parse existing tenants
	data, _ := cm.Object["data"].(map[string]interface{})
	tenantsYAML, _ := data["tenants"].(string)

	var tenants []TenantEntry
	if tenantsYAML != "" {
		if err := json.Unmarshal([]byte(tenantsYAML), &tenants); err != nil {
			// Try to recover — start fresh
			tenants = nil
		}
	}

	if add {
		// Check if already present
		for _, t := range tenants {
			if t.TenantID == tenantID {
				return nil // Already in list, idempotent
			}
		}
		tenants = append(tenants, TenantEntry{TenantID: tenantID})
	} else {
		// Remove tenant
		filtered := make([]TenantEntry, 0, len(tenants))
		for _, t := range tenants {
			if t.TenantID != tenantID {
				filtered = append(filtered, t)
			}
		}
		tenants = filtered
	}

	// Serialize and update
	tenantsJSON, err := json.Marshal(tenants)
	if err != nil {
		return fmt.Errorf("marshal tenants: %w", err)
	}

	if data == nil {
		data = make(map[string]interface{})
	}
	data["tenants"] = string(tenantsJSON)
	cm.Object["data"] = data

	_, err = client.Resource(configMapGVR).Namespace(namespace).Update(ctx, cm, metav1.UpdateOptions{})
	return err
}

func createTenantConfigMap(
	ctx context.Context,
	client dynamic.Interface,
	namespace, name, cellID, tenantID string,
) error {
	tenants := []TenantEntry{{TenantID: tenantID}}
	tenantsJSON, _ := json.Marshal(tenants)

	cm := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
				"labels": map[string]interface{}{
					"app.kubernetes.io/managed-by": "loopzero-operator",
					"loopzero.io/cell-id":              cellID,
					"app.kubernetes.io/component":  "applicationset-tenant-list",
				},
			},
			"data": map[string]interface{}{
				"tenants": string(tenantsJSON),
			},
		},
	}

	_, err := client.Resource(configMapGVR).Namespace(namespace).Create(ctx, cm, metav1.CreateOptions{})
	return err
}
