// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package k8s

import (
	"context"
	"os"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/loopzero/loopzero/api/internal/routes"
)

var tenantGVR = schema.GroupVersionResource{
	Group:    "loopzero.io",
	Version:  "v1",
	Resource: "tenants",
}

// getDynamicClient returns a dynamic client for integration tests.
// It tries in-cluster config first, then falls back to kubeconfig.
// Returns nil if no cluster is available.
func getDynamicClient(t *testing.T) dynamic.Interface {
	t.Helper()

	config, err := rest.InClusterConfig()
	if err != nil {
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			home, _ := os.UserHomeDir()
			kubeconfig = home + "/.kube/config"
		}
		kctx := os.Getenv("KUBE_CONTEXT")
		if kctx == "" {
			kctx = "k3d-loopzero-mgmt"
		}

		loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}
		overrides := &clientcmd.ConfigOverrides{CurrentContext: kctx}
		config, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
		if err != nil {
			t.Skipf("skipping: no kubernetes cluster available: %v", err)
			return nil
		}
	}

	client, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Skipf("skipping: failed to create dynamic client: %v", err)
		return nil
	}
	return client
}

// cleanupTenant deletes a Tenant CR, ignoring NotFound errors.
func cleanupTenant(t *testing.T, client dynamic.Interface, name string) {
	t.Helper()
	_ = client.Resource(tenantGVR).Namespace("loopzero-system").Delete(
		context.Background(), name, metav1.DeleteOptions{},
	)
}

func TestTenantCRManager_EnsureTenant_Creates(t *testing.T) {
	client := getDynamicClient(t)
	tenantID := "test-cr-create"

	// Clean up before and after
	cleanupTenant(t, client, tenantID)
	t.Cleanup(func() { cleanupTenant(t, client, tenantID) })

	mgr := &TenantCRManager{Client: client}

	err := mgr.EnsureTenant(context.Background(), tenantID, routes.TenantCRSpec{Plan: "starter"})
	if err != nil {
		t.Fatalf("EnsureTenant failed: %v", err)
	}

	// Verify the CR was created
	obj, err := client.Resource(tenantGVR).Namespace("loopzero-system").Get(
		context.Background(), tenantID, metav1.GetOptions{},
	)
	if err != nil {
		t.Fatalf("expected Tenant CR to exist, got error: %v", err)
	}

	spec, ok := obj.Object["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("expected spec to be a map")
	}
	if plan, _ := spec["plan"].(string); plan != "starter" {
		t.Errorf("expected plan 'starter', got %q", plan)
	}
	if displayName, _ := spec["displayName"].(string); displayName != tenantID {
		t.Errorf("expected displayName %q, got %q", tenantID, displayName)
	}
}

func TestTenantCRManager_EnsureTenant_Idempotent(t *testing.T) {
	client := getDynamicClient(t)
	tenantID := "test-cr-idempotent"

	cleanupTenant(t, client, tenantID)
	t.Cleanup(func() { cleanupTenant(t, client, tenantID) })

	mgr := &TenantCRManager{Client: client}

	// First call — creates
	err := mgr.EnsureTenant(context.Background(), tenantID, routes.TenantCRSpec{Plan: "starter"})
	if err != nil {
		t.Fatalf("first EnsureTenant failed: %v", err)
	}

	// Second call — updates (should not error)
	err = mgr.EnsureTenant(context.Background(), tenantID, routes.TenantCRSpec{Plan: "starter"})
	if err != nil {
		t.Fatalf("second EnsureTenant (idempotent) failed: %v", err)
	}
}

func TestTenantCRManager_EnsureTenant_UpdatesPlan(t *testing.T) {
	client := getDynamicClient(t)
	tenantID := "test-cr-update-plan"

	cleanupTenant(t, client, tenantID)
	t.Cleanup(func() { cleanupTenant(t, client, tenantID) })

	mgr := &TenantCRManager{Client: client}

	// Create with starter
	err := mgr.EnsureTenant(context.Background(), tenantID, routes.TenantCRSpec{Plan: "starter"})
	if err != nil {
		t.Fatalf("EnsureTenant (starter) failed: %v", err)
	}

	// Update to growth
	err = mgr.EnsureTenant(context.Background(), tenantID, routes.TenantCRSpec{Plan: "growth"})
	if err != nil {
		t.Fatalf("EnsureTenant (growth) failed: %v", err)
	}

	// Verify plan was updated
	obj, err := client.Resource(tenantGVR).Namespace("loopzero-system").Get(
		context.Background(), tenantID, metav1.GetOptions{},
	)
	if err != nil {
		t.Fatalf("expected Tenant CR to exist, got error: %v", err)
	}

	spec, ok := obj.Object["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("expected spec to be a map")
	}
	if plan, _ := spec["plan"].(string); plan != "growth" {
		t.Errorf("expected plan 'growth', got %q", plan)
	}
}

func TestTenantCRManager_ImplementsInterface(t *testing.T) {
	// Compile-time check
	var _ routes.TenantCRManager = (*TenantCRManager)(nil)
}
