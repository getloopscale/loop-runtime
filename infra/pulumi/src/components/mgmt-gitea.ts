// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import { InfraOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";
import { PLATFORM_NAMESPACE, HELM_VERSIONS } from "./bootstrap-shared";

/**
 * Gitea admin password default — only for dev/staging environments.
 * Production MUST override via: pulumi config set --secret giteaAdminPassword <password>
 * REVIEW-2026-03-30: CRIT-01 fail-fast enforced in bootstrapMgmtCluster().
 */
export const GITEA_ADMIN_PASSWORD_DEFAULT = "loopzero-gitea-dev-2026";

// ---------------------------------------------------------------------------
// NFS Server Provisioner — RWX StorageClass for Gitea HA
// ---------------------------------------------------------------------------

export function installNfsProvisioner(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
): k8s.helm.v3.Release {
  // NFS server needs DAC_READ_SEARCH + SYS_RESOURCE capabilities which violate
  // the `baseline` PSA profile on loopzero-system. Deploy in a dedicated namespace
  // with `privileged` PSA — the StorageClass is cluster-wide, so any namespace
  // (including loopzero-system) can create NFS-backed RWX PVCs.
  const nfsNs = new k8s.core.v1.Namespace(
    "nfs-system",
    {
      metadata: {
        name: "nfs-system",
        labels: {
          "app.kubernetes.io/managed-by": "pulumi",
          "app.kubernetes.io/part-of": "loopzero-platform",
          "pod-security.kubernetes.io/enforce": "privileged",
          "pod-security.kubernetes.io/warn": "privileged",
          "pod-security.kubernetes.io/audit": "privileged",
        },
      },
    },
    { provider },
  );

  return new k8s.helm.v3.Release(
    "nfs-server-provisioner",
    {
      name: "nfs-server-provisioner",
      chart: "nfs-server-provisioner",
      version: "1.8.0",
      repositoryOpts: {
        repo: "https://kubernetes-sigs.github.io/nfs-ganesha-server-and-external-provisioner/",
      },
      namespace: "nfs-system",
      createNamespace: false,
      values: {
        fullnameOverride: "nfs-provisioner",
        persistence: {
          enabled: true,
          size: "20Gi",
          storageClass: "do-block-storage",
        },
        storageClass: {
          name: "nfs",
          defaultClass: false,
          reclaimPolicy: "Retain",
          allowVolumeExpansion: true,
        },
        resources: {
          requests: { cpu: "50m", memory: "128Mi" },
          limits: { cpu: "200m", memory: "256Mi" },
        },
      },
    },
    { provider, dependsOn: [nfsNs] },
  );
}

// ---------------------------------------------------------------------------
// Gitea Redis — dedicated lightweight instance for session/cache/queue
// ---------------------------------------------------------------------------

export function installGiteaRedis(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
): k8s.apps.v1.Deployment {
  // Using official Redis image (redis:7-alpine) instead of Bitnami chart.
  // Bitnami chart tags get unpublished from Docker Hub, causing ImagePullBackOff.
  // Official Redis images are stable and long-lived.
  const deploy = new k8s.apps.v1.Deployment(
    "gitea-redis",
    {
      metadata: {
        name: "gitea-redis",
        namespace: PLATFORM_NAMESPACE,
        labels: { app: "gitea-redis" },
      },
      spec: {
        replicas: 1,
        selector: { matchLabels: { app: "gitea-redis" } },
        template: {
          metadata: { labels: { app: "gitea-redis" } },
          spec: {
            securityContext: {
              runAsNonRoot: true,
              runAsUser: 999,
              runAsGroup: 999,
              fsGroup: 999,
              // Sprint 8 v3.2 / CHG-002a / PSA audit fix 2026-04-27: gitea-redis
              // pod was missing seccompProfile per restricted:latest profile.
              // Other 6 mgmt Deployments (loopzero-cloud, loopzero-dashboard, tenant-
              // controller, loopzero-api, loopzero-controller, kratos-ui) already
              // declare RuntimeDefault.
              seccompProfile: { type: "RuntimeDefault" },
            },
            containers: [
              {
                name: "redis",
                image: "redis:7-alpine",
                args: [
                  "redis-server",
                  "--maxmemory", "100mb",
                  "--maxmemory-policy", "allkeys-lru",
                  "--save", "", // No persistence — cache data is ephemeral
                ],
                ports: [{ containerPort: 6379, name: "redis" }],
                resources: {
                  requests: { cpu: "20m", memory: "64Mi" },
                  limits: { cpu: "200m", memory: "128Mi" },
                },
                readinessProbe: {
                  exec: { command: ["redis-cli", "ping"] },
                  initialDelaySeconds: 5,
                  periodSeconds: 10,
                },
                livenessProbe: {
                  exec: { command: ["redis-cli", "ping"] },
                  initialDelaySeconds: 10,
                  periodSeconds: 15,
                },
                securityContext: {
                  allowPrivilegeEscalation: false,
                  readOnlyRootFilesystem: true,
                  capabilities: { drop: ["ALL"] },
                },
                volumeMounts: [
                  { name: "data", mountPath: "/data" },
                ],
              },
            ],
            volumes: [
              { name: "data", emptyDir: {} },
            ],
          },
        },
      },
    },
    { provider, dependsOn: [ns] },
  );

  new k8s.core.v1.Service(
    "gitea-redis-svc",
    {
      metadata: {
        name: "gitea-redis",
        namespace: PLATFORM_NAMESPACE,
        labels: { app: "gitea-redis" },
      },
      spec: {
        selector: { app: "gitea-redis" },
        ports: [{ port: 6379, targetPort: 6379, name: "redis" }],
      },
    },
    { provider, dependsOn: [deploy] },
  );

  return deploy;
}

// ---------------------------------------------------------------------------
// Gitea — Internal Git server
// ---------------------------------------------------------------------------

/**
 * Gitea — lightweight git server for desired state storage.
 *
 * Runs as a StatefulSet with a PVC for repository data.
 * Uses the managed PostgreSQL as database backend.
 * ArgoCD reads from Gitea to reconcile cluster state.
 *
 * HA configuration:
 *   - Session, cache, queue → dedicated Redis (gitea-redis)
 *   - Non-git storage (attachments, LFS, avatars) → S3 (DO Spaces)
 *   - Git repos → NFS shared volume (ReadWriteMany) when replicas > 1
 *   - Prometheus metrics enabled at /metrics (token-protected)
 *   - Database → external managed PostgreSQL (SSL required)
 */
export function installGitea(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  config: PlatformStackConfig,
  giteaRedis: k8s.apps.v1.Deployment,
  nfsProvisioner: k8s.helm.v3.Release,
): k8s.helm.v3.Release {
  const redisUrl = `redis://gitea-redis.${PLATFORM_NAMESPACE}.svc.cluster.local:6379`;

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
        fullnameOverride: "gitea",
        // Use managed PostgreSQL — no embedded database
        postgresql: { enabled: false },
        "postgresql-ha": { enabled: false },
        // Disable bundled Redis — we deploy our own (gitea-redis)
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
              LFS_START_SERVER: "true",
            },
            service: {
              DISABLE_REGISTRATION: true,
            },
            // --- HA-ready: externalized state ---
            // Session → Redis (survives pod restart, shared across replicas)
            session: {
              PROVIDER: "redis",
              PROVIDER_CONFIG: `${redisUrl}/0?pool_size=100&idle_timeout=120s`,
            },
            // Cache → Redis (shared across replicas, no stale local caches)
            cache: {
              ADAPTER: "redis",
              HOST: `${redisUrl}/1?pool_size=100&idle_timeout=120s`,
            },
            // Queue → Redis (task queue shared across replicas)
            queue: {
              TYPE: "redis",
              CONN_STR: `${redisUrl}/2?pool_size=50&idle_timeout=120s`,
            },
            // --- S3 storage for non-git data ---
            storage: {
              STORAGE_TYPE: "minio",
              MINIO_ENDPOINT: infra.spacesEndpoint.apply((ep: string) => ep.replace("https://", "")),
              MINIO_BUCKET: infra.bucketName, // artifacts bucket, /gitea prefix
              MINIO_LOCATION: config.region,
              MINIO_BASE_PATH: "gitea",
              MINIO_USE_SSL: "true",
              MINIO_ACCESS_KEY_ID: config.spacesAccessKeyId,
              MINIO_SECRET_ACCESS_KEY: config.spacesSecretAccessKey,
            },
            lfs: {
              STORAGE_TYPE: "minio",
              MINIO_ENDPOINT: infra.spacesEndpoint.apply((ep: string) => ep.replace("https://", "")),
              MINIO_BUCKET: infra.bucketName,
              MINIO_LOCATION: config.region,
              MINIO_BASE_PATH: "gitea/lfs",
              MINIO_USE_SSL: "true",
              MINIO_ACCESS_KEY_ID: config.spacesAccessKeyId,
              MINIO_SECRET_ACCESS_KEY: config.spacesSecretAccessKey,
            },
            // --- Prometheus metrics ---
            metrics: {
              ENABLED: "true",
              ENABLED_ISSUE_BY_LABEL: "false",
              ENABLED_ISSUE_BY_REPOSITORY: "false",
            },
            // --- Indexer ---
            indexer: {
              ISSUE_INDEXER_TYPE: "db", // No Elasticsearch needed for internal use
            },
          },
        },
        // HA: 2 replicas with shared NFS volume for git repos.
        replicaCount: 2,
        persistence: {
          enabled: true,
          size: "10Gi",
          storageClass: "nfs",
          accessModes: ["ReadWriteMany"],
        },
        resources: {
          requests: { cpu: "100m", memory: "256Mi" },
          limits: { cpu: "500m", memory: "512Mi" },
        },
      },
    },
    { provider, dependsOn: [ns, giteaRedis, nfsProvisioner] },
  );
}
