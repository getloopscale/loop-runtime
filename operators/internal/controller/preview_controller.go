// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller

import (
	"context"
	"fmt"
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
	"github.com/loopzero/loopzero/pkg/statemachine"
)

const previewFinalizer = "loopzero.io/preview-cleanup"

// previewSM will be used when preview environment logic is implemented (Sprint 5).
var _ = statemachine.NewPreviewMachine

// PreviewReconciler reconciles PreviewEnvironment CRs.
// Creates temporary namespaces on the runtime cluster for PR previews.
type PreviewReconciler struct {
	client.Client
	RuntimeClient client.Client
	Scheme        *runtime.Scheme
	BaseDomain    string
}

// Reconcile handles the reconciliation loop for PreviewEnvironment resources.
func (r *PreviewReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
	ctx, span := startReconcileSpan(ctx, "preview", req.Name, req.Namespace)
	defer span.End()
	start := time.Now()
	defer func() {
		if retErr != nil {
			recordError(span, retErr)
		} else {
			setSuccess(span)
		}
		reconcileOutcome("preview", start, retErr)
	}()

	logger := log.FromContext(ctx)

	var preview loopzerov1.PreviewEnvironment
	if err := r.Get(ctx, req.NamespacedName, &preview); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

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

	// Initialize phase
	if preview.Status.Phase == "" {
		preview.Status.Phase = loopzerov1.PreviewPhaseCreating
		now := metav1.Now()
		preview.Status.CreatedAt = &now
	}

	// Phase dispatch
	switch preview.Status.Phase {
	case loopzerov1.PreviewPhaseCreating:
		return r.reconcileCreating(ctx, &preview)
	case loopzerov1.PreviewPhaseActive:
		return r.reconcileActive(ctx, &preview)
	case loopzerov1.PreviewPhaseExpiring:
		return r.reconcileExpiring(ctx, &preview)
	default:
		logger.Info("preview in terminal phase, no action", "phase", preview.Status.Phase)
		return ctrl.Result{}, nil
	}
}

// reconcileCreating provisions the preview namespace and route.
func (r *PreviewReconciler) reconcileCreating(ctx context.Context, preview *loopzerov1.PreviewEnvironment) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	nsName := fmt.Sprintf("preview-%s-pr%d", preview.Spec.TenantID, preview.Spec.PRNumber)

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
	if _, err := controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, ns, func() error {
		ns.Labels["pod-security.kubernetes.io/enforce"] = "restricted"
		ns.Labels["pod-security.kubernetes.io/warn"] = "restricted"
		return nil
	}); err != nil {
		conditions.SetCondition(&preview.Status.Conditions, conditions.TypePreviewNamespaceReady,
			metav1.ConditionFalse, "CreateFailed", err.Error(), preview.Generation)
		if statusErr := r.Status().Update(ctx, preview); statusErr != nil {
			logger.Error(statusErr, "failed to update preview status")
		}
		return ctrl.Result{}, err
	}

	conditions.SetCondition(&preview.Status.Conditions, conditions.TypePreviewNamespaceReady,
		metav1.ConditionTrue, "Ready", "Preview namespace created", preview.Generation)

	// Set URL and transition to Active
	preview.Status.URL = fmt.Sprintf("https://pr%d-%s.preview.%s",
		preview.Spec.PRNumber, preview.Spec.ProjectID, r.BaseDomain)
	preview.Status.Phase = loopzerov1.PreviewPhaseActive

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

// reconcileActive checks TTL and health.
func (r *PreviewReconciler) reconcileActive(ctx context.Context, preview *loopzerov1.PreviewEnvironment) (ctrl.Result, error) {
	// Check TTL
	if preview.Spec.ExpiresAt != nil && time.Now().After(preview.Spec.ExpiresAt.Time) {
		log.FromContext(ctx).Info("preview TTL expired, transitioning to Expiring",
			"preview", preview.Name,
			"expiresAt", preview.Spec.ExpiresAt.Time)
		preview.Status.Phase = loopzerov1.PreviewPhaseExpiring
		if err := r.Status().Update(ctx, preview); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Still active, requeue for next TTL check
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// reconcileExpiring starts cleanup of the preview.
func (r *PreviewReconciler) reconcileExpiring(ctx context.Context, preview *loopzerov1.PreviewEnvironment) (ctrl.Result, error) {
	// Transition to Deleting
	preview.Status.Phase = loopzerov1.PreviewPhaseDeleting
	if err := r.Status().Update(ctx, preview); err != nil {
		return ctrl.Result{}, err
	}

	// Trigger deletion by removing the CR (finalizer will handle cleanup)
	if err := r.Delete(ctx, preview); err != nil && !errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// reconcileDelete handles cleanup when the CR is being deleted.
func (r *PreviewReconciler) reconcileDelete(ctx context.Context, preview *loopzerov1.PreviewEnvironment) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(preview, previewFinalizer) {
		return ctrl.Result{}, nil
	}

	// Delete namespace on runtime cluster
	nsName := fmt.Sprintf("preview-%s-pr%d", preview.Spec.TenantID, preview.Spec.PRNumber)
	ns := &corev1.Namespace{}
	if err := r.RuntimeClient.Get(ctx, client.ObjectKey{Name: nsName}, ns); err == nil {
		if ns.DeletionTimestamp.IsZero() {
			if err := r.RuntimeClient.Delete(ctx, ns); err != nil && !errors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}
		// Namespace still exists, requeue
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Namespace gone — remove finalizer
	controllerutil.RemoveFinalizer(preview, previewFinalizer)
	if err := r.Update(ctx, preview); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("preview cleanup complete", "preview", preview.Name)
	return ctrl.Result{}, nil
}

// SetupPreviewController registers the PreviewController with the manager.
func SetupPreviewController(mgr ctrl.Manager, runtimeClient client.Client, baseDomain string) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopzerov1.PreviewEnvironment{}).
		Named("preview").
		Complete(&PreviewReconciler{
			Client:        mgr.GetClient(),
			RuntimeClient: runtimeClient,
			Scheme:        mgr.GetScheme(),
			BaseDomain:    baseDomain,
		})
}
