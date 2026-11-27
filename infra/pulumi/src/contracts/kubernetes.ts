// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as pulumi from "@pulumi/pulumi";
import { BaseLabels, NodeClass } from "../core/types";

/**
 * Network dependency is REQUIRED — a cluster must always live inside a VPC.
 * Each provider maps this to its own construct (DO: vpcUuid, AWS: subnetIds).
 */
export interface ClusterNetworkRef {
  readonly vpcId: pulumi.Input<string>;
}

export interface KubernetesClusterSpec {
  readonly name: string;
  readonly region: pulumi.Input<string>;
  readonly version?: string;
  readonly nodeClass: NodeClass;
  readonly minNodes: number;
  readonly maxNodes?: number;
  readonly autoScale?: boolean;
  readonly network: ClusterNetworkRef;
  readonly tags: BaseLabels;
  /** Enable protect (recommended for prod, disabled for dev/ephemeral). */
  readonly protect?: boolean;
}

export interface KubernetesClusterOutput {
  readonly clusterId: pulumi.Output<string>;
  readonly clusterName: pulumi.Output<string>;
  readonly kubeconfig: pulumi.Output<string>;
  readonly endpoint: pulumi.Output<string>;
  readonly nodePoolName: pulumi.Output<string>;
}
