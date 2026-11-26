// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import { NetworkSpec, NetworkOutput } from "./network";
import { KubernetesClusterSpec, KubernetesClusterOutput } from "./kubernetes";
import { ObjectStorageSpec, ObjectStorageOutput, CdnSpec, CdnOutput, StorageEndpointSpec, StorageEndpointOutput } from "./storage";
import { DatabaseClusterSpec, DatabaseClusterOutput } from "./database";
import { ContainerRegistrySpec, ContainerRegistryOutput } from "./registry";

/**
 * Each provider capability is its own interface (ISP).
 * CloudProviderFactory composes them all.
 *
 * This stack is AS-IS legacy retired at AM10 and implements DigitalOcean only;
 * no provider is added here (new infrastructure is Terraform, ../loop-runtime-infra-live).
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
    IProviderDatabase,
    IProviderRegistry {}
