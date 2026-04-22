// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// External Secrets Operator (ESO): Helm, scoped SA, ClusterSecretStore.

import * as k8s from "@pulumi/kubernetes";
import { InfraOutputs } from "./infra";
import { PLATFORM_NAMESPACE } from "./bootstrap-shared";

/**
 * Installs ESO on the runtime cluster and configures cross-cluster secret sync from mgmt.
 * Creates a scoped ServiceAccount on mgmt cluster with read-only Secret access.
 */
export function installESO(
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
          "pod-security.kubernetes.io/enforce": "baseline",
          "pod-security.kubernetes.io/warn": "restricted",
          "pod-security.kubernetes.io/audit": "restricted",
        },
      },
    },
    { provider },
  );

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

  // Scoped ServiceAccount on mgmt cluster for ESO secret reading
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

  const scopedToken = esoMgmtToken.data.apply(
    (data) => data?.["token"] ? Buffer.from(data["token"], "base64").toString("utf-8") : "",
  );
  const scopedCa = esoMgmtToken.data.apply(
    (data) => data?.["ca.crt"] ? Buffer.from(data["ca.crt"]).toString("base64") : "",
  );

  // Store scoped token in runtime cluster for ESO
  new k8s.core.v1.Secret(
    "eso-mgmt-token",
    {
      metadata: { name: "mgmt-cluster-token", namespace: "external-secrets" },
      stringData: { token: scopedToken, ca: scopedCa },
    },
    { provider, dependsOn: [esoNs] },
  );

  // ClusterSecretStore — cross-cluster secret sync
  new k8s.apiextensions.CustomResource(
    "eso-cluster-secret-store",
    {
      apiVersion: "external-secrets.io/v1beta1",
      kind: "ClusterSecretStore",
      metadata: { name: "loopzero-mgmt-store" },
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
