// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package app_test

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/loopzero/loopzero/operators/operatortest"
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
)

func TestAppControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "App Controller Suite")
}

var _ = BeforeSuite(func() {
	h := operatortest.StartEnvtest(GinkgoT(), operatortest.EnvtestConfig{
		CRDPath:    "../../config/crd/bases",
		EnableApp:  true,
		BaseDomain: "dev.loopzero.example",
	})
	cfg = h.Cfg
	k8sClient = h.K8sClient
	testEnv = h.TestEnv
	scheme = h.Scheme
	ctx = h.Ctx
	cancel = h.Cancel
})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	Expect(testEnv.Stop()).To(Succeed())
})

// Quality-Gate Phase 2: findCondition helper removed (no callers).
