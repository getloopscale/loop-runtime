// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as pulumi from "@pulumi/pulumi";

/**
 * ARCH-008: Container registry contract.
 * Closes the abstraction leak where infra.ts directly imported @pulumi/digitalocean
 * for ContainerRegistry resources.
 */

export interface ContainerRegistrySpec {
  /** Registry name (e.g., "loopruntime-system") */
  name: string;

  /** Subscription tier (e.g., "professional", "starter", "basic") */
  tier: string;

  /** Region for the registry (e.g., "sfo3") */
  region: string;
}

export interface ContainerRegistryOutput {
  /** The registry endpoint URL (e.g., "registry.digitalocean.com/loopruntime-system") */
  endpoint: pulumi.Output<string>;

  /** The registry name */
  name: pulumi.Output<string>;

  /** The underlying Pulumi resource for dependency tracking */
  resource: pulumi.Resource;
}
