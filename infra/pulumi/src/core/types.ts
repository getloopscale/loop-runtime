// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

/**
 * Central type definitions for the LoopZero infrastructure platform.
 * These types are provider-independent and used across all layers.
 */

export type CloudProvider = "digitalocean";

export type Environment = "dev" | "staging" | "prod";

/**
 * Abstract node size classes. Each provider resolves to a concrete instance type.
 * This prevents provider-specific strings (s-2vcpu-4gb, t3.large) from leaking
 * into contracts and config.
 */
export type NodeClass = "small" | "medium" | "large" | "xlarge";

export interface BaseLabels {
  readonly project: string;
  readonly environment: Environment;
  readonly managedBy: "pulumi";
  readonly owner?: string;
  readonly costCenter?: string;
}
