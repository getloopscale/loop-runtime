// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { NetworkSpec, NetworkOutput } from "./network";
import { KubernetesClusterSpec, KubernetesClusterOutput } from "./kubernetes";
import { ObjectStorageSpec, ObjectStorageOutput, CdnSpec, CdnOutput, StorageEndpointSpec, StorageEndpointOutput } from "./storage";
import { DnsRecordSpec, DnsRecordOutput } from "./dns";
import { DatabaseClusterSpec, DatabaseClusterOutput } from "./database";
import { ContainerRegistrySpec, ContainerRegistryOutput } from "./registry";

/**
 * Each provider capability is its own interface (ISP).
 * CloudProviderFactory composes them all.
 *
 * When adding a new provider:
 * 1. Create providers/{name}/ with individual resource files
 * 2. Create a node class map in providers/{name}/nodes.ts
 * 3. Implement CloudProviderFactory
 * 4. Add case to resolveProvider() in components/infra.ts
 */

export interface IProviderNetwork {
  createNetwork(spec: NetworkSpec): NetworkOutput;
}

export interface IProviderKubernetes {
  createKubernetesCluster(spec: KubernetesClusterSpec): KubernetesClusterOutput;
}

export interface IProviderStorage {
  createObjectStorage(spec: ObjectStorageSpec): ObjectStorageOutput;
  createCdn(spec: CdnSpec): CdnOutput;
  getStorageEndpoint(spec: StorageEndpointSpec): StorageEndpointOutput;
}

export interface IProviderDns {
  createDnsRecord(spec: DnsRecordSpec): DnsRecordOutput;
}

export interface IProviderDatabase {
  createDatabaseCluster(spec: DatabaseClusterSpec): DatabaseClusterOutput;
}

/**
 * ARCH-008: Container registry provider for storing container images.
 */
export interface IProviderRegistry {
  createContainerRegistry(spec: ContainerRegistrySpec): ContainerRegistryOutput;
}

// REVIEW-2026-03-30: Fixes MED-02 — removed IProviderCompute (dead code, zero call sites).
export interface CloudProviderFactory
  extends IProviderNetwork,
    IProviderKubernetes,
    IProviderStorage,
    IProviderDns,
    IProviderDatabase,
    IProviderRegistry {}
