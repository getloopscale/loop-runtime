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

	// ArgoCDTokenAgeSeconds is the age in seconds of the currently active
	// ArgoCD API token used by loopzero-api. -1 sentinel = unknown / no token
	// minted yet. Self-healing token provider exposes this so Prometheus
	// can alert on tokens that aren't being rotated as expected.
	// CHG-2026-04-30-DOGFOOD-LOCAL-HEAL NEW-15.
	ArgoCDTokenAgeSeconds = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopzero_argocd_token_age_seconds",
		Help: "Seconds since the active ArgoCD API token was last minted by KubeMintingTokenProvider. -1 = unknown.",
	})

	// ArgoCDTokenRefreshesTotal counts token mint attempts by trigger and
	// outcome. trigger ∈ {startup, startup_empty, proactive, proactive_empty,
	// auth_failure, manual}. outcome ∈ {success, error, skipped}.
	ArgoCDTokenRefreshesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_argocd_token_refreshes_total",
		Help: "Total ArgoCD token mint attempts. Labels: trigger (proactive|auth_failure|...), outcome (success|error|skipped).",
	}, []string{"trigger", "outcome"})
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

// Cloudflare KV metrics — emitted by cfkv.Client on every Put/Delete.
var (
	CFKVOperationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_cfkv_operations_total",
		Help: "Total Cloudflare KV operations by type and result.",
	}, []string{"op", "result"}) // op: "put"/"delete", result: "success"/"failure"

	CFKVOperationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loopzero_cfkv_operation_duration_seconds",
		Help:    "Duration of Cloudflare KV operations.",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5},
	}, []string{"op"})

	CFKVDriftDetected = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopzero_cfkv_drift_detected",
		Help: "Set to 1 when KV state diverges from database state. 0 when reconciled.",
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

	// CellDiscoveryCount tracks the number of cells currently discovered by DynamicRouter.
	// MEET-053 A-09: Feeds LoopZeroCellDiscoveryEmpty alert (critical when == 0 for 60s).
	CellDiscoveryCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopzero_cell_discovery_count",
		Help: "Number of cells currently discovered by DynamicRouter.",
	})

	// OrphanedTenants tracks tenants assigned to cells that no longer exist in the router.
	// MEET-053 D-04: Non-zero value means tenants cannot deploy until cell is restored
	// or tenants are migrated.
	OrphanedTenants = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopzero_orphaned_tenants",
		Help: "Number of tenants assigned to cells not currently discovered by the router.",
	})

	// CellRoutingDecisions tracks every decision made by DynamicRouter.Resolve()
	// labelled by cell + outcome. CHG-2026-04-30-INFRA-NORMALIZE / Phase 5
	// T5.4 (ADR D6 mandate: observability BEFORE feature ships) +
	// MEET-INFRA-NORMALIZE-T3-multi-cell condition C19 (cell-unavailable
	// handled deterministically, returns 503 not 500).
	//
	// Decision values:
	//   "assigned"   — cell found, request routed normally
	//   "fallback"   — primary cell unavailable, default-cell substituted
	//   "error"      — cell_id empty or unknown, no fallback possible
	//
	// Alert MultiCellRouterErrorRate fires when
	// rate(loopzero_cell_routing_decisions_total{decision="error"}[5m]) > 0.01.
	CellRoutingDecisions = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_cell_routing_decisions_total",
		Help: "Routing decisions made by DynamicRouter, labelled by cell + outcome (assigned|fallback|error).",
	}, []string{"cell", "decision"})

	// CellHealth is 1 if the cell's ClusterCredential reports
	// status.connected=true AND its kubeconfig responds to a probe within
	// the refresh interval, else 0. Aggregates the connectivity SoT (CC
	// CRD) into a single boolean for alerting.
	//
	// Alert CellUnhealthy fires when loopzero_cell_health == 0 for any cell
	// for 2 consecutive minutes. Pages on-call.
	CellHealth = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loopzero_cell_health",
		Help: "1 if cell's ClusterCredential is connected and reachable, 0 otherwise.",
	}, []string{"cell"})

	// CellCapacityPods tracks the number of pods (across all tenant
	// namespaces) currently scheduled on each cell. Feeds capacity
	// planning + the `cells.capacity_tenants_max` gate (when consumer
	// in placement layer ships, T5.5 follow-up). Updated by a periodic
	// collector (planned: every 60s, mirroring CellDiscoveryCount).
	CellCapacityPods = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loopzero_cell_capacity_pods",
		Help: "Number of pods currently scheduled on each cell (sum across tenant namespaces).",
	}, []string{"cell"})

	// ClusterCredentialStatus mirrors ClusterCredential CR
	// status.connected as a per-cell gauge. Lets alerts trigger on
	// connectivity flapping without polling K8s API directly. Audit
	// T0.1 action item A5.
	//
	// Distinct from CellHealth: this is the RAW CC status; CellHealth
	// adds active probing (a CC may report connected=true but the API
	// server became unreachable in the last 60s).
	ClusterCredentialStatus = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loopzero_cluster_credential_status",
		Help: "1 if ClusterCredential.status.connected=true, 0 otherwise. Per-cell.",
	}, []string{"cell", "connected"})

	// PulumiArchivesTotal tracks the number of Pulumi state archives
	// in s3://loopzero-archive/pulumi-state/. CHG-2026-04-30-INFRA-NORMALIZE
	// Phase 3 condition C20. Updated by the quarterly archive scan
	// (.github/workflows/pulumi-archive.yml). Alert
	// PulumiArchiveStaleness fires when no archive in 90d.
	PulumiArchivesTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopzero_pulumi_archives_total",
		Help: "Number of Pulumi state archives currently in S3 (loopzero-archive/pulumi-state/).",
	})

	// PulumiStacksTotal tracks the number of live Pulumi stacks across
	// the canonical project. C20 + monitors paralelas-stack
	// re-introduction (any stack outside the canonical name set →
	// PulumiOrphanStackDetected alert).
	PulumiStacksTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopzero_pulumi_stacks_total",
		Help: "Number of live Pulumi stacks in the canonical project (loopzero-infra).",
	})
)

// Leader election metrics — MEET-2026-04-04-029 P1.
var (
	// LeaderStatus is 1 if this replica holds the leader advisory lock, 0 otherwise.
	// Alert: LoopZeroLeaderLost fires when all replicas report 0 for > 2 minutes.
	LeaderStatus = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopzero_api_leader_status",
		Help: "Whether this API replica holds the leader advisory lock (1=leader, 0=follower).",
	})
)

// Service auth secret age metric — WS.1 security blocker.
// Tracks how long the current service auth secret has been in use.
// Alert: LoopZeroServiceAuthSecretExpiring fires when age > 80 days.
var (
	ServiceAuthSecretAge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopzero_service_auth_secret_age_seconds",
		Help: "Age of the current service auth secret in seconds.",
	})
)

// Custom domain metrics — MEET-2026-04-05-033.
// Emitted by DomainHandler, DomainProvisioner, and DomainHealthChecker.
var (
	CustomDomainsTotal = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loopzero_custom_domains_total",
		Help: "Number of custom domains by status.",
	}, []string{"tenant", "status"})

	DomainVerificationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loopzero_domain_verification_duration_seconds",
		Help:    "Duration of DNS TXT verification checks.",
		Buckets: []float64{0.1, 0.5, 1, 2, 5, 10},
	}, []string{"result"})

	DomainProvisioningDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loopzero_domain_provisioning_duration_seconds",
		Help:    "Duration from domain verification to active state.",
		Buckets: []float64{10, 30, 60, 120, 300, 600},
	}, []string{"result"})

	// Cardinality cap (audit gap M08 #19): the `tenant` label was removed
	// to bound series count at N (domains) instead of N×M (domains×tenants).
	// Tenant attribution remains forensically recoverable by joining on
	// `loopzero_custom_domains_total{tenant=...}` or querying audit_log by
	// domain. Operators that previously consumed `tenant` in alert
	// descriptions are migrated below to reference the audit trail.
	DomainTLSExpiryDays = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loopzero_domain_tls_expiry_days",
		Help: "Days until TLS certificate expires for a custom domain.",
	}, []string{"domain"})

	DomainVerificationFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_domain_verification_failures_total",
		Help: "Total DNS verification failures.",
	}, []string{"tenant"})

	DomainCNAMECheckFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_domain_cname_check_failures_total",
		Help: "Total CNAME re-verification failures for active domains.",
	}, []string{"tenant"})

	DomainHealthCheckerErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_domain_health_checker_errors_total",
		Help: "Total errors in the domain health checker by check type.",
	}, []string{"check"})

	// v2: CF Zone Management metrics
	DomainCFZoneErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_domain_cf_zone_errors_total",
		Help: "Total errors from Cloudflare Zone API.",
	}, []string{"operation"})

	DomainNSPropagationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loopzero_domain_ns_propagation_duration_seconds",
		Help:    "Duration from domain creation to NS propagation (pending_ns → ns_active).",
		Buckets: []float64{60, 300, 900, 1800, 3600, 7200, 14400, 43200, 86400},
	}, []string{"result"})
)

// GitHub webhook metrics — MEET-044 P1.
var (
	WebhookPushTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_webhook_push_total",
		Help: "Total GitHub push webhook events by result.",
	}, []string{"result"}) // result: "deployed", "skipped", "error"

	WebhookProcessingDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loopzero_webhook_processing_duration_seconds",
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
		Name: "loopzero_github_webhook_deliveries_total",
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
		Name: "loopzero_deployments_step_events_total",
		Help: "Total deployment step events emitted to SSE, labelled by step name and outcome.",
	}, []string{"step", "status"})

	// EC-1 — Build cluster SA token expiry surface (Plan J-17 T1.1.bis).
	// argo_step_watcher informer reconnects after a 401/403 from the
	// build cluster API server bump this counter. Grafana alert
	// rate(loopzero_argo_watcher_token_expired_total[5m]) > 0 pages the
	// operator to rotate the SA token before events stop flowing.
	ArgoWatcherTokenExpiredTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loopzero_argo_watcher_token_expired_total",
		Help: "Total times argo_step_watcher received 401/403 from the build cluster API.",
	})

	// ArgoCD convergence observability — MEET-044 P1.
	RevisionMismatchTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loopzero_release_revision_mismatch_total",
		Help: "Total revision mismatches detected by convergence loop.",
	})
	RevisionResyncTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loopzero_release_revision_resync_total",
		Help: "Total re-syncs triggered due to revision mismatch.",
	})
	RevisionResyncFailureTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loopzero_release_revision_resync_failure_total",
		Help: "Total re-sync failures after revision mismatch.",
	})

	// Superseded build cancellation — MEET-044 P0.
	BuildsSupersededTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_builds_superseded_total",
		Help: "Total QUEUED builds cancelled due to newer push.",
	}, []string{"tenant"})
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

	// P2-9: Pool saturation metrics — detect contention before it causes failures.
	DBPoolMaxConns = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loopzero_postgres_pool_max_conns",
		Help: "Maximum configured connections in the pool.",
	})

	DBPoolAcquireCount = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loopzero_postgres_pool_acquire_total",
		Help: "Total number of connection acquire attempts.",
	})

	DBPoolAcquireDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "loopzero_postgres_pool_acquire_duration_seconds",
		Help:    "Time spent waiting for a connection from the pool.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 5.0},
	})

	DBPoolEmptyAcquireCount = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loopzero_postgres_pool_empty_acquire_total",
		Help: "Number of times all connections were in use when acquire was called (pool saturation signal).",
	})
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

// Business metrics — observability gaps resolution plan (Phase 4A).
var (
	// DeployLeadTime measures the total time from build creation to release ACTIVE
	// plus health probe success. This is the executive metric: "how long until the
	// user's code is live?"
	DeployLeadTime = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loopzero_deploy_lead_time_seconds",
		Help:    "Total time from build creation to healthy URL (deploy lead time).",
		Buckets: []float64{30, 60, 120, 180, 300, 600, 900, 1800},
	}, []string{"tenant", "environment"})

	// FalseSuccessTotal counts releases that were marked ACTIVE but later detected
	// as unhealthy by the convergence loop's health check. A non-zero rate indicates
	// problems with health probes or deployment reliability.
	FalseSuccessTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_false_success_total",
		Help: "Releases marked ACTIVE that later became unhealthy (false success).",
	}, []string{"tenant", "environment"})

	// RetryAmplificationTotal counts total retry attempts per operation type.
	// High values indicate cascading failures or misconfigured timeouts.
	RetryAmplificationTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_retry_amplification_total",
		Help: "Total retry attempts by operation type.",
	}, []string{"operation"})

	// SecretExternalSecretTotal counts ExternalSecret CR create/update calls
	// from SecretK8sManager (cell-0-cluster path).
	//
	// dogfood-2026-05-08-fixes T2.3 (F-2 instrumentation, ADR D3).
	//
	// Cardinality: ONLY label is `result` ∈ {success, fail, skip} — 3 series
	// total. NO tenant label (would be unbounded with N tenants); per EC-3
	// of the edge-case review.
	//
	// Use:
	//   - "success" — ExternalSecret CR created or updated successfully
	//   - "fail" — SecretK8sManager.CreateSecret returned an error. ANY of its
	//     error exits, not only the runtime client's (RBAC, network, etc.):
	//     also the mgmt-cluster Secret write and the namespace derivation.
	//     B-177 widened this from "runtime client returned error", which had
	//     been the description AND the implementation.
	//   - "skip" — manager nil at boot (CELL_0_KUBECONFIG missing) so the
	//     entire path was bypassed. THIS IS THE INVISIBLE-FAILURE SIGNAL
	//     that F-2 was originally caused by.
	//
	// The invariant behind those three, and the reason B-176 and B-177 both
	// exist: `POST /secrets` persists the row BEFORE any of this runs, so
	// every way out of the sync path must move exactly one series. An exit
	// that counts nothing leaves a row in `secrets` while
	// sum(loopzero_secret_externalsecret_total) reads zero — and that sum is what
	// the B-126 canary deferral
	// (docs/decisions.md § 2)
	// uses as its trigger. Adding an early return here without an increment
	// re-opens that hole. TestCreateSecret_EveryExitMovesTheCounter
	// (api/internal/k8s/secretsync) is the guard.
	//
	// Alert opportunity: rate(loopzero_secret_externalsecret_total{result="skip"}[5m]) > 0
	// indicates the API silently no-ops `loopzero secret set` calls.
	SecretExternalSecretTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_secret_externalsecret_total",
		Help: "ExternalSecret CR create/update outcomes from loopzero-api workload sync path.",
	}, []string{"result"})

	// SecretAuditWriteFailedTotal counts cases where the broker secret-fetch
	// audit write (SecretReadAuditAdapter.WriteAudit) returned error.
	// T2.3 of plan 2026-06-10-j22-layer2-code-review-fixes (closes HIGH #16
	// silent audit error on cross_tenant_attempt): the prior code path did
	// `_ = AuditWriter.WriteAudit(...)` discarding the error on the most
	// security-critical event (EC-07 violation). Without this counter, audit
	// pipeline failures (e.g., audit_log INSERT timeout, advisory lock
	// contention) would mask forensic evidence of cross-tenant attempts.
	//
	// Cardinality: ONLY label is `event_type` ∈ {secret_read_broker,
	// cross_tenant_attempt} — 2 series total. NO tenant label
	// (would be unbounded with N tenants).
	//
	// Alert opportunity:
	//   rate(loopzero_secret_audit_write_failed_total{event_type="cross_tenant_attempt"}[5m]) > 0
	//   indicates the security audit trail is breaking for the very events
	//   it exists to record. Page immediately.
	SecretAuditWriteFailedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_secret_audit_write_failed_total",
		Help: "Broker secret-fetch audit write failures (T2.3 closes HIGH #16).",
	}, []string{"event_type"})

	// SecretDecryptFailedTotal counts failures on the secret READ path.
	//
	// SCOPE — both readers, and the list is the whole list:
	//
	//   - PostgresSecretStore.decryptFromStore
	//     (api/internal/db/store/secret_wrapper.go), the single dispatcher
	//     every decrypt inside the store passes through. Its only non-test
	//     caller is ListForResync, a deploy-time hook.
	//   - rawSecretReader.ReadSecret (api/cmd/setup_secret_fetch.go), the
	//     broker endpoint /internal/v1/secret-fetch/... that External Secrets
	//     Operator calls for EVERY user secret. It queries the secrets table
	//     itself and calls Transit directly — it does NOT go through the store
	//     dispatcher, whatever the InternalSecretReader interface comment used
	//     to claim.
	//
	// The second reader arrived with B-175. Between B-126 and that item this
	// counter covered resync only while reading as though it covered
	// everything, so a revoked key on the path that serves users left the
	// series at zero and an operator would have concluded nothing broke. That
	// is the failure this SCOPE list exists to prevent: when a third reader
	// appears, it is named here or the counter starts lying again.
	//
	// B-126: until this counter existed, a decrypt failure was reported ONLY
	// as a per-request error. A revoked or rotated Transit key
	// (transit/keys/loopzero-tenants-<id>) makes every ciphertext of that tenant
	// undecryptable at once, and per-request errors do not add up into
	// anything: the failure looked like one user's broken secret, not like a
	// key that is gone. Aggregating the same failures turns that into a
	// visible step change.
	//
	// What this counter does NOT do, said plainly: it still needs somebody to
	// read a secret. A tenant whose secrets are not being read stays silent
	// however revoked its key is. Detection that does not wait for a reader is
	// the canary, deferred with a named trigger — see
	// docs/decisions.md.
	//
	// Cardinality: ONLY label is `reason` ∈ {empty_ciphertext,
	// vault_transit_error, aes_legacy_error} — 3 series total. NO tenant
	// label, for the same reason SecretExternalSecretTotal (EC-3) and
	// SecretAuditWriteFailedTotal state above: it would be unbounded with N
	// tenants. The tenant identity survives in the error chain the caller
	// logs, which is where a free-form, high-cardinality value belongs.
	//
	// The three reasons split the two failures whose responses differ
	// (B-126 DoD bullet 2 — restoring a key and restoring a backup are not
	// the same operation):
	//
	//   - "vault_transit_error"  — Transit refused or was unreachable. Key
	//     side. A revoked key lands here; so does an unreachable OpenBao,
	//     because the store talks to Transit through a consumer-defined
	//     interface and deliberately does not import pkg/vault to tell the
	//     typed errors apart. Distinguishing the two is the operator's first
	//     step, from the logged error, not the metric's.
	//   - "aes_legacy_error"     — a pre-T10.3 AES row would not open. Data
	//     side, or a lost/rotated local AES key. Nothing writes this format
	//     any more (B-162), so a non-zero rate here is a legacy row that has
	//     become unreadable.
	//   - "empty_ciphertext"     — the EC-06 NULL/empty row guard fired. Data
	//     side: a row exists whose encrypted_value is empty, which no write
	//     path in the tree can produce.
	//
	// Alert opportunity:
	//   sum(rate(loopzero_secret_decrypt_failed_total{reason="vault_transit_error"}[5m])) > 0
	//   means secrets that were readable are not readable now. Page: the
	//   ciphertext is orphaned until the key comes back. The `sum` is what
	//   makes the query cover both readers without naming either.
	SecretDecryptFailedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_secret_decrypt_failed_total",
		Help: "Secret read-path decrypt failures, by reason (B-126).",
	}, []string{"reason"})

	// WorkqueueTerminalWriteFailTotal counts cases where the workqueue
	// consumer's terminal-state write (Complete or Fail) FAILED — the
	// underlying DB INSERT/UPDATE returned error. Without this metric,
	// such failures are silent: handler.Complete fails → item stays
	// InFlight → re-dequeued → handler runs twice → duplicate side-effects.
	//
	// T1.7 of deep-review-remediation-plan (closes find_workqueue_silent_db_errors HIGH).
	//
	// Label `terminal` ∈ {complete, fail} indicates which path failed:
	//   - "complete" — successful handler followed by FAILED Complete write
	//                  (most dangerous: leads to duplicate handler invocation)
	//   - "fail"     — failed handler followed by FAILED Fail write
	//                  (orphan rows accumulate in workqueue table)
	//
	// Alert opportunity: sum(rate(loopzero_workqueue_terminal_write_fail_total[5m])) > 0
	// indicates the queue is in an at-least-once-becomes-at-most-once degenerate
	// state and operators should investigate immediately.
	WorkqueueTerminalWriteFailTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_workqueue_terminal_write_fail_total",
		Help: "Workqueue consumer terminal-state DB write failures (Complete/Fail).",
	}, []string{"terminal"})

	// CellPinnerCRPatchFailTotal counts CellPinner.Pin operations where the
	// PostgreSQL UPDATE on tenants.cell_id succeeded but the subsequent
	// Tenant CR MergePatch FAILED. Result: DB and CR diverged. The operator
	// reads tenant.Spec.CellID from the CR (not the DB) for new
	// EnvironmentReconciler operations, so until the CR is patched manually
	// the tenant's new environments land on the OLD cell.
	//
	// T1.5 of deep-review-remediation-plan (closes find_arch_005 MEDIUM).
	//
	// Alert opportunity: any non-zero rate over 5min indicates divergence.
	CellPinnerCRPatchFailTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loopzero_cell_pinner_cr_patch_fail_total",
		Help: "CellPinner DB-updated-but-CR-patch-failed events (DB↔CR divergence window).",
	})

	// ReleaseAppAttestationSaveFailTotal counts failures to persist per-app
	// attestation rows during ContainerDelivery.handle. The release proceeds
	// (best-effort), but the cosign + SBOM provenance is missing from the DB
	// for the affected release. Operators need this signal to detect a
	// regression in the attestation pipeline; previously the error was
	// discarded by `_ = saveErr` (find_code_005 / CODE-005 MEDIUM).
	//
	// T6.5 of deep-review-remediation-plan.
	ReleaseAppAttestationSaveFailTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_release_attestation_save_fail_total",
		Help: "Per-app attestation persistence failures during release container delivery.",
	}, []string{"environment"})

	// AuditLogPrunerDeletedTotal counts the audit_log rows the retention pruner
	// has DELETEd since process start. T7.3 of deep-review-remediation-plan +
	// ADR-0047. Use rate() to derive prunes/sec; sum() over scrape window to
	// see total volume. Healthy state: trend in flat steps every nightly run.
	AuditLogPrunerDeletedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loopzero_audit_log_pruner_deleted_total",
		Help: "audit_log rows deleted by the retention pruner (1-year window per ADR-0047).",
	})

	// AuditLogPrunerFailTotal counts pruner runs that errored during a batch
	// (DB error, missing trigger bypass var, deadlock). Alert opportunity:
	//   increase(loopzero_audit_log_pruner_fail_total[24h]) > 0
	AuditLogPrunerFailTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "loopzero_audit_log_pruner_fail_total",
		Help: "audit_log pruner run failures (per batch).",
	})

	// DomainVerifyRateLimitedTotal counts POST /api/v1/domains/{domain}/verify
	// requests rejected by the per-(tenant, domain) rate limiter wired in
	// m3-verify-rate-limit-middleware (J11D7 LOCKED ADR: 1 request / 30s).
	// Label cardinality bounded by tenant count (acceptable per existing
	// metrics.go conventions; revisit at >10k tenants).
	DomainVerifyRateLimitedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_domain_verify_rate_limited_total",
		Help: "Total POST /verify requests rejected by the per-(tenant, domain) rate limiter.",
	}, []string{"tenant"})

	// DomainZoneCleanupFailuresTotal counts compensating DNS-provider calls
	// that failed while setting up a custom domain: a zone the request created
	// and could not delete (operation="delete_zone") is left with no domain
	// pointing at it; a record it could not delete (operation="delete_record")
	// is a conflicting or duplicate record left in the zone.
	DomainZoneCleanupFailuresTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_domain_zone_cleanup_failures_total",
		Help: "Total compensating DNS-provider deletes that failed during custom domain setup.",
	}, []string{"operation"})
)
