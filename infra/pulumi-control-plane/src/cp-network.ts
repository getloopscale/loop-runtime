// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Network: NetworkPolicies, scoped kubeconfig creation, runtime cluster registration.

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import {
  PLATFORM_NAMESPACE,
  InfraOutputs,
  ClusterOutputs,
  RbacRule,
  parseKubeconfig,
} from "./mgmt-control-plane-services";

export function createPlatformNetworkPolicies(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
): void {
  // Intra-namespace + external webhook access
  new k8s.networking.v1.NetworkPolicy(
    "platform-network-policy",
    {
      metadata: { name: "platform-network-policy", namespace: PLATFORM_NAMESPACE },
      spec: {
        podSelector: {},
        policyTypes: ["Ingress"],
        ingress: [
          { from: [{ podSelector: {} }] },
          {
            ports: [
              { port: 10250, protocol: "TCP" },
              { port: 8443, protocol: "TCP" },
              { port: 8000, protocol: "TCP" },
              { port: 8080, protocol: "TCP" },
              { port: 3000, protocol: "TCP" },
              { port: 9090, protocol: "TCP" },
              { port: 9402, protocol: "TCP" },
            ],
          },
        ],
      },
    },
    { provider, dependsOn: [ns] },
  );

  // Egress: DNS, K8s API, PostgreSQL, HTTPS, cross-cluster
  new k8s.networking.v1.NetworkPolicy(
    "platform-egress-policy",
    {
      metadata: { name: "platform-egress-policy", namespace: PLATFORM_NAMESPACE },
      spec: {
        podSelector: {},
        policyTypes: ["Egress"],
        egress: [
          { ports: [{ port: 53, protocol: "UDP" }, { port: 53, protocol: "TCP" }] },
          { ports: [{ port: 443, protocol: "TCP" }, { port: 6443, protocol: "TCP" }] },
          { ports: [{ port: 25060, protocol: "TCP" }] },
          { ports: [{ port: 80, protocol: "TCP" }, { port: 8080, protocol: "TCP" }, { port: 9000, protocol: "TCP" }] },
          { ports: [{ port: 30100, protocol: "TCP" }, { port: 30090, protocol: "TCP" }] },
          { to: [{ podSelector: {} }] },
        ],
      },
    },
    { provider, dependsOn: [ns] },
  );
}

export function registerRuntimeCluster(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  argocd: k8s.helm.v3.Release,
): void {
  const argocdConfig = infra.runtimeCluster.kubeconfig.apply((kc: string) => {
    const parsed = parseKubeconfig(kc);
    return JSON.stringify({
      bearerToken: parsed.token,
      tlsClientConfig: { caData: parsed.certificateAuthorityData },
    });
  });

  new k8s.core.v1.Secret(
    "argocd-runtime-cluster",
    {
      metadata: {
        name: "runtime-cluster",
        namespace: PLATFORM_NAMESPACE,
        labels: { "argocd.argoproj.io/secret-type": "cluster" },
      },
      stringData: {
        name: "runtime",
        server: infra.runtimeCluster.endpoint,
        config: argocdConfig,
      },
    },
    { provider, dependsOn: [ns, argocd] },
  );
}

export function createScopedKubeconfig(
  role: string,
  cluster: ClusterOutputs,
  namespace: string,
  rules: RbacRule[],
): pulumi.Output<string> {
  const k8sProvider = new k8s.Provider(`${role}-scoped-k8s`, {
    kubeconfig: cluster.kubeconfig,
  });

  const saName = `loopzero-${role}-sa`;

  const sa = new k8s.core.v1.ServiceAccount(
    `${role}-scoped-sa`,
    { metadata: { name: saName, namespace } },
    { provider: k8sProvider },
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
      roleRef: { kind: "ClusterRole", name: saName, apiGroup: "rbac.authorization.k8s.io" },
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
