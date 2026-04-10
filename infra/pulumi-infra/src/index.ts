// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as pulumi from "@pulumi/pulumi";
import { loadInfraStackConfig } from "./config/stack";
import { createInfra } from "./components/infra";

// --- Configuration ---
const config = loadInfraStackConfig();

// --- Phase 1: Base Infrastructure ---
const infra = createInfra(config);

// --- Stack Outputs (InfraStackOutputs contract) ---
// Only export what downstream consumers (platform, control-plane, CI) actually read.
// See ADR-001 for the full contract. Adding fields is backwards-compatible;
// removing fields requires coordinated migration.

// Clusters — kubeconfigs (CI + StackReference consumers)
export const mgmtClusterName = infra.mgmtCluster.clusterName;
export const mgmtClusterEndpoint = infra.mgmtCluster.endpoint;
export const mgmtKubeconfig = pulumi.secret(infra.mgmtCluster.kubeconfig);

export const buildClusterName = infra.buildCluster.clusterName;
export const buildClusterEndpoint = infra.buildCluster.endpoint;
export const buildKubeconfig = pulumi.secret(infra.buildCluster.kubeconfig);

export const runtimeClusterName = infra.runtimeCluster.clusterName;
export const runtimeClusterEndpoint = infra.runtimeCluster.endpoint;
export const runtimeKubeconfig = pulumi.secret(infra.runtimeCluster.kubeconfig);

// Database — connection details (CI seed + API config)
export const dbHost = infra.database.privateHost;
export const dbPort = infra.database.port;
export const dbUri = pulumi.secret(infra.database.uri);
export const dbPassword = pulumi.secret(infra.database.password);

// Storage — bucket names and endpoints (Loki, API S3, CF Worker)
export const bucketName = infra.bucketName;
export const bucketEndpoint = infra.bucketEndpoint;
export const buildsBucketName = infra.buildsBucketName;
export const logsBucketName = infra.logsBucketName;
export const staticBucketName = infra.staticBucketName;
export const staticBucketEndpoint = infra.staticBucketEndpoint;
export const spacesEndpoint = infra.spacesEndpoint;
export const s3Endpoint = infra.s3Endpoint;

// Container Registry — image builds (CI + bootstrap image tags)
export const registryEndpoint = infra.registryEndpoint;
export const registryName = infra.registryName;
