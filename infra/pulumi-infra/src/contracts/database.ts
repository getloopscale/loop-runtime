// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as pulumi from "@pulumi/pulumi";
import { BaseLabels, NodeClass } from "../core/types";

/**
 * Managed database contract.
 *
 * Platform state lives here — not in K8s. Managed databases provide
 * automatic backups, failover, and patching without operator burden.
 *
 * Each provider maps this to its own construct:
 * - DigitalOcean: DatabaseCluster
 * - AWS: RDS
 */

export interface DatabaseClusterSpec {
  readonly name: string;
  readonly region: pulumi.Input<string>;
  readonly engine: "postgresql";
  readonly version: string;
  readonly size: NodeClass;
  readonly nodeCount: number;
  readonly network: { readonly vpcId: pulumi.Input<string> };
  readonly tags: BaseLabels;
  /** K8s cluster IDs allowed to connect to the database (firewall trusted sources). */
  readonly trustedK8sClusters?: pulumi.Input<string>[];
}

export interface DatabaseClusterOutput {
  readonly clusterId: pulumi.Output<string>;
  readonly host: pulumi.Output<string>;
  readonly privateHost: pulumi.Output<string>;
  readonly port: pulumi.Output<number>;
  readonly database: pulumi.Output<string>;
  readonly user: pulumi.Output<string>;
  readonly password: pulumi.Output<string>;
  readonly uri: pulumi.Output<string>;
  // Role-separated users per service
  readonly apiUser?: pulumi.Output<string>;
  readonly apiUserPassword?: pulumi.Output<string>;
  readonly giteaUser?: pulumi.Output<string>;
  readonly giteaUserPassword?: pulumi.Output<string>;
  readonly kratosUser?: pulumi.Output<string>;
  readonly kratosUserPassword?: pulumi.Output<string>;
  readonly hydraUser?: pulumi.Output<string>;
  readonly hydraUserPassword?: pulumi.Output<string>;
}
