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
 * The first stack to run creates the registry. Subsequent stacks
 * also declare it — Pulumi handles this via retainOnDelete (so
 * destroying one stack doesn't delete the shared registry) and
 * the DO API returns the existing registry on create (idempotent).
 *
 * If the registry already exists in the Pulumi state, subsequent
 * `pulumi up` runs are no-ops. If it exists in DO but not in state,
 * run: `pulumi import digitalocean:index/containerRegistry:ContainerRegistry loopzero-infra-registry loopzero-infra-registry`
 */
export class DigitalOceanRegistryProvider implements IProviderRegistry {
  createContainerRegistry(spec: ContainerRegistrySpec): ContainerRegistryOutput {
    const registry = new digitalocean.ContainerRegistry(spec.name, {
      name: spec.name,
      subscriptionTierSlug: spec.tier,
      region: spec.region,
    }, {
      // retainOnDelete: if this stack is destroyed, the registry stays in DO.
      // Other stacks (and all images) depend on it.
      retainOnDelete: true,
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
