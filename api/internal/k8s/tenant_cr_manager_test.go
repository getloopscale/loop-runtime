// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package k8s

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/loopzero/loopzero/api/internal/domain"
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
// Skips when -short is set (these are integration tests requiring a live cluster).
func getDynamicClient(t *testing.T) dynamic.Interface {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping: integration test requires a live Kubernetes cluster (-short flag set)")
	}

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

// cleanupTenant deletes a Tenant CR and BLOCKS until the apiserver
// returns NotFound. Two failure modes are handled:
//
//  1. Async deletion: K8s Delete returns immediately but the object
//     stays in etcd until finalizers run. Consecutive test runs that
//     reuse the same name catch the CR mid-deletion and EnsureTenant
//     takes the wrong branch (Update on a terminating object), then
//     AddEnvironment Get → NotFound.
//
//  2. Stuck finalizer: the Tenant CRD declares finalizer
//     `loopzero.io/tenant-cleanup` which is owned by the LoopZero Operator.
//     Test environments (k3d local) typically run WITHOUT the operator,
//     so deletions never complete. We patch the finalizers list to
//     empty so the apiserver removes the object immediately.
//
// Combined: Delete first (graceful — operator-aware envs honor it),
// then patch finalizers if it's still around, then poll for NotFound.
func cleanupTenant(t *testing.T, client dynamic.Interface, name string) {
	t.Helper()
	ctx := context.Background()
	res := client.Resource(tenantGVR).Namespace("loopzero-system")

	_ = res.Delete(ctx, name, metav1.DeleteOptions{})

	// If finalizers block deletion, strip them via JSON merge patch.
	// Idempotent: NotFound here means delete already completed.
	patch := []byte(`{"metadata":{"finalizers":null}}`)
	_, _ = res.Patch(ctx, name, k8stypes.MergePatchType, patch, metav1.PatchOptions{})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, err := res.Get(ctx, name, metav1.GetOptions{})
		if k8serrors.IsNotFound(err) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Logf("cleanupTenant: %q not deleted within 5s — apiserver may be slow", name)
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

	// Add a new environment. T5.1 of plan 2026-06-10-j22-layer2-code-review-fixes:
	// post-M2-canonical-fix the CRD requires non-empty `spec.environments[].project`
	// (operators/api/v1/tenant_types.go EC-1); use canonical test project.
	err = mgr.AddEnvironment(context.Background(), tenantID, "testproj", "staging")
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

	// Add "staging" twice — second call should be a no-op. T5.1: canonical test project per M2 CRD invariant.
	if err := mgr.AddEnvironment(context.Background(), tenantID, "testproj", "staging"); err != nil {
		t.Fatalf("first AddEnvironment failed: %v", err)
	}
	if err := mgr.AddEnvironment(context.Background(), tenantID, "testproj", "staging"); err != nil {
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

	// T5.1: post-M2-canonical-fix EnsureTenant starts with empty environments;
	// add production first so removing staging doesn't trigger "at least one
	// environment required" guard.
	if err := mgr.AddEnvironment(context.Background(), tenantID, "testproj", "production"); err != nil {
		t.Fatalf("AddEnvironment(production) failed: %v", err)
	}
	// Add staging, then remove it. T5.1: canonical test project per M2 CRD invariant.
	if err := mgr.AddEnvironment(context.Background(), tenantID, "testproj", "staging"); err != nil {
		t.Fatalf("AddEnvironment failed: %v", err)
	}
	if err := mgr.RemoveEnvironment(context.Background(), tenantID, "testproj", "staging"); err != nil {
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
	err = mgr.RemoveEnvironment(context.Background(), tenantID, "", "does-not-exist")
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
	err = mgr.RemoveEnvironment(context.Background(), tenantID, "", "production")
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

	// T5.1 of plan 2026-06-10-j22-layer2-code-review-fixes: post-M2-canonical-fix
	// EnsureTenant creates with empty environments []; the canonical T4.1 chain
	// in routes/projects.go appends production via EnvironmentEnsurer. This test
	// invokes the manager directly, so we replicate the chain here.
	if err := mgr.AddEnvironment(context.Background(), tenantID, "testproj", "production"); err != nil {
		t.Fatalf("AddEnvironment(production) failed: %v", err)
	}

	envs, err := mgr.ListEnvironments(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("ListEnvironments failed: %v", err)
	}

	// After explicit AddEnvironment, tenant has 1 production env
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

	// T5.1: replicate the canonical T4.1 chain that EnsureTenant no longer does.
	if err := mgr.AddEnvironment(context.Background(), tenantID, "testproj", "production"); err != nil {
		t.Fatalf("AddEnvironment(production) failed: %v", err)
	}

	// Existing environment
	detail, err := mgr.GetEnvironmentDetail(context.Background(), tenantID, "testproj", "production")
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
	detail, err = mgr.GetEnvironmentDetail(context.Background(), tenantID, "testproj", "nonexistent")
	if err != nil {
		t.Fatalf("GetEnvironmentDetail (nonexistent) failed: %v", err)
	}
	if detail != nil {
		t.Errorf("expected nil for nonexistent environment, got %+v", detail)
	}
}

// =============================================================================
// M2 v1.2 — T2.1 per-(project, env) tuple tests
// Plan: 2026-06-07-runtime-envs-per-project-m2 v1.2
// =============================================================================

// TestTenantCRManager_AddEnvironment_WritesProjectInSpec asserts that the
// canonical project field lands on the Tenant CR spec.environments entry
// when the project argument is non-empty.
func TestTenantCRManager_AddEnvironment_WritesProjectInSpec(t *testing.T) {
	client := getDynamicClient(t)
	tenantID := "m2-test-write-project"
	cleanupTenant(t, client, tenantID)
	t.Cleanup(func() { cleanupTenant(t, client, tenantID) })

	mgr := &TenantCRManager{Client: client}
	if err := mgr.EnsureTenant(context.Background(), tenantID, routes.TenantCRSpec{Plan: "starter"}); err != nil {
		t.Fatalf("EnsureTenant failed: %v", err)
	}

	if err := mgr.AddEnvironment(context.Background(), tenantID, "web", "staging"); err != nil {
		t.Fatalf("AddEnvironment(web, staging) failed: %v", err)
	}

	envs, err := mgr.ListEnvironments(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("ListEnvironments failed: %v", err)
	}
	var found *domain.EnvironmentEntry
	for i := range envs {
		if envs[i].Name == "staging" && envs[i].Project == "web" {
			found = &envs[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected entry (staging, project=web) in spec.environments, got %v", envs)
	}
}

// TestTenantCRManager_AddEnvironment_SameNameDifferentProjectsBothPersist
// asserts that the (name, project) tuple is the uniqueness key —
// `staging+web` and `staging+api` coexist as 2 distinct entries.
func TestTenantCRManager_AddEnvironment_SameNameDifferentProjectsBothPersist(t *testing.T) {
	client := getDynamicClient(t)
	tenantID := "m2-test-same-name-diff-proj"
	cleanupTenant(t, client, tenantID)
	t.Cleanup(func() { cleanupTenant(t, client, tenantID) })

	mgr := &TenantCRManager{Client: client}
	if err := mgr.EnsureTenant(context.Background(), tenantID, routes.TenantCRSpec{Plan: "growth"}); err != nil {
		t.Fatalf("EnsureTenant failed: %v", err)
	}

	if err := mgr.AddEnvironment(context.Background(), tenantID, "web", "staging"); err != nil {
		t.Fatalf("AddEnvironment(web, staging) failed: %v", err)
	}
	if err := mgr.AddEnvironment(context.Background(), tenantID, "api", "staging"); err != nil {
		t.Fatalf("AddEnvironment(api, staging) failed: %v", err)
	}

	envs, _ := mgr.ListEnvironments(context.Background(), tenantID)
	staging := 0
	for _, e := range envs {
		if e.Name == "staging" {
			staging++
		}
	}
	if staging != 2 {
		t.Errorf("expected 2 staging entries (one per project), got %d", staging)
	}
}

// TestTenantCRManager_RemoveEnvironment_OnlyRemovesMatchingProject asserts
// that the filter is per-(name, project) tuple — removing `staging+web`
// leaves `staging+api` untouched.
func TestTenantCRManager_RemoveEnvironment_OnlyRemovesMatchingProject(t *testing.T) {
	client := getDynamicClient(t)
	tenantID := "m2-test-remove-tuple"
	cleanupTenant(t, client, tenantID)
	t.Cleanup(func() { cleanupTenant(t, client, tenantID) })

	mgr := &TenantCRManager{Client: client}
	if err := mgr.EnsureTenant(context.Background(), tenantID, routes.TenantCRSpec{Plan: "growth"}); err != nil {
		t.Fatalf("EnsureTenant failed: %v", err)
	}
	if err := mgr.AddEnvironment(context.Background(), tenantID, "web", "staging"); err != nil {
		t.Fatalf("AddEnvironment(web, staging) failed: %v", err)
	}
	if err := mgr.AddEnvironment(context.Background(), tenantID, "api", "staging"); err != nil {
		t.Fatalf("AddEnvironment(api, staging) failed: %v", err)
	}

	if err := mgr.RemoveEnvironment(context.Background(), tenantID, "web", "staging"); err != nil {
		t.Fatalf("RemoveEnvironment(web, staging) failed: %v", err)
	}

	envs, _ := mgr.ListEnvironments(context.Background(), tenantID)
	for _, e := range envs {
		if e.Name == "staging" && e.Project == "web" {
			t.Errorf("(staging, web) should have been removed; envs=%v", envs)
		}
	}
	// The (staging, api) tuple MUST remain.
	apiKept := false
	for _, e := range envs {
		if e.Name == "staging" && e.Project == "api" {
			apiKept = true
			break
		}
	}
	if !apiKept {
		t.Errorf("(staging, api) should have been preserved; envs=%v", envs)
	}
}

// TestTenantCRManager_GetEnvironmentDetail_FiltersByProject asserts that
// lookup is per-(name, project) — same name in a different project does
// not match.
func TestTenantCRManager_GetEnvironmentDetail_FiltersByProject(t *testing.T) {
	client := getDynamicClient(t)
	tenantID := "m2-test-get-filter"
	cleanupTenant(t, client, tenantID)
	t.Cleanup(func() { cleanupTenant(t, client, tenantID) })

	mgr := &TenantCRManager{Client: client}
	if err := mgr.EnsureTenant(context.Background(), tenantID, routes.TenantCRSpec{Plan: "growth"}); err != nil {
		t.Fatalf("EnsureTenant failed: %v", err)
	}
	if err := mgr.AddEnvironment(context.Background(), tenantID, "web", "staging"); err != nil {
		t.Fatalf("AddEnvironment(web, staging) failed: %v", err)
	}

	got, err := mgr.GetEnvironmentDetail(context.Background(), tenantID, "web", "staging")
	if err != nil {
		t.Fatalf("GetEnvironmentDetail(web, staging) failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil detail for (web, staging)")
	}
	other, _ := mgr.GetEnvironmentDetail(context.Background(), tenantID, "api", "staging")
	if other != nil {
		t.Errorf("expected nil detail for (api, staging) — tuple not present, got %+v", other)
	}
}

// TestTenantCRManager_ListEnvironments_ReturnsProjectPerEntry asserts
// that the Project field of each EnvironmentEntry is populated from the
// CR spec.
func TestTenantCRManager_ListEnvironments_ReturnsProjectPerEntry(t *testing.T) {
	client := getDynamicClient(t)
	tenantID := "m2-test-list-project"
	cleanupTenant(t, client, tenantID)
	t.Cleanup(func() { cleanupTenant(t, client, tenantID) })

	mgr := &TenantCRManager{Client: client}
	if err := mgr.EnsureTenant(context.Background(), tenantID, routes.TenantCRSpec{Plan: "growth"}); err != nil {
		t.Fatalf("EnsureTenant failed: %v", err)
	}
	if err := mgr.AddEnvironment(context.Background(), tenantID, "web", "dev"); err != nil {
		t.Fatalf("AddEnvironment(web, dev) failed: %v", err)
	}

	envs, err := mgr.ListEnvironments(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("ListEnvironments failed: %v", err)
	}
	var hit *domain.EnvironmentEntry
	for i := range envs {
		if envs[i].Name == "dev" {
			hit = &envs[i]
			break
		}
	}
	if hit == nil {
		t.Fatalf("expected entry (dev), got %v", envs)
	}
	if hit.Project != "web" {
		t.Errorf("expected Project=web on (dev), got %q", hit.Project)
	}
}

// TestTenantCRManager_AddEnvironment_RejectsCombinedNamespaceOver63Chars
// asserts the EC-4 pre-flight guard: when the combined RFC 1123 namespace
// `loopzero-<tenant>-<project>-<env>` would exceed 63 chars, AddEnvironment
// fails BEFORE the apiserver round-trip.
//
// This test does NOT need a live cluster — the guard fires on the
// `naming.Namespace` validator. We still use the integration harness
// to keep the test next to its siblings; the cluster Get/Update is
// never reached.
func TestTenantCRManager_AddEnvironment_RejectsCombinedNamespaceOver63Chars(t *testing.T) {
	client := getDynamicClient(t)
	mgr := &TenantCRManager{Client: client}

	// loopzero- (5) + tenant (15) + - (1) + project (30) + - (1) + env (15) = 67 chars > 63
	tenantID := "tn-08fa2ecedc3c7e" // 16 chars
	veryLongProject := "veryveryverylongprojectname-30"
	envName := "staging-extra-12"

	err := mgr.AddEnvironment(context.Background(), tenantID, veryLongProject, envName)
	if err == nil {
		t.Fatal("expected error for combined namespace > 63 chars, got nil")
	}
	// The error MUST mention the 63-char limit OR contain "exceeds".
	msg := err.Error()
	if !contains63HintInMessage(msg) {
		t.Errorf("expected error to mention RFC 1123 / 63-char ceiling, got %q", msg)
	}
}

// contains63HintInMessage reports whether the error message points the
// operator at the 63-char ceiling that fired. Matches a few variants
// emitted by pkg/naming.ValidateKubernetesName.
func contains63HintInMessage(msg string) bool {
	for _, hint := range []string{"63", "RFC 1123", "too long", "exceeds", "length"} {
		if len(hint) > 0 && containsCI(msg, hint) {
			return true
		}
	}
	return false
}

// containsCI is a case-insensitive substring check (no extra dep).
func containsCI(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	if len(haystack) < len(needle) {
		return false
	}
	// Cheap lowercasing avoids importing strings here (kept consistent
	// with other tiny helpers in this file).
	lowerH := make([]byte, len(haystack))
	for i := 0; i < len(haystack); i++ {
		c := haystack[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		lowerH[i] = c
	}
	lowerN := make([]byte, len(needle))
	for i := 0; i < len(needle); i++ {
		c := needle[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		lowerN[i] = c
	}
	for i := 0; i+len(lowerN) <= len(lowerH); i++ {
		match := true
		for j := 0; j < len(lowerN); j++ {
			if lowerH[i+j] != lowerN[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
