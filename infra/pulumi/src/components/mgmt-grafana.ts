// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Grafana: Helm installation + dashboard ConfigMap provisioning.

import * as k8s from "@pulumi/kubernetes";
import * as fs from "fs";
import * as path from "path";
import { PlatformStackConfig } from "../config/stack";
import { PLATFORM_NAMESPACE, HELM_VERSIONS } from "./bootstrap-shared";

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
      timeout: 600,
      cleanupOnFail: true,
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
                name: "Tempo",
                type: "tempo",
                url: `http://tempo.${PLATFORM_NAMESPACE}.svc.cluster.local:3200`,
                access: "proxy",
                jsonData: {
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
        persistence: { enabled: true, size: "5Gi", storageClassName: "do-block-storage" },
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

/**
 * Creates ConfigMaps for all LoopZero dashboard JSON files.
 * The Grafana sidecar container watches for ConfigMaps with label
 * `grafana_dashboard=1` and auto-loads them into the "LoopZero" folder.
 */
export function provisionGrafanaDashboards(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  grafana: k8s.helm.v3.Release,
): void {
  const dashboardDir = path.resolve(__dirname, "../../../helm/monitoring/dashboards");

  if (!fs.existsSync(dashboardDir)) {
    return;
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
