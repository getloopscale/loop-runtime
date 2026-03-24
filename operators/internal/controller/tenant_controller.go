package controller

import (
	"context"
	"fmt"
	"time"

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
	"github.com/loopzero/loopzero/operators/internal/gitea"
	miniop "github.com/loopzero/loopzero/operators/internal/minio"
	"github.com/loopzero/loopzero/pkg/naming"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

const tenantFinalizer = "loopzero.io/tenant-cleanup"

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
type TenantReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	BuildClient        client.Client               // targets the build cluster
	RuntimeClient      client.Client               // targets the runtime cluster
	GitProvisioner     gitea.GitRepoProvisioner
	ArgoCDProvisioner  argocd.Provisioner
	MinIOProvisioner   miniop.CredentialProvisioner // nil disables per-tenant MinIO credential isolation
	GiteaURL           string
	WorkloadClusterURL string // URL of the workload cluster API server
	MinIOEndpoint      string // MinIO endpoint injected into per-tenant build Secrets
}

// TenantControllerConfig holds configuration for the TenantController.
type TenantControllerConfig struct {
	GiteaURL           string
	WorkloadClusterURL string
	MinIOEndpoint      string                      // MinIO endpoint injected into per-tenant build Secrets
	MinIOProvisioner   miniop.CredentialProvisioner // nil disables per-tenant MinIO credential isolation
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

	// Detect spec change: if generation advanced while Phase is Active, transition back to Provisioning
	// so all reconcile steps re-execute (condition-gated checks compare ObservedGeneration).
	if tenant.Status.Phase == loopzerov1.TenantPhaseActive &&
		tenant.Generation != tenant.Status.ObservedGeneration {
		tenant.Status.Phase = loopzerov1.TenantPhase(
			tenantSM.Transition(statemachine.TenantActive, statemachine.TenantSpecChanged),
		)
	}

	// S1.6: Reconcile Gitea repo
	// Skip if GitRepoReady is already True at current generation
	if !conditions.IsConditionTrueForGeneration(tenant.Status.Conditions, conditions.TypeGitRepoReady, tenant.Generation) {
		if err := r.reconcileGitRepo(ctx, &tenant); err != nil {
			conditions.SetCondition(&tenant.Status.Conditions, conditions.TypeGitRepoReady,
				metav1.ConditionFalse, "ReconcileFailed", err.Error(), tenant.Generation)
			if statusErr := r.Status().Update(ctx, &tenant); statusErr != nil {
				logger.Error(statusErr, "failed to update tenant status", "tenant", tenant.Name)
			}
			return ctrl.Result{}, err
		}
	}

	// S1.6b: Reconcile ArgoCD Application per environment
	// Skip if ArgoCDAppReady is already True at current generation
	if !conditions.IsConditionTrueForGeneration(tenant.Status.Conditions, conditions.TypeArgoCDAppReady, tenant.Generation) {
		if err := r.reconcileArgoCDApp(ctx, &tenant); err != nil {
			conditions.SetCondition(&tenant.Status.Conditions, conditions.TypeArgoCDAppReady,
				metav1.ConditionFalse, "ReconcileFailed", err.Error(), tenant.Generation)
			if statusErr := r.Status().Update(ctx, &tenant); statusErr != nil {
				logger.Error(statusErr, "failed to update tenant status", "tenant", tenant.Name)
			}
			return ctrl.Result{}, err
		}
	}

	// S1.5: Reconcile build namespace + QuotaEnforced
	// Skip if BOTH BuildNamespaceReady AND QuotaEnforced are True at current generation
	// (both conditions are set inside reconcileBuildNamespace)
	if !conditions.IsConditionTrueForGeneration(tenant.Status.Conditions, conditions.TypeBuildNamespaceReady, tenant.Generation) ||
		!conditions.IsConditionTrueForGeneration(tenant.Status.Conditions, conditions.TypeQuotaEnforced, tenant.Generation) {
		if err := r.reconcileBuildNamespace(ctx, &tenant); err != nil {
			conditions.SetCondition(&tenant.Status.Conditions, conditions.TypeBuildNamespaceReady,
				metav1.ConditionFalse, "ReconcileFailed", err.Error(), tenant.Generation)
			if statusErr := r.Status().Update(ctx, &tenant); statusErr != nil {
				logger.Error(statusErr, "failed to update tenant status", "tenant", tenant.Name)
			}
			return ctrl.Result{}, err
		}
	}

	// S14.3: Reconcile PriorityClass based on tenant plan
	// Skip if PriorityClassReady is already True at current generation
	if !conditions.IsConditionTrueForGeneration(tenant.Status.Conditions, conditions.TypePriorityClassReady, tenant.Generation) {
		if err := r.reconcilePriorityClass(ctx, &tenant); err != nil {
			conditions.SetCondition(&tenant.Status.Conditions, conditions.TypePriorityClassReady,
				metav1.ConditionFalse, "ReconcileFailed", err.Error(), tenant.Generation)
			if statusErr := r.Status().Update(ctx, &tenant); statusErr != nil {
				logger.Error(statusErr, "failed to update tenant status", "tenant", tenant.Name)
			}
			return ctrl.Result{}, err
		}
	}

	// Create Environment CRs for each spec.environments[] (delegating namespace/RBAC/NetworkPolicy to EnvironmentController)
	if err := r.reconcileEnvironmentCRs(ctx, &tenant); err != nil {
		if statusErr := r.Status().Update(ctx, &tenant); statusErr != nil {
			logger.Error(statusErr, "failed to update tenant status", "tenant", tenant.Name)
		}
		return ctrl.Result{}, err
	}

	// S1.7: Phase transition — Active when all conditions are True (via state machine)
	if allConditionsTrue(tenant.Status.Conditions) && tenant.Status.Phase == loopzerov1.TenantPhaseProvisioning {
		tenant.Status.Phase = loopzerov1.TenantPhase(
			tenantSM.Transition(statemachine.TenantProvisioning, statemachine.TenantAllReady),
		)
	}
	tenant.Status.ObservedGeneration = tenant.Generation

	// Update status
	if err := r.Status().Update(ctx, &tenant); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("reconciled tenant", "tenant", tenant.Name, "phase", tenant.Status.Phase)
	return ctrl.Result{}, nil
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

// allConditionsTrue checks if all required conditions are True.
func allConditionsTrue(conditions []metav1.Condition) bool {
	for _, req := range requiredProvisioningConditions {
		found := false
		for _, c := range conditions {
			if c.Type == req && c.Status == metav1.ConditionTrue {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// reconcileDelete handles the verify-then-remove finalizer pattern for Tenant deletion.
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
	if tenant.Status.Phase != loopzerov1.TenantPhaseDeleting {
		tenant.Status.Phase = loopzerov1.TenantPhase(
			tenantSM.Transition(currentPhase, statemachine.TenantDelete),
		)
	}
	if err := r.Status().Update(ctx, tenant); err != nil {
		return ctrl.Result{}, err
	}

	// Phase 1: Issue deletes (idempotent)
	// Delete ArgoCD Applications for this tenant
	if err := r.ArgoCDProvisioner.DeleteApplications(ctx, tenant.Name); err != nil {
		logger.Info("non-fatal cleanup failure, continuing", "step", "argocd-cleanup", "error", err, "tenant", tenant.Name)
	}

	// Delete ArgoCD AppProject for this tenant (MOD-5)
	if err := r.ArgoCDProvisioner.DeleteAppProject(ctx, tenant.Name); err != nil {
		logger.Info("non-fatal cleanup failure, continuing", "step", "argocd-appproject-cleanup", "error", err, "tenant", tenant.Name)
	}

	// Delete namespaces on build cluster (build-{tenant})
	var buildNsList corev1.NamespaceList
	if err := r.BuildClient.List(ctx, &buildNsList, client.MatchingLabels{"loopzero.io/tenant": tenant.Name}); err != nil {
		logger.Info("non-fatal cleanup failure on build cluster", "step", "list-build-ns", "error", err)
	} else {
		for i := range buildNsList.Items {
			if buildNsList.Items[i].DeletionTimestamp.IsZero() {
				if err := r.BuildClient.Delete(ctx, &buildNsList.Items[i]); err != nil && !errors.IsNotFound(err) {
					return ctrl.Result{}, err
				}
			}
		}
	}

	// Delete namespaces on runtime cluster ({tenant}-{env})
	var runtimeNsList corev1.NamespaceList
	if err := r.RuntimeClient.List(ctx, &runtimeNsList, client.MatchingLabels{"loopzero.io/tenant": tenant.Name}); err != nil {
		logger.Info("non-fatal cleanup failure on runtime cluster", "step", "list-runtime-ns", "error", err)
	} else {
		for i := range runtimeNsList.Items {
			if runtimeNsList.Items[i].DeletionTimestamp.IsZero() {
				if err := r.RuntimeClient.Delete(ctx, &runtimeNsList.Items[i]); err != nil && !errors.IsNotFound(err) {
					return ctrl.Result{}, err
				}
			}
		}
	}

	// Archive Gitea repo (soft delete, idempotent)
	if err := r.GitProvisioner.ArchiveRepo(ctx, tenant.Spec.GitOps.RepoName); err != nil {
		logger.Info("non-fatal cleanup failure, continuing", "step", "gitea-archive", "error", err, "repo", tenant.Spec.GitOps.RepoName)
	}

	// Phase 2: Verify all namespaces are gone on both clusters
	var remainingBuild corev1.NamespaceList
	_ = r.BuildClient.List(ctx, &remainingBuild, client.MatchingLabels{"loopzero.io/tenant": tenant.Name})
	var remainingRuntime corev1.NamespaceList
	_ = r.RuntimeClient.List(ctx, &remainingRuntime, client.MatchingLabels{"loopzero.io/tenant": tenant.Name})
	remaining := len(remainingBuild.Items) + len(remainingRuntime.Items)
	if remaining > 0 {
		logger.Info("namespaces still exist, requeuing", "count", remaining)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
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
		giteaURL = "http://gitea-http.loopzero-system.svc.cluster.local:3000"
	}
	workloadClusterURL := cfg.WorkloadClusterURL
	if workloadClusterURL == "" {
		workloadClusterURL = "https://kubernetes.default.svc"
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopzerov1.Tenant{}).
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
			MinIOEndpoint:      cfg.MinIOEndpoint,
		})
}
