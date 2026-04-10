// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as k8s from "@pulumi/kubernetes";
import { InfraOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";
import {
  PLATFORM_NAMESPACE,
  HELM_VERSIONS,
  ClusterBootstrapOutputs,
  createPlatformNamespace,
  installCertManager,
  installTraefik,
  lookupLoadBalancerEndpoint,
  createClusterIssuer,
  installMetricsServer,
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

  // MEET-2026-04-09-045: metrics-server for kubectl top + HPA
  installMetricsServer("runtime", provider);

  // cert-manager — needed for Let's Encrypt TLS on tenant app IngressRoutes
  const certManager = installCertManager("runtime", provider, ns);

  // ClusterIssuer — Let's Encrypt production certs for tenant apps
  // Webhook readiness gate via Deployment.get() in createClusterIssuer (MEET-2026-04-01-025 #5)
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

  // MEET-2026-04-04-030 Phase 3.1: RBAC for loopzero-api to create ExternalSecret CRs
  // on the runtime cluster. The SecretK8sManager (api/internal/k8s/secret_k8s_manager.go)
  // creates ExternalSecret CRs in tenant namespaces when `loopzero secret set` is called.
  // The scoped kubeconfig SA (loopzero-runtime-sa) needs permissions for ESO CRDs.
  const esoWriterRole = new k8s.rbac.v1.ClusterRole(
    "loopzero-eso-writer",
    {
      metadata: { name: "loopzero-eso-writer" },
      rules: [
        {
          apiGroups: ["external-secrets.io"],
          resources: ["externalsecrets"],
          verbs: ["get", "list", "create", "update", "patch", "delete"],
        },
        // Also need K8s Secret read to verify sync status
        {
          apiGroups: [""],
          resources: ["secrets"],
          verbs: ["get", "list", "create", "update", "patch", "delete"],
        },
      ],
    },
    { provider },
  );

  new k8s.rbac.v1.ClusterRoleBinding(
    "loopzero-eso-writer",
    {
      metadata: { name: "loopzero-eso-writer" },
      subjects: [
        { kind: "ServiceAccount", name: "loopzero-runtime-sa", namespace: PLATFORM_NAMESPACE },
      ],
      roleRef: { kind: "ClusterRole", name: "loopzero-eso-writer", apiGroup: "rbac.authorization.k8s.io" },
    },
    { provider, dependsOn: [esoWriterRole] },
  );

  // Promtail — installed manually via Helm (not Pulumi).
  // The Pulumi Kubernetes provider crashes with gRPC connection closing
  // when managing Helm releases on the runtime cluster during node pool
  // resize operations. Promtail is already running on the runtime cluster
  // (installed in earlier deploys) and doesn't need Pulumi management.
  //
  // Manual install:
  //   helm install promtail grafana/promtail -n monitoring --create-namespace \
  //     --set config.clients[0].url=http://10.20.0.6:30100/loki/api/v1/push
  //
  // TODO: Re-add to Pulumi when provider bug is fixed or when migrating
  // to a newer pulumi-kubernetes version.

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

  // REVIEW-2026-03-30: Fixes HIGH-04 — was using admin kubeconfig token.
  // Now creates a dedicated ServiceAccount on mgmt cluster with read-only
  // access to Secrets in platform-system namespace only.
  const mgmtProvider = new k8s.Provider("eso-mgmt-k8s", {
    kubeconfig: infra.mgmtCluster.kubeconfig,
  });

  const esoSaName = "eso-secret-reader";
  const esoSa = new k8s.core.v1.ServiceAccount(
    "eso-mgmt-sa",
    { metadata: { name: esoSaName, namespace: PLATFORM_NAMESPACE } },
    { provider: mgmtProvider },
  );

  const esoRole = new k8s.rbac.v1.Role(
    "eso-mgmt-role",
    {
      metadata: { name: esoSaName, namespace: PLATFORM_NAMESPACE },
      rules: [
        // ESO only needs to read Secrets from platform-system namespace
        { apiGroups: [""], resources: ["secrets"], verbs: ["get", "list", "watch"] },
      ],
    },
    { provider: mgmtProvider },
  );

  new k8s.rbac.v1.RoleBinding(
    "eso-mgmt-binding",
    {
      metadata: { name: esoSaName, namespace: PLATFORM_NAMESPACE },
      subjects: [{ kind: "ServiceAccount", name: esoSaName, namespace: PLATFORM_NAMESPACE }],
      roleRef: { kind: "Role", name: esoSaName, apiGroup: "rbac.authorization.k8s.io" },
    },
    { provider: mgmtProvider, dependsOn: [esoRole, esoSa] },
  );

  // Long-lived token for the scoped SA
  const esoMgmtToken = new k8s.core.v1.Secret(
    "eso-mgmt-sa-token",
    {
      metadata: {
        name: `${esoSaName}-token`,
        namespace: PLATFORM_NAMESPACE,
        annotations: { "kubernetes.io/service-account.name": esoSaName },
      },
      type: "kubernetes.io/service-account-token",
    },
    { provider: mgmtProvider, dependsOn: [esoSa] },
  );

  // Extract scoped token + CA from the SA token Secret
  const scopedToken = esoMgmtToken.data.apply(
    (data) => data?.["token"] ? Buffer.from(data["token"], "base64").toString("utf-8") : "",
  );
  const scopedCa = esoMgmtToken.data.apply(
    (data) => data?.["ca.crt"] ? Buffer.from(data["ca.crt"]).toString("base64") : "",
  );

  // Store scoped token in runtime cluster for ESO to use
  new k8s.core.v1.Secret(
    "eso-mgmt-token",
    {
      metadata: {
        name: "mgmt-cluster-token",
        namespace: "external-secrets",
      },
      stringData: {
        token: scopedToken,
        ca: scopedCa,
      },
    },
    { provider, dependsOn: [esoNs] },
  );

  // ClusterSecretStore — tells ESO how to read secrets from mgmt cluster.
  // Uses scoped bearer token auth + mgmt cluster API server URL.
  // NOTE (MED-03): ExternalSecret consumers are created dynamically by the
  // TenantController when tenants are provisioned. The ClusterSecretStore is
  // the infrastructure foundation; consumers are created at runtime.
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
              caBundle: scopedCa,
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
