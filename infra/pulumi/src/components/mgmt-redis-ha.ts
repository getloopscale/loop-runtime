// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Sprint 8 v3.2 / CHG-002a / T2.2 — DigitalOcean Managed Valkey for Harbor HA.
// CHG-2026-04-28-LAYERED-BOOTSTRAP / T1.1 — refactored to L1-only.
//
// L1 (this file): DO Managed Valkey cluster + DatabaseFirewall.
// L2 (Helmfile + scripts/generate-secrets.sh): K8s Secret `harbor-redis-credentials`
//     in loopzero-system, populated from Pulumi outputs.
//
// ADR-0016 D-6: Harbor with 2+ core/registry replicas requires shared Redis
// (sessions, job queue, registry cache). Internal Redis = SPOF. External
// Redis (Valkey, the maintained Redis fork) = managed by DO with HA option,
// automated backups, private networking.

import * as digitalocean from "@pulumi/digitalocean";
import * as pulumi from "@pulumi/pulumi";
import { InfraOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";

export interface MgmtRedisHaOutputs {
  readonly host: pulumi.Output<string>;
  readonly port: pulumi.Output<number>;
  readonly password: pulumi.Output<string>;
  readonly clusterId: pulumi.Output<string>;
}

/**
 * Provision a DigitalOcean Managed Valkey cluster (L1 only — zero K8s).
 * The K8s Secret consumed by Harbor is created at L2 by the Helmfile
 * `scripts/generate-secrets.sh` script using `pulumi stack output`.
 */
export function installMgmtRedisHa(
  infra: InfraOutputs,
  config: PlatformStackConfig,
): MgmtRedisHaOutputs {
  const isProd = config.environment === "prod";

  const cluster = new digitalocean.DatabaseCluster(
    "loopzero-infra-redis-ha",
    {
      name: `loopzero-infra-${config.environment}-${config.region}-redis`,
      engine: "valkey",
      version: "8",
      size: isProd ? "db-amd-2vcpu-4gb" : "db-amd-1vcpu-2gb",
      region: config.region,
      nodeCount: isProd ? 3 : 1,
      privateNetworkUuid: infra.vpcId,
      tags: ["env:" + config.environment, "managed-by:pulumi", "sprint:8-chg-002a"],
    },
    {
      protect: isProd,
      retainOnDelete: isProd,
    },
  );

  const trustedClusters: pulumi.Input<string>[] = [];
  if (infra.mgmtCluster.clusterId) trustedClusters.push(infra.mgmtCluster.clusterId);
  if (infra.buildCluster.clusterId) trustedClusters.push(infra.buildCluster.clusterId);
  new digitalocean.DatabaseFirewall("loopzero-infra-redis-ha-firewall", {
    clusterId: cluster.id,
    rules: trustedClusters.map((id) => ({ type: "k8s", value: id })),
  });

  return {
    host: cluster.privateHost,
    port: cluster.port,
    password: cluster.password,
    clusterId: cluster.id,
  };
}
