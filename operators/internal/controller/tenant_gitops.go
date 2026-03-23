package controller

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/argocd"
	"github.com/loopzero/loopzero/operators/internal/conditions"
	"github.com/loopzero/loopzero/pkg/naming"
)

// reconcileGitRepo ensures the GitOps repository exists via the GitRepoProvisioner.
func (r *TenantReconciler) reconcileGitRepo(ctx context.Context, tenant *loopzerov1.Tenant) error {
	repoURL, err := r.GitProvisioner.EnsureRepo(ctx, tenant.Spec.GitOps.RepoName, tenant.Spec.GitOps.Branch)
	if err != nil {
		return fmt.Errorf("gitea repo %s: %w", tenant.Spec.GitOps.RepoName, err)
	}

	tenant.Status.GitOpsRepo = repoURL
	conditions.SetCondition(&tenant.Status.Conditions, conditions.TypeGitRepoReady,
		metav1.ConditionTrue, "RepoReady", "git repository provisioned", tenant.Generation)
	return nil
}

// reconcileArgoCDApp ensures one ArgoCD Application CR exists per environment for the tenant.
// The Application connects the tenant's Gitea repo to the workload namespace via ArgoCD.
func (r *TenantReconciler) reconcileArgoCDApp(ctx context.Context, tenant *loopzerov1.Tenant) error {
	repoURL := fmt.Sprintf("%s/loopzero/%s.git", r.GiteaURL, tenant.Spec.GitOps.RepoName)

	// MOD-5: Ensure AppProject exists for tenant RBAC isolation before creating Applications.
	// Each tenant gets its own AppProject that restricts source repos and destination namespaces.
	projSpec := argocd.AppProjectSpec{
		Name:        tenant.Name,
		Namespace:   "argocd",
		TenantID:    tenant.Name,
		SourceRepos: []string{repoURL},
	}
	if err := r.ArgoCDProvisioner.EnsureAppProject(ctx, projSpec); err != nil {
		return fmt.Errorf("argocd appproject %s: %w", tenant.Name, err)
	}

	for _, env := range tenant.Spec.Environments {
		appName := fmt.Sprintf("%s-%s", tenant.Name, env.Name)
		destNS, err := naming.EnvironmentNamespace(tenant.Name, env.Name)
		if err != nil {
			return fmt.Errorf("invalid namespace name for env %s: %w", env.Name, err)
		}
		// BLOCKER-3: Path scoped to environment directory.
		// Manifest hierarchy is {environment}/{projectID}/manifests.yaml.
		// Each ArgoCD Application watches only its environment's subtree,
		// preventing cross-environment contamination (e.g., staging manifests applied to production).
		path := env.Name

		spec := argocd.ApplicationSpec{
			Name:           appName,
			Namespace:      "argocd",
			TenantID:       tenant.Name,
			RepoURL:        repoURL,
			TargetRevision: tenant.Spec.GitOps.Branch,
			Path:           path,
			DestinationNS:  destNS,
			DestinationURL: r.WorkloadClusterURL,
		}

		if err := r.ArgoCDProvisioner.EnsureApplication(ctx, spec); err != nil {
			return fmt.Errorf("argocd application %s: %w", appName, err)
		}
		log.FromContext(ctx).Info("argocd application reconciled", "application", appName)
	}

	conditions.SetCondition(&tenant.Status.Conditions, conditions.TypeArgoCDAppReady,
		metav1.ConditionTrue, "ApplicationsReady", "ArgoCD applications provisioned", tenant.Generation)
	return nil
}
