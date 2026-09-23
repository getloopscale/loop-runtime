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

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/cellrouting"
	"github.com/getloopscale/loopzero/operators/internal/conditions"
	"github.com/getloopscale/loopzero/operators/internal/constants"
	"github.com/getloopscale/loopzero/operators/internal/httpclient"
	"github.com/getloopscale/loopzero/operators/internal/reconcileloop"
	"github.com/getloopscale/loopzero/pkg/correlation"
	"github.com/getloopscale/loopzero/pkg/naming"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// exposedRouteSM is the ExposedRoute lifecycle state machine (T3.6 / ARCH-002).
// Currently used to enforce valid initial state. Per-transition refactor
// (each Status.Phase write goes through SM) is a follow-up.
//
// Phase 1 POC of ADR `2026-05-15-T6-1-typed-state-machines.md`: migrated
// to the typed wrapper so `exposedRouteSM.Transition(...)` accepts ONLY
// `ExposedRouteState`/`ExposedRouteEvent` constants. A typo like
// `BuildQueued` would be a compile error, instead of a runtime panic on
// the underlying Machine. Wire format (strings persisted to CR status)
// is byte-for-byte identical to the untyped path — see
// `TestExposedRouteTypedMachine_WireFormatCompatibility`.
var exposedRouteSM = statemachine.NewExposedRouteTypedMachine()

const (
	// exposureFinalizer is the finalizer name for ExposedRoute cleanup.
	exposureFinalizer = constants.FinalizerExposedRoute

	// probeURLOverrideAnnotation allows tests to inject a probe URL.
	probeURLOverrideAnnotation = constants.AnnotationProbeURLOverride
)

// ExposureReconciler reconciles an ExposedRoute object.
// It manages certificate status checks and reachability probes.
//
// ADR-0026 D4 / plan T4.1: kubebuilder:rbac markers.
//
// +kubebuilder:rbac:groups=loopzero.io,resources=exposedroutes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=loopzero.io,resources=exposedroutes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=loopzero.io,resources=exposedroutes/finalizers,verbs=update
// +kubebuilder:rbac:groups=traefik.io,resources=ingressroutes;middlewares,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete
type ExposureReconciler struct {
	client.Client
	Scheme                *runtime.Scheme
	HTTPClient            *http.Client
	AllowProbeURLOverride bool // only true in test environments (SEC-005: prevents SSRF via annotation)

	// RuntimeFactory resolves a client for the cell where the workload runs. The TLS
	// secret lives there, next to the app — not in this cluster. Nil means the check
	// degrades to "not ready", which is the honest answer when we cannot look (#277).
	RuntimeFactory cellrouting.RuntimeClientFactory
}

// exposureDetailTypes per ADR-0025 D3.
var exposureDetailTypes = []string{
	conditions.TypeIngressReady,
	conditions.TypeCertificateIssued,
	conditions.TypeRouteReachable,
}

// Reconcile handles the reconciliation loop for ExposedRoute resources.
func (r *ExposureReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
	ctx, span := reconcileloop.StartReconcileSpan(ctx, "exposure", req.Name, req.Namespace)
	defer span.End()
	start := time.Now()
	defer func() {
		if retErr != nil {
			reconcileloop.RecordError(span, retErr)
		} else {
			reconcileloop.SetSuccess(span)
		}
		reconcileloop.ReconcileOutcome("exposure", start, retErr)
	}()

	logger := log.FromContext(ctx)

	var route loopzerov1.ExposedRoute
	if err := r.Get(ctx, req.NamespacedName, &route); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// T1.1 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap: propagate
	// correlation-id from CR annotation into reconcile context (no-op se ausente).
	ctx = correlation.WithID(ctx, route.GetAnnotations()["loopzero.io/correlation-id"])

	// kstatus universal per ADR-0025 D3 condition #1.
	//
	// The shared helper, not the hand-rolled copy this file used to carry. That
	// copy predated slice-10 and reproduced the two defects that slice fixed:
	// it persisted with Status().Update, so a conditions write — an ARRAY, which
	// RFC 7386 treats as a scalar — could erase a concurrent write of the whole
	// array with no error anywhere; and it DISCARDED the error (`_ =`), making a
	// dropped status write silent in the one place that has nowhere to return to.
	//
	// DeferReadinessSummary carries MergeFromWithOptimisticLock and logs the
	// failure. Duplicated knowledge is why this file still had the pre-fix shape
	// months after the fix (B-085).
	defer reconcileloop.DeferReadinessSummary(ctx, r.Client, req.NamespacedName,
		func() *loopzerov1.ExposedRoute { return &loopzerov1.ExposedRoute{} },
		func(x *loopzerov1.ExposedRoute) *[]metav1.Condition { return &x.Status.Conditions },
		exposureDetailTypes)()

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
	if err := reconcileloop.EnsureFinalizer(ctx, r.Client, &route, exposureFinalizer, req.NamespacedName); err != nil {
		return ctrl.Result{}, err
	}

	// The base snapshot for every status write below.
	//
	// AFTER EnsureFinalizer above, which writes metadata and bumps
	// resourceVersion: a patch derived from an earlier snapshot carries the stale
	// version and becomes an optimistic lock frozen at snapshot time — a WIDER
	// conflict window than the refetch this contract removes. Measured as a real
	// 409 in slice-18.
	//
	// And before the phase initialisation below, which mutates .Status: a
	// snapshot taken after it would diff the phase against itself and the patch
	// would silently omit the very transition it exists to persist.
	//
	// The deletion path returns above and writes no status, so it needs no base
	// of its own.
	base := route.DeepCopy()

	// Initialize phase via state machine (T3.6 / ARCH-002).
	if route.Status.Phase == "" {
		route.Status.Phase = loopzerov1.ExposedRoutePhase(
			exposedRouteSM.Transition(statemachine.XRStateEmpty, statemachine.XREventCreate),
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
				exposedRouteSM.Transition(statemachine.XRStateFailed, statemachine.XREventRetry),
			)
		}
		route.Status.ObservedGeneration = route.Generation

		if err := r.Status().Patch(ctx, &route, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, fmt.Errorf("updating route status: %w", err)
		}
		logger.Info("certificate not ready, requeuing", "route", route.Name)
		return ctrl.Result{RequeueAfter: constants.RequeueProbeInterval}, nil
	}

	// Cert is ready (or TLS is disabled) — transition Pending -> CertIssued
	// via SM. T3.6 follow-up.
	if route.Status.Phase == loopzerov1.ExposedRoutePhasePending {
		route.Status.Phase = loopzerov1.ExposedRoutePhase(
			exposedRouteSM.Transition(statemachine.XRStatePending, statemachine.XREventCertReady),
		)
	}

	// Step 3: Perform reachability probe
	probeURL := r.resolveProbeURL(&route)
	probeCtx, probeSpan := reconcileloop.StartChildSpan(ctx, "exposure.probe",
		attribute.String("probe.url", probeURL))
	probeResult, probeErr := r.performProbe(probeCtx, probeURL)
	if probeErr != nil {
		reconcileloop.RecordError(probeSpan, probeErr)
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
				exposedRouteSM.Transition(statemachine.XRStateCertIssued, statemachine.XREventProbeOK),
			)
		case loopzerov1.ExposedRoutePhaseFailed:
			// Recovery: Failed -> Pending -> CertIssued -> Reachable in one step
			// (cert is ready by this point of the function). Use Retry then walk forward.
			route.Status.Phase = loopzerov1.ExposedRoutePhase(
				exposedRouteSM.Transition(statemachine.XRStateFailed, statemachine.XREventRetry),
			)
			route.Status.Phase = loopzerov1.ExposedRoutePhase(
				exposedRouteSM.Transition(statemachine.XRStatePending, statemachine.XREventCertReady),
			)
			route.Status.Phase = loopzerov1.ExposedRoutePhase(
				exposedRouteSM.Transition(statemachine.XRStateCertIssued, statemachine.XREventProbeOK),
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
					exposedRouteSM.Transition(statemachine.XRStatePending, statemachine.XREventProbeFail),
				)
			case loopzerov1.ExposedRoutePhaseCertIssued:
				route.Status.Phase = loopzerov1.ExposedRoutePhase(
					exposedRouteSM.Transition(statemachine.XRStateCertIssued, statemachine.XREventProbeFail),
				)
			case loopzerov1.ExposedRoutePhaseReachable:
				route.Status.Phase = loopzerov1.ExposedRoutePhase(
					exposedRouteSM.Transition(statemachine.XRStateReachable, statemachine.XREventProbeFail),
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

	if err := r.Status().Patch(ctx, &route, client.MergeFrom(base)); err != nil {
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

// checkCertificateReady reports whether the TLS certificate secret exists in the cell
// where the workload runs. If TLS is disabled, returns true (no certificate needed).
//
// # WHERE IT LOOKS, AND WHY THAT WAS WRONG
//
// It used to call r.Get on route.Namespace with a name derived from the hostname. Both
// were wrong: the ExposedRoute lives in loopzero-system on the MGMT cluster, while the Secret
// is created by the manifest renderer in the tenant namespace on the WORKLOAD cluster,
// under the name tls-{project}-{app}. A Get against a Secret that cannot exist returns
// NotFound, the function returned false, and the route logged "certificate not ready,
// requeuing" every 30s — for 27 days in app-dev, with a valid certificate sitting beside
// the pods (issue #277).
//
// The lookup now walks the same chain reconcileWorkloadStatus already walks:
// ExposedRoute -> LoopZeroApplication -> Tenant -> cellID -> runtime client -> tenant namespace.
//
// Every failure along that chain logs. The previous silence is what let a wrong lookup
// look like a pending certificate for four weeks.
func (r *ExposureReconciler) checkCertificateReady(ctx context.Context, route *loopzerov1.ExposedRoute) bool {
	if !route.Spec.TLS.Enabled {
		return true
	}
	logger := log.FromContext(ctx)

	var app loopzerov1.LoopZeroApplication
	appKey := types.NamespacedName{Name: route.Spec.ApplicationRef, Namespace: route.Namespace}
	if err := r.Get(ctx, appKey, &app); err != nil {
		logger.Info("cert check: LoopZeroApplication not readable",
			"route", route.Name, "application", route.Spec.ApplicationRef, "error", err)
		return false
	}

	var tenant loopzerov1.Tenant
	if err := r.Get(ctx, types.NamespacedName{Name: app.Spec.TenantRef, Namespace: route.Namespace}, &tenant); err != nil {
		logger.Info("cert check: Tenant not readable",
			"route", route.Name, "tenant", app.Spec.TenantRef, "error", err)
		return false
	}

	workloadClient, err := r.RuntimeFactory.For(ctx, tenant.Spec.CellID)
	if err != nil {
		logger.Info("cert check: runtime client unavailable",
			"route", route.Name, "cell_id", tenant.Spec.CellID, "error", err)
		return false
	}

	ns, err := naming.Namespace(tenant.Name, app.Spec.Project, app.Spec.Environment)
	if err != nil {
		logger.Info("cert check: namespace compute failed",
			"route", route.Name, "project", app.Spec.Project, "error", err)
		return false
	}
	secretName, err := naming.AppCertSecretName(app.Spec.Project, app.Spec.ServiceName)
	if err != nil {
		logger.Info("cert check: secret name compute failed",
			"route", route.Name, "project", app.Spec.Project, "error", err)
		return false
	}

	var secret corev1.Secret
	if err := workloadClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: ns}, &secret); err != nil {
		logger.Info("cert check: TLS secret not found in the workload cell",
			"route", route.Name, "secret", secretName, "namespace", ns, "error", err)
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

	// RuntimeFactory resolves the client for the cell where the workload — and its
	// TLS secret — actually lives. Required. A deployment without runtime access
	// passes cellrouting.NewUnavailableRuntimeClientFactory, and the cert check
	// then reports not-ready, which is the honest answer (#277).
	RuntimeFactory cellrouting.RuntimeClientFactory
}

// SetupExposureController registers the ExposureController with the manager.
func SetupExposureController(mgr ctrl.Manager, cfg ExposureControllerConfig) error {
	if cfg.RuntimeFactory == nil {
		return fmt.Errorf("exposure controller: a runtime client factory is required")
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopzerov1.ExposedRoute{}, reconcileloop.ForOption()).
		Named("exposure").
		Complete(&ExposureReconciler{
			Client:                mgr.GetClient(),
			Scheme:                mgr.GetScheme(),
			HTTPClient:            httpclient.NewWithTimeout(constants.ProbeTimeout),
			AllowProbeURLOverride: cfg.AllowProbeURLOverride,
			RuntimeFactory:        cfg.RuntimeFactory,
		})
}
