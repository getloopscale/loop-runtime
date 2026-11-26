// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package platform_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/conditions"
)

// newPlatformBootstrap creates a PlatformBootstrap CR for testing.
// giteaURL, argoCDURL, and apiURL point to httptest servers simulating real services.
func newPlatformBootstrap(name, namespace, giteaURL, argoCDURL, apiURL string) *loopruntimev1.PlatformBootstrap {
	return &loopruntimev1.PlatformBootstrap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: loopruntimev1.PlatformBootstrapSpec{
			Gitea: loopruntimev1.GiteaBootstrapSpec{
				AdminSecretRef: loopruntimev1.SecretKeyRef{
					Name: "gitea-admin",
				},
				Organizations: []string{"loop-runtime"},
				TokenName:     "platform-bootstrap-token",
				InternalURL:   giteaURL,
			},
			ArgoCD: loopruntimev1.ArgoCDBootstrapSpec{
				Namespace:       "argocd",
				AdminSecretName: "argocd-initial-admin-secret",
				InternalURL:     argoCDURL,
			},
			API: loopruntimev1.APIBootstrapSpec{
				BootstrapEndpoint: apiURL + "/internal/bootstrap",
				HealthEndpoint:    apiURL + "/api/v1/health",
			},
			MaxRetries: 3,
		},
	}
}

var _ = Describe("PlatformBootstrapController", Ordered, func() {

	Context("when a new PlatformBootstrap CR is created with all services healthy", func() {
		var (
			giteaServer       *httptest.Server
			argoServer        *httptest.Server
			apiServer         *httptest.Server
			pbName            string
			pbNamespace       string
			giteaTokenCreated atomic.Bool
		)

		BeforeAll(func() {
			pbName = "bootstrap-happy"
			pbNamespace = "default"

			// Gitea mock: version, token creation, org creation
			giteaServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/api/v1/version" && r.Method == http.MethodGet:
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(w, `{"version":"1.21.0"}`)

				case r.URL.Path == "/api/v1/user" && r.Method == http.MethodGet:
					// Token validation: if we have a token, return user
					authHeader := r.Header.Get("Authorization")
					if authHeader != "" {
						w.Header().Set("Content-Type", "application/json")
						_, _ = fmt.Fprint(w, `{"id":1,"login":"gitea_admin"}`)
						return
					}
					http.Error(w, "unauthorized", http.StatusUnauthorized)

				case r.URL.Path == "/api/v1/users/gitea_admin/tokens" && r.Method == http.MethodGet:
					// List tokens - return empty initially
					auth := r.Header.Get("Authorization")
					if auth == "" {
						http.Error(w, "unauthorized", http.StatusUnauthorized)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					if giteaTokenCreated.Load() {
						_, _ = fmt.Fprint(w, `[{"id":1,"name":"platform-bootstrap-token"}]`)
					} else {
						_, _ = fmt.Fprint(w, `[]`)
					}

				case r.URL.Path == "/api/v1/users/gitea_admin/tokens" && r.Method == http.MethodPost:
					// Create token
					giteaTokenCreated.Store(true)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					_, _ = fmt.Fprint(w, `{"id":1,"name":"platform-bootstrap-token","sha1":"test-token-value-12345"}`)

				case r.URL.Path == "/api/v1/orgs/loop-runtime" && r.Method == http.MethodGet:
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(w, `{"id":1,"name":"loop-runtime"}`)

				case r.URL.Path == "/api/v1/orgs" && r.Method == http.MethodPost:
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					_, _ = fmt.Fprint(w, `{"id":1,"name":"loop-runtime"}`)

				default:
					http.Error(w, fmt.Sprintf("unexpected gitea request: %s %s", r.Method, r.URL.Path), http.StatusNotFound)
				}
			}))

			// ArgoCD mock: session creation
			argoServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/api/v1/session" && r.Method == http.MethodPost:
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(w, `{"token":"argocd-jwt-token-12345"}`)

				case r.URL.Path == "/api/version" && r.Method == http.MethodGet:
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(w, `{"Version":"v2.10.0"}`)

				case r.URL.Path == "/api/v1/account/loop-runtime-api/token" && r.Method == http.MethodPost:
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(w, `{"token":"argocd-api-key-token-67890"}`)

				default:
					http.Error(w, fmt.Sprintf("unexpected argocd request: %s %s", r.Method, r.URL.Path), http.StatusNotFound)
				}
			}))

			// API mock: health + bootstrap
			apiServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/api/v1/health" && r.Method == http.MethodGet:
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(w, `{"status":"ok"}`)

				case r.URL.Path == "/internal/bootstrap" && r.Method == http.MethodPost:
					w.WriteHeader(http.StatusOK)
					_, _ = fmt.Fprint(w, `{"seeded":true}`)

				default:
					http.Error(w, fmt.Sprintf("unexpected api request: %s %s", r.Method, r.URL.Path), http.StatusNotFound)
				}
			}))

			// Create prerequisite secrets
			giteaAdminSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gitea-admin",
					Namespace: pbNamespace,
				},
				StringData: map[string]string{
					"username": "gitea_admin",
					"password": "admin-password",
				},
			}
			Expect(k8sClient.Create(ctx, giteaAdminSecret)).To(Succeed())

			// Create argocd namespace and admin secret
			argoNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "argocd"},
			}
			Expect(k8sClient.Create(ctx, argoNS)).To(Or(Succeed(), MatchError(ContainSubstring("already exists"))))

			argoAdminSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-initial-admin-secret",
					Namespace: "argocd",
				},
				StringData: map[string]string{
					"password": "argocd-admin-password",
				},
			}
			Expect(k8sClient.Create(ctx, argoAdminSecret)).To(Succeed())

			// Create platform-tokens secret namespace-local for token storage
			platformTokensSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "loop-runtime-platform-tokens",
					Namespace: pbNamespace,
				},
				StringData: map[string]string{},
			}
			Expect(k8sClient.Create(ctx, platformTokensSecret)).To(Succeed())

			// Create the PlatformBootstrap CR
			pb := newPlatformBootstrap(pbName, pbNamespace, giteaServer.URL, argoServer.URL, apiServer.URL)
			Expect(k8sClient.Create(ctx, pb)).To(Succeed())
		})

		AfterAll(func() {
			giteaServer.Close()
			argoServer.Close()
			apiServer.Close()
		})

		It("should transition through all phases to BootstrapComplete", func() {
			Eventually(func(g Gomega) {
				var pb loopruntimev1.PlatformBootstrap
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      pbName,
					Namespace: pbNamespace,
				}, &pb)).To(Succeed())
				g.Expect(pb.Status.Phase).To(Equal(loopruntimev1.PlatformBootstrapPhaseComplete))
			}, timeout, interval).Should(Succeed())
		})

		It("should set GiteaReady condition to True", func() {
			var pb loopruntimev1.PlatformBootstrap
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      pbName,
				Namespace: pbNamespace,
			}, &pb)).To(Succeed())
			Expect(conditions.IsConditionTrue(pb.Status.Conditions, conditions.TypeGiteaReady)).To(BeTrue())
		})

		It("should set ArgoCDReady condition to True", func() {
			var pb loopruntimev1.PlatformBootstrap
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      pbName,
				Namespace: pbNamespace,
			}, &pb)).To(Succeed())
			Expect(conditions.IsConditionTrue(pb.Status.Conditions, conditions.TypeArgoCDReady)).To(BeTrue())
		})

		It("should set DatabaseSeeded condition to True", func() {
			var pb loopruntimev1.PlatformBootstrap
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      pbName,
				Namespace: pbNamespace,
			}, &pb)).To(Succeed())
			Expect(conditions.IsConditionTrue(pb.Status.Conditions, conditions.TypeDatabaseSeeded)).To(BeTrue())
		})

		It("should store tokens in the platform-tokens Secret", func() {
			var secret corev1.Secret
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "loop-runtime-platform-tokens",
				Namespace: pbNamespace,
			}, &secret)).To(Succeed())
			Expect(secret.Data).To(HaveKey("GITEA_TOKEN"))
			Expect(secret.Data).To(HaveKey("ARGOCD_TOKEN"))
		})
	})

	Context("when Gitea is unreachable", func() {
		var (
			pbName      string
			pbNamespace string
		)

		BeforeAll(func() {
			pbName = "bootstrap-gitea-fail"
			pbNamespace = "default"

			// Create admin secret
			giteaAdminSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gitea-admin-fail",
					Namespace: pbNamespace,
				},
				StringData: map[string]string{
					"username": "gitea_admin",
					"password": "admin-password",
				},
			}
			Expect(k8sClient.Create(ctx, giteaAdminSecret)).To(Succeed())

			// Platform tokens secret for this test
			platformTokensSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "loop-runtime-platform-tokens-fail",
					Namespace: pbNamespace,
				},
				StringData: map[string]string{},
			}
			Expect(k8sClient.Create(ctx, platformTokensSecret)).To(Succeed())

			// Use an unreachable URL
			pb := newPlatformBootstrap(pbName, pbNamespace, "http://127.0.0.1:1", "http://127.0.0.1:2", "http://127.0.0.1:3")
			pb.Spec.Gitea.AdminSecretRef.Name = "gitea-admin-fail"
			pb.Spec.MaxRetries = 2
			Expect(k8sClient.Create(ctx, pb)).To(Succeed())
		})

		It("should stay in ConfiguringGitea phase and increment retryCount", func() {
			Eventually(func(g Gomega) {
				var pb loopruntimev1.PlatformBootstrap
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      pbName,
					Namespace: pbNamespace,
				}, &pb)).To(Succeed())
				g.Expect(pb.Status.Phase).To(SatisfyAny(
					Equal(loopruntimev1.PlatformBootstrapPhaseConfiguringGitea),
					Equal(loopruntimev1.PlatformBootstrapPhaseFailed),
				))
				g.Expect(pb.Status.RetryCount).To(BeNumerically(">", 0))
			}, timeout, interval).Should(Succeed())
		})

		It("should transition to Failed after max retries", func() {
			Eventually(func(g Gomega) {
				var pb loopruntimev1.PlatformBootstrap
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      pbName,
					Namespace: pbNamespace,
				}, &pb)).To(Succeed())
				g.Expect(pb.Status.Phase).To(Equal(loopruntimev1.PlatformBootstrapPhaseFailed))
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("when phase is BootstrapComplete (terminal)", func() {
		var (
			pbName      string
			pbNamespace string
			apiCalls    atomic.Int32
		)

		BeforeAll(func() {
			pbName = "bootstrap-terminal"
			pbNamespace = "default"

			// API server that counts calls
			apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				apiCalls.Add(1)
				switch r.URL.Path {
				case "/api/v1/health":
					_, _ = fmt.Fprint(w, `{"status":"ok"}`)
				case "/internal/bootstrap":
					_, _ = fmt.Fprint(w, `{"seeded":true}`)
				}
			}))
			giteaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/api/v1/version":
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(w, `{"version":"1.21.0"}`)
				case r.URL.Path == "/api/v1/user":
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(w, `{"id":1,"login":"gitea_admin"}`)
				case r.URL.Path == "/api/v1/users/gitea_admin/tokens" && r.Method == http.MethodGet:
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(w, `[{"id":1,"name":"platform-bootstrap-token"}]`)
				case r.URL.Path == "/api/v1/orgs/loop-runtime":
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(w, `{"id":1,"name":"loop-runtime"}`)
				default:
					w.WriteHeader(http.StatusOK)
				}
			}))
			argoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/session":
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(w, `{"token":"argocd-jwt-token"}`)
				case "/api/version":
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(w, `{"Version":"v2.10.0"}`)
				default:
					w.WriteHeader(http.StatusOK)
				}
			}))

			// Prereqs
			giteaAdminSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gitea-admin-terminal",
					Namespace: pbNamespace,
				},
				StringData: map[string]string{
					"username": "gitea_admin",
					"password": "admin-password",
				},
			}
			Expect(k8sClient.Create(ctx, giteaAdminSecret)).To(Succeed())

			platformTokensSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "loop-runtime-platform-tokens-terminal",
					Namespace: pbNamespace,
				},
				StringData: map[string]string{
					"GITEA_TOKEN":  "existing-token",
					"ARGOCD_TOKEN": "existing-argocd-token",
				},
			}
			Expect(k8sClient.Create(ctx, platformTokensSecret)).To(Succeed())

			pb := newPlatformBootstrap(pbName, pbNamespace, giteaServer.URL, argoServer.URL, apiServer.URL)
			pb.Spec.Gitea.AdminSecretRef.Name = "gitea-admin-terminal"
			Expect(k8sClient.Create(ctx, pb)).To(Succeed())

			// Wait for it to reach BootstrapComplete
			Eventually(func(g Gomega) {
				var pb loopruntimev1.PlatformBootstrap
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      pbName,
					Namespace: pbNamespace,
				}, &pb)).To(Succeed())
				g.Expect(pb.Status.Phase).To(Equal(loopruntimev1.PlatformBootstrapPhaseComplete))
			}, timeout, interval).Should(Succeed())

			// Record API call count at this point
			apiCalls.Store(0)

			DeferCleanup(func() {
				apiServer.Close()
				giteaServer.Close()
				argoServer.Close()
			})
		})

		It("should not re-bootstrap (no further API calls)", func() {
			// Wait a bit and check no more API calls were made
			Consistently(func() int32 {
				return apiCalls.Load()
			}, "2s", interval).Should(BeNumerically("<=", 0))
		})
	})

	Context("idempotency: reconciling same state produces same result", func() {
		It("should handle re-reconciliation of a completed bootstrap without changes", func() {
			// First verify the happy path CR exists from earlier ordered test.
			// If running standalone, this test is a no-op — the Ordered container
			// ensures the happy-path test ran first within this suite.
			var pb loopruntimev1.PlatformBootstrap
			key := types.NamespacedName{Name: "bootstrap-happy", Namespace: "default"}
			if err := k8sClient.Get(ctx, key, &pb); err != nil {
				// CR not found — this is expected when running this Context in isolation.
				// The idempotency assertion is meaningless without the happy path state.
				return
			}

			// Phase should still be BootstrapComplete after multiple reconcile cycles
			Consistently(func(g Gomega) {
				var current loopruntimev1.PlatformBootstrap
				g.Expect(k8sClient.Get(ctx, key, &current)).To(Succeed())
				g.Expect(current.Status.Phase).To(Equal(loopruntimev1.PlatformBootstrapPhaseComplete))
			}, "2s", interval).Should(Succeed())
		})
	})
})
