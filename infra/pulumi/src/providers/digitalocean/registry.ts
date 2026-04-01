// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as digitalocean from "@pulumi/digitalocean";
import { IProviderRegistry } from "../../contracts/provider";
import { ContainerRegistrySpec, ContainerRegistryOutput } from "../../contracts/registry";

/**
 * ARCH-008: DigitalOcean Container Registry provider.
 *
 * DigitalOcean allows only ONE container registry per account.
 * All stacks (dev/staging/prod) share the same registry.
 *
 * We always import the registry by name — if it already exists in
 * the DO account, Pulumi adopts it into the current stack's state.
 * If it doesn't exist, the import fails and Pulumi shows a clear
 * error asking the operator to create it first (via the prod stack
 * or manually).
 *
 * This prevents the 409 "name already exists" error that occurs
 * when multiple stacks try to create the same registry.
 */
export class DigitalOceanRegistryProvider implements IProviderRegistry {
  createContainerRegistry(spec: ContainerRegistrySpec): ContainerRegistryOutput {
    const registry = new digitalocean.ContainerRegistry(spec.name, {
      name: spec.name,
      subscriptionTierSlug: spec.tier,
      region: spec.region,
    }, {
      // Import existing registry — DO only allows one per account.
      // The registry name is deterministic, so we can always import by name.
      import: spec.name,
      // Protect from accidental deletion — registry contains all production images.
      protect: true,
      // Don't fail if subscription tier differs between stacks
      // (dev may use "starter", prod may use "professional").
      ignoreChanges: ["subscriptionTierSlug"],
    });

    return {
      endpoint: registry.serverUrl,
      name: registry.name,
      resource: registry,
    };
  }
}
