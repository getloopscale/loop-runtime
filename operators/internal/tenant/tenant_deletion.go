// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package tenant

import (
	"context"
	"fmt"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/conditions"
	"github.com/getloopscale/loop-runtime/operators/internal/constants"
	"github.com/getloopscale/loop-runtime/operators/internal/metrics"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// reconcileDelete handles the verify-then-remove finalizer pattern for Tenant deletion.
//
// Deprovisioning strategy: each CRD owns its own cleanup. The TenantController
// waits for child Environment CRs to disappear (they handle namespace deletion),
// then cleans up tenant-level resources (ArgoCD, Gitea, build namespaces), and
// releases the finalizer only once the build namespaces are verified gone.
func (r *TenantReconciler) reconcileDelete(ctx context.Context, tenant *loopruntimev1.Tenant) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(tenant, tenantFinalizer) {
		return ctrl.Result{}, nil
	}
	log.FromContext(ctx).Info("tenant is being deleted", "tenant", tenant.Name)

	// This path's own base snapshot.
	//
	// Reconcile branches here BEFORE the finalizer block, so the base the main
	// path takes does not exist yet — and one taken at the top of Reconcile to
	// serve both would be stale for the main path. Two snapshots in disjoint
	// scopes make picking the wrong one impossible; the shape validated in
	// slice-21 (B-085).
	base := tenant.DeepCopy()
	if err := r.enterDeletingPhase(ctx, tenant, base); err != nil {
		return ctrl.Result{}, err
	}

	// Phase 1: child Environments own their namespaces; wait for them.
	if result, err := r.deleteChildEnvironments(ctx, tenant, base); err != nil || !result.IsZero() {
		return result, err
	}

	// Phase 2: tenant-level resources, retried with exponential backoff.
	if failures := r.cleanupTenantResources(ctx, tenant); len(failures) > 0 {
		return r.backOffDeletion(ctx, tenant, failures)
	}

	// Phase 3: the build namespaces must be gone before the finalizer goes.
	if result, err := r.verifyBuildNamespacesGone(ctx, tenant); err != nil || !result.IsZero() {
		return result, err
	}

	controllerutil.RemoveFinalizer(tenant, tenantFinalizer)
	if err := r.Update(ctx, tenant); err != nil {
		return ctrl.Result{}, err
	}
	log.FromContext(ctx).Info("tenant deletion complete", "tenant", tenant.Name)
	return ctrl.Result{}, nil
}

// enterDeletingPhase moves the tenant to Deleting through the state machine
// (idempotent once there) and persists it.
func (r *TenantReconciler) enterDeletingPhase(ctx context.Context, tenant, base *loopruntimev1.Tenant) error {
	if tenant.Status.Phase != loopruntimev1.TenantPhaseDeleting {
		currentPhase := statemachine.TenantPhase(tenant.Status.Phase)
		if currentPhase == "" {
			currentPhase = statemachine.TPhEmpty
		}
		next, err := tenantSM.TryTransition(currentPhase, statemachine.TPEventDelete)
		if err != nil {
			log.FromContext(ctx).Error(err, "invalid state transition for delete", "phase", tenant.Status.Phase)
			return err
		}
		tenant.Status.Phase = loopruntimev1.TenantPhase(next)
	}
	return r.patchStatus(ctx, tenant, base)
}

// deleteChildEnvironments deletes every Environment the tenant owns and asks
// to wait while any remains. Every child is attempted; a failed delete is
// recorded on the EnvironmentCleanup condition and returned, so the reconcile
// retries with controller-runtime's backoff (CODE-006 / VAL-005 / T2.4).
func (r *TenantReconciler) deleteChildEnvironments(ctx context.Context, tenant, base *loopruntimev1.Tenant) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	var envList loopruntimev1.EnvironmentList
	if err := r.List(ctx, &envList, client.InNamespace(tenant.Namespace)); err != nil {
		return ctrl.Result{}, fmt.Errorf("listing child Environment CRs: %w", err)
	}
	childEnvCount := 0
	var deleteErrs []error
	var firstFailedEnv string
	for i := range envList.Items {
		env := &envList.Items[i]
		if !ownedBy(env.OwnerReferences, tenant) {
			continue
		}
		childEnvCount++
		if !env.DeletionTimestamp.IsZero() {
			continue
		}
		if err := r.Delete(ctx, env); err != nil {
			logger.Error(err, "failed to delete child Environment CR", "environment", env.Name)
			conditions.SetCondition(&tenant.Status.Conditions, conditions.TypeTenantEnvironmentCleanup,
				metav1.ConditionFalse, "DeleteFailed",
				fmt.Sprintf("failed to delete environment %s: %v", env.Name, err),
				tenant.Generation)
			deleteErrs = append(deleteErrs, err)
			if firstFailedEnv == "" {
				firstFailedEnv = env.Name
			}
			continue
		}
		logger.Info("deleted child Environment CR", "environment", env.Name)
	}
	if len(deleteErrs) > 0 {
		// Persist the condition first so observers see the failure while
		// the reconciler retries.
		if statusErr := r.patchStatus(ctx, tenant, base); statusErr != nil {
			logger.Error(statusErr, "failed to persist EnvironmentCleanup condition")
		}
		return ctrl.Result{}, fmt.Errorf("phase 1: delete %d/%d child environments failed (first: %s): %w",
			len(deleteErrs), childEnvCount, firstFailedEnv, deleteErrs[0])
	}
	if childEnvCount > 0 {
		logger.Info("waiting for child Environment CRs to be deleted", "remaining", childEnvCount)
		return ctrl.Result{RequeueAfter: constants.RequeueDeletionWait}, nil
	}
	return ctrl.Result{}, nil
}

func ownedBy(refs []metav1.OwnerReference, tenant *loopruntimev1.Tenant) bool {
	for _, ref := range refs {
		if ref.UID == tenant.UID {
			return true
		}
	}
	return false
}

// cleanupTenantResources runs every tenant-level cleanup step, each timed
// into the step-duration metric, and returns one entry per failed step. A
// failing step does not stop the ones after it.
func (r *TenantReconciler) cleanupTenantResources(ctx context.Context, tenant *loopruntimev1.Tenant) []string {
	logger := log.FromContext(ctx)
	var failures []string
	run := func(stepName, errPrefix string, fn func() error) {
		if err := timedCleanupStep(stepName, fn); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", errPrefix, err))
			logger.Error(err, "cleanup failure", "step", errPrefix, "tenant", tenant.Name)
		}
	}

	run("argocd-apps", "argocd-cleanup", func() error {
		return r.ArgoCDProvisioner.DeleteApplications(ctx, tenant.Name)
	})
	run("argocd-appproject", "argocd-appproject-cleanup", func() error {
		return r.ArgoCDProvisioner.DeleteAppProject(ctx, tenant.Name)
	})
	run("build-ns", "build-ns-cleanup", func() error {
		return r.deleteBuildNamespaces(ctx, tenant)
	})
	if r.MinIOProvisioner != nil {
		run("minio", "minio-cleanup", func() error {
			return r.MinIOProvisioner.DeleteTenantCredentials(ctx, tenant.Name)
		})
	}
	run("gitea", "gitea-archive", func() error {
		// Audit gap M10 F2 / §6.E.4c — same tenant-ownership invariant
		// as reconcileGitRepo. Refusing to archive a foreign repo prevents
		// a tenant-delete operation from silently archiving another
		// tenant's repository when Spec.GitOps.RepoName is malformed
		// or adversarially set.
		if err := assertRepoNameOwnedByTenant(tenant.Name, tenant.Spec.GitOps.RepoName); err != nil {
			return err
		}
		return r.GitProvisioner.ArchiveRepo(ctx, tenant.Spec.GitOps.RepoName)
	})
	return failures
}

// deleteBuildNamespaces deletes the tenant's namespaces on the build cluster.
func (r *TenantReconciler) deleteBuildNamespaces(ctx context.Context, tenant *loopruntimev1.Tenant) error {
	var buildNsList corev1.NamespaceList
	if err := r.BuildClient.List(ctx, &buildNsList, client.MatchingLabels{constants.LabelTenant: tenant.Name}); err != nil {
		return err
	}
	for i := range buildNsList.Items {
		if !buildNsList.Items[i].DeletionTimestamp.IsZero() {
			continue
		}
		if err := r.BuildClient.Delete(ctx, &buildNsList.Items[i]); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// backOffDeletion records one more failed cleanup attempt on the tenant and
// requeues with exponential backoff (MEET-052); after DeletionMaxAttempts it
// reports the deletion stuck and requeues on the slow stuck interval.
func (r *TenantReconciler) backOffDeletion(ctx context.Context, tenant *loopruntimev1.Tenant, failures []string) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	retryCount := getDeletionRetryCount(tenant) + 1
	setDeletionRetryCount(tenant, retryCount)

	requeue := deletionBackoff(retryCount)
	if retryCount >= constants.DeletionMaxAttempts {
		logger.Error(fmt.Errorf("cleanup stuck after %d attempts: %v", retryCount, failures),
			"tenant deletion stuck — manual intervention required",
			"tenant", tenant.Name, "attempts", retryCount)
		metrics.TenantDeletionStuckTotal.Inc()
		requeue = constants.RequeueDeletionStuck
	} else {
		logger.Info("cleanup incomplete, requeuing with backoff",
			"failures", len(failures), "attempt", retryCount,
			"backoff", requeue.String(), "details", failures)
	}
	if err := r.Update(ctx, tenant); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeue}, nil
}

// verifyBuildNamespacesGone asks to wait while a build namespace remains. A
// build cluster that cannot be read is returned as an error, so it reaches
// controller-runtime's error metrics and backoff instead of a silent requeue.
func (r *TenantReconciler) verifyBuildNamespacesGone(ctx context.Context, tenant *loopruntimev1.Tenant) (ctrl.Result, error) {
	var remainingBuild corev1.NamespaceList
	if err := r.BuildClient.List(ctx, &remainingBuild, client.MatchingLabels{constants.LabelTenant: tenant.Name}); err != nil {
		return ctrl.Result{}, fmt.Errorf("phase 3: verify build namespaces of %s are deleted: %w", tenant.Name, err)
	}
	if len(remainingBuild.Items) > 0 {
		log.FromContext(ctx).Info("build namespaces still exist, requeuing", "count", len(remainingBuild.Items))
		return ctrl.Result{RequeueAfter: constants.RequeueDeletionWait}, nil
	}
	return ctrl.Result{}, nil
}

// timedCleanupStep executes a cleanup function and records its duration to Prometheus.
func timedCleanupStep(step string, fn func() error) error {
	start := time.Now()
	err := fn()
	duration := time.Since(start).Seconds()
	result := "success"
	if err != nil {
		result = "error"
	}
	metrics.TenantDeletionStepDuration.WithLabelValues(step, result).Observe(duration)
	return err
}

// getDeletionRetryCount reads the deletion retry count from the tenant annotation.
func getDeletionRetryCount(tenant *loopruntimev1.Tenant) int {
	annotations := tenant.GetAnnotations()
	if annotations == nil {
		return 0
	}
	v, ok := annotations[constants.AnnotationDeletionRetryCount]
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
}

// setDeletionRetryCount writes the deletion retry count to the tenant annotation.
func setDeletionRetryCount(tenant *loopruntimev1.Tenant, count int) {
	annotations := tenant.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[constants.AnnotationDeletionRetryCount] = strconv.Itoa(count)
	tenant.SetAnnotations(annotations)
}

// deletionBackoff calculates exponential backoff for deletion cleanup retries.
// Formula: min(base * 2^attempt, max). MEET-052.
func deletionBackoff(attempt int) time.Duration {
	backoff := constants.DeletionBackoffBase
	for i := 0; i < attempt; i++ {
		backoff *= 2
		if backoff >= constants.DeletionBackoffMax {
			return constants.DeletionBackoffMax
		}
	}
	return backoff
}
