// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller

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

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/conditions"
	"github.com/loopzero/loopzero/operators/internal/constants"
	"github.com/loopzero/loopzero/operators/internal/shared"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

const environmentFinalizer = constants.FinalizerEnvironment

// environmentSM is the Environment lifecycle state machine (spec §5).
// Phase transitions go through this machine — direct assignment is forbidden (Rule 8).
var environmentSM = statemachine.NewEnvironmentMachine()

// EnvironmentReconciler reconciles an Environment object.
//
// Multi-cluster design: the Environment CR lives in the mgmt cluster,
// but namespace/RBAC/NetworkPolicy resources are created on the runtime cluster.
//   - r.Client          -> mgmt cluster (for reading/updating Environment CR + Tenant CRs).
//   - r.RuntimeClient   -> default runtime cluster (legacy single-cell fallback).
//   - r.RuntimeFactory  -> cell-aware factory: returns the right runtime client
//     based on tenant.spec.cellID (cell-2-activation-plan iter 4 BUG-FIX).
//
// All namespace/RBAC/NetworkPolicy/Quota writes MUST go through
// runtimeClientForEnv() — direct r.RuntimeClient calls bypass cell awareness
// and create resources on the wrong cluster (the bug this factory fixes).
type EnvironmentReconciler struct {
	client.Client
	// RuntimeClient is the legacy single-cluster client. Kept for backward
	// compat: when RuntimeFactory is nil, all reconciles use this client.
	// When RuntimeFactory IS set, RuntimeClient becomes the cell-default
	// fallback (returned by RuntimeFactory.For for empty/cell-default IDs).
	RuntimeClient    client.Client
	RuntimeFactory   shared.RuntimeClientFactory // nil = single-cell mode (legacy)
	Scheme           *runtime.Scheme
	IngressNamespace string // Namespace where ingress controller (Traefik) runs. Default: loopzero-system.
}

// runtimeClientForEnv resolves the right runtime client for the given
// Environment by looking up the parent Tenant's spec.cellID. Falls back
// to the legacy RuntimeClient when:
//   - tenant CR cannot be fetched (transient drift; preserves liveness)
//   - tenant.spec.cellID is empty or "cell-default" (single-cell deployments)
//   - RuntimeFactory is nil (operators not yet migrated to multi-cell)
//
// Errors propagate only when the factory returns an error AND no fallback
// exists — every other path returns a usable client.
func (r *EnvironmentReconciler) runtimeClientForEnv(ctx context.Context, env *loopzerov1.Environment) (client.Client, error) {
	// No factory configured → legacy single-cell behavior.
	if r.RuntimeFactory == nil {
		if r.RuntimeClient == nil {
			return nil, fmt.Errorf("environment %s: no RuntimeClient and no RuntimeFactory configured", env.Name)
		}
		return r.RuntimeClient, nil
	}

	var tenant loopzerov1.Tenant
	tenantNS := env.Namespace
	if err := r.Get(ctx, client.ObjectKey{Name: env.Spec.TenantRef.Name, Namespace: tenantNS}, &tenant); err != nil {
		// Tenant CR not in env namespace; try cluster-scoped lookup.
		if err2 := r.Get(ctx, client.ObjectKey{Name: env.Spec.TenantRef.Name}, &tenant); err2 != nil {
			// Tenant not found — fall back to default client to avoid
			// blocking Environment reconciliation during transient drift.
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
		// Factory failed → fall back to legacy client (preserves liveness).
		if r.RuntimeClient != nil {
			return r.RuntimeClient, nil
		}
		return nil, fmt.Errorf("environment %s (cellID=%q): factory error and no fallback: %w",
			env.Name, cellID, err)
	}
	return c, nil
}

// Reconcile handles the reconciliation loop for Environment resources.
func (r *EnvironmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
	ctx, span := startReconcileSpan(ctx, "environment", req.Name, req.Namespace)
	defer span.End()
	start := time.Now()
	defer func() {
		if retErr != nil {
			recordError(span, retErr)
		} else {
			setSuccess(span)
		}
		reconcileOutcome("environment", start, retErr)
	}()

	logger := log.FromContext(ctx)

	var env loopzerov1.Environment
	if err := r.Get(ctx, req.NamespacedName, &env); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion with verify-then-remove finalizer pattern
	if !env.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &env)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(&env, environmentFinalizer) {
		controllerutil.AddFinalizer(&env, environmentFinalizer)
		if err := r.Update(ctx, &env); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Get(ctx, req.NamespacedName, &env); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Transition from empty to Pending via state machine (Rule 8).
	if env.Status.Phase == "" {
		env.Status.Phase = loopzerov1.EnvironmentPhase(
			environmentSM.Transition(statemachine.EnvironmentEmpty, statemachine.EnvironmentCreate),
		)
	}

	nsName := env.Spec.Namespace.Name
	tenantName := env.Spec.TenantRef.Name

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
			savedStatus := env.Status
			if refetchErr := r.Get(ctx, req.NamespacedName, &env); refetchErr != nil {
				logger.Error(refetchErr, "failed to re-fetch environment for status update")
				return ctrl.Result{}, err
			}
			env.Status = savedStatus
			if statusErr := r.Status().Update(ctx, &env); statusErr != nil {
				logger.Error(statusErr, "failed to update environment status")
			}
			return ctrl.Result{}, err
		}
		conditions.SetCondition(&env.Status.Conditions, step.condType,
			metav1.ConditionTrue, step.successReason, step.successMessage, env.Generation)
	}

	// All conditions True -> transition to Active via state machine (Rule 8).
	if env.Status.Phase != loopzerov1.EnvironmentPhaseActive {
		next, err := environmentSM.TryTransition(statemachine.State(env.Status.Phase), statemachine.EnvironmentProvisioned)
		if err != nil {
			logger.Error(err, "invalid state transition", "phase", env.Status.Phase)
			return ctrl.Result{}, err
		}
		env.Status.Phase = loopzerov1.EnvironmentPhase(next)
	}
	env.Status.Namespace = nsName
	env.Status.ObservedGeneration = env.Generation

	// Re-fetch to get latest resourceVersion before final status update.
	savedStatus := env.Status
	if err := r.Get(ctx, req.NamespacedName, &env); err != nil {
		return ctrl.Result{}, err
	}
	env.Status = savedStatus

	if err := r.Status().Update(ctx, &env); err != nil {
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

	// Transition to Deleting via state machine (Rule 8).
	if env.Status.Phase != loopzerov1.EnvironmentPhaseDeleting {
		next, err := environmentSM.TryTransition(statemachine.State(env.Status.Phase), statemachine.EnvironmentDeleteRequested)
		if err != nil {
			logger.Error(err, "invalid state transition for delete", "phase", env.Status.Phase)
			return ctrl.Result{}, err
		}
		env.Status.Phase = loopzerov1.EnvironmentPhase(next)
		if err := r.Status().Update(ctx, env); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Delete namespace on the cell-aware runtime cluster (BUG-FIX iter 4).
	nsName := env.Spec.Namespace.Name
	runtimeClient, err := r.runtimeClientForEnv(ctx, env)
	if err != nil {
		return ctrl.Result{}, err
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
// mgr.GetClient() (mgmt) + runtimeClient (legacy fallback for cell-default).
// The factory looks up tenant.spec.cellID and resolves the matching
// ClusterCredential to build a runtime client for the right cell.
//
// SetupEnvironmentControllerWithFactory is the cell-aware entrypoint.
// Pass factory=nil for legacy single-cell behavior.
func SetupEnvironmentControllerWithFactory(mgr ctrl.Manager, runtimeClient client.Client, factory shared.RuntimeClientFactory) error {
	ingressNS := os.Getenv("INGRESS_NAMESPACE")
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopzerov1.Environment{}).
		Named("environment").
		Complete(&EnvironmentReconciler{
			Client:           mgr.GetClient(),
			RuntimeClient:    runtimeClient,
			RuntimeFactory:   factory,
			Scheme:           mgr.GetScheme(),
			IngressNamespace: ingressNS,
		})
}
