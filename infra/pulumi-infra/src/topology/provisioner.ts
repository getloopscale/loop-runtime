// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

/**
 * Multi-cluster provisioner — creates clusters from declarative topology.
 *
 * Key design: resources are keyed by `clusterId` (stable identity).
 * Reorder of the topology array does NOT cause replacement.
 * Add/remove of a clusterId creates/destroys only that cluster.
 */

import * as pulumi from "@pulumi/pulumi";
import { CloudProviderFactory } from "../contracts/provider";
import { resourceName } from "../core/naming";
import { standardTags } from "../core/tags";
import { TopologyConfig, ClusterSpec, InternalClusterRegistryEntry } from "./types";
import { Environment } from "../core/types";

export interface ProvisionerConfig {
  readonly project: string;
  readonly environment: Environment;
  readonly factory: CloudProviderFactory;
  readonly vpcId: pulumi.Output<string>;
}

export interface ProvisionedClusters {
  /** All clusters keyed by clusterId */
  readonly registry: Record<string, ProvisionedCluster>;
  /** Convenience: all cluster IDs for DB firewall allowlist */
  readonly allClusterIds: pulumi.Output<string>[];
}

export interface ProvisionedCluster {
  readonly spec: ClusterSpec;
  readonly clusterId: pulumi.Output<string>;
  readonly clusterName: pulumi.Output<string>;
  readonly kubeconfig: pulumi.Output<string>;
  readonly endpoint: pulumi.Output<string>;
}

/**
 * Provision all clusters declared in the topology.
 * Each cluster is created using its `id` as the Pulumi resource name,
 * ensuring stable identity regardless of array order.
 */
export function provisionClusters(
  topology: TopologyConfig,
  cfg: ProvisionerConfig,
): ProvisionedClusters {
  const allSpecs = [...topology.mgmt, ...topology.build, ...topology.runtime];
  const tags = standardTags(cfg.project, cfg.environment);
  const registry: Record<string, ProvisionedCluster> = {};
  const allClusterIds: pulumi.Output<string>[] = [];

  for (const spec of allSpecs) {
    // Deduplicate: if a cluster with this id was already created, reuse it.
    // This supports shared clusters (e.g., mgmt+build on same physical cluster).
    if (registry[spec.id]) {
      continue;
    }

    // Use clusterId as Pulumi resource name — stable identity
    const clusterName = resourceName({
      project: cfg.project,
      env: cfg.environment,
      region: spec.region,
      suffix: spec.id,
    });

    const cluster = cfg.factory.createKubernetesCluster({
      name: clusterName,
      region: spec.region,
      version: spec.k8sVersion,
      nodeClass: spec.nodeClass,
      minNodes: spec.minNodes,
      maxNodes: spec.maxNodes,
      autoScale: spec.maxNodes !== undefined,
      network: { vpcId: cfg.vpcId },
      tags,
    });

    registry[spec.id] = {
      spec,
      clusterId: cluster.clusterId,
      clusterName: cluster.clusterName,
      kubeconfig: cluster.kubeconfig,
      endpoint: cluster.endpoint,
    };

    allClusterIds.push(cluster.clusterId);
  }

  return { registry, allClusterIds };
}

/**
 * Build registry entries for StackReference export.
 * Resolves all Pulumi Outputs into plain values.
 */
export function buildRegistryOutput(
  clusters: ProvisionedClusters,
  environment: string,
): pulumi.Output<Record<string, InternalClusterRegistryEntry>> {
  const entries = Object.entries(clusters.registry);

  const outputPairs = entries.map(([id, cluster]) =>
    pulumi
      .all([cluster.clusterName, cluster.kubeconfig, cluster.endpoint])
      .apply(([name, kc, ep]) => ({
        key: id,
        value: {
          clusterId: id,
          clusterName: name,
          role: cluster.spec.role,
          cellId: cluster.spec.cellId,
          region: cluster.spec.region,
          kubeconfig: kc,
          endpoint: ep,
          labels: {
            "runtime.getloopscale.com/cluster-id": id,
            "runtime.getloopscale.com/role": cluster.spec.role,
            "runtime.getloopscale.com/environment": environment,
            "runtime.getloopscale.com/region": cluster.spec.region,
            ...(cluster.spec.cellId ? { "runtime.getloopscale.com/cell-id": cluster.spec.cellId } : {}),
          },
        } satisfies InternalClusterRegistryEntry,
      })),
  );

  return pulumi.all(outputPairs).apply((pairs) => {
    const result: Record<string, InternalClusterRegistryEntry> = {};
    for (const pair of pairs) {
      result[pair.key] = pair.value;
    }
    return result;
  });
}

// ---------------------------------------------------------------------------
// Convenience helpers — get clusters by role
// ---------------------------------------------------------------------------

/** Get the first (or only) mgmt cluster. Throws if none. */
export function getMgmtCluster(clusters: ProvisionedClusters): ProvisionedCluster {
  const mgmt = Object.values(clusters.registry).find((c) => c.spec.role === "mgmt");
  if (!mgmt) {
    throw new Error("topology: no mgmt cluster in registry");
  }
  return mgmt;
}

/** Get all runtime clusters. */
export function getRuntimeClusters(clusters: ProvisionedClusters): ProvisionedCluster[] {
  return Object.values(clusters.registry).filter((c) => c.spec.role === "runtime");
}

/** Get all build clusters. */
export function getBuildClusters(clusters: ProvisionedClusters): ProvisionedCluster[] {
  return Object.values(clusters.registry).filter((c) => c.spec.role === "build");
}
