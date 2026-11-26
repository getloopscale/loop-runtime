// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package app_test

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/operatortest"
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
		BaseDomain: "dev.getloopscale.com",
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

// Quality-Gate Phase 3 helpers — sibling pattern to tenant_test package
// (deleteTenantWithFinalizer / deleteEnvWithFinalizer in tenant_controller_test.go).
// Each clears finalizers on a CR and deletes it. Idempotent — skip if NotFound.

func deleteAppWithFinalizer(appName string) {
	app := &loopruntimev1.LoopRuntimeApplication{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: appName, Namespace: "default"}, app); err == nil {
		_ = operatortest.UpdateWithRetry(ctx, k8sClient, app, func() {
			app.Finalizers = nil
		})
		_ = k8sClient.Delete(ctx, app)
	}
}

// expectRouteCondition fetches the ExposedRoute CR identified by routeName
// in the default namespace and asserts a specific condition type appears
// with the given status. Phase 3 sibling helper to
// tenant_test.expectEnvCondition / expectTenantCondition.
func expectRouteCondition(routeName, condType string, want metav1.ConditionStatus) {
	Eventually(func(g Gomega) {
		var route loopruntimev1.ExposedRoute
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: routeName, Namespace: "default",
		}, &route)).To(Succeed())
		found := false
		for _, c := range route.Status.Conditions {
			if c.Type == condType {
				found = true
				g.Expect(c.Status).To(Equal(want))
			}
		}
		g.Expect(found).To(BeTrue(), condType+" condition should exist on ExposedRoute CR")
	}, timeout, interval).Should(Succeed())
}

// expectAppCondition fetches the LoopRuntimeApplication CR identified by appName
// in the default namespace and asserts a specific condition type appears
// with the given status.
func expectAppCondition(appName, condType string, want metav1.ConditionStatus) {
	Eventually(func(g Gomega) {
		var app loopruntimev1.LoopRuntimeApplication
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: appName, Namespace: "default",
		}, &app)).To(Succeed())
		found := false
		for _, c := range app.Status.Conditions {
			if c.Type == condType {
				found = true
				g.Expect(c.Status).To(Equal(want))
			}
		}
		g.Expect(found).To(BeTrue(), condType+" condition should exist on LoopRuntimeApplication CR")
	}, timeout, interval).Should(Succeed())
}

// withApp fetches a LoopRuntimeApplication CR by name (in default namespace) inside
// an Eventually loop and runs caller-supplied assertions against it. Phase 3
// helper: collapses ~6 inline 7-line Eventually+Get scaffolds in
// application_controller_test.go that vary only in the inner field assertion.
func withApp(appName string, assertions func(g Gomega, app *loopruntimev1.LoopRuntimeApplication)) {
	Eventually(func(g Gomega) {
		var app loopruntimev1.LoopRuntimeApplication
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: appName, Namespace: "default",
		}, &app)).To(Succeed())
		assertions(g, &app)
	}, timeout, interval).Should(Succeed())
}

// withRoute fetches an ExposedRoute CR by name (in default namespace) inside
// an Eventually loop and runs caller-supplied assertions against it.
// Phase 3 callback-style helper, sibling to withApp.
func withRoute(routeName string, assertions func(g Gomega, route *loopruntimev1.ExposedRoute)) {
	Eventually(func(g Gomega) {
		var route loopruntimev1.ExposedRoute
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: routeName, Namespace: "default",
		}, &route)).To(Succeed())
		assertions(g, &route)
	}, timeout, interval).Should(Succeed())
}

func deleteRouteWithFinalizer(routeName string) {
	route := &loopruntimev1.ExposedRoute{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: routeName, Namespace: "default"}, route); err == nil {
		_ = operatortest.UpdateWithRetry(ctx, k8sClient, route, func() {
			route.Finalizers = nil
		})
		_ = k8sClient.Delete(ctx, route)
	}
}
