// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package platform_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	loopzerov1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/operatortest"
)

func newGitRepositoryBinding(name, namespace, repoURL, secretName, secretKey, secretNamespace, consumerType, consumerNamespace string) *loopzerov1.GitRepositoryBinding {
	return &loopzerov1.GitRepositoryBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: loopzerov1.GitRepositoryBindingSpec{
			RepoURL: repoURL,
			CredentialSecretRef: loopzerov1.CredentialSecretRef{
				Name:      secretName,
				Key:       secretKey,
				Namespace: secretNamespace,
			},
			Consumer: loopzerov1.ConsumerSpec{
				Type:      consumerType,
				Namespace: consumerNamespace,
			},
		},
	}
}

var _ = Describe("GitRepositoryBindingController", Ordered, func() {

	const (
		bindingNamespace = "default"
		argoCDNs         = "argocd"
		credentialKey    = "token"
		testToken        = "fixture-credential-initial"
		rotatedToken     = "fixture-credential-rotated"
	)

	BeforeAll(func() {
		// Ensure the argocd namespace exists for ArgoCD Secret creation tests.
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: argoCDNs,
			},
		}
		err := k8sClient.Create(ctx, ns)
		if err != nil && !errors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}
	})

	Context("happy path: binding created with valid credential", func() {
		var reachableServer *httptest.Server

		BeforeAll(func() {
			// Start a real HTTP server that simulates a reachable Gitea API.
			reachableServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id":1,"name":"test-gitops","full_name":"loopzero/test-gitops"}`))
			}))
		})

		AfterAll(func() {
			reachableServer.Close()
		})

		It("should resolve credentials, create ArgoCD Secret, and set all conditions to True", func() {
			// Arrange: create the credential source Secret.
			credSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "grb-happy-cred",
					Namespace: bindingNamespace,
				},
				Data: map[string][]byte{
					credentialKey: []byte(testToken),
				},
			}
			Expect(k8sClient.Create(ctx, credSecret)).To(Succeed())

			repoURL := fmt.Sprintf("%s/loopzero/test-gitops.git", reachableServer.URL)

			binding := newGitRepositoryBinding(
				"grb-happy", bindingNamespace,
				repoURL,
				"grb-happy-cred", credentialKey, bindingNamespace,
				"argocd", argoCDNs,
			)
			Expect(k8sClient.Create(ctx, binding)).To(Succeed())

			// Assert: CredentialsResolved=True
			Eventually(func(g Gomega) {
				var fetched loopzerov1.GitRepositoryBinding
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "grb-happy", Namespace: bindingNamespace,
				}, &fetched)).To(Succeed())

				cond := findCondition(fetched.Status.Conditions, "CredentialsResolved")
				g.Expect(cond).NotTo(BeNil(), "expected CredentialsResolved condition")
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			}, timeout, interval).Should(Succeed())

			// Assert: RepositoryRegistered=True (ArgoCD Secret created)
			Eventually(func(g Gomega) {
				var fetched loopzerov1.GitRepositoryBinding
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "grb-happy", Namespace: bindingNamespace,
				}, &fetched)).To(Succeed())

				cond := findCondition(fetched.Status.Conditions, "RepositoryRegistered")
				g.Expect(cond).NotTo(BeNil(), "expected RepositoryRegistered condition")
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))

				// Credential hash should be set.
				g.Expect(fetched.Status.CredentialHash).NotTo(BeEmpty())
			}, timeout, interval).Should(Succeed())

			// Assert: ArgoCD Secret exists in argocd namespace with correct labels and data.
			Eventually(func(g Gomega) {
				argoSecretName := fmt.Sprintf("repo-%s-gitops", "grb-happy")
				var argoSecret corev1.Secret
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      argoSecretName,
					Namespace: argoCDNs,
				}, &argoSecret)).To(Succeed())

				g.Expect(argoSecret.Labels).To(HaveKeyWithValue("argocd.argoproj.io/secret-type", "repository"))
				g.Expect(argoSecret.Labels).To(HaveKeyWithValue("loopzero.io/managed-by", "gitrepositorybinding-controller"))
				g.Expect(argoSecret.Data).To(HaveKey("type"))
				g.Expect(argoSecret.Data).To(HaveKey("url"))
				g.Expect(argoSecret.Data).To(HaveKey("username"))
				g.Expect(argoSecret.Data).To(HaveKey("password"))
				g.Expect(string(argoSecret.Data["type"])).To(Equal("git"))
				g.Expect(string(argoSecret.Data["url"])).To(Equal(repoURL))
				g.Expect(string(argoSecret.Data["username"])).To(Equal("loopzero-platform"))
				g.Expect(string(argoSecret.Data["password"])).To(Equal(testToken))
			}, timeout, interval).Should(Succeed())

			// Assert: RepositoryReachable=True
			Eventually(func(g Gomega) {
				var fetched loopzerov1.GitRepositoryBinding
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "grb-happy", Namespace: bindingNamespace,
				}, &fetched)).To(Succeed())

				cond := findCondition(fetched.Status.Conditions, "RepositoryReachable")
				g.Expect(cond).NotTo(BeNil(), "expected RepositoryReachable condition")
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("credential Secret missing", func() {
		It("should set CredentialsResolved=False and requeue", func() {
			binding := newGitRepositoryBinding(
				"grb-no-cred", bindingNamespace,
				"http://gitea.example.com/loopzero/missing-gitops.git",
				"nonexistent-cred-secret", credentialKey, bindingNamespace,
				"argocd", argoCDNs,
			)
			Expect(k8sClient.Create(ctx, binding)).To(Succeed())

			Eventually(func(g Gomega) {
				var fetched loopzerov1.GitRepositoryBinding
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "grb-no-cred", Namespace: bindingNamespace,
				}, &fetched)).To(Succeed())

				cond := findCondition(fetched.Status.Conditions, "CredentialsResolved")
				g.Expect(cond).NotTo(BeNil(), "expected CredentialsResolved condition")
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal("NotFound"))
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("credential rotation: token changed in source Secret", func() {
		var rotationServer *httptest.Server

		BeforeAll(func() {
			rotationServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id":1,"name":"rotation-gitops"}`))
			}))
		})

		AfterAll(func() {
			rotationServer.Close()
		})

		It("should update ArgoCD Secret with the new credential and new hash", func() {
			// Arrange: create credential with initial token.
			credSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "grb-rotation-cred",
					Namespace: bindingNamespace,
				},
				Data: map[string][]byte{
					credentialKey: []byte(testToken),
				},
			}
			Expect(k8sClient.Create(ctx, credSecret)).To(Succeed())

			repoURL := fmt.Sprintf("%s/loopzero/rotation-gitops.git", rotationServer.URL)

			binding := newGitRepositoryBinding(
				"grb-rotation", bindingNamespace,
				repoURL,
				"grb-rotation-cred", credentialKey, bindingNamespace,
				"argocd", argoCDNs,
			)
			Expect(k8sClient.Create(ctx, binding)).To(Succeed())

			// Wait for initial reconciliation to complete.
			var initialHash string
			Eventually(func(g Gomega) {
				var fetched loopzerov1.GitRepositoryBinding
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "grb-rotation", Namespace: bindingNamespace,
				}, &fetched)).To(Succeed())

				g.Expect(fetched.Status.CredentialHash).NotTo(BeEmpty())
				initialHash = fetched.Status.CredentialHash

				cond := findCondition(fetched.Status.Conditions, "RepositoryRegistered")
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			}, timeout, interval).Should(Succeed())

			// Act: rotate the credential.
			var cred corev1.Secret
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "grb-rotation-cred", Namespace: bindingNamespace,
			}, &cred)).To(Succeed())
			Expect(operatortest.UpdateWithRetry(ctx, k8sClient, &cred, func() {
				cred.Data[credentialKey] = []byte(rotatedToken)
			})).To(Succeed())

			// Trigger a reconcile by updating the binding's annotation.
			var bindingCR loopzerov1.GitRepositoryBinding
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "grb-rotation", Namespace: bindingNamespace,
			}, &bindingCR)).To(Succeed())
			Expect(operatortest.UpdateWithRetry(ctx, k8sClient, &bindingCR, func() {
				// Inside the closure: the helper re-reads bindingCR before each
				// attempt, so a nil-map guard placed outside is undone on the retry.
				if bindingCR.Annotations == nil {
					bindingCR.Annotations = make(map[string]string)
				}
				bindingCR.Annotations["platform.loopzero.io/force-reconcile"] = "true"
			})).To(Succeed())

			// Assert: hash changed and ArgoCD Secret updated.
			Eventually(func(g Gomega) {
				var fetched loopzerov1.GitRepositoryBinding
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "grb-rotation", Namespace: bindingNamespace,
				}, &fetched)).To(Succeed())

				g.Expect(fetched.Status.CredentialHash).NotTo(Equal(initialHash),
					"expected credential hash to change after rotation")
			}, 30*time.Second, interval).Should(Succeed())

			// Verify ArgoCD Secret has the rotated password.
			Eventually(func(g Gomega) {
				argoSecretName := fmt.Sprintf("repo-%s-gitops", "grb-rotation")
				var argoSecret corev1.Secret
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      argoSecretName,
					Namespace: argoCDNs,
				}, &argoSecret)).To(Succeed())
				g.Expect(string(argoSecret.Data["password"])).To(Equal(rotatedToken))
			}, 30*time.Second, interval).Should(Succeed())
		})
	})

	Context("delete binding with finalizer cleanup", func() {
		var deleteServer *httptest.Server

		BeforeAll(func() {
			deleteServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id":1,"name":"delete-gitops"}`))
			}))
		})

		AfterAll(func() {
			deleteServer.Close()
		})

		It("should remove the ArgoCD Secret when the binding is deleted", func() {
			// Arrange: create credential and binding.
			credSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "grb-delete-cred",
					Namespace: bindingNamespace,
				},
				Data: map[string][]byte{
					credentialKey: []byte(testToken),
				},
			}
			Expect(k8sClient.Create(ctx, credSecret)).To(Succeed())

			repoURL := fmt.Sprintf("%s/loopzero/delete-gitops.git", deleteServer.URL)

			binding := newGitRepositoryBinding(
				"grb-delete", bindingNamespace,
				repoURL,
				"grb-delete-cred", credentialKey, bindingNamespace,
				"argocd", argoCDNs,
			)
			Expect(k8sClient.Create(ctx, binding)).To(Succeed())

			// Wait for ArgoCD Secret to be created.
			argoSecretName := fmt.Sprintf("repo-%s-gitops", "grb-delete")
			Eventually(func(g Gomega) {
				var argoSecret corev1.Secret
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      argoSecretName,
					Namespace: argoCDNs,
				}, &argoSecret)).To(Succeed())
			}, timeout, interval).Should(Succeed())

			// Act: delete the binding.
			var toDelete loopzerov1.GitRepositoryBinding
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "grb-delete", Namespace: bindingNamespace,
			}, &toDelete)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &toDelete)).To(Succeed())

			// Assert: ArgoCD Secret should be cleaned up.
			Eventually(func(g Gomega) {
				var argoSecret corev1.Secret
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      argoSecretName,
					Namespace: argoCDNs,
				}, &argoSecret)
				g.Expect(errors.IsNotFound(err)).To(BeTrue(), "expected ArgoCD secret to be deleted")
			}, timeout, interval).Should(Succeed())

			// Assert: the CR itself should be gone.
			Eventually(func(g Gomega) {
				var gone loopzerov1.GitRepositoryBinding
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: "grb-delete", Namespace: bindingNamespace,
				}, &gone)
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("idempotency: reconciling same state produces same result", func() {
		var idempotentServer *httptest.Server

		BeforeAll(func() {
			idempotentServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id":1,"name":"idempotent-gitops"}`))
			}))
		})

		AfterAll(func() {
			idempotentServer.Close()
		})

		It("should not change state when reconciled multiple times with same input", func() {
			credSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "grb-idempotent-cred",
					Namespace: bindingNamespace,
				},
				Data: map[string][]byte{
					credentialKey: []byte(testToken),
				},
			}
			Expect(k8sClient.Create(ctx, credSecret)).To(Succeed())

			repoURL := fmt.Sprintf("%s/loopzero/idempotent-gitops.git", idempotentServer.URL)

			binding := newGitRepositoryBinding(
				"grb-idempotent", bindingNamespace,
				repoURL,
				"grb-idempotent-cred", credentialKey, bindingNamespace,
				"argocd", argoCDNs,
			)
			Expect(k8sClient.Create(ctx, binding)).To(Succeed())

			// Wait for first reconciliation to complete.
			Eventually(func(g Gomega) {
				var fetched loopzerov1.GitRepositoryBinding
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "grb-idempotent", Namespace: bindingNamespace,
				}, &fetched)).To(Succeed())

				cond := findCondition(fetched.Status.Conditions, "RepositoryRegistered")
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			}, timeout, interval).Should(Succeed())

			// Assert: state remains stable over multiple reconciliation cycles.
			argoSecretName := fmt.Sprintf("repo-%s-gitops", "grb-idempotent")
			Consistently(func(g Gomega) {
				var fetched loopzerov1.GitRepositoryBinding
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "grb-idempotent", Namespace: bindingNamespace,
				}, &fetched)).To(Succeed())

				cond := findCondition(fetched.Status.Conditions, "CredentialsResolved")
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))

				var argoSecret corev1.Secret
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      argoSecretName,
					Namespace: argoCDNs,
				}, &argoSecret)).To(Succeed())
			}, 3*time.Second, interval).Should(Succeed())
		})
	})
})
