// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";

const HELM_VERSION = "6.24.0";
const PROMTAIL_VERSION = "6.16.6";

export interface LokiArgs {
  provider: k8s.Provider;
  namespace: string;
  /** S3-compatible endpoint for log storage (e.g., "https://s3.nyc3.digitaloceanspaces.com") */
  s3Endpoint: pulumi.Output<string>;
  /** Bucket name for log storage */
  logsBucketName: pulumi.Output<string>;
  /** S3 access key */
  accessKeyId: pulumi.Output<string>;
  /** S3 secret key */
  secretAccessKey: pulumi.Output<string>;
}

/**
 * Loki + Promtail — log aggregation for build cluster.
 * Loki stores logs in S3 (DO Spaces). Promtail ships pod logs to Loki.
 * API queries Loki cross-cluster via NodePort 30100.
 */
export class Loki extends pulumi.ComponentResource {
  public readonly lokiRelease: k8s.helm.v3.Release;
  public readonly promtailRelease: k8s.helm.v3.Release;
  public readonly lokiEndpoint: pulumi.Output<string>;

  constructor(name: string, args: LokiArgs, opts?: pulumi.ComponentResourceOptions) {
    super("loopruntime:platform:Loki", name, {}, opts);

    const monitoringNs = new k8s.core.v1.Namespace(
      `${name}-monitoring`,
      {
        metadata: {
          name: args.namespace,
          labels: {
            "app.kubernetes.io/managed-by": "pulumi",
            "app.kubernetes.io/part-of": "loopruntime-platform",
            // Loki/Promtail need hostPath for log collection — privileged required.
            "pod-security.kubernetes.io/enforce": "privileged",
            "pod-security.kubernetes.io/warn": "restricted",
            "pod-security.kubernetes.io/audit": "restricted",
          },
        },
      },
      { provider: args.provider, parent: this },
    );

    this.lokiRelease = new k8s.helm.v3.Release(
      "loki",
      {
        name: "loki",
        chart: "loki",
        version: HELM_VERSION,
        repositoryOpts: { repo: "https://grafana.github.io/helm-charts" },
        namespace: args.namespace,
        createNamespace: false,
        values: {
          fullnameOverride: "loki",
          loki: {
            auth_enabled: false,
            commonConfig: { replication_factor: 1 },
            storage: {
              type: "s3",
              bucketNames: {
                chunks: args.logsBucketName,
                ruler: args.logsBucketName,
                admin: args.logsBucketName,
              },
              s3: {
                endpoint: args.s3Endpoint,
                bucketnames: args.logsBucketName,
                access_key_id: args.accessKeyId,
                secret_access_key: args.secretAccessKey,
                s3ForcePathStyle: true,
                insecure: false,
                region: "us-east-1",
              },
            },
            limits_config: {
              retention_period: "168h",
              max_query_series: 500,
            },
            schemaConfig: {
              configs: [{
                from: "2026-01-01",
                store: "tsdb",
                object_store: "s3",
                schema: "v13",
                index: { prefix: "loki_index_", period: "24h" },
              }],
            },
          },
          deploymentMode: "SingleBinary",
          singleBinary: {
            replicas: 1,
            persistence: { enabled: true, size: "10Gi" },
            resources: {
              requests: { cpu: "100m", memory: "256Mi" },
              limits: { cpu: "500m", memory: "512Mi" },
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
      { provider: args.provider, parent: this, dependsOn: [monitoringNs] },
    );

    // NodePort for cross-cluster access
    const lokiNodePort = new k8s.core.v1.Service(
      "loki-nodeport",
      {
        metadata: {
          name: "loki-nodeport",
          namespace: args.namespace,
          labels: {
            "app.kubernetes.io/name": "loki",
            "app.kubernetes.io/component": "nodeport-cross-cluster",
          },
        },
        spec: {
          type: "NodePort",
          ports: [{ port: 3100, targetPort: 3100, nodePort: 30100, protocol: "TCP", name: "http" }],
          selector: {
            "app.kubernetes.io/name": "loki",
            "app.kubernetes.io/component": "single-binary",
          },
        },
      },
      { provider: args.provider, parent: this, dependsOn: [this.lokiRelease, monitoringNs] },
    );

    // Promtail — ships pod logs to Loki
    this.promtailRelease = new k8s.helm.v3.Release(
      "promtail",
      {
        name: "promtail",
        chart: "promtail",
        version: PROMTAIL_VERSION,
        forceUpdate: true,
        skipAwait: true,
        repositoryOpts: { repo: "https://grafana.github.io/helm-charts" },
        namespace: args.namespace,
        createNamespace: false,
        values: {
          config: {
            clients: [{
              url: `http://loki.${args.namespace}.svc.cluster.local:3100/loki/api/v1/push`,
              batchwait: "5s",
              batchsize: 1048576,
            }],
          },
          resources: {
            requests: { cpu: "50m", memory: "64Mi" },
            limits: { cpu: "200m", memory: "128Mi" },
          },
        },
      },
      {
        provider: args.provider, parent: this,
        dependsOn: [monitoringNs, this.lokiRelease],
        customTimeouts: { create: "10m", update: "10m" },
      },
    );

    // Loki endpoint requires resolving build cluster node IP
    // This is exported for cross-cluster access from mgmt cluster
    this.lokiEndpoint = pulumi.output("http://pending:30100");

    this.registerOutputs({
      lokiEndpoint: this.lokiEndpoint,
    });
  }
}
