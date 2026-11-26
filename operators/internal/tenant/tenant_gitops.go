// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/argocd"
	"github.com/getloopscale/loop-runtime/operators/internal/conditions"
	"github.com/getloopscale/loop-runtime/operators/internal/constants"
	"github.com/getloopscale/loop-runtime/operators/internal/reconcileloop"
	"github.com/getloopscale/loop-runtime/pkg/domain"
	"github.com/getloopscale/loop-runtime/pkg/naming"
)

// canonicalDestNamespace returns the namespace name to use as an ArgoCD
// App destination for a tenant environment. When projectSlug is set,
// returns the canonical formula `loopzero-<tenant>-<project>-<env>` that
// matches what the manifest renderer emits (renderer_helpers.go:
// namespace). When projectSlug is empty (pre-#69 Tenant CRs created
// before the API started populating env.Project), falls back to the
// legacy two-segment `loopzero-<tenant>-<env>` formula so the controller
// continues reconciling existing installations without manual repair.
//
// J-23 LIVE catch 2026-06-07 #69: the renderer canonical refactor
// (commit a567cdbb5) updated the manifest namespace to the canonical
// three-segment formula, but tenant_gitops.go was emitting destinations
// via the legacy two-segment formula because there was no place in the
// CR spec for the project name. Result: ArgoCD destination mismatch ->
// OutOfSync forever.
//
// T2.1 of plan 2026-06-09-m2-operator-canonical-fix v1.4: empty
// projectSlug is now a CONTRACT VIOLATION rather than a silent fallback
// to the legacy two-segment formula. T1.1 ships CRD-level enforcement
// (TenantEnvironmentEntry.Project required + RFC 1123 Pattern) so every
// caller in production code (tenant_gitops.go:143, :186 + tenant_controller.go:337)
// is guaranteed to pass a non-empty project. A silent fallback is forbidden — it was
// the smoking gun that caused
// all 4 M2 LIVE workarounds — fail-fast with LoopZeroError is canonical
// (feedback_m2_canonical_drift_operator_renderer memory).
func canonicalDestNamespace(tenantSlug, projectSlug, envName string) (string, error) {
	if projectSlug == "" {
		return "", fmt.Errorf("canonicalDestNamespace: Project is required (T1.1 contract — empty projectSlug used to silently fall back to legacy two-segment namespace; that path is removed in T2.1)")
	}
	return naming.Namespace(tenantSlug, projectSlug, envName)
}

// buildApplicationSpecs returns the canonical ArgoCD Application spec for every
// (project, environment) of the tenant plus every active preview.
//
// # WHY THIS IS ONE FUNCTION
//
// Two callers need the identity of these Applications: reconcileArgoCDApp, which
// CREATES them, and pruneOrphanPreviewApps, which DELETES everything outside the set.
// The delete carries resources-finalizer.argocd.argoproj.io, so it takes the tenant's
// workload with it. While the two computed the set independently, any divergence made
// the operator delete, in cascade, exactly what it had just created. They now read from
// here, so divergence is not unlikely — it is unrepresentable.
//
// The name comes from pkg/naming.ArgoAppName: {tenant}-{project}-{env}. Omitting the
// project made two projects of one tenant collide on a single Application with
// conflicting destinations (issue #271; measured at 1519 writes in 5 minutes).
//
// The path is {env}/{project} because that is where the manifests are actually written
// (api/internal/gitops/gitea_pusher.go:136). Pointing at {env} alone let an Application
// read every project's manifests in that environment.
func (r *TenantReconciler) buildApplicationSpecs(
	tenant *loopzerov1.Tenant,
	previewEnvs []loopzerov1.PreviewEnvironment,
	repoURL string,
) ([]argocd.ApplicationSpec, error) {
	cellID := tenant.Spec.CellID
	if cellID == "" {
		cellID = domain.DefaultCellID
	}

	specs := make([]argocd.ApplicationSpec, 0, len(tenant.Spec.Environments)+len(previewEnvs))

	for _, env := range tenant.Spec.Environments {
		appName, err := naming.ArgoAppName(tenant.Name, env.Project, env.Name)
		if err != nil {
			return nil, fmt.Errorf("argocd application name for %s/%s: %w", env.Project, env.Name, err)
		}
		destNS, err := canonicalDestNamespace(tenant.Name, env.Project, env.Name)
		if err != nil {
			return nil, fmt.Errorf("invalid namespace name for env %s: %w", env.Name, err)
		}
		specs = append(specs, argocd.ApplicationSpec{
			Name:           appName,
			Namespace:      constants.DefaultArgoCDNamespace,
			TenantID:       tenant.Name,
			RepoURL:        repoURL,
			TargetRevision: tenant.Spec.GitOps.Branch,
			Path:           filepath.Join(env.Name, env.Project),
			DestinationNS:  destNS,
			DestinationURL: r.WorkloadClusterURL,
			CellID:         cellID,
			Environment:    env.Name,
		})
	}

	// J-23 bug #6 / ADR D5: previews stay OUT of Tenant.spec.environments but still need
	// an Application so the operator-created namespace receives the build manifests.
	for _, pe := range previewEnvs {
		envName := fmt.Sprintf("preview-pr-%d", pe.Spec.PRNumber)
		appName, err := naming.ArgoAppName(tenant.Name, pe.Spec.ProjectID, envName)
		if err != nil {
			return nil, fmt.Errorf("argocd application name for preview %s/%s: %w",
				pe.Spec.ProjectID, envName, err)
		}
		ref, refErr := domain.NewEnvRef(tenant.Name, pe.Spec.ProjectID, envName)
		if refErr != nil {
			return nil, fmt.Errorf("invalid EnvRef for preview env %s: %w", envName, refErr)
		}
		destNS, err := ref.Namespace()
		if err != nil {
			return nil, fmt.Errorf("invalid namespace name for preview env %s: %w", envName, err)
		}
		specs = append(specs, argocd.ApplicationSpec{
			Name:           appName,
			Namespace:      constants.DefaultArgoCDNamespace,
			TenantID:       tenant.Name,
			RepoURL:        repoURL,
			TargetRevision: tenant.Spec.GitOps.Branch,
			Path:           filepath.Join(envName, pe.Spec.ProjectID),
			DestinationNS:  destNS,
			DestinationURL: r.WorkloadClusterURL,
			CellID:         cellID,
			Environment:    envName,
		})
	}

	return specs, nil
}

// expectedApplicationNames is the set pruneOrphanPreviewApps protects from deletion.
// It is derived from buildApplicationSpecs on purpose — see that function's godoc.
func (r *TenantReconciler) expectedApplicationNames(
	tenant *loopzerov1.Tenant,
	previewEnvs []loopzerov1.PreviewEnvironment,
	repoURL string,
) (map[string]bool, error) {
	specs, err := r.buildApplicationSpecs(tenant, previewEnvs, repoURL)
	if err != nil {
		return nil, err
	}
	expected := make(map[string]bool, len(specs))
	for _, s := range specs {
		expected[s.Name] = true
	}
	return expected, nil
}

// buildAppProjectDestNamespaces returns the AppProject spec.destinations
// namespace list for a tenant. T2.2 of plan 2026-06-09-m2-operator-canonical-fix
// v1.4: extracted from reconcileArgoCDApp so the (project, env) iteration is
// unit-testable without spinning up envtest, and so the previous silent
// error-drop (`if nsErr == nil { append }`) is replaced with fail-loud propagation.
// The rule is stated rather than cited: a pointer can rot, a stated rule cannot.
//
// Inputs:
//   - tenant.Spec.Environments contributes one destination per (Project, Name) tuple.
//     With M2 Tenant CRs (T1.1 ships Project required), this is naturally the
//     (project × env) Cartesian product because every Environments entry carries
//     its own Project field. Multi-project tenants emit multiple destinations
//     under the same AppProject — exactly the LIVE workaround manually applied
//     in T6.1 testing of plan 2026-06-08-pre-release-test-rigor.
//   - previewEnvs contributes one destination per active PreviewEnvironment CR,
//     also in the canonical 3-segment form (PreviewController is M2-correct).
//
// Errors:
//   - Any canonicalDestNamespace error propagates upward and aborts the
//     AppProject reconcile (no partial destinations written). EC-15 absorption:
//     better to fail-loud than emit an AppProject with missing destinations
//     that would silently break sync for half the tenant's projects.
func buildAppProjectDestNamespaces(tenant *loopzerov1.Tenant, previewEnvs []loopzerov1.PreviewEnvironment) ([]string, error) {
	destNamespaces := make([]string, 0, len(tenant.Spec.Environments)+len(previewEnvs))
	for _, env := range tenant.Spec.Environments {
		destNS, nsErr := canonicalDestNamespace(tenant.Name, env.Project, env.Name)
		if nsErr != nil {
			return nil, fmt.Errorf("env (project=%q, name=%q): %w", env.Project, env.Name, nsErr)
		}
		destNamespaces = append(destNamespaces, destNS)
	}
	for _, pe := range previewEnvs {
		// All envs (production, staging, preview-pr-N) share the SAME
		// uniform namespace formula: "loopzero-<tenant>-<project>-<env>".
		// Canonical refactor 2026-06-07 — no if/else branching on env type.
		envName := fmt.Sprintf("preview-pr-%d", pe.Spec.PRNumber)
		// T1.3.c of plan 2026-06-11-code-review-remediation v1.2 — call-site
		// migration to the canonical EnvRef pattern per ADR D2. The
		// NewEnvRef constructor enforces the same non-empty invariants
		// pkg/naming.Namespace did, with stronger typing.
		ref, refErr := domain.NewEnvRef(tenant.Name, pe.Spec.ProjectID, envName)
		if refErr != nil {
			return nil, fmt.Errorf("preview env (project=%q, pr=%d): %w", pe.Spec.ProjectID, pe.Spec.PRNumber, refErr)
		}
		destNS, nsErr := ref.Namespace()
		if nsErr != nil {
			return nil, fmt.Errorf("preview env (project=%q, pr=%d): %w", pe.Spec.ProjectID, pe.Spec.PRNumber, nsErr)
		}
		destNamespaces = append(destNamespaces, destNS)
	}
	return destNamespaces, nil
}

// assertRepoNameOwnedByTenant enforces that Spec.GitOps.RepoName carries
// the tenant name as a delimited component — either as a prefix
// (`<tenantName>-...`), as a hyphen-delimited middle segment
// (`...-<tenantName>-...`), or as a hyphen-prefixed suffix
// (`...-<tenantName>`). The delimiter requirement prevents adversarial
// substring collisions like a Tenant `tn-victim` pretending to own a
// repo named `tn-acme-gitops` (no `-tn-victim-`/`-tn-victim`/`tn-victim-`
// occurrence anywhere in the name → rejected).
//
// Audit gap M10 F2 / §6.E.4c: the canonical production pattern is
// `tn-acme-gitops` (api/internal/k8s/tenant_cr_manager.go:308 emits
// `{tenantID}-gitops`). The operator-test fixtures historically use
// `loopzero-tenant-<tenantName>`. Both shapes are accepted because both
// embed the tenant name as a delimited component. A reference that
// does NOT contain the tenant name as a component (typo or attack)
// is rejected before the provisioner is given the foreign name.
func assertRepoNameOwnedByTenant(tenantName, repoName string) error {
	if tenantName == "" {
		return fmt.Errorf("tenant-ownership invariant: empty tenantName (audit M10 F2 §6.E.4c)")
	}
	prefix := tenantName + "-"
	middle := "-" + tenantName + "-"
	suffix := "-" + tenantName
	if !strings.HasPrefix(repoName, prefix) &&
		!strings.Contains(repoName, middle) &&
		!strings.HasSuffix(repoName, suffix) {
		return fmt.Errorf(
			"tenant-ownership invariant: Spec.GitOps.RepoName %q must contain %q as a hyphen-delimited component (audit M10 F2 §6.E.4c)",
			repoName, tenantName,
		)
	}
	return nil
}

// reconcileGitRepo ensures the GitOps repository exists via the GitRepoProvisioner.
func (r *TenantReconciler) reconcileGitRepo(ctx context.Context, tenant *loopzerov1.Tenant) error {
	ctx, span := reconcileloop.StartChildSpan(ctx, "tenant.gitops.ensure")
	defer span.End()

	// Audit gap M10 F2 / §6.E.4c — refuse to act on a RepoName that does
	// not belong to this tenant. The provisioner is given the raw string
	// from Spec.GitOps.RepoName, so a malformed or adversarial value
	// would otherwise cause cross-tenant repo creation/conflict.
	if err := assertRepoNameOwnedByTenant(tenant.Name, tenant.Spec.GitOps.RepoName); err != nil {
		reconcileloop.RecordError(span, err)
		return err
	}

	repoURL, err := r.GitProvisioner.EnsureRepo(ctx, tenant.Spec.GitOps.RepoName, tenant.Spec.GitOps.Branch)
	if err != nil {
		reconcileloop.RecordError(span, err)
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
	ctx, span := reconcileloop.StartChildSpan(ctx, "tenant.argocd.ensure")
	defer span.End()
	defer func() {
		if err != nil {
			reconcileloop.RecordError(span, err)
		}
	}()
	// arch_op_010: Use tenant.Status.GitOpsRepo (set by reconcileGitRepo, which runs first)
	// as the canonical repo URL instead of re-deriving it with a hardcoded org name.
	repoURL := tenant.Status.GitOpsRepo

	// MOD-5: Ensure AppProject exists for tenant RBAC isolation before creating Applications.
	// Each tenant gets its own AppProject that restricts source repos and destination namespaces.
	// Collect destination namespaces from tenant environments for RBAC scoping.
	//
	// LIVE catch 2026-06-07 J-23 bug #6 (preview-pr-N visibility): also
	// collect destination namespaces from PreviewEnvironment CRs of this
	// tenant. ADR D5 keeps PreviewEnvironment OUT of Tenant.spec.environments
	// (strict CRD separation), but they still need an ArgoCD Application
	// + AppProject destination to be deployable. Emitting them here keeps
	// the AppProject single-source-of-truth.
	//
	// A failed List is fatal for this step. The same slice decides which
	// Applications the prune below protects, and an empty stand-in would
	// delete every live preview of the tenant (the ArgoCD finalizer cascades
	// the delete into the workload).
	previewEnvs, err := r.listPreviewEnvsForTenant(ctx, tenant.Name)
	if err != nil {
		return fmt.Errorf("argocd appproject %s: %w", tenant.Name, err)
	}
	destNamespaces, err := buildAppProjectDestNamespaces(tenant, previewEnvs)
	if err != nil {
		return fmt.Errorf("argocd appproject %s: build destinations: %w", tenant.Name, err)
	}
	projSpec := argocd.AppProjectSpec{
		Name:                  tenant.Name,
		Namespace:             constants.DefaultArgoCDNamespace,
		TenantID:              tenant.Name,
		SourceRepos:           []string{repoURL},
		DestinationNamespaces: destNamespaces,
		DestinationServer:     r.WorkloadClusterURL, // SEC-007: explicit server (used as fallback when CellID empty)
		// F-08 fix part 3 (2026-05-07 cleanroom dogfood): pass CellID so
		// EnsureAppProject can use name-based destinations matching what
		// EnsureApplication already writes (ApplicationSpec.CellID below).
		// Without this, AppProject.destinations had server: URL while
		// Application.destination had name: cellID — ArgoCD validation
		// rejected the App with InvalidSpecError on every sync.
		CellID: tenant.Spec.CellID,
	}
	if err := r.ArgoCDProvisioner.EnsureAppProject(ctx, projSpec); err != nil {
		return fmt.Errorf("argocd appproject %s: %w", tenant.Name, err)
	}

	specs, err := r.buildApplicationSpecs(tenant, previewEnvs, repoURL)
	if err != nil {
		return err
	}
	for _, spec := range specs {
		if err := r.ArgoCDProvisioner.EnsureApplication(ctx, spec); err != nil {
			return fmt.Errorf("argocd application %s: %w", spec.Name, err)
		}
		log.FromContext(ctx).Info("argocd application reconciled", "application", spec.Name)
	}

	// #71 LIVE catch 2026-06-07 — prune orphan preview Apps.
	//
	// The emit loops above create/update Apps for the ACTIVE spec.environments
	// and the ACTIVE PreviewEnvironments. They never delete Apps for closed
	// PRs because the input set (previewEnvs) doesn't include deleted
	// previews -- the "active preview" gate in listPreviewEnvsForTenant
	// strips Deleted/Deleting phases before the loop ever sees them.
	//
	// Without a prune step, every PR ever opened against the tenant leaves
	// behind an ArgoCD App stuck in OutOfSync/Missing forever after close.
	// Ownership, declared: the OPERATOR owns the Application's `.spec`. The api
	// writes the input (Tenant.Spec.Environments), triggers operations (/sync,
	// /operation) and deletes only on the administrative-eviction path — it never
	// writes spec, and must not start. ADR
	// docs/decisions.md (B-050).
	//
	// This paragraph used to say instead that preview_controller.go:609 carried a
	// comment claiming "Application CR deletion is owned by the API (best-effort)"
	// while the api never wired it. A comment documenting that ANOTHER comment is
	// wrong is the symptom of ownership nobody declared; the line above is the
	// declaration, so the symptom is no longer needed. The canonical place to prune is here at the
	// tenant operator because (a) this loop already knows the desired set,
	// (b) the watcher (#70) fires reconcile on PreviewEnv delete events, so
	// the prune naturally cascades from PR close → CR delete → tenant
	// reconcile → orphan App removed.
	if err := r.pruneOrphanPreviewApps(ctx, tenant, previewEnvs, repoURL); err != nil {
		// Soft-fail: pruning is best-effort housekeeping. Don't reset the
		// ArgoCDAppReady condition or fail the whole reconcile because the
		// active-Apps emit above already succeeded.
		log.FromContext(ctx).Info("argocd prune: failed (continuing)",
			"tenant", tenant.Name, "error", err.Error())
	}

	conditions.SetCondition(&tenant.Status.Conditions, conditions.TypeArgoCDAppReady,
		metav1.ConditionTrue, "ApplicationsReady", "ArgoCD applications provisioned", tenant.Generation)
	return nil
}

// pruneOrphanPreviewApps deletes ArgoCD Application CRs that belong to the
// tenant but no longer correspond to an active PreviewEnvironment. The
// "expected" set is built from the spec.environments names + the active
// previewEnvs slice; any tenant-labeled App outside that set is considered
// orphan and removed.
//
// #71 LIVE catch 2026-06-07 — the prune side of the preview lifecycle that
// preview_controller.go:609 hand-waved to the API but the API never wired.
// repoURL is threaded through only because the expected set is derived from the very
// specs the reconcile emits — it does not influence which names are protected. Deriving
// beats recomputing: see buildApplicationSpecs.
func (r *TenantReconciler) pruneOrphanPreviewApps(
	ctx context.Context,
	tenant *loopzerov1.Tenant,
	previewEnvs []loopzerov1.PreviewEnvironment,
	repoURL string,
) error {
	// Derived from buildApplicationSpecs, never recomputed here: this set decides what
	// gets DELETED, and the delete cascades through the ArgoCD finalizer into the
	// tenant's workload. A second formula that drifted from the first would make the
	// operator destroy exactly what it had just created.
	expected, err := r.expectedApplicationNames(tenant, previewEnvs, repoURL)
	if err != nil {
		return fmt.Errorf("expected application names for prune: %w", err)
	}

	existing, err := r.ArgoCDProvisioner.ListApplicationNames(ctx, tenant.Name)
	if err != nil {
		return fmt.Errorf("list applications for prune: %w", err)
	}

	for _, name := range existing {
		if expected[name] {
			continue
		}
		if err := r.ArgoCDProvisioner.DeleteApplication(ctx, name); err != nil {
			// Continue on individual failures — best-effort cleanup.
			log.FromContext(ctx).Info("argocd prune: delete failed (continuing)",
				"application", name, "error", err.Error())
			continue
		}
		log.FromContext(ctx).Info("argocd application pruned (orphan)", "application", name)
	}
	return nil
}

// listPreviewEnvsForTenant returns active PreviewEnvironment CRs that
// belong to the given tenant. Filter is in-memory because the list is
// expected to be small (tens of previews max per tenant). Soft-fails
// to nil on error so the gitops reconcile can proceed without preview
// destinations.
func (r *TenantReconciler) listPreviewEnvsForTenant(ctx context.Context, tenantID string) ([]loopzerov1.PreviewEnvironment, error) {
	var all loopzerov1.PreviewEnvironmentList
	if err := r.List(ctx, &all); err != nil {
		return nil, fmt.Errorf("list preview envs: %w", err)
	}
	out := make([]loopzerov1.PreviewEnvironment, 0)
	for _, pe := range all.Items {
		if pe.Spec.TenantID != tenantID {
			continue
		}
		// Skip CRs in terminal/deleting state — no AppProject
		// destination needed for those.
		if pe.Status.Phase == loopzerov1.PreviewPhaseDeleted || pe.Status.Phase == loopzerov1.PreviewPhaseDeleting {
			continue
		}
		out = append(out, pe)
	}
	return out, nil
}
