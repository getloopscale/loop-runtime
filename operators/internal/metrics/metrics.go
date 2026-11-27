// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package metrics provides custom Prometheus metrics for operator controllers.
// All metrics follow the loop_runtime_operator_ prefix convention for Grafana dashboards.
//
// Registration target: controller-runtime's metrics.Registry (NOT the default
// prometheus registry). The operator's metricsserver.Options serves only from
// the controller-runtime registry; metrics registered in
// prometheus.DefaultRegisterer would silently never appear in the /metrics
// endpoint. Discovered LIVE 2026-05-25 during Op B validation when the new
// loop_runtime_clustercredential_ca_fetch_total counter didn't show up despite the
// counter object being created and Inc()'d.
package metrics

import (
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	cb "github.com/getloopscale/loop-runtime/pkg/circuitbreaker"
)

var (
	// ReconcileDuration measures the duration of each reconcile loop in seconds.
	ReconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "loop_runtime_operator_reconcile_duration_seconds",
			Help:    "Duration of reconcile loops by controller and result.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"controller", "result"},
	)

	// ReconcileErrors counts reconcile errors by controller and error type.
	ReconcileErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "loop_runtime_operator_reconcile_errors_total",
			Help: "Total number of reconcile errors by controller and error type.",
		},
		[]string{"controller", "error_type"},
	)

	// ReconcileTotal counts total reconcile invocations by controller and result.
	ReconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "loop_runtime_operator_reconcile_total",
			Help: "Total number of reconcile invocations by controller and result (success/error/requeue).",
		},
		[]string{"controller", "result"},
	)

	// MEET-052: Tenant deletion per-step metrics.

	// TenantDeletionStepDuration measures the duration of each cleanup step
	// during tenant deletion (argocd, build-ns, minio, gitea).
	TenantDeletionStepDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "loop_runtime_tenant_deletion_step_duration_seconds",
			Help:    "Duration of each cleanup step during tenant deletion.",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
		},
		[]string{"step", "result"}, // result: "success" | "error"
	)

	// TenantDeletionStuckTotal counts tenants that exceeded max deletion attempts.
	TenantDeletionStuckTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "loop_runtime_tenant_deletion_stuck_total",
			Help: "Total number of tenants that exceeded max deletion retry attempts.",
		},
	)

	// TenantCircuitBreakerState tracks circuit breaker state per dependency
	// (0=closed, 1=open, 2=half-open), written by
	// ObserveCircuitBreakerTransition on every state change.
	TenantCircuitBreakerState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "loop_runtime_tenant_cb_state",
			Help: "Circuit breaker state per dependency (0=closed, 1=open, 2=half-open).",
		},
		[]string{"dep"},
	)

	// MisplacedTenantTotal counts Tenant CRs found outside the canonical
	// namespace `loop-runtime-system` (ADR-0018 v2 / MEET-ADR-0018 condition C5).
	// Written by tenant.MisplacedTenantScanner, which lists Tenants
	// cluster-wide through an uncached reader at start and every 5 minutes:
	// with C9 cache scoping no reconciler can see a misplaced CR, so the
	// scan is what surfaces it (fail-loud per platform-architect C2).
	// Non-zero value fires the MisplacedTenantDetected alert (>5 min).
	MisplacedTenantTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "loop_runtime_misplaced_tenant_total",
			Help: "Tenant CRs found outside canonical namespace loop-runtime-system (ADR-0018 invariant).",
		},
		[]string{"namespace"},
	)

	// ClusterCredentialCAFetchTotal counts live CA fetch attempts in
	// ClusterCredentialReconciler.reconcileArgoCDClusterSecret (plan
	// 2026-05-25-live-ca-fetch-clustercredential, Op B). result:
	//   - "success"          → live kube-root-ca.crt fetched and used
	//   - "fallback"         → fetch failed; snapshot caData used (degraded)
	//   - "skipped-insecure" → cr.Spec.TLS.Insecure=true short-circuit (Op A precedence)
	// Sustained "fallback" > 0 over 7-day window blocks Phase 5 heal cleanup
	// (T5.0 pre-flight gate enforces this via validate-op-b-soak.sh).
	ClusterCredentialCAFetchTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "loop_runtime_clustercredential_ca_fetch_total",
			Help: "Live CA fetch attempts in ClusterCredentialReconciler. result=success|fallback|skipped-insecure.",
		},
		[]string{"cluster", "result"},
	)
)

func init() {
	ctrlmetrics.Registry.MustRegister(
		ReconcileDuration,
		ReconcileErrors,
		ReconcileTotal,
		TenantDeletionStepDuration,
		TenantDeletionStuckTotal,
		TenantCircuitBreakerState,
		MisplacedTenantTotal,
		// Op B (plan 2026-05-25-live-ca-fetch-clustercredential):
		ClusterCredentialCAFetchTotal,
	)
}

// ObserveCircuitBreakerTransition is the state-change callback for the
// operators' circuit breakers: it logs the transition and publishes the new
// state in TenantCircuitBreakerState, so an open breaker is visible on
// /metrics and not only in the log.
func ObserveCircuitBreakerTransition(name string, from, to cb.State) {
	slog.Info("circuit breaker state change", "breaker", name, "from", from.String(), "to", to.String())
	TenantCircuitBreakerState.WithLabelValues(name).Set(float64(to))
}
