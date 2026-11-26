// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package portable_tests — NetworkPolicy golden tests.
//
// Source mapping:
//   - Capsule: e2e/networkpolicy_test.go
//   - Behavioral contracts: Capsule BC-005 (NetworkPolicy per tenant namespace) +
//     BC-006 (ingress from same tenant only) + BC-004 (drift correction)
//   - Translated to: LoopRuntime EnvironmentController (runtime NetworkPolicies) and
//     TenantController (build namespace egress restriction).
//
// Key behavioral differences from Capsule:
//   - Capsule propagates a single GlobalTenantResource to all tenant namespaces.
//   - LoopRuntime applies TWO separate network policy contexts:
//     1. Build namespace: egress-restrict (blocks internet exfiltration from build pods).
//     2. Runtime namespace: deny-all + allow-ingress-traefik + allow-egress (default-deny posture).
//   - LoopRuntime's build policy protects against malicious Dockerfiles (spec §17.1).
//   - No 0.0.0.0/0 CIDR is ever permitted in build or runtime egress rules.
package tenant_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/operatortest"
)

var _ = Describe("Golden: Network Policies", Ordered, func() {

	// --- GOLDEN TEST: golden_network_policies_build_egress_restrict ---
	// Source: src_network_policies_tenant_networkpolicy_created_in_namespace
	// BC: Capsule BC-005 (NetworkPolicy in namespace) → LoopRuntime build-egress-restrict in build namespace

	Context("golden_network_policies_build_egress_restrict", func() {
		const tenantName = "golden-np-build"

		BeforeAll(func() {
			tenant := newGoldenTenant(tenantName, loopruntimev1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			Eventually(func(g Gomega) {
				var t loopruntimev1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Status.Phase).ToNot(BeEmpty())
			}, timeout, interval).Should(Succeed())
		})

		It("should create a NetworkPolicy named build-egress-restrict in the build namespace", func() {
			expectNetworkPolicyExists("build-egress-restrict", "build-"+tenantName)
		})

		It("should set PolicyType=Egress (only) on build-egress-restrict", func() {
			withNetworkPolicy("build-egress-restrict", "build-"+tenantName, func(g Gomega, np *networkingv1.NetworkPolicy) {
				g.Expect(np.Spec.PolicyTypes).To(ContainElement(networkingv1.PolicyTypeEgress),
					"build policy must be Egress-type to restrict outbound from build pods")
				g.Expect(np.Spec.PolicyTypes).NotTo(ContainElement(networkingv1.PolicyTypeIngress),
					"build policy must NOT restrict ingress — only egress (source exfiltration risk)")
			})
		})

		It("should not permit the 0.0.0.0/0 CIDR in any egress rule (no unrestricted internet)", func() {
			// SECURITY: A malicious Dockerfile could exfiltrate source code via HTTP.
			// build-egress-restrict must allow only specific endpoints (DNS, MinIO, registry, k8s API).
			// Any rule with 0.0.0.0/0 would defeat this protection entirely.
			withNetworkPolicy("build-egress-restrict", "build-"+tenantName, func(g Gomega, np *networkingv1.NetworkPolicy) {
				for _, egressRule := range np.Spec.Egress {
					for _, toPeer := range egressRule.To {
						if toPeer.IPBlock != nil {
							g.Expect(toPeer.IPBlock.CIDR).NotTo(Equal("0.0.0.0/0"),
								"build egress must NOT allow 0.0.0.0/0 — would permit internet exfiltration")
						}
					}
				}
			})
		})

		It("should apply the policy to all pods in the build namespace (empty podSelector)", func() {
			withNetworkPolicy("build-egress-restrict", "build-"+tenantName, func(g Gomega, np *networkingv1.NetworkPolicy) {
				// An empty LabelSelector {} selects all pods in the namespace.
				g.Expect(np.Spec.PodSelector.MatchLabels).To(BeEmpty(),
					"build-egress-restrict must apply to all pods (empty podSelector = select all)")
				g.Expect(np.Spec.PodSelector.MatchExpressions).To(BeEmpty())
			})
		})

		AfterAll(func() {
			cleanupGoldenTenant(tenantName, []string{"production"})
		})
	})

	// --- GOLDEN TEST: golden_network_policies_runtime_deny_all ---
	// Source: src_network_policies_tenant_networkpolicy_created_in_namespace
	// BC: Capsule BC-005 → LoopRuntime deny-all NetworkPolicy in runtime namespace (default-deny posture)

	Context("golden_network_policies_runtime_deny_all", func() {
		const tenantName = "golden-np-rt"

		BeforeAll(func() {
			tenant := newGoldenTenant(tenantName, loopruntimev1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			Eventually(func(g Gomega) {
				var t loopruntimev1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Status.Phase).ToNot(BeEmpty())
			}, timeout, interval).Should(Succeed())
		})

		It("should create a NetworkPolicy named deny-all in the runtime namespace", func() {
			expectNetworkPolicyExists("deny-all", fmt.Sprintf("loop-runtime-%s-default-project-production", tenantName))
		})

		It("should include both Ingress and Egress in deny-all PolicyTypes", func() {
			withNetworkPolicy("deny-all", fmt.Sprintf("loop-runtime-%s-default-project-production", tenantName), func(g Gomega, np *networkingv1.NetworkPolicy) {
				g.Expect(np.Spec.PolicyTypes).To(ContainElements(
					networkingv1.PolicyTypeIngress,
					networkingv1.PolicyTypeEgress,
				), "deny-all must block both ingress and egress by default")
			})
		})

		It("should have no ingress or egress rules in deny-all (empty = block all)", func() {
			withNetworkPolicy("deny-all", fmt.Sprintf("loop-runtime-%s-default-project-production", tenantName), func(g Gomega, np *networkingv1.NetworkPolicy) {
				g.Expect(np.Spec.Ingress).To(BeEmpty(),
					"deny-all must have no ingress rules — empty list means block all ingress")
				g.Expect(np.Spec.Egress).To(BeEmpty(),
					"deny-all must have no egress rules — empty list means block all egress")
			})
		})

		AfterAll(func() {
			cleanupGoldenTenant(tenantName, []string{"production"})
		})
	})

	// --- GOLDEN TEST: golden_network_policies_runtime_three_policies ---
	// Source: src_network_policies_networkpolicy_ingress_from_same_tenant
	// BC: Capsule BC-006 (ingress from same tenant) → LoopRuntime three-policy default-deny posture

	Context("golden_network_policies_runtime_three_policies", func() {
		const tenantName = "golden-np-three"

		BeforeAll(func() {
			tenant := newGoldenTenant(tenantName, loopruntimev1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			Eventually(func(g Gomega) {
				var t loopruntimev1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Status.Phase).ToNot(BeEmpty())
			}, timeout, interval).Should(Succeed())
		})

		It("should create the deny-all NetworkPolicy in the runtime namespace", func() {
			expectNetworkPolicyExists("deny-all", fmt.Sprintf("loop-runtime-%s-default-project-production", tenantName))
		})

		It("should create the allow-ingress-traefik NetworkPolicy in the runtime namespace", func() {
			expectNetworkPolicyExists("allow-ingress-traefik", fmt.Sprintf("loop-runtime-%s-default-project-production", tenantName))
		})

		It("should create the allow-egress NetworkPolicy in the runtime namespace", func() {
			expectNetworkPolicyExists("allow-egress", fmt.Sprintf("loop-runtime-%s-default-project-production", tenantName))
		})

		It("should create at least 3 NetworkPolicies in total in the runtime namespace", func() {
			ns := fmt.Sprintf("loop-runtime-%s-default-project-production", tenantName)
			Eventually(func(g Gomega) {
				var policies networkingv1.NetworkPolicyList
				g.Expect(k8sClient.List(ctx, &policies, client.InNamespace(ns))).To(Succeed())
				g.Expect(len(policies.Items)).To(BeNumerically(">=", 3),
					"runtime namespace must have deny-all + allow-ingress-traefik + allow-egress at minimum")
			}, timeout, interval).Should(Succeed())
		})

		It("should not contain any policy with unrestricted internet egress (0.0.0.0/0)", func() {
			// Same security invariant as build namespace — no runtime policy should open the internet.
			ns := fmt.Sprintf("loop-runtime-%s-default-project-production", tenantName)
			Eventually(func(g Gomega) {
				var policies networkingv1.NetworkPolicyList
				g.Expect(k8sClient.List(ctx, &policies, client.InNamespace(ns))).To(Succeed())
				for _, policy := range policies.Items {
					for _, egressRule := range policy.Spec.Egress {
						for _, toPeer := range egressRule.To {
							if toPeer.IPBlock != nil {
								g.Expect(toPeer.IPBlock.CIDR).NotTo(Equal("0.0.0.0/0"),
									"policy %s must not allow unrestricted internet egress", policy.Name)
							}
						}
					}
				}
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			cleanupGoldenTenant(tenantName, []string{"production"})
		})
	})

	// --- GOLDEN TEST: golden_network_policies_restored_after_modification ---
	// Source: src_network_policies_tenant_networkpolicy_restored_after_modification
	// BC: Capsule BC-004 (drift correction) → LoopRuntime CreateOrUpdate restores mutated NetworkPolicies

	Context("golden_network_policies_restored_after_modification", func() {
		const tenantName = "golden-np-restore"

		BeforeAll(func() {
			tenant := newGoldenTenant(tenantName, loopruntimev1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			// Wait for deny-all to exist before modifying it.
			ns := fmt.Sprintf("loop-runtime-%s-default-project-production", tenantName)
			Eventually(func(g Gomega) {
				var np networkingv1.NetworkPolicy
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "deny-all", Namespace: ns,
				}, &np)).To(Succeed())
			}, timeout, interval).Should(Succeed())
		})

		It("should restore deny-all PolicyTypes after manual modification", func() {
			ns := fmt.Sprintf("loop-runtime-%s-default-project-production", tenantName)

			// Simulate infrastructure drift: modify the deny-all policy to remove PolicyTypes.
			var np networkingv1.NetworkPolicy
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "deny-all", Namespace: ns,
			}, &np)).To(Succeed())
			Expect(operatortest.UpdateWithRetry(ctx, k8sClient, &np, func() {
				np.Spec.PolicyTypes = []networkingv1.PolicyType{} // Remove all — now allows everything
			})).To(Succeed())

			// Trigger re-reconcile via spec update on the Tenant CR.
			var t loopruntimev1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			Expect(operatortest.UpdateWithRetry(ctx, k8sClient, &t, func() {
				t.Spec.DisplayName = "NetworkPolicy Restore Trigger"
			})).To(Succeed())

			// Controller must restore the deny-all policy to its desired state.
			Eventually(func(g Gomega) {
				var restored networkingv1.NetworkPolicy
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "deny-all", Namespace: ns,
				}, &restored)).To(Succeed())
				g.Expect(restored.Spec.PolicyTypes).To(ContainElement(networkingv1.PolicyTypeIngress),
					"deny-all PolicyType=Ingress must be restored after drift correction")
				g.Expect(restored.Spec.PolicyTypes).To(ContainElement(networkingv1.PolicyTypeEgress),
					"deny-all PolicyType=Egress must be restored after drift correction")
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			cleanupGoldenTenant(tenantName, []string{"production"})
		})
	})
})
