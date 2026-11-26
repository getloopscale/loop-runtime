// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Ginkgo envtest specs that pin the EnvironmentController's canonical
// namespace contract (plan 2026-06-09-m2-operator-canonical-fix v1.4, T3.1):
//
//   - An Environment CR with Project=sample and Environment=production makes
//     the controller create namespace `loopzero-tn-X-sample-production` (the
//     three-segment canonical form), never the two-segment legacy form.
//   - An Environment CR whose Project and Environment combine into a name
//     longer than 63 characters gets NamespaceReady=False Reason=NameTooLong
//     and is not requeued (which would hammer the API server with invalid
//     Creates).
//
// Each spec creates an Environment CR, waits for the reconcile to settle,
// then asserts on the created Namespace or the status condition.
//
// These specs exercise the Environment controller but live in this package
// because the envtest suite that runs both the Tenant and the Environment
// controllers (the Environment needs its parent Tenant) is this one; the
// environment package has only fake-client tests and no envtest suite.

package tenant_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	loopzerov1 "github.com/getloopscale/loop-runtime/operators/api/v1"
)

// canonicalNamespaceEnvironment constructs an Environment CR in the `default`
// namespace with the (project, env, tenant) tuple under test.
func canonicalNamespaceEnvironment(project, envName, tenantName string) *loopzerov1.Environment {
	return &loopzerov1.Environment{
		ObjectMeta: metav1.ObjectMeta{
			// #273: the Environment CR identity includes the project.
			Name:      tenantName + "-" + project + "-" + envName,
			Namespace: "default",
		},
		Spec: loopzerov1.EnvironmentCRSpec{
			TenantRef:   loopzerov1.TenantRef{Name: tenantName},
			Environment: envName,
			Project:     project,
			// Spec.Namespace.Name is informational post-T3.1: when Project is
			// set the controller computes the canonical name. Pre-populate
			// with a recognizable wrong value so we can prove the controller
			// did NOT use it.
			Namespace:     loopzerov1.NamespaceSpec{Name: "WRONG-PLACEHOLDER-must-be-overridden"},
			NetworkPolicy: loopzerov1.NetworkPolicyConfig{Isolate: true},
			Plan:          loopzerov1.PlanStarter,
			ResourceQuota: &loopzerov1.ResourceQuotaSpec{CPU: "8", Memory: "16Gi", Storage: "50Gi"},
		},
	}
}

var _ = Describe("EnvironmentController canonical namespace and 63-char overflow", Ordered, func() {

	Context("canonical three-segment namespace", func() {
		It("creates namespace `loopzero-tn-X-sample-production` when Project=sample + Environment=production", func() {
			env := canonicalNamespaceEnvironment("sample", "production", "tn-canon")
			// Spec.Namespace.Name is filtered out by the CRD's `Optional`
			// validation but the controller recomputes from Project regardless.
			env.Spec.Namespace.Name = "tn-canon-production" // M1-shaped (wrong) — controller must override
			createWithParentTenant(env)

			Eventually(func(g Gomega) {
				var ns corev1.Namespace
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "loopzero-tn-canon-sample-production",
				}, &ns)).To(Succeed())
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			// Best-effort cleanup so re-runs (or sister describes) start clean.
			_ = k8sClient.Delete(ctx, &loopzerov1.Environment{
				ObjectMeta: metav1.ObjectMeta{Name: "tn-canon-sample-production", Namespace: "default"},
			})
		})
	})

	Context("names longer than 63 characters", func() {
		// Fixed by MEET-2026-07-02-002. The former ~50% flake was a
		// status-write concurrency bug: the environment reconcile persisted the
		// NameTooLong condition via a refetch-then-restore Status().Update that
		// silently dropped the condition on an optimistic-concurrency conflict
		// (return nil, no requeue), and the phase never reached Pending so a
		// later delete hit an absent Empty→Deleting SM edge. Root-cause fix:
		// single-status-writer merge-patch (Status().Patch(MergeFrom(base))) +
		// requeue-on-conflict + the additive SM edge. Proven deterministically
		// in operators/internal/environment/environment_controller_conflict_test.go
		// (fake-client conflict injection) — this envtest spec is now the
		// end-to-end corroboration, no longer quarantined.
		It("sets Status condition NameTooLong without requeue when computed name >63 chars", func() {
			// Compose tenant + project + env so the canonical name exceeds
			// 63 chars: `loopzero-<26>-<27>-<25>` = 4 + 26 + 1 + 27 + 1 + 25 = 84 chars.
			env := canonicalNamespaceEnvironment(
				"production-data-pipeline-v2", // 27 chars
				"staging-canary-experiment",   // 25 chars
				"tn-very-long-tenant-name-h",  // 26 chars (rest RFC-1123 — leading lowercase + hyphens)
			)
			env.Name = "tn-overflow-staging-canary-experiment"
			Expect(k8sClient.Create(ctx, env)).To(Succeed())

			// Reconcile must NOT requeue indefinitely; assert NameTooLong condition appears.
			Eventually(func(g Gomega) {
				var fetched loopzerov1.Environment
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "tn-overflow-staging-canary-experiment", Namespace: "default",
				}, &fetched)).To(Succeed())
				var found bool
				for _, c := range fetched.Status.Conditions {
					if c.Type == "NamespaceReady" && c.Status == metav1.ConditionFalse && c.Reason == "NameTooLong" {
						found = true
						break
					}
				}
				g.Expect(found).To(BeTrue(),
					"expected NamespaceReady=False Reason=NameTooLong; got conditions: %+v",
					fetched.Status.Conditions)
			}, timeout, interval).Should(Succeed())

			// Sanity: no namespace was actually created at the M2 or any other form.
			var ns corev1.Namespace
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name: "loopzero-tn-very-long-tenant-name-h-production-data-pipeline-v2-staging-canary-experiment",
			}, &ns)
			Expect(err).To(HaveOccurred(), "63-char overflow must NOT create any namespace")
		})

		AfterAll(func() {
			_ = k8sClient.Delete(ctx, &loopzerov1.Environment{
				ObjectMeta: metav1.ObjectMeta{Name: "tn-overflow-staging-canary-experiment", Namespace: "default"},
			})
		})
	})
})
