// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package preview implements the PreviewEnvironment controller
// (operator binary `loopzero-preview-operator` per ADR-0023). Owns
// the preview-environment lifecycle: namespace creation +
// NetworkPolicy + ResourceQuota + LimitRange + TTL expiry +
// condition-wait + cleanup retry. State machine per CLAUDE.md §
// State Machines.
package preview

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
	"github.com/getloopscale/loopzero/pkg/naming"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

const previewFinalizer = "loopzero.io/preview-cleanup"

// previewSM is the PreviewEnvironment lifecycle state machine (spec §5.3).
// T2.1 / COMP-002: phase transitions in this controller are now routed
// through the state machine instead of direct Status.Phase assignments.
var previewSM = statemachine.NewPreviewTypedMachine()

// PreviewReconciler reconciles PreviewEnvironment CRs.
// Creates temporary namespaces on the runtime cluster for PR previews.
//
// ADR-0026 D4 / plan T4.1: kubebuilder:rbac markers for the LOCAL
// (mgmt cluster) RBAC. Cross-cluster RBAC for runtime cluster
// (preview namespaces, NetworkPolicies, ResourceQuota, LimitRange) is
// hand-maintained.
//
// +kubebuilder:rbac:groups=loopzero.io,resources=previewenvironments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=loopzero.io,resources=previewenvironments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=loopzero.io,resources=previewenvironments/finalizers,verbs=update
// +kubebuilder:rbac:groups=loopzero.io,resources=tenants,verbs=get;list;watch
// +kubebuilder:rbac:groups=loopzero.io,resources=clustercredentials,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
type PreviewReconciler struct {
	client.Client
	// RuntimeFactory resolves the runtime cluster of a preview's cell. It is
	// the only way this controller reaches a runtime cluster, and there is no
	// fallback cluster: every runtime write (namespace, NetworkPolicies,
	// ResourceQuota, LimitRange, ServiceAccount) goes to the cell the preview's
	// ArgoCD Application targets, or the reconcile fails and requeues.
	//
	// CellID source priority:
	//   1. preview.Spec.CellID (set by the API at PreviewEnvironment creation)
	//   2. tenant.Spec.CellID (CRs created before Spec.CellID existed)
	RuntimeFactory cellrouting.RuntimeClientFactory
	Scheme         *runtime.Scheme
	// IngressNamespace is the namespace where Traefik runs.
	// Same semantics as EnvironmentReconciler.IngressNamespace —
	// empty string falls back to constants.NamespaceIngress (`traefik`).
	// drift-baseline.md port #3.
	IngressNamespace string
}

// previewCell returns the cell a preview's runtime objects live in: its own
// Spec.CellID, or its Tenant's for CRs created before that field existed. An
// unreadable Tenant is an unknown cell, never a default one (EC-7).
func (r *PreviewReconciler) previewCell(ctx context.Context, preview *loopzerov1.PreviewEnvironment) (string, error) {
	if preview.Spec.CellID != "" {
		return preview.Spec.CellID, nil
	}
	var tenant loopzerov1.Tenant
	key := client.ObjectKey{Name: preview.Spec.TenantID, Namespace: preview.Namespace}
	if err := r.Get(ctx, key, &tenant); err != nil {
		return "", fmt.Errorf("preview %s: read tenant %s to resolve its cell: %w", preview.Name, preview.Spec.TenantID, err)
	}
	return tenant.Spec.CellID, nil
}

// runtimeClientForPreview resolves the runtime cluster client of the
// preview's cell.
func (r *PreviewReconciler) runtimeClientForPreview(ctx context.Context, preview *loopzerov1.PreviewEnvironment) (client.Client, error) {
	cellID, err := r.previewCell(ctx, preview)
	if err != nil {
		return nil, err
	}
	c, err := r.RuntimeFactory.For(ctx, cellID)
	if err != nil {
		return nil, fmt.Errorf("preview %s (cellID=%q): runtime cluster client: %w", preview.Name, cellID, err)
	}
	return c, nil
}

// ingressNS returns the namespace where the ingress controller runs.
// Mirrors EnvironmentReconciler.ingressNS() to keep allow-ingress-traefik
// NetworkPolicy semantics consistent between regular and preview envs.
// drift-baseline.md port #3.
func (r *PreviewReconciler) ingressNS() string {
	if r.IngressNamespace != "" {
		return r.IngressNamespace
	}
	return constants.NamespaceIngress
}

// previewDetailTypes per ADR-0025 D3. ApplicationDeleted/NamespaceDeleted
// are deletion-path conditions and excluded from the steady-state summary.
var previewDetailTypes = []string{
	conditions.TypePreviewNamespaceReady,
	conditions.TypePreviewNetworkPolicyReady,
	conditions.TypePreviewResourceQuotaReady,
	conditions.TypePreviewLimitRangeReady,
	conditions.TypePreviewBuildSubmitted,
	conditions.TypePreviewBuildCompleted,
	conditions.TypePreviewWorkloadHealthy,
}

// Reconcile handles the reconciliation loop for PreviewEnvironment resources.
func (r *PreviewReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
	ctx, span := reconcileloop.StartReconcileSpan(ctx, "preview", req.Name, req.Namespace)
	defer span.End()
	start := time.Now()
	defer func() {
		if retErr != nil {
			reconcileloop.RecordError(span, retErr)
		} else {
			reconcileloop.SetSuccess(span)
		}
		reconcileloop.ReconcileOutcome("preview", start, retErr)
	}()

	logger := log.FromContext(ctx)

	var preview loopzerov1.PreviewEnvironment
	if err := r.Get(ctx, req.NamespacedName, &preview); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// T1.1 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap: propagate
	// correlation-id from CR annotation into reconcile context (no-op se ausente).
	ctx = correlation.WithID(ctx, preview.GetAnnotations()["loopzero.io/correlation-id"])

	// Handle deletion
	if !preview.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &preview)
	}

	// Add finalizer
	if !controllerutil.ContainsFinalizer(&preview, previewFinalizer) {
		controllerutil.AddFinalizer(&preview, previewFinalizer)
		if err := r.Update(ctx, &preview); err != nil {
			return ctrl.Result{}, err
		}
	}

	// The base snapshot for every status write on this path.
	//
	// Placement is the whole trick, and slice-18 paid for getting it wrong on the
	// Tenant controller: it goes AFTER the finalizer Update above, because that
	// Update bumps resourceVersion, and a merge patch derived from a snapshot
	// taken before it carries the stale version — turning the patch into an
	// optimistic lock frozen at snapshot time, which is a WIDER conflict window
	// than the refetch this contract removes.
	//
	// And BEFORE the phase initialisation below, because that mutates .Status: a
	// snapshot taken after it would diff the phase against itself and the patch
	// would silently omit the very transition it exists to persist.
	//
	// The deletion path does not pass through here — it returns above, before the
	// finalizer block — so it takes its own snapshot (see reconcileDelete).
	base := preview.DeepCopy()

	// A new CR enters the lifecycle through the state machine (T2.1 / COMP-002).
	if preview.Status.Phase == "" {
		preview.Status.Phase = loopzerov1.PreviewPhase(
			previewSM.Transition(statemachine.PPhEmpty, statemachine.PPEventCreate),
		)
		now := metav1.Now()
		preview.Status.CreatedAt = &now
	}

	// Phase dispatch. Reuse `start` as the canonical "now" for any
	// time-dependent logic inside this Reconcile pass (audit gap M03 #16
	// — preview TTL with multiple time.Now() calls within a single
	// reconcile was a latent flake source under mock clocks). One reading
	// per reconcile keeps tests deterministic and aligns with the pattern
	// already established in evaluateWorkloadGate.
	switch preview.Status.Phase {
	case loopzerov1.PreviewPhaseCreating:
		return r.reconcileCreating(ctx, &preview, base)
	case loopzerov1.PreviewPhaseActive:
		return r.reconcileActive(ctx, &preview, base, start)
	case loopzerov1.PreviewPhaseExpiring:
		return r.reconcileExpiring(ctx, &preview)
	default:
		logger.Info("preview in terminal phase, no action", "phase", preview.Status.Phase)
		return ctrl.Result{}, nil
	}
}

// previewNamespaceName returns the namespace name for the preview.
//
// A preview is not special at the naming layer: it is the (tenant, project, env) triple
// like any other environment, with env = "preview-pr-{N}". Returns an error if any input
// is invalid (T0.2 / D6).
func (r *PreviewReconciler) previewNamespaceName(preview *loopzerov1.PreviewEnvironment) (string, error) {
	if preview.Spec.PRNumber <= 0 {
		return "", fmt.Errorf("PR number must be positive, got %d", preview.Spec.PRNumber)
	}
	return naming.Namespace(
		preview.Spec.TenantID,
		preview.Spec.ProjectID,
		fmt.Sprintf("preview-pr-%d", preview.Spec.PRNumber),
	)
}

// failPreviewCondition is the canonical "set status condition False +
// patch + log status-write failure" pattern used throughout
// reconcileCreating. Phase 3 helper: shared by 5 call sites, collapsing what
// were 4 inline 7-line copies
// across NamespaceReady/NetworkPolicyReady/ResourceQuotaReady error paths.
// Returns the err untouched so the caller decides the controller-runtime
// result envelope (some paths want Requeue, others want no-requeue).
func (r *PreviewReconciler) failPreviewCondition(ctx context.Context, preview, base *loopzerov1.PreviewEnvironment, condType, reason, msg, statusLogContext string) {
	logger := log.FromContext(ctx)
	conditions.SetCondition(&preview.Status.Conditions, condType,
		metav1.ConditionFalse, reason, msg, preview.Generation)
	if statusErr := r.patchStatus(ctx, preview, base); statusErr != nil {
		if statusLogContext == "" {
			logger.Error(statusErr, "failed to update preview status")
		} else {
			logger.Error(statusErr, "failed to update preview status ("+statusLogContext+")")
		}
	}
}

// reconcileCreating provisions the preview namespace and route.
func (r *PreviewReconciler) reconcileCreating(ctx context.Context, preview, base *loopzerov1.PreviewEnvironment) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	nsName, err := r.previewNamespaceName(preview)
	if err != nil {
		r.failPreviewCondition(ctx, preview, base, conditions.TypePreviewNamespaceReady, "InvalidNaming", err.Error(), "")
		// Naming is input validation — requeue is pointless without a Spec
		// change, but we surface the condition and let the user fix the CR.
		return ctrl.Result{}, nil
	}

	// Create namespace on runtime cluster
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
			Labels: map[string]string{
				constants.LabelTenant:    preview.Spec.TenantID,
				constants.LabelManagedBy: "preview-controller",
				"loopzero.io/preview":        fmt.Sprintf("pr%d", preview.Spec.PRNumber),
			},
		},
	}
	rtClient, err := r.runtimeClientForPreview(ctx, preview)
	if err != nil {
		r.failPreviewCondition(ctx, preview, base, conditions.TypePreviewNamespaceReady, "RuntimeClusterUnresolved", err.Error(), "")
		return ctrl.Result{}, err
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, rtClient, ns, func() error {
		ns.Labels["pod-security.kubernetes.io/enforce"] = "restricted"
		ns.Labels["pod-security.kubernetes.io/warn"] = "restricted"
		return nil
	}); err != nil {
		r.failPreviewCondition(ctx, preview, base, conditions.TypePreviewNamespaceReady, "CreateFailed", err.Error(), "")
		return ctrl.Result{}, err
	}

	conditions.SetCondition(&preview.Status.Conditions, conditions.TypePreviewNamespaceReady,
		metav1.ConditionTrue, "Ready", "Preview namespace created", preview.Generation)

	// Sprint 5 / T1.2 — namespace hardening (NetworkPolicy + ResourceQuota +
	// LimitRange). Each is its own condition for finer-grained observability.
	if npErr := r.reconcilePreviewNetworkPolicies(ctx, rtClient, nsName); npErr != nil {
		r.failPreviewCondition(ctx, preview, base, conditions.TypePreviewNetworkPolicyReady, "ApplyFailed", npErr.Error(), "NP failure")
		return ctrl.Result{}, npErr
	}
	conditions.SetCondition(&preview.Status.Conditions, conditions.TypePreviewNetworkPolicyReady,
		metav1.ConditionTrue, "Ready", "Preview network policies applied (deny-all + allow-ingress + allow-egress)", preview.Generation)

	if quotaErr := r.reconcilePreviewQuota(ctx, rtClient, nsName); quotaErr != nil {
		r.failPreviewCondition(ctx, preview, base, conditions.TypePreviewResourceQuotaReady, "ApplyFailed", quotaErr.Error(), "quota failure")
		return ctrl.Result{}, quotaErr
	}
	conditions.SetCondition(&preview.Status.Conditions, conditions.TypePreviewResourceQuotaReady,
		metav1.ConditionTrue, "Ready", "Preview resource quota applied", preview.Generation)
	conditions.SetCondition(&preview.Status.Conditions, conditions.TypePreviewLimitRangeReady,
		metav1.ConditionTrue, "Ready", "Preview limit range applied", preview.Generation)

	// J-23 (2026-06-07) — pods rendered by the API reference SA `loopzero-app`
	// in their spec. Create the SA right after the isolation primitives so
	// ArgoCD sync finds it on first apply. Ordering matters: SA must exist
	// before the first deploy, otherwise pods CrashLoop with "serviceaccount
	// loopzero-app not found" and the workload-readiness gate keeps requeuing.
	if saErr := r.reconcilePreviewServiceAccount(ctx, rtClient, nsName); saErr != nil {
		r.failPreviewCondition(ctx, preview, base, conditions.TypePreviewServiceAccountReady, "ApplyFailed", saErr.Error(), "SA failure")
		return ctrl.Result{}, saErr
	}
	conditions.SetCondition(&preview.Status.Conditions, conditions.TypePreviewServiceAccountReady,
		metav1.ConditionTrue, "Ready", "Preview loopzero-app ServiceAccount provisioned", preview.Generation)

	// Report the URL the API computed (pkg/naming.PreviewURL), eagerly so it
	// is visible while the workload comes up. The controller holds no URL
	// formula of its own: a second copy had already drifted to a host that
	// routed nowhere.
	preview.Status.URL = preview.Spec.PublicURL

	// Sprint 5 / T1.2 — workload-readiness gating. After the namespace and
	// isolation primitives are in place, wait for the build/release flow
	// (driven externally by the API per ADR D4) to signal that the
	// workload is healthy before transitioning to Active. Three
	// conditions are observed in order:
	//
	//   BuildSubmitted   — set by API when SubmitBuild succeeds
	//   BuildCompleted   — set by API on build callback (True=succeeded, False=failed)
	//   WorkloadHealthy  — set by API/controller when ArgoCD reports Healthy
	//
	// If a condition is missing, the controller requeues. After
	// `previewBuildGracePeriod` without BuildSubmitted appearing, the
	// controller falls through to Active anyway — this preserves
	// backward-compatibility for callers that pre-date the build-hook
	// integration (and for tests that exercise the namespace-only path).
	gateResult, gated := r.evaluateWorkloadGate(preview)
	if gated {
		// Persist conditions/phase changes the gate may have set, then
		// honor its requeue/error.
		if statusErr := r.patchStatus(ctx, preview, base); statusErr != nil {
			logger.Error(statusErr, "failed to update preview status while gating on workload conditions")
			return ctrl.Result{}, statusErr
		}
		return gateResult, nil
	}

	preview.Status.Phase = loopzerov1.PreviewPhase(
		previewSM.Transition(statemachine.PPhCreating, statemachine.PPEventCreationCompleted),
	)

	if err := r.patchStatus(ctx, preview, base); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("preview environment created",
		"preview", preview.Name,
		"namespace", nsName,
		"url", preview.Status.URL)

	// Requeue for TTL checking
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// previewBuildGracePeriod is how long the controller waits for the
// BuildSubmitted condition to appear before falling through to Active.
// Below this threshold, missing BuildSubmitted means the build hook
// hasn't reached the CR yet (race with the webhook handler) — keep
// requeuing. Beyond it, the controller assumes there is no build flow
// for this CR (e.g., test, manual creation) and proceeds.
const (
	previewBuildGracePeriod           = 90 * time.Second
	previewBuildSubmittedRequeueWait  = 30 * time.Second
	previewBuildCompletedRequeueWait  = 30 * time.Second
	previewWorkloadHealthyRequeueWait = 10 * time.Second
)

// evaluateWorkloadGate checks the three workload-readiness conditions
// and decides whether to proceed (gated=false) or requeue (gated=true,
// with the appropriate Result).
//
// Side effects: may set preview.Status.Phase to Failed via the state
// machine if BuildCompleted=False. Caller is responsible for persisting
// the Status with r.Status().Patch off the base snapshot — never Update
// (ADR 2026-08-12-status-writer-contract).
func (r *PreviewReconciler) evaluateWorkloadGate(preview *loopzerov1.PreviewEnvironment) (ctrl.Result, bool) {
	now := time.Now()

	submitted := findCondition(preview.Status.Conditions, conditions.TypePreviewBuildSubmitted)
	if submitted == nil {
		// No build hook reached the CR yet. Use grace period to decide
		// whether to wait for it or proceed anyway.
		if !previewWithinGracePeriod(preview, now) {
			// Grace expired — assume no build flow, proceed.
			return ctrl.Result{}, false
		}
		return ctrl.Result{RequeueAfter: previewBuildSubmittedRequeueWait}, true
	}
	if submitted.Status != metav1.ConditionTrue {
		// API set BuildSubmitted=False — surface as build failure.
		preview.Status.Phase = loopzerov1.PreviewPhase(
			previewSM.Transition(statemachine.PPhCreating, statemachine.PPEventCreationFailed),
		)
		return ctrl.Result{}, true
	}

	completed := findCondition(preview.Status.Conditions, conditions.TypePreviewBuildCompleted)
	if completed == nil {
		return ctrl.Result{RequeueAfter: previewBuildCompletedRequeueWait}, true
	}
	if completed.Status != metav1.ConditionTrue {
		preview.Status.Phase = loopzerov1.PreviewPhase(
			previewSM.Transition(statemachine.PPhCreating, statemachine.PPEventCreationFailed),
		)
		return ctrl.Result{}, true
	}

	healthy := findCondition(preview.Status.Conditions, conditions.TypePreviewWorkloadHealthy)
	if healthy == nil || healthy.Status != metav1.ConditionTrue {
		return ctrl.Result{RequeueAfter: previewWorkloadHealthyRequeueWait}, true
	}

	// All three conditions are True — proceed to Active.
	return ctrl.Result{}, false
}

// previewWithinGracePeriod reports whether the preview is still within
// the build-submission grace window. Anchored at preview.Status.CreatedAt
// (set on first reconcile); falls back to ObjectMeta.CreationTimestamp
// when CreatedAt is nil.
func previewWithinGracePeriod(preview *loopzerov1.PreviewEnvironment, now time.Time) bool {
	anchor := time.Time{}
	if preview.Status.CreatedAt != nil {
		anchor = preview.Status.CreatedAt.Time
	} else if !preview.CreationTimestamp.IsZero() {
		anchor = preview.CreationTimestamp.Time
	}
	if anchor.IsZero() {
		// No anchor — be conservative, treat as within grace.
		return true
	}
	return now.Sub(anchor) < previewBuildGracePeriod
}

// findCondition returns the named condition or nil if absent.
func findCondition(conds []metav1.Condition, name string) *metav1.Condition {
	for i := range conds {
		if conds[i].Type == name {
			return &conds[i]
		}
	}
	return nil
}

// reconcileActive checks TTL and health. The `now` parameter is the
// canonical "current time" for this reconcile pass — captured once at
// Reconcile entry — so TTL comparison is deterministic under mock clocks
// and consistent with other time-dependent decisions in the same pass.
func (r *PreviewReconciler) reconcileActive(ctx context.Context, preview, base *loopzerov1.PreviewEnvironment, now time.Time) (ctrl.Result, error) {
	// Check TTL — transition Active -> Expiring via state machine.
	if preview.Spec.ExpiresAt != nil && now.After(preview.Spec.ExpiresAt.Time) {
		log.FromContext(ctx).Info("preview TTL expired, transitioning to Expiring",
			"preview", preview.Name,
			"expiresAt", preview.Spec.ExpiresAt.Time)
		preview.Status.Phase = loopzerov1.PreviewPhase(
			previewSM.Transition(statemachine.PPhActive, statemachine.PPEventExpirationTriggered),
		)
		if err := r.patchStatus(ctx, preview, base); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Still active, requeue for next TTL check
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// reconcileExpiring starts cleanup of the preview by deleting the CR.
// The finalizer in reconcileDelete handles namespace removal; once the
// namespace is gone, the finalizer is dropped and K8s garbage-collects
// the CR. Phase remains Expiring during this brief window — there is no
// observable Deleted state because the CR vanishes before the controller
// can set it. The state machine transition Expiring -> Deleted is
// symbolic and applied at the end of reconcileDelete.
func (r *PreviewReconciler) reconcileExpiring(ctx context.Context, preview *loopzerov1.PreviewEnvironment) (ctrl.Result, error) {

	// Trigger deletion by removing the CR (finalizer will handle cleanup)
	if err := r.Delete(ctx, preview); err != nil && !errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// previewCleanupAnnotation tracks how many cleanup attempts the controller
// has made. After previewMaxCleanupAttempts the preview transitions to
// CLEANUP_FAILED so the operator can intervene rather than loop forever.
const (
	previewCleanupAnnotation   = "loopzero.io/preview-cleanup-attempts"
	previewMaxCleanupAttempts  = 3
	previewCleanupRequeueDelay = 5 * time.Second
)

// reconcileDelete handles cleanup when the CR is being deleted.
//
// Sprint 5 / T1.3 — adds:
//   - per-step conditions (ApplicationDeleted, NamespaceDeleted) for
//     finer-grained operator UX
//   - retry counter persisted as an annotation on the CR (so the count
//     survives reconcile restarts)
//   - CLEANUP_FAILED state transition after previewMaxCleanupAttempts
//
// Application CR cleanup: the controller has no direct ArgoCD client (it
// runs on the mgmt cluster but does not own the ArgoCD types). The
// Application CR for a preview is created by the API (PreviewService /
// gitops flow). When it exists, the API is responsible for deleting it
// before the controller drops the finalizer; here we only confirm the
// LoopZero-side namespace is gone. (Per ADR D7 — controller owns operational
// state in the runtime cluster; API owns gitops + Application CRs.)
func (r *PreviewReconciler) reconcileDelete(ctx context.Context, preview *loopzerov1.PreviewEnvironment) (ctrl.Result, error) {
	// This path's own base. Reconcile branches here BEFORE the finalizer block,
	// so the snapshot the normal path takes does not exist yet — and one taken at
	// the top of Reconcile to serve both would be stale for the normal path.
	// Two snapshots in disjoint scopes make picking the wrong one impossible.
	base := preview.DeepCopy()

	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(preview, previewFinalizer) {
		return ctrl.Result{}, nil
	}

	// Delete namespace on runtime cluster (T0.2 / D6: canonical naming).
	nsName, err := r.previewNamespaceName(preview)
	if err != nil {
		// Naming inputs invalid — drop finalizer to avoid infinite loop on
		// a malformed CR. The namespace, if any, was never created with a
		// valid name we can target.
		logger.Error(err, "preview cleanup: invalid naming inputs, dropping finalizer", "preview", preview.Name)
		controllerutil.RemoveFinalizer(preview, previewFinalizer)
		if updErr := r.Update(ctx, preview); updErr != nil {
			return ctrl.Result{}, updErr
		}
		return ctrl.Result{}, nil
	}

	// Phase 1 — namespace deletion attempt, in the preview's cell.
	rtClient, err := r.runtimeClientForPreview(ctx, preview)
	if err != nil {
		if !errors.IsNotFound(err) || conditions.IsConditionTrue(preview.Status.Conditions, conditions.TypePreviewNamespaceReady) {
			// A namespace was provisioned (or may have been) and its cell is
			// unknown: releasing the finalizer would leak it.
			return ctrl.Result{}, err
		}
		// Tenant gone, no recorded cell, and no namespace was ever provisioned.
		rtClient = nil
	}
	if rtClient == nil {
		return r.completePreviewDeletion(ctx, preview, base)
	}
	ns := &corev1.Namespace{}
	getErr := rtClient.Get(ctx, client.ObjectKey{Name: nsName}, ns)
	if getErr == nil {
		// Namespace still exists.
		if ns.DeletionTimestamp.IsZero() {
			delErr := rtClient.Delete(ctx, ns)
			if delErr != nil && !errors.IsNotFound(delErr) {
				attempts := r.bumpCleanupAttempts(preview)
				logger.Error(delErr, "preview cleanup: namespace delete failed",
					"namespace", nsName, "attempts", attempts)

				conditions.SetCondition(&preview.Status.Conditions, conditions.TypePreviewNamespaceDeleted,
					metav1.ConditionFalse, "DeleteFailed", delErr.Error(), preview.Generation)

				if attempts >= previewMaxCleanupAttempts {
					// T1.4 of deep-review-2026-05-10-remediation-plan
					// (find_state_preview_panic HIGH): the reconcileDelete
					// path runs for previews in EITHER `Expiring` OR
					// `Deleting` phase. Hardcoding `PreviewExpiring` as the
					// source panic'd when called from the Deleting branch.
					// TryTransition + the actual current phase eliminate the panic.
					nextState, smErr := previewSM.TryTransition(
						statemachine.PreviewPhase(preview.Status.Phase),
						statemachine.PPEventCleanupFailed,
					)
					if smErr != nil {
						logger.Error(smErr, "preview cleanup: invalid SM transition",
							"phase", preview.Status.Phase)
					} else {
						preview.Status.Phase = loopzerov1.PreviewPhase(nextState)
					}
					if statusErr := r.patchStatus(ctx, preview, base); statusErr != nil {
						logger.Error(statusErr, "preview cleanup: failed to update CleanupFailed status")
					}
					// Persist the bumped annotation BEFORE returning so the
					// next reconcile sees the latest count.
					if updateErr := r.Update(ctx, preview); updateErr != nil {
						logger.Error(updateErr, "preview cleanup: failed to persist attempt counter")
					}
					return ctrl.Result{}, delErr
				}

				if updateErr := r.Update(ctx, preview); updateErr != nil {
					logger.Error(updateErr, "preview cleanup: failed to persist attempt counter")
				}
				return ctrl.Result{RequeueAfter: previewCleanupRequeueDelay}, delErr
			}
		}
		// Namespace exists (either we just issued Delete or its DeletionTimestamp
		// is set). Requeue until it's actually gone.
		return ctrl.Result{RequeueAfter: previewCleanupRequeueDelay}, nil
	}
	if !errors.IsNotFound(getErr) {
		// Transient API error during Get — retry.
		return ctrl.Result{RequeueAfter: previewCleanupRequeueDelay}, getErr
	}

	return r.completePreviewDeletion(ctx, preview, base)
}

// completePreviewDeletion records that the preview's namespace is gone and
// drops the finalizer.
func (r *PreviewReconciler) completePreviewDeletion(ctx context.Context, preview, base *loopzerov1.PreviewEnvironment) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Phase 2 — namespace gone. Mark the conditions, drop the finalizer.
	conditions.SetCondition(&preview.Status.Conditions, conditions.TypePreviewNamespaceDeleted,
		metav1.ConditionTrue, "Deleted", "Preview namespace removed", preview.Generation)
	conditions.SetCondition(&preview.Status.Conditions, conditions.TypePreviewApplicationDeleted,
		metav1.ConditionTrue, "Deleted",
		// This used to say "Application CR deletion is owned by the API
		// (best-effort)", and the api never did it. The orphan Application is
		// removed by the tenant-operator reaper (pruneOrphanPreviewApps), which
		// derives the expected set and deletes the rest. ADR
		// 2026-08-12-argocd-application-ownership (B-050): the operator owns spec.
		"Namespace cascade removed the in-cluster workload; the orphan Application is reaped by the tenant operator.",
		preview.Generation)

	// Status update is best-effort here — even if it fails, dropping the
	// finalizer must succeed or the CR is stuck forever.
	if statusErr := r.patchStatus(ctx, preview, base); statusErr != nil {
		logger.Error(statusErr, "preview cleanup: status update failed (continuing)")
	}

	controllerutil.RemoveFinalizer(preview, previewFinalizer)
	if err := r.Update(ctx, preview); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("preview cleanup complete", "preview", preview.Name)
	return ctrl.Result{}, nil
}

// bumpCleanupAttempts increments the cleanup attempt counter stored on
// the CR as an annotation and returns the new value. Annotations survive
// reconcile restarts so the count is durable.
func (r *PreviewReconciler) bumpCleanupAttempts(preview *loopzerov1.PreviewEnvironment) int {
	if preview.Annotations == nil {
		preview.Annotations = make(map[string]string)
	}
	current := 0
	if raw, ok := preview.Annotations[previewCleanupAnnotation]; ok {
		// Best-effort parse: malformed annotation falls back to current=0 and
		// the counter starts fresh. Scan failure is non-fatal — the annotation
		// is operator-set hint, not durable state.
		_, _ = fmt.Sscanf(raw, "%d", &current)
	}
	current++
	preview.Annotations[previewCleanupAnnotation] = fmt.Sprintf("%d", current)
	return current
}

// SetupPreviewController registers the PreviewController with the manager.
// factory is required: it is the only way this controller reaches a runtime
// cluster, and it must resolve the same cell the preview's ArgoCD Application
// targets.
//
// drift-baseline.md port #3: reads INGRESS_NAMESPACE from environment so
// preview NetworkPolicies allow ingress from the same namespace where the
// ingress controller (Traefik) runs. Empty value falls back to
// constants.NamespaceIngress so allow-ingress-traefik NetworkPolicies in
// preview namespaces match those in regular environments.
func SetupPreviewController(mgr ctrl.Manager, factory cellrouting.RuntimeClientFactory) error {
	if factory == nil {
		return fmt.Errorf("preview controller: a runtime client factory is required")
	}
	ingressNS := os.Getenv("INGRESS_NAMESPACE")
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopzerov1.PreviewEnvironment{}, reconcileloop.ForOption()).
		Named("preview").
		Complete(&PreviewReconciler{
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
func (r *PreviewReconciler) patchStatus(ctx context.Context, preview, base *loopzerov1.PreviewEnvironment) error {
	conditions.SetReadinessSummaryViaPkg(&preview.Status.Conditions, preview.Generation, previewDetailTypes)
	return r.Status().Patch(ctx, preview, client.MergeFrom(base))
}
