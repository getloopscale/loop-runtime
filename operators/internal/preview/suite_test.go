// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package preview_test

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	scheme    *runtime.Scheme
	ctx       context.Context
	cancel    context.CancelFunc
	// cellFactory is the factory the controllers resolve runtime clients
	// through; specs assert which cell each preview was routed to.
	cellFactory *operatortest.FakeRuntimeClientFactory
)

func TestPreviewControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Preview Controller Suite")
}

var _ = BeforeSuite(func() {
	h := operatortest.StartEnvtest(GinkgoT(), operatortest.EnvtestConfig{
		CRDPath:       "../../config/crd/bases",
		EnablePreview: true,
		// Tenant is needed because PreviewEnvironment specs may reference
		// existing Tenant CRs for cell-aware lookup.
		EnableTenant: true,
		BaseDomain:   "preview-test.example",
	})
	cfg = h.Cfg
	k8sClient = h.K8sClient
	testEnv = h.TestEnv
	scheme = h.Scheme
	ctx = h.Ctx
	cancel = h.Cancel
	cellFactory = h.CellAwareFactory
})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	Expect(testEnv.Stop()).To(Succeed())
})

// findCondition returns the condition with the given type, or nil.
func findCondition(conds []metav1.Condition, conditionType string) *metav1.Condition {
	for i, c := range conds {
		if c.Type == conditionType {
			return &conds[i]
		}
	}
	return nil
}
