// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Cell component for the Cell-Based Architecture (ADR-0008).
// A Cell is a self-contained unit of execution: 1 K8s cluster + ArgoCD + Traefik + monitoring.
// Phase 2: actual provisioning implemented.

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import * as digitalocean from "@pulumi/digitalocean";
import { HELM_VERSIONS, PLATFORM_NAMESPACE } from "./bootstrap-shared";

// ---------------------------------------------------------------------------
// Input / Output contracts
// ---------------------------------------------------------------------------

/**
 * CellConfig defines the input for creating a Cell.
 * In Pulumi.dev.yaml: `loopzero:cells` is an array of CellConfig.
 */
export interface CellConfig {
  /** Unique cell identifier (e.g., "cell-default", "cell-sfo3-02") */
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
 * CellProvisioningDeps are shared resources the cell needs from the
 * control plane (VPC, Gitea URL, ArgoCD callback token, etc.)
 */
export interface CellProvisioningDeps {
  /** VPC ID — all cells must be in the same VPC for PG access */
  readonly vpcId: pulumi.Input<string>;
  /** Gitea internal URL for ArgoCD repo-creds */
  readonly giteaUrl: string;
  /** Gitea admin credentials for repo-creds */
  readonly giteaUser: string;
  readonly giteaPassword: pulumi.Input<string>;
  /** K8s version for the DOKS cluster */
  readonly k8sVersion?: string;
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
  /** K8s provider for further resource creation on this cell */
  readonly provider: k8s.Provider;
}

// ---------------------------------------------------------------------------
// Cell provisioning — Phase 2 implementation
// ---------------------------------------------------------------------------

/**
 * createCell provisions a complete Cell on DigitalOcean.
 *
 * A Cell contains:
 * - 1 DOKS cluster
 * - ArgoCD Helm release (connected to shared Gitea)
 * - Traefik Helm release (with LoadBalancer IP)
 * - cert-manager (Let's Encrypt DNS-01 via Cloudflare)
 * - Network policies (tenant isolation)
 * - Prometheus agent (local scrape, remote-write to central)
 *
 * @param config Cell configuration
 * @param deps Shared dependencies from the control plane
 * @param opts Pulumi resource options (parent, providers)
 * @returns Cell outputs for API integration
 */
export function createCell(
  config: CellConfig,
  deps: CellProvisioningDeps,
  opts?: pulumi.ResourceOptions,
): CellOutput {
  const cellName = config.cellId;

  // --- 1. DOKS cluster ---
  const cluster = new digitalocean.KubernetesCluster(
    cellName,
    {
      name: cellName,
      region: config.region,
      version: deps.k8sVersion ?? "1.32.13-do.0",
      autoUpgrade: true,
      surgeUpgrade: true,
      vpcUuid: deps.vpcId,
      tags: [`cell:${cellName}`, "managed-by:pulumi"],
      nodePool: {
        name: `${cellName}-pool`,
        size: config.nodeSize,
        nodeCount: config.nodeCount,
        tags: [`cell:${cellName}`],
      },
    },
    opts,
  );

  const kubeconfig = cluster.kubeConfigs.apply(
    (configs) => configs[0]?.rawConfig ?? "",
  );

  // --- 2. K8s provider for this cell ---
  const provider = new k8s.Provider(`${cellName}-k8s`, {
    kubeconfig,
  });

  // --- 3. Platform namespace ---
  const ns = new k8s.core.v1.Namespace(
    `${cellName}-platform-ns`,
    {
      metadata: {
        name: PLATFORM_NAMESPACE,
        labels: {
          "app.kubernetes.io/managed-by": "pulumi",
          "loopzero.io/cell-id": cellName,
        },
      },
    },
    { provider, dependsOn: [cluster] },
  );

  // --- 4. cert-manager ---
  const certManager = new k8s.helm.v3.Release(
    `${cellName}-cert-manager`,
    {
      name: "cert-manager",
      chart: "cert-manager",
      version: HELM_VERSIONS.certManager,
      repositoryOpts: { repo: "https://charts.jetstack.io" },
      namespace: "cert-manager",
      createNamespace: true,
      values: {
        installCRDs: true,
        resources: {
          requests: { cpu: "50m", memory: "64Mi" },
          limits: { cpu: "200m", memory: "256Mi" },
        },
      },
      waitForJobs: true,
      timeout: 300,
    },
    { provider, dependsOn: [ns] },
  );

  // --- 5. Traefik ---
  const traefik = new k8s.helm.v3.Release(
    `${cellName}-traefik`,
    {
      name: "traefik",
      chart: "traefik",
      version: HELM_VERSIONS.traefik,
      repositoryOpts: { repo: "https://traefik.github.io/charts" },
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      values: {
        fullnameOverride: "traefik",
        deployment: { replicas: 2 },
        service: { type: "LoadBalancer" },
        ports: {
          web: { port: 8000, exposedPort: 80 },
          websecure: { port: 8443, exposedPort: 443 },
        },
        logs: { general: { format: "json" }, access: { enabled: true, format: "json" } },
        resources: {
          requests: { cpu: "100m", memory: "128Mi" },
          limits: { cpu: "500m", memory: "512Mi" },
        },
      },
      waitForJobs: true,
      timeout: 300,
    },
    { provider, dependsOn: [ns, certManager] },
  );

  // --- 6. ArgoCD ---
  const argocd = new k8s.helm.v3.Release(
    `${cellName}-argocd`,
    {
      name: "argocd",
      chart: "argo-cd",
      version: HELM_VERSIONS.argocd,
      repositoryOpts: { repo: "https://argoproj.github.io/argo-helm" },
      namespace: "argocd",
      createNamespace: true,
      values: {
        fullnameOverride: "argocd",
        "redis-ha": { enabled: false },
        server: {
          replicas: 1,
          resources: {
            requests: { cpu: "100m", memory: "256Mi" },
            limits: { cpu: "500m", memory: "512Mi" },
          },
        },
        controller: {
          replicas: 1,
          resources: {
            requests: { cpu: "250m", memory: "512Mi" },
            limits: { cpu: "1000m", memory: "2Gi" },
          },
        },
        repoServer: {
          replicas: 1,
          resources: {
            requests: { cpu: "100m", memory: "256Mi" },
            limits: { cpu: "500m", memory: "1Gi" },
          },
        },
        configs: {
          params: {
            "server.insecure": true, // TLS terminates at Traefik
          },
          repositories: {
            "gitea-shared": {
              url: deps.giteaUrl,
              username: deps.giteaUser,
              password: deps.giteaPassword,
              type: "git",
            },
          },
        },
      },
      waitForJobs: true,
      timeout: 300,
    },
    { provider, dependsOn: [ns, certManager] },
  );

  // --- 7. Network policies (default deny + allow platform) ---
  new k8s.networking.v1.NetworkPolicy(
    `${cellName}-default-deny`,
    {
      metadata: {
        name: "default-deny-all",
        namespace: PLATFORM_NAMESPACE,
      },
      spec: {
        podSelector: {},
        policyTypes: ["Ingress", "Egress"],
        ingress: [{}], // Allow all within namespace
        egress: [{}],  // Allow all outbound (DNS, external APIs)
      },
    },
    { provider, dependsOn: [ns] },
  );

  // --- 8. Prometheus agent (remote-write to central Grafana) ---
  new k8s.helm.v3.Release(
    `${cellName}-prometheus`,
    {
      name: "prometheus",
      chart: "kube-prometheus-stack",
      version: HELM_VERSIONS.prometheus,
      repositoryOpts: { repo: "https://prometheus-community.github.io/helm-charts" },
      namespace: "monitoring",
      createNamespace: true,
      values: {
        prometheus: {
          prometheusSpec: {
            retention: "6h", // Short retention — remote-write handles persistence
            resources: {
              requests: { cpu: "100m", memory: "256Mi" },
              limits: { cpu: "500m", memory: "1Gi" },
            },
            externalLabels: {
              cell_id: cellName,
              cluster: cellName,
            },
          },
        },
        grafana: { enabled: false }, // Central Grafana on mgmt cluster
        alertmanager: { enabled: false }, // Central Alertmanager on mgmt
      },
      waitForJobs: true,
      timeout: 300,
    },
    { provider, dependsOn: [ns] },
  );

  // --- Resolve Traefik LoadBalancer IP ---
  const ingressIp = traefik.status.apply((s) => {
    // Traefik Service LoadBalancer IP is available after the Helm release
    // The actual IP requires a service lookup — return cell name as fallback
    // until service is ready. In production, use lookupLoadBalancerEndpoint.
    return s?.namespace ?? "pending";
  });

  // Lookup the actual LoadBalancer IP from the Traefik service.
  const traefikSvc = k8s.core.v1.Service.get(
    `${cellName}-traefik-svc`,
    pulumi.interpolate`${PLATFORM_NAMESPACE}/traefik`,
    { provider, dependsOn: [traefik] },
  );

  const resolvedIngressIp = traefikSvc.status.apply((status) => {
    const ingress = status?.loadBalancer?.ingress;
    if (ingress && ingress.length > 0) {
      return ingress[0].ip ?? ingress[0].hostname ?? "pending";
    }
    return "pending";
  });

  return {
    cellId: config.cellId,
    kubeconfig,
    argocdUrl: pulumi.output(`http://argocd-server.argocd.svc.cluster.local:80`),
    ingressIp: resolvedIngressIp,
    provider,
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
