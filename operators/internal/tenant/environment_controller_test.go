// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	loopzerov1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/conditions"
)

func newEnvironmentCR(name, tenantName, envName, nsName string, plan loopzerov1.PlanTier) *loopzerov1.Environment {
	return &loopzerov1.Environment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: loopzerov1.EnvironmentCRSpec{
			TenantRef:   loopzerov1.TenantRef{Name: tenantName},
			Environment: envName,
			Namespace:   loopzerov1.NamespaceSpec{Name: nsName},
			NetworkPolicy: loopzerov1.NetworkPolicyConfig{
				Isolate: true,
			},
			Plan: plan,
			ResourceQuota: &loopzerov1.ResourceQuotaSpec{
				CPU:     "8",
				Memory:  "16Gi",
				Storage: "50Gi",
			},
		},
	}
}

// createWithParentTenant creates env together with the Tenant it references.
// An Environment is always born from a Tenant, and the Environment controller
// refuses to provision one whose Tenant (and so whose cell) it cannot read.
func createWithParentTenant(env *loopzerov1.Environment) {
	parent := newTenant(env.Spec.TenantRef.Name, loopzerov1.PlanStarter, nil)
	if err := k8sClient.Create(ctx, parent); err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}
	Expect(k8sClient.Create(ctx, env)).To(Succeed())
}

var _ = Describe("EnvironmentController", Ordered, func() {

	Context("when a new Environment CR is created", func() {
		const (
			envCRName  = "env-test-production"
			tenantName = "env-test"
			envName    = "production"
			nsName     = "loopzero-env-test-production"
		)

		BeforeAll(func() {
			envCR := newEnvironmentCR(envCRName, tenantName, envName, nsName, loopzerov1.PlanStarter)
			createWithParentTenant(envCR)
		})

		It("should set Phase to Active", func() {
			withEnv(envCRName, func(g Gomega, env *loopzerov1.Environment) {
				g.Expect(env.Status.Phase).To(Equal(loopzerov1.EnvironmentPhaseActive))
			})
		})

		It("should add the environment-cleanup finalizer", func() {
			withEnv(envCRName, func(g Gomega, env *loopzerov1.Environment) {
				g.Expect(env.Finalizers).To(ContainElement("loopzero.io/environment-cleanup"))
			})
		})

		It("should create the namespace", func() {
			Eventually(func(g Gomega) {
				var ns corev1.Namespace
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, &ns)).To(Succeed())
				g.Expect(ns.Labels).To(HaveKeyWithValue("loopzero.io/tenant", tenantName))
				g.Expect(ns.Labels).To(HaveKeyWithValue("loopzero.io/environment", envName))
				g.Expect(ns.Labels).To(HaveKeyWithValue("loopzero.io/managed-by", "environment-controller"))
			}, timeout, interval).Should(Succeed())
		})

		// Task 7.2: PSA labels on namespaces
		It("should set PodSecurity Admission labels to restricted", func() {
			Eventually(func(g Gomega) {
				var ns corev1.Namespace
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, &ns)).To(Succeed())
				g.Expect(ns.Labels).To(HaveKeyWithValue("pod-security.kubernetes.io/enforce", "restricted"))
				g.Expect(ns.Labels).To(HaveKeyWithValue("pod-security.kubernetes.io/warn", "restricted"))
				g.Expect(ns.Labels).To(HaveKeyWithValue("pod-security.kubernetes.io/audit", "restricted"))
			}, timeout, interval).Should(Succeed())
		})

		// Task 7.4: Dedicated ServiceAccount for app workloads
		It("should create loopzero-app ServiceAccount for workloads", func() {
			Eventually(func(g Gomega) {
				var sa corev1.ServiceAccount
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "loopzero-app", Namespace: nsName,
				}, &sa)).To(Succeed())
				g.Expect(sa.Labels).To(HaveKeyWithValue("loopzero.io/managed-by", "environment-controller"))
				// automountServiceAccountToken should be false
				g.Expect(sa.AutomountServiceAccountToken).NotTo(BeNil())
				g.Expect(*sa.AutomountServiceAccountToken).To(BeFalse())
			}, timeout, interval).Should(Succeed())
		})

		It("should create RBAC resources (ServiceAccount, Role, RoleBinding)", func() {
			Eventually(func(g Gomega) {
				var sa corev1.ServiceAccount
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "loopzero-deployer", Namespace: nsName,
				}, &sa)).To(Succeed())

				var role rbacv1.Role
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "loopzero-deployer", Namespace: nsName,
				}, &role)).To(Succeed())
				// Starter plan: should have deployments
				hasDeployments := false
				for _, rule := range role.Rules {
					for _, res := range rule.Resources {
						if res == "deployments" {
							hasDeployments = true
						}
					}
				}
				g.Expect(hasDeployments).To(BeTrue(), "starter role should include deployments")

				var rb rbacv1.RoleBinding
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "loopzero-deployer", Namespace: nsName,
				}, &rb)).To(Succeed())
				g.Expect(rb.RoleRef.Name).To(Equal("loopzero-deployer"))
				g.Expect(rb.Subjects).To(HaveLen(1))
				g.Expect(rb.Subjects[0].Name).To(Equal("loopzero-deployer"))
			}, timeout, interval).Should(Succeed())
		})

		It("should create NetworkPolicies (deny-all, allow-ingress, allow-egress)", func() {
			Eventually(func(g Gomega) {
				var policies networkingv1.NetworkPolicyList
				g.Expect(k8sClient.List(ctx, &policies, client.InNamespace(nsName))).To(Succeed())
				g.Expect(len(policies.Items)).To(BeNumerically(">=", 3))

				// Verify deny-all exists
				var np networkingv1.NetworkPolicy
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "deny-all", Namespace: nsName,
				}, &np)).To(Succeed())
				g.Expect(np.Spec.PolicyTypes).To(ContainElements(
					networkingv1.PolicyTypeIngress,
					networkingv1.PolicyTypeEgress,
				))
			}, timeout, interval).Should(Succeed())
		})

		It("should create ResourceQuota with spec values", func() {
			Eventually(func(g Gomega) {
				var quota corev1.ResourceQuota
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "tenant-quota", Namespace: nsName,
				}, &quota)).To(Succeed())
				cpuVal := quota.Spec.Hard[corev1.ResourceRequestsCPU]
				g.Expect(cpuVal.String()).To(Equal("8"))
				memVal := quota.Spec.Hard[corev1.ResourceRequestsMemory]
				g.Expect(memVal.String()).To(Equal("16Gi"))
			}, timeout, interval).Should(Succeed())
		})

		It("should create LimitRange with default container limits", func() {
			expectLimitRangeContainerType("tenant-limits", nsName)
		})

		It("should set all conditions to True", func() {
			Eventually(func(g Gomega) {
				var env loopzerov1.Environment
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: envCRName, Namespace: "default",
				}, &env)).To(Succeed())

				for _, condType := range []string{
					conditions.TypeEnvNamespaceReady,
					conditions.TypeEnvRBACReady,
					conditions.TypeEnvNetworkPolicyReady,
					conditions.TypeEnvQuotaReady,
				} {
					found := false
					for _, c := range env.Status.Conditions {
						if c.Type == condType {
							found = true
							g.Expect(c.Status).To(Equal(metav1.ConditionTrue),
								fmt.Sprintf("condition %s should be True", condType))
						}
					}
					g.Expect(found).To(BeTrue(),
						fmt.Sprintf("condition %s should exist", condType))
				}
			}, timeout, interval).Should(Succeed())
		})

		It("should set status.namespace to the actual namespace name", func() {
			withEnv(envCRName, func(g Gomega, env *loopzerov1.Environment) {
				g.Expect(env.Status.Namespace).To(Equal(nsName))
			})
		})

		It("should set observedGeneration", func() {
			withEnv(envCRName, func(g Gomega, env *loopzerov1.Environment) {
				g.Expect(env.Status.ObservedGeneration).To(Equal(env.Generation))
			})
		})

		AfterAll(func() {
			deleteEnvWithFinalizer(envCRName)
			ns := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, ns); err == nil {
				_ = k8sClient.Delete(ctx, ns)
			}
		})
	})

	Context("idempotency — CreateOrUpdate does not duplicate resources", func() {
		const (
			envCRName  = "env-idemp-production"
			tenantName = "env-idemp"
			envName    = "production"
			nsName     = "loopzero-env-idemp-production"
		)

		BeforeAll(func() {
			envCR := newEnvironmentCR(envCRName, tenantName, envName, nsName, loopzerov1.PlanStarter)
			createWithParentTenant(envCR)

			// Wait for Active
			Eventually(func(g Gomega) {
				var env loopzerov1.Environment
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: envCRName, Namespace: "default",
				}, &env)).To(Succeed())
				g.Expect(env.Status.Phase).To(Equal(loopzerov1.EnvironmentPhaseActive))
			}, timeout, interval).Should(Succeed())
		})

		It("should produce identical resource count after re-reconcile", func() {
			// Count resources before re-reconcile
			countResources := func() (int, int, int) {
				var policies networkingv1.NetworkPolicyList
				Expect(k8sClient.List(ctx, &policies, client.InNamespace(nsName))).To(Succeed())

				var quotas corev1.ResourceQuotaList
				Expect(k8sClient.List(ctx, &quotas, client.InNamespace(nsName))).To(Succeed())

				var saList corev1.ServiceAccountList
				Expect(k8sClient.List(ctx, &saList, client.InNamespace(nsName))).To(Succeed())
				saCount := 0
				for _, sa := range saList.Items {
					if sa.Name == "loopzero-deployer" {
						saCount++
					}
				}

				return len(policies.Items), len(quotas.Items), saCount
			}

			pol1, q1, sa1 := countResources()

			// Trigger re-reconcile via spec update — use retry loop for conflict
			Eventually(func(g Gomega) {
				var env loopzerov1.Environment
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: envCRName, Namespace: "default",
				}, &env)).To(Succeed())
				env.Spec.ResourceQuota.CPU = "16"
				g.Expect(k8sClient.Update(ctx, &env)).To(Succeed())
			}, timeout, interval).Should(Succeed())

			// Wait for reconcile to complete with new generation
			withEnv(envCRName, func(g Gomega, env *loopzerov1.Environment) {
				g.Expect(env.Status.ObservedGeneration).To(Equal(env.Generation))
			})

			pol2, q2, sa2 := countResources()
			Expect(pol2).To(Equal(pol1), "policy count should be identical after re-reconcile")
			Expect(q2).To(Equal(q1), "quota count should be identical after re-reconcile")
			Expect(sa2).To(Equal(sa1), "SA count should be identical after re-reconcile")
		})

		It("should update ResourceQuota when spec changes", func() {
			Eventually(func(g Gomega) {
				var quota corev1.ResourceQuota
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "tenant-quota", Namespace: nsName,
				}, &quota)).To(Succeed())
				cpuVal := quota.Spec.Hard[corev1.ResourceRequestsCPU]
				g.Expect(cpuVal.String()).To(Equal("16"))
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			deleteEnvWithFinalizer(envCRName)
			ns := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, ns); err == nil {
				_ = k8sClient.Delete(ctx, ns)
			}
		})
	})

	Context("deletion via finalizer", func() {
		const (
			envCRName  = "env-del-production"
			tenantName = "env-del"
			envName    = "production"
			nsName     = "loopzero-env-del-production"
		)

		It("should clean up namespace when Environment is deleted", func() {
			envCR := newEnvironmentCR(envCRName, tenantName, envName, nsName, loopzerov1.PlanStarter)
			createWithParentTenant(envCR)

			// Wait for Active
			Eventually(func(g Gomega) {
				var env loopzerov1.Environment
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: envCRName, Namespace: "default",
				}, &env)).To(Succeed())
				g.Expect(env.Status.Phase).To(Equal(loopzerov1.EnvironmentPhaseActive))
			}, timeout, interval).Should(Succeed())

			// Delete
			var env loopzerov1.Environment
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: envCRName, Namespace: "default",
			}, &env)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &env)).To(Succeed())

			// Namespace should have DeletionTimestamp
			Eventually(func(g Gomega) {
				var ns corev1.Namespace
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, &ns)).To(Succeed())
				g.Expect(ns.DeletionTimestamp.IsZero()).To(BeFalse())
			}, timeout, interval).Should(Succeed())

			// Simulate namespace cleanup (envtest limitation)
			var ns corev1.Namespace
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, &ns)).To(Succeed())
			ns.Finalizers = nil
			delete(ns.Labels, "loopzero.io/tenant")
			Expect(k8sClient.Update(ctx, &ns)).To(Succeed())

			// Environment CR should be fully deleted (finalizer removed -> K8s deletes)
			Eventually(func(g Gomega) {
				var deleted loopzerov1.Environment
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: envCRName, Namespace: "default",
				}, &deleted)
				if err == nil {
					// Still exists — check finalizer is gone (will be garbage collected)
					g.Expect(deleted.Finalizers).ToNot(ContainElement("loopzero.io/environment-cleanup"))
				}
				// Either NotFound or finalizer removed is success
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("without ResourceQuota spec", func() {
		const (
			envCRName  = "env-noquota-staging"
			tenantName = "env-noquota"
			envName    = "staging"
			nsName     = "loopzero-env-noquota-staging"
		)

		BeforeAll(func() {
			envCR := &loopzerov1.Environment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      envCRName,
					Namespace: "default",
				},
				Spec: loopzerov1.EnvironmentCRSpec{
					TenantRef:   loopzerov1.TenantRef{Name: tenantName},
					Environment: envName,
					Namespace:   loopzerov1.NamespaceSpec{Name: nsName},
					NetworkPolicy: loopzerov1.NetworkPolicyConfig{
						Isolate: true,
					},
					Plan: loopzerov1.PlanStarter,
					// No ResourceQuota
				},
			}
			createWithParentTenant(envCR)
		})

		It("should reach Active without creating ResourceQuota", func() {
			Eventually(func(g Gomega) {
				var env loopzerov1.Environment
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: envCRName, Namespace: "default",
				}, &env)).To(Succeed())
				g.Expect(env.Status.Phase).To(Equal(loopzerov1.EnvironmentPhaseActive))
			}, timeout, interval).Should(Succeed())

			// Namespace should exist
			var ns corev1.Namespace
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, &ns)).To(Succeed())

			// QuotaReady condition should still be True (skip means "nothing to do")
			expectEnvCondition(envCRName, conditions.TypeEnvQuotaReady, metav1.ConditionTrue)
		})

		AfterAll(func() {
			deleteEnvWithFinalizer(envCRName)
			ns := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, ns); err == nil {
				_ = k8sClient.Delete(ctx, ns)
			}
		})
	})

	// Task 7.3: NetworkPolicy must be created even when Isolate=false
	Context("NetworkPolicy with Isolate=false", func() {
		const (
			envCRName  = "env-noisol-production"
			tenantName = "env-noisol"
			envName    = "production"
			nsName     = "loopzero-env-noisol-production"
		)

		BeforeAll(func() {
			envCR := &loopzerov1.Environment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      envCRName,
					Namespace: "default",
				},
				Spec: loopzerov1.EnvironmentCRSpec{
					TenantRef:   loopzerov1.TenantRef{Name: tenantName},
					Environment: envName,
					Namespace:   loopzerov1.NamespaceSpec{Name: nsName},
					NetworkPolicy: loopzerov1.NetworkPolicyConfig{
						Isolate: false,
					},
					Plan: loopzerov1.PlanStarter,
				},
			}
			createWithParentTenant(envCR)
		})

		It("should still create NetworkPolicies even with Isolate=false", func() {
			Eventually(func(g Gomega) {
				var env loopzerov1.Environment
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: envCRName, Namespace: "default",
				}, &env)).To(Succeed())
				g.Expect(env.Status.Phase).To(Equal(loopzerov1.EnvironmentPhaseActive))

				var policies networkingv1.NetworkPolicyList
				g.Expect(k8sClient.List(ctx, &policies, client.InNamespace(nsName))).To(Succeed())
				g.Expect(len(policies.Items)).To(BeNumerically(">=", 1),
					"NetworkPolicy must exist even with Isolate=false")
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			deleteEnvWithFinalizer(envCRName)
			ns := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, ns); err == nil {
				_ = k8sClient.Delete(ctx, ns)
			}
		})
	})

	Context("enterprise plan RBAC", func() {
		const (
			envCRName  = "env-ent-production"
			tenantName = "env-ent"
			envName    = "production"
			nsName     = "loopzero-env-ent-production"
		)

		BeforeAll(func() {
			envCR := newEnvironmentCR(envCRName, tenantName, envName, nsName, loopzerov1.PlanEnterprise)
			createWithParentTenant(envCR)

			Eventually(func(g Gomega) {
				var env loopzerov1.Environment
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: envCRName, Namespace: "default",
				}, &env)).To(Succeed())
				g.Expect(env.Status.Phase).To(Equal(loopzerov1.EnvironmentPhaseActive))
			}, timeout, interval).Should(Succeed())
		})

		It("should include extra RBAC permissions for enterprise plan", func() {
			expectDeployerRoleHasResource(nsName, "ingresses")
		})

		AfterAll(func() {
			deleteEnvWithFinalizer(envCRName)
			ns := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, ns); err == nil {
				_ = k8sClient.Delete(ctx, ns)
			}
		})
	})
})
