// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package app_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/conditions"
	"github.com/getloopscale/loopzero/operators/operatortest"
	"github.com/getloopscale/loopzero/pkg/naming"
)

func newLoopZeroApplicationCR(name, tenantRef, project, env, serviceName string) *loopzerov1.LoopZeroApplication {
	return &loopzerov1.LoopZeroApplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: loopzerov1.LoopZeroApplicationSpec{
			TenantRef:   tenantRef,
			Project:     project,
			Environment: env,
			ServiceName: serviceName,
			AppName:     serviceName,
			Replicas:    1,
			ServicePort: 80,
		},
	}
}

var _ = Describe("ApplicationController", Ordered, func() {

	Context("when a new LoopZeroApplication CR is created without hostname", func() {
		const (
			appName     = "app-test-myapi"
			tenantRef   = "tn-acme"
			project     = "myapi"
			env         = "production"
			serviceName = "myapi-svc"

			routedHostname = "tn-acme-myapi-myapi-svc-production.dev.getloopscale.com"
		)

		BeforeAll(func() {
			app := newLoopZeroApplicationCR(appName, tenantRef, project, env, serviceName)
			Expect(k8sClient.Create(ctx, app)).To(Succeed())
		})

		It("should set the routed hostname via naming.AppHostname", func() {
			// naming.AppHostname("tn-acme", "myapi", "myapi-svc", "production", "dev.getloopscale.com"),
			// the Host rule the IngressRoute renderer emits for this app.
			withApp(appName, func(g Gomega, app *loopzerov1.LoopZeroApplication) {
				g.Expect(app.Spec.Hostname).To(Equal(routedHostname))
			})
		})

		It("should set status.url", func() {
			withApp(appName, func(g Gomega, app *loopzerov1.LoopZeroApplication) {
				g.Expect(app.Status.URL).To(Equal("https://" + routedHostname))
			})
		})

		It("should create an ExposedRoute CR owned by the Application", func() {
			Eventually(func(g Gomega) {
				var route loopzerov1.ExposedRoute
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: appName + "-route", Namespace: "default",
				}, &route)).To(Succeed())
				g.Expect(route.Spec.ApplicationRef).To(Equal(appName))
				g.Expect(route.Spec.Hostname).To(Equal(routedHostname))
				g.Expect(route.Spec.ServiceRef.Name).To(Equal(serviceName))
				g.Expect(route.Spec.ServiceRef.Port).To(Equal(int32(80)))

				// Verify ownerReference
				g.Expect(route.OwnerReferences).To(HaveLen(1))
				g.Expect(route.OwnerReferences[0].Name).To(Equal(appName))
				g.Expect(route.OwnerReferences[0].Kind).To(Equal("LoopZeroApplication"))
			}, timeout, interval).Should(Succeed())
		})

		It("should set phase to Progressing initially", func() {
			// Without actual endpoints, phase stays Progressing
			withApp(appName, func(g Gomega, app *loopzerov1.LoopZeroApplication) {
				g.Expect(app.Status.Phase).To(Equal(loopzerov1.ApplicationPhaseProgressing))
			})
		})

		It("should set ServiceResolved condition to True", func() {
			expectAppCondition(appName, conditions.TypeServiceResolved, metav1.ConditionTrue)
		})

		It("should set observedGeneration", func() {
			withApp(appName, func(g Gomega, app *loopzerov1.LoopZeroApplication) {
				g.Expect(app.Status.ObservedGeneration).To(Equal(app.Generation))
			})
		})

		AfterAll(func() {
			deleteAppWithFinalizer(appName)
			// Also clean up the ExposedRoute (which now has a finalizer too)
			route := &loopzerov1.ExposedRoute{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: appName + "-route", Namespace: "default"}, route); err == nil {
				_ = operatortest.UpdateWithRetry(ctx, k8sClient, route, func() {
					route.Finalizers = nil
				})
				_ = k8sClient.Delete(ctx, route)
			}
		})
	})

	Context("when a LoopZeroApplication CR has hostname already set", func() {
		const (
			appName     = "app-explicit-host"
			tenantRef   = "tn-beta"
			project     = "webapp"
			env         = "staging"
			serviceName = "webapp-svc"
			hostname    = "custom.example.com"
		)

		BeforeAll(func() {
			app := newLoopZeroApplicationCR(appName, tenantRef, project, env, serviceName)
			app.Spec.Hostname = hostname
			Expect(k8sClient.Create(ctx, app)).To(Succeed())
		})

		It("should preserve the existing hostname", func() {
			withApp(appName, func(g Gomega, app *loopzerov1.LoopZeroApplication) {
				g.Expect(app.Spec.Hostname).To(Equal(hostname))
			})
		})

		It("should set URL from the explicit hostname", func() {
			withApp(appName, func(g Gomega, app *loopzerov1.LoopZeroApplication) {
				g.Expect(app.Status.URL).To(Equal("https://custom.example.com"))
			})
		})

		AfterAll(func() {
			deleteAppWithFinalizer(appName)
			// Also clean up the ExposedRoute (which now has a finalizer too)
			route := &loopzerov1.ExposedRoute{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: appName + "-route", Namespace: "default"}, route); err == nil {
				_ = operatortest.UpdateWithRetry(ctx, k8sClient, route, func() {
					route.Finalizers = nil
				})
				_ = k8sClient.Delete(ctx, route)
			}
		})
	})

	Context("when a CR written before spec.appName still holds the old default hostname", func() {
		const (
			appName     = "app-legacy-host"
			tenantRef   = "tn-legacy"
			project     = "shop"
			env         = "production"
			serviceName = "web"

			legacyHostname = "shop-production.dev.getloopscale.com"
			routedHostname = "tn-legacy-shop-web-production.dev.getloopscale.com"
		)

		BeforeAll(func() {
			app := newLoopZeroApplicationCR(appName, tenantRef, project, env, serviceName)
			app.Spec.AppName = ""
			app.Spec.Hostname = legacyHostname
			app.Labels = map[string]string{naming.LabelApp: serviceName}
			Expect(k8sClient.Create(ctx, app)).To(Succeed())
		})

		It("should replace the old default with the routed hostname, reading the app from its label", func() {
			withApp(appName, func(g Gomega, app *loopzerov1.LoopZeroApplication) {
				g.Expect(app.Spec.Hostname).To(Equal(routedHostname))
				g.Expect(app.Status.URL).To(Equal("https://" + routedHostname))
			})
		})

		AfterAll(func() {
			deleteAppWithFinalizer(appName)
			route := &loopzerov1.ExposedRoute{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: appName + "-route", Namespace: "default"}, route); err == nil {
				_ = operatortest.UpdateWithRetry(ctx, k8sClient, route, func() {
					route.Finalizers = nil
				})
				_ = k8sClient.Delete(ctx, route)
			}
		})
	})

	Context("finalizer management", func() {
		const (
			appName     = "app-fin-test"
			tenantRef   = "tn-fin"
			project     = "finsvc"
			env         = "production"
			serviceName = "finsvc-main"
		)

		BeforeAll(func() {
			app := newLoopZeroApplicationCR(appName, tenantRef, project, env, serviceName)
			Expect(k8sClient.Create(ctx, app)).To(Succeed())

			// Wait for initial reconcile
			withApp(appName, func(g Gomega, app *loopzerov1.LoopZeroApplication) {
				g.Expect(app.Status.ObservedGeneration).To(Equal(app.Generation))
			})
		})

		It("should add the app-cleanup finalizer on creation", func() {
			withApp(appName, func(g Gomega, app *loopzerov1.LoopZeroApplication) {
				g.Expect(app.Finalizers).To(ContainElement("loopzero.io/app-cleanup"))
			})
		})

		AfterAll(func() {
			deleteAppWithFinalizer(appName)
		})
	})

	Context("deletion via finalizer — waits for child ExposedRoutes", func() {
		const (
			appName     = "app-del-test"
			tenantRef   = "tn-del"
			project     = "delsvc"
			env         = "production"
			serviceName = "delsvc-main"
		)

		It("should wait for ExposedRoutes to be gone, then remove finalizer", func() {
			app := newLoopZeroApplicationCR(appName, tenantRef, project, env, serviceName)
			Expect(k8sClient.Create(ctx, app)).To(Succeed())

			// Wait for Active + ExposedRoute created
			Eventually(func(g Gomega) {
				var app loopzerov1.LoopZeroApplication
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: appName, Namespace: "default",
				}, &app)).To(Succeed())
				g.Expect(app.Status.ObservedGeneration).To(Equal(app.Generation))
				g.Expect(app.Finalizers).To(ContainElement("loopzero.io/app-cleanup"))
			}, timeout, interval).Should(Succeed())

			// Verify ExposedRoute exists
			Eventually(func(g Gomega) {
				var route loopzerov1.ExposedRoute
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: appName + "-route", Namespace: "default",
				}, &route)).To(Succeed())
			}, timeout, interval).Should(Succeed())

			// Delete the application
			var toDelete loopzerov1.LoopZeroApplication
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: appName, Namespace: "default",
			}, &toDelete)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &toDelete)).To(Succeed())

			// In envtest, ownerRef cascade DOES mark owned resources for deletion.
			// The ExposedRoute has ownerRef cascade + the route-cleanup finalizer.
			// The ExposureController should detect deletion and remove its finalizer.
			// However, envtest GC may not fully process cascade, so we assist by
			// explicitly deleting the route and then letting the controller remove its finalizer.
			Eventually(func(g Gomega) {
				var route loopzerov1.ExposedRoute
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: appName + "-route", Namespace: "default",
				}, &route)
				if err != nil {
					return // Already gone
				}
				// In envtest, ownerRef cascade may not mark it for deletion.
				// Delete it explicitly to trigger the ExposureController's delete path.
				if route.DeletionTimestamp.IsZero() {
					_ = k8sClient.Delete(ctx, &route)
				}
			}, timeout, interval).Should(Succeed())

			// The ExposureController will remove the route-cleanup finalizer on deletion.
			// Wait for the ExposedRoute to be fully gone.
			Eventually(func(g Gomega) {
				var route loopzerov1.ExposedRoute
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: appName + "-route", Namespace: "default",
				}, &route)
				if err == nil {
					// Still exists — check finalizer is gone (controller should remove it)
					g.Expect(route.Finalizers).ToNot(ContainElement("loopzero.io/route-cleanup"),
						"ExposureController should remove the finalizer on deletion")
				}
			}, timeout, interval).Should(Succeed())

			// Application should eventually be fully deleted
			Eventually(func(g Gomega) {
				var deleted loopzerov1.LoopZeroApplication
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: appName, Namespace: "default",
				}, &deleted)
				if err == nil {
					// Still exists — check finalizer is gone
					g.Expect(deleted.Finalizers).ToNot(ContainElement("loopzero.io/app-cleanup"))
				}
				// Either NotFound or finalizer removed is success
			}, 30*time.Second, interval).Should(Succeed())
		})
	})

	Context("idempotency — re-reconcile does not duplicate ExposedRoute", func() {
		const (
			appName     = "app-idemp-test"
			tenantRef   = "tn-idemp"
			project     = "svc"
			env         = "production"
			serviceName = "svc-main"
		)

		BeforeAll(func() {
			app := newLoopZeroApplicationCR(appName, tenantRef, project, env, serviceName)
			Expect(k8sClient.Create(ctx, app)).To(Succeed())

			// Wait for initial reconcile
			Eventually(func(g Gomega) {
				var app loopzerov1.LoopZeroApplication
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: appName, Namespace: "default",
				}, &app)).To(Succeed())
				g.Expect(app.Status.ObservedGeneration).To(Equal(app.Generation))
			}, timeout, interval).Should(Succeed())
		})

		It("should have exactly one ExposedRoute after re-reconcile", func() {
			// Trigger re-reconcile via annotation update
			Eventually(func(g Gomega) {
				var app loopzerov1.LoopZeroApplication
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: appName, Namespace: "default",
				}, &app)).To(Succeed())
				if app.Annotations == nil {
					app.Annotations = make(map[string]string)
				}
				app.Annotations["test.loopzero.io/retrigger"] = "true"
				g.Expect(k8sClient.Update(ctx, &app)).To(Succeed())
			}, timeout, interval).Should(Succeed())

			// Wait for re-reconcile
			Eventually(func(g Gomega) {
				var app loopzerov1.LoopZeroApplication
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: appName, Namespace: "default",
				}, &app)).To(Succeed())
				g.Expect(app.Status.ObservedGeneration).To(Equal(app.Generation))
			}, timeout, interval).Should(Succeed())

			// Count ExposedRoutes in namespace
			var routes loopzerov1.ExposedRouteList
			Expect(k8sClient.List(ctx, &routes)).To(Succeed())
			count := 0
			for _, r := range routes.Items {
				if r.Spec.ApplicationRef == appName {
					count++
				}
			}
			Expect(count).To(Equal(1), "should have exactly one ExposedRoute for this application")
		})

		AfterAll(func() {
			deleteAppWithFinalizer(appName)
			// Also clean up the ExposedRoute (which now has a finalizer too)
			route := &loopzerov1.ExposedRoute{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: appName + "-route", Namespace: "default"}, route); err == nil {
				_ = operatortest.UpdateWithRetry(ctx, k8sClient, route, func() {
					route.Finalizers = nil
				})
				_ = k8sClient.Delete(ctx, route)
			}
		})
	})

	// =============================================================================
	// Cross-tenant tenant-scope propagation — audit M10 F2 / §6.E.4b
	// =============================================================================
	//
	// Asserts that the child ExposedRoute created by reconcileExposedRoute
	// carries the parent's tenant/project/environment labels — and that
	// two parents owned by different tenants produce children with
	// distinct, NON-cross-contaminated labels. Pre-fix: ExposedRoute had
	// no tenant label at all, so Loki/Prometheus queries scoped by
	// `loopzero.io/tenant` silently returned empty.
	Context("tenant scope propagation to child ExposedRoute (audit §6.E.4b)", func() {
		const (
			appA = "xt-app-a"
			appB = "xt-app-b"
			tnA  = "tn-tenant-a"
			tnB  = "tn-tenant-b"
			proj = "shared-project"
			envP = "production"
			svcN = "shared-svc"
		)

		BeforeAll(func() {
			Expect(k8sClient.Create(ctx, newLoopZeroApplicationCR(appA, tnA, proj, envP, svcN))).To(Succeed())
			Expect(k8sClient.Create(ctx, newLoopZeroApplicationCR(appB, tnB, proj, envP, svcN))).To(Succeed())
		})

		It("should label ExposedRoute child with parent's tenant/project/env (tenant A)", func() {
			Eventually(func(g Gomega) {
				var route loopzerov1.ExposedRoute
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: appA + "-route", Namespace: "default",
				}, &route)).To(Succeed())
				g.Expect(route.Labels).NotTo(BeNil(), "child must carry labels")
				g.Expect(route.Labels["loopzero.io/tenant"]).To(Equal(tnA),
					"child ExposedRoute must carry parent tenant label")
				g.Expect(route.Labels["loopzero.io/project"]).To(Equal(proj))
				g.Expect(route.Labels["loopzero.io/environment"]).To(Equal(envP))
			}, timeout, interval).Should(Succeed())
		})

		It("should label tenant B's child with tenant B (no cross-contamination)", func() {
			Eventually(func(g Gomega) {
				var route loopzerov1.ExposedRoute
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: appB + "-route", Namespace: "default",
				}, &route)).To(Succeed())
				g.Expect(route.Labels["loopzero.io/tenant"]).To(Equal(tnB),
					"child ExposedRoute for tenant B must NOT carry tenant A's label")
			}, timeout, interval).Should(Succeed())
		})

		It("should NEVER have a child of tenant A labeled as tenant B (adversarial)", func() {
			// Adversarial: list every ExposedRoute and assert that no route
			// owned by appA has tenant B's label (and vice-versa). This is
			// the inverse of the per-app positive tests — it catches a
			// reconcile bug where the controller might apply labels based
			// on a global variable, env var, or process-wide tenant state.
			var routes loopzerov1.ExposedRouteList
			Expect(k8sClient.List(ctx, &routes)).To(Succeed())
			for _, r := range routes.Items {
				switch r.Spec.ApplicationRef {
				case appA:
					Expect(r.Labels["loopzero.io/tenant"]).To(Equal(tnA))
				case appB:
					Expect(r.Labels["loopzero.io/tenant"]).To(Equal(tnB))
				}
			}
		})

		It("re-applies tenant labels if a manual edit strips them", func() {
			// Simulate an attacker / operator-error that strips the tenant
			// label off the child. The controller's CreateOrUpdate path
			// must converge the label back to the parent's tenantRef on
			// the next reconcile (triggered by any spec touch on parent).
			var route loopzerov1.ExposedRoute
			routeKey := types.NamespacedName{Name: appA + "-route", Namespace: "default"}
			Expect(k8sClient.Get(ctx, routeKey, &route)).To(Succeed())

			// Strip the tenant label and update.
			Expect(operatortest.UpdateWithRetry(ctx, k8sClient, &route, func() {
				delete(route.Labels, "loopzero.io/tenant")
			})).To(Succeed())

			// Trigger reconcile by touching the parent (bump replicas).
			var parent loopzerov1.LoopZeroApplication
			parentKey := types.NamespacedName{Name: appA, Namespace: "default"}
			Expect(k8sClient.Get(ctx, parentKey, &parent)).To(Succeed())
			Expect(operatortest.UpdateWithRetry(ctx, k8sClient, &parent, func() {
				parent.Spec.Replicas = parent.Spec.Replicas + 1
			})).To(Succeed())

			// Eventually, the controller restores the label.
			Eventually(func(g Gomega) {
				var got loopzerov1.ExposedRoute
				g.Expect(k8sClient.Get(ctx, routeKey, &got)).To(Succeed())
				g.Expect(got.Labels["loopzero.io/tenant"]).To(Equal(tnA),
					"controller must converge tenant label back to parent's tenantRef")
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			for _, name := range []string{appA, appB} {
				appObj := &loopzerov1.LoopZeroApplication{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, appObj); err == nil {
					_ = operatortest.UpdateWithRetry(ctx, k8sClient, appObj, func() {
						appObj.Finalizers = nil
					})
					_ = k8sClient.Delete(ctx, appObj)
				}
				route := &loopzerov1.ExposedRoute{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name + "-route", Namespace: "default"}, route); err == nil {
					_ = operatortest.UpdateWithRetry(ctx, k8sClient, route, func() {
						route.Finalizers = nil
					})
					_ = k8sClient.Delete(ctx, route)
				}
			}
		})
	})
})
