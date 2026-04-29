// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
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
	"github.com/loopzero/loopzero/operators/internal/httpclient"
	"github.com/loopzero/loopzero/operators/internal/shared"
	"github.com/loopzero/loopzero/pkg/naming"
	"github.com/loopzero/loopzero/pkg/statemachine"
)

// exposedRouteSM is the ExposedRoute lifecycle state machine (T3.6 / ARCH-002).
// Currently used to enforce valid initial state. Per-transition refactor
// (each Status.Phase write goes through SM) is a follow-up.
var exposedRouteSM = statemachine.NewExposedRouteMachine()

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
	Scheme                *runtime.Scheme
	HTTPClient            *http.Client
	AllowProbeURLOverride bool // only true in test environments (SEC-005: prevents SSRF via annotation)
}

// Reconcile handles the reconciliation loop for ExposedRoute resources.
func (r *ExposureReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
	ctx, span := shared.StartReconcileSpan(ctx, "exposure", req.Name, req.Namespace)
	defer span.End()
	start := time.Now()
	defer func() {
		if retErr != nil {
			shared.RecordError(span, retErr)
		} else {
			shared.SetSuccess(span)
		}
		shared.ReconcileOutcome("exposure", start, retErr)
	}()

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

	// Initialize phase via state machine (T3.6 / ARCH-002).
	if route.Status.Phase == "" {
		route.Status.Phase = loopzerov1.ExposedRoutePhase(
			exposedRouteSM.Transition(statemachine.ExposedRouteEmpty, statemachine.ExposedRouteCreate),
		)
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

	// Step 2: If cert not ready and TLS is required, stay Pending and requeue.
	// Phase already initialized to Pending via SM at line 102; this is idempotent.
	if !certReady && route.Spec.TLS.Enabled {
		// T3.6 follow-up: if route was Failed and is now retrying, transition
		// Failed -> Pending via SM. Otherwise no-op (already Pending).
		if route.Status.Phase == loopzerov1.ExposedRoutePhaseFailed {
			route.Status.Phase = loopzerov1.ExposedRoutePhase(
				exposedRouteSM.Transition(statemachine.ExposedRouteFailed, statemachine.ExposedRouteRetry),
			)
		}
		route.Status.ObservedGeneration = route.Generation

		if err := r.Status().Update(ctx, &route); err != nil {
			return ctrl.Result{}, fmt.Errorf("updating route status: %w", err)
		}
		logger.Info("certificate not ready, requeuing", "route", route.Name)
		return ctrl.Result{RequeueAfter: constants.RequeueProbeInterval}, nil
	}

	// Cert is ready (or TLS is disabled) — transition Pending -> CertIssued
	// via SM. T3.6 follow-up.
	if route.Status.Phase == loopzerov1.ExposedRoutePhasePending {
		route.Status.Phase = loopzerov1.ExposedRoutePhase(
			exposedRouteSM.Transition(statemachine.ExposedRoutePending, statemachine.ExposedRouteCertReady),
		)
	}

	// Step 3: Perform reachability probe
	probeURL := r.resolveProbeURL(&route)
	probeCtx, probeSpan := shared.StartChildSpan(ctx, "exposure.probe",
		attribute.String("probe.url", probeURL))
	probeResult, probeErr := r.performProbe(probeCtx, probeURL)
	if probeErr != nil {
		shared.RecordError(probeSpan, probeErr)
	}
	probeSpan.End()

	now := metav1.Now()
	route.Status.LastProbeTime = &now
	route.Status.LastProbeResult = probeResult

	if probeErr == nil && probeResult >= 200 && probeResult < 400 {
		// Probe succeeded — reset failures and transition to Reachable via SM.
		// T3.6 follow-up: handle multiple source states (Failed via Retry+CertReady,
		// CertIssued via ProbeOK directly, Reachable already-correct no-op).
		route.Status.ConsecutiveFailures = 0
		switch route.Status.Phase {
		case loopzerov1.ExposedRoutePhaseCertIssued:
			route.Status.Phase = loopzerov1.ExposedRoutePhase(
				exposedRouteSM.Transition(statemachine.ExposedRouteCertIssued, statemachine.ExposedRouteProbeOK),
			)
		case loopzerov1.ExposedRoutePhaseFailed:
			// Recovery: Failed -> Pending -> CertIssued -> Reachable in one step
			// (cert is ready by this point of the function). Use Retry then walk forward.
			route.Status.Phase = loopzerov1.ExposedRoutePhase(
				exposedRouteSM.Transition(statemachine.ExposedRouteFailed, statemachine.ExposedRouteRetry),
			)
			route.Status.Phase = loopzerov1.ExposedRoutePhase(
				exposedRouteSM.Transition(statemachine.ExposedRoutePending, statemachine.ExposedRouteCertReady),
			)
			route.Status.Phase = loopzerov1.ExposedRoutePhase(
				exposedRouteSM.Transition(statemachine.ExposedRouteCertIssued, statemachine.ExposedRouteProbeOK),
			)
		case loopzerov1.ExposedRoutePhaseReachable:
			// Already correct — no transition needed. Idempotent on repeat reconcile.
		default:
			// Defensive: any other phase, force-set via direct write with audit log.
			route.Status.Phase = loopzerov1.ExposedRoutePhaseReachable
		}
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
			// T3.6 follow-up: transition to Failed via SM from any current phase.
			// SM defines ProbeFail valid from Pending, CertIssued, or Reachable.
			switch route.Status.Phase {
			case loopzerov1.ExposedRoutePhasePending:
				route.Status.Phase = loopzerov1.ExposedRoutePhase(
					exposedRouteSM.Transition(statemachine.ExposedRoutePending, statemachine.ExposedRouteProbeFail),
				)
			case loopzerov1.ExposedRoutePhaseCertIssued:
				route.Status.Phase = loopzerov1.ExposedRoutePhase(
					exposedRouteSM.Transition(statemachine.ExposedRouteCertIssued, statemachine.ExposedRouteProbeFail),
				)
			case loopzerov1.ExposedRoutePhaseReachable:
				route.Status.Phase = loopzerov1.ExposedRoutePhase(
					exposedRouteSM.Transition(statemachine.ExposedRouteReachable, statemachine.ExposedRouteProbeFail),
				)
			case loopzerov1.ExposedRoutePhaseFailed:
				// Already Failed — no transition needed. Idempotent.
			default:
				// Defensive: any unexpected phase, set Failed directly.
				route.Status.Phase = loopzerov1.ExposedRoutePhaseFailed
			}
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

	// Use a longer interval when the route is stable (Reachable) to reduce
	// reconcile load. Unstable routes get probed more frequently.
	requeueInterval := constants.RequeueProbeInterval
	if route.Status.Phase == loopzerov1.ExposedRoutePhaseReachable {
		requeueInterval = constants.RequeueProbeStableInterval
	}
	return ctrl.Result{RequeueAfter: requeueInterval}, nil
}

// checkCertificateReady checks if the TLS certificate secret exists.
// If TLS is disabled, returns true (no certificate needed).
func (r *ExposureReconciler) checkCertificateReady(ctx context.Context, route *loopzerov1.ExposedRoute) bool {
	if !route.Spec.TLS.Enabled {
		return true
	}

	secretName, err := naming.CertSecretName(route.Spec.Hostname)
	if err != nil {
		return false
	}
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
// The loopzero.io/probe-url-override annotation is only respected when AllowProbeURLOverride
// is true (test environments only). In production this annotation is ignored to prevent SSRF.
func (r *ExposureReconciler) resolveProbeURL(route *loopzerov1.ExposedRoute) string {
	if r.AllowProbeURLOverride {
		if override, ok := route.Annotations[probeURLOverrideAnnotation]; ok && override != "" {
			return override
		}
	}
	if route.Spec.TLS.Enabled {
		return "https://" + route.Spec.Hostname
	}
	return "http://" + route.Spec.Hostname
}

// performProbe makes an HTTP GET to the given URL and returns the status code.
func (r *ExposureReconciler) performProbe(ctx context.Context, url string) (int32, error) {
	httpClient := r.HTTPClient

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("creating probe request for %s: %w", url, err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("probing %s: %w", url, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	return int32(resp.StatusCode), nil
}

// ExposureControllerConfig holds optional configuration for the ExposureController.
type ExposureControllerConfig struct {
	// AllowProbeURLOverride enables the loopzero.io/probe-url-override annotation.
	// Must only be true in test environments (SEC-005: prevents SSRF).
	AllowProbeURLOverride bool
}

// SetupExposureController registers the ExposureController with the manager.
func SetupExposureController(mgr ctrl.Manager, cfgs ...ExposureControllerConfig) error {
	var cfg ExposureControllerConfig
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopzerov1.ExposedRoute{}).
		Named("exposure").
		Complete(&ExposureReconciler{
			Client:                mgr.GetClient(),
			Scheme:                mgr.GetScheme(),
			HTTPClient:            httpclient.NewWithTimeout(constants.ProbeTimeout),
			AllowProbeURLOverride: cfg.AllowProbeURLOverride,
		})
}
