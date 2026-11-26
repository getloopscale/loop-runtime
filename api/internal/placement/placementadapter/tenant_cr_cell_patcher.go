// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package placementadapter

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	"github.com/getloopscale/loop-runtime/api/internal/placement"
)

// tenantGVR is the GroupVersionResource for the Tenant CR. Hardcoded here
// (instead of importing operators/api/v1) so the API has no operator-package
// dependency.
var tenantGVR = schema.GroupVersionResource{
	Group:    "runtime.getloopscale.com",
	Version:  "v1",
	Resource: "tenants",
}

// TenantCRCellPatcher implements placement.TenantCRPatcher against the mgmt
// cluster: it merge-patches spec.cellID on a Tenant CR.
type TenantCRCellPatcher struct {
	client    dynamic.Interface
	namespace string
}

var _ placement.TenantCRPatcher = (*TenantCRCellPatcher)(nil)

// NewTenantCRCellPatcher patches Tenant CRs in namespace (default
// "loopruntime-system", where the tenant operator keeps them).
func NewTenantCRCellPatcher(client dynamic.Interface, namespace string) *TenantCRCellPatcher {
	if namespace == "" {
		namespace = "loopruntime-system"
	}
	return &TenantCRCellPatcher{client: client, namespace: namespace}
}

// PatchTenantCellID merge-patches spec.cellID on the named Tenant CR, leaving
// every other field untouched. Idempotent — re-pinning to the same cell is a
// no-op from the operator's perspective.
func (p *TenantCRCellPatcher) PatchTenantCellID(ctx context.Context, tenantID, cellID string) error {
	patch, err := json.Marshal(map[string]any{"spec": map[string]any{"cellID": cellID}})
	if err != nil {
		return fmt.Errorf("marshal patch: %w", err)
	}
	if _, err := p.client.Resource(tenantGVR).Namespace(p.namespace).
		Patch(ctx, tenantID, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("patch tenant %s: %w", tenantID, err)
	}
	return nil
}

// Namespace is where the patched Tenant CRs live.
func (p *TenantCRCellPatcher) Namespace() string {
	return p.namespace
}
