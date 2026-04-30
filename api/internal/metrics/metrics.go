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

	DomainTLSExpiryDays = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loopzero_domain_tls_expiry_days",
		Help: "Days until TLS certificate expires for a custom domain.",
	}, []string{"domain", "tenant"})

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
)
