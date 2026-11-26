// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package portable_tests contains golden behavioral tests derived from Capsule
// (https://projectcapsule.dev) and normalized for the LoopRuntime PaaS operator suite.
//
// These tests are PORTABLE — they test observable behavior (namespace labels,
// RBAC resources, conditions, phase transitions) and make no assumptions about
// internal implementation details.
//
// To use: copy this file into operators/internal/controller/ and run:
//
//	go test ./internal/controller/ -v -count=1 -run TestControllers -ginkgo.focus="Golden:"
//
// Source mapping:
//   - Capsule: e2e/new_namespace_test.go, e2e/tenant_resources_test.go
//   - Behavioral contract: Capsule BC-001 (namespace + RoleBinding creation)
//   - Translated to: LoopRuntime TenantController + EnvironmentController behaviors
package tenant_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/operatortest"
	"github.com/getloopscale/loop-runtime/pkg/naming"
)

// newGoldenTenant creates a minimal Tenant CR for golden test use.
// Uses the "golden-" prefix to distinguish from existing test tenants.
// Quality-Gate Phase 3: delegate to newTenant + apply Golden-specific overrides
// (was 30-line near-clone of newTenant). Golden vs Test only differ in display
// name + tighter preview policy (2 envs / 24h instead of 3 envs / 48h).
func newGoldenTenant(name string, plan loopruntimev1.PlanTier, envs []string) *loopruntimev1.Tenant {
	t := newTenant(name, plan, envs)
	t.Spec.DisplayName = "Golden Test Tenant " + name
	t.Spec.Preview.MaxEnvironments = 2
	t.Spec.Preview.TTLHours = 24
	return t
}

// cleanupGoldenTenant removes finalizers and deletes the Tenant CR and its namespaces.
// Must be called in AfterAll to avoid envtest state leaking between suites.
func cleanupGoldenTenant(tenantName string, envNames []string) {
	// Remove finalizers so the Tenant can be garbage-collected.
	t := &loopruntimev1.Tenant{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: tenantName, Namespace: "default"}, t); err == nil {
		_ = operatortest.UpdateWithRetry(ctx, k8sClient, t, func() {
			t.Finalizers = nil
		})
		_ = k8sClient.Delete(ctx, t)
	}

	// Clean up Environment CRs.
	for _, env := range envNames {
		envCR := &loopruntimev1.Environment{}
		envKey := types.NamespacedName{Name: tenantName + "-default-project-" + env, Namespace: "default"}
		if err := k8sClient.Get(ctx, envKey, envCR); err == nil {
			_ = operatortest.UpdateWithRetry(ctx, k8sClient, envCR, func() {
				envCR.Finalizers = nil
			})
			_ = k8sClient.Delete(ctx, envCR)
		}
	}

	// Delete all namespaces with this tenant label.
	var nsList corev1.NamespaceList
	if err := k8sClient.List(ctx, &nsList, client.MatchingLabels{"runtime.getloopscale.com/tenant": tenantName}); err == nil {
		for i := range nsList.Items {
			_ = k8sClient.Delete(ctx, &nsList.Items[i])
		}
	}
}

// --- GOLDEN TEST: golden_tenant_isolation_build_namespace_created ---
// Source: src_tenant_isolation_namespace_created_with_rolebindings
// BC: Capsule BC-001 (namespace + owner binding) → LoopRuntime build namespace provisioning

var _ = Describe("Golden: Tenant Isolation", Ordered, func() {

	Context("golden_tenant_isolation_build_namespace_created", func() {
		const tenantName = "golden-iso-build"

		BeforeAll(func() {
			createGoldenTenantAndWaitPhaseSet(tenantName, loopruntimev1.PlanStarter, []string{"production"})
		})

		It("should create build-{tenantName} namespace on the build cluster", func() {
			buildNs := "build-" + tenantName
			Eventually(func(g Gomega) {
				var ns corev1.Namespace
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: buildNs}, &ns)).To(Succeed())
			}, timeout, interval).Should(Succeed())
		})

		It("should set runtime.getloopscale.com/tenant label on the build namespace", func() {
			expectNamespaceLabel("build-"+tenantName, "runtime.getloopscale.com/tenant", tenantName)
		})

		It("should set runtime.getloopscale.com/component=build label on the build namespace", func() {
			expectNamespaceLabel("build-"+tenantName, "runtime.getloopscale.com/component", "build")
		})

		It("should set runtime.getloopscale.com/managed-by=tenant-controller label on the build namespace", func() {
			expectNamespaceLabel("build-"+tenantName, "runtime.getloopscale.com/managed-by", "tenant-controller")
		})

		It("should NOT set ownerReference on build namespace (cross-cluster limitation)", func() {
			// Normalizer note: Capsule used ownerRef for cleanup. LoopRuntime cannot — the build
			// namespace lives on a different physical cluster. Label-based cleanup + finalizer
			// pattern is used instead. This is a behavioral difference, not a missing feature.
			buildNs := "build-" + tenantName
			Eventually(func(g Gomega) {
				var ns corev1.Namespace
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: buildNs}, &ns)).To(Succeed())
				g.Expect(ns.OwnerReferences).To(BeEmpty())
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			cleanupGoldenTenant(tenantName, []string{"production"})
		})
	})

	// --- GOLDEN TEST: golden_tenant_isolation_runtime_namespace_labels ---
	// Source: src_tenant_isolation_namespace_created_with_rolebindings
	// BC: Capsule BC-001 → LoopRuntime runtime namespace per-environment provisioning

	Context("golden_tenant_isolation_runtime_namespace_labels", func() {
		const tenantName = "golden-iso-rt"

		BeforeAll(func() {
			createGoldenTenantAndWaitPhaseSet(tenantName, loopruntimev1.PlanStarter, []string{"production", "staging"})
		})

		It("should create loopruntime-{tenant}-{env} namespace for each declared environment", func() {
			for _, env := range []string{"production", "staging"} {
				nsName := fmt.Sprintf("loopruntime-%s-default-project-%s", tenantName, env)
				Eventually(func(g Gomega) {
					var ns corev1.Namespace
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, &ns)).To(Succeed())
				}, timeout, interval).Should(Succeed())
			}
		})

		It("should set runtime.getloopscale.com/tenant label on every runtime namespace", func() {
			for _, env := range []string{"production", "staging"} {
				expectNamespaceLabel(fmt.Sprintf("loopruntime-%s-default-project-%s", tenantName, env), "runtime.getloopscale.com/tenant", tenantName)
			}
		})

		It("should set runtime.getloopscale.com/environment label on every runtime namespace", func() {
			for _, env := range []string{"production", "staging"} {
				expectNamespaceLabel(fmt.Sprintf("loopruntime-%s-default-project-%s", tenantName, env), "runtime.getloopscale.com/environment", env)
			}
		})

		It("should set runtime.getloopscale.com/managed-by=environment-controller on every runtime namespace", func() {
			for _, env := range []string{"production", "staging"} {
				nsName := fmt.Sprintf("loopruntime-%s-default-project-%s", tenantName, env)
				Eventually(func(g Gomega) {
					var ns corev1.Namespace
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, &ns)).To(Succeed())
					g.Expect(ns.Labels).To(HaveKeyWithValue("runtime.getloopscale.com/managed-by", "environment-controller"))
				}, timeout, interval).Should(Succeed())
			}
		})

		AfterAll(func() {
			cleanupGoldenTenant(tenantName, []string{"production", "staging"})
		})
	})

	// --- GOLDEN TEST: golden_tenant_isolation_cross_tenant_namespace_no_shared_labels ---
	// Source: src_tenant_isolation_non_owner_cannot_select_foreign_tenant
	// BC: Capsule BC-001 → LoopRuntime label-based tenant isolation (each namespace belongs to exactly one tenant)

	Context("golden_tenant_isolation_cross_tenant_namespace_no_shared_labels", func() {
		const (
			tenantA = "golden-cross-a"
			tenantB = "golden-cross-b"
		)

		BeforeAll(func() {
			for _, name := range []string{tenantA, tenantB} {
				tenant := newGoldenTenant(name, loopruntimev1.PlanStarter, []string{"production"})
				Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
			}
			// Wait for both tenants to have their phase set.
			for _, name := range []string{tenantA, tenantB} {
				Eventually(func(g Gomega) {
					var t loopruntimev1.Tenant
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name: name, Namespace: "default",
					}, &t)).To(Succeed())
					g.Expect(t.Status.Phase).ToNot(BeEmpty())
				}, timeout, interval).Should(Succeed())
			}
		})

		It("should give each tenant a distinct namespace that the other cannot claim", func() {
			nsA := fmt.Sprintf("loopruntime-%s-default-project-production", tenantA)
			nsB := fmt.Sprintf("loopruntime-%s-default-project-production", tenantB)

			Eventually(func(g Gomega) {
				var a, b corev1.Namespace
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsA}, &a)).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsB}, &b)).To(Succeed())

				g.Expect(a.Labels).To(HaveKeyWithValue("runtime.getloopscale.com/tenant", tenantA))
				g.Expect(a.Labels).NotTo(HaveKeyWithValue("runtime.getloopscale.com/tenant", tenantB))

				g.Expect(b.Labels).To(HaveKeyWithValue("runtime.getloopscale.com/tenant", tenantB))
				g.Expect(b.Labels).NotTo(HaveKeyWithValue("runtime.getloopscale.com/tenant", tenantA))

				g.Expect(a.Name).NotTo(Equal(b.Name))
			}, timeout, interval).Should(Succeed())
		})

		It("should list only its own namespaces when querying by runtime.getloopscale.com/tenant label", func() {
			for _, name := range []string{tenantA, tenantB} {
				Eventually(func(g Gomega) {
					var nsList corev1.NamespaceList
					g.Expect(k8sClient.List(ctx, &nsList, client.MatchingLabels{
						"runtime.getloopscale.com/tenant": name,
					})).To(Succeed())
					for _, ns := range nsList.Items {
						g.Expect(ns.Labels["runtime.getloopscale.com/tenant"]).To(Equal(name),
							"namespace %s should belong exclusively to tenant %s", ns.Name, name)
					}
				}, timeout, interval).Should(Succeed())
			}
		})

		AfterAll(func() {
			cleanupGoldenTenant(tenantA, []string{"production"})
			cleanupGoldenTenant(tenantB, []string{"production"})
		})
	})

	// --- GOLDEN TEST: golden_tenant_isolation_tenant_reaches_active_phase ---
	// Source: src_tenant_isolation_namespace_lifecycle_status_tracking
	// BC: Capsule BC-001 → LoopRuntime TenantController state machine (Provisioning → Active)

	Context("golden_tenant_isolation_tenant_reaches_active_phase", func() {
		const tenantName = "golden-phase-active"

		BeforeAll(func() {
			tenant := newGoldenTenant(tenantName, loopruntimev1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
		})

		It("should transition to Active phase after provisioning completes", func() {
			Eventually(func(g Gomega) {
				var t loopruntimev1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Status.Phase).To(Equal(loopruntimev1.TenantPhaseActive))
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			cleanupGoldenTenant(tenantName, []string{"production"})
		})
	})

	// --- GOLDEN TEST: golden_tenant_isolation_finalizer_prevents_premature_deletion ---
	// Source: src_tenant_isolation_namespace_lifecycle_status_tracking
	// BC: Capsule BC-001 → LoopRuntime finalizer-based cleanup contract

	Context("golden_tenant_isolation_finalizer_prevents_premature_deletion", func() {
		const tenantName = "golden-finalizer"

		BeforeAll(func() {
			tenant := newGoldenTenant(tenantName, loopruntimev1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
		})

		It("should add the runtime.getloopscale.com/tenant-cleanup finalizer after reconciliation", func() {
			Eventually(func(g Gomega) {
				var t loopruntimev1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Finalizers).To(ContainElement("runtime.getloopscale.com/tenant-cleanup"))
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			cleanupGoldenTenant(tenantName, []string{"production"})
		})
	})

	// --- GOLDEN TEST: golden_tenant_isolation_idempotent_reconciliation ---
	// Source: src_tenant_isolation_additional_rolebindings_propagated
	// BC: Capsule BC-001 (reconcile loop idempotency) → LoopRuntime CreateOrUpdate semantics

	Context("golden_tenant_isolation_idempotent_reconciliation", func() {
		const tenantName = "golden-idempotent"

		BeforeAll(func() {
			createGoldenTenantAndWaitActive(tenantName, loopruntimev1.PlanStarter, []string{"production", "staging"})
		})

		It("should not duplicate namespaces after a spec update triggers re-reconcile", func() {
			// Trigger re-reconcile by updating a non-critical field.
			var t loopruntimev1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			Expect(operatortest.UpdateWithRetry(ctx, k8sClient, &t, func() {
				t.Spec.DisplayName = "Golden Idempotency Verified"
			})).To(Succeed())

			// Wait for the update to propagate.
			withTenant(tenantName, func(g Gomega, updated *loopruntimev1.Tenant) {
				g.Expect(updated.Spec.DisplayName).To(Equal("Golden Idempotency Verified"))
			})

			// Exactly 3 namespaces: production + staging + build.
			Eventually(func(g Gomega) {
				var nsList corev1.NamespaceList
				g.Expect(k8sClient.List(ctx, &nsList, client.MatchingLabels{
					"runtime.getloopscale.com/tenant": tenantName,
				})).To(Succeed())
				g.Expect(nsList.Items).To(HaveLen(3),
					"expected exactly 3 namespaces: production, staging, build — no duplicates")
			}, timeout, interval).Should(Succeed())
		})

		It("should not duplicate the loopruntime-deployer ServiceAccount after re-reconcile", func() {
			nsName := fmt.Sprintf("loopruntime-%s-default-project-production", tenantName)
			Eventually(func(g Gomega) {
				var saList corev1.ServiceAccountList
				g.Expect(k8sClient.List(ctx, &saList, client.InNamespace(nsName))).To(Succeed())
				count := 0
				for _, sa := range saList.Items {
					if sa.Name == "loopruntime-deployer" {
						count++
					}
				}
				g.Expect(count).To(Equal(1), "exactly one loopruntime-deployer SA should exist — no duplicates")
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			cleanupGoldenTenant(tenantName, []string{"production", "staging"})
		})
	})

	// --- GOLDEN TEST: golden_tenant_isolation_rolebinding_deployer ---
	// Source: src_tenant_isolation_namespace_created_with_rolebindings
	// BC: Capsule BC-001 (owner RoleBinding) → LoopRuntime loopruntime-deployer RoleBinding in runtime namespace

	Context("golden_tenant_isolation_rolebinding_deployer", func() {
		const tenantName = "golden-rb-test"

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

		It("should create loopruntime-deployer ServiceAccount in the runtime namespace", func() {
			expectServiceAccountExists("loopruntime-deployer", fmt.Sprintf("loopruntime-%s-default-project-production", tenantName))
		})

		It("should create loopruntime-deployer RoleBinding that binds the SA to a Role", func() {
			expectDeployerRoleBinding(fmt.Sprintf("loopruntime-%s-default-project-production", tenantName))
		})

		AfterAll(func() {
			cleanupGoldenTenant(tenantName, []string{"production"})
		})
	})

	// --- GOLDEN TEST: golden_tenant_isolation_env_cr_created_for_each_environment ---
	// Source: src_tenant_isolation_namespace_created_with_rolebindings
	// BC: Capsule BC-001 → LoopRuntime Environment CR as the delegation unit for namespace/RBAC/NP provisioning

	Context("golden_tenant_isolation_env_cr_created_for_each_environment", func() {
		const tenantName = "golden-env-cr"

		BeforeAll(func() {
			createGoldenTenantAndWaitPhaseSet(tenantName, loopruntimev1.PlanStarter, []string{"production", "staging"})
		})

		It("should create an Environment CR for each declared environment", func() {
			for _, env := range []string{"production", "staging"} {
				envCRName, nerr := naming.EnvironmentCRName(tenantName, "default-project", env)
				Expect(nerr).NotTo(HaveOccurred())
				Eventually(func(g Gomega) {
					var envCR loopruntimev1.Environment
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name: envCRName, Namespace: "default",
					}, &envCR)).To(Succeed())
				}, timeout, interval).Should(Succeed())
			}
		})

		It("should set ownerReference on each Environment CR pointing to the Tenant", func() {
			for _, env := range []string{"production", "staging"} {
				envCRName, nerr := naming.EnvironmentCRName(tenantName, "default-project", env)
				Expect(nerr).NotTo(HaveOccurred())
				Eventually(func(g Gomega) {
					var envCR loopruntimev1.Environment
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name: envCRName, Namespace: "default",
					}, &envCR)).To(Succeed())
					g.Expect(envCR.OwnerReferences).To(HaveLen(1))
					g.Expect(envCR.OwnerReferences[0].Kind).To(Equal("Tenant"))
					g.Expect(envCR.OwnerReferences[0].Name).To(Equal(tenantName))
				}, timeout, interval).Should(Succeed())
			}
		})

		It("should set correct tenantRef and namespace.name on each Environment CR", func() {
			for _, env := range []string{"production", "staging"} {
				envCRName, nerr := naming.EnvironmentCRName(tenantName, "default-project", env)
				Expect(nerr).NotTo(HaveOccurred())
				expectedNS := fmt.Sprintf("loopruntime-%s-default-project-%s", tenantName, env)
				Eventually(func(g Gomega) {
					var envCR loopruntimev1.Environment
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{
						Name: envCRName, Namespace: "default",
					}, &envCR)).To(Succeed())
					g.Expect(envCR.Spec.TenantRef.Name).To(Equal(tenantName))
					g.Expect(envCR.Spec.Environment).To(Equal(env))
					g.Expect(envCR.Spec.Namespace.Name).To(Equal(expectedNS))
				}, timeout, interval).Should(Succeed())
			}
		})

		AfterAll(func() {
			cleanupGoldenTenant(tenantName, []string{"production", "staging"})
		})
	})

	// --- GOLDEN TEST: golden_tenant_isolation_empty_environments_rejected ---
	// Source: src_tenant_isolation_missing_tenant_namespace_creation_fails
	// BC: Capsule BC-003 (spec validation) → LoopRuntime CRD kubebuilder:validation:MinItems=1

	Context("golden_tenant_isolation_empty_environments_accepted", func() {
		It("should ACCEPT a Tenant CR with an empty environments list (CRD MinItems: 0 by design)", func() {
			// environments is MinItems=0 BY DESIGN (config/crd/bases/runtime.getloopscale.com_tenants.yaml
			// schema comment: forcing MinItems=1 required factories to pre-populate).
			// An empty list is accepted at admission. (This spec previously asserted a
			// minItems:1 rejection — stale after the deliberate MinItems 1→0 change.)
			tenant := &loopruntimev1.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "golden-empty-envs",
					Namespace: "default",
				},
				Spec: loopruntimev1.TenantSpec{
					DisplayName:  "Golden Empty Envs",
					Plan:         loopruntimev1.PlanStarter,
					Environments: []loopruntimev1.TenantEnvironmentEntry{},
					GitOps: loopruntimev1.GitOpsSpec{
						RepoName: "loopruntime-tenant-golden-empty-envs",
						Branch:   "main",
					},
					Preview: loopruntimev1.PreviewSpec{},
					// Valid Limits (maxBuildConcurrency CRD minimum is 1) so the CR is
					// otherwise-valid and only the empty environments list is under test.
					Limits: loopruntimev1.ResourceLimits{
						MaxCPU:              resource.MustParse("8"),
						MaxMemory:           resource.MustParse("16Gi"),
						MaxStorage:          resource.MustParse("50Gi"),
						MaxBuildConcurrency: 2,
					},
				},
			}
			err := k8sClient.Create(ctx, tenant)
			Expect(err).NotTo(HaveOccurred(), "empty environments list is accepted (CRD MinItems: 0)")
			_ = k8sClient.Delete(ctx, tenant)
		})

		AfterAll(func() {
			// No-op: tenant was never created.
			t := &loopruntimev1.Tenant{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "golden-empty-envs", Namespace: "default"}, t); err == nil {
				if !errors.IsNotFound(err) {
					_ = operatortest.UpdateWithRetry(ctx, k8sClient, t, func() {
						t.Finalizers = nil
					})
					_ = k8sClient.Delete(ctx, t)
				}
			}
		})
	})

	// --- GOLDEN TEST: golden_tenant_isolation_build_executor_rbac ---
	// Source: src_tenant_isolation_namespace_created_with_rolebindings
	// BC: Capsule BC-001 (owner binding) → LoopRuntime argo-workflow-executor RBAC in build namespace

	Context("golden_tenant_isolation_build_executor_rbac", func() {
		const tenantName = "golden-exec-rbac"

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

		It("should create argo-workflow-executor Role in the build namespace", func() {
			buildNs := "build-" + tenantName
			Eventually(func(g Gomega) {
				var role rbacv1.Role
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "argo-workflow-executor", Namespace: buildNs,
				}, &role)).To(Succeed())
				hasWorkflowTaskResults := false
				for _, rule := range role.Rules {
					for _, res := range rule.Resources {
						if res == "workflowtaskresults" {
							hasWorkflowTaskResults = true
						}
					}
				}
				g.Expect(hasWorkflowTaskResults).To(BeTrue(),
					"executor role must include workflowtaskresults permission for Argo")
			}, timeout, interval).Should(Succeed())
		})

		It("should bind the loopruntime-build-sa ServiceAccount to the executor Role", func() {
			buildNs := "build-" + tenantName
			Eventually(func(g Gomega) {
				var rb rbacv1.RoleBinding
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "argo-workflow-executor", Namespace: buildNs,
				}, &rb)).To(Succeed())
				g.Expect(rb.RoleRef.Name).To(Equal("argo-workflow-executor"))
				g.Expect(rb.Subjects).To(HaveLen(1))
				g.Expect(rb.Subjects[0].Name).To(Equal("loopruntime-build-sa"))
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			cleanupGoldenTenant(tenantName, []string{"production"})
		})
	})

	// --- GOLDEN TEST: golden_tenant_isolation_build_submitter_rbac ---
	// Source: src_tenant_isolation_namespace_created_with_rolebindings
	// BC: Capsule BC-001 → LoopRuntime API workflow-submitter RBAC (cross-namespace subject from argo ns)

	Context("golden_tenant_isolation_build_submitter_rbac", func() {
		const tenantName = "golden-sub-rbac"

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

		It("should create loopruntime-workflow-submitter RoleBinding with subject from the argo namespace", func() {
			buildNs := "build-" + tenantName
			Eventually(func(g Gomega) {
				var rb rbacv1.RoleBinding
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "loopruntime-workflow-submitter", Namespace: buildNs,
				}, &rb)).To(Succeed())
				g.Expect(rb.RoleRef.Name).To(Equal("loopruntime-workflow-submitter"))
				g.Expect(rb.Subjects).To(HaveLen(1))
				g.Expect(rb.Subjects[0].Kind).To(Equal("ServiceAccount"))
				g.Expect(rb.Subjects[0].Name).To(Equal("loopruntime-workflow-submitter"))
				g.Expect(rb.Subjects[0].Namespace).To(Equal("argo"),
					"workflow submitter must be in the argo namespace so the API can submit builds")
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			cleanupGoldenTenant(tenantName, []string{"production"})
		})
	})

	// --- GOLDEN TEST: golden_tenant_isolation_build_namespace_condition ---
	// Source: src_resource_quotas_limitrange_and_quota_replicated_to_namespaces
	// BC: Capsule BC-002 (quota condition) → LoopRuntime BuildNamespaceReady + QuotaEnforced conditions

	Context("golden_tenant_isolation_build_namespace_condition", func() {
		const tenantName = "golden-cond-build"

		BeforeAll(func() {
			tenant := newGoldenTenant(tenantName, loopruntimev1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
		})

		It("should set BuildNamespaceReady condition to True on the Tenant CR", func() {
			expectTenantCondition(tenantName, "BuildNamespaceReady", metav1.ConditionTrue)
		})

		It("should set QuotaEnforced condition to True on the Tenant CR", func() {
			expectTenantCondition(tenantName, "QuotaEnforced", metav1.ConditionTrue)
		})

		AfterAll(func() {
			cleanupGoldenTenant(tenantName, []string{"production"})
		})
	})
})
