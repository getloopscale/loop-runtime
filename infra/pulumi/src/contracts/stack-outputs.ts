// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

/**
 * Canonical stack output contracts for the 3-project split.
 *
 * This is the SINGLE SOURCE OF TRUTH for output types.
 * All projects import from here — no copies.
 *
 * Field additions are backwards-compatible.
 * Field removals or type changes require coordinated migration.
 *
 * See ADR-001 for boundary rationale.
 */

// ---------------------------------------------------------------------------
// loop-runtime-infra outputs (consumed by loop-runtime-platform + loop-runtime-control-plane + CI)
// ---------------------------------------------------------------------------

export interface InfraStackOutputs {
  // Clusters
  readonly mgmtClusterName: string;
  readonly mgmtClusterEndpoint: string;
  readonly mgmtKubeconfig: string; // pulumi.secret
  readonly buildClusterName: string;
  readonly buildClusterEndpoint: string;
  readonly buildKubeconfig: string; // pulumi.secret
  readonly runtimeClusterName: string;
  readonly runtimeClusterEndpoint: string;
  readonly runtimeKubeconfig: string; // pulumi.secret

  // Database
  readonly dbHost: string;
  readonly dbPort: number;
  readonly dbUri: string; // pulumi.secret
  readonly dbPassword: string; // pulumi.secret

  // Storage
  readonly bucketName: string;
  readonly bucketEndpoint: string;
  readonly buildsBucketName: string;
  readonly logsBucketName: string;
  readonly staticBucketName: string;
  readonly staticBucketEndpoint: string;
  readonly spacesEndpoint: string;
  readonly s3Endpoint: string;

  // Container registry
  readonly registryEndpoint: string;
  readonly registryName: string;
}

// ---------------------------------------------------------------------------
// loop-runtime-platform outputs (consumed by loop-runtime-control-plane)
// ---------------------------------------------------------------------------

export interface PlatformStackOutputs {
  // Ingress
  readonly mgmtIngressEndpoint: string;
  readonly runtimeIngressEndpoint: string;

  // Cross-cluster
  readonly lokiEndpoint: string;
  readonly argoMetricsEndpoint: string;

  // Cloudflare (optional)
  readonly cfTunnelId?: string;
  readonly cfKvNamespaceId?: string;
  readonly cfWorkerName?: string;

  // Readiness
  readonly giteaReady: boolean;
  readonly argoCdReady: boolean;
  readonly prometheusReady: boolean;
  readonly grafanaReady: boolean;

  // DNS
  readonly mgmtDnsConfigured: boolean;
  readonly runtimeDnsConfigured: boolean;
}
