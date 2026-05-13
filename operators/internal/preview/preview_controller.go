// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/conditions"
	"github.com/loopzero/loopzero/operators/internal/constants"
	"github.com/loopzero/loopzero/operators/internal/shared"
	"github.com/loopzero/loopzero/pkg/naming"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

const previewFinalizer = "loopzero.io/preview-cleanup"

// previewSM is the PreviewEnvironment lifecycle state machine (spec §5.3).
// T2.1 / COMP-002: phase transitions in this controller are now routed
// through the state machine instead of direct Status.Phase assignments.
var previewSM = statemachine.NewPreviewMachine()

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
type PreviewReconciler struct {
	client.Client
	// RuntimeClient is the FALLBACK runtime cluster client used when
	// RuntimeFactory is nil OR cell resolution fails. Pre-T1.1 this was
	// the only path; post-T1.1 it remains as a back-compat fallback.
	RuntimeClient client.Client
	// RuntimeFactory resolves a per-cell runtime client from a tenant's
	// CellID. T1.1 of deep-review-remediation-plan (closes find_arch_002 HIGH):
	// when set, every reconcile path that touches the runtime cluster
	// (namespace, NetworkPolicy, ResourceQuota, LimitRange, Service)
	// consults this factory to route to the tenant's pinned cell instead
	// of the static RuntimeClient.
	//
	// Fallback semantics:
	//   - RuntimeFactory == nil → use RuntimeClient (legacy behavior).
	//   - RuntimeFactory.Resolve(cellID) returns error → log + use
	//     RuntimeClient with metric increment (degraded mode).
	//
	// CellID source priority:
	//   1. preview.Spec.CellID (set by API at PreviewEnvironment creation)
	//   2. tenant.Spec.CellID (lookup; covers legacy CRs created pre-T1.1)
	//   3. Empty → fallback RuntimeClient (last resort)
	RuntimeFactory shared.RuntimeClientFactory
	Scheme         *runtime.Scheme
	BaseDomain     string
	// IngressNamespace is the namespace where Traefik runs.
	// Same semantics as EnvironmentReconciler.IngressNamespace —
	// empty string falls back to constants.NamespaceIngress (`traefik`).
	// drift-baseline.md port #3.
	IngressNamespace string
}

// runtimeClientForPreview resolves the appropriate runtime client for a
// PreviewEnvironment based on its CellID. T1.1 of deep-review-remediation-plan.
//
// Returns the cell-specific client when RuntimeFactory is wired and the
// preview has a CellID. Falls back to the static RuntimeClient when:
//   - RuntimeFactory is nil (legacy deployment), OR
//   - preview.Spec.CellID is empty AND tenant CellID lookup fails, OR
//   - factory.Resolve returns an error (network issue, ClusterCredential
//     missing — degraded mode)
//
// EC-7 of edge-case review: legacy CRs created BEFORE T1.1 don't have
// CellID populated. We attempt to read tenant.Spec.CellID via the mgmt
// client (r.Client). If that ALSO fails, we use RuntimeClient and emit
// a metric so operators see the legacy-CR drift.
func (r *PreviewReconciler) runtimeClientForPreview(ctx context.Context, preview *loopzerov1.PreviewEnvironment) client.Client {
	if r.RuntimeFactory == nil {
		return r.RuntimeClient
	}

	cellID := preview.Spec.CellID
	if cellID == "" {
		// Legacy CR fallback: read tenant.Spec.CellID. Per EC-7 we MUST
		// fallback to tenant lookup, NOT to default cell.
		tenant := &loopzerov1.Tenant{}
		err := r.Client.Get(ctx, client.ObjectKey{Name: preview.Spec.TenantID, Namespace: preview.Namespace}, tenant)
		if err == nil {
			cellID = tenant.Spec.CellID
		}
	}
	if cellID == "" {
		return r.RuntimeClient
	}

	cellClient, err := r.RuntimeFactory.For(ctx, cellID)
	if err != nil || cellClient == nil {
		// Degraded mode — log + fallback. Don't fail the reconcile because
		// preview reconcile is best-effort eventual; we'll retry next pass.
		return r.RuntimeClient
	}
	return cellClient
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
	ctx, span := shared.StartReconcileSpan(ctx, "preview", req.Name, req.Namespace)
	defer span.End()
	start := time.Now()
	defer func() {
		if retErr != nil {
			shared.RecordError(span, retErr)
		} else {
			shared.SetSuccess(span)
		}
		shared.ReconcileOutcome("preview", start, retErr)
	}()

	logger := log.FromContext(ctx)

	var preview loopzerov1.PreviewEnvironment
	if err := r.Get(ctx, req.NamespacedName, &preview); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// kstatus universal per ADR-0025 D3 condition #1.
	defer func() {
		latest := &loopzerov1.PreviewEnvironment{}
		if err := r.Get(ctx, req.NamespacedName, latest); err != nil {
			return
		}
		if !latest.DeletionTimestamp.IsZero() {
			return
		}
		conditions.SetReadinessSummaryViaPkg(&latest.Status.Conditions, latest.Generation, previewDetailTypes)
		_ = r.Status().Update(ctx, latest)
	}()

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

	// Initialize phase via state machine (T2.1 / COMP-002).
	if preview.Status.Phase == "" {
		preview.Status.Phase = loopzerov1.PreviewPhaseCreating
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
		return r.reconcileCreating(ctx, &preview)
	case loopzerov1.PreviewPhaseActive:
		return r.reconcileActive(ctx, &preview, start)
	case loopzerov1.PreviewPhaseExpiring:
		return r.reconcileExpiring(ctx, &preview)
	default:
		logger.Info("preview in terminal phase, no action", "phase", preview.Status.Phase)
		return ctrl.Result{}, nil
	}
}

// previewNamespaceName returns the namespace name for the preview using
// the canonical naming.PreviewNamespace helper. Format:
// {tenantSlug}-{projectSlug}-pr-{prNumber}. Returns an error if any input
// is invalid (T0.2 / D6).
func (r *PreviewReconciler) previewNamespaceName(preview *loopzerov1.PreviewEnvironment) (string, error) {
	return naming.PreviewNamespace(
		preview.Spec.TenantID,
		preview.Spec.ProjectID,
		preview.Spec.PRNumber,
	)
}

// failPreviewCondition is the canonical "set status condition False +
// update + log status-update failure" pattern used throughout
// reconcileCreating. Phase 3 helper: collapses 4 inline 7-line copies
// across NamespaceReady/NetworkPolicyReady/ResourceQuotaReady error paths.
// Returns the err untouched so the caller decides the controller-runtime
// result envelope (some paths want Requeue, others want no-requeue).
func (r *PreviewReconciler) failPreviewCondition(ctx context.Context, preview *loopzerov1.PreviewEnvironment, condType, reason, msg, statusLogContext string) {
	logger := log.FromContext(ctx)
	conditions.SetCondition(&preview.Status.Conditions, condType,
		metav1.ConditionFalse, reason, msg, preview.Generation)
	if statusErr := r.Status().Update(ctx, preview); statusErr != nil {
		if statusLogContext == "" {
			logger.Error(statusErr, "failed to update preview status")
		} else {
			logger.Error(statusErr, "failed to update preview status ("+statusLogContext+")")
		}
	}
}

// reconcileCreating provisions the preview namespace and route.
func (r *PreviewReconciler) reconcileCreating(ctx context.Context, preview *loopzerov1.PreviewEnvironment) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	nsName, err := r.previewNamespaceName(preview)
	if err != nil {
		r.failPreviewCondition(ctx, preview, conditions.TypePreviewNamespaceReady, "InvalidNaming", err.Error(), "")
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
	rtClient := r.runtimeClientForPreview(ctx, preview)
	if _, err := controllerutil.CreateOrUpdate(ctx, rtClient, ns, func() error {
		ns.Labels["pod-security.kubernetes.io/enforce"] = "restricted"
		ns.Labels["pod-security.kubernetes.io/warn"] = "restricted"
		return nil
	}); err != nil {
		r.failPreviewCondition(ctx, preview, conditions.TypePreviewNamespaceReady, "CreateFailed", err.Error(), "")
		return ctrl.Result{}, err
	}

	conditions.SetCondition(&preview.Status.Conditions, conditions.TypePreviewNamespaceReady,
		metav1.ConditionTrue, "Ready", "Preview namespace created", preview.Generation)

	// Sprint 5 / T1.2 — namespace hardening (NetworkPolicy + ResourceQuota +
	// LimitRange). Each is its own condition for finer-grained observability.
	if npErr := r.reconcilePreviewNetworkPolicies(ctx, nsName); npErr != nil {
		r.failPreviewCondition(ctx, preview, conditions.TypePreviewNetworkPolicyReady, "ApplyFailed", npErr.Error(), "NP failure")
		return ctrl.Result{}, npErr
	}
	conditions.SetCondition(&preview.Status.Conditions, conditions.TypePreviewNetworkPolicyReady,
		metav1.ConditionTrue, "Ready", "Preview network policies applied (deny-all + allow-ingress + allow-egress)", preview.Generation)

	if quotaErr := r.reconcilePreviewQuota(ctx, nsName); quotaErr != nil {
		r.failPreviewCondition(ctx, preview, conditions.TypePreviewResourceQuotaReady, "ApplyFailed", quotaErr.Error(), "quota failure")
		return ctrl.Result{}, quotaErr
	}
	conditions.SetCondition(&preview.Status.Conditions, conditions.TypePreviewResourceQuotaReady,
		metav1.ConditionTrue, "Ready", "Preview resource quota applied", preview.Generation)
	conditions.SetCondition(&preview.Status.Conditions, conditions.TypePreviewLimitRangeReady,
		metav1.ConditionTrue, "Ready", "Preview limit range applied", preview.Generation)

	// Set URL eagerly so it's visible while the workload comes up.
	preview.Status.URL = fmt.Sprintf("https://pr%d-%s.preview.%s",
		preview.Spec.PRNumber, preview.Spec.ProjectID, r.BaseDomain)

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
		if statusErr := r.Status().Update(ctx, preview); statusErr != nil {
			logger.Error(statusErr, "failed to update preview status while gating on workload conditions")
			return ctrl.Result{}, statusErr
		}
		return gateResult, nil
	}

	preview.Status.Phase = loopzerov1.PreviewPhase(
		previewSM.Transition(statemachine.PreviewCreating, statemachine.PreviewCreationCompleted),
	)

	if err := r.Status().Update(ctx, preview); err != nil {
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
// the Status with r.Status().Update.
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
			previewSM.Transition(statemachine.PreviewCreating, statemachine.PreviewCreationFailed),
		)
		return ctrl.Result{}, true
	}

	completed := findCondition(preview.Status.Conditions, conditions.TypePreviewBuildCompleted)
	if completed == nil {
		return ctrl.Result{RequeueAfter: previewBuildCompletedRequeueWait}, true
	}
	if completed.Status != metav1.ConditionTrue {
		preview.Status.Phase = loopzerov1.PreviewPhase(
			previewSM.Transition(statemachine.PreviewCreating, statemachine.PreviewCreationFailed),
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
func (r *PreviewReconciler) reconcileActive(ctx context.Context, preview *loopzerov1.PreviewEnvironment, now time.Time) (ctrl.Result, error) {
	// Check TTL — transition Active -> Expiring via state machine.
	if preview.Spec.ExpiresAt != nil && now.After(preview.Spec.ExpiresAt.Time) {
		log.FromContext(ctx).Info("preview TTL expired, transitioning to Expiring",
			"preview", preview.Name,
			"expiresAt", preview.Spec.ExpiresAt.Time)
		preview.Status.Phase = loopzerov1.PreviewPhase(
			previewSM.Transition(statemachine.PreviewActive, statemachine.PreviewExpirationTriggered),
		)
		if err := r.Status().Update(ctx, preview); err != nil {
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

	// Phase 1 — namespace deletion attempt.
	// T0.1 of deep-review-2026-05-10-remediation-plan (find_arch_002):
	// route namespace ops through the cell-aware client so multi-cell
	// previews land in the correct workload cluster.
	rtClient := r.runtimeClientForPreview(ctx, preview)
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
						statemachine.State(preview.Status.Phase),
						statemachine.PreviewCleanupFailed_,
					)
					if smErr != nil {
						logger.Error(smErr, "preview cleanup: invalid SM transition",
							"phase", preview.Status.Phase)
					} else {
						preview.Status.Phase = loopzerov1.PreviewPhase(nextState)
					}
					if statusErr := r.Status().Update(ctx, preview); statusErr != nil {
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

	// Phase 2 — namespace gone. Mark the conditions, drop the finalizer.
	conditions.SetCondition(&preview.Status.Conditions, conditions.TypePreviewNamespaceDeleted,
		metav1.ConditionTrue, "Deleted", "Preview namespace removed", preview.Generation)
	conditions.SetCondition(&preview.Status.Conditions, conditions.TypePreviewApplicationDeleted,
		metav1.ConditionTrue, "Deleted",
		"Application CR deletion is owned by the API (best-effort). Namespace cascade removed in-cluster workload.",
		preview.Generation)

	// Status update is best-effort here — even if it fails, dropping the
	// finalizer must succeed or the CR is stuck forever.
	if statusErr := r.Status().Update(ctx, preview); statusErr != nil {
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
		fmt.Sscanf(raw, "%d", &current)
	}
	current++
	preview.Annotations[previewCleanupAnnotation] = fmt.Sprintf("%d", current)
	return current
}

// SetupPreviewController registers the PreviewController with the manager.
//
// drift-baseline.md port #3: reads INGRESS_NAMESPACE from environment so
// preview NetworkPolicies allow ingress from the same namespace where the
// ingress controller (Traefik) runs. Empty value falls back to
// constants.NamespaceIngress so allow-ingress-traefik NetworkPolicies in
// preview namespaces match those in regular environments.
func SetupPreviewController(mgr ctrl.Manager, runtimeClient client.Client, baseDomain string) error {
	return setupPreviewControllerWithFactory(mgr, runtimeClient, nil, baseDomain)
}

// setupPreviewControllerWithFactory wires the PreviewReconciler with an
// optional cell-aware RuntimeClientFactory. T1.1 of deep-review-remediation-plan
// (closes find_arch_002 HIGH).
//
// When factory is nil, behavior is identical to SetupPreviewController
// (single-cell legacy path). When factory is set, every runtime-cluster
// operation routes through factory.For(cellID) per the
// runtimeClientForPreview() resolution chain.
//
// Audit gap (registration_test.go COMP-001 family, audit §6.F.1b
// hygiene): made unexported because nothing outside this package calls
// it directly — all callers route through SetupPreviewController(which
// is the canonical exported entry point). Re-export when a real
// cell-aware caller emerges.
func setupPreviewControllerWithFactory(
	mgr ctrl.Manager,
	runtimeClient client.Client,
	factory shared.RuntimeClientFactory,
	baseDomain string,
) error {
	ingressNS := os.Getenv("INGRESS_NAMESPACE")
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopzerov1.PreviewEnvironment{}, shared.ForOption()).
		Named("preview").
		Complete(&PreviewReconciler{
			Client:           mgr.GetClient(),
			RuntimeClient:    runtimeClient,
			RuntimeFactory:   factory,
			Scheme:           mgr.GetScheme(),
			BaseDomain:       baseDomain,
			IngressNamespace: ingressNS,
		})
}
