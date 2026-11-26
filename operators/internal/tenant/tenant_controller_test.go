// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package tenant_test

import (
	"fmt"

	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/conditions"
	"github.com/getloopscale/loop-runtime/operators/operatortest"
	"github.com/getloopscale/loop-runtime/pkg/naming"
)

func newTenant(name string, plan loopruntimev1.PlanTier, envs []string) *loopruntimev1.Tenant {
	environments := make([]loopruntimev1.TenantEnvironmentEntry, len(envs))
	for i, e := range envs {
		environments[i] = loopruntimev1.TenantEnvironmentEntry{Name: e, Cluster: "workload", Project: "default-project"}
	}
	return &loopruntimev1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: loopruntimev1.TenantSpec{
			DisplayName:  "Test Tenant " + name,
			Plan:         plan,
			Environments: environments,
			GitOps: loopruntimev1.GitOpsSpec{
				RepoName: "loop-runtime-tenant-" + name,
				Branch:   "main",
			},
			Preview: loopruntimev1.PreviewSpec{
				MaxEnvironments: 3,
				TTLHours:        48,
			},
			Limits: loopruntimev1.ResourceLimits{
				MaxCPU:              resource.MustParse("8"),
				MaxMemory:           resource.MustParse("16Gi"),
				MaxStorage:          resource.MustParse("50Gi"),
				MaxBuildConcurrency: 2,
			},
		},
	}
}

// drainTenantEnvironments deletes every Environment CR owned by tenantName,
// clears the finalizers envtest has no garbage collector to clear, and returns
// only once none is left.
//
// The exit condition is the point. Clearing a finalizer and returning is not
// enough: the EnvironmentController re-adds it on its next reconcile, so a loop
// whose exit condition is "the Update succeeded" leaves the child alive — and
// the Tenant finalizer then blocks forever on "waiting for child Environment
// CRs to be deleted (remaining: 1)", which is exactly how the PriorityClass
// deletion spec timed out on slow runs (measured 2026-08-06).
func drainTenantEnvironments(tenantName string) {
	Eventually(func(g Gomega) {
		var envs loopruntimev1.EnvironmentList
		g.Expect(k8sClient.List(ctx, &envs, client.InNamespace("default"))).To(Succeed())
		remaining := 0
		for i := range envs.Items {
			env := &envs.Items[i]
			owned := false
			for _, ref := range env.OwnerReferences {
				if ref.Name == tenantName {
					owned = true
					break
				}
			}
			if !owned {
				continue
			}
			remaining++
			if env.DeletionTimestamp.IsZero() {
				_ = k8sClient.Delete(ctx, env)
			}
			if nsName := env.Spec.Namespace.Name; nsName != "" {
				var ns corev1.Namespace
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, &ns); err == nil {
					_ = operatortest.UpdateWithRetry(ctx, k8sClient, &ns, func() {
						ns.Finalizers = nil
						delete(ns.Labels, "runtime.getloopscale.com/tenant")
					})
				}
			}
			_ = operatortest.UpdateWithRetry(ctx, k8sClient, env, func() {
				env.Finalizers = nil
			})
		}
		g.Expect(remaining).To(BeZero(),
			"child Environment CRs of %s are still present; the Tenant finalizer cannot proceed", tenantName)
	}, timeout, interval).Should(Succeed())
}

// cleanupTenantEnvCRs removes finalizers from all Environment CRs owned by the
// given tenant so they can be collected during teardown. In envtest the GC does
// not run, so this must be called explicitly in AfterAll blocks.
//
// Deliberately single-shot and tolerant, unlike drainTenantEnvironments: this
// runs in AfterAll, where blocking until the children are gone turns a teardown
// into a second assertion and fails specs that already passed.
func cleanupTenantEnvCRs(tenantName string) {
	var envList loopruntimev1.EnvironmentList
	if err := k8sClient.List(ctx, &envList, client.InNamespace("default")); err != nil {
		return
	}
	for i := range envList.Items {
		env := &envList.Items[i]
		for _, ref := range env.OwnerReferences {
			if ref.Name == tenantName {
				_ = operatortest.UpdateWithRetry(ctx, k8sClient, env, func() {
					env.Finalizers = nil
				})
				_ = k8sClient.Delete(ctx, env)
				break
			}
		}
	}
}

// expectTenantCondition fetches the Tenant CR and asserts a specific
// condition type appears with the given status. Phase 3 sibling helper to
// expectEnvCondition — eliminates 4+ inline copies in tenant_controller_test
// (Build/Quota/PriorityClass/GitRepo/ArgoCD ready assertions).
func expectTenantCondition(tenantName, condType string, want metav1.ConditionStatus) {
	Eventually(func(g Gomega) {
		var t loopruntimev1.Tenant
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: tenantName, Namespace: "default",
		}, &t)).To(Succeed())
		found := false
		for _, c := range t.Status.Conditions {
			if c.Type == condType {
				found = true
				g.Expect(c.Status).To(Equal(want))
			}
		}
		g.Expect(found).To(BeTrue(), condType+" condition should exist on Tenant CR")
	}, timeout, interval).Should(Succeed())
}

// expectEnvCondition fetches the Environment CR identified by envCRName in
// the default namespace and asserts a specific condition type appears with
// the given status. Phase 3 helper: collapses ~9 inline Eventually+Get+
// iterate copies across tenant_controller_test + environment_controller_test.
func expectEnvCondition(envCRName, condType string, want metav1.ConditionStatus) {
	Eventually(func(g Gomega) {
		var envCR loopruntimev1.Environment
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: envCRName, Namespace: "default",
		}, &envCR)).To(Succeed())
		found := false
		for _, c := range envCR.Status.Conditions {
			if c.Type == condType {
				found = true
				g.Expect(c.Status).To(Equal(want))
			}
		}
		g.Expect(found).To(BeTrue(), condType+" condition should exist on Environment CR")
	}, timeout, interval).Should(Succeed())
}

// createTenantAndWaitActive creates a Tenant CR and waits for its phase to
// become Active. Phase 3 helper — eliminates the ~10-line BeforeAll block
// duplicated across PriorityClass / RBAC / NetworkPolicy / ResourceQuota
// contexts in tenant_controller_test.go.
func createTenantAndWaitActive(tenantName string, plan loopruntimev1.PlanTier, envs []string) {
	tenant := newTenant(tenantName, plan, envs)
	Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
	Eventually(func(g Gomega) {
		var t loopruntimev1.Tenant
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: tenantName, Namespace: "default",
		}, &t)).To(Succeed())
		g.Expect(t.Status.Phase).To(Equal(loopruntimev1.TenantPhaseActive))
	}, timeout, interval).Should(Succeed())
}

// expectDeployerRoleBinding waits for the canonical "loop-runtime-deployer"
// RoleBinding in the given namespace and asserts its RoleRef + Subjects
// shape: binds ServiceAccount "loop-runtime-deployer" to Role "loop-runtime-deployer".
// Phase 3 helper: collapses 2 inline 13-line copies across
// tenant_controller_test + tenant_isolation_test.
func expectDeployerRoleBinding(nsName string) {
	Eventually(func(g Gomega) {
		var rb rbacv1.RoleBinding
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: "loop-runtime-deployer", Namespace: nsName,
		}, &rb)).To(Succeed())
		g.Expect(rb.RoleRef.Name).To(Equal("loop-runtime-deployer"))
		g.Expect(rb.RoleRef.Kind).To(Equal("Role"))
		g.Expect(rb.Subjects).To(HaveLen(1))
		g.Expect(rb.Subjects[0].Kind).To(Equal("ServiceAccount"))
		g.Expect(rb.Subjects[0].Name).To(Equal("loop-runtime-deployer"))
	}, timeout, interval).Should(Succeed())
}

// expectTenantDeleted waits for a Tenant CR to be NotFound (fully deleted
// by Kubernetes after finalizers cleared). Phase 3 helper: collapses ~3
// inline 7-line Eventually+Get+IsNotFound copies in tenant_controller_test
// + tenant_integration_test.
func expectTenantDeleted(tenantName string) {
	Eventually(func(g Gomega) {
		err := k8sClient.Get(ctx, types.NamespacedName{
			Name: tenantName, Namespace: "default",
		}, &loopruntimev1.Tenant{})
		g.Expect(err).To(HaveOccurred())
		g.Expect(errors.IsNotFound(err)).To(BeTrue())
	}, timeout, interval).Should(Succeed())
}

// createGoldenTenantAndWaitActive creates a Tenant via newGoldenTenant and
// waits for its Status.Phase to be Active (full reconcile complete). Phase 3
// sibling helper to createGoldenTenantAndWaitPhaseSet (which uses non-empty)
// and createTenantAndWaitActive (which uses newTenant, not newGoldenTenant).
func createGoldenTenantAndWaitActive(tenantName string, plan loopruntimev1.PlanTier, envs []string) {
	tenant := newGoldenTenant(tenantName, plan, envs)
	Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
	Eventually(func(g Gomega) {
		var t loopruntimev1.Tenant
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: tenantName, Namespace: "default",
		}, &t)).To(Succeed())
		g.Expect(t.Status.Phase).To(Equal(loopruntimev1.TenantPhaseActive))
	}, timeout, interval).Should(Succeed())
}

// createGoldenTenantAndWaitPhaseSet creates a Tenant via newGoldenTenant and
// waits for its Status.Phase to be set (non-empty, meaning reconciler has
// run at least once). Phase 3 sibling helper to createTenantAndWaitActive
// — most golden tests use phase-set rather than phase-active because they
// inspect partial-reconcile state.
func createGoldenTenantAndWaitPhaseSet(tenantName string, plan loopruntimev1.PlanTier, envs []string) {
	tenant := newGoldenTenant(tenantName, plan, envs)
	Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
	Eventually(func(g Gomega) {
		var t loopruntimev1.Tenant
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: tenantName, Namespace: "default",
		}, &t)).To(Succeed())
		g.Expect(t.Status.Phase).ToNot(BeEmpty())
	}, timeout, interval).Should(Succeed())
}

// withEnv fetches an Environment CR by name (in default namespace) inside
// an Eventually loop and runs caller-supplied assertions against it.
// Phase 3 callback-style helper, sibling to withNetworkPolicy / withApp.
func withEnv(envCRName string, assertions func(g Gomega, env *loopruntimev1.Environment)) {
	Eventually(func(g Gomega) {
		var env loopruntimev1.Environment
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: envCRName, Namespace: "default",
		}, &env)).To(Succeed())
		assertions(g, &env)
	}, timeout, interval).Should(Succeed())
}

// withTenant fetches a Tenant CR by name (in default namespace) inside an
// Eventually loop and runs caller-supplied assertions against it.
// Phase 3 callback-style helper, sibling to withEnv / withApp.
func withTenant(tenantName string, assertions func(g Gomega, t *loopruntimev1.Tenant)) {
	Eventually(func(g Gomega) {
		var t loopruntimev1.Tenant
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: tenantName, Namespace: "default",
		}, &t)).To(Succeed())
		assertions(g, &t)
	}, timeout, interval).Should(Succeed())
}

// expectLimitRangeContainerType waits for a LimitRange and asserts it has
// exactly one Container-type limit. Phase 3 helper: collapses 4 inline
// near-clones across tenant_controller_test, environment_controller_test,
// and resource_quotas_test (3 occurrences) of the canonical
// Spec.Limits[0].Type == Container check.
func expectLimitRangeContainerType(lrName, namespace string) {
	Eventually(func(g Gomega) {
		var lr corev1.LimitRange
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: lrName, Namespace: namespace,
		}, &lr)).To(Succeed())
		g.Expect(lr.Spec.Limits).To(HaveLen(1))
		g.Expect(lr.Spec.Limits[0].Type).To(Equal(corev1.LimitTypeContainer))
	}, timeout, interval).Should(Succeed())
}

// expectServiceAccountExists waits for a ServiceAccount by name+namespace
// to exist. Phase 3 sibling helper to expectNetworkPolicyExists.
func expectServiceAccountExists(saName, namespace string) {
	Eventually(func(g Gomega) {
		var sa corev1.ServiceAccount
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: saName, Namespace: namespace,
		}, &sa)).To(Succeed())
	}, timeout, interval).Should(Succeed())
}

// expectNamespaceLabel waits for a Namespace by name and asserts a specific
// label key=value is present. Phase 3 helper: collapses ~6 inline copies in
// tenant_isolation_test.go across build + runtime namespace label checks.
func expectNamespaceLabel(nsName, label, value string) {
	Eventually(func(g Gomega) {
		var ns corev1.Namespace
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, &ns)).To(Succeed())
		g.Expect(ns.Labels).To(HaveKeyWithValue(label, value))
	}, timeout, interval).Should(Succeed())
}

// expectDeployerRoleHasResource waits for the canonical "loop-runtime-deployer"
// Role in the given namespace and asserts that one of its Rules grants
// access to a specific resource (e.g., "ingresses", "networkpolicies").
// Phase 3 helper: collapses 3 inline near-clones in tenant_controller_test
// + environment_controller_test (all checking "enterprise role should
// include ingresses").
func expectDeployerRoleHasResource(nsName, resource string) {
	Eventually(func(g Gomega) {
		var role rbacv1.Role
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: "loop-runtime-deployer", Namespace: nsName,
		}, &role)).To(Succeed())
		hasResource := false
		for _, rule := range role.Rules {
			for _, res := range rule.Resources {
				if res == resource {
					hasResource = true
				}
			}
		}
		g.Expect(hasResource).To(BeTrue(), "enterprise role should include "+resource)
	}, timeout, interval).Should(Succeed())
}

// expectPriorityClass waits for a PriorityClass by name and asserts its
// value + PreemptionPolicy match the per-plan canonical configuration plus
// the standard "runtime.getloopscale.com/managed-by=tenant-controller" label. Phase 3
// helper: eliminates 3 inline near-clones (starter=100/Never,
// growth=500/PreemptLowerPriority, enterprise=1000/PreemptLowerPriority).
func expectPriorityClass(name string, value int32, policy corev1.PreemptionPolicy) {
	Eventually(func(g Gomega) {
		var pc schedulingv1.PriorityClass
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, &pc)).To(Succeed())
		g.Expect(pc.Value).To(Equal(value))
		g.Expect(*pc.PreemptionPolicy).To(Equal(policy))
		g.Expect(pc.Labels).To(HaveKeyWithValue("runtime.getloopscale.com/managed-by", "tenant-controller"))
	}, timeout, interval).Should(Succeed())
}

// expectResourceQuotaExists waits for a ResourceQuota by name+namespace to
// exist. Phase 3 sibling helper to expectNetworkPolicyExists.
func expectResourceQuotaExists(name, namespace string) {
	Eventually(func(g Gomega) {
		var quota corev1.ResourceQuota
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: name, Namespace: namespace,
		}, &quota)).To(Succeed())
	}, timeout, interval).Should(Succeed())
}

// expectNetworkPolicyExists waits for a NetworkPolicy by name+namespace to
// exist. Phase 3 helper: eliminates 3 inline 6-line existence checks in
// network_policies_test.go.
func expectNetworkPolicyExists(name, namespace string) {
	Eventually(func(g Gomega) {
		var np networkingv1.NetworkPolicy
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: name, Namespace: namespace,
		}, &np)).To(Succeed())
	}, timeout, interval).Should(Succeed())
}

// withNetworkPolicy fetches a NetworkPolicy inside an Eventually loop and
// runs caller-supplied assertions against it. Phase 3 helper: eliminates
// the canonical 7-line Eventually+Get scaffold preceding each varying
// assertion in network_policies_test.go.
func withNetworkPolicy(name, namespace string, assertions func(g Gomega, np *networkingv1.NetworkPolicy)) {
	Eventually(func(g Gomega) {
		var np networkingv1.NetworkPolicy
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: name, Namespace: namespace,
		}, &np)).To(Succeed())
		assertions(g, &np)
	}, timeout, interval).Should(Succeed())
}

// deleteEnvWithFinalizer clears finalizers on an Environment CR and deletes
// it. Idempotent. Phase 3 sibling helper to deleteTenantWithFinalizer.
func deleteEnvWithFinalizer(envCRName string) {
	env := &loopruntimev1.Environment{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: envCRName, Namespace: "default"}, env); err == nil {
		_ = operatortest.UpdateWithRetry(ctx, k8sClient, env, func() {
			env.Finalizers = nil
		})
		_ = k8sClient.Delete(ctx, env)
	}
}

// deleteTenantWithFinalizer clears finalizers on a Tenant CR and deletes it.
// Idempotent (skips if Get returns NotFound). Companion to deleteTenantNamespaces.
func deleteTenantWithFinalizer(tenantName string) {
	t := &loopruntimev1.Tenant{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: tenantName, Namespace: "default"}, t); err == nil {
		_ = operatortest.UpdateWithRetry(ctx, k8sClient, t, func() {
			t.Finalizers = nil
		})
		_ = k8sClient.Delete(ctx, t)
	}
}

// deleteTenantNamespaces removes the runtime + build namespaces for a tenant.
// Idempotent — silently skips namespaces that don't exist. Phase 3 helper:
// eliminates ~10 inline copies of this loop across tenant_controller_test.go
// and tenant_integration_test.go (Quality-Gate dupl reduction).
func deleteTenantNamespaces(tenantName string, environments ...string) {
	if len(environments) == 0 {
		environments = []string{"production"}
	}
	nsNames := make([]string, 0, len(environments)+1)
	for _, env := range environments {
		nsNames = append(nsNames, "loop-runtime-"+tenantName+"-default-project-"+env)
	}
	nsNames = append(nsNames, "build-"+tenantName)
	for _, nsName := range nsNames {
		ns := &corev1.Namespace{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, ns); err == nil {
			_ = k8sClient.Delete(ctx, ns)
		}
	}
}

var _ = Describe("TenantController", Ordered, func() {

	Context("when a new Tenant CR is created", func() {
		It("should set Phase (Provisioning initially, then Active)", func() {
			tenant := newTenant("phase-test", loopruntimev1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			Eventually(func(g Gomega) {
				var t loopruntimev1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      "phase-test",
					Namespace: "default",
				}, &t)).To(Succeed())
				// Phase should be set (may already be Active if reconcile completed)
				g.Expect(t.Status.Phase).To(SatisfyAny(
					Equal(loopruntimev1.TenantPhaseProvisioning),
					Equal(loopruntimev1.TenantPhaseActive),
				))
			}, timeout, interval).Should(Succeed())
		})

		It("should add the tenant-cleanup finalizer", func() {
			Eventually(func(g Gomega) {
				var t loopruntimev1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      "phase-test",
					Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Finalizers).To(ContainElement("runtime.getloopscale.com/tenant-cleanup"))
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("when a Tenant CR has minimal spec (empty environments list)", func() {
		// The environments list is MinItems=0 BY DESIGN (see the CRD schema
		// comment in config/crd/bases/runtime.getloopscale.com_tenants.yaml: forcing MinItems=1
		// required factories to pre-populate environments). An empty list is
		// therefore ACCEPTED at admission; the "tenant has production" shape is
		// enforced elsewhere, not by a CRD minItems constraint. (This spec
		// previously asserted a minItems:1 rejection — stale after the
		// deliberate MinItems 1→0 change.)
		It("should ACCEPT an empty environments list (CRD MinItems: 0 by design)", func() {
			tenant := &loopruntimev1.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "empty-envs",
					Namespace: "default",
				},
				Spec: loopruntimev1.TenantSpec{
					DisplayName:  "Empty Envs",
					Plan:         loopruntimev1.PlanStarter,
					Environments: []loopruntimev1.TenantEnvironmentEntry{},
					GitOps: loopruntimev1.GitOpsSpec{
						RepoName: "loop-runtime-tenant-empty",
						Branch:   "main",
					},
					Preview: loopruntimev1.PreviewSpec{},
					// Valid Limits (maxBuildConcurrency has a CRD minimum of 1) so
					// the CR is otherwise-valid and the empty environments list is
					// the only thing under test.
					Limits: loopruntimev1.ResourceLimits{
						MaxCPU:              resource.MustParse("8"),
						MaxMemory:           resource.MustParse("16Gi"),
						MaxStorage:          resource.MustParse("50Gi"),
						MaxBuildConcurrency: 2,
					},
				},
			}
			err := k8sClient.Create(ctx, tenant)
			Expect(err).NotTo(HaveOccurred())
			_ = k8sClient.Delete(ctx, tenant)
		})
	})

	Context("idempotency", func() {
		It("should produce identical state after spec update triggers re-reconcile", func() {
			var t loopruntimev1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "phase-test",
				Namespace: "default",
			}, &t)).To(Succeed())

			Expect(operatortest.UpdateWithRetry(ctx, k8sClient, &t, func() {
				t.Spec.DisplayName = "Updated Display Name"
			})).To(Succeed())

			Eventually(func(g Gomega) {
				var updated loopruntimev1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      "phase-test",
					Namespace: "default",
				}, &updated)).To(Succeed())
				g.Expect(updated.Status.Phase).ToNot(BeEmpty())
				g.Expect(updated.Finalizers).To(ContainElement("runtime.getloopscale.com/tenant-cleanup"))
			}, timeout, interval).Should(Succeed())
		})
	})

	AfterAll(func() {
		for _, name := range []string{"phase-test", "empty-envs"} {
			cleanupTenantEnvCRs(name)
			t := &loopruntimev1.Tenant{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, t); err == nil {
				_ = operatortest.UpdateWithRetry(ctx, k8sClient, t, func() {
					t.Finalizers = nil
				})
				_ = k8sClient.Delete(ctx, t)
			}
		}
	})
})

var _ = Describe("TenantController — Namespace Provisioning", Ordered, func() {
	const tenantName = "ns-test"

	BeforeAll(func() {
		tenant := newTenant(tenantName, loopruntimev1.PlanStarter, []string{"production", "staging"})
		Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

		// Wait for controller to reconcile
		Eventually(func(g Gomega) {
			var t loopruntimev1.Tenant
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			g.Expect(t.Status.Phase).ToNot(BeEmpty())
		}, timeout, interval).Should(Succeed())
	})

	Context("namespace creation", func() {
		It("should create a namespace per environment", func() {
			for _, env := range []string{"production", "staging"} {
				nsName := "loop-runtime-" + tenantName + "-default-project-" + env
				Eventually(func(g Gomega) {
					var ns corev1.Namespace
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, &ns)).To(Succeed())
				}, timeout, interval).Should(Succeed())
			}
		})

		It("should set correct labels on namespaces", func() {
			for _, env := range []string{"production", "staging"} {
				nsName := "loop-runtime-" + tenantName + "-default-project-" + env
				Eventually(func(g Gomega) {
					var ns corev1.Namespace
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, &ns)).To(Succeed())
					g.Expect(ns.Labels).To(HaveKeyWithValue("runtime.getloopscale.com/tenant", tenantName))
					g.Expect(ns.Labels).To(HaveKeyWithValue("runtime.getloopscale.com/environment", env))
					g.Expect(ns.Labels).To(HaveKeyWithValue("runtime.getloopscale.com/managed-by", "environment-controller"))
				}, timeout, interval).Should(Succeed())
			}
		})

		It("should create Environment CRs for each environment", func() {
			for _, env := range []string{"production", "staging"} {
				envCRName := tenantName + "-default-project-" + env
				Eventually(func(g Gomega) {
					var envCR loopruntimev1.Environment
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name: envCRName, Namespace: "default",
					}, &envCR)).To(Succeed())
					g.Expect(envCR.Spec.TenantRef.Name).To(Equal(tenantName))
					g.Expect(envCR.Spec.Environment).To(Equal(env))
					g.Expect(envCR.Spec.Namespace.Name).To(Equal("loop-runtime-" + tenantName + "-default-project-" + env))
					// Verify ownerReference to Tenant
					g.Expect(envCR.OwnerReferences).To(HaveLen(1))
					g.Expect(envCR.OwnerReferences[0].Kind).To(Equal("Tenant"))
					g.Expect(envCR.OwnerReferences[0].Name).To(Equal(tenantName))
				}, timeout, interval).Should(Succeed())
			}
		})

		It("should NOT set ownerReference on namespaces (cross-namespace limitation)", func() {
			var ns corev1.Namespace
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "loop-runtime-" + tenantName + "-default-project-production"}, &ns)).To(Succeed())
			Expect(ns.OwnerReferences).To(BeEmpty())
		})
	})

	Context("ResourceQuota", func() {
		It("should create ResourceQuota in each namespace with spec.limits values", func() {
			for _, env := range []string{"production", "staging"} {
				nsName := "loop-runtime-" + tenantName + "-default-project-" + env
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
				nsName := "loop-runtime-" + tenantName + "-default-project-" + env
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
				envCRName := tenantName + "-default-project-" + env
				Eventually(func(g Gomega) {
					var envCR loopruntimev1.Environment
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name: envCRName, Namespace: "default",
					}, &envCR)).To(Succeed())
					g.Expect(envCR.Status.Phase).To(Equal(loopruntimev1.EnvironmentPhaseActive))
				}, timeout, interval).Should(Succeed())
			}
		})
	})

	Context("idempotency", func() {
		It("should not duplicate namespaces after re-reconcile", func() {
			// Trigger re-reconcile via spec update
			var t loopruntimev1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			Expect(operatortest.UpdateWithRetry(ctx, k8sClient, &t, func() {
				t.Spec.DisplayName = "Updated for idempotency"
			})).To(Succeed())

			// Wait for reconcile to complete
			Eventually(func(g Gomega) {
				var updated loopruntimev1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &updated)).To(Succeed())
				g.Expect(updated.Spec.DisplayName).To(Equal("Updated for idempotency"))
			}, timeout, interval).Should(Succeed())

			// Count namespaces with tenant label
			var nsList corev1.NamespaceList
			Expect(k8sClient.List(ctx, &nsList, client.MatchingLabels{
				"runtime.getloopscale.com/tenant": tenantName,
			})).To(Succeed())
			Expect(nsList.Items).To(HaveLen(3)) // production + staging + build-{tenant}
		})
	})

	AfterAll(func() {
		deleteTenantNamespaces(tenantName, "production", "staging")
		deleteTenantWithFinalizer(tenantName)
	})
})

var _ = Describe("TenantController — RBAC", Ordered, func() {
	const tenantName = "rbac-test"

	BeforeAll(func() {
		tenant := newTenant(tenantName, loopruntimev1.PlanStarter, []string{"production"})
		Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

		Eventually(func(g Gomega) {
			var t loopruntimev1.Tenant
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			g.Expect(t.Status.Phase).ToNot(BeEmpty())
		}, timeout, interval).Should(Succeed())
	})

	Context("ServiceAccount", func() {
		It("should create loop-runtime-deployer ServiceAccount in each namespace", func() {
			expectServiceAccountExists("loop-runtime-deployer", "loop-runtime-"+tenantName+"-default-project-production")
		})
	})

	Context("Role for starter plan", func() {
		It("should create Role with basic deploy permissions", func() {
			nsName := "loop-runtime-" + tenantName + "-default-project-production"
			Eventually(func(g Gomega) {
				var role rbacv1.Role
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "loop-runtime-deployer", Namespace: nsName,
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
		It("should bind loop-runtime-deployer SA to the Role", func() {
			expectDeployerRoleBinding("loop-runtime-" + tenantName + "-default-project-production")
		})
	})

	Context("RBACReady condition (on Environment CR)", func() {
		It("should set RBACReady on Environment CR (not on Tenant)", func() {
			expectEnvCondition(tenantName+"-default-project-production", conditions.TypeEnvRBACReady, metav1.ConditionTrue)
		})
	})

	Context("enterprise plan RBAC", func() {
		It("should include extra permissions for enterprise plan", func() {
			// Create enterprise tenant
			entTenant := newTenant("rbac-ent", loopruntimev1.PlanEnterprise, []string{"production"})
			Expect(k8sClient.Create(ctx, entTenant)).To(Succeed())

			nsName := "loop-runtime-rbac-ent-default-project-production"
			// Enterprise should have ingresses and networkpolicies
			expectDeployerRoleHasResource(nsName, "ingresses")
		})
	})

	Context("idempotency", func() {
		It("should not duplicate RBAC resources after re-reconcile", func() {
			var t loopruntimev1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			Expect(operatortest.UpdateWithRetry(ctx, k8sClient, &t, func() {
				t.Spec.DisplayName = "RBAC idempotency"
			})).To(Succeed())

			// Verify still single SA, Role, RoleBinding
			nsName := "loop-runtime-" + tenantName + "-default-project-production"
			Eventually(func(g Gomega) {
				var saList corev1.ServiceAccountList
				g.Expect(k8sClient.List(ctx, &saList, client.InNamespace(nsName))).To(Succeed())
				saCount := 0
				for _, sa := range saList.Items {
					if sa.Name == "loop-runtime-deployer" {
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
			t := &loopruntimev1.Tenant{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, t); err == nil {
				_ = operatortest.UpdateWithRetry(ctx, k8sClient, t, func() {
					t.Finalizers = nil
				})
				_ = k8sClient.Delete(ctx, t)
			}
		}
	})
})

var _ = Describe("TenantController — NetworkPolicy", Ordered, func() {
	const tenantName = "netpol-test"

	BeforeAll(func() {
		tenant := newTenant(tenantName, loopruntimev1.PlanStarter, []string{"production"})
		Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
		Eventually(func(g Gomega) {
			var t loopruntimev1.Tenant
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			g.Expect(t.Status.Phase).ToNot(BeEmpty())
		}, timeout, interval).Should(Succeed())
	})

	Context("NetworkPolicy creation", func() {
		It("should create 3 NetworkPolicies per namespace (deny-all, allow-ingress, allow-egress)", func() {
			nsName := "loop-runtime-" + tenantName + "-default-project-production"
			Eventually(func(g Gomega) {
				var policies networkingv1.NetworkPolicyList
				g.Expect(k8sClient.List(ctx, &policies, client.InNamespace(nsName))).To(Succeed())
				g.Expect(len(policies.Items)).To(BeNumerically(">=", 3))
			}, timeout, interval).Should(Succeed())
		})

		It("should create deny-all policy", func() {
			nsName := "loop-runtime-" + tenantName + "-default-project-production"
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
			nsName := "loop-runtime-" + tenantName + "-default-project-production"
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
			expectEnvCondition(tenantName+"-default-project-production", conditions.TypeEnvNetworkPolicyReady, metav1.ConditionTrue)
		})
	})

	AfterAll(func() {
		ns := &corev1.Namespace{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: "loop-runtime-" + tenantName + "-default-project-production"}, ns); err == nil {
			_ = k8sClient.Delete(ctx, ns)
		}
		deleteTenantWithFinalizer(tenantName)
	})
})

var _ = Describe("TenantController — Build Namespace + QuotaEnforced", Ordered, func() {
	const tenantName = "build-ns-test"

	BeforeAll(func() {
		tenant := newTenant(tenantName, loopruntimev1.PlanStarter, []string{"production"})
		Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
		Eventually(func(g Gomega) {
			var t loopruntimev1.Tenant
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
				g.Expect(ns.Labels).To(HaveKeyWithValue("runtime.getloopscale.com/tenant", tenantName))
				g.Expect(ns.Labels).To(HaveKeyWithValue("runtime.getloopscale.com/component", "build"))
				g.Expect(ns.Labels).To(HaveKeyWithValue("runtime.getloopscale.com/managed-by", "tenant-controller"))
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
			expectTenantCondition(tenantName, conditions.TypeBuildNamespaceReady, metav1.ConditionTrue)
		})

		It("should set QuotaEnforced to True", func() {
			expectTenantCondition(tenantName, conditions.TypeQuotaEnforced, metav1.ConditionTrue)
		})
	})

	Context("PriorityClassReady condition", func() {
		It("should set PriorityClassReady to True", func() {
			expectTenantCondition(tenantName, conditions.TypePriorityClassReady, metav1.ConditionTrue)
		})
	})

	AfterAll(func() {
		deleteTenantNamespaces(tenantName)
		deleteTenantWithFinalizer(tenantName)
	})
}) // end Build Namespace + QuotaEnforced

var _ = Describe("TenantController — PriorityClass", Ordered, func() {

	Context("starter plan", func() {
		const tenantName = "pc-starter"

		BeforeAll(func() {
			createTenantAndWaitActive(tenantName, loopruntimev1.PlanStarter, []string{"production"})
		})

		It("should create PriorityClass with value 100 and PreemptionPolicy Never", func() {
			expectPriorityClass("loop-runtime-tenant-starter", 100, corev1.PreemptNever)
		})

		It("should set PriorityClassReady condition to True", func() {
			expectTenantCondition(tenantName, conditions.TypePriorityClassReady, metav1.ConditionTrue)
		})

		It("should be idempotent — second reconciliation does not fail", func() {
			var t loopruntimev1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			Expect(operatortest.UpdateWithRetry(ctx, k8sClient, &t, func() {
				t.Spec.DisplayName = "PC starter idempotency"
			})).To(Succeed())

			Eventually(func(g Gomega) {
				var updated loopruntimev1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &updated)).To(Succeed())
				g.Expect(updated.Status.Phase).To(Equal(loopruntimev1.TenantPhaseActive))
				g.Expect(updated.Status.ObservedGeneration).To(Equal(updated.Generation))
			}, timeout, interval).Should(Succeed())

			// PriorityClass still exists with correct values
			var pc schedulingv1.PriorityClass
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "loop-runtime-tenant-starter"}, &pc)).To(Succeed())
			Expect(pc.Value).To(Equal(int32(100)))
		})

		AfterAll(func() {
			deleteTenantWithFinalizer(tenantName)
			deleteTenantNamespaces(tenantName)
		})
	})

	Context("growth plan", func() {
		const tenantName = "pc-growth"

		BeforeAll(func() {
			createTenantAndWaitActive(tenantName, loopruntimev1.PlanGrowth, []string{"production"})
		})

		It("should create PriorityClass with value 500 and PreemptLowerPriority", func() {
			expectPriorityClass("loop-runtime-tenant-growth", 500, corev1.PreemptLowerPriority)
		})

		AfterAll(func() {
			deleteTenantWithFinalizer(tenantName)
			deleteTenantNamespaces(tenantName)
		})
	})

	// The api writes the canonical tier names; the CRD must accept them.
	Context("pro plan", func() {
		const tenantName = "pc-pro"

		BeforeAll(func() {
			createTenantAndWaitActive(tenantName, loopruntimev1.PlanPro, []string{"production"})
		})

		It("should create the paid mid-tier PriorityClass", func() {
			expectPriorityClass("loop-runtime-tenant-growth", 500, corev1.PreemptLowerPriority)
		})

		AfterAll(func() {
			deleteTenantWithFinalizer(tenantName)
			deleteTenantNamespaces(tenantName)
		})
	})

	Context("enterprise plan", func() {
		const tenantName = "pc-enterprise"

		BeforeAll(func() {
			createTenantAndWaitActive(tenantName, loopruntimev1.PlanEnterprise, []string{"production"})
		})

		It("should create PriorityClass with value 1000 and PreemptLowerPriority", func() {
			expectPriorityClass("loop-runtime-tenant-enterprise", 1000, corev1.PreemptLowerPriority)
		})

		AfterAll(func() {
			deleteTenantWithFinalizer(tenantName)
			deleteTenantNamespaces(tenantName)
		})
	})

	Context("plan upgrade creates new PriorityClass", func() {
		const tenantName = "pc-upgrade"

		BeforeAll(func() {
			createTenantAndWaitActive(tenantName, loopruntimev1.PlanStarter, []string{"production"})
		})

		It("should create growth PriorityClass after plan upgrade from starter to growth", func() {
			// Upgrade plan
			var t loopruntimev1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			Expect(operatortest.UpdateWithRetry(ctx, k8sClient, &t, func() {
				t.Spec.Plan = loopruntimev1.PlanGrowth
			})).To(Succeed())

			// New PriorityClass should be created
			Eventually(func(g Gomega) {
				var pc schedulingv1.PriorityClass
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "loop-runtime-tenant-growth"}, &pc)).To(Succeed())
				g.Expect(pc.Value).To(Equal(int32(500)))
				g.Expect(*pc.PreemptionPolicy).To(Equal(corev1.PreemptLowerPriority))
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			deleteTenantWithFinalizer(tenantName)
			deleteTenantNamespaces(tenantName)
		})
	})

	Context("deletion does NOT delete PriorityClass (shared across tenants)", func() {
		const tenantName = "pc-delete"

		BeforeAll(func() {
			tenant := newTenant(tenantName, loopruntimev1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
			Eventually(func(g Gomega) {
				var t loopruntimev1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Status.Phase).To(Equal(loopruntimev1.TenantPhaseActive))
			}, timeout, interval).Should(Succeed())

			// Verify PriorityClass exists before deletion
			Eventually(func(g Gomega) {
				var pc schedulingv1.PriorityClass
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "loop-runtime-tenant-starter"}, &pc)).To(Succeed())
			}, timeout, interval).Should(Succeed())
		})

		It("should NOT delete PriorityClass when tenant is deleted", func() {
			// Delete tenant
			var t loopruntimev1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &t)).To(Succeed())

			// Clean up child Environment CRs so tenant finalizer can proceed
			// (envtest doesn't run GC, so we must manually delete and remove finalizers)
			drainTenantEnvironments(tenantName)

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

			// Wait for tenant to be fully deleted
			expectTenantDeleted(tenantName)

			// PriorityClass should still exist
			var pc schedulingv1.PriorityClass
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "loop-runtime-tenant-starter"}, &pc)).To(Succeed())
			Expect(pc.Value).To(Equal(int32(100)))
		})
	})
}) // end PriorityClass

var _ = Describe("TenantController — Gitea Repo Provisioning", Ordered, func() {
	const tenantName = "gitea-test"

	BeforeAll(func() {
		tenant := newTenant(tenantName, loopruntimev1.PlanStarter, []string{"production"})
		Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
		Eventually(func(g Gomega) {
			var t loopruntimev1.Tenant
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			g.Expect(t.Status.Phase).ToNot(BeEmpty())
		}, timeout, interval).Should(Succeed())
	})

	Context("GitRepoReady condition", func() {
		It("should set GitRepoReady to True", func() {
			expectTenantCondition(tenantName, conditions.TypeGitRepoReady, metav1.ConditionTrue)
		})
	})

	Context("status.gitopsRepo", func() {
		It("should populate gitopsRepo URL in status", func() {
			Eventually(func(g Gomega) {
				var t loopruntimev1.Tenant
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
			var t loopruntimev1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			initialCalls := fakeGitProvider.GetCalls()
			Expect(operatortest.UpdateWithRetry(ctx, k8sClient, &t, func() {
				t.Spec.DisplayName = "Gitea idempotency"
			})).To(Succeed())

			// Wait for reconcile
			Eventually(func(g Gomega) {
				var updated loopruntimev1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &updated)).To(Succeed())
				g.Expect(updated.Spec.DisplayName).To(Equal("Gitea idempotency"))
				// EnsureRepo called again but fake is idempotent (returns same URL)
				g.Expect(fakeGitProvider.GetCalls()).To(BeNumerically(">", initialCalls))
			}, timeout, interval).Should(Succeed())

			// GitRepoReady still True
			var updated loopruntimev1.Tenant
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
		for _, nsName := range []string{"build-" + tenantName, "loop-runtime-" + tenantName + "-default-project-production"} {
			ns := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, ns); err == nil {
				_ = k8sClient.Delete(ctx, ns)
			}
		}
		deleteTenantWithFinalizer(tenantName)
	})
}) // end Gitea Repo Provisioning

var _ = Describe("TenantController — Condition-Gated Reconciliation (Crash Recovery)", Ordered, func() {
	const tenantName = "crash-recovery"

	BeforeAll(func() {
		tenant := newTenant(tenantName, loopruntimev1.PlanStarter, []string{"production"})
		Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

		// Wait for full reconciliation (Phase=Active means all conditions True)
		Eventually(func(g Gomega) {
			var t loopruntimev1.Tenant
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			g.Expect(t.Status.Phase).To(Equal(loopruntimev1.TenantPhaseActive))
		}, timeout, interval).Should(Succeed())
	})

	Context("when all conditions are already True at current generation", func() {
		It("should run all steps idempotently and preserve existing resources", func() {
			// Get the tenant and record current state
			var t loopruntimev1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())

			// Record the git provider call count before triggering re-reconcile
			callsBefore := fakeGitProvider.GetCalls()

			// Add a custom annotation to the namespace to detect if it gets re-reconciled
			// (CreateOrUpdate preserves existing annotations, so the canary should persist)
			nsName := "loop-runtime-" + tenantName + "-default-project-production"
			var ns corev1.Namespace
			// Phase=Active (asserted in BeforeAll) does not imply this namespace is
			// already readable — it is created by the Environment reconcile, which
			// races the assertion. Wait for it rather than Get-and-hope.
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, &ns)).To(Succeed())
			}, timeout, interval).Should(Succeed())
			Expect(operatortest.UpdateWithRetry(ctx, k8sClient, &ns, func() {
				// Inside the closure: the helper re-reads ns before each attempt,
				// so a nil-map guard placed outside is undone on the retry.
				if ns.Annotations == nil {
					ns.Annotations = make(map[string]string)
				}
				ns.Annotations["test.runtime.getloopscale.com/canary"] = "should-persist"
			})).To(Succeed())

			// Trigger re-reconcile via annotation on tenant (NOT spec change, so generation stays same)
			// Retry update with conflict resolution (409 is expected in envtest
			// when concurrent reconciles modify the same object).
			Eventually(func() error {
				if err := k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t); err != nil {
					return err
				}
				if t.Annotations == nil {
					t.Annotations = make(map[string]string)
				}
				t.Annotations["test.runtime.getloopscale.com/trigger"] = "re-reconcile"
				return k8sClient.Update(ctx, &t)
			}, timeout, interval).Should(Succeed())

			// Wait for controller to process the re-reconcile (no hardcoded sleep)
			Eventually(func() int {
				return fakeGitProvider.GetCalls()
			}, timeout, interval).Should(BeNumerically(">", callsBefore),
				"EnsureRepo should be called on every reconcile (unconditional, idempotent)")

			// Verify: canary annotation on namespace should still be present
			// (CreateOrUpdate preserves annotations not managed by the controller)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, &ns)).To(Succeed())
			Expect(ns.Annotations).To(HaveKeyWithValue("test.runtime.getloopscale.com/canary", "should-persist"),
				"namespace annotations should be preserved by idempotent CreateOrUpdate")
		})

		It("should still have all tenant-scoped conditions True and Phase Active", func() {
			var t loopruntimev1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			Expect(t.Status.Phase).To(Equal(loopruntimev1.TenantPhaseActive))

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
			var t loopruntimev1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())

			callsBefore := fakeGitProvider.GetCalls()

			// Change spec to bump generation
			Expect(operatortest.UpdateWithRetry(ctx, k8sClient, &t, func() {
				t.Spec.DisplayName = "Crash Recovery Updated"
			})).To(Succeed())

			// Wait for reconcile to complete with new generation
			Eventually(func(g Gomega) {
				var updated loopruntimev1.Tenant
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
		deleteTenantNamespaces(tenantName)
		deleteTenantWithFinalizer(tenantName)
	})
})

var _ = Describe("TenantController — Phase Transition + Deletion", Ordered, func() {
	const tenantName = "phase-trans"

	Context("Phase transition to Active", func() {
		It("should set Phase=Active when all conditions are True", func() {
			tenant := newTenant(tenantName, loopruntimev1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			Eventually(func(g Gomega) {
				var t loopruntimev1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Status.Phase).To(Equal(loopruntimev1.TenantPhaseActive))
			}, timeout, interval).Should(Succeed())
		})

		It("should set ObservedGeneration equal to Generation", func() {
			var t loopruntimev1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			Expect(t.Status.ObservedGeneration).To(Equal(t.Generation))
		})

		It("should have all 5 tenant-scoped conditions True", func() {
			var t loopruntimev1.Tenant
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
			var t loopruntimev1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			Expect(operatortest.UpdateWithRetry(ctx, k8sClient, &t, func() {
				t.Spec.Plan = loopruntimev1.PlanEnterprise
			})).To(Succeed())

			nsName := "loop-runtime-" + tenantName + "-default-project-production"
			expectDeployerRoleHasResource(nsName, "ingresses")
		})
	})

	Context("deletion with verify-then-remove finalizer (waits for child Environment CRs)", func() {
		It("should set Phase=Deleting when tenant is deleted", func() {
			var t loopruntimev1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &t)).To(Succeed())

			// Controller should set Phase=Deleting
			Eventually(func(g Gomega) {
				var deleting loopruntimev1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &deleting)).To(Succeed())
				g.Expect(deleting.Status.Phase).To(Equal(loopruntimev1.TenantPhaseDeleting))
			}, timeout, interval).Should(Succeed())
		})

		It("should keep finalizer while child Environment CRs still exist", func() {
			// Child Environment CRs are marked for deletion via ownerRef cascade,
			// but they have their own finalizer so they persist until namespace cleanup completes.
			var t loopruntimev1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			Expect(t.Finalizers).To(ContainElement("runtime.getloopscale.com/tenant-cleanup"))

			// Drain the child Environment CRs (they carry the environment-cleanup
			// finalizer, which envtest has no GC to clear) so the Tenant finalizer
			// can proceed to the assertions below.
			drainTenantEnvironments(tenantName)

			// Clean up build namespaces — remove tenant label so the controller
			// sees zero remaining namespaces and proceeds to remove the finalizer.
			// Use Eventually to handle race conditions with the controller's requeue loop.
			Eventually(func(g Gomega) {
				var buildNsList corev1.NamespaceList
				g.Expect(k8sClient.List(ctx, &buildNsList, client.MatchingLabels{
					"runtime.getloopscale.com/tenant": tenantName,
				})).To(Succeed())
				for i := range buildNsList.Items {
					ns := buildNsList.Items[i].DeepCopy()
					ns.Finalizers = nil
					delete(ns.Labels, "runtime.getloopscale.com/tenant")
					g.Expect(k8sClient.Update(ctx, ns)).To(Succeed())
				}
				// Verify labels are actually gone
				var verify corev1.NamespaceList
				g.Expect(k8sClient.List(ctx, &verify, client.MatchingLabels{
					"runtime.getloopscale.com/tenant": tenantName,
				})).To(Succeed())
				g.Expect(verify.Items).To(BeEmpty(), "build namespaces should have no tenant label")
			}, timeout, interval).Should(Succeed())

			// Tenant should eventually be fully deleted
			Eventually(func(g Gomega) {
				var deleted loopruntimev1.Tenant
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
		tenant := newTenant(tenantName, loopruntimev1.PlanStarter, []string{"production", "staging"})
		Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

		// Wait for full reconciliation (Phase=Active means all conditions True)
		Eventually(func(g Gomega) {
			var t loopruntimev1.Tenant
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			g.Expect(t.Status.Phase).To(Equal(loopruntimev1.TenantPhaseActive))
		}, timeout, interval).Should(Succeed())
	})

	Context("ArgoCD Application creation", func() {
		It("should ensure one ArgoCD Application per environment", func() {
			for _, env := range []string{"production", "staging"} {
				appName, nerr := naming.ArgoAppName(tenantName, "default-project", env)
				Expect(nerr).NotTo(HaveOccurred())
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
			var t loopruntimev1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())

			appName, nerr := naming.ArgoAppName(tenantName, "default-project", "production")
			Expect(nerr).NotTo(HaveOccurred())
			fakeArgoCDProvider.Mu.Lock()
			app := fakeArgoCDProvider.Applications[appName]
			fakeArgoCDProvider.Mu.Unlock()

			Expect(app.RepoURL).To(ContainSubstring(t.Spec.GitOps.RepoName))
		})

		It("should set correct destination namespace per environment", func() {
			for _, env := range []string{"production", "staging"} {
				appName, nerr := naming.ArgoAppName(tenantName, "default-project", env)
				Expect(nerr).NotTo(HaveOccurred())
				expectedNS := fmt.Sprintf("loop-runtime-%s-default-project-%s", tenantName, env)

				fakeArgoCDProvider.Mu.Lock()
				app := fakeArgoCDProvider.Applications[appName]
				fakeArgoCDProvider.Mu.Unlock()

				Expect(app.DestinationNS).To(Equal(expectedNS),
					"ArgoCD Application %s should target namespace %s", appName, expectedNS)
			}
		})

		It("should set correct path per environment+project (BLOCKER-3 + #271: scoped to the project subtree)", func() {
			// BLOCKER-3: Path is the environment name, not "." (root).
			// This scopes each ArgoCD Application to only its environment's manifests,
			// preventing cross-environment contamination.
			for _, env := range []string{"production", "staging"} {
				appName, nerr := naming.ArgoAppName(tenantName, "default-project", env)
				Expect(nerr).NotTo(HaveOccurred())

				fakeArgoCDProvider.Mu.Lock()
				app := fakeArgoCDProvider.Applications[appName]
				fakeArgoCDProvider.Mu.Unlock()

				// #271: the path is {env}/{project}, matching where the pusher writes
				// the manifests (api/internal/gitops/gitea_pusher.go:136). Pointing at
				// {env} alone made the App read every project's manifests in that
				// environment.
				Expect(app.Path).To(Equal(env+"/default-project"),
					"ArgoCD Application %s should be scoped to its own project subtree", appName)
			}
		})

		It("should set tenantID on each application", func() {
			for _, env := range []string{"production", "staging"} {
				appName, nerr := naming.ArgoAppName(tenantName, "default-project", env)
				Expect(nerr).NotTo(HaveOccurred())

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
				appName, nerr := naming.ArgoAppName(tenantName, "default-project", env)
				Expect(nerr).NotTo(HaveOccurred())

				fakeArgoCDProvider.Mu.Lock()
				app := fakeArgoCDProvider.Applications[appName]
				fakeArgoCDProvider.Mu.Unlock()

				Expect(app.DestinationURL).To(Equal("https://kubernetes.default.svc"),
					"ArgoCD Application %s should have destination URL from reconciler config", appName)
			}
		})

		It("should set namespace to argocd", func() {
			appName, nerr := naming.ArgoAppName(tenantName, "default-project", "production")
			Expect(nerr).NotTo(HaveOccurred())

			fakeArgoCDProvider.Mu.Lock()
			app := fakeArgoCDProvider.Applications[appName]
			fakeArgoCDProvider.Mu.Unlock()

			Expect(app.Namespace).To(Equal("argocd"))
		})

		It("should set targetRevision to main", func() {
			appName, nerr := naming.ArgoAppName(tenantName, "default-project", "production")
			Expect(nerr).NotTo(HaveOccurred())

			fakeArgoCDProvider.Mu.Lock()
			app := fakeArgoCDProvider.Applications[appName]
			fakeArgoCDProvider.Mu.Unlock()

			Expect(app.TargetRevision).To(Equal("main"))
		})
	})

	Context("ArgoCDAppReady condition", func() {
		It("should set ArgoCDAppReady condition to True", func() {
			expectTenantCondition(tenantName, conditions.TypeArgoCDAppReady, metav1.ConditionTrue)
		})
	})

	Context("idempotency", func() {
		It("should not create duplicate applications after re-reconcile", func() {
			fakeArgoCDProvider.Mu.Lock()
			callsBefore := fakeArgoCDProvider.EnsureCalls
			fakeArgoCDProvider.Mu.Unlock()

			// Trigger re-reconcile via spec update
			var t loopruntimev1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			Expect(operatortest.UpdateWithRetry(ctx, k8sClient, &t, func() {
				t.Spec.DisplayName = "ArgoCD idempotency"
			})).To(Succeed())

			// Wait for reconcile to complete
			Eventually(func(g Gomega) {
				var updated loopruntimev1.Tenant
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
		deleteTenantNamespaces(tenantName, "production", "staging")
		deleteTenantWithFinalizer(tenantName)
	})
})

var _ = Describe("TenantController — ArgoCD Deletion Cleanup", Ordered, func() {
	const tenantName = "argocd-del"

	BeforeAll(func() {
		tenant := newTenant(tenantName, loopruntimev1.PlanStarter, []string{"production"})
		Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

		// Wait for full reconciliation
		Eventually(func(g Gomega) {
			var t loopruntimev1.Tenant
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			g.Expect(t.Status.Phase).To(Equal(loopruntimev1.TenantPhaseActive))
		}, timeout, interval).Should(Succeed())
	})

	It("should call DeleteApplications on tenant deletion", func() {
		// Record delete calls before deletion
		fakeArgoCDProvider.Mu.Lock()
		deleteCallsBefore := fakeArgoCDProvider.DeleteCalls
		fakeArgoCDProvider.Mu.Unlock()

		// Delete the tenant
		var t loopruntimev1.Tenant
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: tenantName, Namespace: "default",
		}, &t)).To(Succeed())
		Expect(k8sClient.Delete(ctx, &t)).To(Succeed())

		// Wait for Phase=Deleting
		Eventually(func(g Gomega) {
			var d loopruntimev1.Tenant
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &d)).To(Succeed())
			g.Expect(d.Status.Phase).To(Equal(loopruntimev1.TenantPhaseDeleting))
		}, timeout, interval).Should(Succeed())

		// Clean up child Environment CRs first (tenant waits for them before calling DeleteApplications)
		drainTenantEnvironments(tenantName)

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
			"runtime.getloopscale.com/tenant": tenantName,
		})).To(Succeed())
		for i := range nsList.Items {
			ns := &nsList.Items[i]
			Expect(operatortest.UpdateWithRetry(ctx, k8sClient, ns, func() {
				ns.Finalizers = nil
				delete(ns.Labels, "runtime.getloopscale.com/tenant")
			})).To(Succeed())
		}

		// Wait for tenant to be fully deleted
		expectTenantDeleted(tenantName)
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
		tenant := newTenant(tenantName, loopruntimev1.PlanStarter, []string{"production", "staging", "dev"})
		Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

		// Wait for controller to reconcile and create all 3 Environment CRs
		Eventually(func(g Gomega) {
			for _, env := range []string{"production", "staging", "dev"} {
				var envCR loopruntimev1.Environment
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName + "-default-project-" + env, Namespace: "default",
				}, &envCR)).To(Succeed())
			}
		}, timeout, interval).Should(Succeed())
	})

	AfterAll(func() {
		cleanupTenantEnvCRs(tenantName)
		var t loopruntimev1.Tenant
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: tenantName, Namespace: "default"}, &t); err == nil {
			_ = operatortest.UpdateWithRetry(ctx, k8sClient, &t, func() {
				t.Finalizers = nil
			})
			_ = k8sClient.Delete(ctx, &t)
		}
	})

	Context("when an environment is removed from spec.environments[]", func() {
		It("should garbage-collect the orphaned Environment CR", func() {
			// Arrange: verify all 3 Environment CRs exist
			var envList loopruntimev1.EnvironmentList
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
			// Retry on conflict — the reconciler may update the Tenant status
			// between our Get and Update, causing a 409 Conflict.
			Eventually(func(g Gomega) {
				var t loopruntimev1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				t.Spec.Environments = []loopruntimev1.TenantEnvironmentEntry{
					{Name: "production", Cluster: "workload", Project: "default-project"},
					{Name: "staging", Cluster: "workload", Project: "default-project"},
				}
				g.Expect(k8sClient.Update(ctx, &t)).To(Succeed())
			}, "10s", "500ms").Should(Succeed())

			// Wait for TenantController to issue Delete on the orphan Environment CR.
			// Accept two outcomes: CR exists with DeletionTimestamp (finalizer holding it),
			// or CR already fully deleted (no finalizer / envtest race).
			var devEnvStillExists bool
			Eventually(func(g Gomega) {
				var devEnv loopruntimev1.Environment
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName + "-dev", Namespace: "default",
				}, &devEnv)
				if errors.IsNotFound(err) {
					// Already fully deleted — GC succeeded
					devEnvStillExists = false
					return
				}
				g.Expect(err).NotTo(HaveOccurred())
				// DeletionTimestamp should be set (tenant controller issued Delete)
				g.Expect(devEnv.DeletionTimestamp.IsZero()).To(BeFalse(),
					"orphan Environment CR should be marked for deletion")
				devEnvStillExists = true
			}, timeout, interval).Should(Succeed())

			// Simulate EnvironmentController completing namespace cleanup
			// (only needed if the CR still exists with a finalizer).
			if devEnvStillExists {
				Eventually(func(g Gomega) {
					var devEnv loopruntimev1.Environment
					err := k8sClient.Get(ctx, types.NamespacedName{
						Name: tenantName + "-dev", Namespace: "default",
					}, &devEnv)
					if errors.IsNotFound(err) {
						return // Already gone
					}
					g.Expect(err).NotTo(HaveOccurred())
					var ns corev1.Namespace
					if err := k8sClient.Get(ctx, types.NamespacedName{Name: devEnv.Spec.Namespace.Name}, &ns); err == nil {
						ns.Finalizers = nil
						delete(ns.Labels, "runtime.getloopscale.com/tenant")
						g.Expect(k8sClient.Update(ctx, &ns)).To(Succeed())
					}
					devEnv.Finalizers = nil
					g.Expect(k8sClient.Update(ctx, &devEnv)).To(Succeed())
				}, timeout, interval).Should(Succeed())
			}

			// Assert: the "dev" Environment CR should be fully deleted
			Eventually(func(g Gomega) {
				var devEnv loopruntimev1.Environment
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName + "-dev", Namespace: "default",
				}, &devEnv)
				g.Expect(err).To(HaveOccurred())
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			// The other 2 Environment CRs should still exist
			for _, env := range []string{"production", "staging"} {
				var envCR loopruntimev1.Environment
				Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName + "-default-project-" + env, Namespace: "default",
				}, &envCR)).To(Succeed())
			}
		})

		It("should not delete Environment CRs owned by a different tenant", func() {
			// Arrange: create a second tenant with an env that has a similar name pattern
			otherTenant := newTenant("gc-env-other", loopruntimev1.PlanStarter, []string{"staging"})
			Expect(k8sClient.Create(ctx, otherTenant)).To(Succeed())

			// Wait for other tenant's Environment CR to exist
			Eventually(func(g Gomega) {
				var envCR loopruntimev1.Environment
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "gc-env-other-default-project-staging", Namespace: "default",
				}, &envCR)).To(Succeed())
			}, timeout, interval).Should(Succeed())

			// Act: remove "staging" from the FIRST tenant. The controllers write
			// this Tenant concurrently (status patches, the Environment delete
			// pruning its tuple), so the update retries on conflict.
			t := loopruntimev1.Tenant{ObjectMeta: metav1.ObjectMeta{Name: tenantName, Namespace: "default"}}
			Expect(operatortest.UpdateWithRetry(ctx, k8sClient, &t, func() {
				t.Spec.Environments = []loopruntimev1.TenantEnvironmentEntry{
					{Name: "production", Cluster: "workload", Project: "default-project"},
				}
			})).To(Succeed())

			// Wait for TenantController to mark the staging CR for deletion
			Eventually(func(g Gomega) {
				var envCR loopruntimev1.Environment
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName + "-default-project-staging", Namespace: "default",
				}, &envCR)).To(Succeed())
				g.Expect(envCR.DeletionTimestamp.IsZero()).To(BeFalse())
			}, timeout, interval).Should(Succeed())

			// Simulate EnvironmentController completing cleanup
			Eventually(func(g Gomega) {
				var envCR loopruntimev1.Environment
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName + "-default-project-staging", Namespace: "default",
				}, &envCR)).To(Succeed())
				var ns corev1.Namespace
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: envCR.Spec.Namespace.Name}, &ns); err == nil {
					ns.Finalizers = nil
					delete(ns.Labels, "runtime.getloopscale.com/tenant")
					g.Expect(k8sClient.Update(ctx, &ns)).To(Succeed())
				}
				envCR.Finalizers = nil
				g.Expect(k8sClient.Update(ctx, &envCR)).To(Succeed())
			}, timeout, interval).Should(Succeed())

			// Assert: first tenant's staging CR should be fully deleted
			Eventually(func(g Gomega) {
				var envCR loopruntimev1.Environment
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName + "-default-project-staging", Namespace: "default",
				}, &envCR)
				g.Expect(err).To(HaveOccurred())
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			// Assert: other tenant's staging CR must still exist
			var otherEnv loopruntimev1.Environment
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "gc-env-other-default-project-staging", Namespace: "default",
			}, &otherEnv)).To(Succeed())
			Expect(otherEnv.OwnerReferences[0].Name).To(Equal("gc-env-other"))

			// Cleanup
			cleanupTenantEnvCRs("gc-env-other")
			var ot loopruntimev1.Tenant
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "gc-env-other", Namespace: "default"}, &ot); err == nil {
				_ = operatortest.UpdateWithRetry(ctx, k8sClient, &ot, func() {
					ot.Finalizers = nil
				})
				_ = k8sClient.Delete(ctx, &ot)
			}
		})
	})
})
