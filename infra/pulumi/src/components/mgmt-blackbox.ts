// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Blackbox Exporter — HTTP/DNS/TCP probe infrastructure (Phase 1A observability).

import * as k8s from "@pulumi/kubernetes";
import { PLATFORM_NAMESPACE, HELM_VERSIONS } from "./bootstrap-shared";

export function installBlackboxExporter(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
): k8s.helm.v3.Release {
  return new k8s.helm.v3.Release(
    "blackbox-exporter",
    {
      name: "blackbox-exporter",
      chart: "prometheus-blackbox-exporter",
      version: HELM_VERSIONS.blackboxExporter,
      repositoryOpts: {
        repo: "https://prometheus-community.github.io/helm-charts",
      },
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      values: {
        fullnameOverride: "blackbox-exporter",
        securityContext: {
          runAsNonRoot: true,
          readOnlyRootFilesystem: true,
        },
        resources: {
          requests: { cpu: "25m", memory: "32Mi" },
          limits: { cpu: "100m", memory: "64Mi" },
        },
        config: {
          modules: {
            http_2xx: {
              prober: "http",
              timeout: "10s",
              http: {
                valid_http_versions: ["HTTP/1.1", "HTTP/2.0"],
                follow_redirects: true,
                preferred_ip_protocol: "ip4",
              },
            },
            dns_resolve: {
              prober: "dns",
              timeout: "5s",
              dns: {
                query_name: "kubernetes.default.svc.cluster.local",
                query_type: "A",
                preferred_ip_protocol: "ip4",
              },
            },
            tcp_connect: {
              prober: "tcp",
              timeout: "5s",
              tcp: {
                preferred_ip_protocol: "ip4",
              },
            },
          },
        },
      },
    },
    { provider, dependsOn: [ns] },
  );
}
