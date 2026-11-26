// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package tenancy

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/getloopscale/loop-runtime/api/internal/k8s"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
	apiv1 "github.com/getloopscale/loop-runtime/operators/api/v1"
)

// lockedClient wraps a controller-runtime client, serializing each individual
// API call under a mutex — modeling the real API server's storage-layer
// serialization. The controller-runtime fake client (v0.23.x) is not safe for
// concurrent use by its internal object tracker; this wrapper lets the
// concurrency test exercise the optimistic-concurrency retry path (Get →
// mutate → Update → Conflict → retry, interleaved across goroutines) without a
// data race in the test tool itself. Get and Update are locked independently
// (NOT jointly), so cross-goroutine interleaving still produces the
// resourceVersion Conflicts that drive RetryOnConflict — exactly what a real
// API server does. Production uses the real typed client against the API
// server, which is the actual serialization point; TenantCRManager holds no
// shared mutable state of its own.
type lockedClient struct {
	client.Client
	mu *sync.Mutex
}

func (c *lockedClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Client.Get(ctx, key, obj, opts...)
}

func (c *lockedClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Client.Create(ctx, obj, opts...)
}

func (c *lockedClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Client.Update(ctx, obj, opts...)
}

// newTenantMgrLocked builds a TenantCRManager whose fake client is wrapped in
// lockedClient — for the concurrent-writer test (see lockedClient doc).
func newTenantMgrLocked(t *testing.T) *TenantCRManager {
	t.Helper()
	scheme, err := k8s.NewScheme()
	if err != nil {
		t.Fatalf("NewScheme: %v", err)
	}
	base := fake.NewClientBuilder().WithScheme(scheme).Build()
	return &TenantCRManager{Client: &lockedClient{Client: base, mu: &sync.Mutex{}}}
}

// newTenantMgr builds a TenantCRManager backed by the controller-runtime fake
// typed client (M8). Unlike the pre-M8 dynamic-client integration tests, these
// run as UNIT tests with NO live cluster — the typed migration makes the manager
// fully unit-testable (and independent of the flaky tenant envtest suite).
func newTenantMgr(t *testing.T) *TenantCRManager {
	t.Helper()
	scheme, err := k8s.NewScheme()
	if err != nil {
		t.Fatalf("NewScheme: %v", err)
	}
	return &TenantCRManager{Client: fake.NewClientBuilder().WithScheme(scheme).Build()}
}

func getTenant(t *testing.T, m *TenantCRManager, tenantID string) *apiv1.Tenant {
	t.Helper()
	var got apiv1.Tenant
	if err := m.Client.Get(context.Background(), client.ObjectKey{Namespace: tenantNamespace, Name: tenantID}, &got); err != nil {
		t.Fatalf("expected Tenant CR %q to exist: %v", tenantID, err)
	}
	return &got
}

func TestTenantCRManager_EnsureTenant_Creates(t *testing.T) {
	mgr := newTenantMgr(t)
	tenantID := "test-cr-create"

	if err := mgr.EnsureTenant(context.Background(), tenantID, ports.TenantCRSpec{Plan: "starter"}); err != nil {
		t.Fatalf("EnsureTenant failed: %v", err)
	}

	got := getTenant(t, mgr, tenantID)
	if string(got.Spec.Plan) != "starter" {
		t.Errorf("expected plan 'starter', got %q", got.Spec.Plan)
	}
	if got.Spec.DisplayName != tenantID {
		t.Errorf("expected displayName %q, got %q", tenantID, got.Spec.DisplayName)
	}
}

func TestTenantCRManager_EnsureTenant_Idempotent(t *testing.T) {
	mgr := newTenantMgr(t)
	tenantID := "test-cr-idempotent"

	if err := mgr.EnsureTenant(context.Background(), tenantID, ports.TenantCRSpec{Plan: "starter"}); err != nil {
		t.Fatalf("first EnsureTenant failed: %v", err)
	}
	if err := mgr.EnsureTenant(context.Background(), tenantID, ports.TenantCRSpec{Plan: "starter"}); err != nil {
		t.Fatalf("second EnsureTenant (idempotent) failed: %v", err)
	}
}

func TestTenantCRManager_EnsureTenant_UpdatesPlan(t *testing.T) {
	mgr := newTenantMgr(t)
	tenantID := "test-cr-update-plan"

	if err := mgr.EnsureTenant(context.Background(), tenantID, ports.TenantCRSpec{Plan: "starter"}); err != nil {
		t.Fatalf("EnsureTenant (starter) failed: %v", err)
	}
	if err := mgr.EnsureTenant(context.Background(), tenantID, ports.TenantCRSpec{Plan: "growth"}); err != nil {
		t.Fatalf("EnsureTenant (growth) failed: %v", err)
	}

	got := getTenant(t, mgr, tenantID)
	if string(got.Spec.Plan) != "growth" {
		t.Errorf("expected plan 'growth', got %q", got.Spec.Plan)
	}
}

func TestTenantCRManager_ImplementsInterface(t *testing.T) {
	var _ ports.TenantCRManager = (*TenantCRManager)(nil)
}

func TestTenantCRManager_AddEnvironment(t *testing.T) {
	mgr := newTenantMgr(t)
	tenantID := "test-add-env"

	if err := mgr.EnsureTenant(context.Background(), tenantID, ports.TenantCRSpec{Plan: "starter"}); err != nil {
		t.Fatalf("EnsureTenant failed: %v", err)
	}
	if err := mgr.AddEnvironment(context.Background(), tenantID, "testproj", "staging"); err != nil {
		t.Fatalf("AddEnvironment failed: %v", err)
	}

	envs, err := mgr.ListEnvironments(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("ListEnvironments failed: %v", err)
	}
	found := false
	for _, e := range envs {
		if e.Name == "staging" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'staging' in environments, got %v", envs)
	}
}

func TestTenantCRManager_AddEnvironment_ReportsAnExistingTupleAndKeepsOneEntry(t *testing.T) {
	mgr := newTenantMgr(t)
	tenantID := "test-add-env-idem"

	if err := mgr.EnsureTenant(context.Background(), tenantID, ports.TenantCRSpec{Plan: "starter"}); err != nil {
		t.Fatalf("EnsureTenant failed: %v", err)
	}
	if err := mgr.AddEnvironment(context.Background(), tenantID, "testproj", "staging"); err != nil {
		t.Fatalf("first AddEnvironment failed: %v", err)
	}
	if err := mgr.AddEnvironment(context.Background(), tenantID, "testproj", "staging"); !errors.Is(err, ports.ErrEnvironmentExists) {
		t.Fatalf("second AddEnvironment = %v, want ports.ErrEnvironmentExists", err)
	}

	envs, _ := mgr.ListEnvironments(context.Background(), tenantID)
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
	mgr := newTenantMgr(t)
	tenantID := "test-rm-env"

	if err := mgr.EnsureTenant(context.Background(), tenantID, ports.TenantCRSpec{Plan: "starter"}); err != nil {
		t.Fatalf("EnsureTenant failed: %v", err)
	}
	if err := mgr.AddEnvironment(context.Background(), tenantID, "testproj", "production"); err != nil {
		t.Fatalf("AddEnvironment(production) failed: %v", err)
	}
	if err := mgr.AddEnvironment(context.Background(), tenantID, "testproj", "staging"); err != nil {
		t.Fatalf("AddEnvironment(staging) failed: %v", err)
	}
	if err := mgr.RemoveEnvironment(context.Background(), tenantID, "testproj", "staging"); err != nil {
		t.Fatalf("RemoveEnvironment failed: %v", err)
	}

	envs, _ := mgr.ListEnvironments(context.Background(), tenantID)
	for _, e := range envs {
		if e.Name == "staging" {
			t.Errorf("expected 'staging' removed, still present in %v", envs)
		}
	}
}

// CHG-2026-04-23-AUDIT-12: removing a non-existing env returns
// ErrEnvironmentNotFound (no silent idempotence) so the handler maps to 404.
func TestTenantCRManager_RemoveEnvironment_NonExisting_ReturnsNotFound(t *testing.T) {
	mgr := newTenantMgr(t)
	tenantID := "test-rm-env-idem"

	if err := mgr.EnsureTenant(context.Background(), tenantID, ports.TenantCRSpec{Plan: "starter"}); err != nil {
		t.Fatalf("EnsureTenant failed: %v", err)
	}
	err := mgr.RemoveEnvironment(context.Background(), tenantID, "", "does-not-exist")
	if err == nil {
		t.Fatalf("RemoveEnvironment (non-existing) must not succeed silently")
	}
	if !errors.Is(err, ErrEnvironmentNotFound) {
		t.Fatalf("want ErrEnvironmentNotFound, got %v", err)
	}
}

func TestTenantCRManager_RemoveEnvironment_RejectsLastEnvironment(t *testing.T) {
	mgr := newTenantMgr(t)
	tenantID := "test-rm-env-last"

	if err := mgr.EnsureTenant(context.Background(), tenantID, ports.TenantCRSpec{Plan: "starter"}); err != nil {
		t.Fatalf("EnsureTenant failed: %v", err)
	}
	// Add exactly one env, then attempt to remove it → must be rejected.
	if err := mgr.AddEnvironment(context.Background(), tenantID, "testproj", "production"); err != nil {
		t.Fatalf("AddEnvironment(production) failed: %v", err)
	}
	if err := mgr.RemoveEnvironment(context.Background(), tenantID, "testproj", "production"); err == nil {
		t.Fatal("expected error when removing the last environment, got nil")
	}
}

func TestTenantCRManager_ListEnvironments(t *testing.T) {
	mgr := newTenantMgr(t)
	tenantID := "test-list-env"

	if err := mgr.EnsureTenant(context.Background(), tenantID, ports.TenantCRSpec{Plan: "starter"}); err != nil {
		t.Fatalf("EnsureTenant failed: %v", err)
	}
	if err := mgr.AddEnvironment(context.Background(), tenantID, "testproj", "production"); err != nil {
		t.Fatalf("AddEnvironment(production) failed: %v", err)
	}

	envs, err := mgr.ListEnvironments(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("ListEnvironments failed: %v", err)
	}
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
	mgr := newTenantMgr(t)
	tenantID := "test-get-env-detail"

	if err := mgr.EnsureTenant(context.Background(), tenantID, ports.TenantCRSpec{Plan: "starter"}); err != nil {
		t.Fatalf("EnsureTenant failed: %v", err)
	}
	if err := mgr.AddEnvironment(context.Background(), tenantID, "testproj", "production"); err != nil {
		t.Fatalf("AddEnvironment(production) failed: %v", err)
	}

	detail, err := mgr.GetEnvironmentDetail(context.Background(), tenantID, "testproj", "production")
	if err != nil {
		t.Fatalf("GetEnvironmentDetail failed: %v", err)
	}
	if detail == nil {
		t.Fatal("expected detail for 'production', got nil")
	}
	if detail.Name != "production" || detail.Status != "active" {
		t.Errorf("unexpected detail: %+v", detail)
	}

	other, err := mgr.GetEnvironmentDetail(context.Background(), tenantID, "testproj", "nonexistent")
	if err != nil {
		t.Fatalf("GetEnvironmentDetail (nonexistent) failed: %v", err)
	}
	if other != nil {
		t.Errorf("expected nil for nonexistent environment, got %+v", other)
	}
}

// --- M2 v1.2 per-(project, env) tuple tests ---

func TestTenantCRManager_AddEnvironment_WritesProjectInSpec(t *testing.T) {
	mgr := newTenantMgr(t)
	tenantID := "m2-test-write-project"
	if err := mgr.EnsureTenant(context.Background(), tenantID, ports.TenantCRSpec{Plan: "starter"}); err != nil {
		t.Fatalf("EnsureTenant failed: %v", err)
	}
	if err := mgr.AddEnvironment(context.Background(), tenantID, "web", "staging"); err != nil {
		t.Fatalf("AddEnvironment(web, staging) failed: %v", err)
	}

	// Read the typed CR directly — assert the project field landed on the entry.
	got := getTenant(t, mgr, tenantID)
	found := false
	for _, e := range got.Spec.Environments {
		if e.Name == "staging" && e.Project == "web" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected (staging, project=web) in spec.environments, got %+v", got.Spec.Environments)
	}
}

func TestTenantCRManager_AddEnvironment_SameNameDifferentProjectsBothPersist(t *testing.T) {
	mgr := newTenantMgr(t)
	tenantID := "m2-test-same-name-diff-proj"
	if err := mgr.EnsureTenant(context.Background(), tenantID, ports.TenantCRSpec{Plan: "growth"}); err != nil {
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

func TestTenantCRManager_RemoveEnvironment_OnlyRemovesMatchingProject(t *testing.T) {
	mgr := newTenantMgr(t)
	tenantID := "m2-test-remove-tuple"
	if err := mgr.EnsureTenant(context.Background(), tenantID, ports.TenantCRSpec{Plan: "growth"}); err != nil {
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
	apiKept := false
	for _, e := range envs {
		if e.Name == "staging" && e.Project == "web" {
			t.Errorf("(staging, web) should have been removed; envs=%v", envs)
		}
		if e.Name == "staging" && e.Project == "api" {
			apiKept = true
		}
	}
	if !apiKept {
		t.Errorf("(staging, api) should have been preserved; envs=%v", envs)
	}
}

func TestTenantCRManager_GetEnvironmentDetail_FiltersByProject(t *testing.T) {
	mgr := newTenantMgr(t)
	tenantID := "m2-test-get-filter"
	if err := mgr.EnsureTenant(context.Background(), tenantID, ports.TenantCRSpec{Plan: "growth"}); err != nil {
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
		t.Errorf("expected nil detail for (api, staging), got %+v", other)
	}
}

func TestTenantCRManager_ListEnvironments_ReturnsProjectPerEntry(t *testing.T) {
	mgr := newTenantMgr(t)
	tenantID := "m2-test-list-project"
	if err := mgr.EnsureTenant(context.Background(), tenantID, ports.TenantCRSpec{Plan: "growth"}); err != nil {
		t.Fatalf("EnsureTenant failed: %v", err)
	}
	if err := mgr.AddEnvironment(context.Background(), tenantID, "web", "dev"); err != nil {
		t.Fatalf("AddEnvironment(web, dev) failed: %v", err)
	}

	envs, _ := mgr.ListEnvironments(context.Background(), tenantID)
	var hit *ports.EnvironmentEntry
	for i := range envs {
		if envs[i].Name == "dev" {
			hit = &envs[i]
		}
	}
	if hit == nil {
		t.Fatalf("expected entry (dev), got %v", envs)
	}
	if hit.Project != "web" {
		t.Errorf("expected Project=web on (dev), got %q", hit.Project)
	}
}

// EC-4 pre-flight: a combined namespace `loopruntime-<tenant>-<project>-<env>` over 63
// chars fails BEFORE any client round-trip (fires on the naming validator).
func TestTenantCRManager_AddEnvironment_RejectsCombinedNamespaceOver63Chars(t *testing.T) {
	mgr := newTenantMgr(t)
	tenantID := "tn-08fa2ecedc3c7e"
	veryLongProject := "veryveryverylongprojectname-30"
	envName := "staging-extra-12"

	err := mgr.AddEnvironment(context.Background(), tenantID, veryLongProject, envName)
	if err == nil {
		t.Fatal("expected error for combined namespace > 63 chars, got nil")
	}
	msg := strings.ToLower(err.Error())
	hinted := false
	for _, h := range []string{"63", "rfc 1123", "too long", "exceeds", "length"} {
		if strings.Contains(msg, h) {
			hinted = true
		}
	}
	if !hinted {
		t.Errorf("expected error to mention the 63-char ceiling, got %q", err.Error())
	}
}

// TestEnsureTenant_concurrentUpdate exercises the optimistic-concurrency path
// under the race detector: concurrent writers on the same tenant must not lose
// updates. Run with `go test -race`.
func TestEnsureTenant_concurrentUpdate(t *testing.T) {
	mgr := newTenantMgrLocked(t)
	tenantID := "test-concurrent"
	if err := mgr.EnsureTenant(context.Background(), tenantID, ports.TenantCRSpec{Plan: "starter"}); err != nil {
		t.Fatalf("seed EnsureTenant failed: %v", err)
	}

	const writers = 8
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		go func() {
			errs <- mgr.EnsureTenant(context.Background(), tenantID, ports.TenantCRSpec{Plan: "growth"})
		}()
	}
	for i := 0; i < writers; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent EnsureTenant failed: %v", err)
		}
	}
	got := getTenant(t, mgr, tenantID)
	if string(got.Spec.Plan) != "growth" {
		t.Errorf("expected final plan 'growth' after concurrent updates, got %q", got.Spec.Plan)
	}
}

// The Tenant CR names the cell the engine placed the tenant on. Without it the CRD
// default cell-0 applies, and a tenant placed on cell-1 is provisioned on cell-0
// while the engine routes its deploys to cell-1 (#412).
func TestEnsureTenant_CreatesTheTenantOnItsPlacedCell(t *testing.T) {
	mgr := newTenantMgr(t)

	if err := mgr.EnsureTenant(context.Background(), "tn-placed", ports.TenantCRSpec{Plan: "starter", CellID: "cell-1"}); err != nil {
		t.Fatalf("EnsureTenant: %v", err)
	}

	if got := getTenant(t, mgr, "tn-placed").Spec.CellID; got != "cell-1" {
		t.Errorf("spec.cellID = %q, want cell-1", got)
	}
}

// Moving a tenant is the admin pin's job — audited and atomic with the database.
// EnsureTenant runs on every project create and must not move an existing tenant.
func TestEnsureTenant_DoesNotMoveAnExistingTenant(t *testing.T) {
	mgr := newTenantMgr(t)
	ctx := context.Background()
	if err := mgr.EnsureTenant(ctx, "tn-settled", ports.TenantCRSpec{Plan: "starter", CellID: "cell-0"}); err != nil {
		t.Fatalf("first EnsureTenant: %v", err)
	}

	if err := mgr.EnsureTenant(ctx, "tn-settled", ports.TenantCRSpec{Plan: "pro", CellID: "cell-1"}); err != nil {
		t.Fatalf("second EnsureTenant: %v", err)
	}

	got := getTenant(t, mgr, "tn-settled")
	if got.Spec.CellID != "cell-0" {
		t.Errorf("spec.cellID = %q, want cell-0 kept — a project create must not move the tenant", got.Spec.CellID)
	}
	if got.Spec.Plan != "pro" {
		t.Errorf("spec.plan = %q, want pro (the update still applies)", got.Spec.Plan)
	}
}

// A tenant provisioned at sign-up has database rows but no Tenant CR: the CR is
// created with its first project. Its environment list is empty, not an error.
func TestTenantCRManager_ListEnvironments_TenantWithoutCRHasNoEnvironments(t *testing.T) {
	mgr := newTenantMgr(t)

	envs, err := mgr.ListEnvironments(context.Background(), "tn-without-project")
	if err != nil {
		t.Fatalf("ListEnvironments for a tenant with no Tenant CR: want no error, got %v", err)
	}
	if envs == nil || len(envs) != 0 {
		t.Fatalf("want an empty, non-nil list, got %#v", envs)
	}
}

// Only the missing CR means "no environments". Any other read failure (RBAC,
// an unreachable API server) must still reach the caller.
func TestTenantCRManager_ListEnvironments_OtherReadFailuresStillFail(t *testing.T) {
	scheme, err := k8s.NewScheme()
	if err != nil {
		t.Fatalf("NewScheme: %v", err)
	}
	readErr := k8serrors.NewForbidden(tenantsGroupResource, "tn-forbidden", errors.New("rbac denied"))
	c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
			return readErr
		},
	}).Build()
	mgr := &TenantCRManager{Client: c}

	envs, err := mgr.ListEnvironments(context.Background(), "tn-forbidden")
	if !errors.Is(err, readErr) {
		t.Fatalf("want the read error to propagate, got envs=%#v err=%v", envs, err)
	}
}
