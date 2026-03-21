package controller

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/argocd"
	"github.com/loopzero/loopzero/operators/internal/gitea"
	miniop "github.com/loopzero/loopzero/operators/internal/minio"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

const tenantFinalizer = "loopzero.io/tenant-cleanup"

// tenantSM is the Tenant lifecycle state machine (spec §5).
// Phase transitions go through this machine — direct assignment is forbidden.
var tenantSM = statemachine.NewTenantMachine()

// TenantReconciler reconciles a Tenant object.
type TenantReconciler struct {
	client.Client
	GitProvisioner     gitea.GitRepoProvisioner
	ArgoCDProvisioner  argocd.Provisioner
	MinIOProvisioner   miniop.CredentialProvisioner // REVIEW-BLOCKER-3: per-tenant MinIO credentials
	GiteaURL           string
	WorkloadClusterURL string // URL of the workload cluster API server
	MinIOEndpoint      string // REVIEW-BLOCKER-3: MinIO endpoint for build Secret
}

// TenantControllerConfig holds configuration for the TenantController.
type TenantControllerConfig struct {
	GiteaURL           string
	WorkloadClusterURL string
	MinIOEndpoint      string // REVIEW-BLOCKER-3: MinIO endpoint injected into per-tenant build Secrets
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

	// S1.2: Reconcile namespaces + ResourceQuota + LimitRange
	// Skip if NamespacesReady is already True at current generation (crash recovery optimization)
	if !conditionIsTrue(tenant.Status.Conditions, loopzerov1.ConditionNamespacesReady, tenant.Generation) {
		if err := r.reconcileNamespaces(ctx, &tenant); err != nil {
			loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionNamespacesReady,
				metav1.ConditionFalse, "ReconcileFailed", err.Error(), tenant.Generation)
			if statusErr := r.Status().Update(ctx, &tenant); statusErr != nil {
				logger.Error(statusErr, "failed to update tenant status", "tenant", tenant.Name)
			}
			return ctrl.Result{}, err
		}
	}

	// S1.3: Reconcile RBAC (ServiceAccount + Role + RoleBinding)
	// Skip if RBACReady is already True at current generation
	if !conditionIsTrue(tenant.Status.Conditions, loopzerov1.ConditionRBACReady, tenant.Generation) {
		if err := r.reconcileRBAC(ctx, &tenant); err != nil {
			loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionRBACReady,
				metav1.ConditionFalse, "ReconcileFailed", err.Error(), tenant.Generation)
			if statusErr := r.Status().Update(ctx, &tenant); statusErr != nil {
				logger.Error(statusErr, "failed to update tenant status", "tenant", tenant.Name)
			}
			return ctrl.Result{}, err
		}
	}

	// S1.4: Reconcile NetworkPolicies (deny-all + allow-ingress + allow-egress)
	// Skip if NetworkPolicyReady is already True at current generation
	if !conditionIsTrue(tenant.Status.Conditions, loopzerov1.ConditionNetworkPolicyReady, tenant.Generation) {
		if err := r.reconcileNetworkPolicies(ctx, &tenant); err != nil {
			loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionNetworkPolicyReady,
				metav1.ConditionFalse, "ReconcileFailed", err.Error(), tenant.Generation)
			if statusErr := r.Status().Update(ctx, &tenant); statusErr != nil {
				logger.Error(statusErr, "failed to update tenant status", "tenant", tenant.Name)
			}
			return ctrl.Result{}, err
		}
	}

	// S1.6: Reconcile Gitea repo
	// Skip if GitRepoReady is already True at current generation
	if !conditionIsTrue(tenant.Status.Conditions, loopzerov1.ConditionGitRepoReady, tenant.Generation) {
		if err := r.reconcileGitRepo(ctx, &tenant); err != nil {
			loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionGitRepoReady,
				metav1.ConditionFalse, "ReconcileFailed", err.Error(), tenant.Generation)
			if statusErr := r.Status().Update(ctx, &tenant); statusErr != nil {
				logger.Error(statusErr, "failed to update tenant status", "tenant", tenant.Name)
			}
			return ctrl.Result{}, err
		}
	}

	// S1.6b: Reconcile ArgoCD Application per environment
	// Skip if ArgoCDAppReady is already True at current generation
	if !conditionIsTrue(tenant.Status.Conditions, loopzerov1.ConditionArgoCDAppReady, tenant.Generation) {
		if err := r.reconcileArgoCDApp(ctx, &tenant); err != nil {
			loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionArgoCDAppReady,
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
	if !conditionIsTrue(tenant.Status.Conditions, loopzerov1.ConditionBuildNamespaceReady, tenant.Generation) ||
		!conditionIsTrue(tenant.Status.Conditions, loopzerov1.ConditionQuotaEnforced, tenant.Generation) {
		if err := r.reconcileBuildNamespace(ctx, &tenant); err != nil {
			loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionBuildNamespaceReady,
				metav1.ConditionFalse, "ReconcileFailed", err.Error(), tenant.Generation)
			if statusErr := r.Status().Update(ctx, &tenant); statusErr != nil {
				logger.Error(statusErr, "failed to update tenant status", "tenant", tenant.Name)
			}
			return ctrl.Result{}, err
		}
	}

	// S14.3: Reconcile PriorityClass based on tenant plan
	// Skip if PriorityClassReady is already True at current generation
	if !conditionIsTrue(tenant.Status.Conditions, loopzerov1.ConditionPriorityClassReady, tenant.Generation) {
		if err := r.reconcilePriorityClass(ctx, &tenant); err != nil {
			loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionPriorityClassReady,
				metav1.ConditionFalse, "ReconcileFailed", err.Error(), tenant.Generation)
			if statusErr := r.Status().Update(ctx, &tenant); statusErr != nil {
				logger.Error(statusErr, "failed to update tenant status", "tenant", tenant.Name)
			}
			return ctrl.Result{}, err
		}
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

// conditionIsTrue checks if a specific condition is True AND its ObservedGeneration
// matches the current resource generation. This enables condition-gated reconciliation:
// if a step already succeeded at the current generation, we skip re-executing it.
// When the spec changes (generation bumps), all steps are re-executed.
func conditionIsTrue(conditions []metav1.Condition, conditionType string, generation int64) bool {
	for _, c := range conditions {
		if c.Type == conditionType {
			return c.Status == metav1.ConditionTrue && c.ObservedGeneration == generation
		}
	}
	return false
}

// requiredProvisioningConditions lists all conditions that must be True
// for a tenant to transition from PROVISIONING to ACTIVE.
// Add new conditions here when adding new reconciliation steps.
var requiredProvisioningConditions = []string{
	loopzerov1.ConditionGitRepoReady,
	loopzerov1.ConditionNamespacesReady,
	loopzerov1.ConditionRBACReady,
	loopzerov1.ConditionNetworkPolicyReady,
	loopzerov1.ConditionArgoCDAppReady,
	loopzerov1.ConditionBuildNamespaceReady,
	loopzerov1.ConditionQuotaEnforced,
	loopzerov1.ConditionPriorityClassReady,
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

	// Delete all namespaces with tenant label
	var nsList corev1.NamespaceList
	if err := r.List(ctx, &nsList, client.MatchingLabels{"loopzero.io/tenant": tenant.Name}); err != nil {
		return ctrl.Result{}, err
	}
	for i := range nsList.Items {
		if nsList.Items[i].DeletionTimestamp.IsZero() {
			if err := r.Delete(ctx, &nsList.Items[i]); err != nil && !errors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}
	}

	// Archive Gitea repo (soft delete, idempotent)
	if err := r.GitProvisioner.ArchiveRepo(ctx, tenant.Spec.GitOps.RepoName); err != nil {
		logger.Info("non-fatal cleanup failure, continuing", "step", "gitea-archive", "error", err, "repo", tenant.Spec.GitOps.RepoName)
	}

	// Phase 2: Verify all namespaces are gone
	var remaining corev1.NamespaceList
	if err := r.List(ctx, &remaining, client.MatchingLabels{"loopzero.io/tenant": tenant.Name}); err != nil {
		return ctrl.Result{}, err
	}
	if len(remaining.Items) > 0 {
		logger.Info("namespaces still exist, requeuing", "count", len(remaining.Items))
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

// SetupTenantController registers the TenantController with the manager.
// cfg.GiteaURL configures the Gitea base URL for ArgoCD Application repo references.
// If empty, it defaults to the in-cluster Gitea service URL.
// cfg.WorkloadClusterURL configures the workload cluster API server URL for ArgoCD Applications.
// If empty, it defaults to "https://kubernetes.default.svc" (backward compatible for single-cluster setups).
func SetupTenantController(mgr ctrl.Manager, gitProvisioner gitea.GitRepoProvisioner, argoCDProvisioner argocd.Provisioner, cfg TenantControllerConfig) error {
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
			GitProvisioner:     gitProvisioner,
			ArgoCDProvisioner:  argoCDProvisioner,
			GiteaURL:           giteaURL,
			WorkloadClusterURL: workloadClusterURL,
		})
}
