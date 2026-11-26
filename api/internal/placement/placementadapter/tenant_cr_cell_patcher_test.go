// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package placementadapter

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

// newFakeTenantClient seeds a Tenant CR carrying spec.cellID and the extra
// fields passed in spec.
func newFakeTenantClient(tenantID, namespace string, spec map[string]interface{}) *dynamicfake.FakeDynamicClient {
	objects := []runtime.Object{}
	if tenantID != "" {
		objects = append(objects, &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "runtime.getloopscale.com/v1",
			"kind":       "Tenant",
			"metadata":   map[string]interface{}{"name": tenantID, "namespace": namespace},
			"spec":       spec,
		}})
	}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{tenantGVR: "TenantList"}, objects...)
}

func tenantSpecField(t *testing.T, client *dynamicfake.FakeDynamicClient, namespace, tenantID, field string) string {
	t.Helper()
	got, err := client.Resource(tenantGVR).Namespace(namespace).Get(context.Background(), tenantID, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get tenant %s: %v", tenantID, err)
	}
	v, _, _ := unstructured.NestedString(got.Object, "spec", field)
	return v
}

func TestTenantCRCellPatcher_PatchesCellIDAndPreservesOtherFields(t *testing.T) {
	client := newFakeTenantClient("tn-x", "loopruntime-system",
		map[string]interface{}{"cellID": "cell-0", "displayName": "preserved-display"})
	p := NewTenantCRCellPatcher(client, "loopruntime-system")

	if err := p.PatchTenantCellID(context.Background(), "tn-x", "cell-1"); err != nil {
		t.Fatalf("PatchTenantCellID: %v", err)
	}

	if got := tenantSpecField(t, client, "loopruntime-system", "tn-x", "cellID"); got != "cell-1" {
		t.Errorf("spec.cellID = %q, want cell-1", got)
	}
	if got := tenantSpecField(t, client, "loopruntime-system", "tn-x", "displayName"); got != "preserved-display" {
		t.Errorf("a merge patch must keep spec.displayName, got %q", got)
	}
}

func TestTenantCRCellPatcher_RespectsCustomNamespace(t *testing.T) {
	client := newFakeTenantClient("tn-x", "loopruntime-prod", map[string]interface{}{"cellID": "cell-0"})
	p := NewTenantCRCellPatcher(client, "loopruntime-prod")

	if err := p.PatchTenantCellID(context.Background(), "tn-x", "cell-1"); err != nil {
		t.Fatalf("PatchTenantCellID: %v", err)
	}

	if got := tenantSpecField(t, client, "loopruntime-prod", "tn-x", "cellID"); got != "cell-1" {
		t.Errorf("spec.cellID = %q, want cell-1", got)
	}
}

func TestTenantCRCellPatcher_DefaultsToLoopRuntimeSystemNamespace(t *testing.T) {
	p := NewTenantCRCellPatcher(newFakeTenantClient("", "", nil), "")

	if p.Namespace() != "loopruntime-system" {
		t.Errorf("Namespace() = %q, want loopruntime-system", p.Namespace())
	}
}

func TestTenantCRCellPatcher_MissingTenantIsAnError(t *testing.T) {
	p := NewTenantCRCellPatcher(newFakeTenantClient("", "", nil), "loopruntime-system")

	if err := p.PatchTenantCellID(context.Background(), "tn-missing", "cell-1"); err == nil {
		t.Fatal("expected an error patching an absent Tenant CR")
	}
}
