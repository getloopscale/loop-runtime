// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

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

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/cellrouting"
	"github.com/getloopscale/loop-runtime/operators/internal/conditions"
	"github.com/getloopscale/loop-runtime/operators/internal/constants"
	"github.com/getloopscale/loop-runtime/operators/internal/reconcileloop"
	"github.com/getloopscale/loop-runtime/pkg/correlation"
	"github.com/getloopscale/loop-runtime/pkg/domain"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

const environmentFinalizer = constants.FinalizerEnvironment

// environmentSM is the Environment lifecycle state machine (spec §5).
// Phase transitions go through this machine — direct assignment is forbidden (Rule 8).
var environmentSM = statemachine.NewEnvironmentTypedMachine()

// EnvironmentReconciler reconciles an Environment object.
//
// Multi-cluster design: the Environment CR lives in the mgmt cluster, while the
// namespace, RBAC, NetworkPolicy and quota live on the runtime cluster of the
// tenant's cell.
//   - r.Client         -> mgmt cluster (Environment CR + Tenant CR reads)
//   - r.RuntimeFactory -> the runtime cluster of a cell, by cellID. Required.
//
// There is no fallback cluster. A cell that cannot be resolved fails the
// reconcile and requeues with backoff: writing a tenant namespace to whichever
// cluster happened to answer is the silent misplacement this design exists to
// prevent. The only cell with a default client is cellID "" (a tenant that was
// never placed), and that mapping belongs to the factory, not to this type.
//
// ADR-0026 D4 / plan T4.1: kubebuilder:rbac markers for the LOCAL
// (mgmt cluster) RBAC. Cross-cluster RBAC for runtime cluster
// (namespaces/RBAC/NetworkPolicies in tenant envs) is hand-maintained.
//
// +kubebuilder:rbac:groups=runtime.getloopscale.com,resources=environments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=runtime.getloopscale.com,resources=environments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=runtime.getloopscale.com,resources=environments/finalizers,verbs=update
// +kubebuilder:rbac:groups=runtime.getloopscale.com,resources=tenants,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups="",namespace=loopruntime-system,resources=secrets,verbs=get;list;watch
type EnvironmentReconciler struct {
	client.Client
	RuntimeFactory   cellrouting.RuntimeClientFactory
	Scheme           *runtime.Scheme
	IngressNamespace string // Namespace where ingress controller (Traefik) runs. Default: traefik.
}

// tenantCell returns the cell the parent Tenant is placed in. The Tenant is the
// canonical record of placement (tenants.cell_id mirrors into Spec.CellID), so
// an unreadable Tenant is an unknown cell, never a default one.
func (r *EnvironmentReconciler) tenantCell(ctx context.Context, env *loopruntimev1.Environment) (string, error) {
	var tenant loopruntimev1.Tenant
	key := client.ObjectKey{Name: env.Spec.TenantRef.Name, Namespace: env.Namespace}
	if err := r.Get(ctx, key, &tenant); err != nil {
		return "", fmt.Errorf("environment %s: read tenant %s to resolve its cell: %w",
			env.Name, env.Spec.TenantRef.Name, err)
	}
	return tenant.Spec.CellID, nil
}

// runtimeClientForCell resolves the runtime cluster client of a cell.
func (r *EnvironmentReconciler) runtimeClientForCell(ctx context.Context, env *loopruntimev1.Environment, cellID string) (client.Client, error) {
	c, err := r.RuntimeFactory.For(ctx, cellID)
	if err != nil {
		return nil, fmt.Errorf("environment %s (cellID=%q): runtime cluster client: %w", env.Name, cellID, err)
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

	var env loopruntimev1.Environment
	if err := r.Get(ctx, req.NamespacedName, &env); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// T1.1 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap: propagate
	// correlation-id from CR annotation into reconcile context (no-op when absent).
	ctx = correlation.WithID(ctx, env.GetAnnotations()["runtime.getloopscale.com/correlation-id"])

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
	// `r.patchStatus(ctx, &env, base)`. A merge patch
	// carries no resourceVersion (no optimistic-concurrency 409) and sends only
	// the changed status fields, so a concurrent spec/metadata write by the
	// Tenant controller can never clobber this status write. This replaces the
	// former refetch-then-restore `Status().Update` dance, which lost updates on
	// conflict and silently dropped the condition (the t31 flake). If a SECOND
	// status writer is ever introduced this invariant must be revisited — see
	// `CLAUDE.md` § Single-status-writer invariant, which is the contract itself and not a
	// summary of one. The design rule this used to cite was deleted in `852ade70f`; its
	// name is left out deliberately, because writing a dead path into the note explaining
	// that the path is dead is how the citation survives its own correction.
	base := env.DeepCopy()

	// Transition from empty to Pending via state machine (Rule 8).
	if env.Status.Phase == "" {
		env.Status.Phase = loopruntimev1.EnvironmentPhase(
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
	// reconcile-loop with a cryptic error. The rule is one sentence, so it is stated
	// rather than cited: fail loudly with an actionable message, never drop the error.
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
			if statusErr := r.patchStatus(ctx, &env, base); statusErr != nil {
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
			if statusErr := r.patchStatus(ctx, &env, base); statusErr != nil {
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

	// Resolve the cell once for every step below. A failure here is surfaced on
	// the namespace condition and requeued; nothing is written anywhere until
	// the tenant's cell answers.
	cellID, err := r.tenantCell(ctx, &env)
	var runtimeClient client.Client
	if err == nil {
		runtimeClient, err = r.runtimeClientForCell(ctx, &env, cellID)
	}
	if err != nil {
		conditions.SetCondition(&env.Status.Conditions, conditions.TypeEnvNamespaceReady,
			metav1.ConditionFalse, "RuntimeClusterUnresolved", err.Error(), env.Generation)
		if statusErr := r.patchStatus(ctx, &env, base); statusErr != nil {
			logger.Error(statusErr, "failed to persist RuntimeClusterUnresolved condition")
		}
		return ctrl.Result{}, err
	}

	steps := []envStep{
		{conditions.TypeEnvNamespaceReady, "NamespaceReady", "namespace provisioned",
			func() error { return r.reconcileNamespace(ctx, runtimeClient, &env, nsName, tenantName) }},
		{conditions.TypeEnvRBACReady, "RBACConfigured", "RBAC resources provisioned",
			func() error { return r.reconcileRBAC(ctx, runtimeClient, &env, nsName) }},
		{conditions.TypeEnvNetworkPolicyReady, "NetworkPoliciesConfigured", "network policies provisioned",
			func() error { return r.reconcileNetworkPolicies(ctx, runtimeClient, nsName) }},
		{conditions.TypeEnvQuotaReady, "QuotaReady", "resource quota applied",
			func() error { return r.reconcileQuota(ctx, runtimeClient, &env, nsName) }},
	}

	for _, step := range steps {
		if err := step.fn(); err != nil {
			conditions.SetCondition(&env.Status.Conditions, step.condType,
				metav1.ConditionFalse, "ReconcileFailed", err.Error(), env.Generation)
			// MA-1: merge-patch (no 409). The step error already drives the
			// requeue below; a status-write failure is logged, not swallowed.
			if statusErr := r.patchStatus(ctx, &env, base); statusErr != nil {
				logger.Error(statusErr, "failed to update environment status")
			}
			return ctrl.Result{}, err
		}
		conditions.SetCondition(&env.Status.Conditions, step.condType,
			metav1.ConditionTrue, step.successReason, step.successMessage, env.Generation)
	}

	// All conditions True -> transition to Active via state machine (Rule 8).
	if env.Status.Phase != loopruntimev1.EnvironmentPhaseActive {
		next, err := environmentSM.TryTransition(statemachine.EnvironmentPhase(env.Status.Phase), statemachine.EPEventProvisioned)
		if err != nil {
			logger.Error(err, "invalid state transition", "phase", env.Status.Phase)
			return ctrl.Result{}, err
		}
		env.Status.Phase = loopruntimev1.EnvironmentPhase(next)
	}
	env.Status.Namespace = nsName
	env.Status.CellID = cellID
	env.Status.ObservedGeneration = env.Generation

	// MA-1: merge-patch the accumulated status (phase + conditions + namespace +
	// observedGeneration) off the base snapshot. No refetch, no resourceVersion,
	// no 409 against a concurrent Tenant-controller spec/metadata write.
	if err := r.patchStatus(ctx, &env, base); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("reconciled environment", "environment", env.Name, "phase", env.Status.Phase, "namespace", nsName)
	// Requeue periodically to detect and correct drift on cross-cluster resources.
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// reconcileDelete handles the verify-then-remove finalizer pattern for Environment deletion.
func (r *EnvironmentReconciler) reconcileDelete(ctx context.Context, env *loopruntimev1.Environment) (ctrl.Result, error) {
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
	if env.Status.Phase != loopruntimev1.EnvironmentPhaseDeleting {
		next, err := environmentSM.TryTransition(statemachine.EnvironmentPhase(env.Status.Phase), statemachine.EPEventDeleteRequested)
		if err != nil {
			logger.Error(err, "invalid state transition for delete", "phase", env.Status.Phase)
			return ctrl.Result{}, err
		}
		env.Status.Phase = loopruntimev1.EnvironmentPhase(next)
		if err := r.patchStatus(ctx, env, base); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Delete the namespace in the cell it was provisioned in.
	nsName := env.Spec.Namespace.Name
	gone, err := r.deleteNamespace(ctx, env, nsName)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !gone {
		logger.Info("namespace still exists, requeuing", "namespace", nsName)
		return ctrl.Result{RequeueAfter: constants.RequeueNamespaceDeletionWait}, nil
	}

	// Namespace gone — remove the tuple from the Tenant BEFORE releasing the finalizer,
	// or the Application is orphaned (B-074). After the finalizer there is no second
	// chance: the CR is gone.
	if err := r.pruneTenantEnvironmentEntry(ctx, env); err != nil {
		return ctrl.Result{}, err
	}

	// Namespace gone — remove finalizer
	controllerutil.RemoveFinalizer(env, environmentFinalizer)
	if err := r.Update(ctx, env); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("environment deletion complete", "environment", env.Name)
	return ctrl.Result{}, nil
}

// pruneTenantEnvironmentEntry removes this environment's (Project, Environment)
// tuple from `Tenant.Spec.Environments`.
//
// B-074: deleting the Environment CR used to delete the Namespace and release
// the finalizer, and nothing else. The ArgoCD Application stayed alive pointing
// at a Namespace that no longer existed, because the reaper that would reach
// it — `pruneOrphanPreviewApps` — only deletes Applications OUTSIDE
// `expectedApplicationNames`, a set derived from exactly this list by the
// (Project, Name) tuple. While the tuple stayed, the Application stayed
// EXPECTED.
//
// The tuple is removed instead of deleting the Application here: the owner of
// its `.spec` is the tenant operator (ADR
// 2026-08-12-argocd-application-ownership, B-050). Opening a second deletion
// path in a controller that is not the owner is "correct code in the wrong
// place". Reusing the existing reaper keeps one owner and one mechanism.
//
// T2 record:
// knowledge-base/decisions/MEET-ASYNC-AMENDMENT-2026-08-19-environment-delete-orphans-application.md
func (r *EnvironmentReconciler) pruneTenantEnvironmentEntry(ctx context.Context, env *loopruntimev1.Environment) error {
	logger := log.FromContext(ctx)

	var tenant loopruntimev1.Tenant
	key := client.ObjectKey{Name: env.Spec.TenantRef.Name, Namespace: env.Namespace}
	if err := r.Get(ctx, key, &tenant); err != nil {
		if errors.IsNotFound(err) {
			// The Tenant is already gone: there is no tuple to remove, and holding
			// the finalizer here would block the Environment's deletion forever.
			return nil
		}
		return err
	}

	base := tenant.DeepCopy()
	kept := make([]loopruntimev1.TenantEnvironmentEntry, 0, len(tenant.Spec.Environments))
	for _, e := range tenant.Spec.Environments {
		if e.Project == env.Spec.Project && e.Name == env.Spec.Environment {
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) == len(tenant.Spec.Environments) {
		return nil
	}
	tenant.Spec.Environments = kept

	// Merge patch off a base snapshot: it carries no resourceVersion, so it does not
	// 409 against a concurrent api write to another field of the same spec.
	if err := r.Patch(ctx, &tenant, client.MergeFrom(base)); err != nil {
		return err
	}
	logger.Info("removed environment tuple from tenant spec",
		"tenant", tenant.Name, "project", env.Spec.Project, "environment", env.Spec.Environment)
	return nil
}

// deletionCell returns the cell holding this environment's namespace. The cell
// recorded at provisioning wins: the Tenant may have been re-pinned since, and
// the namespace is where it was created, not where the tenant points now.
// Environments provisioned before the cell was recorded fall back to the
// Tenant's current cell.
//
// ok is false when nothing was ever provisioned (no recorded namespace) and the
// cell is unknowable — the only case where there is nothing to delete.
func (r *EnvironmentReconciler) deletionCell(ctx context.Context, env *loopruntimev1.Environment) (cellID string, ok bool, err error) {
	if env.Status.CellID != "" {
		return env.Status.CellID, true, nil
	}
	cellID, err = r.tenantCell(ctx, env)
	if err == nil {
		return cellID, true, nil
	}
	if errors.IsNotFound(err) && env.Status.Namespace == "" {
		return "", false, nil
	}
	return "", false, fmt.Errorf("environment %s: cannot locate the cell holding namespace %q: %w",
		env.Name, env.Spec.Namespace.Name, err)
}

// deleteNamespace deletes the tenant-owned namespace in the environment's cell
// and reports whether it is gone. Any lookup failure other than NotFound is
// returned: "could not see it" is not "it is gone", and releasing the finalizer
// on that confusion leaks the namespace and its workloads.
func (r *EnvironmentReconciler) deleteNamespace(ctx context.Context, env *loopruntimev1.Environment, nsName string) (bool, error) {
	cellID, ok, err := r.deletionCell(ctx, env)
	if err != nil {
		return false, err
	}
	if !ok {
		return true, nil
	}
	runtimeClient, err := r.runtimeClientForCell(ctx, env, cellID)
	if err != nil {
		return false, err
	}

	ns := &corev1.Namespace{}
	if err := runtimeClient.Get(ctx, client.ObjectKey{Name: nsName}, ns); err != nil {
		if errors.IsNotFound(err) {
			return true, nil
		}
		return false, fmt.Errorf("environment %s: get namespace %s in cell %q: %w", env.Name, nsName, cellID, err)
	}
	if ns.Labels[constants.LabelTenant] != env.Spec.TenantRef.Name {
		// Not ours: never delete a namespace another tenant (or nobody) owns.
		return true, nil
	}
	if ns.DeletionTimestamp.IsZero() {
		if err := runtimeClient.Delete(ctx, ns); err != nil && !errors.IsNotFound(err) {
			return false, err
		}
	}
	return false, nil
}

// SetupEnvironmentControllerWithFactory registers the EnvironmentController.
// factory is required: it is the only way this controller reaches a runtime
// cluster. A single-cell deployment passes a factory whose default client is
// that cluster.
func SetupEnvironmentControllerWithFactory(mgr ctrl.Manager, factory cellrouting.RuntimeClientFactory) error {
	if factory == nil {
		return fmt.Errorf("environment controller: a runtime client factory is required")
	}
	ingressNS := os.Getenv("INGRESS_NAMESPACE")
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopruntimev1.Environment{}, reconcileloop.ForOption()).
		Named("environment").
		Complete(&EnvironmentReconciler{
			Client:           mgr.GetClient(),
			RuntimeFactory:   factory,
			Scheme:           mgr.GetScheme(),
			IngressNamespace: ingressNS,
		})
}

// patchStatus is the single status writer of this controller. It folds the
// kstatus readiness summary in as the last mutation (ADR-0025 D3) and persists
// with a merge patch off base, so every write carries a Ready condition that
// agrees with the conditions written beside it.
func (r *EnvironmentReconciler) patchStatus(ctx context.Context, env, base *loopruntimev1.Environment) error {
	conditions.SetReadinessSummaryViaPkg(&env.Status.Conditions, env.Generation, environmentDetailTypes)
	return r.Status().Patch(ctx, env, client.MergeFrom(base))
}
