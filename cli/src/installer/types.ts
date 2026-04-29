// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

/**
 * Platform installation configuration provided by the customer.
 */
export interface PlatformConfig {
  domain: string;
  clusterIssuer?: string;

  database: {
    external: boolean;
    url?: string; // Required when external=true
  };

  registry: {
    type: 'docr' | 'harbor' | 'ecr' | 'gcr';
    endpoint: string;
    username?: string;
    password?: string;
  };

  auth: {
    type: 'bundled' | 'external';
    issuer?: string;
    clientId?: string;
    systemSecret?: string;  // Auto-generated if empty (32 chars, for Hydra JWT signing)
  };

  // Cluster mode
  clusters?: {
    mode: 'single' | 'multi';
    build?: { kubeconfigPath?: string };
    workload?: { kubeconfigPath?: string };
  };

  // Admin account (created during install)
  admin?: {
    email: string;
    password?: string;
  };

  storage: {
    endpoint: string;
    bucket: string;
    accessKey?: string;
    secretKey?: string;
  };

  profile: 'minimal' | 'production';

  // CHG-2026-04-28-LAYERED-BOOTSTRAP / T3.1 — optional `provider` flag.
  // When set, `loopzero platform install` provisions cloud infra (clusters, DB,
  // storage, registry) via Pulumi L1 BEFORE running Helmfile L2.
  // When unset, assumes the cluster already exists (self-hosting profile).
  provider?: 'digitalocean' | 'aws' | 'k3d';
  providerConfig?: {
    region?: string;
    cfAccountId?: string;
    cfZoneId?: string;
    cfApiToken?: string;
    digitaloceanToken?: string;
    spacesAccessKey?: string;
    spacesSecretKey?: string;
  };
}

/**
 * Helm release definition for a prerequisite or component.
 */
export interface HelmRelease {
  name: string;
  chart: string;        // chart ref (repo/chart or oci://)
  version: string;
  namespace: string;
  createNamespace?: boolean;
  values?: Record<string, unknown>;
  valuesFile?: string;
  waitReady?: boolean;
  timeout?: string;
  order: number;        // Installation order (lower = first)
}

/**
 * Step result for progress reporting.
 */
export interface StepResult {
  name: string;
  status: 'success' | 'skipped' | 'failed';
  duration: number;     // milliseconds
  error?: string;
}

/**
 * Component status for `loopzero platform status`.
 */
export interface ComponentStatus {
  name: string;
  status: 'Running' | 'Pending' | 'Failed' | 'NotInstalled';
  replicas?: string;    // "2/2"
  version?: string;
}

/**
 * Platform status for `loopzero platform status`.
 */
export interface PlatformStatus {
  phase: 'NotInstalled' | 'Installing' | 'Ready' | 'Degraded' | 'Failed';
  version?: string;
  components: ComponentStatus[];
  crdsInstalled: number;
  endpoints: {
    api?: string;
    dashboard?: string;
  };
}
