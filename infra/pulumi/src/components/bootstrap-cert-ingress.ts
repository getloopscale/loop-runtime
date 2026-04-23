// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// TLS + Ingress: cert-manager, Traefik, LoadBalancer lookup, ClusterIssuer.

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import { PlatformStackConfig } from "../config/stack";
import { PLATFORM_NAMESPACE, HELM_VERSIONS } from "./bootstrap-shared";

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
      waitForJobs: true,
      timeout: 300,
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
        service: { type: "LoadBalancer" },
        providers: {
          kubernetesCRD: { enabled: true },
          kubernetesIngress: { enabled: true },
        },
        resources: {
          requests: { cpu: "100m", memory: "128Mi" },
          limits: { cpu: "500m", memory: "512Mi" },
        },
        deployment: { replicas: 2 },
        podDisruptionBudget: { enabled: true, minAvailable: 1 },
        topologySpreadConstraints: [
          {
            maxSkew: 1,
            topologyKey: "kubernetes.io/hostname",
            whenUnsatisfiable: "ScheduleAnyway",
            labelSelector: {
              matchLabels: { "app.kubernetes.io/name": "traefik" },
            },
          },
        ],
        logs: {
          general: { format: "json" },
          access: { enabled: true, format: "json" },
        },
      },
    },
    { provider, dependsOn },
  );
}

/**
 * Reads the Traefik LoadBalancer Service to get the external endpoint.
 * DigitalOcean returns an IP; AWS returns a hostname (ELB).
 */
export function lookupLoadBalancerEndpoint(
  role: string,
  provider: k8s.Provider,
  traefik: k8s.helm.v3.Release,
): pulumi.Output<string> {
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

/**
 * Creates a ClusterIssuer for Let's Encrypt production certificates.
 * Uses DNS-01 challenge via Cloudflare DNS API when cfApiToken is available,
 * falling back to HTTP-01 via Traefik when not.
 */
export function createClusterIssuer(
  provider: k8s.Provider,
  certManager: k8s.helm.v3.Release,
  config: PlatformStackConfig,
  role: string = "mgmt",
): k8s.apiextensions.CustomResource {
  if (config.cfApiToken) {
    new k8s.core.v1.Secret(
      `cloudflare-api-token-secret-${role}`,
      {
        metadata: {
          name: "cloudflare-api-token-secret",
          namespace: PLATFORM_NAMESPACE,
        },
        stringData: { "api-token": config.cfApiToken },
      },
      { provider, dependsOn: [certManager] },
    );
  }

  const solvers = config.cfApiToken
    ? [
        {
          selector: { dnsZones: [config.dnsZone] },
          dns01: {
            cloudflare: {
              apiTokenSecretRef: { name: "cloudflare-api-token-secret", key: "api-token" },
            },
          },
        },
        {
          selector: {},
          http01: { ingress: { class: "traefik" } },
        },
      ]
    : [
        {
          http01: { ingress: { class: "traefik" } },
        },
      ];

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
          privateKeySecretRef: { name: "letsencrypt-prod-account-key" },
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
