// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Plan v1.2 T3.1 — LoopZeroManagedDb ComponentResource.
//
// Wraps `digitalocean.DatabaseCluster` with LoopZero conventions. Supports both
// PostgreSQL and Valkey engines (per ADR D4: drill = ephemeral managed DBs).
//
// Per D4 ADR sizing:
//   PostgreSQL: db-amd-2vcpu-4gb mgmt-tier (Bug #26: AMD variant, sfo3 capacity)
//   Valkey:     db-amd-1vcpu-2gb non-prod tier
//
// Per-app databases + users are created as separate resources downstream of
// this component (LoopZeroManagedDb exposes .id for clusterId reference).

import * as digitalocean from "@pulumi/digitalocean";
import * as pulumi from "@pulumi/pulumi";

export interface LoopZeroManagedDbArgs {
  /** Cluster name (e.g., "loopzero-drill-pg" or "loopzero-drill-redis"). */
  clusterName: pulumi.Input<string>;
  /** Engine ("pg" | "valkey" | "mysql" | "mongodb"). */
  engine: pulumi.Input<string>;
  /** Engine version string (e.g., "16" for PG, "8" for Valkey). */
  version: pulumi.Input<string>;
  /** DO managed DB size slug (e.g., "db-amd-2vcpu-4gb"). */
  size: pulumi.Input<string>;
  /** DO region. */
  region: pulumi.Input<string>;
  /** Node count (1 for non-HA drill, 3+ for prod HA). */
  nodeCount: pulumi.Input<number>;
  /** VPC UUID (private network attachment for security). */
  privateNetworkUuid: pulumi.Input<string>;
  /** Tags array (cost tracking + drill-isolation per D7). */
  tags?: pulumi.Input<pulumi.Input<string>[]>;
}

export class LoopZeroManagedDb extends pulumi.ComponentResource {
  public readonly cluster: digitalocean.DatabaseCluster;
  public readonly id: pulumi.Output<string>;
  public readonly host: pulumi.Output<string>;
  public readonly privateHost: pulumi.Output<string>;
  public readonly port: pulumi.Output<number>;
  public readonly user: pulumi.Output<string>;
  public readonly password: pulumi.Output<string>;
  public readonly database: pulumi.Output<string>;
  public readonly uri: pulumi.Output<string>;
  public readonly privateUri: pulumi.Output<string>;

  constructor(
    name: string,
    args: LoopZeroManagedDbArgs,
    opts?: pulumi.ComponentResourceOptions,
  ) {
    super("loopzero:db:Managed", name, {}, opts);

    this.cluster = new digitalocean.DatabaseCluster(
      name,
      {
        name: args.clusterName,
        engine: args.engine,
        version: args.version,
        size: args.size,
        region: args.region,
        nodeCount: args.nodeCount,
        privateNetworkUuid: args.privateNetworkUuid,
        tags: args.tags,
      },
      { parent: this },
    );

    this.id = this.cluster.id;
    this.host = this.cluster.host;
    this.privateHost = this.cluster.privateHost;
    this.port = this.cluster.port;
    this.user = this.cluster.user;
    this.password = this.cluster.password;
    this.database = this.cluster.database;
    this.uri = this.cluster.uri;
    this.privateUri = this.cluster.privateUri;

    this.registerOutputs({
      id: this.id,
      host: this.host,
      privateHost: this.privateHost,
      port: this.port,
      user: this.user,
      database: this.database,
    });
  }
}
