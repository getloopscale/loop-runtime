// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Extracted from mgmt-bootstrap.ts — Scoped kubeconfig creation for RBAC.

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import { ClusterOutputs } from "./infra";

export interface RbacRule {
  apiGroups: string[];
  resources: string[];
  verbs: string[];
}

/**
 * Creates a scoped kubeconfig for a remote cluster using a dedicated ServiceAccount
 * with minimal RBAC instead of the full admin kubeconfig.
 */
export function createScopedKubeconfig(
  role: string,
  cluster: ClusterOutputs,
  namespace: string,
  rules: RbacRule[],
): pulumi.Output<string> {
  const k8sProvider = new k8s.Provider(`${role}-scoped-k8s`, {
    kubeconfig: cluster.kubeconfig,
  });

  const nsResource = new k8s.core.v1.Namespace(
    `${role}-scoped-ns`,
    { metadata: { name: namespace } },
    { provider: k8sProvider, ignoreChanges: ["metadata"] },
  );

  const saName = `loopzero-${role}-sa`;

  const sa = new k8s.core.v1.ServiceAccount(
    `${role}-scoped-sa`,
    {
      metadata: { name: saName, namespace },
    },
    { provider: k8sProvider, dependsOn: [nsResource] },
  );

  const clusterRole = new k8s.rbac.v1.ClusterRole(
    `${role}-scoped-role`,
    {
      metadata: { name: saName },
      rules: rules.map((r) => ({
        apiGroups: r.apiGroups,
        resources: r.resources,
        verbs: r.verbs,
      })),
    },
    { provider: k8sProvider },
  );

  new k8s.rbac.v1.ClusterRoleBinding(
    `${role}-scoped-binding`,
    {
      metadata: { name: saName },
      subjects: [{ kind: "ServiceAccount", name: saName, namespace }],
      roleRef: {
        kind: "ClusterRole",
        name: saName,
        apiGroup: "rbac.authorization.k8s.io",
      },
    },
    { provider: k8sProvider, dependsOn: [clusterRole, sa] },
  );

  const tokenSecret = new k8s.core.v1.Secret(
    `${role}-scoped-token`,
    {
      metadata: {
        name: `${saName}-token`,
        namespace,
        annotations: { "kubernetes.io/service-account.name": saName },
      },
      type: "kubernetes.io/service-account-token",
    },
    { provider: k8sProvider, dependsOn: [sa] },
  );

  return pulumi
    .all([cluster.endpoint, tokenSecret.data])
    .apply(([endpoint, data]) => {
      const token = data?.["token"]
        ? Buffer.from(data["token"], "base64").toString("utf-8")
        : "";
      const ca = data?.["ca.crt"] ?? "";

      return `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: ${endpoint}
    certificate-authority-data: ${ca}
  name: ${role}
contexts:
- context:
    cluster: ${role}
    user: ${saName}
    namespace: ${namespace}
  name: ${role}
current-context: ${role}
users:
- name: ${saName}
  user:
    token: ${token}`;
    });
}
