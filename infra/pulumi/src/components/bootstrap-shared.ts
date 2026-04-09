// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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
} as const;

// ---------------------------------------------------------------------------
// Kubeconfig parser (infra-arch-005: replaces fragile regex extraction)
// ---------------------------------------------------------------------------

/**
 * Parsed fields from a DOKS kubeconfig YAML.
 * Extracts bearer token and CA certificate data without a full YAML parser
 * by walking lines and matching key-value pairs. More robust than regex
 * because it handles variable whitespace and only matches YAML scalar values.
 */
export interface KubeconfigFields {
  readonly token: string;
  readonly certificateAuthorityData: string;
  readonly server: string;
}

/**
 * Parse a DOKS kubeconfig YAML string and extract key fields.
 * Returns empty strings for missing fields instead of failing silently.
 *
 * This replaces the fragile `/token:\s*(.+)/` regex that could break
 * if DigitalOcean changes kubeconfig format (e.g., exec-based auth).
 */
export function parseKubeconfig(raw: string): KubeconfigFields {
  let token = "";
  let certificateAuthorityData = "";
  let server = "";

  for (const line of raw.split("\n")) {
    const trimmed = line.trim();
    // Match "key: value" patterns (YAML scalar values only)
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

  // REVIEW-2026-03-30: Fixes MED-04 — fail fast on missing fields instead of
  // returning empty strings that cause deferred runtime failures (e.g., ESO
  // trying to authenticate with an empty bearer token).
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
  readonly tenantControllerReady: pulumi.Output<boolean>;
  readonly oryKratosReady: pulumi.Output<boolean>;
  readonly oryHydraReady: pulumi.Output<boolean>;
  readonly tlsConfigured: pulumi.Output<boolean>;
}

export interface BuildBootstrapOutputs {
  readonly argoWorkflowsReady: pulumi.Output<boolean>;
  readonly lokiEndpoint: pulumi.Output<string>;
}

export interface BootstrapOutputs {
  readonly mgmt: MgmtBootstrapOutputs;
  readonly build: BuildBootstrapOutputs;
  readonly runtime: ClusterBootstrapOutputs;
}

// ---------------------------------------------------------------------------
// Shared utilities — used by mgmt, build, and runtime bootstrap modules
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
        },
      },
    },
    { provider },
  );
}

export function installCertManager(
  role: string,
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
): k8s.helm.v3.Release {
  return new k8s.helm.v3.Release(
    `${role}-cert-manager`,
    {
      name: `${role}-cert-manager`,
      chart: "cert-manager",
      version: HELM_VERSIONS.certManager,
      repositoryOpts: {
        repo: "https://charts.jetstack.io",
      },
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      // Wait for ALL cert-manager pods (including webhook) to be Ready.
      // Without this, the ClusterIssuer creation fails because the webhook
      // isn't serving yet: "http: server gave HTTP response to HTTPS client".
      // Critical for fresh deploys (destroy+recreate).
      waitForJobs: true,
      timeout: 300, // 5 min for webhook to become ready
      values: {
        installCRDs: true,
        global: {
          leaderElection: {
            namespace: PLATFORM_NAMESPACE,
          },
        },
        resources: {
          requests: { cpu: "50m", memory: "64Mi" },
          limits: { cpu: "200m", memory: "256Mi" },
        },
      },
    },
    { provider, dependsOn: [ns] },
  );
}

export function installTraefik(
  role: string,
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  certManager?: k8s.helm.v3.Release,
): k8s.helm.v3.Release {
  const dependsOn: pulumi.Resource[] = [ns];
  if (certManager) {
    dependsOn.push(certManager);
  }

  // Helm release name stays "mgmt-traefik" for both mgmt and runtime clusters.
  // K8s resources on the runtime cluster have annotation meta.helm.sh/release-name=mgmt-traefik.
  // Changing the release name requires deleting all Traefik resources first (Helm ownership).
  // The fullnameOverride differentiates K8s resource names: mgmt-traefik vs runtime-traefik.
  // TODO: Rename to ${role}-traefik on next destroy+recreate cycle.
  return new k8s.helm.v3.Release(
    `${role}-traefik`,
    {
      name: "mgmt-traefik",
      chart: "traefik",
      version: HELM_VERSIONS.traefik,
      repositoryOpts: {
        repo: "https://traefik.github.io/charts",
      },
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      values: {
        // Deterministic Service name so lookupLoadBalancerEndpoint can find it.
        // Without this, the chart template may produce an unexpected name.
        fullnameOverride: `${role}-traefik`,
        ports: {
          web: {
            port: 8000,
            expose: { default: true },
            exposedPort: 80,
          },
          websecure: {
            port: 8443,
            expose: { default: true },
            exposedPort: 443,
          },
        },
        service: {
          type: "LoadBalancer",
        },
        providers: {
          kubernetesCRD: { enabled: true },
          kubernetesIngress: { enabled: true },
        },
        resources: {
          requests: { cpu: "100m", memory: "128Mi" },
          limits: { cpu: "500m", memory: "512Mi" },
        },
      },
    },
    {
      provider,
      dependsOn,
    },
  );
}

/**
 * Reads the Traefik LoadBalancer Service to get the external endpoint.
 *
 * Pulumi's K8s provider waits for the LB to be assigned before
 * marking the Service as ready, so this output resolves only
 * after the cloud provider has allocated an IP/hostname.
 *
 * DigitalOcean returns an IP; AWS returns a hostname (ELB).
 */
export function lookupLoadBalancerEndpoint(
  role: string,
  provider: k8s.Provider,
  traefik: k8s.helm.v3.Release,
): pulumi.Output<string> {
  // Service name is deterministic: fullnameOverride="{role}-traefik"
  // is set in installTraefik, so the K8s Service name matches exactly.
  const svc = k8s.core.v1.Service.get(
    `${role}-traefik-lb-lookup`,
    pulumi.interpolate`${PLATFORM_NAMESPACE}/${role}-traefik`,
    { provider, dependsOn: [traefik] },
  );

  return svc.status.apply((status) => {
    const ingress = status?.loadBalancer?.ingress?.[0];
    return ingress?.ip ?? ingress?.hostname ?? "";
  });
}

// ---------------------------------------------------------------------------
// TLS — Let's Encrypt via cert-manager + Traefik IngressRoute
// ---------------------------------------------------------------------------

/**
 * Creates a ClusterIssuer for Let's Encrypt production certificates.
 * Uses DNS-01 challenge via Cloudflare DNS API when cfApiToken is available,
 * falling back to HTTP-01 via Traefik when not.
 *
 * DNS-01 is required when ingress is behind Cloudflare Tunnel (no public IP
 * for HTTP-01 validation). The Cloudflare API token needs Zone:DNS:Edit permission.
 */
export function createClusterIssuer(
  provider: k8s.Provider,
  certManager: k8s.helm.v3.Release,
  config: PlatformStackConfig,
  role: string = "mgmt",
): k8s.apiextensions.CustomResource {
  // Cloudflare API token secret for DNS-01 challenge.
  // Created on EVERY cluster with cert-manager — each cluster's ClusterIssuer
  // references this secret by name in the same namespace.
  if (config.cfApiToken) {
    new k8s.core.v1.Secret(
      `cloudflare-api-token-secret-${role}`,
      {
        metadata: {
          name: "cloudflare-api-token-secret",
          namespace: PLATFORM_NAMESPACE,
        },
        stringData: {
          "api-token": config.cfApiToken,
        },
      },
      { provider, dependsOn: [certManager] },
    );
  }

  // Dual solver strategy:
  // 1. DNS-01 via Cloudflare for platform domains (*.loopzero.example) — required behind CF Tunnel
  // 2. HTTP-01 via Traefik as catch-all for custom domains (e.g., aquim.ai on Hostinger)
  //
  // cert-manager selects the most specific matching solver per Certificate.
  // DNS-01 matches only the configured dnsZone; HTTP-01 catches everything else.
  // Without this, custom domains on non-Cloudflare DNS providers can never issue TLS.
  const solvers = config.cfApiToken
    ? [
        {
          selector: {
            dnsZones: [config.dnsZone],
          },
          dns01: {
            cloudflare: {
              apiTokenSecretRef: {
                name: "cloudflare-api-token-secret",
                key: "api-token",
              },
            },
          },
        },
        {
          selector: {},
          http01: {
            ingress: {
              class: "traefik",
            },
          },
        },
      ]
    : [
        {
          http01: {
            ingress: {
              class: "traefik",
            },
          },
        },
      ];

  // ROOT CAUSE FIX: The "http: server gave HTTP response to HTTPS client" error was
  // caused by the default-deny-ingress NetworkPolicy in platform-system blocking the
  // K8s API server from reaching the cert-manager webhook pod. The allow-cert-manager-webhook
  // NetworkPolicy in mgmt-bootstrap.ts fixes this by allowing ingress on port 10250.
  //
  // Previous misdiagnosis: We assumed this was a webhook TLS timing issue (cainjector
  // needing time to generate certs). It was actually a network issue — the webhook was
  // serving TLS correctly, but the K8s API server couldn't connect to it.
  return new k8s.apiextensions.CustomResource(
    `${role}-letsencrypt-prod`,
    {
      apiVersion: "cert-manager.io/v1",
      kind: "ClusterIssuer",
      metadata: {
        name: "letsencrypt-prod",
        labels: {
          "app.kubernetes.io/managed-by": "pulumi",
          "app.kubernetes.io/part-of": "loopzero-platform",
          "loopzero.io/environment": config.environment,
        },
      },
      spec: {
        acme: {
          email: `admin@${config.dnsZone}`,
          server: "https://acme-v02.api.letsencrypt.org/directory",
          privateKeySecretRef: {
            name: "letsencrypt-prod-account-key",
          },
          solvers,
        },
      },
    },
    {
      provider,
      dependsOn: [certManager],
      customTimeouts: { create: "5m", update: "2m" },
    },
  );
}
