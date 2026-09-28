// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as pulumi from "@pulumi/pulumi";
import { resourceName, bucketName } from "../core/naming";
import { standardTags } from "../core/tags";
import { InfraError } from "../core/errors";
import { CloudProvider } from "../core/types";
import { CloudProviderFactory } from "../contracts/provider";
import { DatabaseClusterOutput } from "../contracts/database";
import { DigitalOceanProviderFactory } from "../providers/digitalocean";
import { InfraStackConfig } from "../config/stack";

// ---------------------------------------------------------------------------
// Output types — contracts between Phase 1 and subsequent phases
// ---------------------------------------------------------------------------

export interface ClusterOutputs {
  readonly clusterId: pulumi.Output<string>;
  readonly clusterName: pulumi.Output<string>;
  readonly kubeconfig: pulumi.Output<string>;
  readonly endpoint: pulumi.Output<string>;
}

export interface InfraOutputs {
  // Network
  readonly networkId: pulumi.Output<string>;
  readonly vpcId: pulumi.Output<string>;

  // Clusters
  readonly mgmtCluster: ClusterOutputs;
  readonly buildCluster: ClusterOutputs;
  readonly runtimeCluster: ClusterOutputs;

  // Data
  readonly database: DatabaseClusterOutput;

  // Storage
  readonly bucketName: pulumi.Output<string>;
  readonly bucketEndpoint: pulumi.Output<string>;
  readonly buildsBucketName: pulumi.Output<string>;
  readonly logsBucketName: pulumi.Output<string>;
  readonly staticBucketName: pulumi.Output<string>;
  readonly staticBucketEndpoint: pulumi.Output<string>;
  readonly staticCdnEndpoint: pulumi.Output<string>;
  // Provider-agnostic storage endpoints (eliminates hardcoded digitaloceanspaces.com).
  readonly spacesEndpoint: pulumi.Output<string>;
  readonly s3Endpoint: pulumi.Output<string>;

  // Container Registry
  readonly registryEndpoint: pulumi.Output<string>;
  readonly registryName: pulumi.Output<string>;
}

// ---------------------------------------------------------------------------
// Provider resolution
// ---------------------------------------------------------------------------

/**
 * Resolves the concrete provider factory from the config string.
 * This is the ONLY place that maps provider name → implementation.
 *
 * DigitalOcean is the only provider this stack has or will have: it is AS-IS
 * legacy retired at AM10, and the AWS re-platform lives in Terraform
 * (../loopzero-infra-live), not here. Any other name is a configuration error.
 */
export function resolveProvider(provider: CloudProvider): CloudProviderFactory {
  switch (provider) {
    case "digitalocean":
      return new DigitalOceanProviderFactory();
    default:
      throw new InfraError({
        message: `Unsupported provider: ${provider satisfies never} — this legacy stack provisions DigitalOcean only`,
        code: "INVALID_CONFIG",
        context: { provider },
      });
  }
}

// ---------------------------------------------------------------------------
// Phase 1 — Base Infrastructure (full production topology)
// ---------------------------------------------------------------------------

/**
 * Creates the complete infrastructure topology:
 *
 *   VPC (shared network)
 *   ├── Management Cluster (ArgoCD, Gitea, API, Traefik, cert-manager, Prometheus, Grafana, MinIO)
 *   ├── Build Cluster (Argo Workflows + BuildKit — isolated resource consumption for builds)
 *   ├── Runtime Cluster (user applications only, managed by ArgoCD)
 *   ├── Managed PostgreSQL (platform state)
 *   └── Object Storage (artifacts, builds, backups)
 *
 * Dependency chain:
 *   Network → everything else (all resources inside VPC)
 *   Clusters, DB, Storage are independent of each other
 */
export function createInfra(config: InfraStackConfig): InfraOutputs {
  const factory = resolveProvider(config.provider);
  const tags = standardTags(config.project, config.environment);

  const baseName = resourceName({
    project: config.project,
    env: config.environment,
    region: config.region,
  });

  // --- Shared network ---
  const network = factory.createNetwork({
    name: `${baseName}-net`,
    region: config.region,
    tags,
  });

  // --- Management cluster ---
  const mgmtCluster = factory.createKubernetesCluster({
    name: `${baseName}-mgmt`,
    region: config.region,
    version: config.mgmtK8sVersion,
    nodeClass: config.mgmtNodeClass,
    minNodes: config.mgmtMinNodes,
    maxNodes: config.mgmtMaxNodes,
    autoScale: config.mgmtMaxNodes !== undefined,
    network: { vpcId: network.vpcId },
    tags,
  });

  // --- Runtime cluster ---
  const runtimeCluster = factory.createKubernetesCluster({
    name: `${baseName}-runtime`,
    region: config.region,
    version: config.runtimeK8sVersion,
    nodeClass: config.runtimeNodeClass,
    minNodes: config.runtimeMinNodes,
    maxNodes: config.runtimeMaxNodes,
    autoScale: config.runtimeMaxNodes !== undefined,
    network: { vpcId: network.vpcId },
    tags,
  });

  // --- Build cluster ---
  // Isolated compute for Argo Workflows + BuildKit builds.
  // Separate from mgmt to prevent build resource spikes from affecting platform services.
  const buildCluster = factory.createKubernetesCluster({
    name: `${baseName}-build`,
    region: config.region,
    version: config.buildK8sVersion,
    nodeClass: config.buildNodeClass,
    minNodes: config.buildMinNodes,
    maxNodes: config.buildMaxNodes,
    autoScale: config.buildMaxNodes !== undefined,
    network: { vpcId: network.vpcId },
    tags,
  });

  // --- Managed PostgreSQL ---
  const database = factory.createDatabaseCluster({
    name: `${baseName}-db`,
    region: config.region,
    engine: "postgresql",
    version: config.dbVersion,
    size: config.dbNodeClass,
    nodeCount: config.dbNodeCount,
    network: { vpcId: network.vpcId },
    tags,
    // Allow all 3 DOKS clusters to connect — critical for pipeline idempotency.
    // Without this, destroy+recreate leaves DB unreachable (no trusted sources).
    trustedK8sClusters: [mgmtCluster.clusterId, buildCluster.clusterId, runtimeCluster.clusterId],
  });

  // --- Object storage ---
  const storage = factory.createObjectStorage({
    name: bucketName({
      project: config.project,
      env: config.environment,
      region: config.region,
      suffix: "artifacts",
    }),
    region: config.region,
    versioning: true,
    tags,
  });

  // Builds bucket — API uploads source bundles here (S3-compatible).
  // CWTs on the build cluster download from this bucket during clone step.
  const buildsBucket = factory.createObjectStorage({
    name: bucketName({ project: config.project, env: config.environment, region: config.region, suffix: "builds" }),
    region: config.region,
    versioning: false,
  // One 500 MB bundle per commit on the only store that grows with customer
  // activity. 30 days matches the in-cluster MinIO ILM rule so the two agree
  // (SD-15.8, audit 2026-09-07).
  expireAfterDays: 30,
    tags,
  });

  // Loki logs bucket — stores build logs from Argo Workflows via Loki S3 backend.
  // Without this bucket, Loki pod crashes on startup with "bucket not found".
  const logsBucket = factory.createObjectStorage({
    name: bucketName({ project: config.project, env: config.environment, region: config.region, suffix: "logs" }),
    region: config.region,
    versioning: false,
    tags,
  });

  // Static assets bucket — stores built frontend assets uploaded by Argo Workflows.
  // CF Worker (or nginx-s3-gateway fallback) serves these to browsers.
  const staticBucket = factory.createObjectStorage({
    name: bucketName({ project: config.project, env: config.environment, region: config.region, suffix: "static-assets" }),
    region: config.region,
    versioning: false,
    tags,
  });

  // Enable CDN on static assets bucket for edge caching.
  // CF Worker uses the direct endpoint; CDN is an additional optimization layer.
  const staticCdn = factory.createCdn({
    origin: staticBucket.endpoint,
    ttl: 3600, // 1 hour edge cache
  });

  // Storage endpoint — provider-specific S3-compatible endpoint for SDKs and Loki.
  const storageEndpoints = factory.getStorageEndpoint({ region: config.region });

  // --- Container registry ---
  // REVIEW-2026-03-30: Fixes MED-01 — now uses factory instead of direct DO import.
  // DigitalOcean has one registry per account (global, not per-region).
  // Professional tier: unlimited repos, 100GB storage.
  const registry = factory.createContainerRegistry({
    name: `${config.project}-registry`,
    tier: "professional",
    region: config.region,
  });

  return {
    networkId: network.networkId,
    vpcId: network.vpcId,
    mgmtCluster: {
      clusterId: mgmtCluster.clusterId,
      clusterName: mgmtCluster.clusterName,
      kubeconfig: mgmtCluster.kubeconfig,
      endpoint: mgmtCluster.endpoint,
    },
    buildCluster: {
      clusterId: buildCluster.clusterId,
      clusterName: buildCluster.clusterName,
      kubeconfig: buildCluster.kubeconfig,
      endpoint: buildCluster.endpoint,
    },
    runtimeCluster: {
      clusterId: runtimeCluster.clusterId,
      clusterName: runtimeCluster.clusterName,
      kubeconfig: runtimeCluster.kubeconfig,
      endpoint: runtimeCluster.endpoint,
    },
    database,
    bucketName: storage.bucketName,
    bucketEndpoint: storage.endpoint,
    buildsBucketName: buildsBucket.bucketName,
    logsBucketName: logsBucket.bucketName,
    staticBucketName: staticBucket.bucketName,
    staticBucketEndpoint: staticBucket.endpoint,
    staticCdnEndpoint: staticCdn.cdnEndpoint,
    spacesEndpoint: storageEndpoints.endpoint,
    s3Endpoint: storageEndpoints.s3Endpoint,
    registryEndpoint: registry.endpoint,
    registryName: registry.name,
  };
}
