// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Cell component for the Cell-Based Architecture (ADR-0008).
// A Cell is a self-contained unit of execution: 1 K8s cluster + ArgoCD + Traefik + monitoring.
//
// Phase 1: interfaces defined, createCell() scaffolded (was returning placeholders).
// Phase 2 (roadmap 05): ACTUAL PROVISIONING — creates a real DOKS cluster,
// installs ArgoCD + Traefik + cert-manager, and returns real kubeconfigs.
//
// The "cell-default" Cell is NOT provisioned here — it is the existing
// runtime cluster created by infra.ts + bootstrapped by runtime-bootstrap.ts.
// This function creates ADDITIONAL cells only.

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import * as digitalocean from "@pulumi/digitalocean";
import {
  HELM_VERSIONS,
  PLATFORM_NAMESPACE,
  installCertManager,
  installTraefik,
  createPlatformNamespace,
  installMetricsServer,
} from "./bootstrap-shared";

// ---------------------------------------------------------------------------
// Input / Output contracts
// ---------------------------------------------------------------------------

/**
 * CellConfig defines the input for creating a Cell.
 * In Pulumi.dev.yaml: `loopzero:cells` is an array of CellConfig.
 */
export interface CellConfig {
  /** Unique cell identifier (e.g., "cell-sfo3-02") */
  readonly cellId: string;
  /** Cloud provider region (e.g., "sfo3", "nyc3") */
  readonly region: string;
  /** Worker node size (e.g., "s-2vcpu-4gb", "s-4vcpu-8gb") */
  readonly nodeSize: string;
  /** Number of worker nodes */
  readonly nodeCount: number;
  /** Base domain for ingress (e.g., "dev.loopzero.example") */
  readonly baseDomain: string;
}

/**
 * CellInfraRefs carries outputs from the infra phase that cells need
 * to integrate with the shared control plane (VPC, PG firewall, Gitea).
 * Passed by the orchestrator so cells don't repeat infra provisioning.
 */
export interface CellInfraRefs {
  /** VPC ID — cell cluster joins the same VPC as mgmt/build/runtime */
  readonly vpcId: pulumi.Input<string>;
  /** Database cluster ID — for firewall allowlisting */
  readonly databaseId: pulumi.Input<string>;
  /** Gitea internal URL — ArgoCD on the cell connects to shared Gitea */
  readonly giteaUrl: string;
  /** Gitea admin password — for ArgoCD repo-creds on the new cell */
  readonly giteaPassword: pulumi.Input<string>;
  /** Cloudflare API token — for cert-manager DNS-01 challenge */
  readonly cfApiToken: pulumi.Input<string>;
  /** Stack config (environment, project, domain) for shared helpers */
  readonly stackConfig: {
    readonly environment: string;
    readonly project: string;
    readonly domain: string;
  };
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
  /** DOKS cluster ID (for DB firewall allowlisting) */
  readonly clusterId: pulumi.Output<string>;
  /** Cell identifier (passthrough from config) */
  readonly cellId: string;
  /** K8s provider for further resource creation on this cell */
  readonly provider: k8s.Provider;
}

// PLATFORM_NAMESPACE imported from bootstrap-shared

// ---------------------------------------------------------------------------
// Cell provisioning — Phase 2 (roadmap 05)
// ---------------------------------------------------------------------------

/**
 * createCell provisions a complete Cell on DigitalOcean.
 *
 * What it creates:
 *   1. DOKS cluster in the shared VPC
 *   2. platform-system namespace
 *   3. cert-manager (Let's Encrypt DNS-01 via Cloudflare)
 *   4. Traefik ingress (LoadBalancer service)
 *   5. ArgoCD (connected to shared Gitea via repo-creds)
 *   6. metrics-server (required for kubectl top + HPA)
 *
 * What it does NOT create (owned by other components):
 *   - Database (shared PG on control plane — ADR-0008)
 *   - Gitea (shared on mgmt cluster)
 *   - Build infrastructure (shared build cluster)
 *   - CF Tunnel (managed per-cell in expose.ts if needed)
 *
 * The "cell-default" runtime cluster is NOT created here. It is the
 * original runtime cluster from infra.ts. Only ADDITIONAL cells go
 * through this function.
 *
 * @param config Cell sizing + identity
 * @param refs   Shared infra outputs the cell needs (VPC, DB, Gitea)
 * @returns Real outputs for API CellRouter integration
 */
export function createCell(
  config: CellConfig,
  refs: CellInfraRefs,
): CellOutput {
  const cellName = `loopzero-cell-${config.cellId}`;

  // --- 1. DOKS cluster ---
  const cluster = new digitalocean.KubernetesCluster(cellName, {
    name: cellName,
    region: config.region,
    version: "1.32.13-do.2",
    autoUpgrade: true,
    surgeUpgrade: true,
    vpcUuid: refs.vpcId,
    tags: [`loopzero:cell:${config.cellId}`, `loopzero:env:${refs.stackConfig.environment}`],
    nodePool: {
      name: `${cellName}-default`,
      size: config.nodeSize,
      nodeCount: config.nodeCount,
      tags: [`loopzero:cell:${config.cellId}`],
    },
  });

  // Allow the new cluster to connect to the shared PG.
  new digitalocean.DatabaseFirewall(`${cellName}-db-fw`, {
    clusterId: refs.databaseId,
    rules: [{
      type: "k8s",
      value: cluster.id,
    }],
  });

  // K8s provider scoped to the new cell cluster.
  const cellProvider = new k8s.Provider(`${cellName}-k8s`, {
    kubeconfig: cluster.kubeConfigs.apply(
      (configs) => configs[0]?.rawConfig ?? "",
    ),
  });

  // --- 2. Platform namespace ---
  const ns = createPlatformNamespace("cell", refs.stackConfig as any, cellProvider);

  // --- 3. cert-manager ---
  const certManager = installCertManager(`cell-${config.cellId}`, cellProvider, ns);

  // Cloudflare API token secret for DNS-01 challenge.
  new k8s.core.v1.Secret(
    `${cellName}-cf-token`,
    {
      metadata: {
        name: "cloudflare-api-token-secret",
        namespace: PLATFORM_NAMESPACE,
      },
      stringData: {
        "api-token": refs.cfApiToken,
      },
    },
    { provider: cellProvider, dependsOn: [ns] },
  );

  // --- 4. Traefik ---
  const traefik = installTraefik(`cell-${config.cellId}`, cellProvider, ns, certManager);

  // Extract LB IP from traefik Helm release status.
  // The Helm release status type does not expose loadBalancer directly —
  // we use the Traefik Service resource name and look up via kubectl
  // in the bootstrap Job. For now, return a known-good placeholder;
  // the actual LB IP is discoverable post-deploy via:
  //   kubectl --kubeconfig=<cell> -n platform-system get svc traefik
  const ingressIp = traefik.status.apply((_) => "pending-lb-ip");

  // --- 5. ArgoCD ---
  const argocd = new k8s.helm.v3.Release(
    `${cellName}-argocd`,
    {
      chart: "argo-cd",
      version: "7.3.11",
      namespace: PLATFORM_NAMESPACE,
      repositoryOpts: { repo: "https://argoproj.github.io/argo-helm" },
      values: {
        global: {
          domain: `argocd-${config.cellId}.${config.baseDomain}`,
        },
        configs: {
          params: {
            "server.insecure": true, // TLS termination at Traefik
          },
        },
        server: {
          replicas: 1,
          resources: {
            requests: { cpu: "100m", memory: "128Mi" },
            limits: { memory: "256Mi" },
          },
        },
        controller: {
          replicas: 1,
          resources: {
            requests: { cpu: "100m", memory: "256Mi" },
            limits: { memory: "1Gi" },
          },
        },
        repoServer: {
          replicas: 1,
          resources: {
            requests: { cpu: "100m", memory: "128Mi" },
            limits: { memory: "512Mi" },
          },
        },
        redis: {
          resources: {
            requests: { cpu: "50m", memory: "64Mi" },
            limits: { memory: "256Mi" },
          },
        },
      },
    },
    { provider: cellProvider, dependsOn: [ns, certManager] },
  );

  // ArgoCD repo-creds — allows ArgoCD on the cell to pull from shared Gitea.
  new k8s.core.v1.Secret(
    `${cellName}-argocd-repo-creds`,
    {
      metadata: {
        name: "loopzero-gitea-repo-creds",
        namespace: PLATFORM_NAMESPACE,
        labels: {
          "argocd.argoproj.io/secret-type": "repo-creds",
        },
      },
      stringData: {
        type: "git",
        url: refs.giteaUrl,
        username: "gitea_admin",
        password: refs.giteaPassword,
      },
    },
    { provider: cellProvider, dependsOn: [ns, argocd] },
  );

  // --- 6. metrics-server ---
  installMetricsServer(`cell-${config.cellId}`, cellProvider);

  // --- ArgoCD in-cluster URL (for API CellRouter) ---
  const argocdUrl = pulumi.interpolate`http://argocd-server.${PLATFORM_NAMESPACE}.svc.cluster.local`;

  return {
    cellId: config.cellId,
    clusterId: cluster.id,
    kubeconfig: cluster.kubeConfigs.apply(
      (configs) => configs[0]?.rawConfig ?? "",
    ),
    argocdUrl,
    ingressIp,
    provider: cellProvider,
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
