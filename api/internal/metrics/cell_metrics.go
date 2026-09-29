// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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

	// EdgeRouteLookups counts the edge's host -> cell lookups by outcome
	// (routed|not_found|unavailable|unauthorized|bad_request). A rise in
	// "unavailable" means the edge is refusing live traffic.
	EdgeRouteLookups = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "loopzero_edge_route_lookups_total",
		Help: "Edge host-to-cell lookups served by /internal/edge/route, labelled by outcome.",
	}, []string{"outcome"})

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
	// the canonical project. C20 + monitors the re-introduction of a
	// parallel-project stack (any stack outside the canonical name set →
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
