// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant

import (
	"context"
	"fmt"
	"time"

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
	"github.com/loopzero/loopzero/operators/internal/harbor"
	miniop "github.com/loopzero/loopzero/operators/internal/minio"
	"github.com/loopzero/loopzero/operators/internal/shared"
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
	HarborClient       harbor.Provisioner           // nil disables Harbor integration (backward compat with DOCR)
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
	HarborClient           harbor.Provisioner           // nil disables Harbor integration
	HarborRegistryHost     string                       // e.g. "harbor.dev.loopzero.example"
	Credentials            BuildCredentials             // storage + registry credentials
	RegistryCIDRs          []string                     // CIDR ranges for container registry (RISK-007)
	MaxConcurrentReconciles int                         // 0 defaults to 5 (MEET-052: backpressure for bulk deletion)
}

// tenantDetailTypes are the per-step conditions that SetReadinessSummary
// classifies for kstatus. Per ADR-0025 D3.
var tenantDetailTypes = []string{
	conditions.TypeGitRepoReady,
	conditions.TypeQuotaEnforced,
	conditions.TypeBuildNamespaceReady,
	conditions.TypePriorityClassReady,
	conditions.TypeArgoCDAppReady,
	// HarborProjectReady is only required when HarborClient is configured;
	// included here so kstatus.Compute waits for it in Harbor mode. In DOCR
	// mode the controller never sets it, so SetReadinessSummary classifies
	// as Reconciling (missing detail). Acceptable: DOCR-mode tenants stay
	// Reconciling forever from a kstatus standpoint UNTIL the per-tenant
	// requiredProvisioningConditions() omits Harbor — TODO follow-up: add
	// runtime-conditional detailTypes selector.
	conditions.TypeHarborProjectReady,
}

// Reconcile handles the reconciliation loop for Tenant resources.
func (r *TenantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
	ctx, span := shared.StartReconcileSpan(ctx, "tenant", req.Name, req.Namespace)
	defer span.End()
	start := time.Now()
	defer func() {
		if retErr != nil {
			shared.RecordError(span, retErr)
		} else {
			shared.SetSuccess(span)
		}
		shared.ReconcileOutcome("tenant", start, retErr)
	}()

	logger := log.FromContext(ctx)

	var tenant loopzerov1.Tenant
	if err := r.Get(ctx, req.NamespacedName, &tenant); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// kstatus universal: SetReadinessSummary MUST be the LAST status
	// mutation per ADR-0025 D3 condition #1. Defer ensures it runs even
	// on early return / panic from internal helpers.
	defer func() {
		latest := &loopzerov1.Tenant{}
		if err := r.Get(ctx, req.NamespacedName, latest); err != nil {
			return
		}
		if !latest.DeletionTimestamp.IsZero() {
			return
		}
		// Adjust detailTypes for DOCR mode (HarborClient nil → skip Harbor).
		dt := tenantDetailTypes
		if r.HarborClient == nil {
			dt = dt[:len(dt)-1] // drop trailing TypeHarborProjectReady
		}
		conditions.SetReadinessSummary(&latest.Status.Conditions, latest.Generation, dt)
		_ = r.Status().Update(ctx, latest)
	}()

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
		For(&loopzerov1.Tenant{}, shared.ForOption()).
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
