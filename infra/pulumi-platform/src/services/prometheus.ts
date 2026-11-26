// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";

const HELM_VERSION = "25.27.0";

export interface PrometheusArgs {
  provider: k8s.Provider;
  namespace: string;
  /** Cross-cluster Argo Workflows metrics target (e.g., "http://10.x.x.x:30090") */
  argoMetricsEndpoint?: pulumi.Output<string>;
}

/**
 * Prometheus — metrics collection with kube-state-metrics, node-exporter,
 * and Alertmanager. Includes LoopRuntime-specific alert rules.
 */
export class Prometheus extends pulumi.ComponentResource {
  public readonly release: k8s.helm.v3.Release;
  public readonly ready: pulumi.Output<boolean>;

  constructor(name: string, args: PrometheusArgs, opts?: pulumi.ComponentResourceOptions) {
    super("loop-runtime:platform:Prometheus", name, {}, opts);

    const extraScrape = args.argoMetricsEndpoint
      ? args.argoMetricsEndpoint.apply((target) => `
- job_name: argo-workflows
  scrape_interval: 15s
  scrape_timeout: 10s
  metrics_path: /metrics
  static_configs:
    - targets:
        - "${target}"
      labels:
        cluster: build
        component: argo-workflows
`)
      : pulumi.output("");

    this.release = new k8s.helm.v3.Release(
      "prometheus",
      {
        name: "prometheus",
        chart: "prometheus",
        version: HELM_VERSION,
        repositoryOpts: { repo: "https://prometheus-community.github.io/helm-charts" },
        namespace: args.namespace,
        createNamespace: false,
        values: {
          fullnameOverride: "prometheus",
          alertmanager: {
            enabled: true,
            resources: {
              requests: { cpu: "50m", memory: "64Mi" },
              limits: { cpu: "100m", memory: "128Mi" },
            },
          },
          "prometheus-pushgateway": { enabled: false },
          extraScrapeConfigs: extraScrape,
          serverFiles: {
            "alerting_rules.yml": {
              groups: [{
                name: "loop-runtime-platform",
                rules: [
                  {
                    alert: "LoopRuntimeAPIDown",
                    expr: 'up{job="loop-runtime-api"} == 0',
                    for: "1m",
                    labels: { severity: "critical" },
                    annotations: {
                      summary: "LoopRuntime API is down",
                      description: "LoopRuntime API unreachable for 1+ minute.",
                    },
                  },
                  {
                    alert: "LoopRuntimeDeployFailing",
                    expr: 'increase(loop_runtime_api_request_duration_seconds_count{path="/api/v1/builds",status=~"5.."}[10m]) > 3',
                    for: "2m",
                    labels: { severity: "warning" },
                    annotations: {
                      summary: "Multiple deploy failures",
                      description: "3+ build failures in 10 minutes.",
                    },
                  },
                  {
                    alert: "LoopRuntimeHighAPILatency",
                    expr: 'histogram_quantile(0.95, rate(loop_runtime_api_request_duration_seconds_bucket{path!~"/api/v1/health.*|/api/v1/builds/.*/stream"}[5m])) > 2',
                    for: "5m",
                    labels: { severity: "warning" },
                    annotations: {
                      summary: "High API latency (P95 > 2s)",
                      description: "API P95 latency exceeds 2s for 5 minutes.",
                    },
                  },
                  {
                    alert: "LoopRuntimePodCrashLooping",
                    expr: 'rate(kube_pod_container_status_restarts_total{namespace=~"loop-runtime-.*|loop-runtime-system"}[15m]) > 0',
                    for: "5m",
                    labels: { severity: "warning" },
                    annotations: {
                      summary: "Pod crash-looping",
                      description: "{{ $labels.namespace }}/{{ $labels.pod }} restarting frequently.",
                    },
                  },
                  {
                    alert: "LoopRuntimeDBPoolSaturation",
                    expr: "(loop_runtime_postgres_connections_active / loop_runtime_postgres_pool_max_conns) > 0.8",
                    for: "5m",
                    labels: { severity: "warning" },
                    annotations: {
                      summary: "PostgreSQL pool >80% utilized",
                      description: "{{ $value | humanizePercentage }} pool active for 5+ min.",
                    },
                  },
                ],
              }],
            },
          },
          server: {
            retention: "15d",
            persistentVolume: { enabled: true, size: "10Gi" },
            resources: {
              requests: { cpu: "200m", memory: "512Mi" },
              limits: { cpu: "500m", memory: "1Gi" },
            },
          },
          "kube-state-metrics": { fullnameOverride: "kube-state-metrics" },
          // node-exporter needs hostNetwork/hostPID/hostPath — incompatible with PSA baseline.
          // Disabled in loop-runtime-system namespace. Deploy separately in kube-system if needed.
          "prometheus-node-exporter": { enabled: false },
        },
      },
      { provider: args.provider, parent: this },
    );

    this.ready = this.release.status.apply((s) => s.status === "deployed");
    this.registerOutputs({ ready: this.ready });
  }
}
