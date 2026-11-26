// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package platform_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/conditions"
)

// buildKubeconfigBytes creates a serialized kubeconfig pointing at the given server
// with a fake token (for unreachable/unauthenticated cluster tests).
func buildKubeconfigBytes(server string) []byte {
	kubeconfig := clientcmdapi.NewConfig()
	kubeconfig.Clusters["test-cluster"] = &clientcmdapi.Cluster{
		Server:                server,
		InsecureSkipTLSVerify: true,
	}
	kubeconfig.AuthInfos["test-user"] = &clientcmdapi.AuthInfo{
		Token: "fake-token",
	}
	kubeconfig.Contexts["test-context"] = &clientcmdapi.Context{
		Cluster:  "test-cluster",
		AuthInfo: "test-user",
	}
	kubeconfig.CurrentContext = "test-context"

	data, err := clientcmd.Write(*kubeconfig)
	Expect(err).NotTo(HaveOccurred())
	return data
}

// buildEnvtestKubeconfigBytes creates a kubeconfig that authenticates against the envtest API server
// using the same TLS certificates from the envtest rest.Config. This produces a kubeconfig that
// actually works against the test control plane.
func buildEnvtestKubeconfigBytes() []byte {
	kubeconfig := clientcmdapi.NewConfig()
	kubeconfig.Clusters["envtest"] = &clientcmdapi.Cluster{
		Server:                   cfg.Host,
		CertificateAuthorityData: cfg.CAData,
	}
	kubeconfig.AuthInfos["envtest-user"] = &clientcmdapi.AuthInfo{
		ClientCertificateData: cfg.CertData,
		ClientKeyData:         cfg.KeyData,
	}
	kubeconfig.Contexts["envtest-context"] = &clientcmdapi.Context{
		Cluster:  "envtest",
		AuthInfo: "envtest-user",
	}
	kubeconfig.CurrentContext = "envtest-context"

	data, err := clientcmd.Write(*kubeconfig)
	Expect(err).NotTo(HaveOccurred())
	return data
}

func newClusterCredential(name, namespace, clusterName, apiServer, secretName, secretNamespace string) *loopruntimev1.ClusterCredential {
	return &loopruntimev1.ClusterCredential{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: loopruntimev1.ClusterCredentialSpec{
			ClusterName: clusterName,
			APIServer:   apiServer,
			SecretRef: loopruntimev1.SecretReference{
				Name:      secretName,
				Namespace: secretNamespace,
			},
			Scope: loopruntimev1.CredentialScope{
				ArgoCD:    true,
				Operators: false,
			},
		},
	}
}

var _ = Describe("ClusterCredentialController", Ordered, func() {

	const argoCDNamespace = "argocd"

	BeforeAll(func() {
		// Ensure the argocd namespace exists for ArgoCD secret creation tests.
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: argoCDNamespace,
			},
		}
		err := k8sClient.Create(ctx, ns)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}
	})

	Context("when the referenced Secret does not exist", func() {
		It("should set condition SecretNotFound=True and not panic", func() {
			cr := newClusterCredential(
				"cc-no-secret", "default",
				"workload-1", "https://unreachable.example.com:6443",
				"nonexistent-secret", "default",
			)
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			// The controller should set SecretNotFound=True and requeue after 30s.
			Eventually(func(g Gomega) {
				var fetched loopruntimev1.ClusterCredential
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "cc-no-secret", Namespace: "default",
				}, &fetched)).To(Succeed())

				g.Expect(fetched.Status.Connected).To(BeFalse())
				cond := findCondition(fetched.Status.Conditions, conditions.TypeSecretNotFound)
				g.Expect(cond).NotTo(BeNil(), "expected SecretNotFound condition to exist")
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))

				// The typed read above cannot tell an ABSENT `connected` from a
				// stored false — Go gives the zero value either way. The stored
				// document can, and the difference is what `kubectl get` prints.
				//
				// It matters because the controller writes by merge patch, which
				// sends the DIFF: `connected` is omitted whenever it did not
				// change, so on this path — secret missing, connected already
				// false — nothing carries the key. Without a schema default the
				// apiserver stores no key and the Connected column renders blank
				// for every credential that has not yet connected. Measured, and
				// it is why the field carries +kubebuilder:default=false.
				var raw unstructured.Unstructured
				raw.SetGroupVersionKind(loopruntimev1.GroupVersion.WithKind("ClusterCredential"))
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "cc-no-secret", Namespace: "default",
				}, &raw)).To(Succeed())
				_, found, err := unstructured.NestedBool(raw.Object, "status", "connected")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(found).To(BeTrue(),
					"status.connected is absent from the stored object — kubectl get renders the "+
						"Connected column blank. The schema default is what materialises it under a "+
						"merge patch that does not carry the field (B-085).")
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("when the referenced Secret exists with a valid kubeconfig pointing at an unreachable cluster", func() {
		It("should set status.connected=false and condition Connected=False", func() {
			// Create the secret with kubeconfig pointing to an unreachable host.
			secretName := "cc-unreachable-secret"
			kubeconfigData := buildKubeconfigBytes("https://192.0.2.1:6443") // TEST-NET, unreachable

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: "default",
				},
				Data: map[string][]byte{
					"kubeconfig": kubeconfigData,
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			cr := newClusterCredential(
				"cc-unreachable", "default",
				"workload-unreachable", "https://192.0.2.1:6443",
				secretName, "default",
			)
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			Eventually(func(g Gomega) {
				var fetched loopruntimev1.ClusterCredential
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "cc-unreachable", Namespace: "default",
				}, &fetched)).To(Succeed())

				g.Expect(fetched.Status.Connected).To(BeFalse())
				cond := findCondition(fetched.Status.Conditions, conditions.TypeConnected)
				g.Expect(cond).NotTo(BeNil(), "expected Connected condition to exist")
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("when the referenced Secret exists with a valid kubeconfig pointing at the envtest API server (reachable)", func() {
		It("should set status.connected=true, condition Connected=True, and create ArgoCD cluster secret", func() {
			secretName := "cc-reachable-secret"

			// Use the envtest API server credentials so the connectivity check succeeds.
			envtestURL := cfg.Host
			kubeconfigData := buildEnvtestKubeconfigBytes()

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: "default",
				},
				Data: map[string][]byte{
					"kubeconfig": kubeconfigData,
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			cr := newClusterCredential(
				"cc-reachable", "default",
				"workload-reachable", envtestURL,
				secretName, "default",
			)
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			// status.connected should become true.
			// Use 30s timeout because during the full suite the controller worker
			// may be busy with other CRs (unreachable clusters take 5s each).
			Eventually(func(g Gomega) {
				var fetched loopruntimev1.ClusterCredential
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "cc-reachable", Namespace: "default",
				}, &fetched)).To(Succeed())

				g.Expect(fetched.Status.Connected).To(BeTrue())
				g.Expect(fetched.Status.LastCheckedAt).NotTo(BeNil())

				cond := findCondition(fetched.Status.Conditions, conditions.TypeConnected)
				g.Expect(cond).NotTo(BeNil(), "expected Connected condition to exist")
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			}, 30*time.Second, interval).Should(Succeed())

			// ArgoCD cluster secret should exist in argocd namespace
			Eventually(func(g Gomega) {
				var argoSecret corev1.Secret
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("cluster-%s", "workload-reachable"),
					Namespace: argoCDNamespace,
				}, &argoSecret)).To(Succeed())

				g.Expect(argoSecret.Labels).To(HaveKeyWithValue("argocd.argoproj.io/secret-type", "cluster"))
				// K8s stores StringData as Data (base64 encoded), so check Data keys.
				g.Expect(argoSecret.Data).To(HaveKey("name"))
				g.Expect(argoSecret.Data).To(HaveKey("server"))
				g.Expect(argoSecret.Data).To(HaveKey("config"))

				// CHG-002b regression: caData must be base64-encoded PEM,
				// not raw PEM string. ArgoCD's `tlsClientConfig.caData`
				// expects base64 bytes; raw PEM ("-----BEGIN ...") starts
				// with `-` which is NOT valid base64 → ArgoCD fails with
				// `illegal base64 data at input byte 0` and emits
				// `InvalidSpecError: unable to find destination server`
				// for every Application managed by this credential.
				configRaw := argoSecret.Data["config"]
				var argoCfg struct {
					TLSClientConfig struct {
						CAData string `json:"caData"`
					} `json:"tlsClientConfig"`
				}
				g.Expect(json.Unmarshal(configRaw, &argoCfg)).To(Succeed())
				if argoCfg.TLSClientConfig.CAData != "" {
					g.Expect(argoCfg.TLSClientConfig.CAData).NotTo(HavePrefix("-----BEGIN"),
						"caData must be base64-encoded PEM, not raw PEM")
					decoded, err := base64.StdEncoding.DecodeString(argoCfg.TLSClientConfig.CAData)
					g.Expect(err).NotTo(HaveOccurred(),
						"caData must be valid base64")
					g.Expect(string(decoded)).To(HavePrefix("-----BEGIN"),
						"decoded caData must be a PEM block")
				}
			}, 30*time.Second, interval).Should(Succeed())
		})
	})

	Context("when the referenced Secret exists but kubeconfig data is invalid", func() {
		It("should set Connected=False with a meaningful error reason", func() {
			secretName := "cc-bad-kubeconfig-secret"

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: "default",
				},
				Data: map[string][]byte{
					"kubeconfig": []byte("not-a-valid-kubeconfig"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			cr := newClusterCredential(
				"cc-bad-kubeconfig", "default",
				"workload-bad", "https://example.com:6443",
				secretName, "default",
			)
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			Eventually(func(g Gomega) {
				var fetched loopruntimev1.ClusterCredential
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "cc-bad-kubeconfig", Namespace: "default",
				}, &fetched)).To(Succeed())

				g.Expect(fetched.Status.Connected).To(BeFalse())
				cond := findCondition(fetched.Status.Conditions, conditions.TypeConnected)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("when the Secret is created after the ClusterCredential", func() {
		It("should eventually resolve SecretNotFound and check connectivity", func() {
			cr := newClusterCredential(
				"cc-late-secret", "default",
				"workload-late", "https://192.0.2.1:6443",
				"late-secret", "default",
			)
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			// First, SecretNotFound should be True.
			Eventually(func(g Gomega) {
				var fetched loopruntimev1.ClusterCredential
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "cc-late-secret", Namespace: "default",
				}, &fetched)).To(Succeed())

				cond := findCondition(fetched.Status.Conditions, conditions.TypeSecretNotFound)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			}, timeout, interval).Should(Succeed())

			// Now create the secret (pointing to unreachable host).
			kubeconfigData := buildKubeconfigBytes("https://192.0.2.1:6443")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "late-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"kubeconfig": kubeconfigData,
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			// After requeue (~30s), the controller should pick up the secret.
			// Since we are in envtest, we can wait a bit longer.
			// The secret points to unreachable host so Connected=False.
			Eventually(func(g Gomega) {
				var fetched loopruntimev1.ClusterCredential
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "cc-late-secret", Namespace: "default",
				}, &fetched)).To(Succeed())

				cond := findCondition(fetched.Status.Conditions, conditions.TypeConnected)
				g.Expect(cond).NotTo(BeNil(), "expected Connected condition after secret was created")
				// SecretNotFound should now be False
				snfCond := findCondition(fetched.Status.Conditions, conditions.TypeSecretNotFound)
				g.Expect(snfCond).NotTo(BeNil())
				g.Expect(snfCond.Status).To(Equal(metav1.ConditionFalse))
			}, 45*time.Second, interval).Should(Succeed())
		})
	})

	Context("when the ClusterCredential is deleted", func() {
		It("should clean up the ArgoCD cluster secret", func() {
			secretName := "cc-delete-secret"
			envtestURL := cfg.Host
			kubeconfigData := buildEnvtestKubeconfigBytes()

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: "default",
				},
				Data: map[string][]byte{
					"kubeconfig": kubeconfigData,
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			cr := newClusterCredential(
				"cc-delete-test", "default",
				"workload-delete", envtestURL,
				secretName, "default",
			)
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			// Wait for ArgoCD secret to be created (longer timeout for full suite runs).
			argoSecretName := fmt.Sprintf("cluster-%s", "workload-delete")
			Eventually(func(g Gomega) {
				var argoSecret corev1.Secret
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      argoSecretName,
					Namespace: argoCDNamespace,
				}, &argoSecret)).To(Succeed())
			}, 30*time.Second, interval).Should(Succeed())

			// Delete the ClusterCredential
			var toDelete loopruntimev1.ClusterCredential
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "cc-delete-test", Namespace: "default",
			}, &toDelete)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &toDelete)).To(Succeed())

			// ArgoCD secret should be cleaned up
			Eventually(func(g Gomega) {
				var argoSecret corev1.Secret
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      argoSecretName,
					Namespace: argoCDNamespace,
				}, &argoSecret)
				g.Expect(errors.IsNotFound(err)).To(BeTrue(), "expected ArgoCD secret to be deleted")
			}, timeout, interval).Should(Succeed())

			// The CR itself should be gone
			Eventually(func(g Gomega) {
				var gone loopruntimev1.ClusterCredential
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: "cc-delete-test", Namespace: "default",
				}, &gone)
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("periodic recheck", func() {
		It("should update lastCheckedAt on subsequent reconciliations", func() {
			secretName := "cc-recheck-secret"
			envtestURL := cfg.Host
			kubeconfigData := buildEnvtestKubeconfigBytes()

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: "default",
				},
				Data: map[string][]byte{
					"kubeconfig": kubeconfigData,
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			cr := newClusterCredential(
				"cc-recheck", "default",
				"workload-recheck", envtestURL,
				secretName, "default",
			)
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			// Wait for first check. Use a longer timeout because during the full suite
			// the controller worker may be busy reconciling other ClusterCredential CRs.
			var firstCheckTime *metav1.Time
			Eventually(func(g Gomega) {
				var fetched loopruntimev1.ClusterCredential
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "cc-recheck", Namespace: "default",
				}, &fetched)).To(Succeed())
				g.Expect(fetched.Status.LastCheckedAt).NotTo(BeNil())
				firstCheckTime = fetched.Status.LastCheckedAt
			}, 30*time.Second, interval).Should(Succeed())

			Expect(firstCheckTime).NotTo(BeNil())
		})
	})

	Context("TLS insecure mode (issue #67)", func() {
		// Regression for github.com/getloopscale/loop-runtime#67: when a CR declares
		// spec.tls.insecure=true (the supported way to bypass TLS verification
		// for development clusters with self-signed certs), the rendered
		// ArgoCD cluster Secret MUST have config.tlsClientConfig.insecure=true.
		//
		// Pre-fix root cause: bootstrap scripts (bootstrap-cell-cc-common.sh,
		// clustercredential-create.sh) created CRs WITHOUT tls.insecure, so
		// CR.Spec.TLS.Insecure defaulted to false → reconciler wrote Secret
		// with insecure=false + stale caData → ArgoCD x509 errors after k3s
		// rotated its server CA. Reactive workaround (heal H19 patching the
		// Secret) was overwritten by the next reconcile of this same loop.
		//
		// Fix: declarative — bash scripts set spec.tls.insecure=true for
		// k3d-local CRs; this test proves the reconciler honors the flag
		// and idempotency preserves it across reconciles.
		It("propagates spec.tls.insecure=true to Secret config AND preserves across reconciles", func() {
			secretName := "cc-tls-insecure-secret"
			envtestURL := cfg.Host
			kubeconfigData := buildEnvtestKubeconfigBytes()

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: "default",
				},
				Data: map[string][]byte{
					"kubeconfig": kubeconfigData,
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			cr := newClusterCredential(
				"cc-tls-insecure", "default",
				"workload-tls-insecure", envtestURL,
				secretName, "default",
			)
			cr.Spec.TLS.Insecure = true
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			argoSecretName := fmt.Sprintf("cluster-%s", "workload-tls-insecure")

			// Wait for first reconciliation to write Secret with insecure=true.
			Eventually(func(g Gomega) {
				var argoSecret corev1.Secret
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      argoSecretName,
					Namespace: argoCDNamespace,
				}, &argoSecret)).To(Succeed())

				configRaw := argoSecret.Data["config"]
				var argoCfg struct {
					TLSClientConfig struct {
						Insecure bool   `json:"insecure"`
						CAData   string `json:"caData,omitempty"`
					} `json:"tlsClientConfig"`
				}
				g.Expect(json.Unmarshal(configRaw, &argoCfg)).To(Succeed())
				g.Expect(argoCfg.TLSClientConfig.Insecure).To(BeTrue(),
					"spec.tls.insecure=true must propagate to Secret config.tlsClientConfig.insecure")
			}, 30*time.Second, interval).Should(Succeed())

			// Critical regression assertion: Consistently, across the
			// reconciler's natural re-runs (Secret watcher + connectivity
			// requeue), the insecure flag stays true. This is the test
			// that would have caught the original bug — when a heal patch
			// flipped insecure to true externally, the next reconcile
			// rewrote the Secret from CR.Spec (which had insecure=false
			// because scripts didn't set it). With the fix, CR.Spec is
			// the source-of-truth AND it carries insecure=true.
			Consistently(func(g Gomega) {
				var argoSecret corev1.Secret
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      argoSecretName,
					Namespace: argoCDNamespace,
				}, &argoSecret)).To(Succeed())

				configRaw := argoSecret.Data["config"]
				var argoCfg struct {
					TLSClientConfig struct {
						Insecure bool `json:"insecure"`
					} `json:"tlsClientConfig"`
				}
				g.Expect(json.Unmarshal(configRaw, &argoCfg)).To(Succeed())
				g.Expect(argoCfg.TLSClientConfig.Insecure).To(BeTrue(),
					"insecure flag must persist across reconciles — operator MUST NOT regress to false")
			}, 5*time.Second, interval).Should(Succeed())
		})
	})

	Context("idempotency", func() {
		It("should not change state when reconciled multiple times with same input", func() {
			secretName := "cc-idempotent-secret"
			envtestURL := cfg.Host
			kubeconfigData := buildEnvtestKubeconfigBytes()

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: "default",
				},
				Data: map[string][]byte{
					"kubeconfig": kubeconfigData,
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			cr := newClusterCredential(
				"cc-idempotent", "default",
				"workload-idempotent", envtestURL,
				secretName, "default",
			)
			Expect(k8sClient.Create(ctx, cr)).To(Succeed())

			// Wait for first reconciliation. Use a longer timeout because during the full suite
			// the controller worker may be busy reconciling other ClusterCredential CRs
			// (including ones with unreachable clusters that take 5s per connectivity check).
			Eventually(func(g Gomega) {
				var fetched loopruntimev1.ClusterCredential
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "cc-idempotent", Namespace: "default",
				}, &fetched)).To(Succeed())
				g.Expect(fetched.Status.Connected).To(BeTrue())
			}, 30*time.Second, interval).Should(Succeed())

			// Verify ArgoCD secret exists
			argoSecretName := fmt.Sprintf("cluster-%s", "workload-idempotent")
			var argoSecret corev1.Secret
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      argoSecretName,
				Namespace: argoCDNamespace,
			}, &argoSecret)).To(Succeed())

			// The ArgoCD secret should still exist and not be duplicated after multiple reconciles.
			Consistently(func(g Gomega) {
				var fetched loopruntimev1.ClusterCredential
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "cc-idempotent", Namespace: "default",
				}, &fetched)).To(Succeed())
				g.Expect(fetched.Status.Connected).To(BeTrue())

				var secret corev1.Secret
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      argoSecretName,
					Namespace: argoCDNamespace,
				}, &secret)).To(Succeed())
			}, 3*time.Second, interval).Should(Succeed())
		})
	})
})
