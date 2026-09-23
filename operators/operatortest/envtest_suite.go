// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package operatortest

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/environment"
)

// TestT is the minimal subset of testing.TB / Ginkgo's FullGinkgoTInterface
// that StartEnvtest needs. Defined as a local interface so callers can pass
// either *testing.T (Helper + Fatalf) or ginkgo.GinkgoT() (Helper + Fatalf
// without needing to satisfy testing.TB's unexported `private()` marker).
type TestT interface {
	Helper()
	Fatalf(format string, args ...interface{})
}

// EnvtestHandle holds the shared envtest infrastructure each split package
// consumes via BeforeSuite. Centralized here so that tenant/, app/, preview/,
// and platform/ test suites do not maintain 4 divergent copies of boot logic.
//
// ADR-0023 D1 + plan T1.1: split-tree migration cannot duplicate
// suite_test.go four times — the bootstrap logic (envtest binary discovery,
// scheme registration, manager startup, cache sync) MUST live in one place.
type EnvtestHandle struct {
	Cfg       *rest.Config
	K8sClient client.Client
	TestEnv   *envtest.Environment
	Scheme    *runtime.Scheme
	Manager   ctrl.Manager
	Ctx       context.Context
	Cancel    context.CancelFunc

	// Fake provisioners. Each split package shares the same instances so
	// that cross-package state (e.g., a Tenant created by tenant_test.go and
	// observed by application_test.go) reconciles consistently.
	FakeGit     *FakeGitProvisioner
	FakeArgoCD  *FakeArgoCDProvisioner
	FakeMinIO   *FakeMinIOProvisioner
	BuildClient client.Client
	RuntimeCli  client.Client

	// CellAwareFactory is the factory every cell-aware controller in the
	// suite resolves runtime clients through. Specs assert against Calls()
	// to verify which cell a write was routed to.
	CellAwareFactory *FakeRuntimeClientFactory
}

// FakeRuntimeClientFactory implements shared.RuntimeClientFactory for
// envtest suites. T1.2 of deep-review-remediation-plan (closes
// find_arch_003 HIGH): every For(cellID) call is recorded so tests can
// assert "factory was actually exercised" rather than passing factory=nil
// like pre-T1.2 envtest did.
//
// All cellIDs route to the SAME runtimeClient (the envtest cluster) since
// envtest doesn't actually have multi-cluster semantics — but the
// invocation pattern matches production cell-aware code paths exactly.
type FakeRuntimeClientFactory struct {
	mu     sync.Mutex
	target client.Client
	calls  []string
}

// NewFakeRuntimeClientFactory builds a tracking factory routing all
// cellIDs to target.
func NewFakeRuntimeClientFactory(target client.Client) *FakeRuntimeClientFactory {
	return &FakeRuntimeClientFactory{target: target}
}

// For implements shared.RuntimeClientFactory. Records the cellID and
// returns the wrapped target client.
func (f *FakeRuntimeClientFactory) For(_ context.Context, cellID string) (client.Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, cellID)
	return f.target, nil
}

// Calls returns a defensive copy of the recorded cellID call list. Used
// by tests to assert the factory was exercised with expected cell IDs.
func (f *FakeRuntimeClientFactory) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	copy(out, f.calls)
	return out
}

// Reset clears recorded calls. Tests use this between sub-tests to
// isolate assertions.
func (f *FakeRuntimeClientFactory) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = nil
}

// EnvtestConfig configures which controllers a test suite needs.
//
// Each split-package suite_test.go fills in the controllers it exercises
// and calls StartEnvtest; controllers it does NOT need stay disabled to
// minimize surface area + reduce flake risk.
type EnvtestConfig struct {
	// CRDPath is the relative path from the calling test's package to the
	// CRD bases directory. Typically "../../config/crd/bases".
	CRDPath string

	// EnableTenant wires SetupTenantController + SetupEnvironmentControllerWithFactory,
	// the latter routed through the tracking CellAwareFactory. Tenant suite uses this.
	EnableTenant bool

	// EnableApp wires SetupApplicationController + SetupExposureController.
	EnableApp bool

	// EnablePreview wires SetupPreviewController.
	EnablePreview bool

	// EnablePlatform wires SetupClusterCredentialController +
	// SetupGitRepositoryBindingController + SetupPlatformBootstrapController.
	EnablePlatform bool

	// BaseDomain is used by Application and Preview controllers for hostname
	// assignment. Defaults to "dev.loopzero.example" if empty.
	BaseDomain string
}

// StartEnvtest brings up envtest, starts the manager with the configured
// controllers, waits for cache sync, and returns a handle the caller's
// AfterSuite uses to clean up.
//
// CRITICAL: caller MUST defer h.Stop() (or AfterSuite -> h.Stop()) or the
// envtest binary leaks across test runs.
//
// Accepts either *testing.T (for go test direct callers) or ginkgo.GinkgoT()
// (for Ginkgo BeforeSuite blocks). Both satisfy TestT.
func StartEnvtest(t TestT, cfg EnvtestConfig) *EnvtestHandle {
	t.Helper()

	logf.SetLogger(zap.New(zap.WriteTo(os.Stderr), zap.UseDevMode(true)))

	ctx, cancel := context.WithCancel(context.Background())

	binaryDir := os.Getenv("KUBEBUILDER_ASSETS")
	if binaryDir == "" {
		home, _ := os.UserHomeDir()
		envtestBase := filepath.Join(home, ".local", "share", "kubebuilder-envtest", "k8s")
		if entries, err := os.ReadDir(envtestBase); err == nil {
			for i := len(entries) - 1; i >= 0; i-- {
				candidate := filepath.Join(envtestBase, entries[i].Name())
				if _, statErr := os.Stat(filepath.Join(candidate, "etcd")); statErr == nil {
					binaryDir = candidate
					break
				}
			}
		}
	}

	crdPath := cfg.CRDPath
	if crdPath == "" {
		crdPath = filepath.Join("..", "..", "config", "crd", "bases")
	}

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{crdPath},
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: binaryDir,
	}

	restCfg, err := testEnv.Start()
	if err != nil {
		cancel()
		t.Fatalf("envtest start: %v", err)
	}

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		cancel()
		_ = testEnv.Stop()
		t.Fatalf("client-go scheme: %v", err)
	}
	if err := loopzerov1.AddToScheme(scheme); err != nil {
		cancel()
		_ = testEnv.Stop()
		t.Fatalf("loopzerov1 scheme: %v", err)
	}

	k8sClient, err := client.New(restCfg, client.Options{Scheme: scheme})
	if err != nil {
		cancel()
		_ = testEnv.Stop()
		t.Fatalf("client New: %v", err)
	}

	mgr, err := ctrl.NewManager(restCfg, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	if err != nil {
		cancel()
		_ = testEnv.Stop()
		t.Fatalf("manager New: %v", err)
	}

	buildClient, err := client.New(restCfg, client.Options{Scheme: scheme})
	if err != nil {
		cancel()
		_ = testEnv.Stop()
		t.Fatalf("build client New: %v", err)
	}
	runtimeClient, err := client.New(restCfg, client.Options{Scheme: scheme})
	if err != nil {
		cancel()
		_ = testEnv.Stop()
		t.Fatalf("runtime client New: %v", err)
	}

	h := &EnvtestHandle{
		Cfg:         restCfg,
		K8sClient:   k8sClient,
		TestEnv:     testEnv,
		Scheme:      scheme,
		Manager:     mgr,
		Ctx:         ctx,
		Cancel:      cancel,
		FakeGit:     NewFakeGitProvisioner(),
		FakeArgoCD:  NewFakeArgoCDProvisioner(),
		FakeMinIO:   NewFakeMinIOProvisioner(),
		BuildClient: buildClient,
		RuntimeCli:  runtimeClient,
	}

	// Every cell-aware controller reaches the runtime cluster through this
	// tracking factory, so a spec can assert via h.CellAwareFactory.Calls()
	// which cell each write was routed to. envtest has one API server, so
	// every cell resolves to it; the routing decision is what is observed.
	h.CellAwareFactory = NewFakeRuntimeClientFactory(runtimeClient)

	baseDomain := cfg.BaseDomain
	if baseDomain == "" {
		baseDomain = "dev.loopzero.example"
	}

	if cfg.EnableTenant {
		if err := SetupTenantController(mgr, h.FakeGit, h.FakeArgoCD, buildClient, runtimeClient); err != nil {
			h.Stop()
			t.Fatalf("SetupTenantController: %v", err)
		}
		if err := environment.SetupEnvironmentControllerWithFactory(mgr, h.CellAwareFactory); err != nil {
			h.Stop()
			t.Fatalf("SetupEnvironmentControllerWithFactory: %v", err)
		}
	}
	if cfg.EnableApp {
		if err := SetupApplicationController(mgr, baseDomain); err != nil {
			h.Stop()
			t.Fatalf("SetupApplicationController: %v", err)
		}
		if err := SetupExposureController(mgr); err != nil {
			h.Stop()
			t.Fatalf("SetupExposureController: %v", err)
		}
	}
	if cfg.EnablePreview {
		if err := SetupPreviewController(mgr, runtimeClient, baseDomain); err != nil {
			h.Stop()
			t.Fatalf("SetupPreviewController: %v", err)
		}
	}
	if cfg.EnablePlatform {
		if err := SetupClusterCredentialController(mgr); err != nil {
			h.Stop()
			t.Fatalf("SetupClusterCredentialController: %v", err)
		}
		if err := SetupGitRepositoryBindingController(mgr, nil); err != nil {
			h.Stop()
			t.Fatalf("SetupGitRepositoryBindingController: %v", err)
		}
		if err := SetupPlatformBootstrapController(mgr, nil); err != nil {
			h.Stop()
			t.Fatalf("SetupPlatformBootstrapController: %v", err)
		}
	}

	go func() {
		if startErr := mgr.Start(ctx); startErr != nil {
			// Test will fail later via Eventually timeouts; logging here
			// would be lost in test output anyway.
			_ = startErr
		}
	}()

	if !mgr.GetCache().WaitForCacheSync(ctx) {
		h.Stop()
		t.Fatalf("cache sync failed")
	}

	return h
}

// Stop cancels the manager context and stops envtest. Idempotent.
func (h *EnvtestHandle) Stop() {
	if h == nil {
		return
	}
	if h.Cancel != nil {
		h.Cancel()
	}
	if h.TestEnv != nil {
		_ = h.TestEnv.Stop()
	}
}

// DefaultTimeout is the standard Eventually timeout for split-tree envtest
// specs. Increased from 15s to 30s in fire 30 of deep-review-remediation
// after observing intermittent flakes in TestPlatformControllers (the
// CredentialsResolved condition reconcile occasionally exceeded the 15s
// envelope under load — Princípio 7: flaky tests are bugs).
//
// Per CLAUDE.md global Princípio 7: "Testes devem ser determinísticos.
// Se um teste falha intermitentemente (flaky), ele é um bug e deve ser
// corrigido ou removido imediatamente."
//
// Increasing timeout is the conservative fix — doesn't mask logic bugs
// (a missing reconcile would still timeout) but absorbs CI/host
// scheduling jitter that produced the 15s flakes.
//
// Raised again 30s -> 60s on 2026-08-06 for the same reason, measured: each
// suite passes in isolation (internal/platform 3/3 green, 62-71s; the
// preview->platform pair green) but `go test ./... -p 1` intermittently blows
// one 30s Eventually — and a DIFFERENT spec each run (GitRepositoryBinding
// "credential Secret missing" one run, TenantController "PriorityClass starter
// plan idempotent" the next). A dependency regression would fail the same spec
// deterministically; a spec that only fails when other envtest control planes
// shared the host is scheduling jitter. 60s keeps a missing reconcile failing.
const DefaultTimeout = 60 * time.Second

// DefaultInterval is the standard Eventually polling interval.
const DefaultInterval = 250 * time.Millisecond
