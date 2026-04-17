// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller

import (
	"context"
	"fmt"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/argocd"
	"github.com/loopzero/loopzero/operators/internal/conditions"
	"github.com/loopzero/loopzero/operators/internal/constants"
	"github.com/loopzero/loopzero/operators/internal/gitea"
	"github.com/loopzero/loopzero/operators/internal/metrics"
	"github.com/loopzero/loopzero/operators/internal/harbor"
	miniop "github.com/loopzero/loopzero/operators/internal/minio"
	"github.com/loopzero/loopzero/pkg/naming"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// tenantFinalizer uses the centralized constant.
const tenantFinalizer = constants.FinalizerTenant

// tenantSM is the Tenant lifecycle state machine (spec §5).
// Phase transitions go through this machine — direct assignment is forbidden.
var tenantSM = statemachine.NewTenantMachine()

// TenantReconciler reconciles a Tenant object.
//
// Multi-cluster design: the controller runs in the mgmt cluster but creates
// resources on the build and runtime clusters using separate clients.
//   - r.Client        → mgmt cluster (Tenant CRDs, status, Gitea, ArgoCD)
//   - r.BuildClient   → build cluster (build-{tenant} namespaces, quotas, secrets)
//   - r.RuntimeClient → runtime cluster ({tenant}-{env} namespaces, RBAC, network policies)
//
// ARCH-002: Field usage by sub-file:
//   - tenant_controller.go: Client, Scheme, RuntimeClient (environment CRs)
//   - tenant_build.go:      BuildClient, MinIOProvisioner, Credentials
//   - tenant_gitops.go:     Client, GitProvisioner, ArgoCDProvisioner, GiteaURL, WorkloadClusterURL
//   - tenant_credentials.go: BuildClient, Credentials (DOToken, RegistryDockerConfig)
//   - tenant_priority.go:   RuntimeClient
//   - tenant_harbor.go:     HarborClient, HarborRegistryHost, BuildClient, RuntimeClient
type TenantReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	BuildClient        client.Client               // targets the build cluster
	RuntimeClient      client.Client               // targets the runtime cluster
	GitProvisioner     gitea.GitRepoProvisioner
	ArgoCDProvisioner  argocd.Provisioner
	MinIOProvisioner   miniop.CredentialProvisioner // nil disables per-tenant MinIO credential isolation
	HarborClient       *harbor.Client              // nil disables Harbor integration (backward compat with DOCR)
	HarborRegistryHost string                       // e.g. "harbor.dev.loopzero.example" — used in Docker config.json
	GiteaURL           string
	WorkloadClusterURL string           // URL of the workload cluster API server
	Credentials        BuildCredentials // storage + registry credentials for build namespace secrets
	RegistryCIDRs      []string         // CIDR ranges for container registry egress restriction (RISK-007)
}

// TenantControllerConfig holds configuration for the TenantController.
type TenantControllerConfig struct {
	GiteaURL               string
	WorkloadClusterURL     string
	MinIOProvisioner       miniop.CredentialProvisioner // nil disables per-tenant MinIO credential isolation
	HarborClient           *harbor.Client              // nil disables Harbor integration
	HarborRegistryHost     string                       // e.g. "harbor.dev.loopzero.example"
	Credentials            BuildCredentials             // storage + registry credentials
	RegistryCIDRs          []string                     // CIDR ranges for container registry (RISK-007)
	MaxConcurrentReconciles int                         // 0 defaults to 5 (MEET-052: backpressure for bulk deletion)
}

// Reconcile handles the reconciliation loop for Tenant resources.
func (r *TenantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
	ctx, span := startReconcileSpan(ctx, "tenant", req.Name, req.Namespace)
	defer span.End()
	start := time.Now()
	defer func() {
		if retErr != nil {
			recordError(span, retErr)
		} else {
			setSuccess(span)
		}
		reconcileOutcome("tenant", start, retErr)
	}()

	logger := log.FromContext(ctx)

	var tenant loopzerov1.Tenant
	if err := r.Get(ctx, req.NamespacedName, &tenant); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion with verify-then-remove finalizer pattern
	if !tenant.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &tenant)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(&tenant, tenantFinalizer) {
		controllerutil.AddFinalizer(&tenant, tenantFinalizer)
		if err := r.Update(ctx, &tenant); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Get(ctx, req.NamespacedName, &tenant); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Set Phase to Provisioning if empty (via state machine)
	if tenant.Status.Phase == "" {
		tenant.Status.Phase = loopzerov1.TenantPhase(
			tenantSM.Transition(statemachine.TenantEmpty, statemachine.TenantCreate),
		)
	}

	// All reconcile steps run unconditionally (idempotent via EnsureX/CreateOrUpdate).
	// No condition-gated skipping — this ensures drift is corrected even when conditions
	// were set True by a previous controller version that no longer applies.
	steps := []reconcileStep{
		{conditions.TypeGitRepoReady, func() error { return r.reconcileGitRepo(ctx, &tenant) }},
		{conditions.TypeArgoCDAppReady, func() error { return r.reconcileArgoCDApp(ctx, &tenant) }},
		{conditions.TypeBuildNamespaceReady, func() error { return r.reconcileBuildNamespace(ctx, &tenant) }},
		{conditions.TypePriorityClassReady, func() error { return r.reconcilePriorityClass(ctx, &tenant) }},
		{conditions.TypeHarborProjectReady, func() error { return r.ensureHarborProject(ctx, &tenant) }},
		{"", func() error { return r.ensureHarborRobotAccounts(ctx, &tenant) }},
		{"", func() error { return r.reconcileEnvironmentCRs(ctx, &tenant) }},
	}
	if err := r.runSteps(ctx, req, &tenant, steps); err != nil {
		return ctrl.Result{}, err
	}

	// S1.7: Phase transition — Active when all conditions are True (via state machine)
	if conditions.AllConditionsTrue(tenant.Status.Conditions, r.requiredProvisioningConditions()...) && tenant.Status.Phase == loopzerov1.TenantPhaseProvisioning {
		tenant.Status.Phase = loopzerov1.TenantPhase(
			tenantSM.Transition(statemachine.TenantProvisioning, statemachine.TenantAllReady),
		)
	}
	tenant.Status.ObservedGeneration = tenant.Generation

	// Re-fetch to get latest resourceVersion before final status update.
	// Intermediate operations (CreateOrUpdate on child resources) may have triggered
	// concurrent reconciles that bumped the Tenant's resourceVersion.
	savedStatus := tenant.Status
	if err := r.Get(ctx, req.NamespacedName, &tenant); err != nil {
		return ctrl.Result{}, err
	}
	tenant.Status = savedStatus

	// Update status
	if err := r.Status().Update(ctx, &tenant); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("reconciled tenant", "tenant", tenant.Name, "phase", tenant.Status.Phase)
	return ctrl.Result{}, nil
}

// reconcileStep pairs a condition type with its reconcile function.
// If condType is empty, no condition is set on failure (used for steps without conditions).
type reconcileStep struct {
	condType string
	fn       func() error
}

// runSteps executes reconcile steps sequentially, setting conditions on failure.
// On error it re-fetches the resource to get the latest resourceVersion before
// updating status (best-effort) and returns the error.
func (r *TenantReconciler) runSteps(ctx context.Context, req ctrl.Request, tenant *loopzerov1.Tenant, steps []reconcileStep) error {
	logger := log.FromContext(ctx)
	for _, step := range steps {
		if err := step.fn(); err != nil {
			if step.condType != "" {
				conditions.SetCondition(&tenant.Status.Conditions, step.condType,
					metav1.ConditionFalse, "ReconcileFailed", err.Error(), tenant.Generation)
			}
			// Re-fetch to avoid stale resourceVersion from concurrent reconciles
			// triggered by child resource CreateOrUpdate operations.
			savedStatus := tenant.Status
			if refetchErr := r.Get(ctx, req.NamespacedName, tenant); refetchErr != nil {
				logger.Error(refetchErr, "failed to re-fetch tenant for status update", "tenant", tenant.Name)
				return err
			}
			tenant.Status = savedStatus
			if statusErr := r.Status().Update(ctx, tenant); statusErr != nil {
				logger.Error(statusErr, "failed to update tenant status", "tenant", tenant.Name)
			}
			return err
		}
	}
	return nil
}

// requiredProvisioningConditions returns the conditions that must be True
// for a tenant to transition from PROVISIONING to ACTIVE.
// When HarborClient is configured, HarborProjectReady is required.
// When HarborClient is nil (DOCR mode), Harbor conditions are skipped.
func (r *TenantReconciler) requiredProvisioningConditions() []string {
	required := []string{
		conditions.TypeGitRepoReady,
		conditions.TypeArgoCDAppReady,
		conditions.TypeBuildNamespaceReady,
		conditions.TypeQuotaEnforced,
		conditions.TypePriorityClassReady,
	}
	if r.HarborClient != nil {
		required = append(required, conditions.TypeHarborProjectReady)
	}
	return required
}


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
	currentPhase := statemachine.State(tenant.Status.Phase)
	if currentPhase == "" {
		currentPhase = statemachine.TenantEmpty
	}
	// Only transition if not already Deleting (idempotent)
	// Uses TryTransition because tenant.Status.Phase comes from CRD status (external source).
	if tenant.Status.Phase != loopzerov1.TenantPhaseDeleting {
		next, err := tenantSM.TryTransition(currentPhase, statemachine.TenantDelete)
		if err != nil {
			logger.Error(err, "invalid state transition for delete", "phase", tenant.Status.Phase)
			return ctrl.Result{}, err
		}
		tenant.Status.Phase = loopzerov1.TenantPhase(next)
	}
	if err := r.Status().Update(ctx, tenant); err != nil {
		return ctrl.Result{}, err
	}

	// Phase 1: Delete child Environment CRs and wait for them to be fully cleaned up.
	// Environment CRs have a finalizer (loopzero.io/environment-cleanup) that the
	// EnvironmentController handles — it cleans up the target namespace before
	// removing the finalizer. We explicitly delete them here because ownerRef
	// cascade with blockOwnerDeletion + finalizer creates a deadlock: GC waits
	// for the child to be deleted, but the child's finalizer prevents GC deletion.
	var envList loopzerov1.EnvironmentList
	if err := r.List(ctx, &envList, client.InNamespace(tenant.Namespace)); err != nil {
		return ctrl.Result{}, fmt.Errorf("listing child Environment CRs: %w", err)
	}
	childEnvCount := 0
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
		// Explicitly delete if not already marked for deletion
		if env.DeletionTimestamp.IsZero() {
			if err := r.Delete(ctx, env); err != nil {
				logger.Error(err, "failed to delete child Environment CR", "environment", env.Name)
				conditions.SetCondition(&tenant.Status.Conditions, "EnvironmentCleanup",
					metav1.ConditionFalse, "DeleteFailed",
					fmt.Sprintf("failed to delete environment %s: %v", env.Name, err),
					tenant.Generation)
			} else {
				logger.Info("deleted child Environment CR", "environment", env.Name)
			}
		}
	}
	if childEnvCount > 0 {
		logger.Info("waiting for child Environment CRs to be deleted", "remaining", childEnvCount)
		return ctrl.Result{RequeueAfter: constants.RequeueDeletionWait}, nil
	}

	// Phase 2: All child Environments are gone. Clean up tenant-level resources.
	// Track cleanup failures — finalizer is only removed when all steps succeed or
	// resources are confirmed gone. This prevents orphaning ArgoCD apps and Gitea repos.
	// MEET-052: Each step is instrumented with per-step duration metrics.
	var cleanupErrors []string

	// Delete Harbor project for this tenant (idempotent — no-op if HarborClient is nil)
	if err := timedCleanupStep("harbor", func() error {
		return r.deleteHarborProject(ctx, tenant)
	}); err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Sprintf("harbor-cleanup: %v", err))
		logger.Error(err, "cleanup failure", "step", "harbor-cleanup", "tenant", tenant.Name)
	}

	// Delete ArgoCD Applications for this tenant
	if err := timedCleanupStep("argocd-apps", func() error {
		return r.ArgoCDProvisioner.DeleteApplications(ctx, tenant.Name)
	}); err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Sprintf("argocd-cleanup: %v", err))
		logger.Error(err, "cleanup failure", "step", "argocd-cleanup", "tenant", tenant.Name)
	}

	// Delete ArgoCD AppProject for this tenant (MOD-5)
	if err := timedCleanupStep("argocd-appproject", func() error {
		return r.ArgoCDProvisioner.DeleteAppProject(ctx, tenant.Name)
	}); err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Sprintf("argocd-appproject-cleanup: %v", err))
		logger.Error(err, "cleanup failure", "step", "argocd-appproject-cleanup", "tenant", tenant.Name)
	}

	// Delete namespaces on build cluster (build-{tenant})
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

	// Delete MinIO credentials for this tenant (arch_op_008: cleanup on deletion)
	if r.MinIOProvisioner != nil {
		if err := timedCleanupStep("minio", func() error {
			return r.MinIOProvisioner.DeleteTenantCredentials(ctx, tenant.Name)
		}); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Sprintf("minio-cleanup: %v", err))
			logger.Error(err, "cleanup failure", "step", "minio-cleanup", "tenant", tenant.Name)
		}
	}

	// Archive Gitea repo (soft delete, idempotent)
	if err := timedCleanupStep("gitea", func() error {
		return r.GitProvisioner.ArchiveRepo(ctx, tenant.Spec.GitOps.RepoName)
	}); err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Sprintf("gitea-archive: %v", err))
		logger.Error(err, "cleanup failure", "step", "gitea-archive", "repo", tenant.Spec.GitOps.RepoName)
	}

	// If any cleanup step failed, apply exponential backoff (MEET-052).
	// After DeletionMaxAttempts, stop requeuing and emit critical log for alerting.
	if len(cleanupErrors) > 0 {
		retryCount := getDeletionRetryCount(tenant)
		retryCount++

		if retryCount >= constants.DeletionMaxAttempts {
			logger.Error(fmt.Errorf("cleanup stuck after %d attempts: %v", retryCount, cleanupErrors),
				"tenant deletion stuck — manual intervention required",
				"tenant", tenant.Name, "attempts", retryCount)
			// Update annotation to reflect final count but do NOT requeue.
			// PrometheusRule loopzero_tenant_deletion_stuck will alert on this.
			metrics.TenantDeletionStuckTotal.Inc()
			setDeletionRetryCount(tenant, retryCount)
			if err := r.Update(ctx, tenant); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
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
// MEET-052: Per-step metrics for deletion observability.
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

// reconcileEnvironmentCRs creates/updates Environment CRs for each spec.environments[],
// then garbage-collects orphaned Environment CRs that are no longer in the spec.
// Each Environment CR is owned by the Tenant CR (cascade delete).
// The EnvironmentController watches these CRs and manages namespaces/RBAC/NetworkPolicy.
func (r *TenantReconciler) reconcileEnvironmentCRs(ctx context.Context, tenant *loopzerov1.Tenant) error {
	logger := log.FromContext(ctx)

	// Build the desired set of Environment CR names from spec.
	desired := make(map[string]bool, len(tenant.Spec.Environments))
	for _, env := range tenant.Spec.Environments {
		desired[fmt.Sprintf("%s-%s", tenant.Name, env.Name)] = true
	}

	// Create/update loop — one Environment CR per spec entry.
	for _, env := range tenant.Spec.Environments {
		envCRName := fmt.Sprintf("%s-%s", tenant.Name, env.Name)
		nsName, err := naming.EnvironmentNamespace(tenant.Name, env.Name)
		if err != nil {
			return fmt.Errorf("invalid namespace name for env %s: %w", env.Name, err)
		}

		envCR := &loopzerov1.Environment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      envCRName,
				Namespace: tenant.Namespace,
			},
		}
		_, err = controllerutil.CreateOrUpdate(ctx, r.Client, envCR, func() error {
			envCR.Spec.TenantRef.Name = tenant.Name
			envCR.Spec.Environment = env.Name
			envCR.Spec.Namespace.Name = nsName
			envCR.Spec.Plan = tenant.Spec.Plan
			envCR.Spec.NetworkPolicy = loopzerov1.NetworkPolicyConfig{Isolate: true}
			envCR.Spec.ResourceQuota = &loopzerov1.ResourceQuotaSpec{
				CPU:     tenant.Spec.Limits.MaxCPU.String(),
				Memory:  tenant.Spec.Limits.MaxMemory.String(),
				Storage: tenant.Spec.Limits.MaxStorage.String(),
			}
			// Propagate tenant generation to force EnvironmentController re-reconcile
			// when Tenant spec changes. This enables drift correction: if child resources
			// (NetworkPolicy, ResourceQuota) are manually modified/deleted, the next
			// Tenant reconcile bumps this annotation → EnvironmentController re-runs.
			if envCR.Annotations == nil {
				envCR.Annotations = make(map[string]string)
			}
			envCR.Annotations[constants.AnnotationParentGeneration] = fmt.Sprintf("%d", tenant.Generation)
			return controllerutil.SetControllerReference(tenant, envCR, r.Scheme)
		})
		if err != nil {
			return fmt.Errorf("environment CR %s: %w", envCRName, err)
		}

		logger.Info("environment CR reconciled", "environment", envCRName)
	}

	// GC: delete Environment CRs owned by this tenant that are no longer in spec.
	// The EnvironmentController's finalizer handles namespace/RBAC/quota cleanup
	// when its CR is deleted, so we only need to trigger the delete here.
	var envList loopzerov1.EnvironmentList
	if err := r.List(ctx, &envList, client.InNamespace(tenant.Namespace)); err != nil {
		return fmt.Errorf("gc: list environments: %w", err)
	}
	for i := range envList.Items {
		envCR := &envList.Items[i]
		if !metav1.IsControlledBy(envCR, tenant) {
			continue // not owned by this tenant
		}
		if desired[envCR.Name] {
			continue // still in spec — keep it
		}
		logger.Info("garbage-collecting orphan Environment CR",
			"environment", envCR.Name, "tenant", tenant.Name)
		if err := r.Delete(ctx, envCR); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("gc environment CR %s: %w", envCR.Name, err)
		}
	}

	return nil
}

// SetupTenantController registers the TenantController with the manager.
// cfg.GiteaURL configures the Gitea base URL for ArgoCD Application repo references.
// If empty, it defaults to the in-cluster Gitea service URL.
// cfg.WorkloadClusterURL configures the workload cluster API server URL for ArgoCD Applications.
// If empty, it defaults to "https://kubernetes.default.svc" (backward compatible for single-cluster setups).
func SetupTenantController(mgr ctrl.Manager, gitProvisioner gitea.GitRepoProvisioner, argoCDProvisioner argocd.Provisioner, buildClient client.Client, runtimeClient client.Client, cfg TenantControllerConfig) error {
	giteaURL := cfg.GiteaURL
	if giteaURL == "" {
		giteaURL = constants.DefaultGiteaURL
	}
	workloadClusterURL := cfg.WorkloadClusterURL
	if workloadClusterURL == "" {
		workloadClusterURL = constants.DefaultWorkloadClusterURL
	}
	maxConcurrent := cfg.MaxConcurrentReconciles
	if maxConcurrent <= 0 {
		maxConcurrent = 5 // MEET-052: default backpressure for bulk deletion
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopzerov1.Tenant{}).
		Owns(&loopzerov1.Environment{}).
		Named("tenant").
		WithOptions(crcontroller.Options{MaxConcurrentReconciles: maxConcurrent}).
		Complete(&TenantReconciler{
			Client:             mgr.GetClient(),
			Scheme:             mgr.GetScheme(),
			BuildClient:        buildClient,
			RuntimeClient:      runtimeClient,
			GitProvisioner:     gitProvisioner,
			ArgoCDProvisioner:  argoCDProvisioner,
			MinIOProvisioner:   cfg.MinIOProvisioner,
			HarborClient:       cfg.HarborClient,
			HarborRegistryHost: cfg.HarborRegistryHost,
			GiteaURL:           giteaURL,
			WorkloadClusterURL: workloadClusterURL,
			Credentials:        cfg.Credentials,
			RegistryCIDRs:      cfg.RegistryCIDRs,
		})
}
