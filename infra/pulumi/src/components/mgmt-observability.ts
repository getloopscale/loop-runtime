// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Observability orchestrator: Prometheus installation + re-exports from split modules.
// Split modules: mgmt-grafana.ts, mgmt-tempo-otel.ts

import * as k8s from "@pulumi/kubernetes";
import * as pulumi from "@pulumi/pulumi";
import { PLATFORM_NAMESPACE, HELM_VERSIONS } from "./bootstrap-shared";

// Re-exports for backward compatibility — consumers import from this file.
export { installGrafana, provisionGrafanaDashboards } from "./mgmt-grafana";
export { installTempo, installOtelCollector } from "./mgmt-tempo-otel";
export { installBlackboxExporter } from "./mgmt-blackbox";

export function installPrometheus(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  argoMetricsEndpoint: pulumi.Output<string>,
  runtimeKSMEndpoint?: pulumi.Output<string>,
  slackWebhookUrl?: pulumi.Output<string>,
  slackCriticalWebhookUrl?: pulumi.Output<string>,
): k8s.helm.v3.Release {
  // MEET-055 M1: Alertmanager Slack webhook Secret.
  const slackUrl = slackWebhookUrl ?? slackCriticalWebhookUrl;
  if (slackUrl) {
    new k8s.core.v1.Secret(
      "alertmanager-slack-webhooks",
      {
        metadata: {
          name: "alertmanager-slack-webhooks",
          namespace: "monitoring",
        },
        stringData: {
          "slack-webhook-url": slackWebhookUrl ?? pulumi.output(""),
          "slack-critical-webhook-url": slackCriticalWebhookUrl ?? slackWebhookUrl ?? pulumi.output(""),
        },
      },
      { provider },
    );
  }

  return new k8s.helm.v3.Release(
    "prometheus",
    {
      name: "prometheus",
      chart: "prometheus",
      version: HELM_VERSIONS.prometheus,
      repositoryOpts: {
        repo: "https://prometheus-community.github.io/helm-charts",
      },
      namespace: PLATFORM_NAMESPACE,
      timeout: 900,
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
        extraScrapeConfigs: pulumi.all([
          argoMetricsEndpoint,
          runtimeKSMEndpoint ?? pulumi.output(""),
        ]).apply(([argoTarget, ksmTarget]) => {
          let config = `
- job_name: argo-workflows
  scrape_interval: 15s
  scrape_timeout: 10s
  metrics_path: /metrics
  static_configs:
    - targets:
        - "${argoTarget}"
      labels:
        cluster: build
        component: argo-workflows

- job_name: harbor
  scrape_interval: 30s
  scrape_timeout: 10s
  metrics_path: /metrics
  static_configs:
    - targets:
        - "harbor-exporter.loopzero-system.svc.cluster.local:8001"
      labels:
        cluster: mgmt
        component: harbor

- job_name: gitea
  scrape_interval: 30s
  scrape_timeout: 10s
  metrics_path: /metrics
  static_configs:
    - targets:
        - "gitea-http.loopzero-system.svc.cluster.local:3000"
      labels:
        cluster: mgmt
        component: gitea
`;
          if (ksmTarget) {
            config += `
- job_name: runtime-kube-state-metrics
  scrape_interval: 30s
  scrape_timeout: 10s
  metrics_path: /metrics
  static_configs:
    - targets:
        - "${ksmTarget}"
      labels:
        cluster: runtime
        component: kube-state-metrics
`;
          }
          return config;
        }),
        serverFiles: {
          "alerting_rules.yml": {
            groups: [
              {
                name: "loopzero-platform",
                rules: [
                  {
                    alert: "LoopZeroAPIDown",
                    expr: 'up{job="loopzero-api"} == 0',
                    for: "1m",
                    labels: { severity: "critical" },
                    annotations: {
                      summary: "LoopZero API is down",
                      description: "LoopZero API has been unreachable for more than 1 minute.",
                    },
                  },
                  {
                    alert: "LoopZeroDeployFailing",
                    expr: 'increase(loopzero_api_request_duration_seconds_count{path="/api/v1/builds",status=~"5.."}[10m]) > 3',
                    for: "2m",
                    labels: { severity: "warning" },
                    annotations: {
                      summary: "Multiple deploy failures",
                      description: "More than 3 build/deploy failures in the last 10 minutes.",
                    },
                  },
                  {
                    alert: "LoopZeroHighAPILatency",
                    expr: 'histogram_quantile(0.95, rate(loopzero_api_request_duration_seconds_bucket{path!~"/api/v1/health.*|/api/v1/builds/.*/stream"}[5m])) > 2',
                    for: "5m",
                    labels: { severity: "warning" },
                    annotations: {
                      summary: "High API latency (P95 > 2s)",
                      description: "API P95 latency exceeds 2 seconds for 5 minutes.",
                    },
                  },
                  {
                    alert: "LoopZeroPodCrashLooping",
                    expr: 'rate(kube_pod_container_status_restarts_total{namespace=~"loopzero-.*"}[15m]) > 0',
                    for: "5m",
                    labels: { severity: "warning" },
                    annotations: {
                      summary: "Pod crash-looping in LoopZero namespace",
                      description: "{{ $labels.namespace }}/{{ $labels.pod }} is restarting frequently.",
                    },
                  },
                  {
                    alert: "HarborDown",
                    expr: 'up{job="harbor"} == 0',
                    for: "5m",
                    labels: { severity: "critical" },
                    annotations: {
                      summary: "Harbor registry is unreachable",
                      description: "Harbor exporter has been unreachable for more than 5 minutes. Tenant builds cannot push images.",
                    },
                  },
                  {
                    alert: "HarborStorageQuotaHigh",
                    expr: '(harbor_project_quota_usage_byte / harbor_project_quota_byte) > 0.8 and harbor_project_quota_byte > 0',
                    for: "10m",
                    labels: { severity: "warning" },
                    annotations: {
                      summary: "Harbor project storage > 80% quota",
                      description: "Project {{ $labels.project_name }} is using {{ $value | humanizePercentage }} of its storage quota.",
                    },
                  },
                  {
                    alert: "HarborGCFailing",
                    expr: 'harbor_jobservice_job_total{status="error",job_type="GARBAGE_COLLECTION"} > 0',
                    for: "24h",
                    labels: { severity: "warning" },
                    annotations: {
                      summary: "Harbor garbage collection has errors",
                      description: "Harbor GC job has been failing for more than 24 hours. Untagged images are not being cleaned up.",
                    },
                  },
                  {
                    alert: "HarborPGPoolExhaustion",
                    expr: 'count(pg_stat_activity{datname="harbor_db"}) > 8',
                    for: "5m",
                    labels: { severity: "critical" },
                    annotations: {
                      summary: "Harbor PG connections near cap (>8 of 10)",
                      description: "Harbor is using {{ $value }} PostgreSQL connections out of the 10-connection pool cap. If exhausted, LoopZero deploys will fail (pg_advisory_xact_lock timeout).",
                    },
                  },
                  {
                    alert: "HarborPushFailures",
                    expr: 'rate(harbor_core_http_request_total{code=~"5.."}[5m]) > 0',
                    for: "5m",
                    labels: { severity: "warning" },
                    annotations: {
                      summary: "Harbor registry returning 5xx errors",
                      description: "Harbor core is returning server errors at {{ $value | humanize }} req/s. Image push/pull operations may be failing.",
                    },
                  },
                  {
                    alert: "HarborCanaryFailing",
                    expr: 'kube_job_status_failed{namespace="loopzero-system",job_name=~"harbor-canary.*"} > 0',
                    for: "10m",
                    labels: { severity: "critical" },
                    annotations: {
                      summary: "Harbor canary test is failing",
                      description: "The Harbor canary CronJob has failed jobs for 10+ minutes. Registry may be unreachable for tenant builds.",
                    },
                  },
                ],
              },
            ],
          },
        },
        server: {
          retention: "15d",
          persistentVolume: {
            enabled: true,
            size: "10Gi",
          },
          resources: {
            requests: { cpu: "200m", memory: "512Mi" },
            limits: { cpu: "500m", memory: "1Gi" },
          },
        },
        "kube-state-metrics": {
          fullnameOverride: "kube-state-metrics",
        },
        "prometheus-node-exporter": {
          enabled: false,
        },
      },
    },
    { provider, dependsOn: [ns] },
  );
}
