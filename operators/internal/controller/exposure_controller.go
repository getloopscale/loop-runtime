// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/conditions"
	"github.com/loopzero/loopzero/operators/internal/constants"
	"github.com/loopzero/loopzero/pkg/naming"
)

const (
	// exposureFinalizer is the finalizer name for ExposedRoute cleanup.
	exposureFinalizer = constants.FinalizerExposedRoute

	// probeURLOverrideAnnotation allows tests to inject a probe URL.
	probeURLOverrideAnnotation = constants.AnnotationProbeURLOverride
)

// ExposureReconciler reconciles an ExposedRoute object.
// It manages certificate status checks and reachability probes.
type ExposureReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	HTTPClient *http.Client
}

// Reconcile handles the reconciliation loop for ExposedRoute resources.
func (r *ExposureReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var route loopzerov1.ExposedRoute
	if err := r.Get(ctx, req.NamespacedName, &route); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion — remove finalizer immediately (v1: no external resources to clean up,
	// namespace deletion handles IngressRoute/Certificate cleanup).
	if !route.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&route, exposureFinalizer) {
			controllerutil.RemoveFinalizer(&route, exposureFinalizer)
			if err := r.Update(ctx, &route); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(&route, exposureFinalizer) {
		controllerutil.AddFinalizer(&route, exposureFinalizer)
		if err := r.Update(ctx, &route); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Get(ctx, req.NamespacedName, &route); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Initialize phase if empty
	if route.Status.Phase == "" {
		route.Status.Phase = loopzerov1.ExposedRoutePhasePending
	}

	// Step 1: Check certificate readiness
	certReady := r.checkCertificateReady(ctx, &route)
	route.Status.CertificateReady = certReady

	if certReady {
		conditions.SetCondition(&route.Status.Conditions, conditions.TypeCertificateIssued,
			metav1.ConditionTrue, conditions.ReasonReady,
			"certificate is ready", route.Generation)
	} else {
		conditions.SetCondition(&route.Status.Conditions, conditions.TypeCertificateIssued,
			metav1.ConditionFalse, conditions.ReasonWaiting,
			"waiting for TLS certificate", route.Generation)
	}

	// Step 2: If cert not ready and TLS is required, stay Pending and requeue
	if !certReady && route.Spec.TLS.Enabled {
		route.Status.Phase = loopzerov1.ExposedRoutePhasePending
		route.Status.ObservedGeneration = route.Generation

		if err := r.Status().Update(ctx, &route); err != nil {
			return ctrl.Result{}, fmt.Errorf("updating route status: %w", err)
		}
		logger.Info("certificate not ready, requeuing", "route", route.Name)
		return ctrl.Result{RequeueAfter: constants.RequeueProbeInterval}, nil
	}

	// Cert is ready (or TLS is disabled) — update phase to CertIssued if still Pending
	if route.Status.Phase == loopzerov1.ExposedRoutePhasePending {
		route.Status.Phase = loopzerov1.ExposedRoutePhaseCertIssued
	}

	// Step 3: Perform reachability probe
	probeURL := r.resolveProbeURL(&route)
	probeResult, probeErr := r.performProbe(ctx, probeURL)

	now := metav1.Now()
	route.Status.LastProbeTime = &now
	route.Status.LastProbeResult = probeResult

	if probeErr == nil && probeResult >= 200 && probeResult < 400 {
		// Probe succeeded
		route.Status.ConsecutiveFailures = 0
		route.Status.Phase = loopzerov1.ExposedRoutePhaseReachable
		conditions.SetCondition(&route.Status.Conditions, conditions.TypeRouteReachable,
			metav1.ConditionTrue, conditions.ReasonReady,
			fmt.Sprintf("probe returned %d", probeResult), route.Generation)
	} else {
		// Probe failed
		route.Status.ConsecutiveFailures++
		msg := fmt.Sprintf("probe failed: status %d", probeResult)
		if probeErr != nil {
			msg = fmt.Sprintf("probe failed: %v", probeErr)
		}

		if route.Status.ConsecutiveFailures >= constants.MaxConsecutiveProbeFailures {
			route.Status.Phase = loopzerov1.ExposedRoutePhaseFailed
			conditions.SetCondition(&route.Status.Conditions, conditions.TypeRouteReachable,
				metav1.ConditionFalse, conditions.ReasonFailed,
				fmt.Sprintf("exceeded %d consecutive failures: %s", constants.MaxConsecutiveProbeFailures, msg),
				route.Generation)
		} else {
			conditions.SetCondition(&route.Status.Conditions, conditions.TypeRouteReachable,
				metav1.ConditionFalse, conditions.ReasonUnreachable,
				msg, route.Generation)
		}
	}

	route.Status.ObservedGeneration = route.Generation

	if err := r.Status().Update(ctx, &route); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating route status: %w", err)
	}

	logger.Info("reconciled exposed route",
		"route", route.Name,
		"phase", route.Status.Phase,
		"probeResult", probeResult,
		"consecutiveFailures", route.Status.ConsecutiveFailures)

	return ctrl.Result{RequeueAfter: constants.RequeueProbeInterval}, nil
}

// checkCertificateReady checks if the TLS certificate secret exists.
// If TLS is disabled, returns true (no certificate needed).
func (r *ExposureReconciler) checkCertificateReady(ctx context.Context, route *loopzerov1.ExposedRoute) bool {
	if !route.Spec.TLS.Enabled {
		return true
	}

	secretName := naming.CertSecretName(route.Spec.Hostname)
	var secret corev1.Secret
	key := types.NamespacedName{
		Name:      secretName,
		Namespace: route.Namespace,
	}
	if err := r.Get(ctx, key, &secret); err != nil {
		return false
	}
	return secret.Type == corev1.SecretTypeTLS
}

// resolveProbeURL determines the URL to probe for reachability.
// In tests, the loopzero.io/probe-url-override annotation can override this.
func (r *ExposureReconciler) resolveProbeURL(route *loopzerov1.ExposedRoute) string {
	if override, ok := route.Annotations[probeURLOverrideAnnotation]; ok && override != "" {
		return override
	}
	if route.Spec.TLS.Enabled {
		return "https://" + route.Spec.Hostname
	}
	return "http://" + route.Spec.Hostname
}

// performProbe makes an HTTP GET to the given URL and returns the status code.
func (r *ExposureReconciler) performProbe(ctx context.Context, url string) (int32, error) {
	httpClient := r.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: constants.ProbeTimeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, //nolint:gosec // probes use self-signed certs in dev
				},
			},
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("creating probe request for %s: %w", url, err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("probing %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	return int32(resp.StatusCode), nil
}

// SetupExposureController registers the ExposureController with the manager.
func SetupExposureController(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopzerov1.ExposedRoute{}).
		Named("exposure").
		Complete(&ExposureReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
			HTTPClient: &http.Client{
				Timeout: constants.ProbeTimeout,
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: true, //nolint:gosec // probes use self-signed certs in dev
					},
				},
			},
		})
}
