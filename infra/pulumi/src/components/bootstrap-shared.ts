// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import * as command from "@pulumi/command";
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

  // Helm release name is "mgmt-traefik" for backward compatibility with existing
  // clusters. K8s resources (ServiceAccount, Deployment, etc.) have the annotation
  // meta.helm.sh/release-name=mgmt-traefik. Changing the release name causes Helm
  // to reject the install because the resources "belong" to a different release.
  //
  // The Pulumi resource name uses ${role} for uniqueness in the state file, but
  // the Helm release name on the cluster stays "mgmt-traefik" (both mgmt and runtime).
  // The fullnameOverride differentiates the K8s resource names (mgmt-traefik vs runtime-traefik).
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
      // MED-06 migration: Pulumi state may have this resource under the old name
      // "mgmt-traefik" (before the role-based rename). The alias tells Pulumi that
      // the old resource IS this resource — prevents delete+create replacement.
      aliases: [{ name: "mgmt-traefik" }],
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
  kubeconfig?: pulumi.Output<string>,
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

  // DNS-01 solver when Cloudflare token is available (required behind CF Tunnel).
  // Falls back to HTTP-01 for environments without Cloudflare.
  const solvers = config.cfApiToken
    ? [
        {
          dns01: {
            cloudflare: {
              apiTokenSecretRef: {
                name: "cloudflare-api-token-secret",
                key: "api-token",
              },
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

  // MEET-2026-04-01-025 Action #5: Deterministic cert-manager webhook readiness check.
  // The Helm release reports "deployed" before the webhook's self-signed TLS cert is ready.
  // Without this gate, the ClusterIssuer creation fails with:
  //   "http: server gave HTTP response to HTTPS client"
  // because the K8s API server calls the webhook over HTTPS but gets plain HTTP.
  //
  // This command uses `kubectl apply --dry-run=server` with a self-signed ClusterIssuer
  // to validate the webhook is responding. dry-run=server hits the validating webhook
  // without creating any real resources — the most reliable readiness signal.
  // It runs AFTER cert-manager Helm install and BEFORE the real ClusterIssuer creation.
  const dependsOn: pulumi.Resource[] = [certManager];

  if (kubeconfig) {
    const webhookReady = new command.local.Command(
      `${role}-cert-manager-webhook-ready`,
      {
        create: pulumi.interpolate`
export KUBECONFIG=$(mktemp)
cat > "$KUBECONFIG" <<'KUBEEOF'
${kubeconfig}
KUBEEOF
echo "Waiting for cert-manager webhook to serve TLS..."
for i in $(seq 1 60); do
  if kubectl apply --dry-run=server -f - 2>/dev/null <<'YAMLEOF'
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: webhook-readiness-probe
spec:
  selfSigned: {}
YAMLEOF
  then
    echo "cert-manager webhook is ready (attempt $i)"
    rm -f "$KUBECONFIG"
    exit 0
  fi
  echo "Attempt $i/60: webhook not ready, waiting 5s..."
  sleep 5
done
echo "FATAL: cert-manager webhook not ready after 5 minutes"
rm -f "$KUBECONFIG"
exit 1
        `,
        interpreter: ["/bin/bash", "-c"],
      },
      { dependsOn: [certManager] },
    );
    dependsOn.push(webhookReady);
  }

  // The ClusterIssuer depends on the cert-manager webhook being fully ready.
  // On fresh deploys, the webhook may not be serving when Pulumi tries to create
  // the CR. The webhookReady gate above ensures deterministic readiness.
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
      dependsOn,
      // Retry for up to 5 min — webhook may not be ready on fresh deploys.
      customTimeouts: { create: "5m", update: "2m" },
    },
  );
}
