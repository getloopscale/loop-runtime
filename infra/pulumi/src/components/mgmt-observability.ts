// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// REVIEW-2026-03-30: Extracted from mgmt-bootstrap.ts (ARCH-001 decomposition).
// Owns: Prometheus + Grafana installation on management cluster.

import * as k8s from "@pulumi/kubernetes";
import * as fs from "fs";
import * as path from "path";
import { PlatformStackConfig } from "../config/stack";
import { PLATFORM_NAMESPACE, HELM_VERSIONS } from "./bootstrap-shared";

export function installPrometheus(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
): k8s.helm.v3.Release {
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
                    expr: 'rate(kube_pod_container_status_restarts_total{namespace=~"loopzero-.*|platform-system"}[15m]) > 0',
                    for: "5m",
                    labels: { severity: "warning" },
                    annotations: {
                      summary: "Pod crash-looping in LoopZero namespace",
                      description: "{{ $labels.namespace }}/{{ $labels.pod }} is restarting frequently.",
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
          fullnameOverride: "node-exporter",
        },
      },
    },
    { provider, dependsOn: [ns] },
  );
}

export function installGrafana(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  config: PlatformStackConfig,
): k8s.helm.v3.Release {
  return new k8s.helm.v3.Release(
    "grafana",
    {
      name: "grafana",
      chart: "grafana",
      timeout: 600, // 10 min — Grafana can be slow on first deploy (PVC provisioning)
      version: HELM_VERSIONS.grafana,
      repositoryOpts: {
        repo: "https://grafana.github.io/helm-charts",
      },
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      values: {
        fullnameOverride: "grafana",
        service: { type: "ClusterIP", port: 3000 },
        datasources: {
          "datasources.yaml": {
            apiVersion: 1,
            datasources: [
              {
                name: "Prometheus",
                type: "prometheus",
                url: `http://prometheus-server.${PLATFORM_NAMESPACE}.svc.cluster.local:80`,
                access: "proxy",
                isDefault: true,
              },
              {
                // MEET-2026-03-30-020: Tempo datasource for distributed tracing
                name: "Tempo",
                type: "tempo",
                url: `http://tempo.${PLATFORM_NAMESPACE}.svc.cluster.local:3200`,
                access: "proxy",
                jsonData: {
                  // C2: Correlation-ID remains UUID v4; trace_id is a separate slog field.
                  // Grafana bridges trace→log via derived field query on trace_id.
                  tracesToLogsV2: {
                    datasourceUid: "loki",
                    filterByTraceID: false,
                    customQuery: true,
                    query: `{job="loopzero-api"} |= "\${__trace.traceId}"`,
                  },
                },
              },
            ],
          },
        },
        sidecar: {
          dashboards: {
            enabled: true,
            label: "grafana_dashboard",
            labelValue: "1",
            folder: "/var/lib/grafana/dashboards/sidecar",
            provider: {
              name: "sidecar",
              orgId: 1,
              folder: "LoopZero",
              type: "file",
              disableDeletion: false,
              editable: true,
            },
          },
        },
        dashboardProviders: {
          "dashboardproviders.yaml": {
            apiVersion: 1,
            providers: [
              {
                name: "default",
                orgId: 1,
                folder: "",
                type: "file",
                disableDeletion: false,
                editable: true,
                options: { path: "/var/lib/grafana/dashboards/default" },
              },
            ],
          },
        },
        dashboards: {
          default: {
            "k8s-cluster": { gnetId: 7249, revision: 1, datasource: "Prometheus" },
            "node-exporter": { gnetId: 1860, revision: 37, datasource: "Prometheus" },
            "k8s-pods": { gnetId: 6417, revision: 1, datasource: "Prometheus" },
          },
        },
        persistence: { enabled: true, size: "5Gi" },
        // MEET-2026-04-09-045: RWO PVC requires Recreate strategy.
        // RollingUpdate (default) causes multi-attach deadlock — new pod can't
        // mount the volume while old pod still holds it.
        strategy: { type: "Recreate" },
        adminUser: "admin",
        "grafana.ini": {
          server: {
            domain: `grafana.${config.domain}`,
            root_url: `https://grafana.${config.domain}/`,
            serve_from_sub_path: false,
          },
          "auth.anonymous": {
            enabled: config.environment !== "prod",
            org_role: "Viewer",
          },
        },
        resources: {
          requests: { cpu: "100m", memory: "128Mi" },
          limits: { cpu: "300m", memory: "256Mi" },
        },
      },
    },
    { provider, dependsOn: [ns] },
  );
}

// ---------------------------------------------------------------------------
// Grafana Dashboard ConfigMaps — auto-provisioned via sidecar
// ---------------------------------------------------------------------------

/**
 * Creates ConfigMaps for all LoopZero dashboard JSON files.
 * The Grafana sidecar container watches for ConfigMaps with label
 * `grafana_dashboard=1` and auto-loads them into the "LoopZero" folder.
 * This ensures dashboards survive `pulumi up` and pod restarts.
 */
export function provisionGrafanaDashboards(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  grafana: k8s.helm.v3.Release,
): void {
  const dashboardDir = path.resolve(__dirname, "../../../helm/monitoring/dashboards");

  if (!fs.existsSync(dashboardDir)) {
    return; // dashboards dir not found — skip (CI without infra checkout)
  }

  const files = fs.readdirSync(dashboardDir).filter((f) => f.endsWith(".json"));

  for (const file of files) {
    const name = `grafana-dash-${file.replace(".json", "")}`;
    const content = fs.readFileSync(path.join(dashboardDir, file), "utf-8");

    new k8s.core.v1.ConfigMap(
      name,
      {
        metadata: {
          name,
          namespace: PLATFORM_NAMESPACE,
          labels: {
            grafana_dashboard: "1",
            "app.kubernetes.io/managed-by": "pulumi",
          },
        },
        data: { [file]: content },
      },
      { provider, dependsOn: [ns, grafana] },
    );
  }
}

// ---------------------------------------------------------------------------
// Tempo — distributed trace backend (MEET-2026-03-30-020)
// ---------------------------------------------------------------------------

export function installTempo(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  config: PlatformStackConfig,
): k8s.helm.v3.Release {
  // Storage config: local filesystem for dev, S3 (DO Spaces) for prod.
  const useS3 = !!config.tempoStorageBucket;
  const storageConfig: Record<string, unknown> = {
    trace: {
      backend: useS3 ? "s3" : "local",
      local: { path: "/var/tempo/traces" },
      wal: { path: "/var/tempo/wal" },
      ...(useS3
        ? {
            s3: {
              bucket: config.tempoStorageBucket,
              endpoint: `${config.region}.digitaloceanspaces.com`,
              forcepathstyle: true,
            },
          }
        : {}),
    },
  };

  return new k8s.helm.v3.Release(
    "tempo",
    {
      name: "tempo",
      chart: "tempo",
      version: HELM_VERSIONS.tempo,
      repositoryOpts: {
        repo: "https://grafana.github.io/helm-charts",
      },
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      values: {
        fullnameOverride: "tempo",
        tempo: {
          receivers: {
            otlp: {
              protocols: {
                grpc: { endpoint: "0.0.0.0:4317" },
                http: { endpoint: "0.0.0.0:4318" },
              },
            },
          },
          retention: "168h",
          storage: storageConfig,
        },
        server: { http_listen_port: 3200 },
        persistence: { enabled: true, size: "5Gi" },
        resources: {
          requests: { cpu: "100m", memory: "256Mi" },
          limits: { cpu: "300m", memory: "512Mi" },
        },
      },
    },
    { provider, dependsOn: [ns] },
  );
}

// ---------------------------------------------------------------------------
// OpenTelemetry Collector — centralized trace pipeline
// ---------------------------------------------------------------------------

/**
 * Installs the OTEL Collector as a centralized trace pipeline.
 * Services send OTLP to the collector, which samples, batches, and
 * forwards to Tempo. Also exposes Prometheus metrics for collector health.
 *
 * Config from infra/helm/otel/values.yaml:
 * - Receivers: OTLP gRPC (4317) + HTTP (4318)
 * - Processors: probabilistic_sampler (100%, tunable) + batch (5s/1024)
 * - Exporters: Tempo (OTLP) + Prometheus (collector metrics)
 */
export function installOtelCollector(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  tempo: k8s.helm.v3.Release,
): k8s.helm.v3.Release {
  return new k8s.helm.v3.Release(
    "otel-collector",
    {
      name: "otel-collector",
      chart: "opentelemetry-collector",
      version: HELM_VERSIONS.otelCollector,
      repositoryOpts: {
        repo: "https://open-telemetry.github.io/opentelemetry-helm-charts",
      },
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      values: {
        fullnameOverride: "otel-collector",
        image: {
          repository: "otel/opentelemetry-collector-contrib",
          tag: HELM_VERSIONS.otelCollector,
        },
        mode: "deployment",
        securityContext: { runAsNonRoot: true },
        resources: {
          requests: { cpu: "50m", memory: "128Mi" },
          limits: { cpu: "200m", memory: "256Mi" },
        },
        livenessProbe: {
          httpGet: { path: "/", port: 13133 },
          initialDelaySeconds: 10,
          periodSeconds: 15,
        },
        readinessProbe: {
          httpGet: { path: "/", port: 13133 },
          initialDelaySeconds: 10,
          periodSeconds: 15,
        },
        config: {
          extensions: {
            health_check: { endpoint: "0.0.0.0:13133" },
          },
          receivers: {
            otlp: {
              protocols: {
                grpc: { endpoint: "0.0.0.0:4317" },
                http: { endpoint: "0.0.0.0:4318" },
              },
            },
          },
          processors: {
            batch: { timeout: "5s", send_batch_size: 1024 },
            probabilistic_sampler: { sampling_percentage: 100 },
          },
          exporters: {
            debug: { verbosity: "basic" },
            prometheus: { endpoint: "0.0.0.0:8889" },
            "otlp/tempo": {
              endpoint: `tempo.${PLATFORM_NAMESPACE}.svc:4317`,
              tls: { insecure: true },
            },
          },
          service: {
            extensions: ["health_check"],
            pipelines: {
              traces: {
                receivers: ["otlp"],
                processors: ["probabilistic_sampler", "batch"],
                exporters: ["otlp/tempo"],
              },
              metrics: {
                receivers: ["otlp"],
                processors: ["batch"],
                exporters: ["prometheus"],
              },
            },
          },
        },
        command: {
          extraArgs: ["--set=service.telemetry.metrics.address=0.0.0.0:8890"],
        },
        ports: {
          metrics: { enabled: true, containerPort: 8890, servicePort: 8890, protocol: "TCP" },
          otlp: { enabled: true, containerPort: 4317, servicePort: 4317, protocol: "TCP" },
          "otlp-http": { enabled: true, containerPort: 4318, servicePort: 4318, protocol: "TCP" },
          prometheus: { enabled: true, containerPort: 8889, servicePort: 8889, protocol: "TCP" },
        },
      },
    },
    { provider, dependsOn: [ns, tempo] },
  );
}
