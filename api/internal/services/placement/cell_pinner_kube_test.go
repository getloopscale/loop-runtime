// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// CHG-2026-04-30-DELETE-SINGLE-CELL — coverage for CellPinner.WithKubeClient
// + patchTenantCR path. Production wiring (api/cmd/setup_server.go) injects
// a real dynamic.Interface; these tests use the dynamic-fake builder to
// exercise the patch path without a real K8s cluster.

package placement

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

// metav1Get/Update kept as small helpers so the test file's literal
// metav1.GetOptions{}/UpdateOptions{} doesn't make every line noisy.
func metav1Get() metav1.GetOptions       { return metav1.GetOptions{} }
func metav1Update() metav1.UpdateOptions { return metav1.UpdateOptions{} }

// newFakeTenantClient builds a dynamic.Interface seeded with a Tenant CR
// for the given tenantID + cellID. The fake supports PATCH, GET, and
// LIST on the loopzero.io/v1/tenants resource via the unstructured tracker.
func newFakeTenantClient(t *testing.T, tenantID, cellID, namespace string) *dynamicfake.FakeDynamicClient {
	t.Helper()
	scheme := runtime.NewScheme()
	gvrToListKind := map[schema.GroupVersionResource]string{
		tenantGVR: "TenantList",
	}
	tenant := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "loopzero.io/v1",
			"kind":       "Tenant",
			"metadata": map[string]interface{}{
				"name":      tenantID,
				"namespace": namespace,
			},
			"spec": map[string]interface{}{
				"cellID": cellID,
			},
		},
	}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, gvrToListKind, tenant)
}

// readCellID retrieves the current cellID from the Tenant CR via the
// fake client. Returns "" when the tenant is missing.
func readCellID(t *testing.T, client *dynamicfake.FakeDynamicClient, tenantID, namespace string) string {
	t.Helper()
	got, err := client.Resource(tenantGVR).Namespace(namespace).Get(context.Background(), tenantID, metav1Get())
	if err != nil {
		t.Fatalf("get tenant %s: %v", tenantID, err)
	}
	cid, _, _ := unstructured.NestedString(got.Object, "spec", "cellID")
	return cid
}

// =============================================================================
// Tests
// =============================================================================

func TestCellPinner_WithKubeClient_NilClientReturnsReceiverUnchanged(t *testing.T) {
	store := &mockPinnerStore{prevCell: "cell-0"}
	p := NewCellPinner(store)
	cp := p.WithKubeClient(nil, "")
	if cp == nil {
		t.Fatal("WithKubeClient(nil, ...) must not return nil")
	}
	// Ensure the no-client path still works (DB only, no patch attempt).
	prev, err := cp.Pin(context.Background(), "tn-x", "cell-1")
	if err != nil {
		t.Fatalf("Pin: %v", err)
	}
	if prev != "cell-0" {
		t.Errorf("expected prev=cell-0, got %q", prev)
	}
}

func TestCellPinner_WithKubeClient_PatchesCROnSuccess(t *testing.T) {
	store := &mockPinnerStore{prevCell: "cell-0"}
	client := newFakeTenantClient(t, "tn-acme", "cell-0", "loopzero-system")
	p := NewCellPinner(store).WithKubeClient(client, "loopzero-system")

	prev, err := p.Pin(context.Background(), "tn-acme", "cell-1")
	if err != nil {
		t.Fatalf("Pin: %v", err)
	}
	if prev != "cell-0" {
		t.Errorf("expected prev=cell-0, got %q", prev)
	}
	if got := readCellID(t, client, "tn-acme", "loopzero-system"); got != "cell-1" {
		t.Errorf("expected CR spec.cellID=cell-1 after patch, got %q", got)
	}
}

func TestCellPinner_WithKubeClient_StoreErrorSkipsCRPatch(t *testing.T) {
	store := &mockPinnerStore{err: errors.New("db down")}
	client := newFakeTenantClient(t, "tn-acme", "cell-0", "loopzero-system")
	p := NewCellPinner(store).WithKubeClient(client, "loopzero-system")

	_, err := p.Pin(context.Background(), "tn-acme", "cell-1")
	if err == nil {
		t.Fatal("expected error from store")
	}
	// CR must remain at cell-0 (no patch attempted when store fails).
	if got := readCellID(t, client, "tn-acme", "loopzero-system"); got != "cell-0" {
		t.Errorf("CR was modified despite store error: %q", got)
	}
}

// emptyTenantClient has no Tenant CR, so every patch fails with not-found.
func emptyTenantClient() *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(),
		map[schema.GroupVersionResource]string{tenantGVR: "TenantList"})
}

func noBackoff(int) time.Duration { return 0 }

func TestCellPinner_CRPatchFailure_RevertsTheDatabaseAndReportsNotApplied(t *testing.T) {
	store := &mockPinnerStore{prevCell: "cell-0"}
	p := NewCellPinner(store).WithKubeClient(emptyTenantClient(), "loopzero-system")
	p.patchBackoff = noBackoff

	_, err := p.Pin(context.Background(), "tn-missing", "cell-1")

	if !errors.Is(err, ErrCellPinNotApplied) {
		t.Fatalf("want ErrCellPinNotApplied, got %v", err)
	}
	if want := []string{"cell-1", "cell-0"}; strings.Join(store.pinnedTo, ",") != strings.Join(want, ",") {
		t.Errorf("store pins = %v, want the move then its reversal %v", store.pinnedTo, want)
	}
}

func TestCellPinner_CRPatchFailure_WithFailedRevert_ReportsDivergence(t *testing.T) {
	store := &mockPinnerStore{prevCell: "cell-0", errOnCell: map[string]error{"cell-0": ErrCellNotPinnable}}
	p := NewCellPinner(store).WithKubeClient(emptyTenantClient(), "loopzero-system")
	p.patchBackoff = noBackoff

	_, err := p.Pin(context.Background(), "tn-missing", "cell-1")

	if !errors.Is(err, ErrCellPinDiverged) {
		t.Fatalf("want ErrCellPinDiverged, got %v", err)
	}
	if !strings.Contains(err.Error(), "loopzero-tenant-cr-shape") {
		t.Errorf("a divergence must carry the repair hint; got %v", err)
	}
}

func TestCellPinner_CRPatchFailure_WithNoPreviousCell_ReportsDivergence(t *testing.T) {
	store := &mockPinnerStore{prevCell: ""}
	p := NewCellPinner(store).WithKubeClient(emptyTenantClient(), "loopzero-system")
	p.patchBackoff = noBackoff

	_, err := p.Pin(context.Background(), "tn-missing", "cell-1")

	if !errors.Is(err, ErrCellPinDiverged) {
		t.Fatalf("a tenant with no previous cell cannot be reverted; want ErrCellPinDiverged, got %v", err)
	}
	if len(store.pinnedTo) != 1 {
		t.Errorf("no revert may be attempted without a previous cell; store pins = %v", store.pinnedTo)
	}
}

func TestCellPinner_WithKubeClient_RespectsCustomNamespace(t *testing.T) {
	store := &mockPinnerStore{prevCell: "cell-0"}
	client := newFakeTenantClient(t, "tn-x", "cell-0", "loopzero-prod")
	p := NewCellPinner(store).WithKubeClient(client, "loopzero-prod")
	if _, err := p.Pin(context.Background(), "tn-x", "cell-1"); err != nil {
		t.Fatalf("Pin: %v", err)
	}
	if got := readCellID(t, client, "tn-x", "loopzero-prod"); got != "cell-1" {
		t.Errorf("custom namespace patch not applied; got %q", got)
	}
}

func TestCellPinner_WithKubeClient_DefaultNamespaceWhenEmpty(t *testing.T) {
	store := &mockPinnerStore{prevCell: "cell-0"}
	client := newFakeTenantClient(t, "tn-x", "cell-0", "loopzero-system")
	p := NewCellPinner(store).WithKubeClient(client, "")
	if _, err := p.Pin(context.Background(), "tn-x", "cell-1"); err != nil {
		t.Fatalf("Pin: %v", err)
	}
	if got := readCellID(t, client, "tn-x", "loopzero-system"); got != "cell-1" {
		t.Errorf("default namespace not honored; got %q", got)
	}
}

func TestCellPinner_PatchPayloadIsMergePatch(t *testing.T) {
	// Verifies the patch is a strategic merge (not full replacement) so
	// other CR fields (annotations, labels, status) are preserved.
	store := &mockPinnerStore{prevCell: "cell-0"}
	client := newFakeTenantClient(t, "tn-x", "cell-0", "loopzero-system")

	// Pre-populate the CR with extra fields that must survive the patch.
	got, _ := client.Resource(tenantGVR).Namespace("loopzero-system").Get(context.Background(), "tn-x", metav1Get())
	if err := unstructured.SetNestedField(got.Object, "preserved-display", "spec", "displayName"); err != nil {
		t.Fatalf("set displayName: %v", err)
	}
	if _, err := client.Resource(tenantGVR).Namespace("loopzero-system").Update(context.Background(), got, metav1Update()); err != nil {
		t.Fatalf("update CR with extra field: %v", err)
	}

	p := NewCellPinner(store).WithKubeClient(client, "loopzero-system")
	if _, err := p.Pin(context.Background(), "tn-x", "cell-1"); err != nil {
		t.Fatalf("Pin: %v", err)
	}
	after, _ := client.Resource(tenantGVR).Namespace("loopzero-system").Get(context.Background(), "tn-x", metav1Get())
	displayName, _, _ := unstructured.NestedString(after.Object, "spec", "displayName")
	cellID, _, _ := unstructured.NestedString(after.Object, "spec", "cellID")
	if displayName != "preserved-display" {
		t.Errorf("MergePatch dropped spec.displayName (was: %q, expected preserved)", displayName)
	}
	if cellID != "cell-1" {
		t.Errorf("expected cellID=cell-1 after merge patch, got %q", cellID)
	}
	if !strings.Contains(cellID, "cell") {
		t.Errorf("unexpected cellID format: %q", cellID)
	}
}
