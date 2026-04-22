// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Bootstrap shared: constants, types, kubeconfig parser, namespace creation.
// Split modules: bootstrap-cert-ingress.ts, bootstrap-registry.ts

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import { PlatformStackConfig } from "../config/stack";

// ---------------------------------------------------------------------------
// Shared constants
// ---------------------------------------------------------------------------

/**
 * Platform namespace — all LoopZero platform services live here.
 * Workload namespaces are separate, created by operators.
 */
export const PLATFORM_NAMESPACE = "platform-system";

/**
 * Pinned Helm chart versions for deterministic provisioning.
 * Same commit = same infrastructure. Update these explicitly, never float.
 */
export const HELM_VERSIONS = {
  certManager: "v1.14.5",
  traefik: "28.3.0",
  argocd: "7.3.11",
  gitea: "10.6.0",
  minio: "5.4.0",
  argoWorkflows: "1.0.5",
  prometheus: "25.27.0",
  grafana: "8.5.2",
  loki: "6.24.0",
  promtail: "6.16.6",
  tempo: "1.14.0",
  otelCollector: "0.96.0",
  oryKratos: "0.61.0",
  oryHydra: "0.61.0",
  metricsServer: "3.12.1",
  kubeStateMetrics: "5.25.1",
} as const;

// ---------------------------------------------------------------------------
// Kubeconfig parser (infra-arch-005: replaces fragile regex extraction)
// ---------------------------------------------------------------------------

export interface KubeconfigFields {
  readonly token: string;
  readonly certificateAuthorityData: string;
  readonly server: string;
}

/**
 * Parse a DOKS kubeconfig YAML string and extract key fields.
 * Returns empty strings for missing fields instead of failing silently.
 */
export function parseKubeconfig(raw: string): KubeconfigFields {
  let token = "";
  let certificateAuthorityData = "";
  let server = "";

  for (const line of raw.split("\n")) {
    const trimmed = line.trim();
    const colonIdx = trimmed.indexOf(":");
    if (colonIdx < 0) continue;
    const key = trimmed.slice(0, colonIdx).trim();
    const value = trimmed.slice(colonIdx + 1).trim();
    if (!value) continue;

    switch (key) {
      case "token":
        if (!token) token = value;
        break;
      case "certificate-authority-data":
        if (!certificateAuthorityData) certificateAuthorityData = value;
        break;
      case "server":
        if (!server) server = value;
        break;
    }
  }

  if (!token || !certificateAuthorityData || !server) {
    const missing = [
      !token && "token",
      !certificateAuthorityData && "certificate-authority-data",
      !server && "server",
    ].filter(Boolean);
    throw new Error(
      `parseKubeconfig: missing required fields: ${missing.join(", ")}. ` +
      `The kubeconfig may use exec-based auth (not bearer token) or has an unexpected format.`,
    );
  }

  return { token, certificateAuthorityData, server };
}

// ---------------------------------------------------------------------------
// Output types
// ---------------------------------------------------------------------------

export interface ClusterBootstrapOutputs {
  readonly ingressReady: pulumi.Output<boolean>;
  readonly ingressEndpoint: pulumi.Output<string>;
  readonly platformNamespace: pulumi.Output<string>;
}

export interface MgmtBootstrapOutputs extends ClusterBootstrapOutputs {
  readonly certManagerReady: pulumi.Output<boolean>;
  readonly argoCdReady: pulumi.Output<boolean>;
  readonly giteaReady: pulumi.Output<boolean>;
  readonly prometheusReady: pulumi.Output<boolean>;
  readonly grafanaReady: pulumi.Output<boolean>;
  readonly loopzeroApiReady: pulumi.Output<boolean>;
  readonly loopzeroControllerReady: pulumi.Output<boolean>;
  readonly tenantControllerReady: pulumi.Output<boolean>;
  readonly oryKratosReady: pulumi.Output<boolean>;
  readonly oryHydraReady: pulumi.Output<boolean>;
  readonly tlsConfigured: pulumi.Output<boolean>;
}

export interface RuntimeBootstrapOutputs extends ClusterBootstrapOutputs {
  readonly ksmEndpoint: pulumi.Output<string>;
}

export interface BuildBootstrapOutputs {
  readonly argoWorkflowsReady: pulumi.Output<boolean>;
  readonly lokiEndpoint: pulumi.Output<string>;
  readonly argoMetricsEndpoint: pulumi.Output<string>;
}

export interface BootstrapOutputs {
  readonly mgmt: MgmtBootstrapOutputs;
  readonly build: BuildBootstrapOutputs;
  readonly runtime: RuntimeBootstrapOutputs;
  readonly runtime2?: { ingressReady: pulumi.Output<boolean> };
}

/**
 * Cloudflare resource outputs produced BEFORE bootstrap (Phase 1.5).
 */
export interface CloudflareOutputs {
  readonly kvNamespaceId?: pulumi.Output<string>;
}

// ---------------------------------------------------------------------------
// Shared utilities
// ---------------------------------------------------------------------------

export function createPlatformNamespace(
  role: string,
  config: PlatformStackConfig,
  provider: k8s.Provider,
): k8s.core.v1.Namespace {
  return new k8s.core.v1.Namespace(
    `${role}-${PLATFORM_NAMESPACE}`,
    {
      metadata: {
        name: PLATFORM_NAMESPACE,
        labels: {
          "app.kubernetes.io/managed-by": "pulumi",
          "app.kubernetes.io/part-of": "loopzero-platform",
          "loopzero.io/environment": config.environment,
          "loopzero.io/project": config.project,
          "loopzero.io/cluster-role": role,
          "pod-security.kubernetes.io/enforce": "baseline",
          "pod-security.kubernetes.io/warn": "restricted",
          "pod-security.kubernetes.io/audit": "restricted",
        },
      },
    },
    { provider },
  );
}

// ---------------------------------------------------------------------------
// Re-exports from split modules (backward compatibility)
// ---------------------------------------------------------------------------

export { installCertManager, installTraefik, lookupLoadBalancerEndpoint, createClusterIssuer } from "./bootstrap-cert-ingress";
export { createDockerRegistryPullSecret, installMetricsServer } from "./bootstrap-registry";
