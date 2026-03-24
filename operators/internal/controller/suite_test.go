package controller_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	// Resolve envtest binary path: KUBEBUILDER_ASSETS env var, then setup-envtest,
	// then default kubebuilder path. This ensures tests work both via `task test:unit`
	// (which sets KUBEBUILDER_ASSETS) and direct `go test` invocations. (F-06)
	binaryDir := os.Getenv("KUBEBUILDER_ASSETS")
	if binaryDir == "" {
		// Try common setup-envtest path
		home, _ := os.UserHomeDir()
		candidate := filepath.Join(home, ".local", "share", "kubebuilder-envtest", "k8s", "1.35.0-linux-amd64")
		if _, err := os.Stat(filepath.Join(candidate, "etcd")); err == nil {
			binaryDir = candidate
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

	// Multi-cluster: in envtest all three "clusters" share the same API server.
	// This validates client routing logic; physical separation is tested in E2E (Sprint 8).
	buildClient, err := client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
	runtimeClient, err := client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())

	err = controller.SetupTenantController(mgr, fakeGitProvider, fakeArgoCDProvider, buildClient, runtimeClient, controller.TenantControllerConfig{})
	Expect(err).NotTo(HaveOccurred())

	err = controller.SetupClusterCredentialController(mgr)
	Expect(err).NotTo(HaveOccurred())

	err = controller.SetupPlatformBootstrapController(mgr, nil)
	Expect(err).NotTo(HaveOccurred())

	err = controller.SetupGitRepositoryBindingController(mgr, nil)
	Expect(err).NotTo(HaveOccurred())

	err = controller.SetupEnvironmentController(mgr, runtimeClient)
	Expect(err).NotTo(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		if startErr := mgr.Start(ctx); startErr != nil {
			panic(startErr)
		}
	}()

	// Wait for manager cache to sync before running tests
	Expect(mgr.GetCache().WaitForCacheSync(ctx)).To(BeTrue())
})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
