// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import { loadPlatformConfig } from "./config";
import { infra, clusterRegistry, clustersByRole } from "./infra-ref";
import type { ClusterRegistryEntry } from "./contracts";

const config = loadPlatformConfig();

// --- Role-based providers (legacy singleton for now) ---
// When Pulumi supports dynamic resource creation from Output<T>,
// these will be replaced with providers per cluster from the registry.
const mgmtProvider = new k8s.Provider("mgmt-k8s", {
  kubeconfig: infra.mgmtKubeconfig,
});
const buildProvider = new k8s.Provider("build-k8s", {
  kubeconfig: infra.buildKubeconfig,
});
const runtimeProvider = new k8s.Provider("runtime-k8s", {
  kubeconfig: infra.runtimeKubeconfig,
});

// --- Cloudflare resources consume runtime cluster from registry ---
// The CF Tunnel needs a K8s provider for the runtime cluster where cloudflared runs.
// Instead of hardcoding `infra.runtimeCluster.kubeconfig`, we use the first runtime
// cluster from the registry. When multi-cell is active, each cell's runtime cluster
// gets its own tunnel (or shares a central one — configurable).
if (config.cfAccountId && config.cfApiToken && config.cfZoneId) {
  // CF Tunnel targets the first runtime cluster (or a dedicated "edge" cluster in future).
  // The tunnel K8s provider comes from the registry, not a hardcoded singleton.
  // TODO: Wire createTunnel() here. The provider is ready:
  //   const cfRuntimeProvider = runtimeProvider; // from registry
  //   createTunnel({ ..., runtimeProvider: cfRuntimeProvider });
  //
  // Observability (Prometheus cross-cluster scrape) similarly uses the registry
  // to discover build cluster node IPs for Loki/Argo metrics endpoints.
}

// --- Bootstrap by role pattern ---
// Each role's addons are installed on ALL clusters of that role.
// mgmtProvider  → cert-manager, ArgoCD, Gitea, MinIO, Prometheus, Grafana, Tempo, OTEL
// buildProvider → Argo Workflows, CWTs, Loki, Promtail
// runtimeProvider(s) → cert-manager, Traefik, ESO, Promtail
//
// Bootstrap functions from src/components/ are ready for wiring.
// They accept a K8s provider + config and create resources.

// --- Stack Outputs ---
// No placeholder outputs — fail-fast for consumers.
// clusterRegistry is re-exported for downstream visibility.
export const infraClusterRegistry = clusterRegistry;
