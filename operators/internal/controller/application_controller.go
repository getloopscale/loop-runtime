// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller

import (
	"context"
	"fmt"
	"time"

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
	"github.com/loopzero/loopzero/pkg/naming"
)

const applicationFinalizer = constants.FinalizerApplication

// ApplicationReconciler reconciles a LoopZeroApplication object.
// It manages hostname assignment, ExposedRoute creation, and phase tracking.
type ApplicationReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	BaseDomain string
}

// Reconcile handles the reconciliation loop for LoopZeroApplication resources.
func (r *ApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
	ctx, span := startReconcileSpan(ctx, "application", req.Name, req.Namespace)
	defer span.End()
	start := time.Now()
	defer func() {
		if retErr != nil {
			recordError(span, retErr)
		} else {
			setSuccess(span)
		}
		reconcileOutcome("application", start, retErr)
	}()

	logger := log.FromContext(ctx)

	var app loopzerov1.LoopZeroApplication
	if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !app.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &app)
	}

	if !controllerutil.ContainsFinalizer(&app, applicationFinalizer) {
		controllerutil.AddFinalizer(&app, applicationFinalizer)
		if err := r.Update(ctx, &app); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.reconcileHostname(ctx, req, &app); err != nil {
		return ctrl.Result{}, err
	}

	app.Status.URL = "https://" + app.Spec.Hostname

	conditions.SetCondition(&app.Status.Conditions, conditions.TypeServiceResolved,
		metav1.ConditionTrue, conditions.ReasonReady,
		fmt.Sprintf("service %s resolved", app.Spec.ServiceName),
		app.Generation)

	if err := r.reconcileExposedRoute(ctx, &app); err != nil {
		return ctrl.Result{}, err
	}

	r.reconcileRouteStatus(ctx, &app)
	r.reconcileHealthCondition(&app)

	// Save computed status, then re-fetch to get latest ResourceVersion,
	// then apply status via Patch (avoids UID precondition failures from Update).
	computedStatus := app.Status.DeepCopy()
	computedStatus.ObservedGeneration = app.Generation

	if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
		return ctrl.Result{}, fmt.Errorf("re-fetching application: %w", err)
	}
	// MergeFrom captures the clean base; then we mutate status and patch.
	patch := client.MergeFrom(app.DeepCopy())
	app.Status = *computedStatus
	if err := r.Status().Patch(ctx, &app, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("patching application status: %w", err)
	}

	logger.Info("reconciled application",
		"application", app.Name,
		"phase", app.Status.Phase,
		"hostname", app.Spec.Hostname,
		"url", app.Status.URL)

	return ctrl.Result{}, nil
}

// reconcileHostname computes and persists the hostname if not yet assigned.
func (r *ApplicationReconciler) reconcileHostname(ctx context.Context, req ctrl.Request, app *loopzerov1.LoopZeroApplication) error {
	if app.Spec.Hostname != "" {
		return nil
	}
	hostname, err := naming.Hostname(app.Spec.Project, app.Spec.Environment, r.BaseDomain)
	if err != nil {
		return fmt.Errorf("computing hostname: %w", err)
	}
	app.Spec.Hostname = hostname
	if err := r.Update(ctx, app); err != nil {
		return fmt.Errorf("setting hostname: %w", err)
	}
	return r.Get(ctx, req.NamespacedName, app)
}

// reconcileExposedRoute ensures the ExposedRoute CR exists with correct spec.
func (r *ApplicationReconciler) reconcileExposedRoute(ctx context.Context, app *loopzerov1.LoopZeroApplication) error {
	ctx, span := startChildSpan(ctx, "application.exposed_route.ensure")
	defer span.End()

	logger := log.FromContext(ctx)
	routeName := constants.ExposedRouteName(app.Name)

	route := &loopzerov1.ExposedRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: app.Namespace,
		},
	}
	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, route, func() error {
		route.Spec.ApplicationRef = app.Name
		route.Spec.Hostname = app.Spec.Hostname
		route.Spec.ServiceRef = loopzerov1.ServiceRef{
			Name: app.Spec.ServiceName,
			Port: app.Spec.ServicePort,
		}
		route.Spec.TLS = loopzerov1.TLSConfig{Enabled: true}
		return controllerutil.SetControllerReference(app, route, r.Scheme)
	})
	if err != nil {
		conditions.SetCondition(&app.Status.Conditions, conditions.TypeIngressReady,
			metav1.ConditionFalse, conditions.ReasonFailed,
			fmt.Sprintf("failed to ensure ExposedRoute: %v", err),
			app.Generation)
		if statusErr := r.Status().Update(ctx, app); statusErr != nil {
			logger.Error(statusErr, "failed to update application status")
		}
		return fmt.Errorf("ensuring ExposedRoute %s: %w", routeName, err)
	}

	logger.Info("ExposedRoute reconciled", "route", routeName, "result", result)
	conditions.SetCondition(&app.Status.Conditions, conditions.TypeIngressReady,
		metav1.ConditionTrue, conditions.ReasonReady,
		"ExposedRoute created", app.Generation)
	return nil
}

// reconcileRouteStatus reads the ExposedRoute status and updates reachability conditions.
func (r *ApplicationReconciler) reconcileRouteStatus(ctx context.Context, app *loopzerov1.LoopZeroApplication) {
	routeName := constants.ExposedRouteName(app.Name)
	route := &loopzerov1.ExposedRoute{}
	routeKey := client.ObjectKey{Name: routeName, Namespace: app.Namespace}

	if err := r.Get(ctx, routeKey, route); err != nil {
		// AP-3 fix: surface Get failures as a condition rather than silently
		// swallowing them. NotFound is normal during initial reconcile;
		// transient errors must be observable so operators can diagnose
		// reconciler stalls. See ADR-0023 hoist condition + roadmap §AP-3.
		if !errors.IsNotFound(err) {
			log.FromContext(ctx).Error(err, "failed to read ExposedRoute for status sync",
				"route", routeKey)
		}
		conditions.SetCondition(&app.Status.Conditions, conditions.TypeRouteReachable,
			metav1.ConditionUnknown, conditions.ReasonNotFound,
			"ExposedRoute not yet observable", app.Generation)
		return
	}

	app.Status.Reachable = route.Status.Phase == loopzerov1.ExposedRoutePhaseReachable

	if route.Status.CertificateReady {
		conditions.SetCondition(&app.Status.Conditions, conditions.TypeCertificateIssued,
			metav1.ConditionTrue, conditions.ReasonReady,
			"certificate ready", app.Generation)
	} else {
		conditions.SetCondition(&app.Status.Conditions, conditions.TypeCertificateIssued,
			metav1.ConditionFalse, conditions.ReasonWaiting,
			"waiting for certificate", app.Generation)
	}

	if app.Status.Reachable {
		conditions.SetCondition(&app.Status.Conditions, conditions.TypeRouteReachable,
			metav1.ConditionTrue, conditions.ReasonReady,
			"route is reachable", app.Generation)
	} else {
		conditions.SetCondition(&app.Status.Conditions, conditions.TypeRouteReachable,
			metav1.ConditionFalse, conditions.ReasonWaiting,
			"waiting for route to become reachable", app.Generation)
	}

	app.Status.Phase = r.computePhase(app)
}

// reconcileHealthCondition sets the ApplicationHealthy condition based on phase.
func (r *ApplicationReconciler) reconcileHealthCondition(app *loopzerov1.LoopZeroApplication) {
	if app.Status.Phase == loopzerov1.ApplicationPhaseReachable || app.Status.Phase == loopzerov1.ApplicationPhaseServing {
		conditions.SetCondition(&app.Status.Conditions, conditions.TypeApplicationHealthy,
			metav1.ConditionTrue, conditions.ReasonReady,
			"application is healthy", app.Generation)
	} else {
		conditions.SetCondition(&app.Status.Conditions, conditions.TypeApplicationHealthy,
			metav1.ConditionFalse, conditions.ReasonReconciling,
			fmt.Sprintf("application phase: %s", app.Status.Phase), app.Generation)
	}
}

// reconcileDelete handles the verify-then-remove finalizer pattern for Application deletion.
func (r *ApplicationReconciler) reconcileDelete(ctx context.Context, app *loopzerov1.LoopZeroApplication) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(app, applicationFinalizer) {
		return ctrl.Result{}, nil
	}

	logger.Info("application is being deleted", "application", app.Name)

	var routeList loopzerov1.ExposedRouteList
	if err := r.List(ctx, &routeList, client.InNamespace(app.Namespace)); err != nil {
		return ctrl.Result{}, fmt.Errorf("listing child ExposedRoute CRs: %w", err)
	}
	childRouteCount := 0
	for _, route := range routeList.Items {
		for _, ref := range route.OwnerReferences {
			if ref.UID == app.UID {
				childRouteCount++
				break
			}
		}
	}
	if childRouteCount > 0 {
		logger.Info("waiting for child ExposedRoute CRs to be deleted", "remaining", childRouteCount)
		return ctrl.Result{RequeueAfter: constants.RequeueDeletionWait}, nil
	}

	controllerutil.RemoveFinalizer(app, applicationFinalizer)
	if err := r.Update(ctx, app); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("application deletion complete", "application", app.Name)
	return ctrl.Result{}, nil
}

// computePhase determines the ApplicationPhase based on current state.
// Uses all defined phases: Progressing → Serving → Reachable, Degraded, Down.
func (r *ApplicationReconciler) computePhase(app *loopzerov1.LoopZeroApplication) loopzerov1.ApplicationPhase {
	if app.Status.Reachable {
		return loopzerov1.ApplicationPhaseReachable
	}
	if app.Status.EndpointsReady {
		return loopzerov1.ApplicationPhaseServing
	}
	// Check conditions for degraded/down states.
	// "Waiting" is normal progression (not yet reachable), "Failed" is degraded.
	// AP-2 fix: use typed constants from internal/conditions instead of
	// hardcoded string literals. See ADR-0023 hoist condition + roadmap §AP-2.
	for _, c := range app.Status.Conditions {
		if c.Type == conditions.TypeRouteReachable && c.Status == metav1.ConditionFalse {
			switch c.Reason {
			case conditions.ReasonFailed:
				return loopzerov1.ApplicationPhaseDown
			case conditions.ReasonDegraded:
				return loopzerov1.ApplicationPhaseDegraded
			default:
				// "Waiting" or any other reason — still progressing
				return loopzerov1.ApplicationPhaseProgressing
			}
		}
	}
	return loopzerov1.ApplicationPhaseProgressing
}

// SetupApplicationController registers the ApplicationController with the manager.
func SetupApplicationController(mgr ctrl.Manager, baseDomain string) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopzerov1.LoopZeroApplication{}).
		Owns(&loopzerov1.ExposedRoute{}).
		Named("application").
		Complete(&ApplicationReconciler{
			Client:     mgr.GetClient(),
			Scheme:     mgr.GetScheme(),
			BaseDomain: baseDomain,
		})
}
