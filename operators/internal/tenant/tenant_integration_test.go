// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package tenant_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/conditions"
	"github.com/getloopscale/loop-runtime/operators/operatortest"
)

// conditionStatus returns the condition status for a given type, or "" if not found.
func conditionStatus(tenant *loopruntimev1.Tenant, condType string) metav1.ConditionStatus {
	for _, c := range tenant.Status.Conditions {
		if c.Type == condType {
			return c.Status
		}
	}
	return ""
}

var _ = Describe("TenantController Integration", Ordered, func() {

	Context("happy path — full provisioning", func() {
		const tenantName = "integ-acme"
		var tenant *loopruntimev1.Tenant

		It("should provision all resources and reach Active phase", func() {
			tenant = newTenant(tenantName, loopruntimev1.PlanStarter, []string{"production", "staging"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			// Wait for Phase=Active — must use outer `tenant` pointer so that
			// later assertions (lines below + line 97 conditions) see the
			// fully-reconciled state, not stale data from before Create returned.
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, tenant)).To(Succeed())
				g.Expect(tenant.Status.Phase).To(Equal(loopruntimev1.TenantPhaseActive))
			}, timeout, interval).Should(Succeed())

			// Verify ALL expected objects exist (async — EnvironmentController creates these)
			// Namespaces + ResourceQuotas + RBAC + NetworkPolicy
			Eventually(func(g Gomega) {
				for _, env := range []string{"production", "staging"} {
					var ns corev1.Namespace
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name: fmt.Sprintf("loopruntime-%s-default-project-%s", tenantName, env),
					}, &ns)).To(Succeed())
					g.Expect(ns.Labels).To(HaveKeyWithValue("runtime.getloopscale.com/tenant", tenantName))
					g.Expect(ns.Labels).To(HaveKeyWithValue("runtime.getloopscale.com/environment", env))

					var quota corev1.ResourceQuota
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name:      "tenant-quota",
						Namespace: fmt.Sprintf("loopruntime-%s-default-project-%s", tenantName, env),
					}, &quota)).To(Succeed())
					cpuVal := quota.Spec.Hard[corev1.ResourceRequestsCPU]
					g.Expect(cpuVal.String()).To(Equal("8"))
				}

				var sa corev1.ServiceAccount
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "loopruntime-deployer", Namespace: "loopruntime-" + tenantName + "-default-project-production",
				}, &sa)).To(Succeed())

				var policies networkingv1.NetworkPolicyList
				g.Expect(k8sClient.List(ctx, &policies, client.InNamespace("loopruntime-"+tenantName+"-default-project-production"))).To(Succeed())
				g.Expect(len(policies.Items)).To(BeNumerically(">=", 3))
			}, timeout, interval).Should(Succeed())

			// Build namespace
			var buildNs corev1.Namespace
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "build-" + tenantName,
			}, &buildNs)).To(Succeed())
			Expect(buildNs.Labels).To(HaveKeyWithValue("runtime.getloopscale.com/component", "build"))

			// REVIEW-BLOCKER: Build namespace must have egress-restricting NetworkPolicy
			var buildPolicies networkingv1.NetworkPolicyList
			Expect(k8sClient.List(ctx, &buildPolicies, client.InNamespace("build-"+tenantName))).To(Succeed())
			Expect(len(buildPolicies.Items)).To(BeNumerically(">=", 1), "build namespace must have at least 1 NetworkPolicy")
			var foundEgressPolicy bool
			for _, p := range buildPolicies.Items {
				if p.Name == "build-egress-restrict" {
					foundEgressPolicy = true
					Expect(p.Spec.PolicyTypes).To(ContainElement(networkingv1.PolicyTypeEgress))
				}
			}
			Expect(foundEgressPolicy).To(BeTrue(), "build-egress-restrict NetworkPolicy must exist")

			// Tenant-scoped conditions True (namespace/RBAC/NetworkPolicy are now on Environment CRs)
			Expect(conditionStatus(tenant, conditions.TypeGitRepoReady)).To(Equal(metav1.ConditionTrue))
			Expect(conditionStatus(tenant, conditions.TypeBuildNamespaceReady)).To(Equal(metav1.ConditionTrue))
			Expect(conditionStatus(tenant, conditions.TypeQuotaEnforced)).To(Equal(metav1.ConditionTrue))

			// Verify Environment CRs exist and are Active (async — EnvironmentController reconciles)
			Eventually(func(g Gomega) {
				for _, env := range []string{"production", "staging"} {
					var envCR loopruntimev1.Environment
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name: tenantName + "-default-project-" + env, Namespace: "default",
					}, &envCR)).To(Succeed())
					g.Expect(envCR.Status.Phase).To(Equal(loopruntimev1.EnvironmentPhaseActive))
				}
			}, timeout, interval).Should(Succeed())

			// ObservedGeneration
			Expect(tenant.Status.ObservedGeneration).To(Equal(tenant.Generation))

			// GitOps repo
			Expect(tenant.Status.GitOpsRepo).ToNot(BeEmpty())
		})

		AfterAll(func() {
			// Clean up Environment CRs (remove finalizers)
			for _, env := range []string{"production", "staging"} {
				envCR := &loopruntimev1.Environment{}
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName + "-default-project-" + env, Namespace: "default",
				}, envCR); err == nil {
					_ = operatortest.UpdateWithRetry(ctx, k8sClient, envCR, func() {
						envCR.Finalizers = nil
					})
					_ = k8sClient.Delete(ctx, envCR)
				}
			}
			for _, nsName := range []string{
				"loopruntime-" + tenantName + "-default-project-production",
				"loopruntime-" + tenantName + "-default-project-staging",
				"build-" + tenantName,
			} {
				ns := &corev1.Namespace{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, ns); err == nil {
					_ = k8sClient.Delete(ctx, ns)
				}
			}
			deleteTenantWithFinalizer(tenantName)
		})
	})

	Context("partial failure — Gitea unavailable", func() {
		const tenantName = "integ-gitfail"

		It("should provision K8s resources but report GitRepoReady=False", func() {
			// Inject Gitea failure
			fakeGitProvider.SetErr(fmt.Errorf("gitea unavailable: connection refused"))

			tenant := newTenant(tenantName, loopruntimev1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			// Should stay in Provisioning due to GitRepoReady=False
			withTenant(tenantName, func(g Gomega, t *loopruntimev1.Tenant) {
				g.Expect(t.Status.Phase).To(Equal(loopruntimev1.TenantPhaseProvisioning))
				g.Expect(conditionStatus(t, conditions.TypeGitRepoReady)).To(Equal(metav1.ConditionFalse))
			})

			// Remove error — controller should recover
			fakeGitProvider.SetErr(nil)

			withTenant(tenantName, func(g Gomega, t *loopruntimev1.Tenant) {
				g.Expect(t.Status.Phase).To(Equal(loopruntimev1.TenantPhaseActive))
				g.Expect(conditionStatus(t, conditions.TypeGitRepoReady)).To(Equal(metav1.ConditionTrue))
			})
		})

		AfterAll(func() {
			fakeGitProvider.SetErr(nil)
			cleanupTenantEnvCRs(tenantName)
			deleteTenantNamespaces(tenantName)
			deleteTenantWithFinalizer(tenantName)
		})
	})

	Context("idempotency — 3 consecutive reconciles", func() {
		const tenantName = "integ-idemp"

		It("should produce identical state after multiple reconciles", func() {
			tenant := newTenant(tenantName, loopruntimev1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			// Wait for Active
			withTenant(tenantName, func(g Gomega, t *loopruntimev1.Tenant) {
				g.Expect(t.Status.Phase).To(Equal(loopruntimev1.TenantPhaseActive))
			})

			// Count resources
			countResources := func() (int, int, int) {
				var nsList corev1.NamespaceList
				Expect(k8sClient.List(ctx, &nsList, client.MatchingLabels{
					"runtime.getloopscale.com/tenant": tenantName,
				})).To(Succeed())

				var policies networkingv1.NetworkPolicyList
				Expect(k8sClient.List(ctx, &policies, client.InNamespace("loopruntime-"+tenantName+"-default-project-production"))).To(Succeed())

				var quotas corev1.ResourceQuotaList
				Expect(k8sClient.List(ctx, &quotas, client.InNamespace("loopruntime-"+tenantName+"-default-project-production"))).To(Succeed())

				return len(nsList.Items), len(policies.Items), len(quotas.Items)
			}

			// Wait until Environment-derived resources are settled (race: TenantPhaseActive
			// is set before EnvironmentController finishes creating NPs/quotas in the runtime
			// namespace). The 3 expected NPs come from environment_network.go (DenyAll +
			// AllowIngress + AllowEgress) and the quota from environment_quota.go.
			Eventually(func(g Gomega) {
				_, pol, q := countResources()
				g.Expect(pol).To(Equal(3), "expected 3 NetworkPolicies (DenyAll, AllowIngress, AllowEgress)")
				g.Expect(q).To(BeNumerically(">=", 1), "expected at least 1 ResourceQuota")
			}, timeout, interval).Should(Succeed())

			ns1, pol1, q1 := countResources()

			// Trigger 2 more reconciles via spec updates (retry on conflict)
			for i := 0; i < 2; i++ {
				displayName := fmt.Sprintf("idemp-%d", i)
				Eventually(func(g Gomega) {
					var t loopruntimev1.Tenant
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name: tenantName, Namespace: "default",
					}, &t)).To(Succeed())
					t.Spec.DisplayName = displayName
					g.Expect(k8sClient.Update(ctx, &t)).To(Succeed())
				}, timeout, interval).Should(Succeed())

				// Wait for reconcile
				Eventually(func(g Gomega) {
					var updated loopruntimev1.Tenant
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name: tenantName, Namespace: "default",
					}, &updated)).To(Succeed())
					g.Expect(updated.Spec.DisplayName).To(Equal(displayName))
				}, timeout, interval).Should(Succeed())
			}

			// Count again — must be identical
			ns2, pol2, q2 := countResources()
			Expect(ns2).To(Equal(ns1), "namespace count should be identical")
			Expect(pol2).To(Equal(pol1), "policy count should be identical")
			Expect(q2).To(Equal(q1), "quota count should be identical")
		})

		AfterAll(func() {
			cleanupTenantEnvCRs(tenantName)
			deleteTenantNamespaces(tenantName)
			deleteTenantWithFinalizer(tenantName)
		})
	})

	Context("spec update — plan upgrade", func() {
		const tenantName = "integ-upgrade"

		It("should re-reconcile RBAC when plan changes from starter to growth", func() {
			tenant := newTenant(tenantName, loopruntimev1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			// Wait Active
			withTenant(tenantName, func(g Gomega, t *loopruntimev1.Tenant) {
				g.Expect(t.Status.Phase).To(Equal(loopruntimev1.TenantPhaseActive))
			})

			// Upgrade to growth — retry on conflict (reconciler may update status concurrently)
			Eventually(func(g Gomega) {
				var t loopruntimev1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				t.Spec.Plan = loopruntimev1.PlanGrowth
				g.Expect(k8sClient.Update(ctx, &t)).To(Succeed())
			}, "10s", "500ms").Should(Succeed())

			// Verify Role now includes secrets
			expectDeployerRoleHasResource("loopruntime-"+tenantName+"-default-project-production", "secrets")
		})

		AfterAll(func() {
			cleanupTenantEnvCRs(tenantName)
			deleteTenantNamespaces(tenantName)
			deleteTenantWithFinalizer(tenantName)
		})
	})

	Context("deletion via finalizer (waits for child Environment CRs)", func() {
		const tenantName = "integ-delete"

		It("should wait for child Environment CRs, then remove finalizer", func() {
			tenant := newTenant(tenantName, loopruntimev1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			// Wait Active
			Eventually(func(g Gomega) {
				var t loopruntimev1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Status.Phase).To(Equal(loopruntimev1.TenantPhaseActive))
			}, timeout, interval).Should(Succeed())

			// Verify finalizer present
			var t loopruntimev1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			Expect(t.Finalizers).To(ContainElement("runtime.getloopscale.com/tenant-cleanup"))

			// Delete
			Expect(k8sClient.Delete(ctx, &t)).To(Succeed())

			// Phase should become Deleting
			Eventually(func(g Gomega) {
				var d loopruntimev1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &d)).To(Succeed())
				g.Expect(d.Status.Phase).To(Equal(loopruntimev1.TenantPhaseDeleting))
			}, timeout, interval).Should(Succeed())

			// Tenant should still have finalizer (waiting for child Environment CRs)
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			Expect(t.Finalizers).To(ContainElement("runtime.getloopscale.com/tenant-cleanup"))

			// Simulate child Environment CR cascade deletion + cleanup
			// (envtest doesn't run GC, so we must manually delete and remove finalizers)
			var envList loopruntimev1.EnvironmentList
			Expect(k8sClient.List(ctx, &envList, client.InNamespace("default"))).To(Succeed())
			for i := range envList.Items {
				env := &envList.Items[i]
				for _, ref := range env.OwnerReferences {
					if ref.Name == tenantName {
						if env.DeletionTimestamp.IsZero() {
							Expect(k8sClient.Delete(ctx, env)).To(Succeed())
						}
						break
					}
				}
			}

			// Remove environment-cleanup finalizers and namespace labels
			Eventually(func(g Gomega) {
				var envs loopruntimev1.EnvironmentList
				g.Expect(k8sClient.List(ctx, &envs, client.InNamespace("default"))).To(Succeed())
				for i := range envs.Items {
					env := &envs.Items[i]
					for _, ref := range env.OwnerReferences {
						if ref.Name == tenantName {
							nsName := env.Spec.Namespace.Name
							var ns corev1.Namespace
							if err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, &ns); err == nil {
								ns.Finalizers = nil
								delete(ns.Labels, "runtime.getloopscale.com/tenant")
								g.Expect(k8sClient.Update(ctx, &ns)).To(Succeed())
							}
							env.Finalizers = nil
							g.Expect(k8sClient.Update(ctx, env)).To(Succeed())
							break
						}
					}
				}
			}, timeout, interval).Should(Succeed())

			// Clean up build namespaces
			var buildNsList corev1.NamespaceList
			_ = k8sClient.List(ctx, &buildNsList, client.MatchingLabels{"runtime.getloopscale.com/tenant": tenantName})
			for i := range buildNsList.Items {
				ns := &buildNsList.Items[i]
				_ = operatortest.UpdateWithRetry(ctx, k8sClient, ns, func() {
					ns.Finalizers = nil
					delete(ns.Labels, "runtime.getloopscale.com/tenant")
				})
			}

			// Tenant should be fully deleted
			expectTenantDeleted(tenantName)
		})
	})
})
