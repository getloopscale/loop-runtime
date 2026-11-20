// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Plan v1.2 T3.1 — LoopZeroCFZone ComponentResource.
//
// Wraps `cloudflare.Zone` with LoopZero conventions. Drill convention: zone is
// ephemeral (created + destroyed per drill iteration); accountId from
// secret config.

import * as cloudflare from "@pulumi/cloudflare";
import * as pulumi from "@pulumi/pulumi";

export interface LoopZeroCFZoneArgs {
  /** Domain name (e.g., "drill.getloopscale.com"). */
  domain: pulumi.Input<string>;
  /** Cloudflare account ID (typically secret-sourced). */
  accountId: pulumi.Input<string>;
  /** Zone type (default "full"). */
  type?: pulumi.Input<string>;
}

export class LoopZeroCFZone extends pulumi.ComponentResource {
  public readonly zone: cloudflare.Zone;
  public readonly id: pulumi.Output<string>;
  public readonly nameServers: pulumi.Output<string[]>;

  constructor(
    name: string,
    args: LoopZeroCFZoneArgs,
    opts?: pulumi.ComponentResourceOptions,
  ) {
    super("loopzero:dns:CFZone", name, {}, opts);

    this.zone = new cloudflare.Zone(
      name,
      {
        zone: args.domain,
        accountId: args.accountId,
        type: args.type ?? "full",
      },
      { parent: this },
    );

    this.id = this.zone.id;
    this.nameServers = this.zone.nameServers;

    this.registerOutputs({
      id: this.id,
      nameServers: this.nameServers,
    });
  }
}
