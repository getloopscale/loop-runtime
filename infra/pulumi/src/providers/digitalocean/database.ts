// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as digitalocean from "@pulumi/digitalocean";
import { IProviderDatabase } from "../../contracts/provider";
import { DatabaseClusterSpec, DatabaseClusterOutput } from "../../contracts/database";
import { resolveDbSize } from "./nodes";
import { tagsToStringArray } from "../../core/tags";

/**
 * DigitalOcean managed database provider.
 *
 * Creates a managed PostgreSQL cluster inside the VPC.
 * Platform state (jobs, installs, metadata) lives here — not in K8s.
 *
 * DigitalOcean managed databases provide:
 * - Automatic daily backups (7-day retention)
 * - Automatic failover (HA plans)
 * - Automatic maintenance windows
 * - Private networking via VPC
 */
export class DigitalOceanDatabaseProvider implements IProviderDatabase {
  createDatabaseCluster(spec: DatabaseClusterSpec): DatabaseClusterOutput {
    const cluster = new digitalocean.DatabaseCluster(spec.name, {
      name: spec.name,
      engine: "pg",
      version: spec.version,
      size: resolveDbSize(spec.size),
      region: spec.region,
      nodeCount: spec.nodeCount,
      privateNetworkUuid: spec.network.vpcId,
      tags: tagsToStringArray(spec.tags),
    }, {
      // Protect prod databases from accidental deletion. Dev/ephemeral
      // environments use protect=false to enable clean destroy+up cycles.
      protect: spec.protect ?? false,
      retainOnDelete: spec.protect ?? false,
    });

    // Managed PostgreSQL only creates "defaultdb". Platform services need their own databases.
    new digitalocean.DatabaseDb(`${spec.name}-gitea`, {
      clusterId: cluster.id,
      name: "gitea",
    });

    new digitalocean.DatabaseDb(`${spec.name}-loopruntime`, {
      clusterId: cluster.id,
      name: "loopruntime",
    });

    // Ory Kratos (identity management) and Hydra (OAuth2 server) require separate databases.
    // Each manages its own migration history and expects full ownership of the database.
    new digitalocean.DatabaseDb(`${spec.name}-kratos`, {
      clusterId: cluster.id,
      name: "kratos",
    });

    new digitalocean.DatabaseDb(`${spec.name}-hydra`, {
      clusterId: cluster.id,
      name: "hydra",
    });

    // Firewall: allow only specified K8s clusters to connect.
    // Without this, the DB is unreachable from all pods (connection timeout on port 25060).
    // This is critical for idempotency — destroy+recreate must re-add firewall rules.
    if (spec.trustedK8sClusters && spec.trustedK8sClusters.length > 0) {
      new digitalocean.DatabaseFirewall(`${spec.name}-firewall`, {
        clusterId: cluster.id,
        rules: spec.trustedK8sClusters.map((id, i) => ({
          type: "k8s",
          value: id,
        })),
      });
    }

    return {
      clusterId: cluster.id,
      host: cluster.host,
      privateHost: cluster.privateHost,
      port: cluster.port,
      database: cluster.database,
      user: cluster.user,
      password: cluster.password,
      uri: cluster.uri,
    };
  }
}
