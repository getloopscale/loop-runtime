// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

/**
 * Topology types — declarative multi-cluster configuration.
 *
 * The topology is the source of truth for how many clusters exist,
 * what role each plays, and which cell (if any) a runtime cluster belongs to.
 *
 * Key rules:
 * - `id` is globally unique and immutable (rename = destroy + create)
 * - `cellId` is required for runtime clusters
 * - Resources are keyed by `id`, not array position (stable identity)
 * - Reorder of lists must not cause replacement
 */

import { NodeClass } from "../core/types";

// ---------------------------------------------------------------------------
// Cluster spec — one entry per cluster in the topology
// ---------------------------------------------------------------------------

export interface ClusterSpec {
  /** Unique cluster identifier. Immutable — used as Pulumi resource name. */
  readonly id: string;
  /** Cluster role: what services this cluster runs. */
  readonly role: "mgmt" | "build" | "runtime";
  /** Cell identifier. Required for runtime clusters, optional for build (cell-build mode). */
  readonly cellId?: string;
  /** Cloud provider region (e.g., "sfo3", "nyc3"). */
  readonly region: string;
  /** Kubernetes version override. Uses provider default if omitted. */
  readonly k8sVersion?: string;
  /** Node size class. */
  readonly nodeClass: NodeClass;
  /** Minimum node count. */
  readonly minNodes: number;
  /** Maximum node count (enables autoscaling if set). */
  readonly maxNodes?: number;
  /** Build cluster strategy. Only relevant for role=build. */
  readonly buildStrategy?: "shared" | "dedicated";
}

// ---------------------------------------------------------------------------
// Topology config — loaded from Pulumi stack config
// ---------------------------------------------------------------------------

export interface TopologyConfig {
  /** Management plane clusters (typically 1). */
  readonly mgmt: ClusterSpec[];
  /** Build plane clusters (1+ shared, or N dedicated per cell). */
  readonly build: ClusterSpec[];
  /** Runtime plane clusters (1 per cell). */
  readonly runtime: ClusterSpec[];
}

// ---------------------------------------------------------------------------
// Cluster registry — the output of provisioning
// ---------------------------------------------------------------------------

/**
 * Internal registry entry — used within the Pulumi program.
 * Contains Pulumi Outputs that resolve after apply.
 */
export interface InternalClusterRegistryEntry {
  readonly clusterId: string;
  readonly clusterName: string; // resolved at apply time
  readonly role: "mgmt" | "build" | "runtime";
  readonly cellId?: string;
  readonly region: string;
  readonly kubeconfig: string; // secret, resolved at apply time
  readonly endpoint: string;
  readonly labels: Record<string, string>;
}

/**
 * Serialized registry entry — exported via StackReference.
 * All values are plain strings (no Pulumi Outputs).
 */
export interface ClusterRegistryEntry {
  readonly clusterId: string;
  readonly clusterName: string;
  readonly role: "mgmt" | "build" | "runtime";
  readonly cellId?: string;
  readonly region: string;
  readonly kubeconfig: string; // secret
  readonly endpoint: string;
  readonly labels: Record<string, string>;
}

/**
 * The cluster registry — keyed by clusterId.
 * This is the primary contract between loop-runtime-infra and downstream consumers.
 */
export type ClusterRegistry = Record<string, ClusterRegistryEntry>;
