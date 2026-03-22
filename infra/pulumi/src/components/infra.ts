import * as pulumi from "@pulumi/pulumi";
import * as digitalocean from "@pulumi/digitalocean";
import { resourceName, bucketName } from "../core/naming";
import { standardTags } from "../core/tags";
import { InfraError } from "../core/errors";
import { CloudProvider } from "../core/types";
import { CloudProviderFactory } from "../contracts/provider";
import { ComputeInstanceOutput } from "../contracts/compute";
import { DatabaseClusterOutput } from "../contracts/database";
import { DigitalOceanProviderFactory } from "../providers/digitalocean";
import { AwsProviderFactory } from "../providers/aws";
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
  readonly runtimeCluster: ClusterOutputs;

  // VMs
  readonly runners: ComputeInstanceOutput[];

  // Data
  readonly database: DatabaseClusterOutput;

  // Storage
  readonly bucketName: pulumi.Output<string>;
  readonly bucketEndpoint: pulumi.Output<string>;

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
 */
export function resolveProvider(provider: CloudProvider): CloudProviderFactory {
  switch (provider) {
    case "digitalocean":
      return new DigitalOceanProviderFactory();
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
 *   ├── Management Cluster (ArgoCD, Gitea, API, Traefik, cert-manager)
 *   ├── Runtime Cluster (user applications only)
 *   ├── Runner VMs (CI/CD builds — isolated resource consumption)
 *   ├── Managed PostgreSQL (platform state)
 *   └── Object Storage (artifacts, builds, backups)
 *
 * Gitea runs inside the mgmt cluster as a StatefulSet (Phase 2).
 *
 * Dependency chain:
 *   Network → everything else (all resources inside VPC)
 *   Clusters, VMs, DB, Storage are independent of each other
 */
export function createInfra(config: PlatformStackConfig): InfraOutputs {
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

  // --- Runner VMs ---
  const runners: ComputeInstanceOutput[] = [];
  for (let i = 0; i < config.runnerCount; i++) {
    const runner = factory.createComputeInstance({
      name: `${baseName}-runner-${i}`,
      region: config.region,
      size: config.runnerNodeClass,
      image: config.runnerImage,
      network: { vpcId: network.vpcId },
      sshKeyIds: config.sshKeyIds,
      tags,
    });
    runners.push(runner);
  }

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

  // --- Container registry ---
  // DigitalOcean has one registry per account (global, not per-region).
  // Starter tier is free (500MB) and sufficient for dev.
  // DOKS clusters in the same account can pull images automatically
  // via `doctl kubernetes cluster registry add` (run once post-provision).
  const registry = new digitalocean.ContainerRegistry(`${baseName}-registry`, {
    name: `${config.project}-registry`,
    subscriptionTierSlug: "starter",
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
    runtimeCluster: {
      clusterId: runtimeCluster.clusterId,
      clusterName: runtimeCluster.clusterName,
      kubeconfig: runtimeCluster.kubeconfig,
      endpoint: runtimeCluster.endpoint,
    },
    runners,
    database,
    bucketName: storage.bucketName,
    bucketEndpoint: storage.endpoint,
    registryEndpoint: registry.endpoint,
    registryName: registry.name,
  };
}
