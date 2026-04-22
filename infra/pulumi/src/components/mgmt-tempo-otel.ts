// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Tempo (distributed tracing) + OpenTelemetry Collector (trace pipeline).

import * as k8s from "@pulumi/kubernetes";
import * as pulumi from "@pulumi/pulumi";
import { PlatformStackConfig } from "../config/stack";
import { PLATFORM_NAMESPACE, HELM_VERSIONS } from "./bootstrap-shared";

// ---------------------------------------------------------------------------
// Tempo — distributed trace backend (MEET-2026-03-30-020)
// ---------------------------------------------------------------------------

export function installTempo(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  config: PlatformStackConfig,
  storageEndpoint?: pulumi.Output<string>,
): k8s.helm.v3.Release {
  const useS3 = !!config.tempoStorageBucket;
  const storageConfig: Record<string, unknown> = {
    trace: {
      backend: useS3 ? "s3" : "local",
      local: { path: "/var/tempo/traces" },
      wal: { path: "/var/tempo/wal" },
      ...(useS3 && storageEndpoint
        ? {
            s3: {
              bucket: config.tempoStorageBucket,
              endpoint: storageEndpoint.apply(e => e.replace("https://", "")),
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
