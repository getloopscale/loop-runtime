import * as pulumi from "@pulumi/pulumi";
import { resourceName, bucketName } from "../core/naming";
import { standardTags } from "../core/tags";
import { InfraError } from "../core/errors";
import { CloudProvider } from "../core/types";
import { CloudProviderFactory } from "../contracts/provider";
import { DigitalOceanProviderFactory } from "../providers/digitalocean";
import { AwsProviderFactory } from "../providers/aws";
import { PlatformStackConfig } from "../config/stack";

/**
 * Phase 1 outputs — raw infrastructure.
 * These are consumed by Phase 2 (bootstrap) to install platform services.
 */
export interface InfraOutputs {
  readonly networkId: pulumi.Output<string>;
  readonly vpcId: pulumi.Output<string>;
  readonly clusterName: pulumi.Output<string>;
  readonly kubeconfig: pulumi.Output<string>;
  readonly endpoint: pulumi.Output<string>;
  readonly bucketName: pulumi.Output<string>;
  readonly bucketEndpoint: pulumi.Output<string>;
}

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

/**
 * Phase 1 — Base Infrastructure
 *
 * Creates: VPC, Kubernetes cluster, Object storage.
 * Does NOT install anything on the cluster — that's Phase 2.
 *
 * Dependency chain: Network → Cluster (inside VPC) → Storage (independent)
 */
export function createInfra(config: PlatformStackConfig): InfraOutputs {
  const factory = resolveProvider(config.provider);
  const tags = standardTags(config.project, config.environment);

  const baseName = resourceName({
    project: config.project,
    env: config.environment,
    region: config.region,
  });

  const network = factory.createNetwork({
    name: `${baseName}-net`,
    region: config.region,
    tags,
  });

  const cluster = factory.createKubernetesCluster({
    name: `${baseName}-k8s`,
    region: config.region,
    version: config.k8sVersion,
    nodeClass: config.nodeClass,
    minNodes: config.minNodes,
    maxNodes: config.maxNodes,
    autoScale: config.maxNodes !== undefined,
    network: { vpcId: network.vpcId },
    tags,
  });

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

  return {
    networkId: network.networkId,
    vpcId: network.vpcId,
    clusterName: cluster.clusterName,
    kubeconfig: cluster.kubeconfig,
    endpoint: cluster.endpoint,
    bucketName: storage.bucketName,
    bucketEndpoint: storage.endpoint,
  };
}
