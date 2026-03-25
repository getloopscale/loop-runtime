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
	"github.com/loopzero/loopzero/pkg/naming"
)

const applicationFinalizer = "loopzero.io/app-cleanup"

// ApplicationReconciler reconciles a LoopZeroApplication object.
// It manages hostname assignment, ExposedRoute creation, and phase tracking.
type ApplicationReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	BaseDomain string
}

// Reconcile handles the reconciliation loop for LoopZeroApplication resources.
func (r *ApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var app loopzerov1.LoopZeroApplication
	if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion with verify-then-remove finalizer pattern
	if !app.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &app)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(&app, applicationFinalizer) {
		controllerutil.AddFinalizer(&app, applicationFinalizer)
		if err := r.Update(ctx, &app); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Step 1: Compute hostname if empty
	if app.Spec.Hostname == "" {
		app.Spec.Hostname = naming.Hostname(app.Spec.Project, app.Spec.Environment, r.BaseDomain)
		if err := r.Update(ctx, &app); err != nil {
			return ctrl.Result{}, fmt.Errorf("setting hostname: %w", err)
		}
		// Re-fetch after update to avoid stale resourceVersion
		if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Step 2: Set URL
	app.Status.URL = "https://" + app.Spec.Hostname

	// Step 3: Set ServiceResolved condition
	conditions.SetCondition(&app.Status.Conditions, conditions.TypeServiceResolved,
		metav1.ConditionTrue, conditions.ReasonReady,
		fmt.Sprintf("service %s resolved", app.Spec.ServiceName),
		app.Generation)

	// Step 4: Ensure ExposedRoute CR exists (CreateOrUpdate with ownerReference)
	routeName := app.Name + "-route"
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
		route.Spec.TLS = loopzerov1.TLSConfig{
			Enabled: true,
		}
		return controllerutil.SetControllerReference(&app, route, r.Scheme)
	})
	if err != nil {
		conditions.SetCondition(&app.Status.Conditions, conditions.TypeIngressReady,
			metav1.ConditionFalse, conditions.ReasonFailed,
			fmt.Sprintf("failed to ensure ExposedRoute: %v", err),
			app.Generation)
		if statusErr := r.Status().Update(ctx, &app); statusErr != nil {
			logger.Error(statusErr, "failed to update application status")
		}
		return ctrl.Result{}, fmt.Errorf("ensuring ExposedRoute %s: %w", routeName, err)
	}
	logger.Info("ExposedRoute reconciled", "route", routeName, "result", result)

	conditions.SetCondition(&app.Status.Conditions, conditions.TypeIngressReady,
		metav1.ConditionTrue, conditions.ReasonReady,
		"ExposedRoute created", app.Generation)

	// Step 5: Read ExposedRoute status and update Application reachability
	if err := r.Get(ctx, client.ObjectKeyFromObject(route), route); err == nil {
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
	}

	// Step 6: Determine phase
	app.Status.Phase = r.computePhase(&app)

	// Step 7: Set ApplicationHealthy condition based on phase
	if app.Status.Phase == loopzerov1.ApplicationPhaseReachable || app.Status.Phase == loopzerov1.ApplicationPhaseServing {
		conditions.SetCondition(&app.Status.Conditions, conditions.TypeApplicationHealthy,
			metav1.ConditionTrue, conditions.ReasonReady,
			"application is healthy", app.Generation)
	} else {
		conditions.SetCondition(&app.Status.Conditions, conditions.TypeApplicationHealthy,
			metav1.ConditionFalse, conditions.ReasonReconciling,
			fmt.Sprintf("application phase: %s", app.Status.Phase), app.Generation)
	}

	app.Status.ObservedGeneration = app.Generation

	if err := r.Status().Update(ctx, &app); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating application status: %w", err)
	}

	logger.Info("reconciled application",
		"application", app.Name,
		"phase", app.Status.Phase,
		"hostname", app.Spec.Hostname,
		"url", app.Status.URL)

	return ctrl.Result{}, nil
}

// reconcileDelete handles the verify-then-remove finalizer pattern for Application deletion.
// It waits for all owned ExposedRoute CRs to be deleted before removing the finalizer.
func (r *ApplicationReconciler) reconcileDelete(ctx context.Context, app *loopzerov1.LoopZeroApplication) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(app, applicationFinalizer) {
		return ctrl.Result{}, nil
	}

	logger.Info("application is being deleted", "application", app.Name)

	// Check if any owned ExposedRoute CRs still exist
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
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// All ExposedRoutes gone — remove finalizer
	controllerutil.RemoveFinalizer(app, applicationFinalizer)
	if err := r.Update(ctx, app); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("application deletion complete", "application", app.Name)
	return ctrl.Result{}, nil
}

// computePhase determines the ApplicationPhase based on current state.
func (r *ApplicationReconciler) computePhase(app *loopzerov1.LoopZeroApplication) loopzerov1.ApplicationPhase {
	if app.Status.Reachable {
		return loopzerov1.ApplicationPhaseReachable
	}
	if app.Status.EndpointsReady {
		return loopzerov1.ApplicationPhaseServing
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
