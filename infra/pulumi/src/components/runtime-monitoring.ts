// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Runtime cluster monitoring: Promtail, kube-state-metrics, NodePort.

import * as k8s from "@pulumi/kubernetes";
import * as pulumi from "@pulumi/pulumi";
import * as command from "@pulumi/command";
import { InfraOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";
import { HELM_VERSIONS } from "./bootstrap-shared";

export interface RuntimeMonitoringOutputs {
  readonly ksmEndpoint: pulumi.Output<string>;
}

export function installRuntimeMonitoring(
  provider: k8s.Provider,
  infra: InfraOutputs,
  config: PlatformStackConfig,
): RuntimeMonitoringOutputs {
  const runtimeMonitoringNs = new k8s.core.v1.Namespace(
    "runtime-monitoring",
    {
      metadata: {
        name: "monitoring",
        labels: {
          "app.kubernetes.io/managed-by": "pulumi",
          "app.kubernetes.io/part-of": "loopzero-platform",
          "pod-security.kubernetes.io/enforce": "privileged",
          "pod-security.kubernetes.io/warn": "restricted",
          "pod-security.kubernetes.io/audit": "restricted",
        },
      },
    },
    { provider },
  );

  // Promtail — ships runtime pod logs to centralized Loki on build cluster.
  if (config.lokiPushUrl) {
    new k8s.helm.v3.Release(
      "runtime-promtail",
      {
        name: "promtail",
        chart: "promtail",
        version: HELM_VERSIONS.promtail,
        forceUpdate: true,
        skipAwait: true,
        repositoryOpts: {
          repo: "https://grafana.github.io/helm-charts",
        },
        namespace: "monitoring",
        createNamespace: false,
        values: {
          config: {
            clients: [
              {
                url: `${config.lokiPushUrl}/loki/api/v1/push`,
                batchwait: "5s",
                batchsize: 1048576,
                backoff_config: {
                  max_retries: 20,
                  min_period: "500ms",
                  max_period: "5m",
                },
              },
            ],
          },
          resources: {
            requests: { cpu: "50m", memory: "64Mi" },
            limits: { cpu: "200m", memory: "128Mi" },
          },
        },
      },
      {
        provider,
        dependsOn: [runtimeMonitoringNs],
        customTimeouts: { create: "10m", update: "10m" },
      },
    );
  }

  // kube-state-metrics
  const ksm = new k8s.helm.v3.Release(
    "runtime-kube-state-metrics",
    {
      name: "kube-state-metrics",
      chart: "kube-state-metrics",
      version: HELM_VERSIONS.kubeStateMetrics,
      repositoryOpts: {
        repo: "https://prometheus-community.github.io/helm-charts",
      },
      namespace: "monitoring",
      createNamespace: false,
      values: {
        fullnameOverride: "kube-state-metrics",
        resources: {
          requests: { cpu: "50m", memory: "64Mi" },
          limits: { cpu: "100m", memory: "128Mi" },
        },
      },
    },
    { provider, dependsOn: [runtimeMonitoringNs] },
  );

  // NodePort for cross-cluster Prometheus scrape
  new k8s.core.v1.Service(
    "runtime-ksm-nodeport",
    {
      metadata: {
        name: "ksm-nodeport",
        namespace: "monitoring",
        labels: {
          "app.kubernetes.io/name": "kube-state-metrics",
          "app.kubernetes.io/component": "nodeport-cross-cluster",
        },
      },
      spec: {
        type: "NodePort",
        ports: [
          { port: 8080, targetPort: 8080, nodePort: 30092, protocol: "TCP", name: "metrics" },
        ],
        selector: { "app.kubernetes.io/name": "kube-state-metrics" },
      },
    },
    { provider, dependsOn: [ksm, runtimeMonitoringNs] },
  );

  // Resolve runtime cluster node IP for cross-cluster access
  const runtimeNodeIp = new command.local.Command(
    "resolve-runtime-node-ip",
    {
      create: pulumi.interpolate`kubectl --kubeconfig <(echo '${infra.runtimeCluster.kubeconfig}') get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}'`,
      interpreter: ["/bin/bash", "-c"],
      triggers: [infra.runtimeCluster.endpoint],
    },
  );
  const ksmEndpoint = runtimeNodeIp.stdout.apply(
    (ip: string) => ip.trim() ? `${ip.trim()}:30092` : "pending-runtime-ksm-ip:30092",
  );

  return { ksmEndpoint };
}
