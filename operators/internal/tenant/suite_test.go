// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant_test

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/getloopscale/loopzero/operators/operatortest"
)

const (
	timeout  = operatortest.DefaultTimeout
	interval = operatortest.DefaultInterval
)

// Per-package envtest globals — populated by BeforeSuite via
// operatortest.StartEnvtest. Mirror the legacy monolith naming so test
// specs migrate with minimal edits.
var (
	cfg                *rest.Config
	k8sClient          client.Client
	testEnv            *envtest.Environment
	scheme             *runtime.Scheme
	ctx                context.Context
	cancel             context.CancelFunc
	fakeGitProvider    *operatortest.FakeGitProvisioner
	fakeArgoCDProvider *operatortest.FakeArgoCDProvisioner
	fakeMinIOProvider  *operatortest.FakeMinIOProvisioner
	// cellFactory is the factory the Tenant and Environment controllers
	// resolve runtime clients through; specs assert which cell was used.
	cellFactory *operatortest.FakeRuntimeClientFactory
)

func TestTenantControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Tenant Controller Suite")
}

var _ = BeforeSuite(func() {
	h := operatortest.StartEnvtest(GinkgoT(), operatortest.EnvtestConfig{
		CRDPath:      "../../config/crd/bases",
		EnableTenant: true,
		// Enable App + Preview + Platform too so envtest covers the full
		// reconcile graph touched by tenant integration tests (e.g., a
		// Tenant create cascades into Environment + Application reconciles).
		EnableApp:      true,
		EnablePreview:  true,
		EnablePlatform: true,
		BaseDomain:     "dev.loopzero.example",
	})
	cfg = h.Cfg
	k8sClient = h.K8sClient
	testEnv = h.TestEnv
	scheme = h.Scheme
	ctx = h.Ctx
	cancel = h.Cancel
	fakeGitProvider = h.FakeGit
	fakeArgoCDProvider = h.FakeArgoCD
	fakeMinIOProvider = h.FakeMinIO
	cellFactory = h.CellAwareFactory
})

// Clear injected errors between test specs (preserves data state).
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
	Expect(testEnv.Stop()).To(Succeed())
})

// Quality-Gate Phase 2: findCondition helper removed (no callers).

// silence unused-import warnings for the time package — referenced by
// envtest specs that use Eventually with explicit durations.
var _ = time.Second
