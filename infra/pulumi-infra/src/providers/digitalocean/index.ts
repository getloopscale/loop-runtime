// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { CloudProviderFactory, IProviderDns } from "../../contracts/provider";
import { DigitalOceanNetworkProvider } from "./network";
import { DigitalOceanKubernetesProvider } from "./kubernetes";
import { DigitalOceanStorageProvider } from "./storage";
import { DigitalOceanDnsProvider } from "./dns";
import { DigitalOceanDatabaseProvider } from "./database";
import { DigitalOceanRegistryProvider } from "./registry";

export interface DigitalOceanProviderOpts {
  /** When provided, DNS records are created in Cloudflare instead of DO DNS.
   *  Required when the domain's nameservers are Cloudflare (not DigitalOcean). */
  dnsOverride?: IProviderDns;
}

/**
 * DigitalOcean provider factory — fully functional.
 *
 * Each capability is delegated to a focused provider class (SRP).
 * Only files inside providers/digitalocean/ may import @pulumi/digitalocean.
 *
 * DNS can be overridden with a Cloudflare provider when the domain's authoritative
 * nameservers are Cloudflare (common when using CF Tunnel or CF WAF).
 */
export class DigitalOceanProviderFactory implements CloudProviderFactory {
  private readonly network = new DigitalOceanNetworkProvider();
  private readonly kubernetes = new DigitalOceanKubernetesProvider();
  private readonly storage = new DigitalOceanStorageProvider();
  private readonly dns: IProviderDns;
  private readonly database = new DigitalOceanDatabaseProvider();
  private readonly registry = new DigitalOceanRegistryProvider();

  constructor(opts?: DigitalOceanProviderOpts) {
    this.dns = opts?.dnsOverride ?? new DigitalOceanDnsProvider();
  }

  createNetwork = this.network.createNetwork.bind(this.network);
  createKubernetesCluster = this.kubernetes.createKubernetesCluster.bind(this.kubernetes);
  createObjectStorage = this.storage.createObjectStorage.bind(this.storage);
  createCdn = this.storage.createCdn.bind(this.storage);
  getStorageEndpoint = this.storage.getStorageEndpoint.bind(this.storage);
  createDnsRecord = (spec: import("../../contracts/dns").DnsRecordSpec) => this.dns.createDnsRecord(spec);
  createDatabaseCluster = this.database.createDatabaseCluster.bind(this.database);
  createContainerRegistry = this.registry.createContainerRegistry.bind(this.registry);
}
