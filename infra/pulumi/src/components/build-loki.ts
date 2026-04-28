// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Build cluster: Loki + Promtail + NodePort for log aggregation.

import * as k8s from "@pulumi/kubernetes";
import { InfraOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";
import { HELM_VERSIONS } from "./bootstrap-shared";

export interface BuildLokiOutputs {
  readonly loki: k8s.helm.v3.Release;
  readonly monitoringNs: k8s.core.v1.Namespace;
  readonly lokiNodePort: k8s.core.v1.Service;
}

export function installBuildLoki(
  provider: k8s.Provider,
  infra: InfraOutputs,
  config: PlatformStackConfig,
): BuildLokiOutputs {
  const monitoringNs = new k8s.core.v1.Namespace(
    "build-monitoring",
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

  // Loki — log aggregation, S3 storage, NodePort for cross-cluster access by API
  const loki = new k8s.helm.v3.Release(
    "loki",
    {
      name: "loki",
      chart: "loki",
      version: HELM_VERSIONS.loki,
      repositoryOpts: {
        repo: "https://grafana.github.io/helm-charts",
      },
      namespace: "monitoring",
      createNamespace: false,
      values: {
        fullnameOverride: "loki",
        loki: {
          auth_enabled: false,
          commonConfig: { replication_factor: 1 },
          storage: {
            type: "s3",
            bucketNames: {
              chunks: infra.logsBucketName,
              ruler: infra.logsBucketName,
              admin: infra.logsBucketName,
            },
            s3: {
              endpoint: infra.s3Endpoint,
              bucketnames: infra.logsBucketName,
              access_key_id: config.spacesAccessKeyId,
              secret_access_key: config.spacesSecretAccessKey,
              s3ForcePathStyle: true,
              insecure: false,
              region: "us-east-1",
            },
          },
          limits_config: {
            retention_period: "168h",
            max_query_series: 500,
            // CHG-002b 2026-04-27 — L3 stream cap fix. Default 5000
            // streams/tenant saturated within ~3 L3 cycles. New cap 100k
            // gives headroom for L4 (500 tenants).
            max_global_streams_per_user: 100000,
            max_streams_per_user: 0,
            ingestion_rate_mb: 16,
            ingestion_burst_size_mb: 32,
            max_entries_limit_per_query: 10000,
            per_stream_rate_limit: "10MB",
            per_stream_rate_limit_burst: "20MB",
          },
          ingester: {
            // Flush idle chunks every 15min (default 1h) so streams release
            // memory faster — critical for L3 churn (100 short-lived workflows).
            chunk_idle_period: "15m",
            chunk_target_size: 1572864,
            max_chunk_age: "1h",
          },
          schemaConfig: {
            configs: [
              {
                from: "2026-01-01",
                store: "tsdb",
                object_store: "s3",
                schema: "v13",
                index: { prefix: "loki_index_", period: "24h" },
              },
            ],
          },
        },
        deploymentMode: "SingleBinary",
        singleBinary: {
          replicas: 1,
          persistence: { enabled: true, size: "10Gi" },
          resources: {
            // CHG-002b 2026-04-27 — was 256Mi/512Mi (sized for <100 builds/day).
            // L3 saturated even before the stream cap due to insufficient memory
            // for active chunks. New ceiling 2Gi gives headroom for L4.
            requests: { cpu: "200m", memory: "512Mi" },
            limits: { cpu: "2000m", memory: "2Gi" },
          },
        },
        read: { replicas: 0 },
        write: { replicas: 0 },
        backend: { replicas: 0 },
        gateway: { enabled: false },
        monitoring: {
          selfMonitoring: { enabled: false },
          lokiCanary: { enabled: false },
        },
        test: { enabled: false },
        chunksCache: { enabled: false },
        resultsCache: { enabled: false },
      },
    },
    { provider, dependsOn: [monitoringNs] },
  );

  // NodePort for cross-cluster access from mgmt API
  const lokiNodePort = new k8s.core.v1.Service(
    "loki-nodeport",
    {
      metadata: {
        name: "loki-nodeport",
        namespace: "monitoring",
        labels: {
          "app.kubernetes.io/name": "loki",
          "app.kubernetes.io/component": "nodeport-cross-cluster",
        },
      },
      spec: {
        type: "NodePort",
        ports: [
          { port: 3100, targetPort: 3100, nodePort: 30100, protocol: "TCP", name: "http" },
        ],
        selector: {
          "app.kubernetes.io/name": "loki",
          "app.kubernetes.io/component": "single-binary",
        },
      },
    },
    { provider, dependsOn: [loki, monitoringNs] },
  );

  // Promtail — ships Argo Workflows pod logs to Loki (same cluster)
  new k8s.helm.v3.Release(
    "promtail",
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
              url: "http://loki.monitoring.svc.cluster.local:3100/loki/api/v1/push",
              batchwait: "5s",
              batchsize: 1048576,
              backoff_config: {
                max_retries: 20,
                min_period: "500ms",
                max_period: "5m",
              },
            },
          ],
          snippets: {
            scrapeConfigs: `
- job_name: argo-workflow-pods
  kubernetes_sd_configs:
    - role: pod
  relabel_configs:
    - source_labels: [__meta_kubernetes_pod_label_workflows_argoproj_io_workflow]
      action: keep
      regex: .+
    - source_labels: [__meta_kubernetes_pod_uid, __meta_kubernetes_pod_container_name]
      target_label: __path__
      separator: /
      replacement: /var/log/pods/*$1/$2/*.log
    - source_labels: [__meta_kubernetes_pod_label_workflows_argoproj_io_workflow]
      target_label: workflow
    - source_labels: [__meta_kubernetes_pod_label_workflows_argoproj_io_node_name]
      target_label: step
    - source_labels: [__meta_kubernetes_namespace]
      target_label: namespace
    - source_labels: [__meta_kubernetes_pod_name]
      target_label: pod
    - source_labels: [__meta_kubernetes_pod_container_name]
      target_label: container`,
          },
        },
        resources: {
          requests: { cpu: "50m", memory: "64Mi" },
          limits: { cpu: "200m", memory: "128Mi" },
        },
      },
    },
    {
      provider,
      dependsOn: [monitoringNs, loki],
      customTimeouts: { create: "10m", update: "10m" },
    },
  );

  return { loki, monitoringNs, lokiNodePort };
}
