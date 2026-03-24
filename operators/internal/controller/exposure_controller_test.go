package controller_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/conditions"
)

func newExposedRouteCR(name, appRef, hostname, serviceName string, servicePort int32, tlsEnabled bool) *loopzerov1.ExposedRoute {
	return &loopzerov1.ExposedRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: loopzerov1.ExposedRouteSpec{
			ApplicationRef: appRef,
			Hostname:       hostname,
			ServiceRef: loopzerov1.ServiceRef{
				Name: serviceName,
				Port: servicePort,
			},
			TLS: loopzerov1.TLSConfig{
				Enabled: tlsEnabled,
			},
		},
	}
}

var _ = Describe("ExposureController", Ordered, func() {

	Context("finalizer management", func() {
		const (
			routeName   = "route-fin-test"
			appRef      = "app-fin-route"
			hostname    = "fin-test.dev.loopzero.example"
			serviceName = "fin-svc"
		)

		BeforeAll(func() {
			route := newExposedRouteCR(routeName, appRef, hostname, serviceName, 80, false)
			Expect(k8sClient.Create(ctx, route)).To(Succeed())
		})

		It("should add the route-cleanup finalizer on creation", func() {
			Eventually(func(g Gomega) {
				var route loopzerov1.ExposedRoute
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: routeName, Namespace: "default",
				}, &route)).To(Succeed())
				g.Expect(route.Finalizers).To(ContainElement("loopzero.io/route-cleanup"))
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			route := &loopzerov1.ExposedRoute{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: routeName, Namespace: "default"}, route); err == nil {
				route.Finalizers = nil
				_ = k8sClient.Update(ctx, route)
				_ = k8sClient.Delete(ctx, route)
			}
		})
	})

	Context("deletion via finalizer — removes finalizer immediately", func() {
		const (
			routeName   = "route-del-test"
			appRef      = "app-del-route"
			hostname    = "del-test.dev.loopzero.example"
			serviceName = "del-svc"
		)

		It("should remove finalizer immediately on delete (no external cleanup in v1)", func() {
			route := newExposedRouteCR(routeName, appRef, hostname, serviceName, 80, false)
			Expect(k8sClient.Create(ctx, route)).To(Succeed())

			// Wait for finalizer to be added
			Eventually(func(g Gomega) {
				var r loopzerov1.ExposedRoute
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: routeName, Namespace: "default",
				}, &r)).To(Succeed())
				g.Expect(r.Finalizers).To(ContainElement("loopzero.io/route-cleanup"))
			}, timeout, interval).Should(Succeed())

			// Delete the route
			var toDelete loopzerov1.ExposedRoute
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: routeName, Namespace: "default",
			}, &toDelete)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &toDelete)).To(Succeed())

			// Route should be fully deleted (finalizer removed immediately by controller)
			Eventually(func(g Gomega) {
				var deleted loopzerov1.ExposedRoute
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: routeName, Namespace: "default",
				}, &deleted)
				if err == nil {
					// Still exists — check finalizer is gone
					g.Expect(deleted.Finalizers).ToNot(ContainElement("loopzero.io/route-cleanup"))
				}
				// Either NotFound or finalizer removed is success
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("when an ExposedRoute has TLS enabled and cert secret exists", func() {
		const (
			routeName   = "route-cert-ready"
			appRef      = "app-cert-test"
			hostname    = "cert-test.dev.loopzero.example"
			serviceName = "cert-svc"
		)

		BeforeAll(func() {
			// Create the TLS secret that simulates cert-manager output.
			// naming.CertSecretName("cert-test.dev.loopzero.example") => "cert-test-dev-loopzero-dev-tls"
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cert-test-dev-loopzero-dev-tls",
					Namespace: "default",
				},
				Type: corev1.SecretTypeTLS,
				Data: map[string][]byte{
					"tls.crt": []byte("fake-cert"),
					"tls.key": []byte("fake-key"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			route := newExposedRouteCR(routeName, appRef, hostname, serviceName, 80, true)
			Expect(k8sClient.Create(ctx, route)).To(Succeed())
		})

		It("should set certificateReady to true", func() {
			Eventually(func(g Gomega) {
				var route loopzerov1.ExposedRoute
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: routeName, Namespace: "default",
				}, &route)).To(Succeed())
				g.Expect(route.Status.CertificateReady).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})

		It("should set CertificateIssued condition to True", func() {
			Eventually(func(g Gomega) {
				var route loopzerov1.ExposedRoute
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: routeName, Namespace: "default",
				}, &route)).To(Succeed())
				found := false
				for _, c := range route.Status.Conditions {
					if c.Type == conditions.TypeCertificateIssued {
						found = true
						g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
					}
				}
				g.Expect(found).To(BeTrue(), "CertificateIssued condition should exist")
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			route := &loopzerov1.ExposedRoute{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: routeName, Namespace: "default"}, route); err == nil {
				route.Finalizers = nil
				_ = k8sClient.Update(ctx, route)
				_ = k8sClient.Delete(ctx, route)
			}
			secret := &corev1.Secret{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cert-test-dev-loopzero-dev-tls", Namespace: "default"}, secret); err == nil {
				_ = k8sClient.Delete(ctx, secret)
			}
		})
	})

	Context("when TLS is enabled but cert secret does not exist", func() {
		const (
			routeName   = "route-no-cert"
			appRef      = "app-no-cert"
			hostname    = "no-cert.dev.loopzero.example"
			serviceName = "no-cert-svc"
		)

		BeforeAll(func() {
			route := newExposedRouteCR(routeName, appRef, hostname, serviceName, 80, true)
			Expect(k8sClient.Create(ctx, route)).To(Succeed())
		})

		It("should stay in Pending phase with certificateReady false", func() {
			// Wait for at least one reconcile to set the phase
			Eventually(func(g Gomega) {
				var route loopzerov1.ExposedRoute
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: routeName, Namespace: "default",
				}, &route)).To(Succeed())
				g.Expect(route.Status.Phase).To(Equal(loopzerov1.ExposedRoutePhasePending))
			}, timeout, interval).Should(Succeed())

			// Then verify it stays Pending
			Consistently(func(g Gomega) {
				var route loopzerov1.ExposedRoute
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: routeName, Namespace: "default",
				}, &route)).To(Succeed())
				g.Expect(route.Status.CertificateReady).To(BeFalse())
				g.Expect(route.Status.Phase).To(Equal(loopzerov1.ExposedRoutePhasePending))
			}, "3s", interval).Should(Succeed())
		})

		It("should set CertificateIssued condition to False", func() {
			Eventually(func(g Gomega) {
				var route loopzerov1.ExposedRoute
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: routeName, Namespace: "default",
				}, &route)).To(Succeed())
				found := false
				for _, c := range route.Status.Conditions {
					if c.Type == conditions.TypeCertificateIssued {
						found = true
						g.Expect(c.Status).To(Equal(metav1.ConditionFalse))
					}
				}
				g.Expect(found).To(BeTrue(), "CertificateIssued condition should exist")
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			route := &loopzerov1.ExposedRoute{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: routeName, Namespace: "default"}, route); err == nil {
				route.Finalizers = nil
				_ = k8sClient.Update(ctx, route)
				_ = k8sClient.Delete(ctx, route)
			}
		})
	})

	Context("when TLS is disabled", func() {
		const (
			routeName   = "route-no-tls"
			appRef      = "app-no-tls"
			hostname    = "no-tls.dev.loopzero.example"
			serviceName = "no-tls-svc"
		)

		BeforeAll(func() {
			route := newExposedRouteCR(routeName, appRef, hostname, serviceName, 80, false)
			Expect(k8sClient.Create(ctx, route)).To(Succeed())
		})

		It("should set certificateReady to true (TLS not required)", func() {
			Eventually(func(g Gomega) {
				var route loopzerov1.ExposedRoute
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: routeName, Namespace: "default",
				}, &route)).To(Succeed())
				g.Expect(route.Status.CertificateReady).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			route := &loopzerov1.ExposedRoute{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: routeName, Namespace: "default"}, route); err == nil {
				route.Finalizers = nil
				_ = k8sClient.Update(ctx, route)
				_ = k8sClient.Delete(ctx, route)
			}
		})
	})

	Context("probe URL override for testing", func() {
		var server *httptest.Server

		Context("when probe target is a healthy httptest server", func() {
			const (
				routeName   = "route-probe-healthy"
				appRef      = "app-probe-healthy"
				serviceName = "probe-healthy-svc"
			)

			BeforeAll(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
					fmt.Fprintln(w, "healthy")
				}))

				route := newExposedRouteCR(routeName, appRef, server.URL, serviceName, 80, false)
				// Use annotation to override probe URL for testing
				route.Annotations = map[string]string{
					"loopzero.io/probe-url-override": server.URL,
				}
				Expect(k8sClient.Create(ctx, route)).To(Succeed())
			})

			It("should reach Reachable phase", func() {
				Eventually(func(g Gomega) {
					var route loopzerov1.ExposedRoute
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name: routeName, Namespace: "default",
					}, &route)).To(Succeed())
					g.Expect(route.Status.Phase).To(Equal(loopzerov1.ExposedRoutePhaseReachable))
					g.Expect(route.Status.ConsecutiveFailures).To(Equal(int32(0)))
				}, timeout, interval).Should(Succeed())
			})

			It("should set RouteReachable condition to True", func() {
				Eventually(func(g Gomega) {
					var route loopzerov1.ExposedRoute
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name: routeName, Namespace: "default",
					}, &route)).To(Succeed())
					found := false
					for _, c := range route.Status.Conditions {
						if c.Type == conditions.TypeRouteReachable {
							found = true
							g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
						}
					}
					g.Expect(found).To(BeTrue(), "RouteReachable condition should exist")
				}, timeout, interval).Should(Succeed())
			})

			AfterAll(func() {
				server.Close()
				route := &loopzerov1.ExposedRoute{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: routeName, Namespace: "default"}, route); err == nil {
					route.Finalizers = nil
					_ = k8sClient.Update(ctx, route)
					_ = k8sClient.Delete(ctx, route)
				}
			})
		})

		Context("when probe target returns errors", func() {
			const (
				routeName   = "route-probe-unhealthy"
				appRef      = "app-probe-unhealthy"
				serviceName = "probe-unhealthy-svc"
			)

			BeforeAll(func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusServiceUnavailable)
				}))

				route := newExposedRouteCR(routeName, appRef, server.URL, serviceName, 80, false)
				route.Annotations = map[string]string{
					"loopzero.io/probe-url-override": server.URL,
				}
				Expect(k8sClient.Create(ctx, route)).To(Succeed())
			})

			It("should increment consecutiveFailures", func() {
				Eventually(func(g Gomega) {
					var route loopzerov1.ExposedRoute
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name: routeName, Namespace: "default",
					}, &route)).To(Succeed())
					g.Expect(route.Status.ConsecutiveFailures).To(BeNumerically(">=", int32(1)))
				}, timeout, interval).Should(Succeed())
			})

			It("should set lastProbeResult to 503", func() {
				Eventually(func(g Gomega) {
					var route loopzerov1.ExposedRoute
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name: routeName, Namespace: "default",
					}, &route)).To(Succeed())
					g.Expect(route.Status.LastProbeResult).To(Equal(int32(http.StatusServiceUnavailable)))
				}, timeout, interval).Should(Succeed())
			})

			AfterAll(func() {
				server.Close()
				route := &loopzerov1.ExposedRoute{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: routeName, Namespace: "default"}, route); err == nil {
					route.Finalizers = nil
					_ = k8sClient.Update(ctx, route)
					_ = k8sClient.Delete(ctx, route)
				}
			})
		})
	})
})
