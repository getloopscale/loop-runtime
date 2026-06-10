// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package metrics provides custom Prometheus metrics for operator controllers.
// All metrics follow the loopzero_operator_ prefix convention for Grafana dashboards.
//
// Registration target: controller-runtime's metrics.Registry (NOT the default
// prometheus registry). The operator's metricsserver.Options serves only from
// the controller-runtime registry; metrics registered in
// prometheus.DefaultRegisterer would silently never appear in the /metrics
// endpoint. Discovered LIVE 2026-05-25 during Op B validation when the new
// loopzero_clustercredential_ca_fetch_total counter didn't show up despite the
// counter object being created and Inc()'d.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// ReconcileDuration measures the duration of each reconcile loop in seconds.
	ReconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "loopzero_operator_reconcile_duration_seconds",
			Help:    "Duration of reconcile loops by controller and result.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"controller", "result"},
	)

	// ReconcileErrors counts reconcile errors by controller and error type.
	ReconcileErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "loopzero_operator_reconcile_errors_total",
			Help: "Total number of reconcile errors by controller and error type.",
		},
		[]string{"controller", "error_type"},
	)

	// ReconcileTotal counts total reconcile invocations by controller and result.
	ReconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "loopzero_operator_reconcile_total",
			Help: "Total number of reconcile invocations by controller and result (success/error/requeue).",
		},
		[]string{"controller", "result"},
	)

	// ProvisionerDuration measures the duration of provisioner operations in seconds.
	ProvisionerDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "loopzero_operator_provisioner_duration_seconds",
			Help:    "Duration of provisioner operations (gitea, argocd, harbor, minio).",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"provisioner", "operation"},
	)

	// CRDCount tracks the number of CRD instances by type (set during reconcile).
	CRDCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "loopzero_operator_crd_count",
			Help: "Current count of CRD instances by type.",
		},
		[]string{"crd_type"},
	)

	// MEET-052: Tenant deletion per-step metrics.

	// TenantDeletionStepDuration measures the duration of each cleanup step
	// during tenant deletion (harbor, argocd, build-ns, minio, gitea).
	TenantDeletionStepDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "loopzero_tenant_deletion_step_duration_seconds",
			Help:    "Duration of each cleanup step during tenant deletion.",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
		},
		[]string{"step", "result"}, // result: "success" | "error"
	)

	// TenantDeletionStuckTotal counts tenants that exceeded max deletion attempts.
	TenantDeletionStuckTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "loopzero_tenant_deletion_stuck_total",
			Help: "Total number of tenants that exceeded max deletion retry attempts.",
		},
	)

	// TenantCircuitBreakerState tracks circuit breaker state per dependency.
	// 0=closed, 1=open, 2=half-one.
	TenantCircuitBreakerState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "loopzero_tenant_cb_state",
			Help: "Circuit breaker state per dependency (0=closed, 1=open, 2=half-open).",
		},
		[]string{"dep"},
	)

	// MisplacedTenantTotal counts Tenant CRs found outside the canonical
	// namespace `loopzero-system` (ADR-0018 v2 / MEET-ADR-0018 condition C5).
	// Emitted by tenant-controller startup scan + every reconcile cycle that
	// detects a misplaced CR. Non-zero value should fire the
	// MisplacedTenantDetected alert in Prometheus (>5 min sustained).
	//
	// Pre-fix this counter did not exist; misplaced CRs went undetected and
	// were processed by the cluster-wide informer (iter 7-8 of dogfood). With
	// C9 cache scoping the operator no longer sees them, but the startup
	// scan uses an unscoped client to surface the gap explicitly — fail-loud
	// per platform-architect C2.
	MisplacedTenantTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "loopzero_misplaced_tenant_total",
			Help: "Tenant CRs found outside canonical namespace loopzero-system (ADR-0018 invariant).",
		},
		[]string{"namespace"},
	)

	// ─── ADR-0027 D5 / plan T5.2 — Level IV "Deep Insights" metrics ───
	//
	// These metrics satisfy the L4 capability rubric (≥10 controller-specific
	// metrics + ≥3 SLO alerts) per capability-matrix.md. Emitted by:
	//   - SelfHealTotal: Application reaper transitions; preview TTL expiries;
	//                    cell credential token refresh (in-process).
	//   - OrphanCRTotal: detected on reaper sweeps + on per-reconcile checks.
	//   - FinalizerBlockedSeconds: how long finalizer holds delete; alert
	//                              when P95 > deletion budget.
	//   - ReconcileQueueDepth: surface by `controller_runtime_active_workers`
	//                         saturation; T5.2 adds the LoopZero-flavored gauge
	//                         for cross-binary aggregation.
	//   - StatusUpdateConflictsTotal: per ADR-0024 SRE condition #3 — observe
	//                                 retry helper effectiveness.

	// SelfHealTotal counts in-process self-heal events (ADR-0027 L5
	// evidence). Examples: application_reaper transition; preview
	// TTL expiry; cluster credential token refresh.
	SelfHealTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "loopzero_self_heal_total",
			Help: "In-process self-heal events by controller and action (ADR-0027 L5 evidence).",
		},
		[]string{"controller", "action"},
	)

	// OrphanCRTotal counts orphan CR detections (e.g., LoopZeroApplication
	// whose gitops manifest disappeared). Per ADR-0027 L4/L5 boundary.
	OrphanCRTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "loopzero_orphan_cr_total",
			Help: "Orphan CRs detected by reaper sweeps (manifest removed from gitops).",
		},
		[]string{"kind"},
	)

	// FinalizerBlockedSeconds tracks how long a finalizer holds the
	// deletion of a CR. P95 above the deletion budget (typically 5min)
	// triggers an SLO alert.
	FinalizerBlockedSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "loopzero_finalizer_blocked_seconds",
			Help:    "Duration that a finalizer held a CR deletion before unblocking.",
			Buckets: []float64{0.5, 1, 5, 10, 30, 60, 120, 300, 600},
		},
		[]string{"kind"},
	)

	// ReconcileQueueDepth surfaces per-controller workqueue backlog.
	// Aggregated across the 4 split binaries via Prometheus federation
	// (ADR-0023 split topology). Saturation alert fires when sustained > N.
	ReconcileQueueDepth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "loopzero_reconcile_queue_depth",
			Help: "Current workqueue backlog per controller.",
		},
		[]string{"controller"},
	)

	// StatusUpdateConflictsTotal counts conflict errors hit by
	// UpdateStatusWithRetry (ADR-0024 SRE condition #3). Sustained high
	// rate indicates contention pressure that the retry helper is masking
	// — prompts capacity planning conversation.
	StatusUpdateConflictsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "loopzero_status_update_conflicts_total",
			Help: "Status().Update conflicts retried via conditions.UpdateStatusWithRetry (ADR-0024).",
		},
		[]string{"controller"},
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
			Name: "loopzero_clustercredential_ca_fetch_total",
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
		ProvisionerDuration,
		CRDCount,
		TenantDeletionStepDuration,
		TenantDeletionStuckTotal,
		TenantCircuitBreakerState,
		MisplacedTenantTotal,
		// ADR-0027 L4 metrics:
		SelfHealTotal,
		OrphanCRTotal,
		FinalizerBlockedSeconds,
		ReconcileQueueDepth,
		StatusUpdateConflictsTotal,
		// Op B (plan 2026-05-25-live-ca-fetch-clustercredential):
		ClusterCredentialCAFetchTotal,
	)
}
