// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as digitalocean from "@pulumi/digitalocean";
import { IProviderRegistry } from "../../contracts/provider";
import { ContainerRegistrySpec, ContainerRegistryOutput } from "../../contracts/registry";

/**
 * ARCH-008: DigitalOcean Container Registry provider.
 *
 * DigitalOcean allows only ONE container registry per account.
 * All stacks (dev/staging/prod) share the same registry.
 *
 * We import the registry by name — if it already exists in the DO
 * account, Pulumi adopts it into the current stack's state. Combined
 * with retainOnDelete, destroying one stack never deletes the shared registry.
 */
export class DigitalOceanRegistryProvider implements IProviderRegistry {
  createContainerRegistry(spec: ContainerRegistrySpec): ContainerRegistryOutput {
    const registry = new digitalocean.ContainerRegistry(spec.name, {
      name: spec.name,
      subscriptionTierSlug: spec.tier,
      region: spec.region,
    }, {
      // Import existing registry — DO only allows one per account.
      import: spec.name,
      // retainOnDelete + protect: destroying a stack never deletes the shared registry.
      retainOnDelete: true,
      protect: true,
      ignoreChanges: ["subscriptionTierSlug", "region"],
    });

    // Construct the endpoint deterministically: registry.digitalocean.com/{name}
    const endpoint = registry.name.apply(n => `registry.digitalocean.com/${n}`);

    return {
      endpoint,
      name: registry.name,
      resource: registry,
    };
  }
}
