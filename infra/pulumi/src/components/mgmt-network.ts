// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Extracted from mgmt-bootstrap.ts — Network policies + runtime cluster registration.

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import { InfraOutputs } from "./infra";
import { PLATFORM_NAMESPACE, parseKubeconfig } from "./bootstrap-shared";

/** Creates NetworkPolicies for the platform namespace. */
export function createPlatformNetworkPolicies(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
): void {
  new k8s.networking.v1.NetworkPolicy(
    "platform-network-policy",
    {
      metadata: {
        name: "platform-network-policy",
        namespace: PLATFORM_NAMESPACE,
      },
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

  new k8s.networking.v1.NetworkPolicy(
    "platform-egress-policy",
    {
      metadata: {
        name: "platform-egress-policy",
        namespace: PLATFORM_NAMESPACE,
      },
      spec: {
        podSelector: {},
        policyTypes: ["Egress"],
        egress: [
          { ports: [{ port: 53, protocol: "UDP" }, { port: 53, protocol: "TCP" }] },
          { ports: [{ port: 443, protocol: "TCP" }, { port: 6443, protocol: "TCP" }] },
          { ports: [{ port: 25060, protocol: "TCP" }] },
          { ports: [{ port: 80, protocol: "TCP" }, { port: 8080, protocol: "TCP" }, { port: 9000, protocol: "TCP" }] },
          { ports: [{ port: 30100, protocol: "TCP" }, { port: 30090, protocol: "TCP" }, { port: 30092, protocol: "TCP" }] },
          { to: [{ podSelector: {} }] },
        ],
      },
    },
    { provider, dependsOn: [ns] },
  );
}

/** Registers the runtime cluster in ArgoCD as a deploy target. */
export function registerRuntimeCluster(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  argocd: k8s.helm.v3.Release,
): void {
  const argocdConfig = infra.runtimeCluster.kubeconfig.apply((kc) => {
    const parsed = parseKubeconfig(kc);
    return JSON.stringify({
      bearerToken: parsed.token,
      tlsClientConfig: {
        caData: parsed.certificateAuthorityData,
      },
    });
  });

  new k8s.core.v1.Secret(
    "argocd-runtime-cluster",
    {
      metadata: {
        name: "runtime-cluster",
        namespace: PLATFORM_NAMESPACE,
        labels: {
          "argocd.argoproj.io/secret-type": "cluster",
        },
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
