// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

/**
 * Cell — the logical unit of operation in multi-cell architecture.
 *
 * A Cell associates a `cellId` with a runtime cluster, publishes cell outputs,
 * and registers the cell in the control plane via ClusterCredential CRD metadata.
 *
 * Cells do NOT install bootstrap addons (Traefik, cert-manager, etc.) —
 * that's the responsibility of the role-based bootstrap in loopruntime-platform.
 * Cells add cell-specific metadata, labels, and registrations.
 *
 * Infra-topology-refactor.md Phase 6.
 */

import * as pulumi from "@pulumi/pulumi";
import { ClusterSpec } from "./types";
import { ProvisionedCluster } from "./provisioner";

// ---------------------------------------------------------------------------
// Cell types
// ---------------------------------------------------------------------------

export interface CellConfig {
  /** Unique cell identifier (e.g., "cell-0", "cell-sfo3-02"). */
  readonly cellId: string;
  /** The runtime cluster backing this cell. */
  readonly cluster: ProvisionedCluster;
  /** Environment name for labeling. */
  readonly environment: string;
  /** Base domain for app routing (e.g., "dev.getloopscale.com"). */
  readonly domain: string;
}

export interface CellOutput {
  /** Cell identifier. */
  readonly cellId: string;
  /** Cluster ID of the backing runtime cluster. */
  readonly clusterId: string;
  /** Region where the cell runs. */
  readonly region: string;
  /** Kubeconfig for the runtime cluster (secret). */
  readonly kubeconfig: pulumi.Output<string>;
  /** API server endpoint. */
  readonly endpoint: pulumi.Output<string>;
  /** Cluster name (for registry integration). */
  readonly clusterName: pulumi.Output<string>;
  /** Labels for discovery and routing. */
  readonly labels: Record<string, string>;
}

// ---------------------------------------------------------------------------
// Cell factory
// ---------------------------------------------------------------------------

/**
 * Create a Cell from a runtime cluster.
 *
 * This is the logical composition that:
 * 1. Associates cellId with the runtime cluster
 * 2. Produces cell-scoped outputs for StackReference consumers
 * 3. Generates labels for ClusterCredential registration (done by loopruntime-platform)
 *
 * Bootstrap addons (Traefik, cert-manager, ESO) are NOT installed here —
 * they are installed by the role-based bootstrap pipeline in loopruntime-platform.
 */
export function createCell(cfg: CellConfig): CellOutput {
  const spec = cfg.cluster.spec;

  return {
    cellId: cfg.cellId,
    clusterId: spec.id,
    region: spec.region,
    kubeconfig: cfg.cluster.kubeconfig,
    endpoint: cfg.cluster.endpoint,
    clusterName: cfg.cluster.clusterName,
    labels: {
      "runtime.getloopscale.com/cell-id": cfg.cellId,
      "runtime.getloopscale.com/cluster-id": spec.id,
      "runtime.getloopscale.com/role": "runtime",
      "runtime.getloopscale.com/environment": cfg.environment,
      "runtime.getloopscale.com/region": spec.region,
      "runtime.getloopscale.com/domain": cfg.domain,
    },
  };
}

/**
 * Create all cells from runtime clusters in the topology.
 * Returns a map keyed by cellId.
 */
export function createCells(
  runtimeClusters: ProvisionedCluster[],
  environment: string,
  domain: string,
): Record<string, CellOutput> {
  const cells: Record<string, CellOutput> = {};

  for (const cluster of runtimeClusters) {
    const cellId = cluster.spec.cellId;
    if (!cellId) {
      throw new Error(`runtime cluster ${cluster.spec.id} has no cellId`);
    }

    cells[cellId] = createCell({
      cellId,
      cluster,
      environment,
      domain,
    });
  }

  return cells;
}
