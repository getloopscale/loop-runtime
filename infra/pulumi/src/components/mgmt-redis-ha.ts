// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Sprint 8 v3.2 / CHG-002a / T2.2 — DigitalOcean Managed Valkey for Harbor HA.
//
// ADR-0016 D-6: Harbor with 2+ core/registry replicas requires shared Redis
// (sessions, job queue, registry cache). Internal Redis = SPOF. External
// Redis (Valkey, the maintained Redis fork) = managed by DO with HA option,
// automated backups, private networking. Password is exposed via K8s Secret
// in `loopzero-system` so Harbor's Helm chart can consume via envFrom — never
// literal in values.yaml (Security veto).
//
// Cost: ~$15/mo for db-amd-1vcpu-2gb single-node (dev). Production should
// upgrade to 3-node HA tier (~$45/mo) per Pulumi.prod.yaml override.
//
// Engine: `valkey` — DigitalOcean replaced `redis` engine in 2026 with
// Valkey (BSD-licensed Redis fork after Redis Inc relicensed to SSPL).
// Valkey is wire-protocol compatible with Redis 7.x, no client changes
// needed. Harbor uses gomodule/redigo which speaks RESP — Valkey works.

import * as digitalocean from "@pulumi/digitalocean";
import * as k8s from "@pulumi/kubernetes";
import * as pulumi from "@pulumi/pulumi";
import { InfraOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";

const PLATFORM_NAMESPACE = "loopzero-system";

export interface MgmtRedisHaOutputs {
  readonly host: pulumi.Output<string>;
  readonly port: pulumi.Output<number>;
  readonly password: pulumi.Output<string>;
  readonly secretName: string;
  readonly clusterId: pulumi.Output<string>;
}

/**
 * Provision a DigitalOcean Managed Valkey cluster + a K8s Secret in
 * loopzero-system carrying the credentials for Harbor (and any other consumer
 * needing a managed Redis-compatible endpoint).
 *
 * Idempotent: re-running pulumi up updates the cluster in place; the K8s
 * Secret is overwritten with the latest values via patchForce.
 *
 * @param provider K8s provider for mgmt cluster
 * @param ns       loopzero-system namespace dependency
 * @param infra    Infra outputs (VPC ID, region, trusted clusters)
 * @param config   Stack config (region, environment, protect flag for prod)
 */
export function installMgmtRedisHa(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  config: PlatformStackConfig,
): MgmtRedisHaOutputs {
  const isProd = config.environment === "prod";

  // Single-node for dev; prod overrides to 3-node HA tier via Pulumi config.
  // Use the smallest AMD tier that meets the chart's <60ms p99 requirement.
  const cluster = new digitalocean.DatabaseCluster(
    "loopzero-infra-redis-ha",
    {
      name: `loopzero-infra-${config.environment}-${config.region}-redis`,
      engine: "valkey",
      version: "8",
      // db-amd-1vcpu-2gb is the smallest tier that supports private network.
      // Prod should override to db-amd-2vcpu-4gb (3 nodes) for HA.
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

  // Firewall: only the mgmt + build clusters need Harbor Redis access.
  // Runtime + runtime-2 do NOT pull from Harbor directly (they pull from DOCR).
  // Following same pattern as PG firewall in providers/digitalocean/database.ts.
  const trustedClusters: pulumi.Input<string>[] = [];
  if (infra.mgmtCluster.clusterId) trustedClusters.push(infra.mgmtCluster.clusterId);
  if (infra.buildCluster.clusterId) trustedClusters.push(infra.buildCluster.clusterId);
  new digitalocean.DatabaseFirewall("loopzero-infra-redis-ha-firewall", {
    clusterId: cluster.id,
    rules: trustedClusters.map((id) => ({ type: "k8s", value: id })),
  });

  // K8s Secret in loopzero-system carrying credentials for Helm consumers.
  // Pattern matches CLAUDE.md Rule 6 (no internal terms) — secret keys
  // use Harbor-friendly env var names that the chart consumes via envFrom.
  // Updated on every pulumi up via `patchForce` so password rotations
  // (e.g., DO ops triggered) flow through automatically.
  const secret = new k8s.core.v1.Secret(
    "harbor-redis-credentials",
    {
      metadata: {
        name: "harbor-redis-credentials",
        namespace: PLATFORM_NAMESPACE,
        annotations: { "pulumi.com/patchForce": "true" },
        labels: {
          "app.kubernetes.io/name": "harbor",
          "app.kubernetes.io/component": "redis-credentials",
          "app.kubernetes.io/part-of": "loopzero",
          "app.kubernetes.io/managed-by": "pulumi",
          "loopzero.io/sprint": "8-chg-002a",
        },
      },
      type: "Opaque",
      stringData: {
        // Harbor reads REDIS_HOST/REDIS_PORT/REDIS_PASSWORD via envFrom.
        // The chart constructs the connection URL at runtime.
        REDIS_HOST: cluster.privateHost,
        REDIS_PORT: cluster.port.apply((p) => `${p}`),
        REDIS_PASSWORD: cluster.password,
        // Some consumers (e.g., harbor-core) prefer a single URL.
        // Build with TLS — DO managed Valkey requires TLS on port 25061.
        REDIS_URL: pulumi.interpolate`rediss://default:${cluster.password}@${cluster.privateHost}:${cluster.port}/0`,
      },
    },
    { provider, dependsOn: [ns] },
  );

  return {
    host: cluster.privateHost,
    port: cluster.port,
    password: cluster.password,
    secretName: "harbor-redis-credentials",
    clusterId: cluster.id,
  };
}
