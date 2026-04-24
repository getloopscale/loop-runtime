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
  };

  storage: {
    endpoint: string;
    bucket: string;
    accessKey?: string;
    secretKey?: string;
  };

  profile: 'minimal' | 'production';
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
