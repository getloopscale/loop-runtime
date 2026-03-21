package controller_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
)

// conditionStatus returns the condition status for a given type, or "" if not found.
func conditionStatus(tenant *loopzerov1.Tenant, condType string) metav1.ConditionStatus {
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
		var tenant *loopzerov1.Tenant

		It("should provision all resources and reach Active phase", func() {
			tenant = newTenant(tenantName, loopzerov1.PlanStarter, []string{"production", "staging"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			// Wait for Phase=Active
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, tenant)).To(Succeed())
				g.Expect(tenant.Status.Phase).To(Equal(loopzerov1.TenantPhaseActive))
			}, timeout, interval).Should(Succeed())

			// Verify ALL expected objects exist
			// Namespaces
			for _, env := range []string{"production", "staging"} {
				var ns corev1.Namespace
				Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: fmt.Sprintf("%s-%s", tenantName, env),
				}, &ns)).To(Succeed())
				Expect(ns.Labels).To(HaveKeyWithValue("loopzero.io/tenant", tenantName))
				Expect(ns.Labels).To(HaveKeyWithValue("loopzero.io/environment", env))
			}

			// ResourceQuotas
			for _, env := range []string{"production", "staging"} {
				var quota corev1.ResourceQuota
				Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      "tenant-quota",
					Namespace: fmt.Sprintf("%s-%s", tenantName, env),
				}, &quota)).To(Succeed())
				cpuVal := quota.Spec.Hard[corev1.ResourceRequestsCPU]
				Expect(cpuVal.String()).To(Equal("8"))
			}

			// RBAC
			var sa corev1.ServiceAccount
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "loopzero-deployer", Namespace: tenantName + "-production",
			}, &sa)).To(Succeed())

			// NetworkPolicy (>= 3 per namespace)
			var policies networkingv1.NetworkPolicyList
			Expect(k8sClient.List(ctx, &policies, client.InNamespace(tenantName+"-production"))).To(Succeed())
			Expect(len(policies.Items)).To(BeNumerically(">=", 3))

			// Build namespace
			var buildNs corev1.Namespace
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "build-" + tenantName,
			}, &buildNs)).To(Succeed())
			Expect(buildNs.Labels).To(HaveKeyWithValue("loopzero.io/component", "build"))

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

			// All 6 conditions True
			Expect(conditionStatus(tenant, loopzerov1.ConditionGitRepoReady)).To(Equal(metav1.ConditionTrue))
			Expect(conditionStatus(tenant, loopzerov1.ConditionNamespacesReady)).To(Equal(metav1.ConditionTrue))
			Expect(conditionStatus(tenant, loopzerov1.ConditionRBACReady)).To(Equal(metav1.ConditionTrue))
			Expect(conditionStatus(tenant, loopzerov1.ConditionNetworkPolicyReady)).To(Equal(metav1.ConditionTrue))
			Expect(conditionStatus(tenant, loopzerov1.ConditionBuildNamespaceReady)).To(Equal(metav1.ConditionTrue))
			Expect(conditionStatus(tenant, loopzerov1.ConditionQuotaEnforced)).To(Equal(metav1.ConditionTrue))

			// ObservedGeneration
			Expect(tenant.Status.ObservedGeneration).To(Equal(tenant.Generation))

			// GitOps repo
			Expect(tenant.Status.GitOpsRepo).ToNot(BeEmpty())
		})

		AfterAll(func() {
			for _, nsName := range []string{
				tenantName + "-production",
				tenantName + "-staging",
				"build-" + tenantName,
			} {
				ns := &corev1.Namespace{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, ns); err == nil {
					_ = k8sClient.Delete(ctx, ns)
				}
			}
			t := &loopzerov1.Tenant{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: tenantName, Namespace: "default"}, t); err == nil {
				t.Finalizers = nil
				_ = k8sClient.Update(ctx, t)
				_ = k8sClient.Delete(ctx, t)
			}
		})
	})

	Context("partial failure — Gitea unavailable", func() {
		const tenantName = "integ-gitfail"

		It("should provision K8s resources but report GitRepoReady=False", func() {
			// Inject Gitea failure
			fakeGitProvider.SetErr(fmt.Errorf("gitea unavailable: connection refused"))

			tenant := newTenant(tenantName, loopzerov1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			// Should stay in Provisioning due to GitRepoReady=False
			Eventually(func(g Gomega) {
				var t loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Status.Phase).To(Equal(loopzerov1.TenantPhaseProvisioning))
				g.Expect(conditionStatus(&t, loopzerov1.ConditionGitRepoReady)).To(Equal(metav1.ConditionFalse))
			}, timeout, interval).Should(Succeed())

			// Remove error — controller should recover
			fakeGitProvider.SetErr(nil)

			Eventually(func(g Gomega) {
				var t loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Status.Phase).To(Equal(loopzerov1.TenantPhaseActive))
				g.Expect(conditionStatus(&t, loopzerov1.ConditionGitRepoReady)).To(Equal(metav1.ConditionTrue))
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			fakeGitProvider.SetErr(nil)
			for _, nsName := range []string{tenantName + "-production", "build-" + tenantName} {
				ns := &corev1.Namespace{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, ns); err == nil {
					_ = k8sClient.Delete(ctx, ns)
				}
			}
			t := &loopzerov1.Tenant{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: tenantName, Namespace: "default"}, t); err == nil {
				t.Finalizers = nil
				_ = k8sClient.Update(ctx, t)
				_ = k8sClient.Delete(ctx, t)
			}
		})
	})

	Context("idempotency — 3 consecutive reconciles", func() {
		const tenantName = "integ-idemp"

		It("should produce identical state after multiple reconciles", func() {
			tenant := newTenant(tenantName, loopzerov1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			// Wait for Active
			Eventually(func(g Gomega) {
				var t loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Status.Phase).To(Equal(loopzerov1.TenantPhaseActive))
			}, timeout, interval).Should(Succeed())

			// Count resources
			countResources := func() (int, int, int) {
				var nsList corev1.NamespaceList
				Expect(k8sClient.List(ctx, &nsList, client.MatchingLabels{
					"loopzero.io/tenant": tenantName,
				})).To(Succeed())

				var policies networkingv1.NetworkPolicyList
				Expect(k8sClient.List(ctx, &policies, client.InNamespace(tenantName+"-production"))).To(Succeed())

				var quotas corev1.ResourceQuotaList
				Expect(k8sClient.List(ctx, &quotas, client.InNamespace(tenantName+"-production"))).To(Succeed())

				return len(nsList.Items), len(policies.Items), len(quotas.Items)
			}

			ns1, pol1, q1 := countResources()

			// Trigger 2 more reconciles via spec updates (retry on conflict)
			for i := 0; i < 2; i++ {
				displayName := fmt.Sprintf("idemp-%d", i)
				Eventually(func(g Gomega) {
					var t loopzerov1.Tenant
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name: tenantName, Namespace: "default",
					}, &t)).To(Succeed())
					t.Spec.DisplayName = displayName
					g.Expect(k8sClient.Update(ctx, &t)).To(Succeed())
				}, timeout, interval).Should(Succeed())

				// Wait for reconcile
				Eventually(func(g Gomega) {
					var updated loopzerov1.Tenant
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
			for _, nsName := range []string{tenantName + "-production", "build-" + tenantName} {
				ns := &corev1.Namespace{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, ns); err == nil {
					_ = k8sClient.Delete(ctx, ns)
				}
			}
			t := &loopzerov1.Tenant{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: tenantName, Namespace: "default"}, t); err == nil {
				t.Finalizers = nil
				_ = k8sClient.Update(ctx, t)
				_ = k8sClient.Delete(ctx, t)
			}
		})
	})

	Context("spec update — plan upgrade", func() {
		const tenantName = "integ-upgrade"

		It("should re-reconcile RBAC when plan changes from starter to growth", func() {
			tenant := newTenant(tenantName, loopzerov1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			// Wait Active
			Eventually(func(g Gomega) {
				var t loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Status.Phase).To(Equal(loopzerov1.TenantPhaseActive))
			}, timeout, interval).Should(Succeed())

			// Upgrade to growth
			var t loopzerov1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			t.Spec.Plan = loopzerov1.PlanGrowth
			Expect(k8sClient.Update(ctx, &t)).To(Succeed())

			// Verify Role now includes secrets
			nsName := tenantName + "-production"
			Eventually(func(g Gomega) {
				var role rbacv1.Role
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "loopzero-deployer", Namespace: nsName,
				}, &role)).To(Succeed())
				hasSecrets := false
				for _, rule := range role.Rules {
					for _, res := range rule.Resources {
						if res == "secrets" {
							hasSecrets = true
						}
					}
				}
				g.Expect(hasSecrets).To(BeTrue(), "growth role should include secrets")
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			for _, nsName := range []string{tenantName + "-production", "build-" + tenantName} {
				ns := &corev1.Namespace{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, ns); err == nil {
					_ = k8sClient.Delete(ctx, ns)
				}
			}
			t := &loopzerov1.Tenant{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: tenantName, Namespace: "default"}, t); err == nil {
				t.Finalizers = nil
				_ = k8sClient.Update(ctx, t)
				_ = k8sClient.Delete(ctx, t)
			}
		})
	})

	Context("deletion via finalizer (verify-then-remove)", func() {
		const tenantName = "integ-delete"

		It("should delete namespaces by label, verify, then remove finalizer", func() {
			tenant := newTenant(tenantName, loopzerov1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			// Wait Active
			Eventually(func(g Gomega) {
				var t loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Status.Phase).To(Equal(loopzerov1.TenantPhaseActive))
			}, timeout, interval).Should(Succeed())

			// Verify finalizer present
			var t loopzerov1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			Expect(t.Finalizers).To(ContainElement("loopzero.io/tenant-cleanup"))

			// Delete
			Expect(k8sClient.Delete(ctx, &t)).To(Succeed())

			// Phase should become Deleting
			Eventually(func(g Gomega) {
				var d loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &d)).To(Succeed())
				g.Expect(d.Status.Phase).To(Equal(loopzerov1.TenantPhaseDeleting))
			}, timeout, interval).Should(Succeed())

			// Namespaces should have DeletionTimestamp
			Eventually(func(g Gomega) {
				var nsList corev1.NamespaceList
				g.Expect(k8sClient.List(ctx, &nsList, client.MatchingLabels{
					"loopzero.io/tenant": tenantName,
				})).To(Succeed())
				for _, ns := range nsList.Items {
					g.Expect(ns.DeletionTimestamp.IsZero()).To(BeFalse())
				}
			}, timeout, interval).Should(Succeed())

			// Simulate namespace cleanup (envtest limitation)
			var nsList corev1.NamespaceList
			Expect(k8sClient.List(ctx, &nsList, client.MatchingLabels{
				"loopzero.io/tenant": tenantName,
			})).To(Succeed())
			for i := range nsList.Items {
				ns := &nsList.Items[i]
				ns.Finalizers = nil
				delete(ns.Labels, "loopzero.io/tenant")
				Expect(k8sClient.Update(ctx, ns)).To(Succeed())
			}

			// Tenant should be fully deleted
			Eventually(func(g Gomega) {
				var deleted loopzerov1.Tenant
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &deleted)
				g.Expect(err).To(HaveOccurred())
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}, 30*time.Second, interval).Should(Succeed())
		})
	})
})
