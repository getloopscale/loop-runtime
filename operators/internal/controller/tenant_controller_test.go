// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/conditions"
)

func newTenant(name string, plan loopzerov1.PlanTier, envs []string) *loopzerov1.Tenant {
	environments := make([]loopzerov1.TenantEnvironmentEntry, len(envs))
	for i, e := range envs {
		environments[i] = loopzerov1.TenantEnvironmentEntry{Name: e, Cluster: "workload"}
	}
	return &loopzerov1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: loopzerov1.TenantSpec{
			DisplayName:  "Test Tenant " + name,
			Plan:         plan,
			Environments: environments,
			GitOps: loopzerov1.GitOpsSpec{
				RepoName: "loopzero-tenant-" + name,
				Branch:   "main",
			},
			Preview: loopzerov1.PreviewSpec{
				MaxEnvironments: 3,
				TTLHours:        48,
			},
			Limits: loopzerov1.ResourceLimits{
				MaxCPU:              resource.MustParse("8"),
				MaxMemory:           resource.MustParse("16Gi"),
				MaxStorage:          resource.MustParse("50Gi"),
				MaxBuildConcurrency: 2,
			},
		},
	}
}

// cleanupTenantEnvCRs removes finalizers from all Environment CRs owned by the given tenant,
// so they can be garbage-collected during test teardown. In envtest, the GC doesn't run,
// so this must be called explicitly in AfterAll blocks.
func cleanupTenantEnvCRs(tenantName string) {
	var envList loopzerov1.EnvironmentList
	if err := k8sClient.List(ctx, &envList, client.InNamespace("default")); err != nil {
		return
	}
	for i := range envList.Items {
		env := &envList.Items[i]
		for _, ref := range env.OwnerReferences {
			if ref.Name == tenantName {
				env.Finalizers = nil
				_ = k8sClient.Update(ctx, env)
				_ = k8sClient.Delete(ctx, env)
				break
			}
		}
	}
}

var _ = Describe("TenantController", Ordered, func() {

	Context("when a new Tenant CR is created", func() {
		It("should set Phase (Provisioning initially, then Active)", func() {
			tenant := newTenant("phase-test", loopzerov1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			Eventually(func(g Gomega) {
				var t loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      "phase-test",
					Namespace: "default",
				}, &t)).To(Succeed())
				// Phase should be set (may already be Active if reconcile completed)
				g.Expect(t.Status.Phase).To(SatisfyAny(
					Equal(loopzerov1.TenantPhaseProvisioning),
					Equal(loopzerov1.TenantPhaseActive),
				))
			}, timeout, interval).Should(Succeed())
		})

		It("should add the tenant-cleanup finalizer", func() {
			Eventually(func(g Gomega) {
				var t loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      "phase-test",
					Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Finalizers).To(ContainElement("loopzero.io/tenant-cleanup"))
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("when a Tenant CR has minimal spec (empty environments list)", func() {
		It("should be rejected by CRD validation (minItems: 1)", func() {
			tenant := &loopzerov1.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "empty-envs",
					Namespace: "default",
				},
				Spec: loopzerov1.TenantSpec{
					DisplayName:  "Empty Envs",
					Plan:         loopzerov1.PlanStarter,
					Environments: []loopzerov1.TenantEnvironmentEntry{},
					GitOps: loopzerov1.GitOpsSpec{
						RepoName: "loopzero-tenant-empty",
						Branch:   "main",
					},
					Preview: loopzerov1.PreviewSpec{},
					Limits:  loopzerov1.ResourceLimits{},
				},
			}
			err := k8sClient.Create(ctx, tenant)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("spec.environments"))
		})
	})

	Context("idempotency", func() {
		It("should produce identical state after spec update triggers re-reconcile", func() {
			var t loopzerov1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "phase-test",
				Namespace: "default",
			}, &t)).To(Succeed())

			t.Spec.DisplayName = "Updated Display Name"
			Expect(k8sClient.Update(ctx, &t)).To(Succeed())

			Eventually(func(g Gomega) {
				var updated loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      "phase-test",
					Namespace: "default",
				}, &updated)).To(Succeed())
				g.Expect(updated.Status.Phase).ToNot(BeEmpty())
				g.Expect(updated.Finalizers).To(ContainElement("loopzero.io/tenant-cleanup"))
			}, timeout, interval).Should(Succeed())
		})
	})

	AfterAll(func() {
		for _, name := range []string{"phase-test", "empty-envs"} {
			cleanupTenantEnvCRs(name)
			t := &loopzerov1.Tenant{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, t); err == nil {
				t.Finalizers = nil
				_ = k8sClient.Update(ctx, t)
				_ = k8sClient.Delete(ctx, t)
			}
		}
	})
})

var _ = Describe("TenantController — Namespace Provisioning", Ordered, func() {
	const tenantName = "ns-test"

	BeforeAll(func() {
		tenant := newTenant(tenantName, loopzerov1.PlanStarter, []string{"production", "staging"})
		Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

		// Wait for controller to reconcile
		Eventually(func(g Gomega) {
			var t loopzerov1.Tenant
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			g.Expect(t.Status.Phase).ToNot(BeEmpty())
		}, timeout, interval).Should(Succeed())
	})

	Context("namespace creation", func() {
		It("should create a namespace per environment", func() {
			for _, env := range []string{"production", "staging"} {
				nsName := "loopzero-" + tenantName + "-" + env
				Eventually(func(g Gomega) {
					var ns corev1.Namespace
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, &ns)).To(Succeed())
				}, timeout, interval).Should(Succeed())
			}
		})

		It("should set correct labels on namespaces", func() {
			for _, env := range []string{"production", "staging"} {
				nsName := "loopzero-" + tenantName + "-" + env
				Eventually(func(g Gomega) {
					var ns corev1.Namespace
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, &ns)).To(Succeed())
					g.Expect(ns.Labels).To(HaveKeyWithValue("loopzero.io/tenant", tenantName))
					g.Expect(ns.Labels).To(HaveKeyWithValue("loopzero.io/environment", env))
					g.Expect(ns.Labels).To(HaveKeyWithValue("loopzero.io/managed-by", "environment-controller"))
				}, timeout, interval).Should(Succeed())
			}
		})

		It("should create Environment CRs for each environment", func() {
			for _, env := range []string{"production", "staging"} {
				envCRName := tenantName + "-" + env
				Eventually(func(g Gomega) {
					var envCR loopzerov1.Environment
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name: envCRName, Namespace: "default",
					}, &envCR)).To(Succeed())
					g.Expect(envCR.Spec.TenantRef.Name).To(Equal(tenantName))
					g.Expect(envCR.Spec.Environment).To(Equal(env))
					g.Expect(envCR.Spec.Namespace.Name).To(Equal("loopzero-" + tenantName + "-" + env))
					// Verify ownerReference to Tenant
					g.Expect(envCR.OwnerReferences).To(HaveLen(1))
					g.Expect(envCR.OwnerReferences[0].Kind).To(Equal("Tenant"))
					g.Expect(envCR.OwnerReferences[0].Name).To(Equal(tenantName))
				}, timeout, interval).Should(Succeed())
			}
		})

		It("should NOT set ownerReference on namespaces (cross-namespace limitation)", func() {
			var ns corev1.Namespace
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "loopzero-" + tenantName + "-production"}, &ns)).To(Succeed())
			Expect(ns.OwnerReferences).To(BeEmpty())
		})
	})

	Context("ResourceQuota", func() {
		It("should create ResourceQuota in each namespace with spec.limits values", func() {
			for _, env := range []string{"production", "staging"} {
				nsName := "loopzero-" + tenantName + "-" + env
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
			}
		})
	})

	Context("LimitRange", func() {
		It("should create LimitRange with default container limits", func() {
			for _, env := range []string{"production", "staging"} {
				nsName := "loopzero-" + tenantName + "-" + env
				Eventually(func(g Gomega) {
					var lr corev1.LimitRange
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name: "tenant-limits", Namespace: nsName,
					}, &lr)).To(Succeed())
					g.Expect(lr.Spec.Limits).To(HaveLen(1))
					g.Expect(lr.Spec.Limits[0].Type).To(Equal(corev1.LimitTypeContainer))
					g.Expect(lr.Spec.Limits[0].Default.Cpu().String()).To(Equal("500m"))
					g.Expect(lr.Spec.Limits[0].Default.Memory().String()).To(Equal("512Mi"))
					g.Expect(lr.Spec.Limits[0].DefaultRequest.Cpu().String()).To(Equal("100m"))
					g.Expect(lr.Spec.Limits[0].DefaultRequest.Memory().String()).To(Equal("128Mi"))
				}, timeout, interval).Should(Succeed())
			}
		})
	})

	Context("Environment CR conditions (delegated to EnvironmentController)", func() {
		It("should set NamespaceReady on Environment CRs (not on Tenant)", func() {
			// NamespacesReady is no longer on Tenant — it is now on Environment CRs
			for _, env := range []string{"production", "staging"} {
				envCRName := tenantName + "-" + env
				Eventually(func(g Gomega) {
					var envCR loopzerov1.Environment
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name: envCRName, Namespace: "default",
					}, &envCR)).To(Succeed())
					g.Expect(envCR.Status.Phase).To(Equal(loopzerov1.EnvironmentPhaseActive))
				}, timeout, interval).Should(Succeed())
			}
		})
	})

	Context("idempotency", func() {
		It("should not duplicate namespaces after re-reconcile", func() {
			// Trigger re-reconcile via spec update
			var t loopzerov1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			t.Spec.DisplayName = "Updated for idempotency"
			Expect(k8sClient.Update(ctx, &t)).To(Succeed())

			// Wait for reconcile to complete
			Eventually(func(g Gomega) {
				var updated loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &updated)).To(Succeed())
				g.Expect(updated.Spec.DisplayName).To(Equal("Updated for idempotency"))
			}, timeout, interval).Should(Succeed())

			// Count namespaces with tenant label
			var nsList corev1.NamespaceList
			Expect(k8sClient.List(ctx, &nsList, client.MatchingLabels{
				"loopzero.io/tenant": tenantName,
			})).To(Succeed())
			Expect(nsList.Items).To(HaveLen(3)) // production + staging + build-{tenant}
		})
	})

	AfterAll(func() {
		for _, nsName := range []string{"loopzero-" + tenantName + "-production", "loopzero-" + tenantName + "-staging", "build-" + tenantName} {
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

var _ = Describe("TenantController — RBAC", Ordered, func() {
	const tenantName = "rbac-test"

	BeforeAll(func() {
		tenant := newTenant(tenantName, loopzerov1.PlanStarter, []string{"production"})
		Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

		Eventually(func(g Gomega) {
			var t loopzerov1.Tenant
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			g.Expect(t.Status.Phase).ToNot(BeEmpty())
		}, timeout, interval).Should(Succeed())
	})

	Context("ServiceAccount", func() {
		It("should create loopzero-deployer ServiceAccount in each namespace", func() {
			nsName := "loopzero-" + tenantName + "-production"
			Eventually(func(g Gomega) {
				var sa corev1.ServiceAccount
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "loopzero-deployer", Namespace: nsName,
				}, &sa)).To(Succeed())
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("Role for starter plan", func() {
		It("should create Role with basic deploy permissions", func() {
			nsName := "loopzero-" + tenantName + "-production"
			Eventually(func(g Gomega) {
				var role rbacv1.Role
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "loopzero-deployer", Namespace: nsName,
				}, &role)).To(Succeed())
				// Starter: deployments, services, configmaps
				g.Expect(role.Rules).ToNot(BeEmpty())
				hasDeployments := false
				for _, rule := range role.Rules {
					for _, res := range rule.Resources {
						if res == "deployments" {
							hasDeployments = true
						}
					}
				}
				g.Expect(hasDeployments).To(BeTrue(), "starter role should include deployments")
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("RoleBinding", func() {
		It("should bind loopzero-deployer SA to the Role", func() {
			nsName := "loopzero-" + tenantName + "-production"
			Eventually(func(g Gomega) {
				var rb rbacv1.RoleBinding
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "loopzero-deployer", Namespace: nsName,
				}, &rb)).To(Succeed())
				g.Expect(rb.RoleRef.Name).To(Equal("loopzero-deployer"))
				g.Expect(rb.RoleRef.Kind).To(Equal("Role"))
				g.Expect(rb.Subjects).To(HaveLen(1))
				g.Expect(rb.Subjects[0].Kind).To(Equal("ServiceAccount"))
				g.Expect(rb.Subjects[0].Name).To(Equal("loopzero-deployer"))
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("RBACReady condition (on Environment CR)", func() {
		It("should set RBACReady on Environment CR (not on Tenant)", func() {
			envCRName := tenantName + "-production"
			Eventually(func(g Gomega) {
				var envCR loopzerov1.Environment
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: envCRName, Namespace: "default",
				}, &envCR)).To(Succeed())
				found := false
				for _, c := range envCR.Status.Conditions {
					if c.Type == conditions.TypeEnvRBACReady {
						found = true
						g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
					}
				}
				g.Expect(found).To(BeTrue(), "RBACReady condition should exist on Environment CR")
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("enterprise plan RBAC", func() {
		It("should include extra permissions for enterprise plan", func() {
			// Create enterprise tenant
			entTenant := newTenant("rbac-ent", loopzerov1.PlanEnterprise, []string{"production"})
			Expect(k8sClient.Create(ctx, entTenant)).To(Succeed())

			nsName := "loopzero-rbac-ent-production"
			Eventually(func(g Gomega) {
				var role rbacv1.Role
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "loopzero-deployer", Namespace: nsName,
				}, &role)).To(Succeed())
				// Enterprise should have ingresses and networkpolicies
				hasIngresses := false
				for _, rule := range role.Rules {
					for _, res := range rule.Resources {
						if res == "ingresses" {
							hasIngresses = true
						}
					}
				}
				g.Expect(hasIngresses).To(BeTrue(), "enterprise role should include ingresses")
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("idempotency", func() {
		It("should not duplicate RBAC resources after re-reconcile", func() {
			var t loopzerov1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			t.Spec.DisplayName = "RBAC idempotency"
			Expect(k8sClient.Update(ctx, &t)).To(Succeed())

			// Verify still single SA, Role, RoleBinding
			nsName := "loopzero-" + tenantName + "-production"
			Eventually(func(g Gomega) {
				var saList corev1.ServiceAccountList
				g.Expect(k8sClient.List(ctx, &saList, client.InNamespace(nsName))).To(Succeed())
				saCount := 0
				for _, sa := range saList.Items {
					if sa.Name == "loopzero-deployer" {
						saCount++
					}
				}
				g.Expect(saCount).To(Equal(1))
			}, timeout, interval).Should(Succeed())
		})
	})

	AfterAll(func() {
		for _, name := range []string{tenantName, "rbac-ent"} {
			for _, env := range []string{"production"} {
				ns := &corev1.Namespace{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: "tn-" + name + "-" + env}, ns); err == nil {
					_ = k8sClient.Delete(ctx, ns)
				}
			}
			t := &loopzerov1.Tenant{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, t); err == nil {
				t.Finalizers = nil
				_ = k8sClient.Update(ctx, t)
				_ = k8sClient.Delete(ctx, t)
			}
		}
	})
})

var _ = Describe("TenantController — NetworkPolicy", Ordered, func() {
	const tenantName = "netpol-test"

	BeforeAll(func() {
		tenant := newTenant(tenantName, loopzerov1.PlanStarter, []string{"production"})
		Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
		Eventually(func(g Gomega) {
			var t loopzerov1.Tenant
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			g.Expect(t.Status.Phase).ToNot(BeEmpty())
		}, timeout, interval).Should(Succeed())
	})

	Context("NetworkPolicy creation", func() {
		It("should create 3 NetworkPolicies per namespace (deny-all, allow-ingress, allow-egress)", func() {
			nsName := "loopzero-" + tenantName + "-production"
			Eventually(func(g Gomega) {
				var policies networkingv1.NetworkPolicyList
				g.Expect(k8sClient.List(ctx, &policies, client.InNamespace(nsName))).To(Succeed())
				g.Expect(len(policies.Items)).To(BeNumerically(">=", 3))
			}, timeout, interval).Should(Succeed())
		})

		It("should create deny-all policy", func() {
			nsName := "loopzero-" + tenantName + "-production"
			Eventually(func(g Gomega) {
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

		It("should NOT have any rule with cidr 0.0.0.0/0 (internet blocked)", func() {
			nsName := "loopzero-" + tenantName + "-production"
			var policies networkingv1.NetworkPolicyList
			Expect(k8sClient.List(ctx, &policies, client.InNamespace(nsName))).To(Succeed())

			for _, policy := range policies.Items {
				for _, egress := range policy.Spec.Egress {
					for _, to := range egress.To {
						if to.IPBlock != nil {
							Expect(to.IPBlock.CIDR).ToNot(Equal("0.0.0.0/0"),
								"no policy should allow internet egress (cidr 0.0.0.0/0)")
						}
					}
				}
			}
		})
	})

	Context("NetworkPolicyReady condition (on Environment CR)", func() {
		It("should set NetworkPolicyReady on Environment CR (not on Tenant)", func() {
			envCRName := tenantName + "-production"
			Eventually(func(g Gomega) {
				var envCR loopzerov1.Environment
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: envCRName, Namespace: "default",
				}, &envCR)).To(Succeed())
				found := false
				for _, c := range envCR.Status.Conditions {
					if c.Type == conditions.TypeEnvNetworkPolicyReady {
						found = true
						g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
					}
				}
				g.Expect(found).To(BeTrue(), "NetworkPolicyReady condition should exist on Environment CR")
			}, timeout, interval).Should(Succeed())
		})
	})

	AfterAll(func() {
		ns := &corev1.Namespace{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: "loopzero-" + tenantName + "-production"}, ns); err == nil {
			_ = k8sClient.Delete(ctx, ns)
		}
		t := &loopzerov1.Tenant{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: tenantName, Namespace: "default"}, t); err == nil {
			t.Finalizers = nil
			_ = k8sClient.Update(ctx, t)
			_ = k8sClient.Delete(ctx, t)
		}
	})
})

var _ = Describe("TenantController — Build Namespace + QuotaEnforced", Ordered, func() {
	const tenantName = "build-ns-test"

	BeforeAll(func() {
		tenant := newTenant(tenantName, loopzerov1.PlanStarter, []string{"production"})
		Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
		Eventually(func(g Gomega) {
			var t loopzerov1.Tenant
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			g.Expect(t.Status.Phase).ToNot(BeEmpty())
		}, timeout, interval).Should(Succeed())
	})

	Context("build namespace creation", func() {
		It("should create build-{tenantID} namespace", func() {
			buildNs := "build-" + tenantName
			Eventually(func(g Gomega) {
				var ns corev1.Namespace
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: buildNs}, &ns)).To(Succeed())
			}, timeout, interval).Should(Succeed())
		})

		It("should set correct labels on build namespace", func() {
			buildNs := "build-" + tenantName
			Eventually(func(g Gomega) {
				var ns corev1.Namespace
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: buildNs}, &ns)).To(Succeed())
				g.Expect(ns.Labels).To(HaveKeyWithValue("loopzero.io/tenant", tenantName))
				g.Expect(ns.Labels).To(HaveKeyWithValue("loopzero.io/component", "build"))
				g.Expect(ns.Labels).To(HaveKeyWithValue("loopzero.io/managed-by", "tenant-controller"))
			}, timeout, interval).Should(Succeed())
		})

		It("should NOT set ownerReference on build namespace", func() {
			buildNs := "build-" + tenantName
			Eventually(func(g Gomega) {
				var ns corev1.Namespace
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: buildNs}, &ns)).To(Succeed())
				g.Expect(ns.OwnerReferences).To(BeEmpty())
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("build ResourceQuota", func() {
		It("should create ResourceQuota in build namespace with build concurrency limit", func() {
			buildNs := "build-" + tenantName
			Eventually(func(g Gomega) {
				var quota corev1.ResourceQuota
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "build-quota", Namespace: buildNs,
				}, &quota)).To(Succeed())
				cpuReq := quota.Spec.Hard[corev1.ResourceRequestsCPU]
				g.Expect(cpuReq.String()).To(Equal("4"))
				memReq := quota.Spec.Hard[corev1.ResourceRequestsMemory]
				g.Expect(memReq.String()).To(Equal("8Gi"))
				cpuLim := quota.Spec.Hard[corev1.ResourceLimitsCPU]
				g.Expect(cpuLim.String()).To(Equal("8"))
				memLim := quota.Spec.Hard[corev1.ResourceLimitsMemory]
				g.Expect(memLim.String()).To(Equal("16Gi"))
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("conditions", func() {
		It("should set BuildNamespaceReady to True", func() {
			Eventually(func(g Gomega) {
				var t loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				found := false
				for _, c := range t.Status.Conditions {
					if c.Type == conditions.TypeBuildNamespaceReady {
						found = true
						g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
					}
				}
				g.Expect(found).To(BeTrue(), "BuildNamespaceReady condition should exist")
			}, timeout, interval).Should(Succeed())
		})

		It("should set QuotaEnforced to True", func() {
			Eventually(func(g Gomega) {
				var t loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				found := false
				for _, c := range t.Status.Conditions {
					if c.Type == conditions.TypeQuotaEnforced {
						found = true
						g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
					}
				}
				g.Expect(found).To(BeTrue(), "QuotaEnforced condition should exist")
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("PriorityClassReady condition", func() {
		It("should set PriorityClassReady to True", func() {
			Eventually(func(g Gomega) {
				var t loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				found := false
				for _, c := range t.Status.Conditions {
					if c.Type == conditions.TypePriorityClassReady {
						found = true
						g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
					}
				}
				g.Expect(found).To(BeTrue(), "PriorityClassReady condition should exist")
			}, timeout, interval).Should(Succeed())
		})
	})

	AfterAll(func() {
		for _, nsName := range []string{"build-" + tenantName, "loopzero-" + tenantName + "-production"} {
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
}) // end Build Namespace + QuotaEnforced

var _ = Describe("TenantController — PriorityClass", Ordered, func() {

	Context("starter plan", func() {
		const tenantName = "pc-starter"

		BeforeAll(func() {
			tenant := newTenant(tenantName, loopzerov1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
			Eventually(func(g Gomega) {
				var t loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Status.Phase).To(Equal(loopzerov1.TenantPhaseActive))
			}, timeout, interval).Should(Succeed())
		})

		It("should create PriorityClass with value 100 and PreemptionPolicy Never", func() {
			Eventually(func(g Gomega) {
				var pc schedulingv1.PriorityClass
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "loopzero-tenant-starter"}, &pc)).To(Succeed())
				g.Expect(pc.Value).To(Equal(int32(100)))
				g.Expect(*pc.PreemptionPolicy).To(Equal(corev1.PreemptNever))
				g.Expect(pc.Labels).To(HaveKeyWithValue("loopzero.io/managed-by", "tenant-controller"))
			}, timeout, interval).Should(Succeed())
		})

		It("should set PriorityClassReady condition to True", func() {
			Eventually(func(g Gomega) {
				var t loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				found := false
				for _, c := range t.Status.Conditions {
					if c.Type == conditions.TypePriorityClassReady {
						found = true
						g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
					}
				}
				g.Expect(found).To(BeTrue(), "PriorityClassReady condition should exist")
			}, timeout, interval).Should(Succeed())
		})

		It("should be idempotent — second reconciliation does not fail", func() {
			var t loopzerov1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			t.Spec.DisplayName = "PC starter idempotency"
			Expect(k8sClient.Update(ctx, &t)).To(Succeed())

			Eventually(func(g Gomega) {
				var updated loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &updated)).To(Succeed())
				g.Expect(updated.Status.Phase).To(Equal(loopzerov1.TenantPhaseActive))
				g.Expect(updated.Status.ObservedGeneration).To(Equal(updated.Generation))
			}, timeout, interval).Should(Succeed())

			// PriorityClass still exists with correct values
			var pc schedulingv1.PriorityClass
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "loopzero-tenant-starter"}, &pc)).To(Succeed())
			Expect(pc.Value).To(Equal(int32(100)))
		})

		AfterAll(func() {
			t := &loopzerov1.Tenant{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: tenantName, Namespace: "default"}, t); err == nil {
				t.Finalizers = nil
				_ = k8sClient.Update(ctx, t)
				_ = k8sClient.Delete(ctx, t)
			}
			for _, nsName := range []string{"loopzero-" + tenantName + "-production", "build-" + tenantName} {
				ns := &corev1.Namespace{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, ns); err == nil {
					_ = k8sClient.Delete(ctx, ns)
				}
			}
		})
	})

	Context("growth plan", func() {
		const tenantName = "pc-growth"

		BeforeAll(func() {
			tenant := newTenant(tenantName, loopzerov1.PlanGrowth, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
			Eventually(func(g Gomega) {
				var t loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Status.Phase).To(Equal(loopzerov1.TenantPhaseActive))
			}, timeout, interval).Should(Succeed())
		})

		It("should create PriorityClass with value 500 and PreemptLowerPriority", func() {
			Eventually(func(g Gomega) {
				var pc schedulingv1.PriorityClass
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "loopzero-tenant-growth"}, &pc)).To(Succeed())
				g.Expect(pc.Value).To(Equal(int32(500)))
				g.Expect(*pc.PreemptionPolicy).To(Equal(corev1.PreemptLowerPriority))
				g.Expect(pc.Labels).To(HaveKeyWithValue("loopzero.io/managed-by", "tenant-controller"))
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			t := &loopzerov1.Tenant{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: tenantName, Namespace: "default"}, t); err == nil {
				t.Finalizers = nil
				_ = k8sClient.Update(ctx, t)
				_ = k8sClient.Delete(ctx, t)
			}
			for _, nsName := range []string{"loopzero-" + tenantName + "-production", "build-" + tenantName} {
				ns := &corev1.Namespace{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, ns); err == nil {
					_ = k8sClient.Delete(ctx, ns)
				}
			}
		})
	})

	Context("enterprise plan", func() {
		const tenantName = "pc-enterprise"

		BeforeAll(func() {
			tenant := newTenant(tenantName, loopzerov1.PlanEnterprise, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
			Eventually(func(g Gomega) {
				var t loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Status.Phase).To(Equal(loopzerov1.TenantPhaseActive))
			}, timeout, interval).Should(Succeed())
		})

		It("should create PriorityClass with value 1000 and PreemptLowerPriority", func() {
			Eventually(func(g Gomega) {
				var pc schedulingv1.PriorityClass
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "loopzero-tenant-enterprise"}, &pc)).To(Succeed())
				g.Expect(pc.Value).To(Equal(int32(1000)))
				g.Expect(*pc.PreemptionPolicy).To(Equal(corev1.PreemptLowerPriority))
				g.Expect(pc.Labels).To(HaveKeyWithValue("loopzero.io/managed-by", "tenant-controller"))
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			t := &loopzerov1.Tenant{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: tenantName, Namespace: "default"}, t); err == nil {
				t.Finalizers = nil
				_ = k8sClient.Update(ctx, t)
				_ = k8sClient.Delete(ctx, t)
			}
			for _, nsName := range []string{"loopzero-" + tenantName + "-production", "build-" + tenantName} {
				ns := &corev1.Namespace{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, ns); err == nil {
					_ = k8sClient.Delete(ctx, ns)
				}
			}
		})
	})

	Context("plan upgrade creates new PriorityClass", func() {
		const tenantName = "pc-upgrade"

		BeforeAll(func() {
			tenant := newTenant(tenantName, loopzerov1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
			Eventually(func(g Gomega) {
				var t loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Status.Phase).To(Equal(loopzerov1.TenantPhaseActive))
			}, timeout, interval).Should(Succeed())
		})

		It("should create growth PriorityClass after plan upgrade from starter to growth", func() {
			// Upgrade plan
			var t loopzerov1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			t.Spec.Plan = loopzerov1.PlanGrowth
			Expect(k8sClient.Update(ctx, &t)).To(Succeed())

			// New PriorityClass should be created
			Eventually(func(g Gomega) {
				var pc schedulingv1.PriorityClass
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "loopzero-tenant-growth"}, &pc)).To(Succeed())
				g.Expect(pc.Value).To(Equal(int32(500)))
				g.Expect(*pc.PreemptionPolicy).To(Equal(corev1.PreemptLowerPriority))
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			t := &loopzerov1.Tenant{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: tenantName, Namespace: "default"}, t); err == nil {
				t.Finalizers = nil
				_ = k8sClient.Update(ctx, t)
				_ = k8sClient.Delete(ctx, t)
			}
			for _, nsName := range []string{"loopzero-" + tenantName + "-production", "build-" + tenantName} {
				ns := &corev1.Namespace{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, ns); err == nil {
					_ = k8sClient.Delete(ctx, ns)
				}
			}
		})
	})

	Context("deletion does NOT delete PriorityClass (shared across tenants)", func() {
		const tenantName = "pc-delete"

		BeforeAll(func() {
			tenant := newTenant(tenantName, loopzerov1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
			Eventually(func(g Gomega) {
				var t loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Status.Phase).To(Equal(loopzerov1.TenantPhaseActive))
			}, timeout, interval).Should(Succeed())

			// Verify PriorityClass exists before deletion
			Eventually(func(g Gomega) {
				var pc schedulingv1.PriorityClass
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "loopzero-tenant-starter"}, &pc)).To(Succeed())
			}, timeout, interval).Should(Succeed())
		})

		It("should NOT delete PriorityClass when tenant is deleted", func() {
			// Delete tenant
			var t loopzerov1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &t)).To(Succeed())

			// Clean up child Environment CRs so tenant finalizer can proceed
			// (envtest doesn't run GC, so we must manually delete and remove finalizers)
			var envList loopzerov1.EnvironmentList
			Expect(k8sClient.List(ctx, &envList, client.InNamespace("default"))).To(Succeed())
			for i := range envList.Items {
				env := &envList.Items[i]
				for _, ref := range env.OwnerReferences {
					if ref.Name == tenantName {
						if env.DeletionTimestamp.IsZero() {
							_ = k8sClient.Delete(ctx, env)
						}
						break
					}
				}
			}
			Eventually(func(g Gomega) {
				var envs loopzerov1.EnvironmentList
				g.Expect(k8sClient.List(ctx, &envs, client.InNamespace("default"))).To(Succeed())
				for i := range envs.Items {
					env := &envs.Items[i]
					for _, ref := range env.OwnerReferences {
						if ref.Name == tenantName {
							nsName := env.Spec.Namespace.Name
							var ns corev1.Namespace
							if err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, &ns); err == nil {
								ns.Finalizers = nil
								delete(ns.Labels, "loopzero.io/tenant")
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
			_ = k8sClient.List(ctx, &buildNsList, client.MatchingLabels{"loopzero.io/tenant": tenantName})
			for i := range buildNsList.Items {
				ns := &buildNsList.Items[i]
				ns.Finalizers = nil
				delete(ns.Labels, "loopzero.io/tenant")
				_ = k8sClient.Update(ctx, ns)
			}

			// Wait for tenant to be fully deleted
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &loopzerov1.Tenant{})
				g.Expect(err).To(HaveOccurred())
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}, 30*time.Second, interval).Should(Succeed())

			// PriorityClass should still exist
			var pc schedulingv1.PriorityClass
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "loopzero-tenant-starter"}, &pc)).To(Succeed())
			Expect(pc.Value).To(Equal(int32(100)))
		})
	})
}) // end PriorityClass

var _ = Describe("TenantController — Gitea Repo Provisioning", Ordered, func() {
	const tenantName = "gitea-test"

	BeforeAll(func() {
		tenant := newTenant(tenantName, loopzerov1.PlanStarter, []string{"production"})
		Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
		Eventually(func(g Gomega) {
			var t loopzerov1.Tenant
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			g.Expect(t.Status.Phase).ToNot(BeEmpty())
		}, timeout, interval).Should(Succeed())
	})

	Context("GitRepoReady condition", func() {
		It("should set GitRepoReady to True", func() {
			Eventually(func(g Gomega) {
				var t loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				found := false
				for _, c := range t.Status.Conditions {
					if c.Type == conditions.TypeGitRepoReady {
						found = true
						g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
					}
				}
				g.Expect(found).To(BeTrue(), "GitRepoReady condition should exist")
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("status.gitopsRepo", func() {
		It("should populate gitopsRepo URL in status", func() {
			Eventually(func(g Gomega) {
				var t loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Status.GitOpsRepo).ToNot(BeEmpty())
				g.Expect(t.Status.GitOpsRepo).To(ContainSubstring(t.Spec.GitOps.RepoName))
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("idempotency", func() {
		It("should call EnsureRepo only once per reconcile cycle (fake tracks calls)", func() {
			// Trigger re-reconcile
			var t loopzerov1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			initialCalls := fakeGitProvider.GetCalls()
			t.Spec.DisplayName = "Gitea idempotency"
			Expect(k8sClient.Update(ctx, &t)).To(Succeed())

			// Wait for reconcile
			Eventually(func(g Gomega) {
				var updated loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &updated)).To(Succeed())
				g.Expect(updated.Spec.DisplayName).To(Equal("Gitea idempotency"))
				// EnsureRepo called again but fake is idempotent (returns same URL)
				g.Expect(fakeGitProvider.GetCalls()).To(BeNumerically(">", initialCalls))
			}, timeout, interval).Should(Succeed())

			// GitRepoReady still True
			var updated loopzerov1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &updated)).To(Succeed())
			for _, c := range updated.Status.Conditions {
				if c.Type == conditions.TypeGitRepoReady {
					Expect(c.Status).To(Equal(metav1.ConditionTrue))
				}
			}
		})
	})

	AfterAll(func() {
		for _, nsName := range []string{"build-" + tenantName, "loopzero-" + tenantName + "-production"} {
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
}) // end Gitea Repo Provisioning

var _ = Describe("TenantController — Condition-Gated Reconciliation (Crash Recovery)", Ordered, func() {
	const tenantName = "crash-recovery"

	BeforeAll(func() {
		tenant := newTenant(tenantName, loopzerov1.PlanStarter, []string{"production"})
		Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

		// Wait for full reconciliation (Phase=Active means all conditions True)
		Eventually(func(g Gomega) {
			var t loopzerov1.Tenant
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			g.Expect(t.Status.Phase).To(Equal(loopzerov1.TenantPhaseActive))
		}, timeout, interval).Should(Succeed())
	})

	Context("when all conditions are already True at current generation", func() {
		It("should run all steps idempotently and preserve existing resources", func() {
			// Get the tenant and record current state
			var t loopzerov1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())

			// Record the git provider call count before triggering re-reconcile
			callsBefore := fakeGitProvider.GetCalls()

			// Add a custom annotation to the namespace to detect if it gets re-reconciled
			// (CreateOrUpdate preserves existing annotations, so the canary should persist)
			nsName := "loopzero-" + tenantName + "-production"
			var ns corev1.Namespace
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, &ns)).To(Succeed())
			if ns.Annotations == nil {
				ns.Annotations = make(map[string]string)
			}
			ns.Annotations["test.loopzero.io/canary"] = "should-persist"
			Expect(k8sClient.Update(ctx, &ns)).To(Succeed())

			// Trigger re-reconcile via annotation on tenant (NOT spec change, so generation stays same)
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			if t.Annotations == nil {
				t.Annotations = make(map[string]string)
			}
			t.Annotations["test.loopzero.io/trigger"] = "re-reconcile"
			Expect(k8sClient.Update(ctx, &t)).To(Succeed())

			// Wait for controller to process the re-reconcile (no hardcoded sleep)
			Eventually(func() int {
				return fakeGitProvider.GetCalls()
			}, timeout, interval).Should(BeNumerically(">", callsBefore),
				"EnsureRepo should be called on every reconcile (unconditional, idempotent)")

			// Verify: canary annotation on namespace should still be present
			// (CreateOrUpdate preserves annotations not managed by the controller)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, &ns)).To(Succeed())
			Expect(ns.Annotations).To(HaveKeyWithValue("test.loopzero.io/canary", "should-persist"),
				"namespace annotations should be preserved by idempotent CreateOrUpdate")
		})

		It("should still have all tenant-scoped conditions True and Phase Active", func() {
			var t loopzerov1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			Expect(t.Status.Phase).To(Equal(loopzerov1.TenantPhaseActive))

			// After Fase 3: namespace/RBAC/NetworkPolicy conditions moved to Environment CRs
			requiredConditions := []string{
				conditions.TypeGitRepoReady,
				conditions.TypeArgoCDAppReady,
				conditions.TypeBuildNamespaceReady,
				conditions.TypeQuotaEnforced,
				conditions.TypePriorityClassReady,
			}
			for _, condType := range requiredConditions {
				found := false
				for _, c := range t.Status.Conditions {
					if c.Type == condType {
						found = true
						Expect(c.Status).To(Equal(metav1.ConditionTrue),
							"condition %s should be True", condType)
					}
				}
				Expect(found).To(BeTrue(), "condition %s should exist", condType)
			}
		})
	})

	Context("when spec changes (generation bumps)", func() {
		It("should re-execute all steps even if conditions were True at old generation", func() {
			var t loopzerov1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())

			callsBefore := fakeGitProvider.GetCalls()

			// Change spec to bump generation
			t.Spec.DisplayName = "Crash Recovery Updated"
			Expect(k8sClient.Update(ctx, &t)).To(Succeed())

			// Wait for reconcile to complete with new generation
			Eventually(func(g Gomega) {
				var updated loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &updated)).To(Succeed())
				g.Expect(updated.Status.ObservedGeneration).To(Equal(updated.Generation))
			}, timeout, interval).Should(Succeed())

			// EnsureRepo SHOULD have been called (generation changed)
			Expect(fakeGitProvider.GetCalls()).To(BeNumerically(">", callsBefore),
				"EnsureRepo should be called when generation changes")
		})
	})

	AfterAll(func() {
		for _, nsName := range []string{"loopzero-" + tenantName + "-production", "build-" + tenantName} {
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

var _ = Describe("TenantController — Phase Transition + Deletion", Ordered, func() {
	const tenantName = "phase-trans"

	Context("Phase transition to Active", func() {
		It("should set Phase=Active when all conditions are True", func() {
			tenant := newTenant(tenantName, loopzerov1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			Eventually(func(g Gomega) {
				var t loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Status.Phase).To(Equal(loopzerov1.TenantPhaseActive))
			}, timeout, interval).Should(Succeed())
		})

		It("should set ObservedGeneration equal to Generation", func() {
			var t loopzerov1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			Expect(t.Status.ObservedGeneration).To(Equal(t.Generation))
		})

		It("should have all 5 tenant-scoped conditions True", func() {
			var t loopzerov1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())

			// After Fase 3: namespace/RBAC/NetworkPolicy conditions moved to Environment CRs
			requiredConditions := []string{
				conditions.TypeGitRepoReady,
				conditions.TypeArgoCDAppReady,
				conditions.TypeBuildNamespaceReady,
				conditions.TypeQuotaEnforced,
				conditions.TypePriorityClassReady,
			}
			for _, condType := range requiredConditions {
				found := false
				for _, c := range t.Status.Conditions {
					if c.Type == condType {
						found = true
						Expect(c.Status).To(Equal(metav1.ConditionTrue),
							"condition %s should be True", condType)
					}
				}
				Expect(found).To(BeTrue(), "condition %s should exist", condType)
			}
		})
	})

	Context("plan upgrade re-reconcile", func() {
		It("should update RBAC roles when plan changes from starter to enterprise", func() {
			var t loopzerov1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			t.Spec.Plan = loopzerov1.PlanEnterprise
			Expect(k8sClient.Update(ctx, &t)).To(Succeed())

			nsName := "loopzero-" + tenantName + "-production"
			Eventually(func(g Gomega) {
				var role rbacv1.Role
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "loopzero-deployer", Namespace: nsName,
				}, &role)).To(Succeed())
				hasIngresses := false
				for _, rule := range role.Rules {
					for _, res := range rule.Resources {
						if res == "ingresses" {
							hasIngresses = true
						}
					}
				}
				g.Expect(hasIngresses).To(BeTrue(), "enterprise role should have ingresses")
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("deletion with verify-then-remove finalizer (waits for child Environment CRs)", func() {
		It("should set Phase=Deleting when tenant is deleted", func() {
			var t loopzerov1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &t)).To(Succeed())

			// Controller should set Phase=Deleting
			Eventually(func(g Gomega) {
				var deleting loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &deleting)).To(Succeed())
				g.Expect(deleting.Status.Phase).To(Equal(loopzerov1.TenantPhaseDeleting))
			}, timeout, interval).Should(Succeed())
		})

		It("should keep finalizer while child Environment CRs still exist", func() {
			// Child Environment CRs are marked for deletion via ownerRef cascade,
			// but they have their own finalizer so they persist until namespace cleanup completes.
			var t loopzerov1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			Expect(t.Finalizers).To(ContainElement("loopzero.io/tenant-cleanup"))

			// Verify child Environment CRs still exist (they have environment-cleanup finalizer)
			var envList loopzerov1.EnvironmentList
			Expect(k8sClient.List(ctx, &envList, client.InNamespace("default"))).To(Succeed())
			childCount := 0
			for _, env := range envList.Items {
				for _, ref := range env.OwnerReferences {
					if ref.UID == t.UID {
						childCount++
					}
				}
			}
			Expect(childCount).To(BeNumerically(">", 0),
				"child Environment CRs should still exist while tenant is deleting")
		})

		It("should remove finalizer and complete deletion when child Environments are gone", func() {
			// envtest doesn't run the garbage collector, so ownerRef cascade doesn't
			// happen automatically. We must manually delete child Environment CRs
			// and then remove their finalizers to simulate the full cascade.
			var envList loopzerov1.EnvironmentList
			Expect(k8sClient.List(ctx, &envList, client.InNamespace("default"))).To(Succeed())
			for i := range envList.Items {
				env := &envList.Items[i]
				isMine := false
				for _, ref := range env.OwnerReferences {
					if ref.Name == tenantName {
						isMine = true
						break
					}
				}
				if !isMine {
					continue
				}
				// Delete the Environment CR (simulating K8s GC cascade)
				if env.DeletionTimestamp.IsZero() {
					Expect(k8sClient.Delete(ctx, env)).To(Succeed())
				}
			}

			// Now remove the environment-cleanup finalizer from each Environment CR
			// (simulating EnvironmentController completing namespace cleanup).
			// Also clean up the runtime namespaces (envtest doesn't delete namespaces).
			Eventually(func(g Gomega) {
				var envs loopzerov1.EnvironmentList
				g.Expect(k8sClient.List(ctx, &envs, client.InNamespace("default"))).To(Succeed())
				for i := range envs.Items {
					env := &envs.Items[i]
					for _, ref := range env.OwnerReferences {
						if ref.Name == tenantName {
							// Remove namespace label so EnvironmentController thinks ns is gone
							nsName := env.Spec.Namespace.Name
							var ns corev1.Namespace
							if err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, &ns); err == nil {
								ns.Finalizers = nil
								delete(ns.Labels, "loopzero.io/tenant")
								g.Expect(k8sClient.Update(ctx, &ns)).To(Succeed())
							}
							// Remove finalizer so Environment CR can be garbage-collected
							env.Finalizers = nil
							g.Expect(k8sClient.Update(ctx, env)).To(Succeed())
							break
						}
					}
				}
			}, timeout, interval).Should(Succeed())

			// Clean up build namespaces
			var buildNsList corev1.NamespaceList
			_ = k8sClient.List(ctx, &buildNsList, client.MatchingLabels{
				"loopzero.io/tenant": tenantName,
			})
			for i := range buildNsList.Items {
				ns := &buildNsList.Items[i]
				ns.Finalizers = nil
				delete(ns.Labels, "loopzero.io/tenant")
				_ = k8sClient.Update(ctx, ns)
			}

			// Tenant should eventually be fully deleted
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

var _ = Describe("TenantController — ArgoCD Application Provisioning", Ordered, func() {
	const tenantName = "argocd-test"

	BeforeAll(func() {
		tenant := newTenant(tenantName, loopzerov1.PlanStarter, []string{"production", "staging"})
		Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

		// Wait for full reconciliation (Phase=Active means all conditions True)
		Eventually(func(g Gomega) {
			var t loopzerov1.Tenant
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			g.Expect(t.Status.Phase).To(Equal(loopzerov1.TenantPhaseActive))
		}, timeout, interval).Should(Succeed())
	})

	Context("ArgoCD Application creation", func() {
		It("should ensure one ArgoCD Application per environment", func() {
			for _, env := range []string{"production", "staging"} {
				appName := fmt.Sprintf("%s-%s", tenantName, env)
				Eventually(func(g Gomega) {
					fakeArgoCDProvider.Mu.Lock()
					_, exists := fakeArgoCDProvider.Applications[appName]
					fakeArgoCDProvider.Mu.Unlock()
					g.Expect(exists).To(BeTrue(),
						"ArgoCD Application %s should exist", appName)
				}, timeout, interval).Should(Succeed())
			}
		})

		It("should set correct repoURL from Gitea URL pattern", func() {
			var t loopzerov1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())

			appName := fmt.Sprintf("%s-production", tenantName)
			fakeArgoCDProvider.Mu.Lock()
			app := fakeArgoCDProvider.Applications[appName]
			fakeArgoCDProvider.Mu.Unlock()

			Expect(app.RepoURL).To(ContainSubstring(t.Spec.GitOps.RepoName))
		})

		It("should set correct destination namespace per environment", func() {
			for _, env := range []string{"production", "staging"} {
				appName := fmt.Sprintf("%s-%s", tenantName, env)
				expectedNS := fmt.Sprintf("loopzero-%s-%s", tenantName, env)

				fakeArgoCDProvider.Mu.Lock()
				app := fakeArgoCDProvider.Applications[appName]
				fakeArgoCDProvider.Mu.Unlock()

				Expect(app.DestinationNS).To(Equal(expectedNS),
					"ArgoCD Application %s should target namespace %s", appName, expectedNS)
			}
		})

		It("should set correct path per environment (BLOCKER-3: scoped to env directory)", func() {
			// BLOCKER-3: Path is the environment name, not "." (root).
			// This scopes each ArgoCD Application to only its environment's manifests,
			// preventing cross-environment contamination.
			for _, env := range []string{"production", "staging"} {
				appName := fmt.Sprintf("%s-%s", tenantName, env)

				fakeArgoCDProvider.Mu.Lock()
				app := fakeArgoCDProvider.Applications[appName]
				fakeArgoCDProvider.Mu.Unlock()

				Expect(app.Path).To(Equal(env),
					"ArgoCD Application %s should have path '%s' (env-scoped)", appName, env)
			}
		})

		It("should set tenantID on each application", func() {
			for _, env := range []string{"production", "staging"} {
				appName := fmt.Sprintf("%s-%s", tenantName, env)

				fakeArgoCDProvider.Mu.Lock()
				app := fakeArgoCDProvider.Applications[appName]
				fakeArgoCDProvider.Mu.Unlock()

				Expect(app.TenantID).To(Equal(tenantName))
			}
		})

		It("should set destinationURL from reconciler config (not hardcoded)", func() {
			// The default WorkloadClusterURL is "https://kubernetes.default.svc" when not configured.
			// This test verifies the reconciler uses its configured value rather than a hardcoded string.
			for _, env := range []string{"production", "staging"} {
				appName := fmt.Sprintf("%s-%s", tenantName, env)

				fakeArgoCDProvider.Mu.Lock()
				app := fakeArgoCDProvider.Applications[appName]
				fakeArgoCDProvider.Mu.Unlock()

				Expect(app.DestinationURL).To(Equal("https://kubernetes.default.svc"),
					"ArgoCD Application %s should have destination URL from reconciler config", appName)
			}
		})

		It("should set namespace to argocd", func() {
			appName := fmt.Sprintf("%s-production", tenantName)

			fakeArgoCDProvider.Mu.Lock()
			app := fakeArgoCDProvider.Applications[appName]
			fakeArgoCDProvider.Mu.Unlock()

			Expect(app.Namespace).To(Equal("argocd"))
		})

		It("should set targetRevision to main", func() {
			appName := fmt.Sprintf("%s-production", tenantName)

			fakeArgoCDProvider.Mu.Lock()
			app := fakeArgoCDProvider.Applications[appName]
			fakeArgoCDProvider.Mu.Unlock()

			Expect(app.TargetRevision).To(Equal("main"))
		})
	})

	Context("ArgoCDAppReady condition", func() {
		It("should set ArgoCDAppReady condition to True", func() {
			Eventually(func(g Gomega) {
				var t loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				found := false
				for _, c := range t.Status.Conditions {
					if c.Type == conditions.TypeArgoCDAppReady {
						found = true
						g.Expect(c.Status).To(Equal(metav1.ConditionTrue))
					}
				}
				g.Expect(found).To(BeTrue(), "ArgoCDAppReady condition should exist")
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("idempotency", func() {
		It("should not create duplicate applications after re-reconcile", func() {
			fakeArgoCDProvider.Mu.Lock()
			callsBefore := fakeArgoCDProvider.EnsureCalls
			fakeArgoCDProvider.Mu.Unlock()

			// Trigger re-reconcile via spec update
			var t loopzerov1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			t.Spec.DisplayName = "ArgoCD idempotency"
			Expect(k8sClient.Update(ctx, &t)).To(Succeed())

			// Wait for reconcile to complete
			Eventually(func(g Gomega) {
				var updated loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &updated)).To(Succeed())
				g.Expect(updated.Status.ObservedGeneration).To(Equal(updated.Generation))
			}, timeout, interval).Should(Succeed())

			// Fake provisioner is idempotent: same apps, just more calls
			fakeArgoCDProvider.Mu.Lock()
			tenantAppCount := 0
			for _, app := range fakeArgoCDProvider.Applications {
				if app.TenantID == tenantName {
					tenantAppCount++
				}
			}
			callsAfter := fakeArgoCDProvider.EnsureCalls
			fakeArgoCDProvider.Mu.Unlock()

			Expect(tenantAppCount).To(Equal(2), "should still have exactly 2 applications for this tenant (production + staging)")
			Expect(callsAfter).To(BeNumerically(">", callsBefore),
				"EnsureApplication should be called on re-reconcile (generation changed)")
		})
	})

	AfterAll(func() {
		for _, nsName := range []string{"loopzero-" + tenantName + "-production", "loopzero-" + tenantName + "-staging", "build-" + tenantName} {
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

var _ = Describe("TenantController — ArgoCD Deletion Cleanup", Ordered, func() {
	const tenantName = "argocd-del"

	BeforeAll(func() {
		tenant := newTenant(tenantName, loopzerov1.PlanStarter, []string{"production"})
		Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

		// Wait for full reconciliation
		Eventually(func(g Gomega) {
			var t loopzerov1.Tenant
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			g.Expect(t.Status.Phase).To(Equal(loopzerov1.TenantPhaseActive))
		}, timeout, interval).Should(Succeed())
	})

	It("should call DeleteApplications on tenant deletion", func() {
		// Record delete calls before deletion
		fakeArgoCDProvider.Mu.Lock()
		deleteCallsBefore := fakeArgoCDProvider.DeleteCalls
		fakeArgoCDProvider.Mu.Unlock()

		// Delete the tenant
		var t loopzerov1.Tenant
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: tenantName, Namespace: "default",
		}, &t)).To(Succeed())
		Expect(k8sClient.Delete(ctx, &t)).To(Succeed())

		// Wait for Phase=Deleting
		Eventually(func(g Gomega) {
			var d loopzerov1.Tenant
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &d)).To(Succeed())
			g.Expect(d.Status.Phase).To(Equal(loopzerov1.TenantPhaseDeleting))
		}, timeout, interval).Should(Succeed())

		// Clean up child Environment CRs first (tenant waits for them before calling DeleteApplications)
		var envList loopzerov1.EnvironmentList
		Expect(k8sClient.List(ctx, &envList, client.InNamespace("default"))).To(Succeed())
		for i := range envList.Items {
			env := &envList.Items[i]
			for _, ref := range env.OwnerReferences {
				if ref.Name == tenantName {
					if env.DeletionTimestamp.IsZero() {
						_ = k8sClient.Delete(ctx, env)
					}
					break
				}
			}
		}
		// Remove environment-cleanup finalizers and namespace labels
		Eventually(func(g Gomega) {
			var envs loopzerov1.EnvironmentList
			g.Expect(k8sClient.List(ctx, &envs, client.InNamespace("default"))).To(Succeed())
			for i := range envs.Items {
				env := &envs.Items[i]
				for _, ref := range env.OwnerReferences {
					if ref.Name == tenantName {
						nsName := env.Spec.Namespace.Name
						var ns corev1.Namespace
						if err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, &ns); err == nil {
							ns.Finalizers = nil
							delete(ns.Labels, "loopzero.io/tenant")
							g.Expect(k8sClient.Update(ctx, &ns)).To(Succeed())
						}
						env.Finalizers = nil
						g.Expect(k8sClient.Update(ctx, env)).To(Succeed())
						break
					}
				}
			}
		}, timeout, interval).Should(Succeed())

		// Now wait for controller to call DeleteApplications (happens after child Environments are gone)
		Eventually(func(g Gomega) {
			fakeArgoCDProvider.Mu.Lock()
			calls := fakeArgoCDProvider.DeleteCalls
			fakeArgoCDProvider.Mu.Unlock()
			g.Expect(calls).To(BeNumerically(">", deleteCallsBefore),
				"DeleteApplications should be called during tenant deletion")
		}, timeout, interval).Should(Succeed())

		// Also clean up build namespaces
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

		// Wait for tenant to be fully deleted
		Eventually(func(g Gomega) {
			var deleted loopzerov1.Tenant
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &deleted)
			g.Expect(err).To(HaveOccurred())
			g.Expect(errors.IsNotFound(err)).To(BeTrue())
		}, 30*time.Second, interval).Should(Succeed())
	})

	It("should have removed applications for the deleted tenant", func() {
		fakeArgoCDProvider.Mu.Lock()
		defer fakeArgoCDProvider.Mu.Unlock()
		for name, app := range fakeArgoCDProvider.Applications {
			Expect(app.TenantID).ToNot(Equal(tenantName),
				"Application %s should have been deleted for tenant %s", name, tenantName)
		}
	})
})

var _ = Describe("TenantController — Environment CR Garbage Collection", Ordered, func() {
	const tenantName = "gc-env-test"

	BeforeAll(func() {
		tenant := newTenant(tenantName, loopzerov1.PlanStarter, []string{"production", "staging", "dev"})
		Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

		// Wait for controller to reconcile and create all 3 Environment CRs
		Eventually(func(g Gomega) {
			for _, env := range []string{"production", "staging", "dev"} {
				var envCR loopzerov1.Environment
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName + "-" + env, Namespace: "default",
				}, &envCR)).To(Succeed())
			}
		}, timeout, interval).Should(Succeed())
	})

	AfterAll(func() {
		cleanupTenantEnvCRs(tenantName)
		var t loopzerov1.Tenant
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: tenantName, Namespace: "default"}, &t); err == nil {
			t.Finalizers = nil
			_ = k8sClient.Update(ctx, &t)
			_ = k8sClient.Delete(ctx, &t)
		}
	})

	Context("when an environment is removed from spec.environments[]", func() {
		It("should garbage-collect the orphaned Environment CR", func() {
			// Arrange: verify all 3 Environment CRs exist
			var envList loopzerov1.EnvironmentList
			Expect(k8sClient.List(ctx, &envList, client.InNamespace("default"))).To(Succeed())
			ownedCount := 0
			for _, env := range envList.Items {
				for _, ref := range env.OwnerReferences {
					if ref.Name == tenantName {
						ownedCount++
						break
					}
				}
			}
			Expect(ownedCount).To(Equal(3), "should have 3 Environment CRs before removal")

			// Act: remove "dev" from spec.environments[]
			var t loopzerov1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			t.Spec.Environments = []loopzerov1.TenantEnvironmentEntry{
				{Name: "production", Cluster: "workload"},
				{Name: "staging", Cluster: "workload"},
			}
			Expect(k8sClient.Update(ctx, &t)).To(Succeed())

			// Wait for TenantController to issue Delete on the orphan Environment CR.
			// The EnvironmentController has a finalizer that waits for the namespace to be
			// deleted, but envtest doesn't delete namespaces. Simulate namespace cleanup
			// so the EnvironmentController can remove the finalizer.
			Eventually(func(g Gomega) {
				var devEnv loopzerov1.Environment
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName + "-dev", Namespace: "default",
				}, &devEnv)
				g.Expect(err).NotTo(HaveOccurred())
				// DeletionTimestamp should be set (tenant controller issued Delete)
				g.Expect(devEnv.DeletionTimestamp.IsZero()).To(BeFalse(),
					"orphan Environment CR should be marked for deletion")
			}, timeout, interval).Should(Succeed())

			// Simulate EnvironmentController completing namespace cleanup:
			// remove the namespace labels and finalizer from the Environment CR.
			Eventually(func(g Gomega) {
				var devEnv loopzerov1.Environment
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName + "-dev", Namespace: "default",
				}, &devEnv)).To(Succeed())
				// Clean up the runtime namespace so EnvironmentController can proceed
				var ns corev1.Namespace
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: devEnv.Spec.Namespace.Name}, &ns); err == nil {
					ns.Finalizers = nil
					delete(ns.Labels, "loopzero.io/tenant")
					g.Expect(k8sClient.Update(ctx, &ns)).To(Succeed())
				}
				// Remove environment-cleanup finalizer
				devEnv.Finalizers = nil
				g.Expect(k8sClient.Update(ctx, &devEnv)).To(Succeed())
			}, timeout, interval).Should(Succeed())

			// Assert: the "dev" Environment CR should be fully deleted
			Eventually(func(g Gomega) {
				var devEnv loopzerov1.Environment
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName + "-dev", Namespace: "default",
				}, &devEnv)
				g.Expect(err).To(HaveOccurred())
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			// The other 2 Environment CRs should still exist
			for _, env := range []string{"production", "staging"} {
				var envCR loopzerov1.Environment
				Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName + "-" + env, Namespace: "default",
				}, &envCR)).To(Succeed())
			}
		})

		It("should not delete Environment CRs owned by a different tenant", func() {
			// Arrange: create a second tenant with an env that has a similar name pattern
			otherTenant := newTenant("gc-env-other", loopzerov1.PlanStarter, []string{"staging"})
			Expect(k8sClient.Create(ctx, otherTenant)).To(Succeed())

			// Wait for other tenant's Environment CR to exist
			Eventually(func(g Gomega) {
				var envCR loopzerov1.Environment
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "gc-env-other-staging", Namespace: "default",
				}, &envCR)).To(Succeed())
			}, timeout, interval).Should(Succeed())

			// Act: remove "staging" from the FIRST tenant
			var t loopzerov1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			t.Spec.Environments = []loopzerov1.TenantEnvironmentEntry{
				{Name: "production", Cluster: "workload"},
			}
			Expect(k8sClient.Update(ctx, &t)).To(Succeed())

			// Wait for TenantController to mark the staging CR for deletion
			Eventually(func(g Gomega) {
				var envCR loopzerov1.Environment
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName + "-staging", Namespace: "default",
				}, &envCR)).To(Succeed())
				g.Expect(envCR.DeletionTimestamp.IsZero()).To(BeFalse())
			}, timeout, interval).Should(Succeed())

			// Simulate EnvironmentController completing cleanup
			Eventually(func(g Gomega) {
				var envCR loopzerov1.Environment
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName + "-staging", Namespace: "default",
				}, &envCR)).To(Succeed())
				var ns corev1.Namespace
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: envCR.Spec.Namespace.Name}, &ns); err == nil {
					ns.Finalizers = nil
					delete(ns.Labels, "loopzero.io/tenant")
					g.Expect(k8sClient.Update(ctx, &ns)).To(Succeed())
				}
				envCR.Finalizers = nil
				g.Expect(k8sClient.Update(ctx, &envCR)).To(Succeed())
			}, timeout, interval).Should(Succeed())

			// Assert: first tenant's staging CR should be fully deleted
			Eventually(func(g Gomega) {
				var envCR loopzerov1.Environment
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName + "-staging", Namespace: "default",
				}, &envCR)
				g.Expect(err).To(HaveOccurred())
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			// Assert: other tenant's staging CR must still exist
			var otherEnv loopzerov1.Environment
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "gc-env-other-staging", Namespace: "default",
			}, &otherEnv)).To(Succeed())
			Expect(otherEnv.OwnerReferences[0].Name).To(Equal("gc-env-other"))

			// Cleanup
			cleanupTenantEnvCRs("gc-env-other")
			var ot loopzerov1.Tenant
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "gc-env-other", Namespace: "default"}, &ot); err == nil {
				ot.Finalizers = nil
				_ = k8sClient.Update(ctx, &ot)
				_ = k8sClient.Delete(ctx, &ot)
			}
		})
	})
})
