// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package tenant implements the Tenant + Environment controllers
// (operator binary `loopzero-tenant-operator` per ADR-0023). Owns the
// per-tenant lifecycle: provisioning, multi-cell placement resolve
// via cellrouting.RuntimeClientFactory, GitOps repo creation, credential
// rotation, deletion (soft + grace + cleanup). Cross-cluster
// reconciliation routes through ClusterCredential CR lookup by
// `spec.cellID`.
package tenant

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crcontroller "sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/argocd"
	"github.com/getloopscale/loopzero/operators/internal/cellrouting"
	"github.com/getloopscale/loopzero/operators/internal/conditions"
	"github.com/getloopscale/loopzero/operators/internal/constants"
	"github.com/getloopscale/loopzero/operators/internal/gitea"
	"github.com/getloopscale/loopzero/operators/internal/harbor"
	miniop "github.com/getloopscale/loopzero/operators/internal/minio"
	"github.com/getloopscale/loopzero/operators/internal/reconcileloop"
	"github.com/getloopscale/loopzero/operators/internal/vault"
	"github.com/getloopscale/loopzero/pkg/correlation"
	"github.com/getloopscale/loopzero/pkg/naming"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// tenantFinalizer uses the centralized constant.
const tenantFinalizer = constants.FinalizerTenant

// tenantSM is the Tenant lifecycle state machine (spec §5).
// Phase transitions go through this machine — direct assignment is forbidden.
var tenantSM = statemachine.NewTenantTypedMachine()

// TenantReconciler reconciles a Tenant object.
//
// Multi-cluster design: the controller runs in the mgmt cluster but creates
// resources on the build and runtime clusters using separate clients.
//   - r.Client        → mgmt cluster (Tenant CRDs, status, Gitea, ArgoCD)
//   - r.BuildClient   → build cluster (build-{tenant} namespaces, quotas, secrets)
//   - r.RuntimeFactory → the runtime cluster of the tenant's cell, by Spec.CellID
//     (PriorityClass, Harbor pull secrets). There is no default runtime cluster:
//     a tenant pinned to cell-1 must never have its runtime objects written to cell-0.
//
// ARCH-002: Field usage by sub-file:
//   - tenant_controller.go: Client, Scheme (environment CRs)
//   - tenant_build.go:      BuildClient, MinIOProvisioner, Credentials
//   - tenant_gitops.go:     Client, GitProvisioner, ArgoCDProvisioner, GiteaURL, WorkloadClusterURL
//   - tenant_credentials.go: BuildClient, Credentials (DOToken, RegistryDockerConfig)
//   - tenant_priority.go:   RuntimeFactory
//   - tenant_harbor.go:     HarborClient, HarborRegistryHost, BuildClient, RuntimeFactory
//
// ADR-0026 D4 / plan T4.1: kubebuilder:rbac markers for the LOCAL
// (mgmt cluster) RBAC. controller-gen rbac generates ClusterRole rules
// from these. Cross-cluster RBAC (BuildClient, RuntimeFactory) is
// hand-maintained per ADR-0026 condition #2 and lives in
// charts/loopzero-tenant-operator/templates/cross-cluster-rbac.yaml.
//
// +kubebuilder:rbac:groups=loopzero.io,resources=tenants,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=loopzero.io,resources=tenants/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=loopzero.io,resources=tenants/finalizers,verbs=update
// +kubebuilder:rbac:groups=loopzero.io,resources=environments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=loopzero.io,resources=clustercredentials,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=resourcequotas,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=scheduling.k8s.io,resources=priorityclasses,verbs=get;list;watch;create;update;patch
//
// FB-#4 (live boot iter 12): tenant-operator's cellrouting.RuntimeClientFactory
// LISTs ClusterCredentials to resolve cell→runtime-cluster mapping.
// Marker added to satisfy clustercredentials.loopzero.io list permission;
// otherwise tenant-operator crashes with "User \"system:serviceaccount:
// loopzero-system:loopzero-tenant-operator\" cannot list resource clustercredentials"
// on every reconcile of any cell-aware Tenant.
//
// Vocabulary: the mapping named above relates two DIFFERENT things.
// A cell is a logical tenancy partition (the cell_id column); a runtime
// cluster is the machine that cell's workloads run on. Rewriting this as
// "cell→cell-cluster" — the migration the cluster-topology-naming ADR
// floated for this row, considered and rejected on 2026-09-06 — would
// make the sentence circular and erase the mapping it names. Nor is
// "runtime" here apimachinery's runtime package (k8s.io/apimachinery/pkg/runtime, NOT Go's stdlib runtime — the stdlib one has no Scheme type, which is what `*runtime.Scheme` in this tree proves), imported as
// k8s.io/apimachinery/pkg/runtime and used for the Scheme field below.
// See docs/decisions.md
// § Decision on the domain term (2026-09-06).
type TenantReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	BuildClient        client.Client                    // targets the build cluster
	RuntimeFactory     cellrouting.RuntimeClientFactory // resolves the runtime cluster of the tenant cell
	GitProvisioner     gitea.GitRepoProvisioner
	ArgoCDProvisioner  argocd.Provisioner
	MinIOProvisioner   miniop.CredentialProvisioner // nil disables per-tenant MinIO credential isolation
	HarborClient       harbor.Provisioner           // nil disables Harbor integration (backward compat with DOCR)
	HarborRegistryHost string                       // e.g. "harbor.dev.loopzero.example" — used in Docker config.json
	// T3.3 of openbao-adoption v1.3: per-tenant Vault Transit key management.
	// nil disables (vault disabled mode); reconcileTransitKey skips step + sets no condition.
	VaultClient        vault.TenantTransitClient
	GiteaURL           string
	WorkloadClusterURL string           // URL of the workload cluster API server
	Credentials        BuildCredentials // storage + registry credentials for build namespace secrets
	RegistryCIDRs      []string         // CIDR ranges for container registry egress restriction (RISK-007)
}

// TenantControllerConfig holds configuration for the TenantController.
type TenantControllerConfig struct {
	GiteaURL                string
	WorkloadClusterURL      string
	MinIOProvisioner        miniop.CredentialProvisioner // nil disables per-tenant MinIO credential isolation
	HarborClient            harbor.Provisioner           // nil disables Harbor integration
	HarborRegistryHost      string                       // e.g. "harbor.dev.loopzero.example"
	Credentials             BuildCredentials             // storage + registry credentials
	RegistryCIDRs           []string                     // CIDR ranges for container registry (RISK-007)
	MaxConcurrentReconciles int                          // 0 defaults to 5 (MEET-052: backpressure for bulk deletion)
}

// runtimeClient resolves the runtime cluster of the tenant's cell.
func (r *TenantReconciler) runtimeClient(ctx context.Context, tenant *loopzerov1.Tenant) (client.Client, error) {
	c, err := r.RuntimeFactory.For(ctx, tenant.Spec.CellID)
	if err != nil {
		return nil, fmt.Errorf("tenant %s (cellID=%q): runtime cluster client: %w", tenant.Name, tenant.Spec.CellID, err)
	}
	return c, nil
}

// Reconcile handles the reconciliation loop for Tenant resources.
func (r *TenantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
	ctx, span := reconcileloop.StartReconcileSpan(ctx, "tenant", req.Name, req.Namespace)
	defer span.End()
	start := time.Now()
	defer func() {
		if retErr != nil {
			reconcileloop.RecordError(span, retErr)
		} else {
			reconcileloop.SetSuccess(span)
		}
		reconcileloop.ReconcileOutcome("tenant", start, retErr)
	}()

	logger := log.FromContext(ctx)

	var tenant loopzerov1.Tenant
	if err := r.Get(ctx, req.NamespacedName, &tenant); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// T1.1 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap: when the CR
	// carries the `loopzero.io/correlation-id` annotation (set by the API for
	// HTTP-driven flows), inject it into the reconcile context so structured
	// logs + audit_log writes trace back to the originating request. No-op
	// for background reconciles (watch event without annotation).
	ctx = correlation.WithID(ctx, tenant.GetAnnotations()["loopzero.io/correlation-id"])

	// Handle deletion with verify-then-remove finalizer pattern
	if !tenant.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &tenant)
	}

	// Add finalizer if not present
	if err := reconcileloop.EnsureFinalizer(ctx, r.Client, &tenant, tenantFinalizer, req.NamespacedName); err != nil {
		return ctrl.Result{}, err
	}

	// Single-status-writer merge-patch base (CLAUDE.md; ADR
	// 2026-08-12-status-writer-contract). Replaces two re-fetch-then-restore
	// sites (B-085), which re-read the Tenant purely to obtain a resourceVersion
	// that would not conflict and put the stale status back on top — so a write
	// landing in between vanished with no 409 and no log.
	//
	// THE PLACEMENT IS THE WHOLE POINT, and an earlier version of this change put
	// it above EnsureFinalizer, which is wrong. `client.MergeFrom` diffs the whole
	// object, so any field that changed between the snapshot and the write lands
	// in the patch body — including `metadata.resourceVersion`. EnsureFinalizer
	// does Update+Get, bumping the RV on the FIRST reconcile of every Tenant, so a
	// snapshot taken before it ships a STALE optimistic lock: measured against a
	// real apiserver, the stale RV is honoured and returns 409.
	//
	// Worse than merely wrong: the old re-fetch happened immediately before the
	// write, so its conflict window was near zero. A snapshot taken before the
	// eight network-bound steps below would widen that window to the whole
	// reconcile — a reliability regression dressed as a contract fix.
	//
	// Captured HERE: after EnsureFinalizer's metadata write, before any status
	// mutation. Same placement, for the same reason, as
	// environment_controller.go, which documents it.
	base := tenant.DeepCopy()

	// Set Phase to Provisioning if empty (via state machine)
	if tenant.Status.Phase == "" {
		tenant.Status.Phase = loopzerov1.TenantPhase(
			tenantSM.Transition(statemachine.TPhEmpty, statemachine.TPEventCreate),
		)
	}

	// All reconcile steps run unconditionally (idempotent via EnsureX/CreateOrUpdate).
	// No condition-gated skipping — this ensures drift is corrected even when conditions
	// were set True by a previous controller version that no longer applies.
	if err := r.runSteps(ctx, &tenant, base, r.provisioningSteps(ctx, &tenant)); err != nil {
		return ctrl.Result{}, err
	}

	// S1.7: Phase transition — Active when all conditions are True (via state machine)
	if conditions.AllConditionsTrue(tenant.Status.Conditions, r.requiredProvisioningConditions()...) && tenant.Status.Phase == loopzerov1.TenantPhaseProvisioning {
		tenant.Status.Phase = loopzerov1.TenantPhase(
			tenantSM.Transition(statemachine.TPhProvisioning, statemachine.TPEventAllReady),
		)
	}
	tenant.Status.ObservedGeneration = tenant.Generation

	// Final status write. No re-fetch: the patch names only the fields that
	// changed since `base`, so it does not overwrite a concurrent write to fields
	// it never read — which is what the restore-on-top used to do.
	//
	// It carries no resourceVersion only BECAUSE `base` is captured after the
	// finalizer write; see the snapshot comment above. That is asserted by
	// TestTenantStatusPatch_CarriesNoResourceVersion, on the emitted bytes.
	if err := r.patchStatus(ctx, &tenant, base); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("reconciled tenant", "tenant", tenant.Name, "phase", tenant.Status.Phase)
	return ctrl.Result{}, nil
}

// provisioningSteps is the ordered set of reconcile steps a Tenant goes through, and
// the single place that declares it. It is a named method rather than a literal inside
// Reconcile so that a test can assert over the STEPS THEMSELVES — reading the source
// file for step names would compare the spelling of the list instead of the list.
//
// Deliberately absent: the per-tenant Vault Transit key. Provisioning is api-side (ADR
// 2026-08-12-tenant-transit-key-ownership, B-042) — the api creates the key lazily on
// the first encrypt 404, and this step never had a production implementation to run.
// TestTransitKeyProvisioning_IsNotAnOperatorReconcileStep guards the absence.
func (r *TenantReconciler) provisioningSteps(ctx context.Context, tenant *loopzerov1.Tenant) []reconcileStep {
	return []reconcileStep{
		{conditions.TypeGitRepoReady, func() error { return r.reconcileGitRepo(ctx, tenant) }},
		{conditions.TypeArgoCDAppReady, func() error { return r.reconcileArgoCDApp(ctx, tenant) }},
		{conditions.TypeBuildNamespaceReady, func() error { return r.reconcileBuildNamespace(ctx, tenant) }},
		{conditions.TypePriorityClassReady, func() error { return r.reconcilePriorityClass(ctx, tenant) }},
		{conditions.TypeHarborProjectReady, func() error { return r.ensureHarborProject(ctx, tenant) }},
		{"", func() error { return r.ensureHarborRobotAccounts(ctx, tenant) }},
		{"", func() error { return r.reconcileEnvironmentCRs(ctx, tenant) }},
	}
}

// reconcileStep pairs a condition type with its reconcile function.
// If condType is empty, no condition is set on failure (used for steps without conditions).
type reconcileStep struct {
	condType string
	fn       func() error
}

// runSteps executes reconcile steps sequentially, setting conditions on failure.
// On error it writes the condition with a merge patch (best-effort) and returns
// the error. It used to re-fetch first, to dodge a stale resourceVersion; the
// patch has none, so the re-fetch went with it (B-085).
// `base` is the snapshot taken by Reconcile right after its Get. It is a
// parameter rather than something re-derived here: deriving it inside would mean
// a second read, which is the re-fetch this migration removed.
func (r *TenantReconciler) runSteps(ctx context.Context, tenant, base *loopzerov1.Tenant, steps []reconcileStep) error {
	logger := log.FromContext(ctx)
	for _, step := range steps {
		if err := step.fn(); err != nil {
			if step.condType != "" {
				conditions.SetCondition(&tenant.Status.Conditions, step.condType,
					metav1.ConditionFalse, "ReconcileFailed", err.Error(), tenant.Generation)
			}
			// Best-effort status write on the failure path. The re-fetch that
			// stood here existed only to dodge a stale resourceVersion; the patch
			// names only what changed, so there is nothing stale to dodge — see
			// the snapshot comment in Reconcile for why the placement of `base`
			// is what makes that true (B-085).
			if statusErr := r.patchStatus(ctx, tenant, base); statusErr != nil {
				logger.Error(statusErr, "failed to update tenant status", "tenant", tenant.Name)
			}
			return err
		}
	}
	return nil
}

// requiredProvisioningConditions returns the conditions that must be True
// for a tenant to transition from PROVISIONING to ACTIVE, and the detail
// conditions the kstatus readiness summary classifies (ADR-0025 D3): one rule
// for both, so a tenant is never Ready on a condition it is not required to
// meet, nor Active while the summary waits on one it never gets.
// HarborProjectReady belongs to it only when HarborClient is configured
// (DOCR mode never writes it).
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
	// TypeTransitKeyAvailable is deliberately NOT required, and the reason is a trap
	// worth naming: it used to be appended whenever VaultClient != nil, and VaultClient
	// still exists — it carries the shredding half of the interface (B-075). But the
	// step that SET the condition was removed in 2026-08-16 when provisioning moved to
	// the api (ADR 2026-08-12-tenant-transit-key-ownership, B-042).
	//
	// Requiring a condition nobody writes means every Tenant on a deployment that wires
	// the shredder would sit in Provisioning forever, waiting for a step that no longer
	// runs. Guarded by TestTenantActivation_DoesNotWaitForAConditionNobodyWrites.
	return required
}

// reconcileEnvironmentCRs creates/updates Environment CRs for each spec.environments[],
// then garbage-collects orphaned Environment CRs that are no longer in the spec.
// Each Environment CR is owned by the Tenant CR (cascade delete).
// buildEnvironmentCRNames returns the set of Environment CR names this tenant should have,
// one per (project, environment) entry in the spec.
//
// # WHY THIS IS ONE FUNCTION
//
// Two callers need this set: the create/update loop, and the garbage collector that deletes
// every Environment CR outside it. Deleting an Environment CR makes EnvironmentReconciler
// delete the NAMESPACE on the runtime cluster (environment_controller.go, reconcileDelete),
// so a `desired` set that drifted from what the loop creates would wipe the tenants'
// namespaces. They read from here, so divergence is unrepresentable rather than unlikely.
//
// The name carries the project because the CR's content already does: spec.project and
// spec.namespace.name are both project-scoped. While it did not, two projects of one tenant
// sharing an env name collapsed onto a single CR through CreateOrUpdate, and the loser
// silently received no namespace, RBAC or NetworkPolicy. Measured on app-dev 2026-08-08:
// 5 of 9 declared (project, environment) tuples had no Environment CR at all (issue #273).
func buildEnvironmentCRNames(tenant *loopzerov1.Tenant) (map[string]bool, error) {
	names := make(map[string]bool, len(tenant.Spec.Environments))
	for _, env := range tenant.Spec.Environments {
		name, err := naming.EnvironmentCRName(tenant.Name, env.Project, env.Name)
		if err != nil {
			return nil, fmt.Errorf("environment CR name for %s/%s: %w", env.Project, env.Name, err)
		}
		names[name] = true
	}
	return names, nil
}

// The EnvironmentController watches these CRs and manages namespaces/RBAC/NetworkPolicy.
func (r *TenantReconciler) reconcileEnvironmentCRs(ctx context.Context, tenant *loopzerov1.Tenant) error {
	logger := log.FromContext(ctx)

	// Build the desired set of Environment CR names from spec.
	// Derived, never recomputed: this set decides which Environment CRs are GC'd, and
	// deleting one deletes the tenant's namespace on the runtime cluster.
	desired, err := buildEnvironmentCRNames(tenant)
	if err != nil {
		return err
	}

	// Create/update loop — one Environment CR per spec entry.
	for _, env := range tenant.Spec.Environments {
		envCRName, nameErr := naming.EnvironmentCRName(tenant.Name, env.Project, env.Name)
		if nameErr != nil {
			return fmt.Errorf("environment CR name for %s/%s: %w", env.Project, env.Name, nameErr)
		}
		// #69 LIVE catch 2026-06-07: the Environment CR carries the
		// workload namespace name. Use the same canonical-with-fallback
		// helper as tenant_gitops.go so envController->namespace creation
		// matches the destination ArgoCD picks for sync. Without this,
		// the namespace would be created at the legacy path and ArgoCD
		// would fail to apply manifests targeting the canonical name.
		nsName, err := canonicalDestNamespace(tenant.Name, env.Project, env.Name)
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
			// T3.1 of plan 2026-06-09-m2-operator-canonical-fix v1.4:
			// propagate Project from TenantEnvironmentEntry to the child
			// Environment CR so EnvironmentController can compute the
			// canonical M2 namespace independently and surface 63-char
			// overflow via Status condition (EC-3 absorption).
			envCR.Spec.Project = env.Project
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
func SetupTenantController(mgr ctrl.Manager, gitProvisioner gitea.GitRepoProvisioner, argoCDProvisioner argocd.Provisioner, buildClient client.Client, runtimeFactory cellrouting.RuntimeClientFactory, cfg TenantControllerConfig) error {
	if runtimeFactory == nil {
		return fmt.Errorf("tenant controller: a runtime client factory is required")
	}
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
	// #70 LIVE catch 2026-06-07: tenant_gitops.reconcileArgoCDApp iterates
	// PreviewEnvironment CRs and emits ArgoCD App + AppProject destinations
	// for each active one. Without a watcher mapping PreviewEnv events back
	// to the owning Tenant, the tenant-operator only reconciles on Tenant
	// CR changes -- so a freshly-created PreviewEnv (PR opened) never gets
	// an ArgoCD App until an operator runs `kubectl annotate` to force a
	// reconcile. The watcher closes the gap: any change to a
	// PreviewEnvironment enqueues a Reconcile request for its
	// Spec.TenantID, ensuring the ArgoCD App appears within one reconcile
	// of the CR being Active.
	mapPreviewToTenant := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		pe, ok := obj.(*loopzerov1.PreviewEnvironment)
		if !ok || pe.Spec.TenantID == "" {
			return nil
		}
		return []reconcile.Request{{NamespacedName: types.NamespacedName{
			Name:      pe.Spec.TenantID,
			Namespace: pe.Namespace,
		}}}
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&loopzerov1.Tenant{}, reconcileloop.ForOption()).
		Owns(&loopzerov1.Environment{}).
		Watches(&loopzerov1.PreviewEnvironment{}, mapPreviewToTenant).
		Named("tenant").
		WithOptions(crcontroller.Options{MaxConcurrentReconciles: maxConcurrent}).
		Complete(&TenantReconciler{
			Client:             mgr.GetClient(),
			Scheme:             mgr.GetScheme(),
			BuildClient:        buildClient,
			RuntimeFactory:     runtimeFactory,
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

// patchStatus is the single status writer of this controller. It folds the
// kstatus readiness summary in as the last mutation (ADR-0025 D3) and persists
// with a merge patch off base, so every write carries a Ready condition that
// agrees with the conditions written beside it.
func (r *TenantReconciler) patchStatus(ctx context.Context, tenant, base *loopzerov1.Tenant) error {
	conditions.SetReadinessSummaryViaPkg(&tenant.Status.Conditions, tenant.Generation, r.requiredProvisioningConditions())
	return r.Status().Patch(ctx, tenant, client.MergeFrom(base))
}
