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
    });

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
