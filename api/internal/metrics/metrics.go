// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package metrics provides Prometheus instrumentation for the LoopZero API.
//
// All metric names follow the spec §15.2 naming convention: loopzero_<subsystem>_<name>.
// Labels follow Prometheus best practices: lowercase, snake_case, bounded cardinality.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// HTTP request metrics — emitted by the metrics middleware on every request.
var (
	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loopzero_api_request_duration_seconds",
		Help:    "HTTP request duration in seconds.",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"method", "path", "status"})

	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_api_requests_total",
		Help: "Total number of HTTP requests.",
	}, []string{"method", "path", "status"})

	HTTPRequestsInFlight = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopzero_api_requests_in_flight",
		Help: "Number of HTTP requests currently being processed.",
	})
)

// Build metrics — emitted by the build orchestrator on state transitions.
var (
	BuildsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_builds_total",
		Help: "Total number of builds by final status.",
	}, []string{"tenant", "environment", "status"})

	BuildDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loopzero_build_duration_seconds",
		Help:    "Build duration from creation to completion.",
		Buckets: []float64{5, 10, 30, 60, 120, 180, 300, 600},
	}, []string{"tenant", "environment", "status"})

	BuildsActive = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loopzero_builds_active",
		Help: "Number of currently active builds (QUEUED or RUNNING).",
	}, []string{"tenant"})

	BuildQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopzero_build_queue_depth",
		Help: "Number of builds waiting in QUEUED state.",
	})
)

// Release activation metrics (MEET-2026-03-26-008).
var (
	// ReleasingBuilds tracks builds in RELEASING state (image built, release pending).
	// Non-zero for extended periods indicates activation failures that the reconcile
	// loop is retrying. Alert: LoopZeroOrphanBuild fires when > 0 for 5 minutes.
	ReleasingBuilds = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopzero_releasing_builds",
		Help: "Number of builds in RELEASING state (image built, awaiting release activation).",
	})
)

// Deploy/release metrics — emitted when releases transition states.
var (
	DeploysTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_deploys_total",
		Help: "Total number of deploy attempts by final status.",
	}, []string{"tenant", "environment", "status"})

	DeployDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loopzero_deploy_duration_seconds",
		Help:    "End-to-end deploy duration from build creation to release ACTIVE.",
		Buckets: []float64{10, 30, 60, 120, 180, 300, 600, 900},
	}, []string{"tenant", "environment"})
)

// Rollback metrics — emitted by the rollback orchestrator (VAL-OBS-001 fix).
var (
	RollbacksTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_rollbacks_total",
		Help: "Total number of rollback attempts by final status.",
	}, []string{"tenant", "environment", "status"})

	RollbackDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loopzero_rollback_duration_seconds",
		Help:    "Rollback duration from initiation to completion.",
		Buckets: []float64{1, 2, 5, 10, 30, 60, 120},
	}, []string{"tenant", "environment"})
)

// ArgoCD circuit breaker + auth metrics (MEET-2026-03-28-013).
var (
	// ArgoCDCircuitState tracks the circuit breaker state.
	// 0=closed (normal), 1=open (rejecting), 2=half-open (probing).
	// Fixes phantom alert: LoopZeroArgoCDCircuitOpen referenced this metric but it was never emitted.
	ArgoCDCircuitState = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopzero_argocd_circuit_state",
		Help: "ArgoCD circuit breaker state (0=closed, 1=open, 2=half-open).",
	})

	// ArgoCDAuthFailures counts ArgoCD authentication failures (401/403).
	// These are NOT counted toward the circuit breaker threshold.
	ArgoCDAuthFailures = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loopzero_argocd_auth_failures_total",
		Help: "Total ArgoCD authentication failures (401/403). Indicates invalid or expired token.",
	})
)

// Lock contention metrics — emitted by withEnvironmentLock.
var (
	LockAcquired = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_lock_acquired_total",
		Help: "Number of environment lock acquisitions.",
	}, []string{"tenant", "environment"})

	LockConflicts = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_lock_conflicts_total",
		Help: "Number of environment lock acquisition failures (409 conflict).",
	}, []string{"tenant", "environment"})

	LockDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loopzero_lock_hold_duration_seconds",
		Help:    "Duration the environment lock was held.",
		Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30},
	}, []string{"tenant", "environment"})
)

// Idempotency metrics — emitted by idempotency middleware.
var (
	IdempotencyHits = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_idempotency_cache_hits_total",
		Help: "Number of idempotency cache hits (deduplicated requests).",
	}, []string{"endpoint"})

	IdempotencyMisses = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_idempotency_cache_misses_total",
		Help: "Number of idempotency cache misses (new requests).",
	}, []string{"endpoint"})
)

// Health probe metrics — emitted by the health prober after deploy.
var (
	HealthProbeResult = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_health_probe_results_total",
		Help: "Number of health probe attempts by result.",
	}, []string{"result"}) // "success" or "failure"

	HealthProbeDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loopzero_health_probe_duration_seconds",
		Help:    "Duration of health probe attempts.",
		Buckets: []float64{0.5, 1, 2, 5, 10, 15, 30},
	}, []string{"tenant", "environment"})
)

// SSE stream metrics — emitted by SSE stream handler.
var (
	SSEConnectionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopzero_sse_connections_active",
		Help: "Number of active SSE streaming connections.",
	})

	SSEEventsEmitted = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_sse_events_emitted_total",
		Help: "Total number of SSE events emitted to clients.",
	}, []string{"event_type"})
)

// State divergence metrics — MEET-2026-03-31-023 T0-2 and T1-4/T1-8.
// Emitted by the release convergence loop when checking ACTIVE releases against ArgoCD health.
var (
	// DriftDetected is set to 1 when cluster state diverges from desired state in Git.
	// Referenced by PrometheusRule LoopZeroDriftDetected (prometheusrule-critical.yaml).
	// T0-2 fix: this metric was referenced in alerts but never emitted (phantom alert).
	DriftDetected = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loopzero_drift_detected",
		Help: "Set to 1 when cluster state diverges from desired state in Git for a tenant/environment.",
	}, []string{"tenant", "env"})

	// ReleaseStateDivergence is set to 1 when a release marked ACTIVE in PG
	// does not correspond to a healthy deployment in ArgoCD.
	// T1-4: Post-activation health monitoring.
	ReleaseStateDivergence = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loopzero_release_state_divergence",
		Help: "Set to 1 when a release is ACTIVE in PostgreSQL but unhealthy in ArgoCD.",
	}, []string{"tenant", "project", "env"})

	// ReleasesPending tracks the count of releases currently in PENDING state.
	// Alert: LoopZeroReleasesPendingStuck fires when > 0 for 10 minutes.
	ReleasesPending = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopzero_releases_pending",
		Help: "Number of releases currently in PENDING state (awaiting ArgoCD convergence).",
	})
)

// Reconciliation loop health metrics — MEET-2026-03-31-023 T1-8.
var (
	// ReconcileLoopLastSuccess records the timestamp of the last successful loop tick.
	// Alert: LoopZeroReconcileLoopStale fires when stale > 2x interval.
	ReconcileLoopLastSuccess = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loopzero_reconcile_loop_last_success_timestamp",
		Help: "Unix timestamp of the last successful reconcile loop tick.",
	}, []string{"loop"}) // "build" or "release"

	// GitOpsOperationDuration measures the latency of GitOps operations (clone, push, remove).
	GitOpsOperationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loopzero_gitops_operation_duration_seconds",
		Help:    "Duration of GitOps operations (git clone, push, remove).",
		Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
	}, []string{"operation"}) // "push", "remove"

	// ReleaseConvergenceDuration measures time from release creation (PENDING) to activation (ACTIVE).
	ReleaseConvergenceDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loopzero_release_convergence_duration_seconds",
		Help:    "Duration from release creation to ACTIVE state.",
		Buckets: []float64{5, 10, 30, 60, 120, 180, 300},
	}, []string{"tenant", "environment"})
)

// Cell metrics — ADR-0008 Phase 0.
// Emitted by a periodic collector (query: SELECT cell_id, COUNT(*) FROM tenants GROUP BY cell_id).
var (
	// CellTenantCount tracks the number of tenants assigned to each Cell.
	// Used to monitor Cell capacity and trigger auto-provisioning in Phase 3.
	// Alert (Phase 3): LoopZeroCellCapacityWarning fires when > 500 tenants per Cell.
	CellTenantCount = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loopzero_cell_tenant_count",
		Help: "Number of tenants assigned to each Cell.",
	}, []string{"cell_id"})
)

// Database connection pool metrics — emitted by a periodic collector.
var (
	DBConnectionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopzero_postgres_connections_active",
		Help: "Number of active PostgreSQL connections.",
	})

	DBConnectionsIdle = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopzero_postgres_connections_idle",
		Help: "Number of idle PostgreSQL connections.",
	})

	DBConnectionsTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopzero_postgres_connections_total",
		Help: "Total number of PostgreSQL connections in the pool.",
	})
)
