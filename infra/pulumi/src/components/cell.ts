// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Cell component for the Cell-Based Architecture (ADR-0008).
// A Cell is a self-contained unit of execution: 1 K8s cluster + ArgoCD + Traefik + monitoring.
// Phase 1: interfaces defined, createCell() scaffolded.
// Phase 2: actual provisioning implemented.

import * as pulumi from "@pulumi/pulumi";

// ---------------------------------------------------------------------------
// Input / Output contracts
// ---------------------------------------------------------------------------

/**
 * CellConfig defines the input for creating a Cell.
 * In Pulumi.dev.yaml: `loopzero:cells` is an array of CellConfig.
 */
export interface CellConfig {
  /** Unique cell identifier (e.g., "cell-default", "cell-us-east-1") */
  readonly cellId: string;
  /** Cloud provider region (e.g., "sfo3", "nyc3") */
  readonly region: string;
  /** Worker node size (e.g., "s-2vcpu-4gb") */
  readonly nodeSize: string;
  /** Number of worker nodes */
  readonly nodeCount: number;
  /** Base domain for ingress (e.g., "dev.loopzero.example") */
  readonly baseDomain: string;
}

/**
 * CellOutput is the output of a provisioned Cell.
 * Used by the API's CellRouter to connect to each Cell.
 */
export interface CellOutput {
  /** Kubeconfig for the Cell's workload cluster */
  readonly kubeconfig: pulumi.Output<string>;
  /** ArgoCD server URL within the Cell */
  readonly argocdUrl: pulumi.Output<string>;
  /** Traefik ingress public IP (or Tunnel ID) */
  readonly ingressIp: pulumi.Output<string>;
  /** Cell identifier (passthrough from config) */
  readonly cellId: string;
}

// ---------------------------------------------------------------------------
// Cell provisioning
// ---------------------------------------------------------------------------

/**
 * createCell provisions a complete Cell.
 *
 * A Cell contains:
 * - 1 DOKS cluster (via DigitalOcean Kubernetes)
 * - ArgoCD Helm install (connected to shared Gitea)
 * - Traefik Helm install (with dedicated LoadBalancer IP)
 * - cert-manager (Let's Encrypt DNS-01 via Cloudflare)
 * - Network policies (tenant isolation)
 * - Prometheus agent (local scrape, remote-write to central)
 *
 * Phase 1 implementation: scaffolded with TODOs.
 * The "cell-default" Cell uses the existing workload cluster.
 *
 * @param config Cell configuration
 * @param opts Pulumi resource options (parent, providers)
 * @returns Cell outputs for API integration
 */
export function createCell(
  config: CellConfig,
  _opts?: pulumi.ResourceOptions,
): CellOutput {
  // TODO(phase-2): Implement actual Cell provisioning.
  // For Phase 1, the "cell-default" Cell maps to the existing workload cluster.
  // New Cells will be provisioned here using:
  //   1. digitalocean.KubernetesCluster
  //   2. kubernetes.helm.v3.Release (ArgoCD)
  //   3. kubernetes.helm.v3.Release (Traefik)
  //   4. kubernetes.helm.v3.Release (cert-manager)
  //   5. kubernetes.helm.v3.Release (Prometheus agent)
  //   6. ClusterCredential CR creation on mgmt cluster

  return {
    cellId: config.cellId,
    kubeconfig: pulumi.output(`placeholder-kubeconfig-${config.cellId}`),
    argocdUrl: pulumi.output(`http://argocd-${config.cellId}:80`),
    ingressIp: pulumi.output(`0.0.0.0`),
  };
}

/**
 * Default cell configuration matching the current single-cluster setup.
 * Used when no cells are explicitly configured in Pulumi stack.
 */
export const DEFAULT_CELL: CellConfig = {
  cellId: "cell-default",
  region: "sfo3",
  nodeSize: "s-2vcpu-4gb",
  nodeCount: 2,
  baseDomain: "dev.loopzero.example",
};
