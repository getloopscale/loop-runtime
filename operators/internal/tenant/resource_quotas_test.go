// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package portable_tests — Resource Quota golden tests.
//
// Source mapping:
//   - Capsule: e2e/resourcequota_test.go
//   - Behavioral contracts: Capsule BC-002 (quota enforcement) + BC-004 (drift correction)
//   - Translated to: LoopZero TenantController (build namespace quotas) and
//     EnvironmentController (runtime namespace quotas and LimitRanges).
//
// Key behavioral differences from Capsule:
//   - Capsule propagates quotas from a single GlobalTenantResource to all tenant namespaces.
//   - LoopZero has TWO quota contexts: build namespace (build-quota) and runtime namespace (tenant-quota).
//   - Build quota uses fixed platform constants (not configurable per tenant).
//   - Runtime quota reflects Tenant CR spec.limits values.
//   - Both controllers use CreateOrUpdate — drift is corrected on the next reconcile.
package tenant_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
)

// expectLimitRangeField fetches the tenant-limits LimitRange and asserts
// that a specific field on the (single) container limit matches the expected
// value. Phase 3 helper: collapses 4 near-identical It() blocks per limit
// type into table-driven entries (default-CPU/default-mem/request-CPU/request-mem).
func expectLimitRangeField(tenantName string, fieldGetter func(corev1.LimitRangeItem) string, want string) {
	ns := fmt.Sprintf("loopzero-%s-production", tenantName)
	Eventually(func(g Gomega) {
		var lr corev1.LimitRange
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: "tenant-limits", Namespace: ns,
		}, &lr)).To(Succeed())
		g.Expect(fieldGetter(lr.Spec.Limits[0])).To(Equal(want))
	}, timeout, interval).Should(Succeed())
}

var _ = Describe("Golden: Resource Quotas", Ordered, func() {

	// --- GOLDEN TEST: golden_resource_quotas_build_quota_enforced ---
	// Source: src_resource_quotas_limitrange_and_quota_replicated_to_namespaces
	// BC: Capsule BC-002 → LoopZero build-quota ResourceQuota in build namespace

	Context("golden_resource_quotas_build_quota_enforced", func() {
		const tenantName = "golden-quota-build"

		BeforeAll(func() {
			tenant := newGoldenTenant(tenantName, loopzerov1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			Eventually(func(g Gomega) {
				var t loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Status.Phase).ToNot(BeEmpty())
			}, timeout, interval).Should(Succeed())
		})

		It("should create a ResourceQuota named build-quota in the build namespace", func() {
			buildNs := "build-" + tenantName
			Eventually(func(g Gomega) {
				var quota corev1.ResourceQuota
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "build-quota", Namespace: buildNs,
				}, &quota)).To(Succeed())
			}, timeout, interval).Should(Succeed())
		})

		It("should set requests.cpu=4 in build-quota", func() {
			buildNs := "build-" + tenantName
			Eventually(func(g Gomega) {
				var quota corev1.ResourceQuota
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "build-quota", Namespace: buildNs,
				}, &quota)).To(Succeed())
				val := quota.Spec.Hard[corev1.ResourceRequestsCPU]
				g.Expect(val.String()).To(Equal("4"),
					"build quota requests.cpu must be 4 (platform constant)")
			}, timeout, interval).Should(Succeed())
		})

		It("should set requests.memory=8Gi in build-quota", func() {
			buildNs := "build-" + tenantName
			Eventually(func(g Gomega) {
				var quota corev1.ResourceQuota
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "build-quota", Namespace: buildNs,
				}, &quota)).To(Succeed())
				val := quota.Spec.Hard[corev1.ResourceRequestsMemory]
				g.Expect(val.String()).To(Equal("8Gi"),
					"build quota requests.memory must be 8Gi (platform constant)")
			}, timeout, interval).Should(Succeed())
		})

		It("should set limits.cpu=8 in build-quota", func() {
			buildNs := "build-" + tenantName
			Eventually(func(g Gomega) {
				var quota corev1.ResourceQuota
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "build-quota", Namespace: buildNs,
				}, &quota)).To(Succeed())
				val := quota.Spec.Hard[corev1.ResourceLimitsCPU]
				g.Expect(val.String()).To(Equal("8"),
					"build quota limits.cpu must be 8 (platform constant)")
			}, timeout, interval).Should(Succeed())
		})

		It("should set limits.memory=16Gi in build-quota", func() {
			buildNs := "build-" + tenantName
			Eventually(func(g Gomega) {
				var quota corev1.ResourceQuota
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "build-quota", Namespace: buildNs,
				}, &quota)).To(Succeed())
				val := quota.Spec.Hard[corev1.ResourceLimitsMemory]
				g.Expect(val.String()).To(Equal("16Gi"),
					"build quota limits.memory must be 16Gi (platform constant)")
			}, timeout, interval).Should(Succeed())
		})

		It("should set count/workflows.argoproj.io equal to MaxBuildConcurrency when configured", func() {
			// newGoldenTenant sets MaxBuildConcurrency=2, so the quota must reflect that.
			buildNs := "build-" + tenantName
			Eventually(func(g Gomega) {
				var quota corev1.ResourceQuota
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "build-quota", Namespace: buildNs,
				}, &quota)).To(Succeed())
				val := quota.Spec.Hard[corev1.ResourceName("count/workflows.argoproj.io")]
				g.Expect(val.String()).To(Equal("2"),
					"workflow count limit must match spec.limits.maxBuildConcurrency")
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			cleanupGoldenTenant(tenantName, []string{"production"})
		})
	})

	// --- GOLDEN TEST: golden_resource_quotas_runtime_quota_from_tenant_limits ---
	// Source: src_resource_quotas_limitrange_and_quota_replicated_to_namespaces
	// BC: Capsule BC-002 → LoopZero tenant-quota ResourceQuota in runtime namespace

	Context("golden_resource_quotas_runtime_quota_from_tenant_limits", func() {
		const tenantName = "golden-quota-rt"

		BeforeAll(func() {
			// Create tenant with explicit limits — these must be reflected in the quota.
			environments := []loopzerov1.TenantEnvironmentEntry{
				{Name: "production", Cluster: "workload"},
			}
			tenant := &loopzerov1.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tenantName,
					Namespace: "default",
				},
				Spec: loopzerov1.TenantSpec{
					DisplayName:  "Golden Quota Runtime",
					Plan:         loopzerov1.PlanStarter,
					Environments: environments,
					GitOps: loopzerov1.GitOpsSpec{
						RepoName: "loopzero-tenant-" + tenantName,
						Branch:   "main",
					},
					Preview: loopzerov1.PreviewSpec{MaxEnvironments: 2, TTLHours: 24},
					Limits: loopzerov1.ResourceLimits{
						MaxCPU:              resource.MustParse("8"),
						MaxMemory:           resource.MustParse("16Gi"),
						MaxStorage:          resource.MustParse("50Gi"),
						MaxBuildConcurrency: 2,
					},
				},
			}
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			Eventually(func(g Gomega) {
				var t loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Status.Phase).ToNot(BeEmpty())
			}, timeout, interval).Should(Succeed())
		})

		It("should create a ResourceQuota named tenant-quota in the runtime namespace", func() {
			ns := fmt.Sprintf("loopzero-%s-production", tenantName)
			Eventually(func(g Gomega) {
				var quota corev1.ResourceQuota
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "tenant-quota", Namespace: ns,
				}, &quota)).To(Succeed())
			}, timeout, interval).Should(Succeed())
		})

		It("should reflect spec.limits.maxCPU in requests.cpu of tenant-quota", func() {
			ns := fmt.Sprintf("loopzero-%s-production", tenantName)
			Eventually(func(g Gomega) {
				var quota corev1.ResourceQuota
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "tenant-quota", Namespace: ns,
				}, &quota)).To(Succeed())
				val := quota.Spec.Hard[corev1.ResourceRequestsCPU]
				g.Expect(val.String()).To(Equal("8"),
					"tenant-quota requests.cpu must match spec.limits.maxCPU")
			}, timeout, interval).Should(Succeed())
		})

		It("should reflect spec.limits.maxMemory in requests.memory of tenant-quota", func() {
			ns := fmt.Sprintf("loopzero-%s-production", tenantName)
			Eventually(func(g Gomega) {
				var quota corev1.ResourceQuota
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "tenant-quota", Namespace: ns,
				}, &quota)).To(Succeed())
				val := quota.Spec.Hard[corev1.ResourceRequestsMemory]
				g.Expect(val.String()).To(Equal("16Gi"),
					"tenant-quota requests.memory must match spec.limits.maxMemory")
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			cleanupGoldenTenant(tenantName, []string{"production"})
		})
	})

	// --- GOLDEN TEST: golden_resource_quotas_limitrange_default_container_limits ---
	// Source: src_resource_quotas_limitrange_and_quota_replicated_to_namespaces
	// BC: Capsule BC-002 → LoopZero tenant-limits LimitRange in runtime namespace

	Context("golden_resource_quotas_limitrange_default_container_limits", func() {
		const tenantName = "golden-lr-rt"

		BeforeAll(func() {
			tenant := newGoldenTenant(tenantName, loopzerov1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			Eventually(func(g Gomega) {
				var t loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Status.Phase).ToNot(BeEmpty())
			}, timeout, interval).Should(Succeed())
		})

		It("should create a LimitRange named tenant-limits in the runtime namespace", func() {
			ns := fmt.Sprintf("loopzero-%s-production", tenantName)
			Eventually(func(g Gomega) {
				var lr corev1.LimitRange
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "tenant-limits", Namespace: ns,
				}, &lr)).To(Succeed())
			}, timeout, interval).Should(Succeed())
		})

		It("should set Container type in tenant-limits LimitRange", func() {
			ns := fmt.Sprintf("loopzero-%s-production", tenantName)
			Eventually(func(g Gomega) {
				var lr corev1.LimitRange
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "tenant-limits", Namespace: ns,
				}, &lr)).To(Succeed())
				g.Expect(lr.Spec.Limits).To(HaveLen(1))
				g.Expect(lr.Spec.Limits[0].Type).To(Equal(corev1.LimitTypeContainer))
			}, timeout, interval).Should(Succeed())
		})

		It("should set default CPU limit=500m in tenant-limits", func() {
			expectLimitRangeField(tenantName, func(li corev1.LimitRangeItem) string { return li.Default.Cpu().String() }, "500m")
		})

		It("should set default memory limit=512Mi in tenant-limits", func() {
			expectLimitRangeField(tenantName, func(li corev1.LimitRangeItem) string { return li.Default.Memory().String() }, "512Mi")
		})

		It("should set default CPU request=100m in tenant-limits", func() {
			expectLimitRangeField(tenantName, func(li corev1.LimitRangeItem) string { return li.DefaultRequest.Cpu().String() }, "100m")
		})

		It("should set default memory request=128Mi in tenant-limits", func() {
			expectLimitRangeField(tenantName, func(li corev1.LimitRangeItem) string { return li.DefaultRequest.Memory().String() }, "128Mi")
		})

		AfterAll(func() {
			cleanupGoldenTenant(tenantName, []string{"production"})
		})
	})

	// --- GOLDEN TEST: golden_resource_quotas_restored_after_manual_deletion ---
	// Source: src_resource_quotas_resourcequota_restored_after_modification
	// BC: Capsule BC-004 (drift correction) → LoopZero CreateOrUpdate restores deleted quotas

	Context("golden_resource_quotas_restored_after_manual_deletion", func() {
		const tenantName = "golden-quota-restore"

		BeforeAll(func() {
			tenant := newGoldenTenant(tenantName, loopzerov1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			// Wait for quota to exist before deleting it.
			ns := fmt.Sprintf("loopzero-%s-production", tenantName)
			Eventually(func(g Gomega) {
				var quota corev1.ResourceQuota
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "tenant-quota", Namespace: ns,
				}, &quota)).To(Succeed())
			}, timeout, interval).Should(Succeed())
		})

		It("should recreate tenant-quota after it is manually deleted", func() {
			ns := fmt.Sprintf("loopzero-%s-production", tenantName)

			// Simulate infrastructure drift: manually delete the ResourceQuota.
			var quota corev1.ResourceQuota
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "tenant-quota", Namespace: ns,
			}, &quota)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &quota)).To(Succeed())

			// Trigger re-reconcile by updating the Tenant CR.
			var t loopzerov1.Tenant
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: tenantName, Namespace: "default",
			}, &t)).To(Succeed())
			t.Spec.DisplayName = "Quota Restore Trigger"
			Expect(k8sClient.Update(ctx, &t)).To(Succeed())

			// Quota must be re-created by the next reconcile loop.
			Eventually(func(g Gomega) {
				var restored corev1.ResourceQuota
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "tenant-quota", Namespace: ns,
				}, &restored)).To(Succeed(),
					"tenant-quota must be automatically restored after manual deletion (drift correction)")
			}, timeout, interval).Should(Succeed())
		})

		It("should restore the correct CPU and memory values after drift correction", func() {
			ns := fmt.Sprintf("loopzero-%s-production", tenantName)
			Eventually(func(g Gomega) {
				var restored corev1.ResourceQuota
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "tenant-quota", Namespace: ns,
				}, &restored)).To(Succeed())
				cpuVal := restored.Spec.Hard[corev1.ResourceRequestsCPU]
				g.Expect(cpuVal.String()).To(Equal("8"),
					"restored quota must have the original spec.limits.maxCPU value")
				memVal := restored.Spec.Hard[corev1.ResourceRequestsMemory]
				g.Expect(memVal.String()).To(Equal("16Gi"),
					"restored quota must have the original spec.limits.maxMemory value")
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			cleanupGoldenTenant(tenantName, []string{"production"})
		})
	})

	// --- GOLDEN TEST: golden_resource_quotas_build_limitrange_created ---
	// Source: src_resource_quotas_limitrange_and_quota_replicated_to_namespaces
	// BC: Capsule BC-002 → LoopZero build-limits LimitRange in build namespace

	Context("golden_resource_quotas_build_limitrange_created", func() {
		const tenantName = "golden-lr-build"

		BeforeAll(func() {
			tenant := newGoldenTenant(tenantName, loopzerov1.PlanStarter, []string{"production"})
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())

			Eventually(func(g Gomega) {
				var t loopzerov1.Tenant
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: tenantName, Namespace: "default",
				}, &t)).To(Succeed())
				g.Expect(t.Status.Phase).ToNot(BeEmpty())
			}, timeout, interval).Should(Succeed())
		})

		It("should create a LimitRange named build-limits in the build namespace", func() {
			buildNs := "build-" + tenantName
			Eventually(func(g Gomega) {
				var lr corev1.LimitRange
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "build-limits", Namespace: buildNs,
				}, &lr)).To(Succeed())
			}, timeout, interval).Should(Succeed())
		})

		It("should set Container type in build-limits LimitRange", func() {
			buildNs := "build-" + tenantName
			Eventually(func(g Gomega) {
				var lr corev1.LimitRange
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "build-limits", Namespace: buildNs,
				}, &lr)).To(Succeed())
				g.Expect(lr.Spec.Limits).To(HaveLen(1))
				g.Expect(lr.Spec.Limits[0].Type).To(Equal(corev1.LimitTypeContainer),
					"build LimitRange must apply to containers so Argo Workflow pods get safe defaults")
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			cleanupGoldenTenant(tenantName, []string{"production"})
		})
	})
})
