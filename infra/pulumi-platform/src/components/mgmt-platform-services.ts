// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Platform services extracted from monolithic mgmt-bootstrap.ts.
// Contains: ArgoCD, Gitea, MinIO, Gitea bootstrap, ArgoCD repo credentials.

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import * as fs from "fs";
import * as path from "path";
import {
  PLATFORM_NAMESPACE,
  HELM_VERSIONS,
} from "./bootstrap-shared";

// Type aliases — these match the shapes passed by the orchestrator.
// Using `any` during migration to avoid importing from monolith.
type InfraOutputs = any;
type PlatformStackConfig = any;

const GITEA_ADMIN_PASSWORD_DEFAULT = "loopzero-gitea-dev-2026";

// --- Platform Services ---

// ---------------------------------------------------------------------------
// ArgoCD
// ---------------------------------------------------------------------------

export function installArgoCD(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  certManager: k8s.helm.v3.Release,
  callbackToken: pulumi.Output<string>,
): k8s.helm.v3.Release {
  return new k8s.helm.v3.Release(
    "argocd",
    {
      name: "argocd",
      chart: "argo-cd",
      version: HELM_VERSIONS.argocd,
      repositoryOpts: {
        repo: "https://argoproj.github.io/argo-helm",
      },
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      values: {
        // Deterministic Service names so IngressRoutes can reference them.
        // Without this, Pulumi's Helm provider adds a hash suffix to the release name.
        fullnameOverride: "argocd",

        global: {
          image: { tag: "v2.13.3" },
        },

        // Server accessed via Traefik — TLS terminated at ingress
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
            // TLS terminated at Traefik, ArgoCD serves plain HTTP
            "server.insecure": true,
            // Manual sync only — API controls when to sync, not ArgoCD auto-sync
            "controller.self.heal.enabled": "false",
            // MEET-2026-03-29-019: Terminate stuck sync operations after 5 minutes.
            "controller.operation.processing.timeout": "300",
          },
          rbac: {
            // LoopZero API: sync + get + action (terminate stuck operations).
            // MEET-2026-03-29-019: action/* needed for TerminateOperation.
            "policy.csv": `p, loopzero-api, applications, sync, */*, allow
p, loopzero-api, applications, get, */*, allow
p, loopzero-api, applications, action/*, */*, allow`,
          },
          // repositories: Gitea repo credentials are created by the Bootstrap step
          // in infra-deploy.yml with real Gitea admin tokens. Do NOT configure
          // repos here — Helm creates secrets WITHOUT credentials, and ArgoCD
          // prioritizes repository-type secrets over repo-creds templates,
          // causing "authentication required" errors.
          cm: {
            // Dedicated service account for LoopZero API — apiKey only, no login.
            // Tokens generated with `argocd account generate-token` never expire.
            // MEET-2026-03-28-013: replaces old pattern of using admin session tokens (24h expiry).
            "accounts.loopzero-api": "apiKey",
            // Health assessment for Application resources
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

        // ArgoCD Notifications — push-based deploy activation (MEET-2026-03-27-012).
        // When an Application reaches Synced + Healthy, ArgoCD calls the API
        // callback endpoint. Replaces Phase D synchronous polling.
        // NOTE: These keys are TOP-LEVEL in the argo-cd Helm chart, NOT under configs.
        notificationsController: { enabled: true },
        notifications: {
          // IMPORTANT: the argo-cd Helm chart v7.x uses "notifiers" (not "services")
          // for the notifications ConfigMap. "services" is silently ignored.
          notifiers: {
            // MEET-2026-04-09-045: Token from RandomPassword — single source of truth.
            // Same token is set in API Secret (LOOPZERO_ARGO_CALLBACK_SECRET).
            "service.webhook.loopzero-api": pulumi.interpolate`url: http://loopzero-api.${PLATFORM_NAMESPACE}.svc.cluster.local:8080/internal/argocd-callback
headers:
- name: Content-Type
  value: application/json
- name: Authorization
  value: "Bearer ${callbackToken}"`,
          },
          templates: {
            "template.sync-completed": `webhook:
  loopzero-api:
    method: POST
    body: |
      {"app_name": "{{.app.metadata.name}}", "sync_status": "{{.app.status.sync.status}}", "health_status": "{{.app.status.health.status}}", "revision": "{{.app.status.sync.revision}}"}`,
          },
          triggers: {
            "trigger.on-sync-healthy": `- when: app.status.sync.status == 'Synced' && app.status.health.status == 'Healthy'
  send: [sync-completed]`,
          },
        },

        // Controller manages syncs to runtime cluster
        controller: {
          containerSecurityContext: { runAsNonRoot: true },
          resources: {
            requests: { cpu: "100m", memory: "256Mi" },
            limits: { cpu: "500m", memory: "512Mi" },
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

        // Repo server clones from Gitea
        repoServer: {
          containerSecurityContext: { runAsNonRoot: true },
          resources: {
            requests: { cpu: "100m", memory: "128Mi" },
            limits: { cpu: "500m", memory: "512Mi" },
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

        // Redis for caching
        redis: {
          containerSecurityContext: { runAsNonRoot: true },
          resources: {
            requests: { cpu: "50m", memory: "64Mi" },
            limits: { cpu: "200m", memory: "256Mi" },
          },
        },
      },
    },
    { provider, dependsOn: [ns, certManager] },
  );
}

// ---------------------------------------------------------------------------
// Gitea
// ---------------------------------------------------------------------------

/**
 * Gitea — lightweight git server for desired state storage.
 *
 * Runs as a StatefulSet with a PVC for repository data.
 * Uses the managed PostgreSQL as database backend.
 * ArgoCD reads from Gitea to reconcile cluster state.
 *
 * The Helm chart creates:
 *   - StatefulSet with 1 replica
 *   - PVC (10Gi default) for /data
 *   - Service "gitea-http" on port 3000
 *   - Service "gitea-ssh" on port 22 (for git push via SSH)
 */
export function installGitea(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  config: PlatformStackConfig,
): k8s.helm.v3.Release {
  return new k8s.helm.v3.Release(
    "gitea",
    {
      name: "gitea",
      chart: "gitea",
      version: HELM_VERSIONS.gitea,
      repositoryOpts: {
        repo: "https://dl.gitea.com/charts/",
      },
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      values: {
        // Deterministic Service names so IngressRoutes can reference them.
        // Without this, Pulumi's Helm provider adds a hash suffix to the release name.
        fullnameOverride: "gitea",
        // Use managed PostgreSQL — no embedded database
        postgresql: { enabled: false },
        "postgresql-ha": { enabled: false },
        redis: { enabled: false },
        "redis-cluster": { enabled: false },
        gitea: {
          admin: {
            username: "gitea_admin",
            password: config.giteaAdminPassword ?? GITEA_ADMIN_PASSWORD_DEFAULT,
            email: "admin@loopzero.internal",
          },
          config: {
            database: {
              DB_TYPE: "postgres",
              HOST: pulumi.interpolate`${infra.database.privateHost}:${infra.database.port}`,
              NAME: "gitea",
              USER: infra.database.user,
              PASSWD: infra.database.password,
              SSL_MODE: "require",
            },
            server: {
              DOMAIN: `git.${config.domain}`,
              ROOT_URL: `https://git.${config.domain}/`,
              HTTP_PORT: "3000",
            },
            service: {
              DISABLE_REGISTRATION: true,
            },
            // In-memory cache instead of Redis — sufficient for internal use
            cache: {
              ADAPTER: "memory",
            },
            session: {
              PROVIDER: "memory",
            },
            queue: {
              TYPE: "level",
            },
          },
        },
        persistence: {
          enabled: true,
          size: "10Gi",
          accessModes: ["ReadWriteOnce"],
        },
        resources: {
          requests: { cpu: "100m", memory: "256Mi" },
          limits: { cpu: "500m", memory: "512Mi" },
        },
      },
    },
    { provider, dependsOn: [ns] },
  );
}

// ---------------------------------------------------------------------------
// MinIO — S3-compatible artifact storage on mgmt cluster
// ---------------------------------------------------------------------------

export function installMinIO(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  config: PlatformStackConfig,
): k8s.helm.v3.Release {
  return new k8s.helm.v3.Release(
    "minio",
    {
      name: "minio",
      chart: "minio",
      version: HELM_VERSIONS.minio,
      repositoryOpts: {
        repo: "https://charts.min.io/",
      },
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      values: {
        fullnameOverride: "minio",
        mode: "standalone",
        rootUser: "loopzero-admin",
        // Production overrides via: pulumi config set --secret minioRootPassword <password>
        rootPassword: config.minioRootPassword ?? "loopzero-minio-dev-2026",
        persistence: {
          enabled: true,
          size: "10Gi",
        },
        service: {
          type: "ClusterIP",
        },
        consoleService: {
          type: "ClusterIP",
        },
        resources: {
          requests: { cpu: "100m", memory: "128Mi" },
          limits: { cpu: "500m", memory: "512Mi" },
        },
        // Auto-create buckets for platform use.
        // The MinIO chart creates a post-install hook Job to create these buckets.
        // The hook Job needs to connect to MinIO pod on port 9000.
        buckets: [
          { name: "loopzero-artifacts", policy: "none" },
          { name: "loopzero-builds", policy: "none" },
        ],
        // Increase post-install hook timeout — on fresh clusters with slow storage
        // provisioning, the default 60s may not be enough for MinIO to start + create buckets.
        postJob: {
          activeDeadlineSeconds: 300,
        },
      },
    },
    {
      provider,
      dependsOn: [ns],
      // Increase timeout: MinIO make-bucket-job may take time on fresh clusters.
      customTimeouts: { create: "10m", update: "10m" },
    },
  );
}

// Re-export functions from split modules
export { bootstrapGitea } from "./mgmt-gitea-bootstrap";
export { createArgoCDRepoCredentials } from "./mgmt-gitea-bootstrap";
