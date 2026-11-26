// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package app_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	loopzerov1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/conditions"
	"github.com/getloopscale/loop-runtime/operators/operatortest"
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
			hostname    = "fin-test.dev.getloopscale.com"
			serviceName = "fin-svc"
		)

		BeforeAll(func() {
			route := newExposedRouteCR(routeName, appRef, hostname, serviceName, 80, false)
			Expect(k8sClient.Create(ctx, route)).To(Succeed())
		})

		It("should add the route-cleanup finalizer on creation", func() {
			withRoute(routeName, func(g Gomega, route *loopzerov1.ExposedRoute) {
				g.Expect(route.Finalizers).To(ContainElement("loopzero.io/route-cleanup"))
			})
		})

		AfterAll(func() {
			deleteRouteWithFinalizer(routeName)
		})
	})

	Context("deletion via finalizer — removes finalizer immediately", func() {
		const (
			routeName   = "route-del-test"
			appRef      = "app-del-route"
			hostname    = "del-test.dev.getloopscale.com"
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
			hostname    = "cert-test.dev.getloopscale.com"
			serviceName = "cert-svc"
		)

		BeforeAll(func() {
			// #277: the Secret lives in the workload's cell, in the tenant namespace, under
			// the name the renderer creates — tls-{project}-{app} — NOT one derived from
			// the hostname. The chain is ExposedRoute -> LoopZeroApplication -> Tenant ->
			// cellID -> namespace.
			tenant := &loopzerov1.Tenant{
				ObjectMeta: metav1.ObjectMeta{Name: "tn-cert", Namespace: "default"},
				Spec: loopzerov1.TenantSpec{
					CellID:      "cell-0",
					DisplayName: "cert test tenant",
					Plan:        loopzerov1.PlanStarter,
					GitOps:      loopzerov1.GitOpsSpec{RepoName: "loopzero-tn-cert", Branch: "main"},
					Limits:      loopzerov1.ResourceLimits{MaxBuildConcurrency: 1},
					Environments: []loopzerov1.TenantEnvironmentEntry{
						{Name: "production", Cluster: "workload", Project: "certproj"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			loopzeroApp := &loopzerov1.LoopZeroApplication{
				ObjectMeta: metav1.ObjectMeta{Name: appRef, Namespace: "default"},
				Spec: loopzerov1.LoopZeroApplicationSpec{
					TenantRef:   "tn-cert",
					Project:     "certproj",
					Environment: "production",
					ServiceName: serviceName,
					AppName:     serviceName,
				},
			}
			Expect(k8sClient.Create(ctx, loopzeroApp)).To(Succeed())

			certNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "loopzero-tn-cert-certproj-production"},
			}
			_ = k8sClient.Create(ctx, certNS) // idempotent across suite runs

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-certproj-" + serviceName,
					Namespace: "loopzero-tn-cert-certproj-production",
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
			expectRouteCondition(routeName, conditions.TypeCertificateIssued, metav1.ConditionTrue)
		})

		AfterAll(func() {
			deleteRouteWithFinalizer(routeName)
			secret := &corev1.Secret{}
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name: "tls-certproj-" + serviceName, Namespace: "loopzero-tn-cert-certproj-production",
			}, secret); err == nil {
				_ = k8sClient.Delete(ctx, secret)
			}
			deleteAppWithFinalizer(appRef)
		})
	})

	Context("when TLS is enabled but cert secret does not exist", func() {
		const (
			routeName   = "route-no-cert"
			appRef      = "app-no-cert"
			hostname    = "no-cert.dev.getloopscale.com"
			serviceName = "no-cert-svc"
		)

		BeforeAll(func() {
			route := newExposedRouteCR(routeName, appRef, hostname, serviceName, 80, true)
			Expect(k8sClient.Create(ctx, route)).To(Succeed())
		})

		It("should stay in Pending phase with certificateReady false", func() {
			// Wait for at least one reconcile to set the phase
			withRoute(routeName, func(g Gomega, route *loopzerov1.ExposedRoute) {
				g.Expect(route.Status.Phase).To(Equal(loopzerov1.ExposedRoutePhasePending))
			})

			// Then verify it stays Pending (use Consistently to assert non-change over 3s)
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
			expectRouteCondition(routeName, conditions.TypeCertificateIssued, metav1.ConditionFalse)
		})

		AfterAll(func() {
			deleteRouteWithFinalizer(routeName)
		})
	})

	Context("when TLS is disabled", func() {
		const (
			routeName   = "route-no-tls"
			appRef      = "app-no-tls"
			hostname    = "no-tls.dev.getloopscale.com"
			serviceName = "no-tls-svc"
		)

		BeforeAll(func() {
			route := newExposedRouteCR(routeName, appRef, hostname, serviceName, 80, false)
			Expect(k8sClient.Create(ctx, route)).To(Succeed())
		})

		It("should set certificateReady to true (TLS not required)", func() {
			withRoute(routeName, func(g Gomega, route *loopzerov1.ExposedRoute) {
				g.Expect(route.Status.CertificateReady).To(BeTrue())
			})
		})

		AfterAll(func() {
			deleteRouteWithFinalizer(routeName)
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
					_, _ = fmt.Fprintln(w, "healthy")
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
				expectRouteCondition(routeName, conditions.TypeRouteReachable, metav1.ConditionTrue)
			})

			AfterAll(func() {
				server.Close()
				deleteRouteWithFinalizer(routeName)
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
					_ = operatortest.UpdateWithRetry(ctx, k8sClient, route, func() {
						route.Finalizers = nil
					})
					_ = k8sClient.Delete(ctx, route)
				}
			})
		})
	})
})
