package controller_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/conditions"
)

func newLoopZeroBuildCR(name, buildID, tenantID, project, env, sourceRef, imageTag string) *loopzerov1.LoopZeroBuild {
	return &loopzerov1.LoopZeroBuild{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: loopzerov1.LoopZeroBuildSpec{
			BuildID:         buildID,
			TenantID:        tenantID,
			ProjectSlug:     project,
			EnvironmentSlug: env,
			SourceRef:       sourceRef,
			ImageTag:        imageTag,
		},
	}
}

var _ = Describe("BuildController", Ordered, func() {

	Context("when a new LoopZeroBuild CR is created", func() {
		const (
			buildName = "build-test-001"
			buildID   = "bld-abc123"
			tenantID  = "tn-acme"
			project   = "myapi"
			env       = "production"
			sourceRef = "builds/tn-acme/myapi/abc123.tar.gz"
			imageTag  = "registry.local/tn-acme/myapi:abc123"
		)

		BeforeAll(func() {
			build := newLoopZeroBuildCR(buildName, buildID, tenantID, project, env, sourceRef, imageTag)
			Expect(k8sClient.Create(ctx, build)).To(Succeed())
		})

		It("should transition phase to Queued", func() {
			Eventually(func(g Gomega) {
				var build loopzerov1.LoopZeroBuild
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: buildName, Namespace: "default",
				}, &build)).To(Succeed())
				g.Expect(build.Status.Phase).To(Equal(loopzerov1.BuildPhaseQueued))
			}, timeout, interval).Should(Succeed())
		})

		It("should set SourcesResolved condition to True", func() {
			Eventually(func(g Gomega) {
				var build loopzerov1.LoopZeroBuild
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: buildName, Namespace: "default",
				}, &build)).To(Succeed())
				g.Expect(conditions.IsConditionTrue(build.Status.Conditions, conditions.TypeSourcesResolved)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})

		It("should set the expected workflowName", func() {
			Eventually(func(g Gomega) {
				var build loopzerov1.LoopZeroBuild
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: buildName, Namespace: "default",
				}, &build)).To(Succeed())
				g.Expect(build.Status.WorkflowName).NotTo(BeEmpty())
			}, timeout, interval).Should(Succeed())
		})

		It("should set observedGeneration", func() {
			Eventually(func(g Gomega) {
				var build loopzerov1.LoopZeroBuild
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: buildName, Namespace: "default",
				}, &build)).To(Succeed())
				g.Expect(build.Status.ObservedGeneration).To(Equal(build.Generation))
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			build := &loopzerov1.LoopZeroBuild{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: buildName, Namespace: "default"}, build); err == nil {
				_ = k8sClient.Delete(ctx, build)
			}
		})
	})

	Context("idempotency - re-reconcile produces same status", func() {
		const (
			buildName = "build-idemp-001"
			buildID   = "bld-idemp123"
			tenantID  = "tn-idemp"
			project   = "svc"
			env       = "staging"
			sourceRef = "builds/tn-idemp/svc/idemp.tar.gz"
			imageTag  = "registry.local/tn-idemp/svc:idemp"
		)

		BeforeAll(func() {
			build := newLoopZeroBuildCR(buildName, buildID, tenantID, project, env, sourceRef, imageTag)
			Expect(k8sClient.Create(ctx, build)).To(Succeed())

			// Wait for initial reconcile
			Eventually(func(g Gomega) {
				var build loopzerov1.LoopZeroBuild
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: buildName, Namespace: "default",
				}, &build)).To(Succeed())
				g.Expect(build.Status.Phase).To(Equal(loopzerov1.BuildPhaseQueued))
			}, timeout, interval).Should(Succeed())
		})

		It("should maintain Queued phase after annotation-triggered re-reconcile", func() {
			// Trigger re-reconcile
			Eventually(func(g Gomega) {
				var build loopzerov1.LoopZeroBuild
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: buildName, Namespace: "default",
				}, &build)).To(Succeed())
				if build.Annotations == nil {
					build.Annotations = make(map[string]string)
				}
				build.Annotations["test.loopzero.io/retrigger"] = "true"
				g.Expect(k8sClient.Update(ctx, &build)).To(Succeed())
			}, timeout, interval).Should(Succeed())

			// Verify phase is still Queued (idempotent)
			Consistently(func(g Gomega) {
				var build loopzerov1.LoopZeroBuild
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: buildName, Namespace: "default",
				}, &build)).To(Succeed())
				g.Expect(build.Status.Phase).To(Equal(loopzerov1.BuildPhaseQueued))
			}, "2s", interval).Should(Succeed())
		})

		AfterAll(func() {
			build := &loopzerov1.LoopZeroBuild{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: buildName, Namespace: "default"}, build); err == nil {
				_ = k8sClient.Delete(ctx, build)
			}
		})
	})

	Context("terminal state - Succeeded build does not requeue", func() {
		const (
			buildName = "build-terminal-001"
			buildID   = "bld-term123"
			tenantID  = "tn-term"
			project   = "svc"
			env       = "production"
			sourceRef = "builds/tn-term/svc/term.tar.gz"
			imageTag  = "registry.local/tn-term/svc:term"
		)

		BeforeAll(func() {
			build := newLoopZeroBuildCR(buildName, buildID, tenantID, project, env, sourceRef, imageTag)
			Expect(k8sClient.Create(ctx, build)).To(Succeed())

			// Wait for Queued
			Eventually(func(g Gomega) {
				var build loopzerov1.LoopZeroBuild
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: buildName, Namespace: "default",
				}, &build)).To(Succeed())
				g.Expect(build.Status.Phase).To(Equal(loopzerov1.BuildPhaseQueued))
			}, timeout, interval).Should(Succeed())

			// Simulate external completion by setting phase to Succeeded via status update.
			// In production, the controller would detect workflow completion.
			// Here we directly set the terminal state to verify the controller is a no-op.
			Eventually(func(g Gomega) {
				var build loopzerov1.LoopZeroBuild
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: buildName, Namespace: "default",
				}, &build)).To(Succeed())
				build.Status.Phase = loopzerov1.BuildPhaseSucceeded
				now := metav1.Now()
				build.Status.CompletedAt = &now
				build.Status.ImageDigest = "sha256:abc123"
				g.Expect(k8sClient.Status().Update(ctx, &build)).To(Succeed())
			}, timeout, interval).Should(Succeed())
		})

		It("should not change phase from Succeeded", func() {
			// Trigger re-reconcile
			Eventually(func(g Gomega) {
				var build loopzerov1.LoopZeroBuild
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: buildName, Namespace: "default",
				}, &build)).To(Succeed())
				if build.Annotations == nil {
					build.Annotations = make(map[string]string)
				}
				build.Annotations["test.loopzero.io/retrigger"] = "terminal-test"
				g.Expect(k8sClient.Update(ctx, &build)).To(Succeed())
			}, timeout, interval).Should(Succeed())

			// Phase should remain Succeeded
			Consistently(func(g Gomega) {
				var build loopzerov1.LoopZeroBuild
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: buildName, Namespace: "default",
				}, &build)).To(Succeed())
				g.Expect(build.Status.Phase).To(Equal(loopzerov1.BuildPhaseSucceeded))
			}, "2s", interval).Should(Succeed())
		})

		AfterAll(func() {
			build := &loopzerov1.LoopZeroBuild{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: buildName, Namespace: "default"}, build); err == nil {
				_ = k8sClient.Delete(ctx, build)
			}
		})
	})
})
