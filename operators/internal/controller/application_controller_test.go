package controller_test

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/conditions"
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
		)

		BeforeAll(func() {
			app := newLoopZeroApplicationCR(appName, tenantRef, project, env, serviceName)
			Expect(k8sClient.Create(ctx, app)).To(Succeed())
		})

		It("should set hostname via naming.Hostname", func() {
			Eventually(func(g Gomega) {
				var app loopzerov1.LoopZeroApplication
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: appName, Namespace: "default",
				}, &app)).To(Succeed())
				// naming.Hostname("myapi", "production", "dev.loopzero.example") => "myapi-production.dev.loopzero.example"
				g.Expect(app.Spec.Hostname).To(Equal("myapi-production.dev.loopzero.example"))
			}, timeout, interval).Should(Succeed())
		})

		It("should set status.url", func() {
			Eventually(func(g Gomega) {
				var app loopzerov1.LoopZeroApplication
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: appName, Namespace: "default",
				}, &app)).To(Succeed())
				g.Expect(app.Status.URL).To(Equal("https://myapi-production.dev.loopzero.example"))
			}, timeout, interval).Should(Succeed())
		})

		It("should create an ExposedRoute CR owned by the Application", func() {
			Eventually(func(g Gomega) {
				var route loopzerov1.ExposedRoute
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: appName + "-route", Namespace: "default",
				}, &route)).To(Succeed())
				g.Expect(route.Spec.ApplicationRef).To(Equal(appName))
				g.Expect(route.Spec.Hostname).To(Equal("myapi-production.dev.loopzero.example"))
				g.Expect(route.Spec.ServiceRef.Name).To(Equal(serviceName))
				g.Expect(route.Spec.ServiceRef.Port).To(Equal(int32(80)))

				// Verify ownerReference
				g.Expect(route.OwnerReferences).To(HaveLen(1))
				g.Expect(route.OwnerReferences[0].Name).To(Equal(appName))
				g.Expect(route.OwnerReferences[0].Kind).To(Equal("LoopZeroApplication"))
			}, timeout, interval).Should(Succeed())
		})

		It("should set phase to Progressing initially", func() {
			Eventually(func(g Gomega) {
				var app loopzerov1.LoopZeroApplication
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: appName, Namespace: "default",
				}, &app)).To(Succeed())
				// Without actual endpoints, phase stays Progressing
				g.Expect(app.Status.Phase).To(Equal(loopzerov1.ApplicationPhaseProgressing))
			}, timeout, interval).Should(Succeed())
		})

		It("should set ServiceResolved condition to True", func() {
			Eventually(func(g Gomega) {
				var app loopzerov1.LoopZeroApplication
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: appName, Namespace: "default",
				}, &app)).To(Succeed())
				found := false
				for _, c := range app.Status.Conditions {
					if c.Type == conditions.TypeServiceResolved {
						found = true
						g.Expect(c.Status).To(Equal(metav1.ConditionTrue),
							fmt.Sprintf("condition %s should be True", conditions.TypeServiceResolved))
					}
				}
				g.Expect(found).To(BeTrue(), "ServiceResolved condition should exist")
			}, timeout, interval).Should(Succeed())
		})

		It("should set observedGeneration", func() {
			Eventually(func(g Gomega) {
				var app loopzerov1.LoopZeroApplication
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: appName, Namespace: "default",
				}, &app)).To(Succeed())
				g.Expect(app.Status.ObservedGeneration).To(Equal(app.Generation))
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			app := &loopzerov1.LoopZeroApplication{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: appName, Namespace: "default"}, app); err == nil {
				app.Finalizers = nil
				_ = k8sClient.Update(ctx, app)
				_ = k8sClient.Delete(ctx, app)
			}
			// Also clean up the ExposedRoute (which now has a finalizer too)
			route := &loopzerov1.ExposedRoute{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: appName + "-route", Namespace: "default"}, route); err == nil {
				route.Finalizers = nil
				_ = k8sClient.Update(ctx, route)
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
			Eventually(func(g Gomega) {
				var app loopzerov1.LoopZeroApplication
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: appName, Namespace: "default",
				}, &app)).To(Succeed())
				g.Expect(app.Spec.Hostname).To(Equal(hostname))
			}, timeout, interval).Should(Succeed())
		})

		It("should set URL from the explicit hostname", func() {
			Eventually(func(g Gomega) {
				var app loopzerov1.LoopZeroApplication
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: appName, Namespace: "default",
				}, &app)).To(Succeed())
				g.Expect(app.Status.URL).To(Equal("https://custom.example.com"))
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			app := &loopzerov1.LoopZeroApplication{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: appName, Namespace: "default"}, app); err == nil {
				app.Finalizers = nil
				_ = k8sClient.Update(ctx, app)
				_ = k8sClient.Delete(ctx, app)
			}
			// Also clean up the ExposedRoute (which now has a finalizer too)
			route := &loopzerov1.ExposedRoute{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: appName + "-route", Namespace: "default"}, route); err == nil {
				route.Finalizers = nil
				_ = k8sClient.Update(ctx, route)
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
			Eventually(func(g Gomega) {
				var app loopzerov1.LoopZeroApplication
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: appName, Namespace: "default",
				}, &app)).To(Succeed())
				g.Expect(app.Status.ObservedGeneration).To(Equal(app.Generation))
			}, timeout, interval).Should(Succeed())
		})

		It("should add the app-cleanup finalizer on creation", func() {
			Eventually(func(g Gomega) {
				var app loopzerov1.LoopZeroApplication
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: appName, Namespace: "default",
				}, &app)).To(Succeed())
				g.Expect(app.Finalizers).To(ContainElement("loopzero.io/app-cleanup"))
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			app := &loopzerov1.LoopZeroApplication{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: appName, Namespace: "default"}, app); err == nil {
				app.Finalizers = nil
				_ = k8sClient.Update(ctx, app)
				_ = k8sClient.Delete(ctx, app)
			}
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
			app := &loopzerov1.LoopZeroApplication{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: appName, Namespace: "default"}, app); err == nil {
				app.Finalizers = nil
				_ = k8sClient.Update(ctx, app)
				_ = k8sClient.Delete(ctx, app)
			}
			// Also clean up the ExposedRoute (which now has a finalizer too)
			route := &loopzerov1.ExposedRoute{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: appName + "-route", Namespace: "default"}, route); err == nil {
				route.Finalizers = nil
				_ = k8sClient.Update(ctx, route)
				_ = k8sClient.Delete(ctx, route)
			}
		})
	})
})
