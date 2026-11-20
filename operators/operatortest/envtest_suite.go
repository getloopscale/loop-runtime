// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package operatortest

import (
	"context"
	"fmt"
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
	// both routed through the tracking CellAwareFactory. Tenant suite uses this.
	EnableTenant bool

	// EnableApp wires SetupApplicationController + SetupExposureController,
	// both routed through CellAwareFactory.
	EnableApp bool

	// EnablePreview wires SetupPreviewController, routed through CellAwareFactory.
	EnablePreview bool

	// EnablePlatform wires SetupClusterCredentialController +
	// SetupGitRepositoryBindingController + SetupPlatformBootstrapController.
	EnablePlatform bool

	// BaseDomain is used by the Application controller for hostname
	// assignment. Defaults to "dev.getloopscale.com" if empty.
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

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{crdPathOrDefault(cfg.CRDPath)},
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: BinaryAssetsDirectory(),
	}
	restCfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("envtest start: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	h, err := newEnvtestHandle(ctx, cancel, testEnv, restCfg)
	if err != nil {
		cancel()
		_ = testEnv.Stop()
		t.Fatalf("envtest handle: %v", err)
	}

	if err := setupControllers(h, cfg); err != nil {
		h.Stop()
		t.Fatalf("%v", err)
	}

	go func() {
		if startErr := h.Manager.Start(ctx); startErr != nil {
			// Test will fail later via Eventually timeouts; logging here
			// would be lost in test output anyway.
			_ = startErr
		}
	}()

	if !h.Manager.GetCache().WaitForCacheSync(ctx) {
		h.Stop()
		t.Fatalf("cache sync failed")
	}

	return h
}

// BinaryAssetsDirectory is where the envtest binaries (kube-apiserver, etcd)
// live on this machine, for a test that builds its own envtest.Environment
// instead of going through StartEnvtest.
func BinaryAssetsDirectory() string {
	return envtestBinaryDir(os.Getenv("KUBEBUILDER_ASSETS"), userHomeDir())
}

// userHomeDir returns the user's home directory, or "" when it is unknown
// (envtestBinaryDir then finds nothing and envtest reports the missing
// binaries itself).
func userHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// envtestBinaryDir resolves the envtest binaries: KUBEBUILDER_ASSETS when
// set, otherwise the newest setup-envtest install under home that carries an
// etcd binary. "" leaves the choice to envtest.
func envtestBinaryDir(kubebuilderAssets, home string) string {
	if kubebuilderAssets != "" {
		return kubebuilderAssets
	}
	if home == "" {
		return ""
	}
	base := filepath.Join(home, ".local", "share", "kubebuilder-envtest", "k8s")
	entries, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	for i := len(entries) - 1; i >= 0; i-- {
		candidate := filepath.Join(base, entries[i].Name())
		if _, statErr := os.Stat(filepath.Join(candidate, "etcd")); statErr == nil {
			return candidate
		}
	}
	return ""
}

// crdPathOrDefault is the CRD directory relative to a split package's tests.
func crdPathOrDefault(crdPath string) string {
	if crdPath != "" {
		return crdPath
	}
	return filepath.Join("..", "..", "config", "crd", "bases")
}

// baseDomainOrDefault is the hostname base domain for the Application controller.
func baseDomainOrDefault(baseDomain string) string {
	if baseDomain != "" {
		return baseDomain
	}
	return "dev.getloopscale.com"
}

// newEnvtestHandle builds the scheme, clients, manager and fakes a suite
// shares. Every cell-aware controller reaches the runtime cluster through the
// tracking factory, so a spec can assert via h.CellAwareFactory.Calls() which
// cell each write was routed to. envtest has one API server, so every cell
// resolves to it; the routing decision is what is observed.
func newEnvtestHandle(ctx context.Context, cancel context.CancelFunc, testEnv *envtest.Environment, restCfg *rest.Config) (*EnvtestHandle, error) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("client-go scheme: %w", err)
	}
	if err := loopzerov1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("loopzerov1 scheme: %w", err)
	}

	newClient := func(role string) (client.Client, error) {
		c, err := client.New(restCfg, client.Options{Scheme: scheme})
		if err != nil {
			return nil, fmt.Errorf("%s client: %w", role, err)
		}
		return c, nil
	}
	k8sClient, err := newClient("test")
	if err != nil {
		return nil, err
	}
	buildClient, err := newClient("build")
	if err != nil {
		return nil, err
	}
	runtimeClient, err := newClient("runtime")
	if err != nil {
		return nil, err
	}

	mgr, err := ctrl.NewManager(restCfg, ctrl.Options{
		Scheme:  scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	if err != nil {
		return nil, fmt.Errorf("manager: %w", err)
	}

	return &EnvtestHandle{
		Cfg:              restCfg,
		K8sClient:        k8sClient,
		TestEnv:          testEnv,
		Scheme:           scheme,
		Manager:          mgr,
		Ctx:              ctx,
		Cancel:           cancel,
		FakeGit:          NewFakeGitProvisioner(),
		FakeArgoCD:       NewFakeArgoCDProvisioner(),
		FakeMinIO:        NewFakeMinIOProvisioner(),
		BuildClient:      buildClient,
		RuntimeCli:       runtimeClient,
		CellAwareFactory: NewFakeRuntimeClientFactory(runtimeClient),
	}, nil
}

// controllerGroup is one EnvtestConfig switch and the controllers it wires.
type controllerGroup struct {
	enabled bool
	setups  []namedSetup
}

type namedSetup struct {
	name  string
	setup func() error
}

// setupControllers registers the controller groups cfg enables.
func setupControllers(h *EnvtestHandle, cfg EnvtestConfig) error {
	mgr := h.Manager
	baseDomain := baseDomainOrDefault(cfg.BaseDomain)
	groups := []controllerGroup{
		{cfg.EnableTenant, []namedSetup{
			{"SetupTenantController", func() error {
				return SetupTenantController(mgr, h.FakeGit, h.FakeArgoCD, h.BuildClient, h.CellAwareFactory)
			}},
			{"SetupEnvironmentControllerWithFactory", func() error {
				return environment.SetupEnvironmentControllerWithFactory(mgr, h.CellAwareFactory)
			}},
		}},
		{cfg.EnableApp, []namedSetup{
			{"SetupApplicationController", func() error { return SetupApplicationController(mgr, baseDomain, h.CellAwareFactory) }},
			{"SetupExposureController", func() error { return SetupExposureController(mgr, h.CellAwareFactory) }},
		}},
		{cfg.EnablePreview, []namedSetup{
			{"SetupPreviewController", func() error { return SetupPreviewController(mgr, h.CellAwareFactory) }},
		}},
		{cfg.EnablePlatform, []namedSetup{
			{"SetupClusterCredentialController", func() error { return SetupClusterCredentialController(mgr) }},
			{"SetupGitRepositoryBindingController", func() error { return SetupGitRepositoryBindingController(mgr, nil) }},
			{"SetupPlatformBootstrapController", func() error { return SetupPlatformBootstrapController(mgr, nil) }},
		}},
	}
	for _, g := range groups {
		if !g.enabled {
			continue
		}
		for _, s := range g.setups {
			if err := s.setup(); err != nil {
				return fmt.Errorf("%s: %w", s.name, err)
			}
		}
	}
	return nil
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
// envelope under load — Principle 7: flaky tests are bugs).
//
// Per the global CLAUDE.md, Principle 7: tests must be deterministic, and a
// test that fails intermittently is a bug to fix or remove immediately.
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
