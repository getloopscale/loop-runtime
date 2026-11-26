// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// Plan v1.2 T3.1 — LoopRuntimeVpc ComponentResource (incremental Pulumi componentize).
//
// Wraps `digitalocean.Vpc` with LoopRuntime conventions. Replaces inline drill-vpc
// declaration. Pattern: 1 VPC per stack (shared across all clusters per D5).
//
// CIDR convention: 10.20.0.0/16 prod, 10.30.0.0/16 homolog, 10.40.0.0/16 drill —
// disjoint so cross-cluster VPN/peering won't conflict.

import * as digitalocean from "@pulumi/digitalocean";
import * as pulumi from "@pulumi/pulumi";

export interface LoopRuntimeVpcArgs {
  /** VPC name (e.g., "loop-runtime-drill-vpc-sfo3"). */
  vpcName: pulumi.Input<string>;
  /** DO region (e.g., "sfo3"). */
  region: pulumi.Input<string>;
  /** CIDR range (e.g., "10.40.0.0/16"). */
  ipRange: pulumi.Input<string>;
  /** Optional description (operator-facing context). */
  description?: pulumi.Input<string>;
}

export class LoopRuntimeVpc extends pulumi.ComponentResource {
  public readonly vpc: digitalocean.Vpc;
  public readonly id: pulumi.Output<string>;
  public readonly name: pulumi.Output<string>;
  public readonly ipRange: pulumi.Output<string>;

  constructor(
    name: string,
    args: LoopRuntimeVpcArgs,
    opts?: pulumi.ComponentResourceOptions,
  ) {
    super("loop-runtime:network:Vpc", name, {}, opts);

    this.vpc = new digitalocean.Vpc(
      name,
      {
        name: args.vpcName,
        region: args.region,
        ipRange: args.ipRange,
        description: args.description,
      },
      { parent: this },
    );

    this.id = this.vpc.id;
    this.name = this.vpc.name;
    this.ipRange = this.vpc.ipRange;

    this.registerOutputs({
      id: this.id,
      name: this.name,
      ipRange: this.ipRange,
    });
  }
}
