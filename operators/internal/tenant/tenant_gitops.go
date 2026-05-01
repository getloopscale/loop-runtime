// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/argocd"
	"github.com/loopzero/loopzero/operators/internal/conditions"
	"github.com/loopzero/loopzero/operators/internal/constants"
	"github.com/loopzero/loopzero/operators/internal/shared"
	"github.com/loopzero/loopzero/pkg/naming"
)

// reconcileGitRepo ensures the GitOps repository exists via the GitRepoProvisioner.
func (r *TenantReconciler) reconcileGitRepo(ctx context.Context, tenant *loopzerov1.Tenant) error {
	ctx, span := shared.StartChildSpan(ctx, "tenant.gitops.ensure")
	defer span.End()

	repoURL, err := r.GitProvisioner.EnsureRepo(ctx, tenant.Spec.GitOps.RepoName, tenant.Spec.GitOps.Branch)
	if err != nil {
		shared.RecordError(span, err)
		return fmt.Errorf("gitea repo %s: %w", tenant.Spec.GitOps.RepoName, err)
	}

	tenant.Status.GitOpsRepo = repoURL
	conditions.SetCondition(&tenant.Status.Conditions, conditions.TypeGitRepoReady,
		metav1.ConditionTrue, "RepoReady", "git repository provisioned", tenant.Generation)
	return nil
}

// reconcileArgoCDApp ensures one ArgoCD Application CR exists per environment for the tenant.
// The Application connects the tenant's Gitea repo to the workload namespace via ArgoCD.
func (r *TenantReconciler) reconcileArgoCDApp(ctx context.Context, tenant *loopzerov1.Tenant) (err error) {
	ctx, span := shared.StartChildSpan(ctx, "tenant.argocd.ensure")
	defer span.End()
	defer func() {
		if err != nil {
			shared.RecordError(span, err)
		}
	}()
	// arch_op_010: Use tenant.Status.GitOpsRepo (set by reconcileGitRepo, which runs first)
	// as the canonical repo URL instead of re-deriving it with a hardcoded org name.
	repoURL := tenant.Status.GitOpsRepo

	// MOD-5: Ensure AppProject exists for tenant RBAC isolation before creating Applications.
	// Each tenant gets its own AppProject that restricts source repos and destination namespaces.
	// Collect destination namespaces from tenant environments for RBAC scoping.
	destNamespaces := make([]string, 0, len(tenant.Spec.Environments))
	for _, env := range tenant.Spec.Environments {
		destNS, nsErr := naming.EnvironmentNamespace(tenant.Name, env.Name)
		if nsErr == nil {
			destNamespaces = append(destNamespaces, destNS)
		}
	}
	projSpec := argocd.AppProjectSpec{
		Name:                  tenant.Name,
		Namespace:             constants.DefaultArgoCDNamespace,
		TenantID:              tenant.Name,
		SourceRepos:           []string{repoURL},
		DestinationNamespaces: destNamespaces,
		DestinationServer:     r.WorkloadClusterURL, // SEC-007: explicit server instead of wildcard
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

		// ADR-0008: Pass CellID so the provisioner can resolve the correct ArgoCD instance.
		cellID := tenant.Spec.CellID
		if cellID == "" {
			cellID = "cell-default"
		}

		spec := argocd.ApplicationSpec{
			Name:           appName,
			Namespace:      constants.DefaultArgoCDNamespace,
			TenantID:       tenant.Name,
			RepoURL:        repoURL,
			TargetRevision: tenant.Spec.GitOps.Branch,
			Path:           path,
			DestinationNS:  destNS,
			DestinationURL: r.WorkloadClusterURL,
			CellID:         cellID,
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
