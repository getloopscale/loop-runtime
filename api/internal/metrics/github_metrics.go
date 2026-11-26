// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// GitHub webhook metrics — MEET-044 P1.
var (
	WebhookPushTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopruntime_webhook_push_total",
		Help: "Total GitHub push webhook events by result.",
	}, []string{"result"}) // result: "deployed", "skipped", "error"

	WebhookProcessingDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loopruntime_webhook_processing_duration_seconds",
		Help:    "Duration of webhook push processing (download + bundle + submit).",
		Buckets: []float64{0.5, 1, 2, 5, 10, 30},
	}, []string{"phase"}) // phase: "download", "bundle", "upload", "submit", "total"

	// Plan J-09 T4.2 (2026-06-05). Counts every webhook delivery the
	// engine receives, labelled by GitHub event_type (push, ping, etc.)
	// AND by handler outcome (status). SLO queries:
	//   - "are pushes reaching us?"  rate(*{event_type='push'}[5m])
	//   - "are pushes auto-deploying?" ratio of status='auto_deploy_triggered'
	//     to event_type='push' totals.
	// Distinct from WebhookPushTotal (above) which only counts push events
	// that reach the auto-deployer -- deliveries_total counts the wire
	// edge (the HMAC-validated payload arriving at the handler).
	GitHubWebhookDeliveriesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopruntime_github_webhook_deliveries_total",
		Help: "Total GitHub webhook deliveries received, labelled by event_type and handler outcome.",
	}, []string{"event_type", "status"})

	// Plan J-17 T1.3 (2026-06-06). Counts every Argo Workflow node phase
	// transition that the argo_step_watcher service observed and emitted
	// as an SSE event. Labels:
	//   - step: the step family from events.StepMetricLabel ("build-image"
	//     for "build-image-checkout") OR "other" (EC-16 cardinality guard —
	//     families are a fixed set, so the label stays bounded however many
	//     apps and languages projects use).
	//   - status: started | succeeded | failed.
	// SLO queries:
	//   - "% builds with all steps succeeded": ratio of {status="failed"}
	//     to {status="started"} grouped by step.
	//   - "time-to-deploy p95": measured client-side via build.started ->
	//     last build.step.succeeded distance (not via this counter).
	DeploymentsStepEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopruntime_deployments_step_events_total",
		Help: "Total deployment step events emitted to SSE, labelled by step name and outcome.",
	}, []string{"step", "status"})

	// EC-1 — Build cluster SA token expiry surface (Plan J-17 T1.1.bis).
	// argo_step_watcher informer reconnects after a 401/403 from the
	// build cluster API server bump this counter. Grafana alert
	// rate(loopruntime_argo_watcher_token_expired_total[5m]) > 0 pages the
	// operator to rotate the SA token before events stop flowing.
	ArgoWatcherTokenExpiredTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loopruntime_argo_watcher_token_expired_total",
		Help: "Total times argo_step_watcher received 401/403 from the build cluster API.",
	})

	// ArgoCD convergence observability — MEET-044 P1.
	RevisionMismatchTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loopruntime_release_revision_mismatch_total",
		Help: "Total revision mismatches detected by convergence loop.",
	})
	RevisionResyncTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loopruntime_release_revision_resync_total",
		Help: "Total re-syncs triggered due to revision mismatch.",
	})
	RevisionResyncFailureTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loopruntime_release_revision_resync_failure_total",
		Help: "Total re-sync failures after revision mismatch.",
	})

	// Superseded build cancellation — MEET-044 P0.
	BuildsSupersededTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopruntime_builds_superseded_total",
		Help: "Total QUEUED builds cancelled due to newer push.",
	}, []string{"tenant"})
)
