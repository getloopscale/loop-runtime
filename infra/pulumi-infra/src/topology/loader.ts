// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

/**
 * Topology loader — parses and validates the declarative topology
 * from Pulumi stack config.
 *
 * Config key: `loop-runtime-infra:topology` (YAML object in Pulumi.<stack>.yaml)
 *
 * If the topology key is absent, falls back to legacy singleton config
 * (mgmtNodeClass, runtimeNodeClass, etc.) for backwards compatibility
 * during the migration period.
 */

import * as pulumi from "@pulumi/pulumi";
import { TopologyConfig, ClusterSpec } from "./types";
import { NodeClass } from "../core/types";

export class TopologyValidationError extends Error {
  constructor(message: string) {
    super(`topology validation: ${message}`);
    this.name = "TopologyValidationError";
  }
}

/**
 * Load topology from Pulumi config.
 * Returns validated TopologyConfig.
 * Throws TopologyValidationError on invalid config.
 */
export function loadTopology(config: pulumi.Config): TopologyConfig {
  const raw = config.getObject<any>("topology");

  if (raw) {
    return parseAndValidate(raw);
  }

  // Fallback: construct topology from legacy singleton config fields
  return buildLegacyTopology(config);
}

// ---------------------------------------------------------------------------
// Parser + validator
// ---------------------------------------------------------------------------

function parseAndValidate(raw: any): TopologyConfig {
  if (!raw || typeof raw !== "object") {
    throw new TopologyValidationError("topology must be an object");
  }

  const mgmt = parsePlane(raw.mgmt ?? raw.controlPlane?.mgmt?.clusters, "mgmt");
  const build = parsePlane(raw.build ?? raw.executionPlanes?.build?.clusters, "build");
  const runtime = parsePlane(raw.runtime ?? raw.executionPlanes?.runtime?.clusters, "runtime");

  const topology: TopologyConfig = { mgmt, build, runtime };
  validate(topology);
  return topology;
}

function parsePlane(raw: any, role: "mgmt" | "build" | "runtime"): ClusterSpec[] {
  if (!raw || !Array.isArray(raw)) {
    throw new TopologyValidationError(`${role}: must be an array of cluster specs`);
  }
  if (raw.length === 0) {
    throw new TopologyValidationError(`${role}: must have at least 1 cluster`);
  }

  return raw.map((entry: any, idx: number) => {
    if (!entry.id || typeof entry.id !== "string") {
      throw new TopologyValidationError(`${role}[${idx}]: missing or invalid 'id'`);
    }
    if (!entry.region || typeof entry.region !== "string") {
      throw new TopologyValidationError(`${role}[${idx}] (${entry.id}): missing 'region'`);
    }
    if (!entry.nodeClass || typeof entry.nodeClass !== "string") {
      throw new TopologyValidationError(`${role}[${idx}] (${entry.id}): missing 'nodeClass'`);
    }
    if (typeof entry.minNodes !== "number" || entry.minNodes < 1) {
      throw new TopologyValidationError(`${role}[${idx}] (${entry.id}): 'minNodes' must be >= 1`);
    }

    // Runtime clusters MUST have cellId
    if (role === "runtime" && !entry.cellId) {
      throw new TopologyValidationError(`${role}[${idx}] (${entry.id}): runtime cluster requires 'cellId'`);
    }

    return {
      id: entry.id,
      role,
      cellId: entry.cellId ?? undefined,
      region: entry.region,
      k8sVersion: entry.k8sVersion ?? undefined,
      nodeClass: entry.nodeClass as NodeClass,
      minNodes: entry.minNodes,
      maxNodes: entry.maxNodes ?? undefined,
      buildStrategy: entry.strategy ?? (role === "build" ? "shared" : undefined),
    } satisfies ClusterSpec;
  });
}

function validate(topology: TopologyConfig): void {
  // Check unique IDs within each role (duplicates across roles = shared cluster, allowed)
  for (const [role, clusters] of [["mgmt", topology.mgmt], ["build", topology.build], ["runtime", topology.runtime]] as const) {
    const ids = new Set<string>();
    for (const cluster of clusters) {
      if (ids.has(cluster.id)) {
        throw new TopologyValidationError(`duplicate cluster id within ${role}: '${cluster.id}'`);
      }
      ids.add(cluster.id);
    }
  }

  // Check unique cellIds for runtime clusters
  const cellIds = new Set<string>();
  for (const cluster of topology.runtime) {
    if (cluster.cellId) {
      if (cellIds.has(cluster.cellId)) {
        throw new TopologyValidationError(`duplicate cellId: '${cluster.cellId}' on cluster '${cluster.id}'`);
      }
      cellIds.add(cluster.cellId);
    }
  }
}

// ---------------------------------------------------------------------------
// Legacy fallback — constructs topology from singleton config fields
// ---------------------------------------------------------------------------

function buildLegacyTopology(config: pulumi.Config): TopologyConfig {
  return {
    mgmt: [{
      id: "mgmt-default",
      role: "mgmt",
      region: config.require("region"),
      k8sVersion: config.get("mgmtK8sVersion") ?? undefined,
      nodeClass: config.require("mgmtNodeClass") as NodeClass,
      minNodes: config.requireNumber("mgmtMinNodes"),
      maxNodes: config.getNumber("mgmtMaxNodes") ?? undefined,
    }],
    build: [{
      id: "build-default",
      role: "build",
      region: config.require("region"),
      k8sVersion: config.get("buildK8sVersion") ?? undefined,
      nodeClass: (config.get("buildNodeClass") ?? "small") as NodeClass,
      minNodes: config.getNumber("buildMinNodes") ?? 2,
      maxNodes: config.getNumber("buildMaxNodes") ?? undefined,
      buildStrategy: "shared",
    }],
    runtime: [{
      id: "runtime-default",
      role: "runtime",
      cellId: "cell-0",
      region: config.require("region"),
      k8sVersion: config.get("runtimeK8sVersion") ?? undefined,
      nodeClass: config.require("runtimeNodeClass") as NodeClass,
      minNodes: config.requireNumber("runtimeMinNodes"),
      maxNodes: config.getNumber("runtimeMaxNodes") ?? undefined,
    }],
  };
}
