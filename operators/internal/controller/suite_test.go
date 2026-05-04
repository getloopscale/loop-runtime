// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/constants"
	"github.com/loopzero/loopzero/operators/internal/controller"
)

const (
	timeout  = 15 * time.Second
	interval = 250 * time.Millisecond
)

var (
	cfg            *rest.Config
	k8sClient      client.Client
	testEnv        *envtest.Environment
	scheme         *runtime.Scheme
	ctx            context.Context
	cancel         context.CancelFunc
	fakeGitProvider    *fakeGiteaProvisioner
	fakeArgoCDProvider *fakeArgoCDProvisioner
	fakeMinIOProvider  *fakeMinIOProvisioner
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.Background())

	By("bootstrapping test environment")
	// Resolve envtest binary path: KUBEBUILDER_ASSETS env var, then setup-envtest,
	// then default kubebuilder path. This ensures tests work both via `task test:unit`
	// (which sets KUBEBUILDER_ASSETS) and direct `go test` invocations. (F-06)
	binaryDir := os.Getenv("KUBEBUILDER_ASSETS")
	if binaryDir == "" {
		// Auto-discover latest envtest binary under the standard setup-envtest path.
		// This avoids hardcoding a specific Kubernetes version.
		home, _ := os.UserHomeDir()
		envtestBase := filepath.Join(home, ".local", "share", "kubebuilder-envtest", "k8s")
		if entries, err := os.ReadDir(envtestBase); err == nil {
			// Iterate in reverse to prefer the latest version (lexicographic order).
			for i := len(entries) - 1; i >= 0; i-- {
				candidate := filepath.Join(envtestBase, entries[i].Name())
				if _, statErr := os.Stat(filepath.Join(candidate, "etcd")); statErr == nil {
					binaryDir = candidate
					break
				}
			}
		}
	}
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: binaryDir,
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	scheme = runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	Expect(loopzerov1.AddToScheme(scheme)).To(Succeed())

	// Use a direct (non-cached) client for test assertions.
	// This ensures reads go straight to the API server.
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	})
	Expect(err).NotTo(HaveOccurred())

	fakeGitProvider = newFakeGiteaProvisioner()
	fakeArgoCDProvider = newFakeArgoCDProvisioner()
	fakeMinIOProvider = newFakeMinIOProvisioner()

	// Multi-cluster: in envtest all three "clusters" share the same API server.
	// This validates client routing logic; physical separation is tested in E2E (Sprint 8).
	buildClient, err := client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
	runtimeClient, err := client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())

	err = controller.SetupTenantController(mgr, fakeGitProvider, fakeArgoCDProvider, buildClient, runtimeClient, controller.TenantControllerConfig{
		MinIOProvisioner: fakeMinIOProvider,
	})
	Expect(err).NotTo(HaveOccurred())

	err = controller.SetupClusterCredentialController(mgr, constants.DefaultArgoCDNamespace)
	Expect(err).NotTo(HaveOccurred())

	err = controller.SetupPlatformBootstrapController(mgr, nil)
	Expect(err).NotTo(HaveOccurred())

	err = controller.SetupGitRepositoryBindingController(mgr, nil)
	Expect(err).NotTo(HaveOccurred())

	err = controller.SetupEnvironmentControllerWithFactory(mgr, runtimeClient, nil)
	Expect(err).NotTo(HaveOccurred())

	err = controller.SetupApplicationController(mgr, "dev.loopzero.example")
	Expect(err).NotTo(HaveOccurred())

	err = controller.SetupExposureController(mgr, controller.ExposureControllerConfig{
		AllowProbeURLOverride: true, // test environment only — allows probe URL injection via annotation
	})
	Expect(err).NotTo(HaveOccurred())

	// Sprint 5 / T1.2 + T1.3: PreviewController envtest registration.
	// Re-uses runtimeClient (envtest pretends both clusters share an API
	// server) and a stable BaseDomain for URL assertions.
	err = controller.SetupPreviewController(mgr, runtimeClient, "preview-test.example")
	Expect(err).NotTo(HaveOccurred())

	// ADR-0006: LoopZeroBuild and LoopZeroRelease CRDs removed.
	// Build state owned by PostgreSQL, Release state owned by PostgreSQL.
	// Controllers were stubs — no reconciliation logic to preserve.

	go func() {
		defer GinkgoRecover()
		if startErr := mgr.Start(ctx); startErr != nil {
			panic(startErr)
		}
	}()

	// Wait for manager cache to sync before running tests
	Expect(mgr.GetCache().WaitForCacheSync(ctx)).To(BeTrue())
})

// Clear injected errors between test specs to prevent error state leakage.
// We only clear errors (not data) because some test Contexts share provisioner
// state across multiple It blocks within the same BeforeAll/AfterAll scope.
var _ = BeforeEach(func() {
	if fakeGitProvider != nil {
		fakeGitProvider.SetErr(nil)
	}
	if fakeArgoCDProvider != nil {
		fakeArgoCDProvider.Mu.Lock()
		fakeArgoCDProvider.EnsureErr = nil
		fakeArgoCDProvider.DeleteErr = nil
		fakeArgoCDProvider.Mu.Unlock()
	}
	if fakeMinIOProvider != nil {
		fakeMinIOProvider.ClearErrors()
	}
})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

// --- Shared test helpers ---

// findCondition returns the condition with the given type, or nil.
func findCondition(conds []metav1.Condition, conditionType string) *metav1.Condition {
	for i, c := range conds {
		if c.Type == conditionType {
			return &conds[i]
		}
	}
	return nil
}
