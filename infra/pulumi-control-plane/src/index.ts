// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import { loadControlPlaneConfig } from "./config";
import { infra, platform } from "./infra-ref";

const config = loadControlPlaneConfig();

// --- K8s provider (control-plane runs on mgmt cluster) ---
const mgmtProvider = new k8s.Provider("mgmt-k8s", {
  kubeconfig: infra.mgmtKubeconfig,
});

// --- TODO: Wire control-plane services ---
// Functions to extract from mgmt-bootstrap.ts and wire here:
// - installLoopZeroAPI()      → Deployment, Service, PDB, ConfigMap, Secrets, RBAC, kubeconfigs
// - installCloudService() → Deployment, Service, ConfigMap, Secret
// - installTenantController() → Deployment, Service, PDB, CRDs, RBAC, kubeconfig
// - installOryKratos()    → Helm release + UI Deployment + Service
// - installOryHydra()     → Helm release
// - createPlatformIngressRoutes() → IngressRoute + Certificate CRs
// - createAPIGatewayRoute()       → IngressRoute + Certificate
// - createCloudGatewayRoute()     → IngressRoute + Certificate
// - createPlatformNetworkPolicies() → NetworkPolicy CRs
// - registerRuntimeCluster() → ArgoCD cluster secret

// --- Stack Outputs ---
export const loopzeroApiReady = pulumi.output(false);
export const tenantControllerReady = pulumi.output(false);
export const oryKratosReady = pulumi.output(false);
export const oryHydraReady = pulumi.output(false);
