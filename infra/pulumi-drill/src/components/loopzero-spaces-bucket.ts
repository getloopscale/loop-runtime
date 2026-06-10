// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Plan v1.2 T3.1 — LoopZeroSpacesBucket ComponentResource (Pulumi componentize).
//
// Per ADR D1 (drill consolidates to single Pulumi project as stack), extract
// reusable DigitalOcean Spaces bucket pattern from infra/pulumi-drill/src/index.ts
// monolith. First component class — sets the pattern for incremental T3.1
// componentization (LoopZeroVPC, LoopZeroMgmtCluster, LoopZeroManagedPG, etc.).
//
// Replaces 5 inline new SpacesBucket() calls (drill-builds, drill-artifacts,
// drill-static, drill-velero T6.1, future). Each instance = 1 line at call site:
//
//   const builds = new LoopZeroSpacesBucket("builds", {
//     bucketName: `loopzero-drill-builds-${region}`,
//     region,
//     forceDestroy: true,
//   });
//
// Outputs: .name + .endpoint (region-derived). Parent: ComponentResource
// "loopzero:spaces:Bucket" (type token for stack-output URN consistency).

import * as digitalocean from "@pulumi/digitalocean";
import * as pulumi from "@pulumi/pulumi";

export interface LoopZeroSpacesBucketArgs {
  /** Concrete bucket name (e.g., loopzero-drill-builds-sfo3). */
  bucketName: pulumi.Input<string>;
  /** DO Spaces region (e.g., "sfo3"). */
  region: pulumi.Input<string>;
  /** Whether to force-destroy on pulumi destroy (default false, true for drill). */
  forceDestroy?: pulumi.Input<boolean>;
  /** ACL — "private" recommended for backup/sensitive content. Defaults to provider default. */
  acl?: pulumi.Input<string>;
  /** Optional lifecycle rules (e.g., 30-day expiration for backup buckets). */
  lifecycleRules?: pulumi.Input<digitalocean.types.input.SpacesBucketLifecycleRule[]>;
}

/**
 * LoopZeroSpacesBucket — LoopZero-standard DigitalOcean Spaces bucket.
 *
 * Wraps `digitalocean.SpacesBucket` with consistent naming, opt-in lifecycle
 * rules, and LoopZero-conventional outputs (.name + .endpoint).
 */
export class LoopZeroSpacesBucket extends pulumi.ComponentResource {
  public readonly bucket: digitalocean.SpacesBucket;
  public readonly name: pulumi.Output<string>;
  public readonly endpoint: pulumi.Output<string>;

  constructor(
    name: string,
    args: LoopZeroSpacesBucketArgs,
    opts?: pulumi.ComponentResourceOptions,
  ) {
    super("loopzero:spaces:Bucket", name, {}, opts);

    this.bucket = new digitalocean.SpacesBucket(
      name,
      {
        name: args.bucketName,
        region: args.region,
        forceDestroy: args.forceDestroy ?? false,
        acl: args.acl,
        lifecycleRules: args.lifecycleRules,
      },
      { parent: this },
    );

    this.name = this.bucket.name;
    this.endpoint = pulumi.interpolate`https://${args.region}.digitaloceanspaces.com`;

    this.registerOutputs({
      name: this.name,
      endpoint: this.endpoint,
    });
  }
}
