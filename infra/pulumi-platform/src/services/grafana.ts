// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import * as fs from "fs";
import * as path from "path";

const HELM_VERSION = "8.5.2";

export interface GrafanaArgs {
  provider: k8s.Provider;
  namespace: string;
  domain: string;
  environment: string;
  /** Prometheus must be installed for datasource */
  prometheus: { release: k8s.helm.v3.Release };
}

/**
 * Grafana — dashboards and visualization with auto-provisioned LoopRuntime dashboards.
 * Datasources: Prometheus + Tempo.
 * Dashboards: loaded from infra/helm/monitoring/dashboards/ via ConfigMap sidecar.
 */
export class Grafana extends pulumi.ComponentResource {
  public readonly release: k8s.helm.v3.Release;
  public readonly ready: pulumi.Output<boolean>;

  constructor(name: string, args: GrafanaArgs, opts?: pulumi.ComponentResourceOptions) {
    super("loopruntime:platform:Grafana", name, {}, opts);

    this.release = new k8s.helm.v3.Release(
      "grafana",
      {
        name: "grafana",
        chart: "grafana",
        timeout: 600,
        cleanupOnFail: true,
        version: HELM_VERSION,
        repositoryOpts: { repo: "https://grafana.github.io/helm-charts" },
        namespace: args.namespace,
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
                  url: `http://prometheus-server.${args.namespace}.svc.cluster.local:80`,
                  access: "proxy",
                  isDefault: true,
                },
                {
                  name: "Tempo",
                  type: "tempo",
                  url: `http://tempo.${args.namespace}.svc.cluster.local:3200`,
                  access: "proxy",
                  jsonData: {
                    tracesToLogsV2: {
                      datasourceUid: "loki",
                      filterByTraceID: false,
                      customQuery: true,
                      query: `{job="loopruntime-api"} |= "\${__trace.traceId}"`,
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
                folder: "LoopRuntime",
                type: "file",
                disableDeletion: false,
                editable: true,
              },
            },
          },
          dashboardProviders: {
            "dashboardproviders.yaml": {
              apiVersion: 1,
              providers: [{
                name: "default",
                orgId: 1,
                folder: "",
                type: "file",
                disableDeletion: false,
                editable: true,
                options: { path: "/var/lib/grafana/dashboards/default" },
              }],
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
          strategy: { type: "Recreate" },
          adminUser: "admin",
          "grafana.ini": {
            server: {
              domain: `grafana.${args.domain}`,
              root_url: `https://grafana.${args.domain}/`,
              serve_from_sub_path: false,
            },
            "auth.anonymous": {
              enabled: args.environment !== "prod",
              org_role: "Viewer",
            },
          },
          resources: {
            requests: { cpu: "100m", memory: "128Mi" },
            limits: { cpu: "300m", memory: "256Mi" },
          },
        },
      },
      { provider: args.provider, parent: this, dependsOn: [args.prometheus.release] },
    );

    // Auto-provision LoopRuntime dashboards via ConfigMap sidecar
    this.provisionDashboards(args.provider, args.namespace);

    this.ready = this.release.status.apply((s) => s.status === "deployed");
    this.registerOutputs({ ready: this.ready });
  }

  /**
   * Load all JSON dashboard files and create ConfigMaps with the sidecar label.
   * Grafana sidecar watches for `grafana_dashboard=1` and auto-loads them.
   */
  private provisionDashboards(provider: k8s.Provider, namespace: string): void {
    const dashboardDir = path.resolve(__dirname, "../../../helm/monitoring/dashboards");

    if (!fs.existsSync(dashboardDir)) {
      return; // CI without infra checkout
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
            namespace,
            labels: {
              grafana_dashboard: "1",
              "app.kubernetes.io/managed-by": "pulumi",
            },
          },
          data: { [file]: content },
        },
        { provider, parent: this, dependsOn: [this.release] },
      );
    }
  }
}
