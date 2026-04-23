// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package k8s

import (
	"context"
	"errors"
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

// Compile-time check: TenantCRManager implements EnvironmentManager.
func TestTenantCRManager_ImplementsEnvironmentManager(t *testing.T) {
	var _ routes.EnvironmentManager = (*TenantCRManager)(nil)
}

func TestTenantCRManager_AddEnvironment(t *testing.T) {
	client := getDynamicClient(t)
	tenantID := "test-add-env"

	cleanupTenant(t, client, tenantID)
	t.Cleanup(func() { cleanupTenant(t, client, tenantID) })

	mgr := &TenantCRManager{Client: client}

	// Create tenant first
	err := mgr.EnsureTenant(context.Background(), tenantID, routes.TenantCRSpec{Plan: "starter"})
	if err != nil {
		t.Fatalf("EnsureTenant failed: %v", err)
	}

	// Add a new environment
	err = mgr.AddEnvironment(context.Background(), tenantID, "staging")
	if err != nil {
		t.Fatalf("AddEnvironment failed: %v", err)
	}

	// Verify the environment was added
	envs, err := mgr.ListEnvironments(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("ListEnvironments failed: %v", err)
	}

	found := false
	for _, e := range envs {
		if e.Name == "staging" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'staging' in environments, got %v", envs)
	}
}

func TestTenantCRManager_AddEnvironment_Idempotent(t *testing.T) {
	client := getDynamicClient(t)
	tenantID := "test-add-env-idem"

	cleanupTenant(t, client, tenantID)
	t.Cleanup(func() { cleanupTenant(t, client, tenantID) })

	mgr := &TenantCRManager{Client: client}

	err := mgr.EnsureTenant(context.Background(), tenantID, routes.TenantCRSpec{Plan: "starter"})
	if err != nil {
		t.Fatalf("EnsureTenant failed: %v", err)
	}

	// Add "staging" twice — second call should be a no-op
	if err := mgr.AddEnvironment(context.Background(), tenantID, "staging"); err != nil {
		t.Fatalf("first AddEnvironment failed: %v", err)
	}
	if err := mgr.AddEnvironment(context.Background(), tenantID, "staging"); err != nil {
		t.Fatalf("second AddEnvironment (idempotent) failed: %v", err)
	}

	envs, err := mgr.ListEnvironments(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("ListEnvironments failed: %v", err)
	}

	count := 0
	for _, e := range envs {
		if e.Name == "staging" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 'staging' entry, got %d in %v", count, envs)
	}
}

func TestTenantCRManager_RemoveEnvironment(t *testing.T) {
	client := getDynamicClient(t)
	tenantID := "test-rm-env"

	cleanupTenant(t, client, tenantID)
	t.Cleanup(func() { cleanupTenant(t, client, tenantID) })

	mgr := &TenantCRManager{Client: client}

	err := mgr.EnsureTenant(context.Background(), tenantID, routes.TenantCRSpec{Plan: "starter"})
	if err != nil {
		t.Fatalf("EnsureTenant failed: %v", err)
	}

	// Add staging, then remove it
	if err := mgr.AddEnvironment(context.Background(), tenantID, "staging"); err != nil {
		t.Fatalf("AddEnvironment failed: %v", err)
	}
	if err := mgr.RemoveEnvironment(context.Background(), tenantID, "staging"); err != nil {
		t.Fatalf("RemoveEnvironment failed: %v", err)
	}

	envs, err := mgr.ListEnvironments(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("ListEnvironments failed: %v", err)
	}

	for _, e := range envs {
		if e.Name == "staging" {
			t.Errorf("expected 'staging' to be removed, but it is still present in %v", envs)
		}
	}
}

// CHG-2026-04-23-AUDIT-12: the previous contract was "silent idempotent" which
// produced misleading 200 "deleted" responses for non-existing environments.
// The new contract: removing a non-existing env returns ErrEnvironmentNotFound
// so the handler can map to HTTP 404.
func TestTenantCRManager_RemoveEnvironment_NonExisting_ReturnsNotFound(t *testing.T) {
	client := getDynamicClient(t)
	tenantID := "test-rm-env-idem"

	cleanupTenant(t, client, tenantID)
	t.Cleanup(func() { cleanupTenant(t, client, tenantID) })

	mgr := &TenantCRManager{Client: client}

	err := mgr.EnsureTenant(context.Background(), tenantID, routes.TenantCRSpec{Plan: "starter"})
	if err != nil {
		t.Fatalf("EnsureTenant failed: %v", err)
	}

	// Remove a non-existing environment — must return ErrEnvironmentNotFound.
	err = mgr.RemoveEnvironment(context.Background(), tenantID, "does-not-exist")
	if err == nil {
		t.Fatalf("RemoveEnvironment (non-existing) must not succeed silently")
	}
	if !errors.Is(err, ErrEnvironmentNotFound) {
		t.Fatalf("want ErrEnvironmentNotFound, got %v", err)
	}
}

func TestTenantCRManager_RemoveEnvironment_RejectsLastEnvironment(t *testing.T) {
	client := getDynamicClient(t)
	tenantID := "test-rm-env-last"

	cleanupTenant(t, client, tenantID)
	t.Cleanup(func() { cleanupTenant(t, client, tenantID) })

	mgr := &TenantCRManager{Client: client}

	err := mgr.EnsureTenant(context.Background(), tenantID, routes.TenantCRSpec{Plan: "starter"})
	if err != nil {
		t.Fatalf("EnsureTenant failed: %v", err)
	}

	// Default tenant has only "production" — cannot remove last env
	err = mgr.RemoveEnvironment(context.Background(), tenantID, "production")
	if err == nil {
		t.Fatal("expected error when removing the last environment, got nil")
	}
}

func TestTenantCRManager_ListEnvironments(t *testing.T) {
	client := getDynamicClient(t)
	tenantID := "test-list-env"

	cleanupTenant(t, client, tenantID)
	t.Cleanup(func() { cleanupTenant(t, client, tenantID) })

	mgr := &TenantCRManager{Client: client}

	err := mgr.EnsureTenant(context.Background(), tenantID, routes.TenantCRSpec{Plan: "starter"})
	if err != nil {
		t.Fatalf("EnsureTenant failed: %v", err)
	}

	envs, err := mgr.ListEnvironments(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("ListEnvironments failed: %v", err)
	}

	// Default tenant has "production"
	if len(envs) != 1 {
		t.Fatalf("expected 1 environment, got %d: %v", len(envs), envs)
	}
	if envs[0].Name != "production" {
		t.Errorf("expected 'production', got %q", envs[0].Name)
	}
	if envs[0].Status != "active" {
		t.Errorf("expected status 'active', got %q", envs[0].Status)
	}
}

func TestTenantCRManager_GetEnvironmentDetail(t *testing.T) {
	client := getDynamicClient(t)
	tenantID := "test-get-env-detail"

	cleanupTenant(t, client, tenantID)
	t.Cleanup(func() { cleanupTenant(t, client, tenantID) })

	mgr := &TenantCRManager{Client: client}

	err := mgr.EnsureTenant(context.Background(), tenantID, routes.TenantCRSpec{Plan: "starter"})
	if err != nil {
		t.Fatalf("EnsureTenant failed: %v", err)
	}

	// Existing environment
	detail, err := mgr.GetEnvironmentDetail(context.Background(), tenantID, "production")
	if err != nil {
		t.Fatalf("GetEnvironmentDetail failed: %v", err)
	}
	if detail == nil {
		t.Fatal("expected detail for 'production', got nil")
		return // unreachable — helps staticcheck
	}
	if detail.Name != "production" {
		t.Errorf("expected name 'production', got %q", detail.Name)
	}
	if detail.Status != "active" {
		t.Errorf("expected status 'active', got %q", detail.Status)
	}

	// Non-existing environment
	detail, err = mgr.GetEnvironmentDetail(context.Background(), tenantID, "nonexistent")
	if err != nil {
		t.Fatalf("GetEnvironmentDetail (nonexistent) failed: %v", err)
	}
	if detail != nil {
		t.Errorf("expected nil for nonexistent environment, got %+v", detail)
	}
}
