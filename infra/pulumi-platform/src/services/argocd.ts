// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";

const HELM_VERSION = "7.3.11";

export interface ArgoCDArgs {
  provider: k8s.Provider;
  namespace: string;
  /** ArgoCD callback token for notifications webhook */
  callbackToken: pulumi.Output<string>;
  /** cert-manager must be installed before ArgoCD */
  certManager: { release: k8s.helm.v3.Release };
}

/**
 * ArgoCD — GitOps engine for cluster convergence.
 * Manual sync only (API controls when to sync).
 * Notifications webhook for deploy activation.
 */
export class ArgoCD extends pulumi.ComponentResource {
  public readonly release: k8s.helm.v3.Release;
  public readonly ready: pulumi.Output<boolean>;

  constructor(name: string, args: ArgoCDArgs, opts?: pulumi.ComponentResourceOptions) {
    super("loopruntime:platform:ArgoCD", name, {}, opts);

    this.release = new k8s.helm.v3.Release(
      "argocd",
      {
        name: "argocd",
        chart: "argo-cd",
        version: HELM_VERSION,
        repositoryOpts: { repo: "https://argoproj.github.io/argo-helm" },
        namespace: args.namespace,
        createNamespace: false,
        values: {
          fullnameOverride: "argocd",
          global: { image: { tag: "v2.13.3" } },
          server: {
            service: { type: "ClusterIP" },
            containerSecurityContext: { runAsNonRoot: true },
            resources: {
              requests: { cpu: "100m", memory: "128Mi" },
              limits: { cpu: "500m", memory: "256Mi" },
            },
            metrics: {
              enabled: true,
              serviceMonitor: {
                enabled: true,
                additionalLabels: { release: "monitoring" },
                namespace: "monitoring",
              },
            },
          },
          configs: {
            params: {
              "server.insecure": true,
              "controller.self.heal.enabled": "false",
              "controller.operation.processing.timeout": "300",
            },
            rbac: {
              "policy.csv": `p, loopruntime-api, applications, sync, */*, allow
p, loopruntime-api, applications, get, */*, allow
p, loopruntime-api, applications, action/*, */*, allow`,
            },
            cm: {
              "accounts.loopruntime-api": "apiKey",
              "resource.customizations.health.argoproj.io_Application": `hs = {}
hs.status = "Progressing"
hs.message = ""
if obj.status ~= nil then
  if obj.status.health ~= nil then
    hs.status = obj.status.health.status
    if obj.status.health.message ~= nil then
      hs.message = obj.status.health.message
    end
  end
end
return hs`,
            },
          },
          notificationsController: { enabled: true },
          notifications: {
            notifiers: {
              "service.webhook.loopruntime-api": pulumi.interpolate`url: http://loopruntime-api.${args.namespace}.svc.cluster.local:8080/internal/argocd-callback
headers:
- name: Content-Type
  value: application/json
- name: Authorization
  value: "Bearer ${args.callbackToken}"`,
            },
            templates: {
              "template.sync-completed": `webhook:
  loopruntime-api:
    method: POST
    body: |
      {"app_name": "{{.app.metadata.name}}", "sync_status": "{{.app.status.sync.status}}", "health_status": "{{.app.status.health.status}}", "revision": "{{.app.status.sync.revision}}"}`,
            },
            triggers: {
              "trigger.on-sync-healthy": `- when: app.status.sync.status == 'Synced' && app.status.health.status == 'Healthy'
  send: [sync-completed]`,
            },
          },
          controller: {
            containerSecurityContext: { runAsNonRoot: true },
            resources: {
              requests: { cpu: "100m", memory: "256Mi" },
              limits: { cpu: "500m", memory: "512Mi" },
            },
          },
          repoServer: {
            containerSecurityContext: { runAsNonRoot: true },
            resources: {
              requests: { cpu: "100m", memory: "128Mi" },
              limits: { cpu: "500m", memory: "512Mi" },
            },
          },
          redis: {
            containerSecurityContext: { runAsNonRoot: true },
            resources: {
              requests: { cpu: "50m", memory: "64Mi" },
              limits: { cpu: "200m", memory: "256Mi" },
            },
          },
        },
      },
      { provider: args.provider, parent: this, dependsOn: [args.certManager.release] },
    );

    this.ready = this.release.status.apply((s) => s.status === "deployed");
    this.registerOutputs({ ready: this.ready });
  }
}
