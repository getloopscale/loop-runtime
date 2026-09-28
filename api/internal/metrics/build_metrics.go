// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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

// Build backpressure metrics — emitted by BuildReconcileLoop per-tenant limiting.
var (
	BuildTenantBackpressure = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_build_tenant_backpressure_total",
		Help: "Builds deferred because tenant exceeded concurrent build limit.",
	}, []string{"tenant"})
)

// LoopZeroApplication CR lifecycle metrics — emitted by ensureApplicationCRs.
var (
	ApplicationCREnsureTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_application_cr_ensure_total",
		Help: "Total LoopZeroApplication CR ensure attempts by result.",
	}, []string{"tenant", "app", "result"}) // result: "success" or "failure"
)

// Build admission control metrics — Roadmap 07 (Backpressure & Admission).
var (
	BuildAdmissionAdmitted = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_build_admission_admitted_total",
		Help: "Builds admitted by the admission gate.",
	}, []string{"cell_id"})

	BuildAdmissionRejected = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_build_admission_rejected_total",
		Help: "Builds rejected by the admission gate.",
	}, []string{"cell_id", "reason"})

	PGCircuitBreakerTripped = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_pg_circuit_breaker_tripped_total",
		Help: "Mutation requests rejected due to PG pool saturation.",
	}, []string{"method"})

	// PlanLimitViolations tracks per-metric per-plan enforcement rejections.
	// B15 (CHG-2026-04-23-B15 / MEET-2026-04-23-059 action #9).
	// Emitted when BuildOrchestrator.SubmitBuild rejects a request via
	// ErrReplicasExceedPlan or ErrScalingAmbiguous.
	PlanLimitViolations = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_plan_limit_violation_total",
		Help: "Plan-limit enforcement rejections by metric and plan.",
	}, []string{"metric", "plan"})
)
