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
import { AwsProviderFactory } from "../providers/aws";
import { HarborRegistryProvider } from "../providers/harbor";
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
  readonly runtimeCluster2: ClusterOutputs;

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
  /**
   * Bucket Velero stores K8s resource backups in. velero-backup-dr-plan T0.1.
   * Versioned + protected in prod. Same LOOPZERO_SPACES_KEY/SECRET as other
   * buckets (DO Spaces uses global API keys per account).
   */
  readonly veleroBackupsBucketName: pulumi.Output<string>;
  // Provider-agnostic storage endpoints (eliminates hardcoded digitaloceanspaces.com).
  readonly spacesEndpoint: pulumi.Output<string>;
  readonly s3Endpoint: pulumi.Output<string>;

  // Container images S3 backend — used by Harbor for blob/manifest storage.
  // Only created when registryType is "harbor".
  readonly containerImagesBucketName?: pulumi.Output<string>;

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
export function createInfra(config: PlatformStackConfig, orphans?: import("../core/adopt-or-create").OrphanLookupResult): InfraOutputs {
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
    cidr: config.vpcCidr,
    tags,
    importId: orphans?.vpcId,
  });

  // --- Management cluster ---
  const isProd = config.environment === "prod";
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
    protect: isProd,
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
    protect: isProd,
  });

  // --- Runtime cluster 2 (multi-cell) ---
  // Second runtime cluster for multi-cell validation. Proves horizontal scaling
  // via DynamicRouter + ClusterCredential discovery. Each cell operates independently
  // with its own ArgoCD apps, namespaces, and ingress.
  const runtimeCluster2 = factory.createKubernetesCluster({
    name: `${baseName}-runtime-2`,
    region: config.region,
    version: config.runtimeK8sVersion,
    nodeClass: config.runtimeNodeClass,
    minNodes: config.runtimeMinNodes,
    maxNodes: config.runtimeMaxNodes,
    autoScale: config.runtimeMaxNodes !== undefined,
    network: { vpcId: network.vpcId },
    tags,
    protect: isProd,
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
    protect: isProd,
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
    trustedK8sClusters: [mgmtCluster.clusterId, buildCluster.clusterId, runtimeCluster.clusterId, runtimeCluster2.clusterId],
    // Protect databases in all environments — accidental deletion loses all tenant data.
    // Use `pulumi state unprotect` + `pulumi destroy --target` for intentional deletion.
    protect: true,
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

  // Velero backups bucket — Velero server stores K8s resource backups here
  // (CRDs, ArgoCD state, Gitea metadata, Secrets, full snapshots).
  // velero-backup-dr-plan v1.0 T0.1: versioning=true protects against
  // accidental delete of backup objects. protect=true in prod prevents
  // `pulumi destroy` from wiping the backup bucket itself. forceDestroy=
  // false in prod so a non-empty backup bucket cannot be dropped without
  // explicit pulumi state surgery. (isProd is declared above near the
  // mgmt cluster — reuse it.)
  const veleroBackupsBucket = factory.createObjectStorage({
    name: bucketName({ project: config.project, env: config.environment, region: config.region, suffix: "velero-backups" }),
    region: config.region,
    versioning: true,
    tags,
    protect: isProd,
    forceDestroy: !isProd,
  });

  // Enable CDN on static assets bucket for edge caching.
  // CF Worker uses the direct endpoint; CDN is an additional optimization layer.
  const staticBucketName = bucketName({ project: config.project, env: config.environment, region: config.region, suffix: "static-assets" });
  const staticCdn = factory.createCdn({
    origin: staticBucket.endpoint,
    ttl: 3600, // 1 hour edge cache
    bucketName: staticBucketName, // Plain string for CDN adopt-or-create lookup
  });

  // Storage endpoint — provider-specific S3-compatible endpoint for SDKs and Loki.
  const storageEndpoints = factory.getStorageEndpoint({ region: config.region });

  // --- Container images bucket (Harbor S3 backend) ---
  // Created when harborExternalUrl is set. Harbor stores tenant image blobs
  // and manifests here via S3-compatible API (DO Spaces).
  // DOCR remains the registry for platform images.
  let containerImagesBucketName: pulumi.Output<string> | undefined;
  if (config.harborExternalUrl) {
    const containerImagesBucket = factory.createObjectStorage({
      name: bucketName({ project: config.project, env: config.environment, region: config.region, suffix: "container-images" }),
      region: config.region,
      versioning: false,
      tags,
    });
    containerImagesBucketName = containerImagesBucket.bucketName;
  }

  // --- Container registry (platform images = DOCR) ---
  // DOCR for platform images (loopzero-api, loopzero-tenant-controller, loopzero-cloud, loopzero-dashboard)
  // Harbor for tenant images — deployed separately in bootstrap phase.
  //
  // ADR-0015: Adopt-or-Create policy for live stateful resources. DO allows ONE
  // registry per account, so we adopt whatever name already exists rather than
  // recreating with a project-derived name. `lookupExistingRegistry()` returns
  // the live registry name (e.g. "loopzero-system") which the provider uses as both
  // the resource name and Pulumi import ID. Falls back to the canonical
  // "loopzero-system" name for fresh accounts (matches bootstrap-registry.ts:44
  // imagePullSecret name).
  const registry = factory.createContainerRegistry({
    name: orphans?.registryImportId ?? "loopzero-system",
    tier: "professional",
    region: config.region,
  });
  const registryEndpoint = registry.endpoint;
  const registryName = registry.name;

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
    runtimeCluster2: {
      clusterId: runtimeCluster2.clusterId,
      clusterName: runtimeCluster2.clusterName,
      kubeconfig: runtimeCluster2.kubeconfig,
      endpoint: runtimeCluster2.endpoint,
    },
    database,
    bucketName: storage.bucketName,
    bucketEndpoint: storage.endpoint,
    buildsBucketName: buildsBucket.bucketName,
    logsBucketName: logsBucket.bucketName,
    staticBucketName: staticBucket.bucketName,
    staticBucketEndpoint: staticBucket.endpoint,
    staticCdnEndpoint: staticCdn.cdnEndpoint,
    veleroBackupsBucketName: veleroBackupsBucket.bucketName,
    spacesEndpoint: storageEndpoints.endpoint,
    s3Endpoint: storageEndpoints.s3Endpoint,
    containerImagesBucketName,
    registryEndpoint,
    registryName,
  };
}
