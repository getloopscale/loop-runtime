// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/argocd"
	"github.com/loopzero/loopzero/operators/internal/conditions"
	"github.com/loopzero/loopzero/operators/internal/constants"
	"github.com/loopzero/loopzero/operators/internal/gitea"
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
type TenantReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	BuildClient        client.Client               // targets the build cluster
	RuntimeClient      client.Client               // targets the runtime cluster
	GitProvisioner     gitea.GitRepoProvisioner
	ArgoCDProvisioner  argocd.Provisioner
	MinIOProvisioner   miniop.CredentialProvisioner // nil disables per-tenant MinIO credential isolation
	GiteaURL           string
	WorkloadClusterURL string           // URL of the workload cluster API server
	Credentials        BuildCredentials // storage + registry credentials for build namespace secrets
	RegistryCIDRs      []string         // CIDR ranges for container registry egress restriction (RISK-007)
}

// TenantControllerConfig holds configuration for the TenantController.
type TenantControllerConfig struct {
	GiteaURL           string
	WorkloadClusterURL string
	MinIOProvisioner   miniop.CredentialProvisioner // nil disables per-tenant MinIO credential isolation
	Credentials        BuildCredentials             // storage + registry credentials
	RegistryCIDRs      []string                     // CIDR ranges for container registry (RISK-007)
}

// Reconcile handles the reconciliation loop for Tenant resources.
func (r *TenantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
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
		{"", func() error { return r.reconcileEnvironmentCRs(ctx, &tenant) }},
	}
	if err := r.runSteps(ctx, req, &tenant, steps); err != nil {
		return ctrl.Result{}, err
	}

	// S1.7: Phase transition — Active when all conditions are True (via state machine)
	if conditions.AllConditionsTrue(tenant.Status.Conditions, requiredProvisioningConditions...) && tenant.Status.Phase == loopzerov1.TenantPhaseProvisioning {
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

// requiredProvisioningConditions lists all conditions that must be True
// for a tenant to transition from PROVISIONING to ACTIVE.
// Add new conditions here when adding new reconciliation steps.
var requiredProvisioningConditions = []string{
	conditions.TypeGitRepoReady,
	conditions.TypeArgoCDAppReady,
	conditions.TypeBuildNamespaceReady,
	conditions.TypeQuotaEnforced,
	conditions.TypePriorityClassReady,
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

	// Phase 1: Wait for child Environment CRs to be deleted.
	// Environment CRs are owned by the Tenant (ownerRef cascade), so K8s marks them
	// for deletion automatically. Each EnvironmentController handles its own namespace cleanup.
	var envList loopzerov1.EnvironmentList
	if err := r.List(ctx, &envList, client.InNamespace(tenant.Namespace)); err != nil {
		return ctrl.Result{}, fmt.Errorf("listing child Environment CRs: %w", err)
	}
	childEnvCount := 0
	for _, env := range envList.Items {
		for _, ref := range env.OwnerReferences {
			if ref.UID == tenant.UID {
				childEnvCount++
				break
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
	var cleanupErrors []string

	// Delete ArgoCD Applications for this tenant
	if err := r.ArgoCDProvisioner.DeleteApplications(ctx, tenant.Name); err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Sprintf("argocd-cleanup: %v", err))
		logger.Error(err, "cleanup failure", "step", "argocd-cleanup", "tenant", tenant.Name)
	}

	// Delete ArgoCD AppProject for this tenant (MOD-5)
	if err := r.ArgoCDProvisioner.DeleteAppProject(ctx, tenant.Name); err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Sprintf("argocd-appproject-cleanup: %v", err))
		logger.Error(err, "cleanup failure", "step", "argocd-appproject-cleanup", "tenant", tenant.Name)
	}

	// Delete namespaces on build cluster (build-{tenant})
	var buildNsList corev1.NamespaceList
	if err := r.BuildClient.List(ctx, &buildNsList, client.MatchingLabels{constants.LabelTenant: tenant.Name}); err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Sprintf("list-build-ns: %v", err))
		logger.Error(err, "cleanup failure on build cluster", "step", "list-build-ns")
	} else {
		for i := range buildNsList.Items {
			if buildNsList.Items[i].DeletionTimestamp.IsZero() {
				if err := r.BuildClient.Delete(ctx, &buildNsList.Items[i]); err != nil && !errors.IsNotFound(err) {
					return ctrl.Result{}, err
				}
			}
		}
	}

	// Delete MinIO credentials for this tenant (arch_op_008: cleanup on deletion)
	if r.MinIOProvisioner != nil {
		if err := r.MinIOProvisioner.DeleteTenantCredentials(ctx, tenant.Name); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Sprintf("minio-cleanup: %v", err))
			logger.Error(err, "cleanup failure", "step", "minio-cleanup", "tenant", tenant.Name)
		}
	}

	// Archive Gitea repo (soft delete, idempotent)
	if err := r.GitProvisioner.ArchiveRepo(ctx, tenant.Spec.GitOps.RepoName); err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Sprintf("gitea-archive: %v", err))
		logger.Error(err, "cleanup failure", "step", "gitea-archive", "repo", tenant.Spec.GitOps.RepoName)
	}

	// If any cleanup step failed, requeue to retry instead of removing the finalizer.
	// This prevents orphaning external resources (ArgoCD Applications, Gitea repos).
	if len(cleanupErrors) > 0 {
		logger.Info("cleanup incomplete, requeuing", "failures", len(cleanupErrors), "details", cleanupErrors)
		return ctrl.Result{RequeueAfter: constants.RequeueDeletionWait}, nil
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

// reconcileEnvironmentCRs creates/updates Environment CRs for each spec.environments[].
// Each Environment CR is owned by the Tenant CR (cascade delete).
// The EnvironmentController watches these CRs and manages namespaces/RBAC/NetworkPolicy.
func (r *TenantReconciler) reconcileEnvironmentCRs(ctx context.Context, tenant *loopzerov1.Tenant) error {
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
			return controllerutil.SetControllerReference(tenant, envCR, r.Scheme)
		})
		if err != nil {
			return fmt.Errorf("environment CR %s: %w", envCRName, err)
		}

		log.FromContext(ctx).Info("environment CR reconciled", "environment", envCRName)
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
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopzerov1.Tenant{}).
		Owns(&loopzerov1.Environment{}).
		Named("tenant").
		Complete(&TenantReconciler{
			Client:             mgr.GetClient(),
			Scheme:             mgr.GetScheme(),
			BuildClient:        buildClient,
			RuntimeClient:      runtimeClient,
			GitProvisioner:     gitProvisioner,
			ArgoCDProvisioner:  argoCDProvisioner,
			MinIOProvisioner:   cfg.MinIOProvisioner,
			GiteaURL:           giteaURL,
			WorkloadClusterURL: workloadClusterURL,
			Credentials:        cfg.Credentials,
			RegistryCIDRs:      cfg.RegistryCIDRs,
		})
}
