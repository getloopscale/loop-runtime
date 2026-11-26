// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// Plan v1.2 T3.1 — LoopRuntimeDOKSCluster ComponentResource.
//
// Wraps `digitalocean.KubernetesCluster` with LoopRuntime-standard 4-cluster
// topology config (mgmt/build/runtime/runtime-2 per ADR D1+D2).
//
// Sizing per ADR D2:
//   mgmt:    medium (s-4vcpu-8gb-amd × 3 nodes) — control plane + ESO + ArgoCD
//   build:   small  (s-2vcpu-4gb-amd × 2 nodes) — BuildKit + Argo Workflows
//   runtime: small  (s-2vcpu-4gb-amd × 2 nodes) — tenant workloads (cell-0+cell-1)
//   runtime-2: small (mirrors runtime) — cell-1 isolation per D3

import * as digitalocean from "@pulumi/digitalocean";
import * as pulumi from "@pulumi/pulumi";

export interface LoopRuntimeDOKSClusterArgs {
  /** Cluster name (e.g., "loopruntime-drill-core-cluster"). */
  clusterName: pulumi.Input<string>;
  /** DO region. */
  region: pulumi.Input<string>;
  /** K8s version slug (e.g., "1.33.9-do.6"). */
  version: pulumi.Input<string>;
  /** VPC UUID (private network attachment). */
  vpcUuid: pulumi.Input<string>;
  /** Node pool name (e.g., "drill-mgmt-pool"). */
  nodePoolName: pulumi.Input<string>;
  /** Node pool size slug (e.g., "s-4vcpu-8gb-amd"). */
  nodeSize: pulumi.Input<string>;
  /** Node count. */
  nodeCount: pulumi.Input<number>;
  /** Tags array. */
  tags?: pulumi.Input<pulumi.Input<string>[]>;
  /** Auto-upgrade (default false — mirror prod posture). */
  autoUpgrade?: pulumi.Input<boolean>;
}

export class LoopRuntimeDOKSCluster extends pulumi.ComponentResource {
  public readonly cluster: digitalocean.KubernetesCluster;
  public readonly id: pulumi.Output<string>;
  public readonly name: pulumi.Output<string>;
  public readonly endpoint: pulumi.Output<string>;
  public readonly kubeConfigs: pulumi.Output<digitalocean.types.output.KubernetesClusterKubeConfig[]>;
  public readonly status: pulumi.Output<string>;
  public readonly clusterSubnet: pulumi.Output<string>;
  public readonly serviceSubnet: pulumi.Output<string>;

  constructor(
    name: string,
    args: LoopRuntimeDOKSClusterArgs,
    opts?: pulumi.ComponentResourceOptions,
  ) {
    super("loopruntime:k8s:DOKSCluster", name, {}, opts);

    this.cluster = new digitalocean.KubernetesCluster(
      name,
      {
        name: args.clusterName,
        region: args.region,
        version: args.version,
        vpcUuid: args.vpcUuid,
        autoUpgrade: args.autoUpgrade ?? false,
        tags: args.tags,
        nodePool: {
          name: args.nodePoolName,
          size: args.nodeSize,
          nodeCount: args.nodeCount,
          tags: args.tags,
        },
      },
      { parent: this },
    );

    this.id = this.cluster.id;
    this.name = this.cluster.name;
    this.endpoint = this.cluster.endpoint;
    this.kubeConfigs = this.cluster.kubeConfigs;
    this.status = this.cluster.status;
    this.clusterSubnet = this.cluster.clusterSubnet;
    this.serviceSubnet = this.cluster.serviceSubnet;

    this.registerOutputs({
      id: this.id,
      name: this.name,
      endpoint: this.endpoint,
      status: this.status,
    });
  }
}
