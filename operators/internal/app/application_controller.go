// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package app implements the Application + Exposure controllers
// (operator binary `loopruntime-app-operator` per ADR-0023). Owns
// reconciliation of LoopRuntimeApplication + ExposedRoute CRs — the
// workload-plane resources that materialize a user's deploy onto
// the runtime cluster.
package app

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

	loopruntimev1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/cellrouting"
	"github.com/getloopscale/loop-runtime/operators/internal/conditions"
	"github.com/getloopscale/loop-runtime/operators/internal/constants"
	"github.com/getloopscale/loop-runtime/operators/internal/reconcileloop"
	"github.com/getloopscale/loop-runtime/pkg/correlation"
	"github.com/getloopscale/loop-runtime/pkg/naming"
)

const applicationFinalizer = constants.FinalizerApplication

// workloadReadyTimeout bounds cross-cluster pod-list calls during the
// fast-path Healthy check (T4.2). 5s matches the convention used by other
// cell-aware reads (e.g., placement.DynamicRouter) — long enough for normal
// rest API roundtrips, short enough to avoid blocking the reconcile loop.
const workloadReadyTimeout = 5 * time.Second

// ApplicationReconciler reconciles a LoopRuntimeApplication object.
// It manages hostname assignment, ExposedRoute creation, and phase tracking.
//
// ADR-0026 D4 / plan T4.1: kubebuilder:rbac markers.
//
// +kubebuilder:rbac:groups=runtime.getloopscale.com,resources=loopruntimeapplications,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=runtime.getloopscale.com,resources=loopruntimeapplications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=runtime.getloopscale.com,resources=loopruntimeapplications/finalizers,verbs=update
// +kubebuilder:rbac:groups=runtime.getloopscale.com,resources=exposedroutes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=runtime.getloopscale.com,resources=tenants,verbs=get;list;watch
// +kubebuilder:rbac:groups=runtime.getloopscale.com,resources=clustercredentials,verbs=get;list;watch
// +kubebuilder:rbac:groups="",namespace=loopruntime-system,resources=secrets,verbs=get;list;watch
type ApplicationReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	BaseDomain string

	// RuntimeFactory resolves a cell-0-cluster client per tenant cell_id
	// (dogfood-2026-05-08-fixes T4.2 / ADR D6). nil = single-cell legacy mode:
	// fast-path Healthy is disabled and Application.phase only converges via
	// ExposedRoute Reachable. Wiring is in app-operator/main.go.
	RuntimeFactory cellrouting.RuntimeClientFactory
}

// applicationDetailTypes per ADR-0025 D3.
var applicationDetailTypes = []string{
	conditions.TypeServiceResolved,
	conditions.TypeIngressReady,
	conditions.TypeCertificateIssued,
	conditions.TypeRouteReachable,
	conditions.TypeApplicationHealthy,
}

// Reconcile handles the reconciliation loop for LoopRuntimeApplication resources.
func (r *ApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
	ctx, span := reconcileloop.StartReconcileSpan(ctx, "application", req.Name, req.Namespace)
	defer span.End()
	start := time.Now()
	defer func() {
		if retErr != nil {
			reconcileloop.RecordError(span, retErr)
		} else {
			reconcileloop.SetSuccess(span)
		}
		reconcileloop.ReconcileOutcome("application", start, retErr)
	}()

	logger := log.FromContext(ctx)

	var app loopruntimev1.LoopRuntimeApplication
	if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Audit gap M08 #10 / §6.C.1b: when the CR carries the
	// `runtime.getloopscale.com/correlation-id` annotation (set by the API during
	// `EnsureLoopRuntimeApplication` for HTTP-driven flows), inject it into the
	// reconcile context so downstream audit_log writes + structured logs
	// trace back to the originating HTTP request. Background reconciles
	// (re-queue, watch event from non-API source) leave the annotation
	// empty and the ctx unchanged — `correlation.WithID` is a no-op on
	// empty string.
	ctx = correlation.WithID(ctx, app.GetAnnotations()["runtime.getloopscale.com/correlation-id"])

	// T5.1 of deep-review-2026-05-10-remediation-plan
	// (find_arch_app_controller_double_write MEDIUM): the deferred
	// Status().Update was an "always-overwrite at end of reconcile" that
	// raced with the explicit `Status().Patch` at line ~153 — both wrote
	// to .status.conditions on the same generation, producing flapping
	// in the audit log + occasional 409 conflicts.
	//
	// kstatus universal per ADR-0025 D3 condition #1 is now folded into
	// computedStatus BEFORE the explicit Patch (single write per pass).

	if !app.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &app)
	}

	if err := reconcileloop.EnsureFinalizer(ctx, r.Client, &app, applicationFinalizer, req.NamespacedName); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileHostname(ctx, req, &app); err != nil {
		return ctrl.Result{}, err
	}

	// The base snapshot for the status patch below.
	//
	// HERE, and not after a re-fetch. The previous shape composed the whole
	// status, re-fetched, and took MergeFrom off the FRESH object while carrying
	// the composite from before — so the diff reverted whatever another writer
	// had changed in between. That is the damage the contract names, wearing a
	// Patch instead of an Update (B-105).
	//
	// Safe here because every metadata/spec write ahead of this point re-reads
	// the object itself: EnsureFinalizer (reconcileloop/finalizer.go) and
	// reconcileHostname (its `return r.Get(...)`). Measured, not assumed — that
	// is what made the third re-fetch removable.
	//
	// Decision: MEET-ASYNC-AMENDMENT-2026-08-14-application-status-anchor
	base := app.DeepCopy()

	app.Status.URL = "https://" + app.Spec.Hostname

	conditions.SetCondition(&app.Status.Conditions, conditions.TypeServiceResolved,
		metav1.ConditionTrue, conditions.ReasonReady,
		fmt.Sprintf("service %s resolved", app.Spec.ServiceName),
		app.Generation)

	if err := r.reconcileExposedRoute(ctx, &app, base); err != nil {
		return ctrl.Result{}, err
	}

	// dogfood-2026-05-08-fixes T4.2 (F-6, ADR D6): fast-path Healthy.
	// Mark EndpointsReady=true the moment the workload Pod is Ready in
	// the tenant's cell, independent of ExposedRoute Reachable. Local k3d
	// has no public DNS for *.loopruntime.local so ExposedRoute may stay Pending
	// forever; production may take 60+s for DNS+cert+Traefik propagation.
	// EndpointsReady=true unblocks phase=Serving.
	r.reconcileWorkloadStatus(ctx, &app)
	r.reconcileRouteStatus(ctx, &app)
	phase, err := advancePhase(&app)
	if err != nil {
		return ctrl.Result{}, err
	}
	app.Status.Phase = phase
	r.reconcileHealthCondition(&app)

	if err := r.patchStatus(ctx, &app, base); err != nil {
		return ctrl.Result{}, fmt.Errorf("patching application status: %w", err)
	}

	logger.Info("reconciled application",
		"application", app.Name,
		"phase", app.Status.Phase,
		"hostname", app.Spec.Hostname,
		"url", app.Status.URL)

	return ctrl.Result{}, nil
}

// patchStatus is the ONLY path in this reconcile that persists .status. It folds
// the kstatus readiness summary in as the last mutation before the write, per
// ADR-0025 D3.
//
// The ordering used to live at each call site, and that is how it was lost: the
// happy path folded the summary in, the ExposedRoute failure branch patched
// without it, and a failed pass persisted a fresh IngressReady=False next to a
// Ready condition left over from the previous generation — stale, and read as
// true precisely when something had just gone wrong.
//
// Routing every persist through here makes the invariant structural instead of
// remembered: a new early-return path cannot reintroduce the defect, because
// there is no way to persist status without summarizing it first.
//
// Every operator controller now persists status this way. The deferred
// re-fetching summary the others used to carry was a SECOND writer by design;
// this controller removed its own (T5.1, see the note in Reconcile) for
// flapping conditions and 409s, and the rest followed.
//
// Decision: MEET-ASYNC-AMENDMENT-2026-08-14-application-status-persist-encapsulation
func (r *ApplicationReconciler) patchStatus(ctx context.Context, app, base *loopruntimev1.LoopRuntimeApplication) error {
	// observedGeneration belongs to the write, not to the happy path. It used to be
	// set only just before the final patch, so the failure branch persisted a Ready
	// condition stamped with the CURRENT generation next to a
	// .status.observedGeneration from the previous one — a status contradicting
	// itself inside a single write, and kstatus reading the whole subresource as
	// stale. The sibling Environment controller already sets it inside its error
	// branches (environment_controller.go); this makes the two agree.
	//
	// It is honest on a failed pass: "I have seen generation N and it is not ready"
	// is exactly what a false Ready at that generation says.
	app.Status.ObservedGeneration = app.Generation
	conditions.SetReadinessSummaryViaPkg(&app.Status.Conditions, app.Generation, applicationDetailTypes)
	return r.Status().Patch(ctx, app, client.MergeFrom(base))
}

// reconcileHostname persists the routed hostname when spec.hostname is unset or
// still holds the controller's old default (see hostnameToAssign).
func (r *ApplicationReconciler) reconcileHostname(ctx context.Context, req ctrl.Request, app *loopruntimev1.LoopRuntimeApplication) error {
	hostname, assign, err := hostnameToAssign(app, r.BaseDomain)
	if err != nil {
		return fmt.Errorf("computing hostname: %w", err)
	}
	if !assign {
		return nil
	}
	app.Spec.Hostname = hostname
	if err := r.Update(ctx, app); err != nil {
		return fmt.Errorf("setting hostname: %w", err)
	}
	return r.Get(ctx, req.NamespacedName, app)
}

// reconcileExposedRoute ensures the ExposedRoute CR exists with correct spec.
func (r *ApplicationReconciler) reconcileExposedRoute(ctx context.Context, app, base *loopruntimev1.LoopRuntimeApplication) error {
	ctx, span := reconcileloop.StartChildSpan(ctx, "application.exposed_route.ensure")
	defer span.End()

	logger := log.FromContext(ctx)
	routeName := constants.ExposedRouteName(app.Name)

	route := &loopruntimev1.ExposedRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: app.Namespace,
		},
	}
	result, err := controllerutil.CreateOrUpdate(ctx, r.Client, route, func() error {
		route.Spec.ApplicationRef = app.Name
		route.Spec.Hostname = app.Spec.Hostname
		route.Spec.ServiceRef = loopruntimev1.ServiceRef{
			Name: app.Spec.ServiceName,
			Port: app.Spec.ServicePort,
		}
		route.Spec.TLS = loopruntimev1.TLSConfig{Enabled: true}
		// Audit gap M10 F2 / §6.E.4b: propagate tenant scope from parent
		// LoopRuntimeApplication into child labels. Without this, Loki/Prometheus
		// queries that filter by `runtime.getloopscale.com/tenant` on ExposedRoute resources
		// silently return empty results, OPA webhooks scoped per tenant
		// have no signal to gate on, and operator self-audit traces cannot
		// correlate child events back to the originating tenant. Re-applied
		// on every reconcile via CreateOrUpdate so a manual edit that
		// strips the labels converges back to the parent's tenant scope.
		if route.Labels == nil {
			route.Labels = map[string]string{}
		}
		route.Labels[constants.LabelTenant] = app.Spec.TenantRef
		route.Labels[constants.LabelProject] = app.Spec.Project
		route.Labels[constants.LabelEnvironment] = app.Spec.Environment
		return controllerutil.SetControllerReference(app, route, r.Scheme)
	})
	if err != nil {
		conditions.SetCondition(&app.Status.Conditions, conditions.TypeIngressReady,
			metav1.ConditionFalse, conditions.ReasonFailed,
			fmt.Sprintf("failed to ensure ExposedRoute: %v", err),
			app.Generation)
		if statusErr := r.patchStatus(ctx, app, base); statusErr != nil {
			logger.Error(statusErr, "failed to patch application status")
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
func (r *ApplicationReconciler) reconcileRouteStatus(ctx context.Context, app *loopruntimev1.LoopRuntimeApplication) {
	routeName := constants.ExposedRouteName(app.Name)
	route := &loopruntimev1.ExposedRoute{}
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

	app.Status.Reachable = route.Status.Phase == loopruntimev1.ExposedRoutePhaseReachable

	setReadyOrWaitingCondition(&app.Status.Conditions, conditions.TypeCertificateIssued,
		route.Status.CertificateReady, app.Generation,
		"certificate ready", "waiting for certificate")

	setReadyOrWaitingCondition(&app.Status.Conditions, conditions.TypeRouteReachable,
		app.Status.Reachable, app.Generation,
		"route is reachable", "waiting for route to become reachable")
}

// setReadyOrWaitingCondition is the canonical if-bool-then-True-else-False
// status-condition shape used when a downstream resource readiness flag
// dictates a True/False outcome with stock ReasonReady/ReasonWaiting and a
// pair of opposite messages. Phase 3 helper: collapses 2 inline 9-line
// if/else copies in reconcileExposure (CertificateIssued + RouteReachable).
func setReadyOrWaitingCondition(conds *[]metav1.Condition, condType string, ready bool, gen int64, readyMsg, waitingMsg string) {
	if ready {
		conditions.SetCondition(conds, condType, metav1.ConditionTrue, conditions.ReasonReady, readyMsg, gen)
	} else {
		conditions.SetCondition(conds, condType, metav1.ConditionFalse, conditions.ReasonWaiting, waitingMsg, gen)
	}
}

// reconcileHealthCondition sets the ApplicationHealthy condition based on phase.
func (r *ApplicationReconciler) reconcileHealthCondition(app *loopruntimev1.LoopRuntimeApplication) {
	if app.Status.Phase == loopruntimev1.ApplicationPhaseReachable || app.Status.Phase == loopruntimev1.ApplicationPhaseServing {
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
func (r *ApplicationReconciler) reconcileDelete(ctx context.Context, app *loopruntimev1.LoopRuntimeApplication) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(app, applicationFinalizer) {
		return ctrl.Result{}, nil
	}

	logger.Info("application is being deleted", "application", app.Name)

	var routeList loopruntimev1.ExposedRouteList
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

// reconcileWorkloadStatus sets app.Status.EndpointsReady=true if the workload
// Pod is Ready in the tenant's cell. Cell resolution: read Tenant CR by
// app.Spec.TenantRef → tenant.Spec.CellID → RuntimeFactory.For(cellID).
//
// dogfood-2026-05-08-fixes T4.2 (F-6, ADR D6 + EC-5 multi-cell awareness +
// EC-9 graceful degrade on cell-0-cluster unreachable).
//
// Behavior:
//   - Factory without runtime access (app-operator started without
//     CELL_0_KUBECONFIG): For returns ErrRuntimeClusterUnavailable → log + return.
//   - Tenant CR not found: log + return (route-status path will set
//     EndpointsReady eventually).
//   - Workload client error (timeout, RBAC, cluster down): log + return.
//     RequeueAfter is implicit (controller-runtime resync cycle).
//   - Pod list returns 0 matches: leave EndpointsReady at current value.
//   - Pod list returns ≥1 with PodReady=True: set EndpointsReady=true.
func (r *ApplicationReconciler) reconcileWorkloadStatus(ctx context.Context, app *loopruntimev1.LoopRuntimeApplication) {
	logger := log.FromContext(ctx)

	// Resolve tenant cellID via Tenant CR (mgmt cluster).
	var tenant loopruntimev1.Tenant
	tenantKey := client.ObjectKey{Name: app.Spec.TenantRef, Namespace: app.Namespace}
	if err := r.Get(ctx, tenantKey, &tenant); err != nil {
		// NotFound is normal during early reconcile or post-tenant-delete.
		// Other errors are transient — controller resync will retry.
		if !errors.IsNotFound(err) {
			logger.V(1).Info("workload-status: failed to read Tenant CR",
				"tenant", app.Spec.TenantRef, "error", err)
		}
		return
	}

	cellCtx, cancel := context.WithTimeout(ctx, workloadReadyTimeout)
	defer cancel()
	workloadClient, err := r.RuntimeFactory.For(cellCtx, tenant.Spec.CellID)
	if err != nil {
		logger.V(1).Info("workload-status: factory.For failed (graceful degrade)",
			"cell_id", tenant.Spec.CellID, "error", err)
		return
	}

	// Compute the workload namespace from the (tenant, project, env) triple — the real
	// isolation boundary. The previous helper dropped the project and produced
	// loopruntime-{tenant}-{env}, a namespace that exists in no cluster: List returned empty
	// without erroring, so readyReplicas was never counted and every LoopRuntimeApplication sat
	// in Progressing forever (measured: 26 days on app-dev). Issue #274.
	ns, err := naming.Namespace(tenant.Name, app.Spec.Project, app.Spec.Environment)
	if err != nil {
		logger.V(1).Info("workload-status: namespace compute failed",
			"tenant", tenant.Name, "env", app.Spec.Environment, "error", err)
		return
	}

	// List Pods using the renderer's canonical labels:
	//   runtime.getloopscale.com/project       = app.Spec.Project
	//   runtime.getloopscale.com/app-name      = app.Spec.ServiceName
	//   runtime.getloopscale.com/environment   = app.Spec.Environment
	//
	// (runtime.getloopscale.com/tenant is implicit via namespace filtering; namespace itself
	// is loopruntime-{tenantRef}-{project}-{environment}.)
	//
	// Source of truth for these labels: api/internal/manifests/renderer_*.go.
	var pods corev1.PodList
	if err := workloadClient.List(cellCtx, &pods,
		client.InNamespace(ns),
		client.MatchingLabels{
			constants.LabelProject:              app.Spec.Project,
			"runtime.getloopscale.com/app-name": app.Spec.ServiceName,
			constants.LabelEnvironment:          app.Spec.Environment,
		},
	); err != nil {
		logger.V(1).Info("workload-status: pod list failed (graceful degrade)",
			"namespace", ns, "cell_id", tenant.Spec.CellID, "error", err)
		return
	}

	for i := range pods.Items {
		for _, c := range pods.Items[i].Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				app.Status.EndpointsReady = true
				return
			}
		}
	}
}

// SetupApplicationControllerWithFactory registers the ApplicationController
// (dogfood-2026-05-08-fixes T4.2). factory is required: with runtime access it
// enables the workload-Pod-Ready fast-path so app.Status.Phase converges to
// Serving as soon as the Pod is Ready; a deployment without runtime access
// passes cellrouting.NewUnavailableRuntimeClientFactory.
func SetupApplicationControllerWithFactory(mgr ctrl.Manager, baseDomain string, factory cellrouting.RuntimeClientFactory) error {
	if factory == nil {
		return fmt.Errorf("application controller: a runtime client factory is required")
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&loopruntimev1.LoopRuntimeApplication{}, reconcileloop.ForOption()).
		Owns(&loopruntimev1.ExposedRoute{}).
		Named("application").
		Complete(&ApplicationReconciler{
			Client:         mgr.GetClient(),
			Scheme:         mgr.GetScheme(),
			BaseDomain:     baseDomain,
			RuntimeFactory: factory,
		})
}
