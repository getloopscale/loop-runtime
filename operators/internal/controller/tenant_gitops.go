package controller

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/argocd"
)

// reconcileGitRepo ensures the GitOps repository exists via the GitRepoProvisioner.
func (r *TenantReconciler) reconcileGitRepo(ctx context.Context, tenant *loopzerov1.Tenant) error {
	repoURL, err := r.GitProvisioner.EnsureRepo(ctx, tenant.Spec.GitOps.RepoName, tenant.Spec.GitOps.Branch)
	if err != nil {
		return fmt.Errorf("gitea repo %s: %w", tenant.Spec.GitOps.RepoName, err)
	}

	tenant.Status.GitOpsRepo = repoURL
	loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionGitRepoReady,
		metav1.ConditionTrue, "RepoReady", "git repository provisioned", tenant.Generation)
	return nil
}

// reconcileArgoCDApp ensures one ArgoCD Application CR exists per environment for the tenant.
// The Application connects the tenant's Gitea repo to the workload namespace via ArgoCD.
func (r *TenantReconciler) reconcileArgoCDApp(ctx context.Context, tenant *loopzerov1.Tenant) error {
	repoURL := fmt.Sprintf("http://gitea-http.loopzero-system.svc.cluster.local:3000/loopzero/%s.git",
		tenant.Spec.GitOps.RepoName)

	for _, env := range tenant.Spec.Environments {
		appName := fmt.Sprintf("%s-%s", tenant.Name, env.Name)
		destNS := fmt.Sprintf("%s-%s", tenant.Name, env.Name)
		// Path matches what the workflow's gitops-push creates: {project}/{env}/
		// Since ArgoCD Application is per-env and the repo is per-tenant,
		// we use "." (root) to catch all project manifests for this environment.
		// TODO: When multi-project support is added, create one Application per project-env.
		path := "."

		spec := argocd.ApplicationSpec{
			Name:           appName,
			Namespace:      "argocd",
			TenantID:       tenant.Name,
			RepoURL:        repoURL,
			TargetRevision: tenant.Spec.GitOps.Branch,
			Path:           path,
			DestinationNS:  destNS,
			DestinationURL: "https://kubernetes.default.svc",
		}

		if err := r.ArgoCDProvisioner.EnsureApplication(ctx, spec); err != nil {
			return fmt.Errorf("argocd application %s: %w", appName, err)
		}
		log.FromContext(ctx).Info("argocd application reconciled", "application", appName)
	}

	loopzerov1.SetCondition(&tenant.Status.Conditions, loopzerov1.ConditionArgoCDAppReady,
		metav1.ConditionTrue, "ApplicationsReady", "ArgoCD applications provisioned", tenant.Generation)
	return nil
}
