// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as pulumi from "@pulumi/pulumi";
import { resolveProvider } from "./components/infra";
import { resourceName, bucketName as makeBucketName } from "./core/naming";
import { standardTags } from "./core/tags";
import { buildExecutionState } from "./core/execution";
import {
  loadTopology,
  provisionClusters,
  buildRegistryOutput,
  getMgmtCluster,
  getRuntimeClusters,
  createCells,
} from "./topology";

// --- Configuration ---
const cfg = new pulumi.Config();
const project = pulumi.getProject();
import { Environment, CloudProvider } from "./core/types";
const environment = cfg.require("environment") as Environment;
const provider = cfg.require("provider") as CloudProvider;
const region = cfg.require("region");
const domain = cfg.require("domain");
const infraVersion = cfg.get("infraVersion") ?? "v3";
const cfZoneId = cfg.get("cfZoneId") ?? undefined;

// --- Topology (declarative multi-cluster) ---
const topology = loadTopology(cfg);

// --- Cloud provider factory ---
const factory = resolveProvider(provider, {
  project, environment, provider, region, domain,
  dnsZone: cfg.get("dnsZone") ?? domain,
  infraVersion,
  cfZoneId,
} as any);

const tags = standardTags(project, environment);
const baseName = resourceName({ project, env: environment, region });

// --- Network (shared by all clusters) ---
const vpcCidr = cfg.get("vpcCidr") ?? undefined; // Override CIDR to avoid overlap with other environments
const network = factory.createNetwork({
  name: `${baseName}-net`,
  region,
  cidr: vpcCidr,
  tags,
});

// --- Clusters (from topology) ---
const clusters = provisionClusters(topology, {
  project,
  environment: environment as any,
  factory,
  vpcId: network.vpcId,
});

// --- Database ---
const spacesAccessKeyId = cfg.requireSecret("spacesAccessKeyId");
const spacesSecretAccessKey = cfg.requireSecret("spacesSecretAccessKey");

const database = factory.createDatabaseCluster({
  name: `${baseName}-db`,
  region,
  engine: "postgresql",
  version: cfg.get("dbVersion") ?? "16",
  size: (cfg.get("dbNodeClass") ?? "small") as any,
  nodeCount: cfg.getNumber("dbNodeCount") ?? 1,
  network: { vpcId: network.vpcId },
  tags,
  // Dynamic: all clusters get DB access (not just hardcoded 3)
  trustedK8sClusters: clusters.allClusterIds,
});

// --- Storage ---
const storageBucket = factory.createObjectStorage({
  name: makeBucketName({ project, env: environment, region, suffix: "artifacts" }),
  region,
  versioning: true,
  tags,
});

const buildsBucket = factory.createObjectStorage({
  name: makeBucketName({ project, env: environment, region, suffix: "builds" }),
  region,
  versioning: false,
  tags,
});

const logsBucket = factory.createObjectStorage({
  name: makeBucketName({ project, env: environment, region, suffix: "logs" }),
  region,
  versioning: false,
  tags,
});

const staticBucket = factory.createObjectStorage({
  name: makeBucketName({ project, env: environment, region, suffix: "static-assets" }),
  region,
  versioning: false,
  tags,
});

const staticCdn = factory.createCdn({
  origin: staticBucket.endpoint,
  ttl: 3600,
});

const storageEndpoints = factory.getStorageEndpoint({ region });

// --- Container Registry ---
const registry = factory.createContainerRegistry({
  name: `${project}-registry`,
  tier: "professional",
  region,
});

// --- Execution State (dynamic, multi-cluster) ---
const clusterComponents: Record<string, pulumi.Output<boolean>> = {};
for (const [id, cluster] of Object.entries(clusters.registry)) {
  clusterComponents[`cluster:${id}`] = cluster.kubeconfig.apply((k) => !!k);
}
export const execution = buildExecutionState({
  environment,
  provider,
  infraVersion,
  clusterCount: Object.keys(clusters.registry).length,
  components: {
    ...clusterComponents,
    "database": database.host.apply((h) => !!h),
    "storage": storageBucket.bucketName.apply((n) => !!n),
    "registry": registry.endpoint.apply((e) => !!e),
    "network": network.vpcId.apply((id) => !!id),
  },
});

// --- Cluster Registry (the primary multi-cluster contract) ---
export const clusterRegistry = pulumi.secret(
  buildRegistryOutput(clusters, environment),
);

// --- Cells (logical units mapping cellId → runtime cluster) ---
const runtimeClusters = getRuntimeClusters(clusters);
const cells = createCells(runtimeClusters, environment, domain);
export const cellRegistry = pulumi.output(
  Object.fromEntries(
    Object.entries(cells).map(([id, cell]) => [
      id,
      {
        cellId: cell.cellId,
        clusterId: cell.clusterId,
        region: cell.region,
        labels: cell.labels,
      },
    ]),
  ),
);

// --- Legacy-compatible outputs (consumed by CI pipelines) ---
// These will be deprecated once loopzero-platform migrates to clusterRegistry.
const mgmt = getMgmtCluster(clusters);
export const mgmtClusterName = mgmt.clusterName;
export const mgmtClusterEndpoint = mgmt.endpoint;
export const mgmtKubeconfig = pulumi.secret(mgmt.kubeconfig);

// First build + runtime clusters (legacy compatibility)
const buildEntries = Object.values(clusters.registry).filter((c) => c.spec.role === "build");
const runtimeEntries = Object.values(clusters.registry).filter((c) => c.spec.role === "runtime");

export const buildClusterName = buildEntries[0]?.clusterName ?? pulumi.output("none");
export const buildClusterEndpoint = buildEntries[0]?.endpoint ?? pulumi.output("none");
export const buildKubeconfig = pulumi.secret(buildEntries[0]?.kubeconfig ?? pulumi.output("none"));

export const runtimeClusterName = runtimeEntries[0]?.clusterName ?? pulumi.output("none");
export const runtimeClusterEndpoint = runtimeEntries[0]?.endpoint ?? pulumi.output("none");
export const runtimeKubeconfig = pulumi.secret(runtimeEntries[0]?.kubeconfig ?? pulumi.output("none"));

// Database
export const dbHost = database.privateHost;
export const dbPort = database.port;
export const dbUri = pulumi.secret(database.uri);
export const dbPassword = pulumi.secret(database.password);

// Storage
export const bucketName = storageBucket.bucketName;
export const bucketEndpoint = storageBucket.endpoint;
export const buildsBucketName = buildsBucket.bucketName;
export const logsBucketName = logsBucket.bucketName;
export const staticBucketName = staticBucket.bucketName;
export const staticBucketEndpoint = staticBucket.endpoint;
export const spacesEndpoint = storageEndpoints.endpoint;
export const s3Endpoint = storageEndpoints.s3Endpoint;

// Container Registry
export const registryEndpoint = registry.endpoint;
export const registryName = registry.name;
