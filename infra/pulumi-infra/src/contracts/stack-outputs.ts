// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

/**
 * Stack output contracts for the 3-project split.
 *
 * These interfaces define the stable API between Pulumi projects.
 * loopruntime-infra exports → loopruntime-platform and loopruntime-control-plane consume.
 * loopruntime-platform exports → loopruntime-control-plane consumes.
 *
 * Field additions are backwards-compatible.
 * Field removals or type changes require coordinated migration.
 *
 * See ADR-001 for boundary rationale.
 */

// ---------------------------------------------------------------------------
// loopruntime-infra outputs (consumed by loopruntime-platform + loopruntime-control-plane)
// ---------------------------------------------------------------------------

export interface InfraStackOutputs {
  // Configuration
  readonly environment: string;
  readonly provider: string;
  readonly region: string;
  readonly domain: string;
  readonly infraVersion: string;

  // Network
  readonly vpcId: string;
  readonly networkId: string;

  // Management cluster
  readonly mgmtClusterName: string;
  readonly mgmtClusterEndpoint: string;
  readonly mgmtKubeconfig: string; // pulumi.secret

  // Build cluster
  readonly buildClusterName: string;
  readonly buildClusterEndpoint: string;
  readonly buildKubeconfig: string; // pulumi.secret

  // Runtime cluster
  readonly runtimeClusterName: string;
  readonly runtimeClusterEndpoint: string;
  readonly runtimeKubeconfig: string; // pulumi.secret

  // Database
  readonly dbHost: string;
  readonly dbPort: number;
  readonly dbUri: string; // pulumi.secret
  readonly dbPassword: string; // pulumi.secret

  // Storage — bucket names (env-scoped via bucketName())
  readonly bucketName: string; // artifacts
  readonly bucketEndpoint: string;
  readonly buildsBucketName: string;
  readonly logsBucketName: string;
  readonly staticBucketName: string;
  readonly staticBucketEndpoint: string;

  // Storage — provider-agnostic endpoints
  readonly spacesEndpoint: string; // https://{region}.digitaloceanspaces.com
  readonly s3Endpoint: string; // https://s3.{region}.digitaloceanspaces.com

  // Container registry
  readonly registryEndpoint: string;
  readonly registryName: string;
}

// ---------------------------------------------------------------------------
// loopruntime-platform outputs (consumed by loopruntime-control-plane)
// ---------------------------------------------------------------------------

export interface PlatformStackOutputs {
  // Ingress — load balancer IPs
  readonly mgmtIngressEndpoint: string;
  readonly runtimeIngressEndpoint: string;

  // Cross-cluster — observability endpoints
  readonly lokiEndpoint: string; // http://{buildNodeIP}:30100
  readonly argoMetricsEndpoint: string; // http://{buildNodeIP}:30090

  // Cloudflare (optional — present only when CF is configured)
  readonly cfTunnelId?: string;
  readonly cfKvNamespaceId?: string;
  readonly cfWorkerName?: string;

  // Readiness signals
  readonly giteaReady: boolean;
  readonly argoCdReady: boolean;
  readonly prometheusReady: boolean;
  readonly grafanaReady: boolean;

  // DNS
  readonly mgmtDnsConfigured: boolean;
  readonly runtimeDnsConfigured: boolean;
}

// ---------------------------------------------------------------------------
// loopruntime-control-plane outputs (operational, not consumed by other projects)
// ---------------------------------------------------------------------------

// NOT REFERENCED (measured 2026-09-07): no module names ControlPlaneStackOutputs.
// It documents the shape a consumer WOULD read from the control-plane stack; the
// consumers that exist read outputs untyped via requireOutput(name).
export interface ControlPlaneStackOutputs {
  // Service readiness
  readonly loopruntimeApiReady: boolean;
  readonly tenantControllerReady: boolean;
  readonly oryKratosReady: boolean;
  readonly oryHydraReady: boolean;

  // Wiring (connection details for operators/debugging)
  readonly wiring: {
    readonly gitea: { readonly internalUrl: string; readonly publicUrl: string };
    readonly grafana: { readonly internalUrl: string; readonly publicUrl: string };
    readonly prometheus: { readonly internalUrl: string };
    readonly registry: { readonly endpoint: string; readonly name: string };
    readonly database: { readonly privateHost: string; readonly port: number; readonly database: string; readonly user: string };
    readonly storage: { readonly bucket: string; readonly endpoint: string };
  };
}
