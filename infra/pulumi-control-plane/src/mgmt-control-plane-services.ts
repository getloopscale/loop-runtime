// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Control-plane services orchestrator.
// Shared constants, types, and re-exports from split modules:
//   cp-ory-kratos.ts, cp-ory-hydra.ts, cp-cloud-service.ts,
//   cp-api-service.ts, cp-tenant-controller.ts, cp-ingress.ts, cp-network.ts

import * as random from "@pulumi/random";

// ---------------------------------------------------------------------------
// Shared constants
// ---------------------------------------------------------------------------

export const PLATFORM_NAMESPACE = "loopzero-system";

// Type aliases for migration — avoids importing from monolith
export type InfraOutputs = any;
export type ClusterOutputs = any;
export type PlatformStackConfig = any;
export type CloudflareOutputs = any;

// Helm chart versions — must match platform project
export const HELM_VERSIONS = {
  oryKratos: "0.61.0",
  oryHydra: "0.61.0",
} as const;

// IngressRoute definitions for platform services
export const PLATFORM_INGRESS_ROUTES = [
  { subdomain: "argocd", serviceName: "argocd-server", servicePort: 80 },
  { subdomain: "git", serviceName: "gitea-http", servicePort: 3000 },
  { subdomain: "grafana", serviceName: "grafana", servicePort: 3000 },
] as const;

// RBAC rule interface for scoped kubeconfig creation
export interface RbacRule {
  apiGroups: string[];
  resources: string[];
  verbs: string[];
}

/** Parse DOKS kubeconfig YAML to extract bearer token and CA certificate */
export function parseKubeconfig(raw: string): { token: string; certificateAuthorityData: string; server: string } {
  let token = "";
  let certificateAuthorityData = "";
  let server = "";
  for (const line of raw.split("\n")) {
    const trimmed = line.trim();
    if (trimmed.startsWith("token:")) token = trimmed.split("token:")[1]?.trim() ?? "";
    if (trimmed.startsWith("certificate-authority-data:")) certificateAuthorityData = trimmed.split("certificate-authority-data:")[1]?.trim() ?? "";
    if (trimmed.startsWith("server:")) server = trimmed.split("server:")[1]?.trim() ?? "";
  }
  return { token, certificateAuthorityData, server };
}

// H12-fix: Service auth secret auto-generated when not provided via config.
// Shared between Cloud service and Engine API — must be the same value.
// Persisted in Pulumi state, stable across deployments.
export const serviceAuthSecret = new random.RandomPassword("service-auth-secret", {
  length: 48,
  special: false,
});

// ---------------------------------------------------------------------------
// Re-exports from split modules
// ---------------------------------------------------------------------------

export { installOryKratos } from "./cp-ory-kratos";
export { installOryHydra } from "./cp-ory-hydra";
export { installCloudService } from "./cp-cloud-service";
export { installLoopZeroAPI } from "./cp-api-service";
export { installTenantController } from "./cp-tenant-controller";
export { createAPIGatewayRoute, createCloudGatewayRoute, createPlatformIngressRoutes } from "./cp-ingress";
export { createPlatformNetworkPolicies, registerRuntimeCluster, createScopedKubeconfig } from "./cp-network";
