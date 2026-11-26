// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";

const HELM_VERSION = "1.14.0";
const OTEL_VERSION = "0.96.0";

export interface TempoArgs {
  provider: k8s.Provider;
  namespace: string;
}

/**
 * Tempo — distributed trace backend.
 * Receives traces via OTLP from the OTEL Collector.
 * Uses local filesystem storage (S3 backend configurable via tempoStorageBucket).
 */
export class Tempo extends pulumi.ComponentResource {
  public readonly release: k8s.helm.v3.Release;

  constructor(name: string, args: TempoArgs, opts?: pulumi.ComponentResourceOptions) {
    super("loopruntime:platform:Tempo", name, {}, opts);

    this.release = new k8s.helm.v3.Release(
      "tempo",
      {
        name: "tempo",
        chart: "tempo",
        version: HELM_VERSION,
        repositoryOpts: { repo: "https://grafana.github.io/helm-charts" },
        namespace: args.namespace,
        createNamespace: false,
        values: {
          fullnameOverride: "tempo",
          tempo: {
            storage: {
              trace: {
                backend: "local",
                local: { path: "/var/tempo/traces" },
                wal: { path: "/var/tempo/wal" },
              },
            },
            receivers: {
              otlp: { protocols: { grpc: { endpoint: "0.0.0.0:4317" } } },
            },
          },
          persistence: { enabled: true, size: "5Gi" },
          resources: {
            requests: { cpu: "50m", memory: "128Mi" },
            limits: { cpu: "200m", memory: "256Mi" },
          },
        },
      },
      { provider: args.provider, parent: this },
    );

    this.registerOutputs({});
  }
}

export interface OtelCollectorArgs {
  provider: k8s.Provider;
  namespace: string;
  tempo: { release: k8s.helm.v3.Release };
}

/**
 * OTEL Collector — centralized trace pipeline.
 * Receives OTLP from API, applies sampling + batching, exports to Tempo.
 */
export class OtelCollector extends pulumi.ComponentResource {
  public readonly release: k8s.helm.v3.Release;

  constructor(name: string, args: OtelCollectorArgs, opts?: pulumi.ComponentResourceOptions) {
    super("loopruntime:platform:OtelCollector", name, {}, opts);

    this.release = new k8s.helm.v3.Release(
      "otel-collector",
      {
        name: "otel-collector",
        chart: "opentelemetry-collector",
        version: OTEL_VERSION,
        repositoryOpts: { repo: "https://open-telemetry.github.io/opentelemetry-helm-charts" },
        namespace: args.namespace,
        createNamespace: false,
        values: {
          fullnameOverride: "otel-collector",
          image: {
            repository: "otel/opentelemetry-collector-contrib",
            tag: OTEL_VERSION,
          },
          mode: "deployment",
          securityContext: { runAsNonRoot: true },
          resources: {
            requests: { cpu: "50m", memory: "128Mi" },
            limits: { cpu: "200m", memory: "256Mi" },
          },
          config: {
            extensions: { health_check: { endpoint: "0.0.0.0:13133" } },
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
              "otlp/tempo": {
                endpoint: `tempo.${args.namespace}.svc:4317`,
                tls: { insecure: true },
              },
            },
            service: {
              extensions: ["health_check"],
              pipelines: {
                traces: {
                  receivers: ["otlp"],
                  processors: ["batch", "probabilistic_sampler"],
                  exporters: ["otlp/tempo", "debug"],
                },
              },
            },
          },
        },
      },
      { provider: args.provider, parent: this, dependsOn: [args.tempo.release] },
    );

    this.registerOutputs({});
  }
}
