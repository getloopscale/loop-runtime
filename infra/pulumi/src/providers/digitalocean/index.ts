// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { CloudProviderFactory } from "../../contracts/provider";
import { DigitalOceanNetworkProvider } from "./network";
import { DigitalOceanKubernetesProvider } from "./kubernetes";
import { DigitalOceanStorageProvider } from "./storage";
import { DigitalOceanDatabaseProvider } from "./database";
import { DigitalOceanRegistryProvider } from "./registry";

/**
 * DigitalOcean provider factory — fully functional.
 *
 * Each capability is delegated to a focused provider class (SRP). Some
 * DigitalOcean resources are built directly by the components that need them
 * (e.g. components/do-spaces-openbao-backups.ts), so this factory is the common path, not
 * an isolation boundary for @pulumi/digitalocean.
 */
export class DigitalOceanProviderFactory implements CloudProviderFactory {
  private readonly network = new DigitalOceanNetworkProvider();
  private readonly kubernetes = new DigitalOceanKubernetesProvider();
  private readonly storage = new DigitalOceanStorageProvider();
  private readonly database = new DigitalOceanDatabaseProvider();
  private readonly registry = new DigitalOceanRegistryProvider();

  createNetwork = this.network.createNetwork.bind(this.network);
  createKubernetesCluster = this.kubernetes.createKubernetesCluster.bind(this.kubernetes);
  createObjectStorage = this.storage.createObjectStorage.bind(this.storage);
  createCdn = this.storage.createCdn.bind(this.storage);
  getStorageEndpoint = this.storage.getStorageEndpoint.bind(this.storage);
  createDatabaseCluster = this.database.createDatabaseCluster.bind(this.database);
  createContainerRegistry = this.registry.createContainerRegistry.bind(this.registry);
}
