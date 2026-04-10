// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import { loadPlatformConfig } from "./config";
import { infra } from "./infra-ref";

const config = loadPlatformConfig();

// --- K8s providers (from infra StackReference) ---
const mgmtProvider = new k8s.Provider("mgmt-k8s", {
  kubeconfig: infra.mgmtKubeconfig,
});
const buildProvider = new k8s.Provider("build-k8s", {
  kubeconfig: infra.buildKubeconfig,
});
const runtimeProvider = new k8s.Provider("runtime-k8s", {
  kubeconfig: infra.runtimeKubeconfig,
});

// --- Cloudflare (optional) ---
// TODO: Wire createTunnel() and createStaticDelivery() from src/providers/cloudflare/

// --- Bootstrap services ---
// TODO: Wire bootstrap functions from src/components/ using providers above.
// Each function receives a K8s provider + config. The code is extracted and ready
// in mgmt-platform-services.ts, build-bootstrap.ts, runtime-bootstrap.ts.
//
// Until wired, this project creates K8s providers only (validates StackReference works).
// Do NOT export placeholder outputs — downstream consumers use requireOutput()
// which will fail explicitly if this project hasn't produced real values yet.
// This is intentional: fail-fast > silent wrong values.

// --- Stack Outputs ---
// Only export outputs that this stack actually produces.
// Consumers that call requireOutput() on missing keys get a clear error
// instead of silently receiving "pending-migration".
//
// Outputs will be added as bootstrap functions are wired:
//   mgmtIngressEndpoint, runtimeIngressEndpoint → from Traefik LB lookup
//   lokiEndpoint, argoMetricsEndpoint → from build-bootstrap node IP resolution
//   giteaReady, argoCdReady, etc. → from Helm release status
//   cfTunnelId, cfKvNamespaceId → from CF resource creation
