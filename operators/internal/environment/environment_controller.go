// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package environment holds the Environment reconciler and the resources it
// owns per environment — namespace, quota, network policy and RBAC.
//
// Its status has exactly ONE writer, which persists with a merge patch off a
// base snapshot taken right after the initial Get: a merge patch carries no
// resourceVersion, so it never 409s against a concurrent spec write by another
// controller.
package environment

import (
	"context"
	"fmt"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/cellrouting"
	"github.com/getloopscale/loopzero/operators/internal/conditions"
	"github.com/getloopscale/loopzero/operators/internal/constants"
	"github.com/getloopscale/loopzero/operators/internal/reconcileloop"
	"github.com/getloopscale/loopzero/pkg/correlation"
	"github.com/getloopscale/loopzero/pkg/domain"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

const environmentFinalizer = constants.FinalizerEnvironment

// environmentSM is the Environment lifecycle state machine (spec §5).
// Phase transitions go through this machine — direct assignment is forbidden (Rule 8).
var environmentSM = statemachine.NewEnvironmentTypedMachine()

// EnvironmentReconciler reconciles an Environment object.
//
// Multi-cluster design: the Environment CR lives in the mgmt cluster,
// but namespace/RBAC/NetworkPolicy resources are created on the runtime cluster.
//   - r.Client          -> mgmt cluster (Environment CR + Tenant CR reads)
//   - r.RuntimeClient   -> default runtime cluster (legacy single-cell fallback)
//   - r.RuntimeFactory  -> cell-aware factory: returns the right runtime client
//     based on tenant.spec.cellID (fix-all-platform-issues-plan T1.1, parity
//     with operators/internal/controller/environment_controller.go from
//     cell-1-activation-plan iter 4 BUG-FIX).
//
// All namespace/RBAC/NetworkPolicy/Quota writes MUST go through
// runtimeClientForEnv() — direct r.RuntimeClient calls bypass cell awareness.
//
// ADR-0026 D4 / plan T4.1: kubebuilder:rbac markers for the LOCAL
// (mgmt cluster) RBAC. Cross-cluster RBAC for runtime cluster
// (namespaces/RBAC/NetworkPolicies in tenant envs) is hand-maintained.
//
// +kubebuilder:rbac:groups=loopzero.io,resources=environments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=loopzero.io,resources=environments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=loopzero.io,resources=environments/finalizers,verbs=update
// +kubebuilder:rbac:groups=loopzero.io,resources=tenants,verbs=get;list;watch
type EnvironmentReconciler struct {
	client.Client
	RuntimeClient    client.Client
	RuntimeFactory   cellrouting.RuntimeClientFactory // nil = single-cell mode (legacy)
	Scheme           *runtime.Scheme
	IngressNamespace string // Namespace where ingress controller (Traefik) runs. Default: loopzero-system.
}

// runtimeClientForEnv resolves the right runtime client for the given
// Environment by looking up the parent Tenant's spec.cellID. Mirrors the
// cell-aware lookup pattern from operators/internal/controller/.
func (r *EnvironmentReconciler) runtimeClientForEnv(ctx context.Context, env *loopzerov1.Environment) (client.Client, error) {
	if r.RuntimeFactory == nil {
		if r.RuntimeClient == nil {
			return nil, fmt.Errorf("environment %s: no RuntimeClient and no RuntimeFactory configured", env.Name)
		}
		return r.RuntimeClient, nil
	}

	var tenant loopzerov1.Tenant
	if err := r.Get(ctx, client.ObjectKey{Name: env.Spec.TenantRef.Name, Namespace: env.Namespace}, &tenant); err != nil {
		if err2 := r.Get(ctx, client.ObjectKey{Name: env.Spec.TenantRef.Name}, &tenant); err2 != nil {
			if r.RuntimeClient != nil {
				return r.RuntimeClient, nil
			}
			return nil, fmt.Errorf("environment %s: tenant %s not found and no fallback: %w",
				env.Name, env.Spec.TenantRef.Name, err2)
		}
	}

	cellID := tenant.Spec.CellID
	c, err := r.RuntimeFactory.For(ctx, cellID)
	if err != nil {
		if r.RuntimeClient != nil {
			return r.RuntimeClient, nil
		}
		return nil, fmt.Errorf("environment %s (cellID=%q): factory error and no fallback: %w",
			env.Name, cellID, err)
	}
	return c, nil
}

// environmentDetailTypes per ADR-0025 D3 — TypePullSecretReady is
// informational only (per types.go:44 comment) and EXCLUDED from kstatus
// classification so DOCR/standalone-multi profiles without source pull
// secret don't permanently report Reconciling.
var environmentDetailTypes = []string{
	conditions.TypeEnvNamespaceReady,
	conditions.TypeEnvRBACReady,
	conditions.TypeEnvNetworkPolicyReady,
	conditions.TypeEnvQuotaReady,
}

// Reconcile handles the reconciliation loop for Environment resources.
func (r *EnvironmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
	ctx, span := reconcileloop.StartReconcileSpan(ctx, "environment", req.Name, req.Namespace)
	defer span.End()
	start := time.Now()
	defer func() {
		if retErr != nil {
			reconcileloop.RecordError(span, retErr)
		} else {
			reconcileloop.SetSuccess(span)
		}
		reconcileloop.ReconcileOutcome("environment", start, retErr)
	}()

	logger := log.FromContext(ctx)

	var env loopzerov1.Environment
	if err := r.Get(ctx, req.NamespacedName, &env); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// T1.1 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap: propagate
	// correlation-id from CR annotation into reconcile context (no-op se ausente).
	ctx = correlation.WithID(ctx, env.GetAnnotations()["loopzero.io/correlation-id"])

	// kstatus universal per ADR-0025 D3 condition #1.
	// Per pkg-sota-faang-grade-plan v1.2 T2.2 closure: delegates to
	// pkg/conditions via adapter (same algorithm, shared core).
	defer reconcileloop.DeferReadinessSummary(ctx, r.Client, req.NamespacedName,
		func() *loopzerov1.Environment { return &loopzerov1.Environment{} },
		func(x *loopzerov1.Environment) *[]metav1.Condition { return &x.Status.Conditions },
		environmentDetailTypes)()

	// Handle deletion with verify-then-remove finalizer pattern
	if !env.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &env)
	}

	// Add finalizer if not present
	if err := reconcileloop.EnsureFinalizer(ctx, r.Client, &env, environmentFinalizer, req.NamespacedName); err != nil {
		return ctrl.Result{}, err
	}

	// MEET-2026-07-02-002 MA-1/MA-8: single-status-writer merge-patch base.
	// Environment `.status` has exactly ONE writer (this controller); the
	// Tenant controller writes only Environment spec/metadata. Capturing a
	// DeepCopy here — after EnsureFinalizer's metadata write, before any status
	// mutation — lets every status write below use
	// `r.Status().Patch(ctx, &env, client.MergeFrom(base))`. A merge patch
	// carries no resourceVersion (no optimistic-concurrency 409) and sends only
	// the changed status fields, so a concurrent spec/metadata write by the
	// Tenant controller can never clobber this status write. This replaces the
	// former refetch-then-restore `Status().Update` dance, which lost updates on
	// conflict and silently dropped the condition (the t31 flake). If a SECOND
	// status writer is ever introduced this invariant must be revisited — see
	// `.claude/rules/loopzero-design.md` § single-status-writer.
	base := env.DeepCopy()

	// Transition from empty to Pending via state machine (Rule 8).
	if env.Status.Phase == "" {
		env.Status.Phase = loopzerov1.EnvironmentPhase(
			environmentSM.Transition(statemachine.EPhEmpty, statemachine.EPEventCreate),
		)
	}

	tenantName := env.Spec.TenantRef.Name

	// T3.1 of plan 2026-06-09-m2-operator-canonical-fix v1.4: compute the
	// canonical M2 namespace from Spec.Project + Spec.Environment when
	// Project is populated (post-T3.1 Tenant CRs from TenantController set
	// it). Fall back to Spec.Namespace.Name for pre-T3.1 CRs whose
	// namespace was hand-written by the upstream renderer/operator chain.
	//
	// EC-3 absorption (R7): if naming.Namespace returns an error (typically
	// because the computed name exceeds the K8s 63-char limit per
	// `pkg/naming/naming.go § MaxKubernetesNameLength`), set Status
	// condition TypeEnvNamespaceReady=False Reason=NameTooLong with the
	// length in the message and return ctrl.Result{} (no requeue). User
	// sees an actionable error in the dashboard instead of an infinite
	// reconcile-loop with a cryptic error per `rules/loopzero-design.md` §
	// Error Philosophy.
	nsName := env.Spec.Namespace.Name
	if env.Spec.Project != "" {
		// T1.3.e of plan 2026-06-11-code-review-remediation v1.2 —
		// canonical EnvRef pattern per ADR D2. Construction-time
		// validation (NewEnvRef) catches empty-required + reserved
		// separator BEFORE the formula runs. The error chain stays
		// fail-loud so the NameTooLong condition surfaces with the
		// same actionable shape the dashboard already renders.
		ref, refErr := domain.NewEnvRef(tenantName, env.Spec.Project, env.Spec.Environment)
		if refErr != nil {
			msg := fmt.Sprintf("EnvRef (tenant=%q, project=%q, env=%q): %v",
				tenantName, env.Spec.Project, env.Spec.Environment, refErr)
			conditions.SetCondition(&env.Status.Conditions, conditions.TypeEnvNamespaceReady,
				metav1.ConditionFalse, "InvalidEnvRef", msg, env.Generation)
			// MA-4: report the observed generation even on the error branch.
			env.Status.ObservedGeneration = env.Generation
			// MA-1/MA-2: merge-patch (no 409); requeue on write failure.
			if statusErr := r.Status().Patch(ctx, &env, client.MergeFrom(base)); statusErr != nil {
				logger.Error(statusErr, "failed to persist InvalidEnvRef condition")
				return ctrl.Result{}, statusErr
			}
			// Structurally invalid until the user fixes the EnvRef; re-reconcile
			// only on CR update (watches handle that automatically) — no requeue.
			return ctrl.Result{}, nil
		}
		canonical, nsErr := ref.Namespace()
		if nsErr != nil {
			msg := fmt.Sprintf("namespace name (tenant=%q, project=%q, env=%q): %v",
				tenantName, env.Spec.Project, env.Spec.Environment, nsErr)
			conditions.SetCondition(&env.Status.Conditions, conditions.TypeEnvNamespaceReady,
				metav1.ConditionFalse, "NameTooLong", msg, env.Generation)
			// MA-4: report the observed generation even on the error branch.
			env.Status.ObservedGeneration = env.Generation
			// MA-1/MA-2: merge-patch (no 409); requeue on write failure so the
			// condition converges instead of being silently dropped (t31 flake).
			if statusErr := r.Status().Patch(ctx, &env, client.MergeFrom(base)); statusErr != nil {
				logger.Error(statusErr, "failed to update NameTooLong status")
				return ctrl.Result{}, statusErr
			}
			// EC-3: do not requeue on success — name is structurally invalid
			// until the user shortens Project or Environment. Re-reconcile only
			// on CR update (controller-runtime watches handle that automatically).
			return ctrl.Result{}, nil
		}
		nsName = canonical
	}

	// envStep pairs a condition type with its reconcile function and success message.
	type envStep struct {
		condType       string
		successReason  string
		successMessage string
		fn             func() error
	}

	steps := []envStep{
		{conditions.TypeEnvNamespaceReady, "NamespaceReady", "namespace provisioned",
			func() error { return r.reconcileNamespace(ctx, &env, nsName, tenantName) }},
		{conditions.TypeEnvRBACReady, "RBACConfigured", "RBAC resources provisioned",
			func() error { return r.reconcileRBAC(ctx, &env, nsName) }},
		{conditions.TypeEnvNetworkPolicyReady, "NetworkPoliciesConfigured", "network policies provisioned",
			func() error {
				return r.reconcileNetworkPolicies(ctx, &env, nsName)
			}},
		{conditions.TypeEnvQuotaReady, "QuotaReady", "resource quota applied",
			func() error { return r.reconcileQuota(ctx, &env, nsName) }},
	}

	for _, step := range steps {
		if err := step.fn(); err != nil {
			conditions.SetCondition(&env.Status.Conditions, step.condType,
				metav1.ConditionFalse, "ReconcileFailed", err.Error(), env.Generation)
			// MA-1: merge-patch (no 409). The step error already drives the
			// requeue below; a status-write failure is logged, not swallowed.
			if statusErr := r.Status().Patch(ctx, &env, client.MergeFrom(base)); statusErr != nil {
				logger.Error(statusErr, "failed to update environment status")
			}
			return ctrl.Result{}, err
		}
		conditions.SetCondition(&env.Status.Conditions, step.condType,
			metav1.ConditionTrue, step.successReason, step.successMessage, env.Generation)
	}

	// All conditions True -> transition to Active via state machine (Rule 8).
	if env.Status.Phase != loopzerov1.EnvironmentPhaseActive {
		next, err := environmentSM.TryTransition(statemachine.EnvironmentPhase(env.Status.Phase), statemachine.EPEventProvisioned)
		if err != nil {
			logger.Error(err, "invalid state transition", "phase", env.Status.Phase)
			return ctrl.Result{}, err
		}
		env.Status.Phase = loopzerov1.EnvironmentPhase(next)
	}
	env.Status.Namespace = nsName
	env.Status.ObservedGeneration = env.Generation

	// MA-1: merge-patch the accumulated status (phase + conditions + namespace +
	// observedGeneration) off the base snapshot. No refetch, no resourceVersion,
	// no 409 against a concurrent Tenant-controller spec/metadata write.
	if err := r.Status().Patch(ctx, &env, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("reconciled environment", "environment", env.Name, "phase", env.Status.Phase, "namespace", nsName)
	// Requeue periodically to detect and correct drift on cross-cluster resources.
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// reconcileDelete handles the verify-then-remove finalizer pattern for Environment deletion.
func (r *EnvironmentReconciler) reconcileDelete(ctx context.Context, env *loopzerov1.Environment) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(env, environmentFinalizer) {
		return ctrl.Result{}, nil
	}

	logger.Info("environment is being deleted", "environment", env.Name)

	// This path's own base snapshot.
	//
	// Reconcile branches here BEFORE the finalizer block, so the base the main
	// path takes does not exist yet — and one taken at the top of Reconcile to
	// serve both would be stale for the main path. Two snapshots in disjoint
	// scopes make picking the wrong one impossible; the shape validated in
	// slice-21 (B-085).
	base := env.DeepCopy()

	// Transition to Deleting via state machine (Rule 8).
	if env.Status.Phase != loopzerov1.EnvironmentPhaseDeleting {
		next, err := environmentSM.TryTransition(statemachine.EnvironmentPhase(env.Status.Phase), statemachine.EPEventDeleteRequested)
		if err != nil {
			logger.Error(err, "invalid state transition for delete", "phase", env.Status.Phase)
			return ctrl.Result{}, err
		}
		env.Status.Phase = loopzerov1.EnvironmentPhase(next)
		if err := r.Status().Patch(ctx, env, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Delete namespace on cell-aware runtime cluster (T1.1 BUG-FIX).
	nsName := env.Spec.Namespace.Name
	runtimeClient, rcErr := r.runtimeClientForEnv(ctx, env)
	if rcErr != nil {
		return ctrl.Result{}, rcErr
	}
	ns := &corev1.Namespace{}
	if err := runtimeClient.Get(ctx, client.ObjectKey{Name: nsName}, ns); err == nil {
		if ns.Labels != nil && ns.Labels[constants.LabelTenant] == env.Spec.TenantRef.Name {
			if ns.DeletionTimestamp.IsZero() {
				if err := runtimeClient.Delete(ctx, ns); err != nil && !errors.IsNotFound(err) {
					return ctrl.Result{}, err
				}
			}
			logger.Info("namespace still exists, requeuing", "namespace", nsName)
			return ctrl.Result{RequeueAfter: constants.RequeueNamespaceDeletionWait}, nil
		}
	}

	// Namespace gone — remove finalizer
	controllerutil.RemoveFinalizer(env, environmentFinalizer)
	if err := r.Update(ctx, env); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("environment deletion complete", "environment", env.Name)
	return ctrl.Result{}, nil
}

// SetupEnvironmentController registers the EnvironmentController with the manager.
//
// Single-cell mode: pass nil as factory; legacy r.RuntimeClient is used for
// every environment regardless of cellID.
//
// Multi-cell mode: pass a CellAwareRuntimeClientFactory built from
// mgr.GetClient() (mgmt) + runtimeClient (legacy fallback for cell-0).
//
// fix-all-platform-issues-plan T1.1 — split-binary parity with the
// monolithic controller from cell-1-activation-plan iter 4.
//
// Quality-Gate Phase 3: SetupEnvironmentController legacy wrapper removed
// — all callers (cmd/* binaries + operatortest envtest_suite) now invoke
// SetupEnvironmentControllerWithFactory directly with factory=nil for the
// legacy single-cell path.

// SetupEnvironmentControllerWithFactory is the cell-aware entrypoint.
func SetupEnvironmentControllerWithFactory(mgr ctrl.Manager, runtimeClient client.Client, factory cellrouting.RuntimeClientFactory) error {
	ingressNS := os.Getenv("INGRESS_NAMESPACE")
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopzerov1.Environment{}, reconcileloop.ForOption()).
		Named("environment").
		Complete(&EnvironmentReconciler{
			Client:           mgr.GetClient(),
			RuntimeClient:    runtimeClient,
			RuntimeFactory:   factory,
			Scheme:           mgr.GetScheme(),
			IngressNamespace: ingressNS,
		})
}
