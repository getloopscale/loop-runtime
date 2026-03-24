package controller_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/conditions"
)

func newLoopZeroReleaseCR(name, releaseID, tenantID, project, env, imageRef string) *loopzerov1.LoopZeroRelease {
	return &loopzerov1.LoopZeroRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: loopzerov1.LoopZeroReleaseSpec{
			ReleaseID:       releaseID,
			TenantID:        tenantID,
			ProjectSlug:     project,
			EnvironmentSlug: env,
			ImageRef:        imageRef,
			Replicas:        1,
		},
	}
}

var _ = Describe("ReleaseController", Ordered, func() {

	Context("when a new LoopZeroRelease CR is created", func() {
		const (
			releaseName = "release-test-001"
			releaseID   = "rel-abc123"
			tenantID    = "tn-acme"
			project     = "myapi"
			env         = "production"
			imageRef    = "registry.local/tn-acme/myapi@sha256:abc123"
		)

		BeforeAll(func() {
			release := newLoopZeroReleaseCR(releaseName, releaseID, tenantID, project, env, imageRef)
			Expect(k8sClient.Create(ctx, release)).To(Succeed())
		})

		It("should transition phase to Ready", func() {
			Eventually(func(g Gomega) {
				var release loopzerov1.LoopZeroRelease
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: releaseName, Namespace: "default",
				}, &release)).To(Succeed())
				g.Expect(release.Status.Phase).To(Equal(loopzerov1.ReleasePhaseReady))
			}, timeout, interval).Should(Succeed())
		})

		It("should set observedGeneration", func() {
			Eventually(func(g Gomega) {
				var release loopzerov1.LoopZeroRelease
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: releaseName, Namespace: "default",
				}, &release)).To(Succeed())
				g.Expect(release.Status.ObservedGeneration).To(Equal(release.Generation))
			}, timeout, interval).Should(Succeed())
		})

		It("should set GitMirrorCompleted condition to False initially", func() {
			Eventually(func(g Gomega) {
				var release loopzerov1.LoopZeroRelease
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: releaseName, Namespace: "default",
				}, &release)).To(Succeed())
				c := conditions.GetCondition(release.Status.Conditions, conditions.TypeGitMirrorCompleted)
				g.Expect(c).NotTo(BeNil())
				g.Expect(c.Status).To(Equal(metav1.ConditionFalse))
			}, timeout, interval).Should(Succeed())
		})

		AfterAll(func() {
			release := &loopzerov1.LoopZeroRelease{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: releaseName, Namespace: "default"}, release); err == nil {
				_ = k8sClient.Delete(ctx, release)
			}
		})
	})

	Context("idempotency - re-reconcile produces same status", func() {
		const (
			releaseName = "release-idemp-001"
			releaseID   = "rel-idemp123"
			tenantID    = "tn-idemp"
			project     = "svc"
			env         = "staging"
			imageRef    = "registry.local/tn-idemp/svc@sha256:idemp"
		)

		BeforeAll(func() {
			release := newLoopZeroReleaseCR(releaseName, releaseID, tenantID, project, env, imageRef)
			Expect(k8sClient.Create(ctx, release)).To(Succeed())

			// Wait for initial reconcile
			Eventually(func(g Gomega) {
				var release loopzerov1.LoopZeroRelease
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: releaseName, Namespace: "default",
				}, &release)).To(Succeed())
				g.Expect(release.Status.Phase).To(Equal(loopzerov1.ReleasePhaseReady))
			}, timeout, interval).Should(Succeed())
		})

		It("should maintain Ready phase after annotation-triggered re-reconcile", func() {
			// Trigger re-reconcile
			Eventually(func(g Gomega) {
				var release loopzerov1.LoopZeroRelease
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: releaseName, Namespace: "default",
				}, &release)).To(Succeed())
				if release.Annotations == nil {
					release.Annotations = make(map[string]string)
				}
				release.Annotations["test.loopzero.io/retrigger"] = "true"
				g.Expect(k8sClient.Update(ctx, &release)).To(Succeed())
			}, timeout, interval).Should(Succeed())

			// Phase should remain Ready
			Consistently(func(g Gomega) {
				var release loopzerov1.LoopZeroRelease
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: releaseName, Namespace: "default",
				}, &release)).To(Succeed())
				g.Expect(release.Status.Phase).To(Equal(loopzerov1.ReleasePhaseReady))
			}, "2s", interval).Should(Succeed())
		})

		AfterAll(func() {
			release := &loopzerov1.LoopZeroRelease{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: releaseName, Namespace: "default"}, release); err == nil {
				_ = k8sClient.Delete(ctx, release)
			}
		})
	})

	Context("terminal state - Active release does not requeue", func() {
		const (
			releaseName = "release-terminal-001"
			releaseID   = "rel-term123"
			tenantID    = "tn-term"
			project     = "svc"
			env         = "production"
			imageRef    = "registry.local/tn-term/svc@sha256:term"
		)

		BeforeAll(func() {
			release := newLoopZeroReleaseCR(releaseName, releaseID, tenantID, project, env, imageRef)
			Expect(k8sClient.Create(ctx, release)).To(Succeed())

			// Wait for Ready
			Eventually(func(g Gomega) {
				var release loopzerov1.LoopZeroRelease
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: releaseName, Namespace: "default",
				}, &release)).To(Succeed())
				g.Expect(release.Status.Phase).To(Equal(loopzerov1.ReleasePhaseReady))
			}, timeout, interval).Should(Succeed())

			// Simulate external completion: set phase to Active via status update.
			Eventually(func(g Gomega) {
				var release loopzerov1.LoopZeroRelease
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: releaseName, Namespace: "default",
				}, &release)).To(Succeed())
				release.Status.Phase = loopzerov1.ReleasePhaseActive
				now := metav1.Now()
				release.Status.CompletedAt = &now
				conditions.SetCondition(&release.Status.Conditions, conditions.TypeReleaseActive,
					metav1.ConditionTrue, conditions.ReasonReady,
					"release is active", release.Generation)
				g.Expect(k8sClient.Status().Update(ctx, &release)).To(Succeed())
			}, timeout, interval).Should(Succeed())
		})

		It("should not change phase from Active", func() {
			// Trigger re-reconcile
			Eventually(func(g Gomega) {
				var release loopzerov1.LoopZeroRelease
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: releaseName, Namespace: "default",
				}, &release)).To(Succeed())
				if release.Annotations == nil {
					release.Annotations = make(map[string]string)
				}
				release.Annotations["test.loopzero.io/retrigger"] = "terminal-test"
				g.Expect(k8sClient.Update(ctx, &release)).To(Succeed())
			}, timeout, interval).Should(Succeed())

			// Phase should remain Active
			Consistently(func(g Gomega) {
				var release loopzerov1.LoopZeroRelease
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: releaseName, Namespace: "default",
				}, &release)).To(Succeed())
				g.Expect(release.Status.Phase).To(Equal(loopzerov1.ReleasePhaseActive))
			}, "2s", interval).Should(Succeed())
		})

		AfterAll(func() {
			release := &loopzerov1.LoopZeroRelease{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: releaseName, Namespace: "default"}, release); err == nil {
				_ = k8sClient.Delete(ctx, release)
			}
		})
	})
})
