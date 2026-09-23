// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/conditions"
	"github.com/getloopscale/loopzero/operators/internal/constants"
	"github.com/getloopscale/loopzero/operators/internal/metrics"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// reconcileDelete handles the verify-then-remove finalizer pattern for Tenant deletion.
//
// Deprovisioning strategy: each CRD owns its own cleanup. The TenantController
// waits for child Environment CRs to disappear (they handle namespace deletion),
// then cleans up tenant-level resources (ArgoCD, Gitea, build namespaces).
func (r *TenantReconciler) reconcileDelete(ctx context.Context, tenant *loopzerov1.Tenant) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(tenant, tenantFinalizer) {
		return ctrl.Result{}, nil
	}

	logger.Info("tenant is being deleted", "tenant", tenant.Name)
	currentPhase := statemachine.TenantPhase(tenant.Status.Phase)
	if currentPhase == "" {
		currentPhase = statemachine.TPhEmpty
	}
	// This path's own base snapshot.
	//
	// Reconcile branches here BEFORE the finalizer block, so the base the main
	// path takes does not exist yet — and one taken at the top of Reconcile to
	// serve both would be stale for the main path. Two snapshots in disjoint
	// scopes make picking the wrong one impossible; the shape validated in
	// slice-21 (B-085).
	base := tenant.DeepCopy()

	// Only transition if not already Deleting (idempotent)
	if tenant.Status.Phase != loopzerov1.TenantPhaseDeleting {
		next, err := tenantSM.TryTransition(currentPhase, statemachine.TPEventDelete)
		if err != nil {
			logger.Error(err, "invalid state transition for delete", "phase", tenant.Status.Phase)
			return ctrl.Result{}, err
		}
		tenant.Status.Phase = loopzerov1.TenantPhase(next)
	}
	if err := r.patchStatus(ctx, tenant, base); err != nil {
		return ctrl.Result{}, err
	}

	// Phase 1: Delete child Environment CRs and wait for them to be fully cleaned up.
	var envList loopzerov1.EnvironmentList
	if err := r.List(ctx, &envList, client.InNamespace(tenant.Namespace)); err != nil {
		return ctrl.Result{}, fmt.Errorf("listing child Environment CRs: %w", err)
	}
	// CODE-006 / VAL-005 / T2.4: collect per-environment Delete errors so we
	// can return an aggregate error after the best-effort loop. Previously,
	// failures were logged + condition-set but never returned, so the
	// reconciler used the standard requeue interval and never engaged the
	// exponential backoff machinery wired in MEET-052. Now: best-effort try
	// to delete every child, but return the first error (with count) so the
	// reconcile retries with backoff.
	childEnvCount := 0
	var deleteErrs []error
	var firstFailedEnv string
	for i := range envList.Items {
		env := &envList.Items[i]
		isChild := false
		for _, ref := range env.OwnerReferences {
			if ref.UID == tenant.UID {
				isChild = true
				break
			}
		}
		if !isChild {
			continue
		}
		childEnvCount++
		if env.DeletionTimestamp.IsZero() {
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
			} else {
				logger.Info("deleted child Environment CR", "environment", env.Name)
			}
		}
	}
	if len(deleteErrs) > 0 {
		// Persist condition before returning so observers can see the
		// failure even while the reconciler retries with backoff.
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

	// Phase 2: Clean up tenant-level resources with per-step metrics.
	var cleanupErrors []string

	runCleanup := func(stepName, errPrefix string, fn func() error) {
		if err := timedCleanupStep(stepName, fn); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Sprintf("%s: %v", errPrefix, err))
			logger.Error(err, "cleanup failure", "step", errPrefix, "tenant", tenant.Name)
		}
	}

	runCleanup("harbor", "harbor-cleanup", func() error {
		return r.deleteHarborProject(ctx, tenant)
	})
	runCleanup("argocd-apps", "argocd-cleanup", func() error {
		return r.ArgoCDProvisioner.DeleteApplications(ctx, tenant.Name)
	})
	runCleanup("argocd-appproject", "argocd-appproject-cleanup", func() error {
		return r.ArgoCDProvisioner.DeleteAppProject(ctx, tenant.Name)
	})

	if err := timedCleanupStep("build-ns", func() error {
		var buildNsList corev1.NamespaceList
		if err := r.BuildClient.List(ctx, &buildNsList, client.MatchingLabels{constants.LabelTenant: tenant.Name}); err != nil {
			return err
		}
		for i := range buildNsList.Items {
			if buildNsList.Items[i].DeletionTimestamp.IsZero() {
				if err := r.BuildClient.Delete(ctx, &buildNsList.Items[i]); err != nil && !errors.IsNotFound(err) {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Sprintf("build-ns-cleanup: %v", err))
		logger.Error(err, "cleanup failure on build cluster", "step", "build-ns-cleanup")
	}

	if r.MinIOProvisioner != nil {
		runCleanup("minio", "minio-cleanup", func() error {
			return r.MinIOProvisioner.DeleteTenantCredentials(ctx, tenant.Name)
		})
	}

	if err := timedCleanupStep("gitea", func() error {
		// Audit gap M10 F2 / §6.E.4c — same tenant-ownership invariant
		// as reconcileGitRepo. Refusing to archive a foreign repo prevents
		// a tenant-delete operation from silently archiving another
		// tenant's repository when Spec.GitOps.RepoName is malformed
		// or adversarially set.
		if err := assertRepoNameOwnedByTenant(tenant.Name, tenant.Spec.GitOps.RepoName); err != nil {
			return err
		}
		return r.GitProvisioner.ArchiveRepo(ctx, tenant.Spec.GitOps.RepoName)
	}); err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Sprintf("gitea-archive: %v", err))
		logger.Error(err, "cleanup failure", "step", "gitea-archive", "repo", tenant.Spec.GitOps.RepoName)
	}

	// If any cleanup step failed, apply exponential backoff (MEET-052).
	if len(cleanupErrors) > 0 {
		retryCount := getDeletionRetryCount(tenant)
		retryCount++

		if retryCount >= constants.DeletionMaxAttempts {
			logger.Error(fmt.Errorf("cleanup stuck after %d attempts: %v", retryCount, cleanupErrors),
				"tenant deletion stuck — manual intervention required",
				"tenant", tenant.Name, "attempts", retryCount)
			metrics.TenantDeletionStuckTotal.Inc()
			setDeletionRetryCount(tenant, retryCount)
			if err := r.Update(ctx, tenant); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: constants.RequeueDeletionStuck}, nil
		}

		backoff := deletionBackoff(retryCount)
		logger.Info("cleanup incomplete, requeuing with backoff",
			"failures", len(cleanupErrors), "attempt", retryCount,
			"backoff", backoff.String(), "details", cleanupErrors)
		setDeletionRetryCount(tenant, retryCount)
		if err := r.Update(ctx, tenant); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: backoff}, nil
	}

	// Phase 3: Verify build namespaces are gone.
	var remainingBuild corev1.NamespaceList
	if err := r.BuildClient.List(ctx, &remainingBuild, client.MatchingLabels{constants.LabelTenant: tenant.Name}); err != nil {
		logger.Error(err, "could not verify build namespace deletion")
		return ctrl.Result{RequeueAfter: constants.RequeueDeletionWait}, nil
	}
	if len(remainingBuild.Items) > 0 {
		logger.Info("build namespaces still exist, requeuing", "count", len(remainingBuild.Items))
		return ctrl.Result{RequeueAfter: constants.RequeueDeletionWait}, nil
	}

	// All clean — remove finalizer
	controllerutil.RemoveFinalizer(tenant, tenantFinalizer)
	if err := r.Update(ctx, tenant); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("tenant deletion complete", "tenant", tenant.Name)
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
func getDeletionRetryCount(tenant *loopzerov1.Tenant) int {
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
func setDeletionRetryCount(tenant *loopzerov1.Tenant, count int) {
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
