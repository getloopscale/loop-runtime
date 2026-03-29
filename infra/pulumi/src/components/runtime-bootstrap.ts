// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import { InfraOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";
import {
  PLATFORM_NAMESPACE,
  ClusterBootstrapOutputs,
  createPlatformNamespace,
  installCertManager,
  installTraefik,
  lookupLoadBalancerEndpoint,
  createClusterIssuer,
} from "./bootstrap-shared";

// ---------------------------------------------------------------------------
// Runtime cluster bootstrap
// ---------------------------------------------------------------------------

export function bootstrapRuntimeCluster(
  infra: InfraOutputs,
  config: PlatformStackConfig,
): ClusterBootstrapOutputs {
  const provider = new k8s.Provider("runtime-k8s", {
    kubeconfig: infra.runtimeCluster.kubeconfig,
  });

  const ns = createPlatformNamespace("runtime", config, provider);

  // cert-manager — needed for Let's Encrypt TLS on tenant app IngressRoutes
  const certManager = installCertManager("runtime", provider, ns);

  // ClusterIssuer — Let's Encrypt production certs for tenant apps
  const runtimeClusterIssuer = createClusterIssuer(provider, certManager, config, "runtime");

  // Traefik — ingress for user apps, depends on cert-manager for TLS
  const traefik = installTraefik("runtime", provider, ns, certManager);

  const ingressEndpoint = lookupLoadBalancerEndpoint(
    "runtime",
    provider,
    traefik,
  );

  // External Secrets Operator — syncs secrets from mgmt cluster to runtime namespaces.
  // ESO runs on runtime (workload) cluster and reads from mgmt via kubeconfig.
  installESO(provider, infra);

  return {
    ingressReady: traefik.status.apply((s) => s.status === "deployed"),
    ingressEndpoint,
    platformNamespace: ns.metadata.name,
  };
}

// ---------------------------------------------------------------------------
// External Secrets Operator (ESO)
// ---------------------------------------------------------------------------

function installESO(
  provider: k8s.Provider,
  infra: InfraOutputs,
): void {
  const esoNs = new k8s.core.v1.Namespace(
    "runtime-external-secrets",
    {
      metadata: {
        name: "external-secrets",
        labels: {
          "app.kubernetes.io/managed-by": "pulumi",
          "app.kubernetes.io/part-of": "loopzero-platform",
        },
      },
    },
    { provider },
  );

  // ESO Helm chart
  const eso = new k8s.helm.v3.Release(
    "external-secrets",
    {
      name: "external-secrets",
      chart: "external-secrets",
      version: "0.14.3",
      repositoryOpts: {
        repo: "https://charts.external-secrets.io",
      },
      namespace: "external-secrets",
      createNamespace: false,
      values: {
        installCRDs: true,
        resources: {
          requests: { cpu: "50m", memory: "64Mi" },
          limits: { cpu: "200m", memory: "128Mi" },
        },
        webhook: { create: true },
        certController: { create: true },
      },
    },
    { provider, dependsOn: [esoNs] },
  );

  // Bearer token Secret — ESO authenticates to mgmt cluster API server.
  // The token is extracted from the mgmt cluster kubeconfig.
  // In production, use a dedicated ServiceAccount with minimal RBAC.
  new k8s.core.v1.Secret(
    "eso-mgmt-token",
    {
      metadata: {
        name: "mgmt-cluster-token",
        namespace: "external-secrets",
      },
      stringData: {
        // Extract token from kubeconfig (DOKS kubeconfigs contain bearer tokens)
        token: infra.mgmtCluster.kubeconfig.apply((kc) => {
          // Parse YAML kubeconfig to extract token
          const tokenMatch = kc.match(/token:\s*(.+)/);
          return tokenMatch ? tokenMatch[1].trim() : "";
        }),
        // Extract CA cert from kubeconfig
        ca: infra.mgmtCluster.kubeconfig.apply((kc) => {
          const caMatch = kc.match(/certificate-authority-data:\s*(.+)/);
          return caMatch ? caMatch[1].trim() : "";
        }),
      },
    },
    { provider, dependsOn: [esoNs] },
  );

  // ClusterSecretStore — tells ESO how to read secrets from mgmt cluster.
  // Uses bearer token auth + mgmt cluster API server URL.
  new k8s.apiextensions.CustomResource(
    "eso-cluster-secret-store",
    {
      apiVersion: "external-secrets.io/v1beta1",
      kind: "ClusterSecretStore",
      metadata: {
        name: "loopzero-mgmt-store",
      },
      spec: {
        provider: {
          kubernetes: {
            remoteNamespace: PLATFORM_NAMESPACE,
            server: {
              url: infra.mgmtCluster.endpoint,
              caBundle: infra.mgmtCluster.kubeconfig.apply((kc) => {
                const caMatch = kc.match(/certificate-authority-data:\s*(.+)/);
                return caMatch ? caMatch[1].trim() : "";
              }),
            },
            auth: {
              token: {
                bearerToken: {
                  name: "mgmt-cluster-token",
                  namespace: "external-secrets",
                  key: "token",
                },
              },
            },
          },
        },
      },
    },
    { provider, dependsOn: [eso] },
  );
}
