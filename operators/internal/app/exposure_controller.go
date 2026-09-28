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
	// correlation-id from CR annotation into reconcile context (no-op when absent).
	ctx = correlation.WithID(ctx, route.GetAnnotations()["loopzero.io/correlation-id"])

	if !route.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.reconcileDelete(ctx, &route)
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
	// And before any status mutation below: a snapshot taken after one would
	// diff the phase against itself and the patch would silently omit the very
	// transition it exists to persist.
	//
	// The deletion path returns above and writes no status, so it needs no base
	// of its own.
	base := route.DeepCopy()

	// Step 1: the certificate. Without it a TLS route cannot be probed, so it
	// waits (Pending) and is looked at again.
	obs := routeObservation{certReady: r.recordCertificate(ctx, &route)}
	if !obs.certReady && route.Spec.TLS.Enabled {
		if err := r.advanceAndPatch(ctx, &route, base, obs); err != nil {
			return ctrl.Result{}, err
		}
		logger.Info("certificate not ready, requeuing", "route", route.Name)
		return ctrl.Result{RequeueAfter: constants.RequeueProbeInterval}, nil
	}

	// Step 2: the reachability probe.
	probeResult, probeErr := r.probeRoute(ctx, &route)
	obs.probeOK, obs.probesExhausted = r.recordProbe(&route, probeResult, probeErr)

	// Step 3: the phase follows from what was observed, through the machine.
	if err := r.advanceAndPatch(ctx, &route, base, obs); err != nil {
		return ctrl.Result{}, err
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

// reconcileDelete removes the finalizer immediately: an ExposedRoute owns no
// external resources (namespace deletion removes the IngressRoute and
// Certificate).
func (r *ExposureReconciler) reconcileDelete(ctx context.Context, route *loopzerov1.ExposedRoute) error {
	if !controllerutil.ContainsFinalizer(route, exposureFinalizer) {
		return nil
	}
	controllerutil.RemoveFinalizer(route, exposureFinalizer)
	return r.Update(ctx, route)
}

// recordCertificate checks the TLS certificate and records the outcome on the
// route's status and CertificateIssued condition.
func (r *ExposureReconciler) recordCertificate(ctx context.Context, route *loopzerov1.ExposedRoute) bool {
	ready := r.checkCertificateReady(ctx, route)
	route.Status.CertificateReady = ready
	if ready {
		conditions.SetCondition(&route.Status.Conditions, conditions.TypeCertificateIssued,
			metav1.ConditionTrue, conditions.ReasonReady, "certificate is ready", route.Generation)
	} else {
		conditions.SetCondition(&route.Status.Conditions, conditions.TypeCertificateIssued,
			metav1.ConditionFalse, conditions.ReasonWaiting, "waiting for TLS certificate", route.Generation)
	}
	return ready
}

// probeRoute performs the reachability probe inside its own span.
func (r *ExposureReconciler) probeRoute(ctx context.Context, route *loopzerov1.ExposedRoute) (int32, error) {
	probeURL := r.resolveProbeURL(route)
	probeCtx, probeSpan := reconcileloop.StartChildSpan(ctx, "exposure.probe",
		attribute.String("probe.url", probeURL))
	defer probeSpan.End()
	result, err := r.performProbe(probeCtx, probeURL)
	if err != nil {
		reconcileloop.RecordError(probeSpan, err)
	}
	return result, err
}

// recordProbe records a probe outcome on the route's status and RouteReachable
// condition, and reports whether it succeeded and whether the consecutive
// failure budget is exhausted.
func (r *ExposureReconciler) recordProbe(route *loopzerov1.ExposedRoute, result int32, probeErr error) (ok, exhausted bool) {
	now := metav1.Now()
	route.Status.LastProbeTime = &now
	route.Status.LastProbeResult = result

	if probeErr == nil && result >= 200 && result < 400 {
		route.Status.ConsecutiveFailures = 0
		conditions.SetCondition(&route.Status.Conditions, conditions.TypeRouteReachable,
			metav1.ConditionTrue, conditions.ReasonReady,
			fmt.Sprintf("probe returned %d", result), route.Generation)
		return true, false
	}

	route.Status.ConsecutiveFailures++
	msg := fmt.Sprintf("probe failed: status %d", result)
	if probeErr != nil {
		msg = fmt.Sprintf("probe failed: %v", probeErr)
	}
	if route.Status.ConsecutiveFailures >= constants.MaxConsecutiveProbeFailures {
		conditions.SetCondition(&route.Status.Conditions, conditions.TypeRouteReachable,
			metav1.ConditionFalse, conditions.ReasonFailed,
			fmt.Sprintf("exceeded %d consecutive failures: %s", constants.MaxConsecutiveProbeFailures, msg),
			route.Generation)
		return false, true
	}
	conditions.SetCondition(&route.Status.Conditions, conditions.TypeRouteReachable,
		metav1.ConditionFalse, conditions.ReasonUnreachable, msg, route.Generation)
	return false, false
}

// advanceAndPatch moves the route's phase through the ExposedRoute machine,
// folds the readiness summary in as the last mutation (ADR-0025 D3) and
// persists the status with a merge patch off base.
func (r *ExposureReconciler) advanceAndPatch(ctx context.Context, route, base *loopzerov1.ExposedRoute, obs routeObservation) error {
	phase, err := advanceRoutePhase(route.Status.Phase, obs)
	if err != nil {
		return err
	}
	route.Status.Phase = phase
	route.Status.ObservedGeneration = route.Generation
	conditions.SetReadinessSummaryViaPkg(&route.Status.Conditions, route.Generation, exposureDetailTypes)
	if err := r.Status().Patch(ctx, route, client.MergeFrom(base)); err != nil {
		return fmt.Errorf("updating route status: %w", err)
	}
	return nil
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
