// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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

	// CHG-2026-04-25-ATTESTATION-WIRE Phase 7: counts releases by
	// whether their image was signed at release-creation time. Lets
	// dashboards/alerts notice when signing silently regresses (e.g.,
	// CWT outputs missing or cosign step failing without breaking
	// the workflow).
	//   signed="true"  → release row has image_signed_at populated
	//   signed="false" → release row has image_signed_at NULL
	ReleaseSignedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_release_signed_total",
		Help: "Releases created, partitioned by whether the image was signed.",
	}, []string{"tenant", "environment", "signed"})

	// Sprint 6 / T3.2: per-app supply-chain attestation metric. Surfaces
	// signing health at the granularity admission policy enforces. Multi-app
	// builds emit one increment per app; ALL must be signed=true for the
	// runtime cluster's policy-controller (D5) to admit.
	ReleaseAppAttestationTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_release_app_attestation_total",
		Help: "Per-app release attestations recorded, partitioned by whether the image was signed.",
	}, []string{"tenant", "environment", "app", "signed"})

	// Sprint 6 / T3.2: SBOM generation metric. One increment per release
	// that includes at least one SBOM (tag value: tenant + environment).
	// Useful for tracking syft adoption across tenants and detecting
	// regressions where the SBOM step starts failing silently
	// (continueOn.failed at the DAG level swallows step errors).
	SBOMGeneratedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_sbom_generated_total",
		Help: "Releases that produced an SBOM (syft step), per (tenant, environment).",
	}, []string{"tenant", "environment"})

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
