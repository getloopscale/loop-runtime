// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as pulumi from "@pulumi/pulumi";
import { resourceName, bucketName } from "../core/naming";
import { standardTags } from "../core/tags";
import { InfraError } from "../core/errors";
import { CloudProvider } from "../core/types";
import { CloudProviderFactory } from "../contracts/provider";
import { DatabaseClusterOutput } from "../contracts/database";
import { DigitalOceanProviderFactory } from "../providers/digitalocean";
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
   * Versioned + protected in prod. Same LOOPRUNTIME_SPACES_KEY/SECRET as other
   * buckets (DO Spaces uses global API keys per account).
   */
  readonly veleroBackupsBucketName: pulumi.Output<string>;
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
 * (../loopruntime-infra-live), not here. Any other name is a configuration error.
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
// Cluster table
// ---------------------------------------------------------------------------

/** The InfraOutputs key each cluster is exported under. */
export type PlatformClusterOutput = "mgmtCluster" | "runtimeCluster" | "runtimeCluster2" | "buildCluster";

export interface PlatformClusterPlan {
  readonly output: PlatformClusterOutput;
  /** Pulumi resource name. Changing it renames — and replaces — a live cluster. */
  readonly name: string;
  readonly version?: string;
  readonly nodeClass: PlatformStackConfig["mgmtNodeClass"];
  readonly minNodes: number;
  readonly maxNodes?: number;
  readonly autoScale: boolean;
}

/**
 * The four clusters of the platform topology, declared once. The ORDER is the
 * order of the database firewall's trusted-cluster rules (mgmt, build, runtime,
 * runtime-2), kept as it was so the refactor changes no rule. This stack is
 * AS-IS legacy retired at AM10: a new cell is not added here (CLAUDE.md —
 * no new Pulumi for target infra).
 */
export function platformClusterPlans(config: PlatformStackConfig, baseName: string): PlatformClusterPlan[] {
  const plan = (
    output: PlatformClusterOutput,
    suffix: string,
    version: string | undefined,
    nodeClass: PlatformClusterPlan["nodeClass"],
    minNodes: number,
    maxNodes: number | undefined,
  ): PlatformClusterPlan => ({
    output,
    name: `${baseName}-${suffix}`,
    version,
    nodeClass,
    minNodes,
    maxNodes,
    autoScale: maxNodes !== undefined,
  });
  return [
    plan("mgmtCluster", "mgmt", config.mgmtK8sVersion, config.mgmtNodeClass, config.mgmtMinNodes, config.mgmtMaxNodes),
    plan("buildCluster", "build", config.buildK8sVersion, config.buildNodeClass, config.buildMinNodes, config.buildMaxNodes),
    plan("runtimeCluster", "runtime", config.runtimeK8sVersion, config.runtimeNodeClass, config.runtimeMinNodes, config.runtimeMaxNodes),
    plan("runtimeCluster2", "runtime-2", config.runtimeK8sVersion, config.runtimeNodeClass, config.runtimeMinNodes, config.runtimeMaxNodes),
  ];
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
    cidr: config.vpcCidr,
    tags,
    importId: orphans?.vpcId,
  });

  // --- Kubernetes clusters (mgmt, runtime, runtime-2, build) ---
  // One declared table (platformClusterPlans) instead of four hand-copied
  // blocks. runtime-2 is the second cell (multi-cell via DynamicRouter +
  // ClusterCredential discovery); build isolates Argo Workflows + BuildKit so
  // build spikes do not starve platform services.
  const isProd = config.environment === "prod";
  const clusters = {} as Record<PlatformClusterOutput, ReturnType<typeof factory.createKubernetesCluster>>;
  for (const plan of platformClusterPlans(config, baseName)) {
    clusters[plan.output] = factory.createKubernetesCluster({
      name: plan.name,
      region: config.region,
      version: plan.version,
      nodeClass: plan.nodeClass,
      minNodes: plan.minNodes,
      maxNodes: plan.maxNodes,
      autoScale: plan.autoScale,
      network: { vpcId: network.vpcId },
      tags,
      protect: isProd,
    });
  }
  const { mgmtCluster, runtimeCluster, runtimeCluster2, buildCluster } = clusters;

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
    // Allow every cluster of the table to connect — critical for pipeline
    // idempotency. Without this, destroy+recreate leaves DB unreachable (no
    // trusted sources). Derived, so a cluster added to the table is trusted.
    trustedK8sClusters: Object.values(clusters).map((c) => c.clusterId),
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

  // --- Container registry (platform images = DOCR) ---
  // DOCR for platform and tenant images (loopruntime-api, loopruntime-tenant-controller, loopruntime-cloud,
  // loopruntime-dashboard, and every tenant build).
  //
  // ADR-0015: Adopt-or-Create policy for live stateful resources. DO allows ONE
  // registry per account, so we adopt whatever name already exists rather than
  // recreating with a project-derived name. `lookupExistingRegistry()` returns
  // the live registry name (e.g. "loopruntime-system") which the provider uses as both
  // the resource name and Pulumi import ID. Falls back to the canonical
  // "loopruntime-system" name for fresh accounts (matches bootstrap-registry.ts:44
  // imagePullSecret name).
  const registry = factory.createContainerRegistry({
    name: orphans?.registryImportId ?? "loopruntime-system",
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
    registryEndpoint,
    registryName,
  };
}
