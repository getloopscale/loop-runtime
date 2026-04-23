// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Extracted from mgmt-bootstrap.ts — Platform IngressRoutes, API + Cloud gateways.

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import { PlatformStackConfig } from "../config/stack";
import { PLATFORM_NAMESPACE } from "./bootstrap-shared";

/** Platform subdomains that need TLS IngressRoutes on the management cluster. */
const PLATFORM_INGRESS_ROUTES = [
  { subdomain: "argocd", serviceName: "argocd-server", servicePort: 80 },
  { subdomain: "git", serviceName: "gitea-http", servicePort: 3000 },
  { subdomain: "grafana", serviceName: "grafana", servicePort: 3000 },
  { subdomain: "auth", serviceName: "ory-hydra-public", servicePort: 4444 },
] as const;

/** Creates TLS-terminated IngressRoutes for platform services. */
export function createPlatformIngressRoutes(
  provider: k8s.Provider,
  config: PlatformStackConfig,
  traefik: k8s.helm.v3.Release,
  clusterIssuer: k8s.apiextensions.CustomResource,
): pulumi.Output<string[]> {
  const createdRoutes: pulumi.Output<string>[] = [];

  for (const route of PLATFORM_INGRESS_ROUTES) {
    const fqdn = `${route.subdomain}.${config.domain}`;
    const certName = `tls-${route.subdomain}`;

    const cert = new k8s.apiextensions.CustomResource(
      `cert-${route.subdomain}`,
      {
        apiVersion: "cert-manager.io/v1",
        kind: "Certificate",
        metadata: { name: certName, namespace: PLATFORM_NAMESPACE },
        spec: {
          secretName: `${certName}-secret`,
          issuerRef: { name: "letsencrypt-prod", kind: "ClusterIssuer" },
          dnsNames: [fqdn],
        },
      },
      { provider, dependsOn: [clusterIssuer] },
    );

    const ingressRoute = new k8s.apiextensions.CustomResource(
      `ingress-${route.subdomain}`,
      {
        apiVersion: "traefik.io/v1alpha1",
        kind: "IngressRoute",
        metadata: {
          name: `${route.subdomain}-ingress`,
          namespace: PLATFORM_NAMESPACE,
          labels: {
            "app.kubernetes.io/managed-by": "pulumi",
            "app.kubernetes.io/part-of": "loopzero-platform",
          },
        },
        spec: {
          entryPoints: ["websecure"],
          routes: [
            {
              match: `Host(\`${fqdn}\`)`,
              kind: "Rule",
              services: [
                {
                  name: route.serviceName,
                  port: route.servicePort,
                  namespace: PLATFORM_NAMESPACE,
                },
              ],
            },
          ],
          tls: { secretName: `${certName}-secret` },
        },
      },
      { provider, dependsOn: [cert, traefik] },
    );

    createdRoutes.push(ingressRoute.metadata.name);
  }

  return pulumi.all(createdRoutes);
}

/** API Gateway — path-based routing under api.{domain}. */
export function createAPIGatewayRoute(
  provider: k8s.Provider,
  config: PlatformStackConfig,
  traefik: k8s.helm.v3.Release,
  clusterIssuer: k8s.apiextensions.CustomResource,
): void {
  const fqdn = `api.${config.domain}`;
  const certName = "tls-api";

  const cert = new k8s.apiextensions.CustomResource(
    "cert-api",
    {
      apiVersion: "cert-manager.io/v1",
      kind: "Certificate",
      metadata: { name: certName, namespace: PLATFORM_NAMESPACE },
      spec: {
        secretName: `${certName}-secret`,
        issuerRef: { name: "letsencrypt-prod", kind: "ClusterIssuer" },
        dnsNames: [fqdn],
      },
    },
    { provider, dependsOn: [clusterIssuer] },
  );

  new k8s.apiextensions.CustomResource(
    "ingress-api-gateway",
    {
      apiVersion: "traefik.io/v1alpha1",
      kind: "IngressRoute",
      metadata: {
        name: "api-gateway",
        namespace: PLATFORM_NAMESPACE,
        labels: {
          "app.kubernetes.io/managed-by": "pulumi",
          "app.kubernetes.io/part-of": "loopzero-platform",
        },
      },
      spec: {
        entryPoints: ["websecure"],
        routes: [
          {
            match: `Host(\`${fqdn}\`) && (PathPrefix(\`/v1/auth\`) || PathPrefix(\`/v1/billing\`) || PathPrefix(\`/v1/webhooks\`) || PathPrefix(\`/auth\`))`,
            kind: "Rule",
            priority: 100,
            services: [{ name: "loopzero-cloud", port: 13002, namespace: PLATFORM_NAMESPACE }],
          },
          {
            match: `Host(\`${fqdn}\`)`,
            kind: "Rule",
            priority: 1,
            services: [{ name: "loopzero-api", port: 8080, namespace: PLATFORM_NAMESPACE }],
          },
        ],
        tls: { secretName: `${certName}-secret` },
      },
    },
    { provider, dependsOn: [cert, traefik] },
  );
}

/** Cloud Gateway — browser-facing entry point under cloud.{domain}. */
export function createCloudGatewayRoute(
  provider: k8s.Provider,
  config: PlatformStackConfig,
  traefik: k8s.helm.v3.Release,
  clusterIssuer: k8s.apiextensions.CustomResource,
): void {
  const fqdn = `cloud.${config.domain}`;
  const certName = "tls-cloud";

  const cert = new k8s.apiextensions.CustomResource(
    "cert-cloud",
    {
      apiVersion: "cert-manager.io/v1",
      kind: "Certificate",
      metadata: { name: certName, namespace: PLATFORM_NAMESPACE },
      spec: {
        secretName: `${certName}-secret`,
        issuerRef: { name: "letsencrypt-prod", kind: "ClusterIssuer" },
        dnsNames: [fqdn],
      },
    },
    { provider, dependsOn: [clusterIssuer] },
  );

  new k8s.apiextensions.CustomResource(
    "ingress-cloud-gateway",
    {
      apiVersion: "traefik.io/v1alpha1",
      kind: "IngressRoute",
      metadata: {
        name: "cloud-gateway",
        namespace: PLATFORM_NAMESPACE,
        labels: {
          "app.kubernetes.io/managed-by": "pulumi",
          "app.kubernetes.io/part-of": "loopzero-platform",
        },
      },
      spec: {
        entryPoints: ["websecure"],
        routes: [
          {
            match: `Host(\`${fqdn}\`) && (PathPrefix(\`/oauth2\`) || PathPrefix(\`/.well-known\`))`,
            kind: "Rule",
            priority: 300,
            services: [{ name: "ory-hydra-public", port: 4444, namespace: PLATFORM_NAMESPACE }],
          },
          {
            match: `Host(\`${fqdn}\`) && (PathPrefix(\`/self-service\`) || PathPrefix(\`/sessions\`) || PathPrefix(\`/schemas\`))`,
            kind: "Rule",
            priority: 200,
            services: [{ name: "ory-kratos-public", port: 80, namespace: PLATFORM_NAMESPACE }],
          },
          {
            match: `Host(\`${fqdn}\`) && (PathPrefix(\`/v1/auth\`) || PathPrefix(\`/v1/billing\`) || PathPrefix(\`/v1/webhooks\`) || PathPrefix(\`/auth\`) || PathPrefix(\`/engine\`) || PathPrefix(\`/metrics\`) || PathPrefix(\`/health\`))`,
            kind: "Rule",
            priority: 100,
            services: [{ name: "loopzero-cloud", port: 13002, namespace: PLATFORM_NAMESPACE }],
          },
          {
            match: `Host(\`${fqdn}\`)`,
            kind: "Rule",
            priority: 1,
            services: [{ name: "loopzero-dashboard", port: 80, namespace: PLATFORM_NAMESPACE }],
          },
        ],
        tls: { secretName: `${certName}-secret` },
      },
    },
    { provider, dependsOn: [cert, traefik] },
  );
}
