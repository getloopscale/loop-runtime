// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as digitalocean from "@pulumi/digitalocean";
import * as pulumi from "@pulumi/pulumi";
import { KubernetesClusterSpec, KubernetesClusterOutput } from "../../contracts/kubernetes";
import { IProviderKubernetes } from "../../contracts/provider";
import { tagsToStringArray } from "../../core/tags";
import { resolveNodeSize } from "./nodes";

export class DigitalOceanKubernetesProvider implements IProviderKubernetes {
  createKubernetesCluster(spec: KubernetesClusterSpec): KubernetesClusterOutput {
    const nodePoolName = `${spec.name}-default`;
    const nodeSize = resolveNodeSize(spec.nodeClass);
    const tags = tagsToStringArray(spec.tags);

    const cluster = new digitalocean.KubernetesCluster(spec.name, {
      name: spec.name,
      region: spec.region,
      version: spec.version ?? "1.32.13-do.2",
      autoUpgrade: true,
      surgeUpgrade: true,
      vpcUuid: spec.network.vpcId,
      tags,
      nodePool: {
        name: nodePoolName,
        size: nodeSize,
        autoScale: spec.autoScale ?? false,
        nodeCount: spec.autoScale ? undefined : spec.minNodes,
        minNodes: spec.autoScale ? spec.minNodes : undefined,
        maxNodes: spec.autoScale ? (spec.maxNodes ?? spec.minNodes) : undefined,
        tags,
      },
    });

    return {
      clusterId: cluster.id,
      clusterName: cluster.name,
      kubeconfig: cluster.kubeConfigs.apply(
        (configs) => configs[0]?.rawConfig ?? "",
      ),
      endpoint: cluster.endpoint,
      nodePoolName: pulumi.output(nodePoolName),
    };
  }
}
