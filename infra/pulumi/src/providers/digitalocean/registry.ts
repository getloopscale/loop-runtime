// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as digitalocean from "@pulumi/digitalocean";
import { IProviderRegistry } from "../../contracts/provider";
import { ContainerRegistrySpec, ContainerRegistryOutput } from "../../contracts/registry";

/**
 * ARCH-008: DigitalOcean Container Registry provider.
 * Moves registry creation behind the CloudProviderFactory abstraction.
 */
export class DigitalOceanRegistryProvider implements IProviderRegistry {
  createContainerRegistry(spec: ContainerRegistrySpec): ContainerRegistryOutput {
    const registry = new digitalocean.ContainerRegistry(spec.name, {
      name: spec.name,
      subscriptionTierSlug: spec.tier,
      region: spec.region,
    });

    return {
      endpoint: registry.serverUrl,
      name: registry.name,
      resource: registry,
    };
  }
}
