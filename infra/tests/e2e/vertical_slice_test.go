// Package e2e contains end-to-end tests that prove system convergence.
//
// The vertical slice test validates the full flow:
//
//	API auth → project creation → Tenant CR → TenantController reconciliation → K8s objects
//
// It uses envtest for K8s API (no real cluster needed) and an in-process API server.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/loopzero/loopzero/api/apitest"
	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/operatortest"
)

// crdBasesPath returns the absolute path to the CRD bases directory.
func crdBasesPath() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "operators", "config", "crd", "bases")
}

// --- Fake implementations for API layer (DIP: real interfaces, test doubles) ---

// fakeTokenValidator accepts any token with value "test-token".
type fakeTokenValidator struct{}

func (f *fakeTokenValidator) ValidateToken(_ context.Context, token string) (*apitest.TokenValidationResult, error) {
	if token != "test-token" {
		return nil, apitest.ErrInvalidToken
	}
	return &apitest.TokenValidationResult{
		UserID:   "user-1",
		Email:    "test@example.com",
		GitHubID: 12345,
	}, nil
}

// fakeMembershipStore returns a single membership for "user-1" in tenant "acme".
type fakeMembershipStore struct{}

func (f *fakeMembershipStore) GetMemberships(_ context.Context, userID string) ([]apitest.TenantMembership, error) {
	if userID == "user-1" {
		return []apitest.TenantMembership{
			{TenantID: "acme", Role: "owner"},
		}, nil
	}
	return nil, nil
}

// fakeProjectStore stores projects in memory.
type fakeProjectStore struct {
	projects map[string]apitest.ProjectResult
}

func newFakeProjectStore() *fakeProjectStore {
	return &fakeProjectStore{projects: make(map[string]apitest.ProjectResult)}
}

func (f *fakeProjectStore) CreateProject(_ context.Context, input apitest.CreateProjectInput) (apitest.ProjectResult, error) {
	key := input.TenantID + "/" + input.Name
	if _, exists := f.projects[key]; exists {
		return apitest.ProjectResult{}, apitest.ErrProjectExists
	}
	result := apitest.ProjectResult{
		ID:     fmt.Sprintf("proj-%s", input.Name),
		Name:   input.Name,
		TeamID: input.TenantID,
	}
	f.projects[key] = result
	return result, nil
}

func (f *fakeProjectStore) GetByName(_ context.Context, tenantID, name string) (*apitest.ProjectResult, error) {
	key := tenantID + "/" + name
	if result, exists := f.projects[key]; exists {
		return &result, nil
	}
	return nil, nil
}

func (f *fakeProjectStore) DeleteProject(_ context.Context, _, _ string) error {
	return nil
}

// k8sTenantCRManager implements TenantCRManager using a real K8s client.
// This is the critical bridge between API and operator — it creates the Tenant CR
// that the TenantController will reconcile.
type k8sTenantCRManager struct {
	k8sClient client.Client
}

func (m *k8sTenantCRManager) EnsureTenant(ctx context.Context, tenantID string, spec apitest.TenantCRSpec) error {
	tenant := &loopzerov1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tenantID,
			Namespace: "default",
		},
	}

	_, err := ctrl.CreateOrUpdate(ctx, m.k8sClient, tenant, func() error {
		tenant.Spec = loopzerov1.TenantSpec{
			DisplayName: tenantID,
			Plan:        loopzerov1.PlanTier(spec.Plan),
			Environments: []loopzerov1.EnvironmentSpec{
				{Name: "production", Cluster: "workload"},
				{Name: "staging", Cluster: "workload"},
			},
			GitOps: loopzerov1.GitOpsSpec{
				RepoName: tenantID + "-gitops",
				Branch:   "main",
			},
			Preview: loopzerov1.PreviewSpec{
				MaxEnvironments: 3,
				TTLHours:        72,
			},
			Limits: loopzerov1.ResourceLimits{
				MaxCPU:              resource.MustParse("4"),
				MaxMemory:           resource.MustParse("8Gi"),
				MaxStorage:          resource.MustParse("20Gi"),
				MaxBuildConcurrency: 2,
			},
		}
		return nil
	})
	return err
}

// --- Test ---

// TestVerticalSlice_APIToControllerConvergence is the Sprint 1 proof of convergence.
// It proves that CLI → API → Tenant CR → TenantController → K8s objects all work together.
func TestVerticalSlice_APIToControllerConvergence(t *testing.T) {
	// Arrange: envtest + controller + API server
	logf.SetLogger(zap.New(zap.WriteTo(io.Discard)))

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{crdBasesPath()},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("failed to start envtest: %v", err)
	}
	defer testEnv.Stop()

	scheme := k8sruntime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add client-go scheme: %v", err)
	}
	if err := loopzerov1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add loopzero scheme: %v", err)
	}

	// Direct client for test assertions (non-cached, reads go to API server).
	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("failed to create k8s client: %v", err)
	}

	// Start controller manager with TenantController.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0", // disable metrics
		},
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	fakeGit := operatortest.NewFakeGitProvisioner()
	fakeArgoCD := operatortest.NewFakeArgoCDProvisioner()
	if err := operatortest.SetupTenantController(mgr, fakeGit, fakeArgoCD); err != nil {
		t.Fatalf("failed to setup controller: %v", err)
	}

	go func() {
		if err := mgr.Start(ctx); err != nil {
			t.Logf("manager stopped: %v", err)
		}
	}()

	// Wait for cache sync.
	if !mgr.GetCache().WaitForCacheSync(ctx) {
		t.Fatal("cache sync failed")
	}

	// Create API server with real TenantCRManager (talks to envtest K8s).
	crMgr := &k8sTenantCRManager{k8sClient: k8sClient}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	router := apitest.NewRouter(apitest.Config{
		Port:           "0",
		Logger:         logger,
		TokenValidator: &fakeTokenValidator{},
		MemberStore:    &fakeMembershipStore{},
		ProjectStore:   newFakeProjectStore(),
		TenantCRMgr:    crMgr,
	})
	ts := httptest.NewServer(router)
	defer ts.Close()

	// ── Step 1: Authentication works (whoami) ──
	t.Run("whoami_returns_authenticated_user", func(t *testing.T) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/v1/auth/whoami", nil)
		req.Header.Set("Authorization", "Bearer test-token")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("whoami request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)

		if result["email"] != "test@example.com" {
			t.Errorf("expected email test@example.com, got %v", result["email"])
		}
		if result["team"] != "acme" {
			t.Errorf("expected team acme, got %v", result["team"])
		}
	})

	// ── Step 2: Correlation-ID crosses API boundary ──
	t.Run("correlation_id_returned_in_response", func(t *testing.T) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/v1/auth/whoami", nil)
		req.Header.Set("Authorization", "Bearer test-token")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		corrID := resp.Header.Get("Correlation-ID")
		if corrID == "" {
			t.Fatal("expected Correlation-ID header, got empty")
		}
	})

	// ── Step 3: API creates project and triggers Tenant CR ──
	t.Run("create_project_triggers_tenant_cr", func(t *testing.T) {
		body := bytes.NewBufferString(`{"name":"my-saas","apps":[]}`)
		req, _ := http.NewRequest("POST", ts.URL+"/api/v1/projects", body)
		req.Header.Set("Authorization", "Bearer test-token")
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("create project request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			respBody, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 201, got %d: %s", resp.StatusCode, respBody)
		}

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)

		if result["name"] != "my-saas" {
			t.Errorf("expected name my-saas, got %v", result["name"])
		}
		if result["team"] != "acme" {
			t.Errorf("expected team acme, got %v", result["team"])
		}
	})

	// ── Step 4: TenantController reconciles → Phase=Active ──
	t.Run("controller_reconciles_tenant_to_active", func(t *testing.T) {
		var tenant loopzerov1.Tenant
		deadline := time.Now().Add(30 * time.Second)

		for time.Now().Before(deadline) {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: "acme", Namespace: "default"}, &tenant)
			if err == nil && tenant.Status.Phase == loopzerov1.TenantPhaseActive {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}

		if tenant.Status.Phase != loopzerov1.TenantPhaseActive {
			t.Fatalf("expected tenant phase Active, got %q (conditions: %+v)",
				tenant.Status.Phase, tenant.Status.Conditions)
		}
	})

	// ── Step 5: All K8s objects exist ──
	t.Run("environment_namespaces_exist", func(t *testing.T) {
		for _, env := range []string{"production", "staging"} {
			nsName := fmt.Sprintf("acme-%s", env)
			var ns corev1.Namespace
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, &ns); err != nil {
				t.Errorf("namespace %s not found: %v", nsName, err)
				continue
			}
			if ns.Labels["loopzero.io/tenant"] != "acme" {
				t.Errorf("namespace %s: expected label loopzero.io/tenant=acme, got %q", nsName, ns.Labels["loopzero.io/tenant"])
			}
			if ns.Labels["loopzero.io/environment"] != env {
				t.Errorf("namespace %s: expected label loopzero.io/environment=%s, got %q", nsName, env, ns.Labels["loopzero.io/environment"])
			}
		}
	})

	t.Run("build_namespace_exists", func(t *testing.T) {
		var ns corev1.Namespace
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: "build-acme"}, &ns); err != nil {
			t.Fatalf("build namespace not found: %v", err)
		}
		if ns.Labels["loopzero.io/component"] != "build" {
			t.Errorf("expected label loopzero.io/component=build, got %q", ns.Labels["loopzero.io/component"])
		}
	})

	t.Run("resource_quotas_exist", func(t *testing.T) {
		for _, env := range []string{"production", "staging"} {
			nsName := fmt.Sprintf("acme-%s", env)
			var quota corev1.ResourceQuota
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "tenant-quota", Namespace: nsName}, &quota); err != nil {
				t.Errorf("resource quota in %s not found: %v", nsName, err)
			}
		}
		var buildQuota corev1.ResourceQuota
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: "build-quota", Namespace: "build-acme"}, &buildQuota); err != nil {
			t.Errorf("build resource quota not found: %v", err)
		}
	})

	t.Run("rbac_resources_exist", func(t *testing.T) {
		for _, env := range []string{"production", "staging"} {
			nsName := fmt.Sprintf("acme-%s", env)

			var sa corev1.ServiceAccount
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "loopzero-deployer", Namespace: nsName}, &sa); err != nil {
				t.Errorf("service account in %s not found: %v", nsName, err)
			}

			var role rbacv1.Role
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "loopzero-deployer", Namespace: nsName}, &role); err != nil {
				t.Errorf("role in %s not found: %v", nsName, err)
			}

			var rb rbacv1.RoleBinding
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "loopzero-deployer", Namespace: nsName}, &rb); err != nil {
				t.Errorf("rolebinding in %s not found: %v", nsName, err)
			}
		}
	})

	t.Run("network_policies_exist", func(t *testing.T) {
		for _, env := range []string{"production", "staging"} {
			nsName := fmt.Sprintf("acme-%s", env)
			policies := []string{"deny-all", "allow-ingress-traefik", "allow-egress"}
			for _, policyName := range policies {
				var np networkingv1.NetworkPolicy
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: policyName, Namespace: nsName}, &np); err != nil {
					t.Errorf("network policy %s in %s not found: %v", policyName, nsName, err)
				}
			}
		}
	})

	// ── Step 6: Conditions reflect real state ──
	t.Run("conditions_all_true", func(t *testing.T) {
		var tenant loopzerov1.Tenant
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: "acme", Namespace: "default"}, &tenant); err != nil {
			t.Fatalf("failed to get tenant: %v", err)
		}

		expectedConditions := []string{
			loopzerov1.ConditionGitRepoReady,
			loopzerov1.ConditionNamespacesReady,
			loopzerov1.ConditionRBACReady,
			loopzerov1.ConditionNetworkPolicyReady,
			loopzerov1.ConditionBuildNamespaceReady,
			loopzerov1.ConditionQuotaEnforced,
		}

		for _, condType := range expectedConditions {
			found := false
			for _, c := range tenant.Status.Conditions {
				if c.Type == condType {
					if c.Status != metav1.ConditionTrue {
						t.Errorf("condition %s expected True, got %s (reason: %s, message: %s)",
							condType, c.Status, c.Reason, c.Message)
					}
					found = true
					break
				}
			}
			if !found {
				t.Errorf("condition %s not found in tenant status", condType)
			}
		}
	})

	// ── Step 7: ObservedGeneration matches ──
	t.Run("observed_generation_matches", func(t *testing.T) {
		var tenant loopzerov1.Tenant
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: "acme", Namespace: "default"}, &tenant); err != nil {
			t.Fatalf("failed to get tenant: %v", err)
		}
		if tenant.Status.ObservedGeneration != tenant.Generation {
			t.Errorf("observedGeneration %d != generation %d",
				tenant.Status.ObservedGeneration, tenant.Generation)
		}
	})

	// ── Step 8: Unauthorized request is rejected ──
	t.Run("unauthorized_request_rejected", func(t *testing.T) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/v1/auth/whoami", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})
}
