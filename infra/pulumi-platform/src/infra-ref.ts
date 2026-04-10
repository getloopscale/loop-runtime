// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as pulumi from "@pulumi/pulumi";
import type { InfraStackOutputs, ClusterRegistry, ClusterRegistryEntry } from "./contracts";

/**
 * Resolves loopzero-infra StackReference outputs.
 *
 * The reference target is parametrized via config key `infraStackRef`.
 * Default: `loopzero-infra/{currentStack}`.
 * Override per-stack: `pulumi config set infraStackRef org/loopzero-infra/dev`
 */

const cfg = new pulumi.Config();
const infraStackName = cfg.get("infraStackRef") ?? `loopzero-infra/${pulumi.getStack()}`;
const infraRef = new pulumi.StackReference(infraStackName);

function get<K extends keyof InfraStackOutputs>(key: K): pulumi.Output<InfraStackOutputs[K]> {
  return infraRef.getOutput(key) as pulumi.Output<InfraStackOutputs[K]>;
}

function secret<K extends keyof InfraStackOutputs>(key: K): pulumi.Output<InfraStackOutputs[K]> {
  return infraRef.requireOutput(key) as pulumi.Output<InfraStackOutputs[K]>;
}

// --- Multi-cluster: ClusterRegistry ---
// Primary contract — iterate by role to bootstrap all clusters of a kind.
export const clusterRegistry: pulumi.Output<ClusterRegistry> =
  infraRef.requireOutput("clusterRegistry") as pulumi.Output<ClusterRegistry>;

/** Get all clusters of a given role from the registry. */
export function clustersByRole(
  registry: ClusterRegistry,
  role: "mgmt" | "build" | "runtime",
): ClusterRegistryEntry[] {
  return Object.values(registry).filter((c) => c.role === role);
}

// --- Legacy singleton outputs (for gradual migration) ---
export const infra = {
  mgmtKubeconfig: secret("mgmtKubeconfig"),
  buildKubeconfig: secret("buildKubeconfig"),
  runtimeKubeconfig: secret("runtimeKubeconfig"),

  mgmtClusterEndpoint: get("mgmtClusterEndpoint"),
  buildClusterEndpoint: get("buildClusterEndpoint"),
  runtimeClusterEndpoint: get("runtimeClusterEndpoint"),

  dbHost: get("dbHost"),
  dbPort: get("dbPort"),
  dbUri: secret("dbUri"),
  dbPassword: secret("dbPassword"),

  bucketName: get("bucketName"),
  bucketEndpoint: get("bucketEndpoint"),
  buildsBucketName: get("buildsBucketName"),
  logsBucketName: get("logsBucketName"),
  staticBucketName: get("staticBucketName"),
  staticBucketEndpoint: get("staticBucketEndpoint"),
  spacesEndpoint: get("spacesEndpoint"),
  s3Endpoint: get("s3Endpoint"),

  registryEndpoint: get("registryEndpoint"),
  registryName: get("registryName"),
};
