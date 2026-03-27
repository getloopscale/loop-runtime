// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as pulumi from "@pulumi/pulumi";
import * as digitalocean from "@pulumi/digitalocean";
import { resourceName, bucketName } from "../core/naming";
import { standardTags } from "../core/tags";
import { InfraError } from "../core/errors";
import { CloudProvider } from "../core/types";
import { CloudProviderFactory } from "../contracts/provider";
import { DatabaseClusterOutput } from "../contracts/database";
import { DigitalOceanProviderFactory } from "../providers/digitalocean";
import { AwsProviderFactory } from "../providers/aws";
import { CloudflareDnsProvider } from "../providers/cloudflare/dns";
import { PlatformStackConfig } from "../config/stack";

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
  readonly staticBucketName: pulumi.Output<string>;
  readonly staticCdnEndpoint: pulumi.Output<string>;

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
 * When cfZoneId is set, DNS records are created in Cloudflare instead of the
 * cloud provider's DNS. Required when the domain's authoritative nameservers
 * are Cloudflare (common with CF Tunnel, WAF, or CDN).
 */
export function resolveProvider(provider: CloudProvider, config?: PlatformStackConfig): CloudProviderFactory {
  const cfDns = config?.cfZoneId
    ? new CloudflareDnsProvider(config.cfZoneId)
    : undefined;

  switch (provider) {
    case "digitalocean":
      return new DigitalOceanProviderFactory({ dnsOverride: cfDns });
    case "aws":
      return new AwsProviderFactory();
    default:
      throw new InfraError({
        message: `Unsupported provider: ${provider satisfies never}`,
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
export function createInfra(config: PlatformStackConfig): InfraOutputs {
  const factory = resolveProvider(config.provider, config);
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
    name: "loopzero-builds",
    region: config.region,
    versioning: false,
    tags,
  });

  // Static assets bucket — stores built frontend assets uploaded by Argo Workflows.
  // CF Worker (or nginx-s3-gateway fallback) serves these to browsers.
  // Created directly (not via factory) to support import of existing bucket.
  // Use factory.createObjectStorage to match the same region string format
  // as other buckets (avoids enum vs string mismatch triggering replacement).
  const staticBucket = factory.createObjectStorage({
    name: "loopzero-static-assets",
    region: config.region,
    versioning: false,
    tags,
  });

  // Enable CDN on static assets bucket for edge caching.
  // The CDN endpoint (*.cdn.digitaloceanspaces.com) provides DO-managed caching.
  // CF Worker uses the direct endpoint; CDN is an additional optimization layer.
  const staticCdn = new digitalocean.Cdn(`${baseName}-static-cdn`, {
    origin: pulumi.interpolate`${staticBucket.bucketName}.${config.region}.digitaloceanspaces.com`,
    ttl: 3600, // 1 hour edge cache
  });

  // --- Container registry ---
  // DigitalOcean has one registry per account (global, not per-region).
  // Starter tier is free (500MB) and sufficient for dev.
  // DOKS clusters in the same account can pull images automatically
  // via `doctl kubernetes cluster registry add` (run once post-provision).
  const registry = new digitalocean.ContainerRegistry(`${baseName}-registry`, {
    name: `${config.project}-registry`,
    subscriptionTierSlug: "basic", // 5 repos, 5GB — starter only allows 1 repo
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
    staticBucketName: staticBucket.bucketName,
    staticCdnEndpoint: staticCdn.endpoint,
    registryEndpoint: registry.endpoint,
    registryName: registry.name,
  };
}
