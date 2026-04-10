// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { InfraOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";
import { bootstrapMgmtCluster } from "./mgmt-bootstrap";
import { bootstrapBuildCluster } from "./build-bootstrap";
import { bootstrapRuntimeCluster } from "./runtime-bootstrap";
import { BootstrapOutputs, CloudflareOutputs } from "./bootstrap-shared";

// Re-export all shared types, constants, and utilities so existing consumers
// (expose.ts, index.ts) continue to import from "./bootstrap" unchanged.
export {
  PLATFORM_NAMESPACE,
  HELM_VERSIONS,
  ClusterBootstrapOutputs,
  MgmtBootstrapOutputs,
  BuildBootstrapOutputs,
  BootstrapOutputs,
  CloudflareOutputs,
  createPlatformNamespace,
  installCertManager,
  installTraefik,
  lookupLoadBalancerEndpoint,
  createClusterIssuer,
} from "./bootstrap-shared";

// ---------------------------------------------------------------------------
// Phase 2 — Bootstrap all clusters
// ---------------------------------------------------------------------------

/**
 * Bootstraps all three clusters with role-appropriate services.
 *
 * Management cluster gets:
 *   namespace -> cert-manager -> ArgoCD -> Traefik -> Gitea -> MinIO
 *   -> Prometheus -> Grafana -> LoopZero API -> TenantController -> TLS
 *
 * Build cluster gets:
 *   Argo Workflows -> CWTs -> Loki + Promtail
 *
 * Runtime cluster gets:
 *   namespace -> cert-manager -> Traefik -> ESO
 *
 * ArgoCD runs only on management -- it syncs to runtime cluster via
 * kubeconfig. This means management cluster outage doesn't affect
 * running applications, only new deploys.
 */
export function bootstrapCluster(
  infra: InfraOutputs,
  config: PlatformStackConfig,
  cf?: CloudflareOutputs,
): BootstrapOutputs {
  // Build cluster bootstrapped first — exports lokiEndpoint + argoMetricsEndpoint for mgmt
  const build = bootstrapBuildCluster(infra, config);
  const mgmt = bootstrapMgmtCluster(infra, config, build.lokiEndpoint, build.argoMetricsEndpoint, cf);
  const runtime = bootstrapRuntimeCluster(infra, config);

  return { mgmt, build, runtime };
}
