// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// Plan v1.2 T3.1 — LoopRuntimeDOProject ComponentResource.
//
// Wraps `digitalocean.Project` with LoopRuntime conventions. Project provides cost
// tracking + token scope isolation per ADR D7 (drill isolated from prod via
// DO project + tag filtering, since DO API doesn't support per-token project
// scoping).

import * as digitalocean from "@pulumi/digitalocean";
import * as pulumi from "@pulumi/pulumi";

export interface LoopRuntimeDOProjectArgs {
  /** Project name (e.g., "loopruntime-drill"). */
  projectName: pulumi.Input<string>;
  /** Description string (typically includes expiration metadata). */
  description: pulumi.Input<string>;
  /** Project purpose (default "Service or API"). */
  purpose?: pulumi.Input<string>;
  /** Environment label (default "Staging" for drill). */
  environment?: pulumi.Input<string>;
}

export class LoopRuntimeDOProject extends pulumi.ComponentResource {
  public readonly project: digitalocean.Project;
  public readonly id: pulumi.Output<string>;

  constructor(
    name: string,
    args: LoopRuntimeDOProjectArgs,
    opts?: pulumi.ComponentResourceOptions,
  ) {
    super("loopruntime:cloud:DOProject", name, {}, opts);

    this.project = new digitalocean.Project(
      name,
      {
        name: args.projectName,
        description: args.description,
        purpose: args.purpose ?? "Service or API",
        environment: args.environment ?? "Staging",
        resources: [],
      },
      { parent: this },
    );

    this.id = this.project.id;

    this.registerOutputs({
      id: this.id,
    });
  }
}
