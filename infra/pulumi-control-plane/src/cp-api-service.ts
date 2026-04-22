// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// LoopZero API: Deployment, Service, PDB, ConfigMap, Secrets, RBAC, kubeconfigs.

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import * as random from "@pulumi/random";
import * as fs from "fs";
import * as path from "path";
import {
  PLATFORM_NAMESPACE,
  InfraOutputs,
  PlatformStackConfig,
  CloudflareOutputs,
  serviceAuthSecret,
} from "./mgmt-control-plane-services";
import { createScopedKubeconfig } from "./cp-network";

export function installLoopZeroAPI(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  config: PlatformStackConfig,
  lokiEndpoint: pulumi.Output<string>,
  callbackToken: random.RandomPassword,
  cf?: CloudflareOutputs,
): k8s.apps.v1.Deployment {
  // Database connection string for the `loopzero` database (created in Phase 1).
  // MEET-2026-03-31-024: When loopzeroApiDbPassword is set, the API connects as loopzero_api role
  // (DML only, no DDL). Migrations continue using the admin user.
  // PG keepalive parameters: detect dead connections in ~90s (MEET-042 P0).
  const pgKeepalive = "tcp_keepalives_idle=60&tcp_keepalives_interval=10&tcp_keepalives_count=3";
  const databaseUrl = config.loopzeroApiDbPassword
    ? pulumi.interpolate`postgres://loopzero_api:${config.loopzeroApiDbPassword}@${infra.database.privateHost}:${infra.database.port}/loopzero?sslmode=require&${pgKeepalive}`
    : pulumi.interpolate`postgres://${infra.database.user}:${infra.database.password}@${infra.database.privateHost}:${infra.database.port}/loopzero?sslmode=require&${pgKeepalive}`;

  const secret = new k8s.core.v1.Secret(
    "loopzero-api-secrets",
    {
      metadata: { name: "loopzero-api-secrets", namespace: PLATFORM_NAMESPACE },
      stringData: {
        DATABASE_URL: databaseUrl,
        LOOPZERO_ARGO_CALLBACK_SECRET: callbackToken.result,
        MINIO_ACCESS_KEY: config.spacesAccessKeyId,
        MINIO_SECRET_KEY: config.spacesSecretAccessKey,
        LOOPZERO_SERVICE_AUTH_SECRET: config.serviceAuthSecret || serviceAuthSecret.result,
      },
    },
    { provider, dependsOn: [ns] },
  );

  // Cloudflare credentials for static frontend delivery (Sprint 10).
  if (config.cfApiToken && config.cfAccountId && cf?.kvNamespaceId) {
    new k8s.core.v1.Secret(
      "loopzero-cf-credentials",
      {
        metadata: { name: "loopzero-cf-credentials", namespace: PLATFORM_NAMESPACE },
        stringData: {
          CF_API_TOKEN: config.cfApiToken,
          CF_ACCOUNT_ID: config.cfAccountId,
          CF_KV_NAMESPACE_ID: cf.kvNamespaceId,
        },
      },
      { provider, dependsOn: [ns] },
    );
  }

  // Database migrations ConfigMap
  const migrationsDir = path.resolve(__dirname, "../../../../api/internal/db/migrations");
  const migrationFiles = fs.readdirSync(migrationsDir).filter((f) => f.endsWith(".up.sql")).sort();
  const migrationData: Record<string, string> = {};
  for (const f of migrationFiles) {
    migrationData[f] = fs.readFileSync(path.join(migrationsDir, f), "utf-8");
  }

  const migrationsConfigMap = new k8s.core.v1.ConfigMap(
    "loopzero-api-migrations",
    {
      metadata: { name: "loopzero-api-migrations", namespace: PLATFORM_NAMESPACE },
      data: migrationData,
    },
    { provider, dependsOn: [ns] },
  );

  // Scoped kubeconfigs for build and runtime clusters
  const buildScopedKubeconfig = createScopedKubeconfig(
    "build",
    infra.buildCluster,
    "argo",
    [
      { apiGroups: ["argoproj.io"], resources: ["workflows"], verbs: ["create", "get", "list", "watch", "delete"] },
      { apiGroups: [""], resources: ["configmaps"], verbs: ["create", "get", "list"] },
      { apiGroups: [""], resources: ["pods", "pods/log"], verbs: ["get", "list", "watch"] },
    ],
  );

  const runtimeScopedKubeconfig = createScopedKubeconfig(
    "runtime",
    infra.runtimeCluster,
    PLATFORM_NAMESPACE,
    [
      { apiGroups: [""], resources: ["pods", "services", "endpoints"], verbs: ["get", "list", "watch"] },
      { apiGroups: [""], resources: ["namespaces"], verbs: ["get", "list"] },
    ],
  );

  const kubeconfigSecret = new k8s.core.v1.Secret(
    "loopzero-api-kubeconfig",
    {
      metadata: { name: "loopzero-api-kubeconfig", namespace: PLATFORM_NAMESPACE },
      stringData: {
        "mgmt-kubeconfig": infra.mgmtCluster.kubeconfig,
        "build-kubeconfig": buildScopedKubeconfig,
        "runtime-kubeconfig": runtimeScopedKubeconfig,
      },
    },
    { provider, dependsOn: [ns] },
  );

  // ServiceAccount + RBAC for Tenant CR management
  const registrySecretName = "loopzero-infra-registry";
  const apiSA = new k8s.core.v1.ServiceAccount(
    "loopzero-api-sa",
    {
      metadata: { name: "loopzero-api", namespace: PLATFORM_NAMESPACE },
      imagePullSecrets: [{ name: registrySecretName }],
    },
    { provider, dependsOn: [ns] },
  );

  const apiClusterRole = new k8s.rbac.v1.ClusterRole(
    "loopzero-api",
    {
      metadata: { name: "loopzero-api" },
      rules: [
        {
          apiGroups: ["loopzero.io"],
          resources: ["tenants"],
          verbs: ["get", "list", "create", "update", "patch"],
        },
      ],
    },
    { provider },
  );

  new k8s.rbac.v1.ClusterRoleBinding(
    "loopzero-api",
    {
      metadata: { name: "loopzero-api" },
      roleRef: {
        apiGroup: "rbac.authorization.k8s.io",
        kind: "ClusterRole",
        name: "loopzero-api",
      },
      subjects: [
        { kind: "ServiceAccount", name: "loopzero-api", namespace: PLATFORM_NAMESPACE },
      ],
    },
    { provider, dependsOn: [apiClusterRole, apiSA] },
  );

  const configMap = new k8s.core.v1.ConfigMap(
    "loopzero-api-config",
    {
      metadata: { name: "loopzero-api-config", namespace: PLATFORM_NAMESPACE },
      data: {
        PORT: "8080",
        METRICS_PORT: "9090",
        TENANT_NAMESPACE: PLATFORM_NAMESPACE,
        LOOPZERO_ENV: config.environment === "prod" ? "production" : "development",
        BASE_DOMAIN: config.domain,
        WORKLOAD_INGRESS_URL: `https://${config.domain}`,
        GITEA_URL: `http://gitea-http.${PLATFORM_NAMESPACE}.svc.cluster.local:3000`,
        ARGOCD_URL: `http://argocd-server.${PLATFORM_NAMESPACE}.svc.cluster.local:80`,
        ARGOCD_INSECURE: "true",
        JWKS_URL: `https://auth.${config.domain}/.well-known/jwks.json`,
        JWT_ISSUER: `https://auth.${config.domain}/`,
        ARGO_NAMESPACE: "argo",
        MGMT_KUBECONFIG: "/etc/loopzero/mgmt-kubeconfig",
        MGMT_KUBE_CONTEXT: pulumi.interpolate`do-${config.region}-${infra.mgmtCluster.clusterName}`,
        BUILD_KUBECONFIG: "/etc/loopzero/build-kubeconfig",
        BUILD_KUBE_CONTEXT: "build",
        RUNTIME_KUBECONFIG: "/etc/loopzero/runtime-kubeconfig",
        RUNTIME_KUBE_CONTEXT: "runtime",
        MINIO_ENDPOINT: infra.spacesEndpoint,
        REGISTRY_URL: infra.registryEndpoint,
        REGISTRY_INSECURE: "false",
        ARGOCD_NAMESPACE: PLATFORM_NAMESPACE,
        RELEASE_PENDING_TIMEOUT: "10m",
        LOKI_URL: pulumi.interpolate`${lokiEndpoint}`,
      },
    },
    { provider, dependsOn: [ns] },
  );

  const svc = new k8s.core.v1.Service(
    "loopzero-api-svc",
    {
      metadata: {
        name: "loopzero-api",
        namespace: PLATFORM_NAMESPACE,
        labels: { app: "loopzero-api" },
        annotations: { "pulumi.com/skipAwait": "true" },
      },
      spec: {
        selector: { app: "loopzero-api" },
        ports: [{ port: 8080, targetPort: 8080, name: "http" }],
      },
    },
    { provider, dependsOn: [ns] },
  );

  new k8s.policy.v1.PodDisruptionBudget(
    "loopzero-api-pdb",
    {
      metadata: { name: "loopzero-api", namespace: PLATFORM_NAMESPACE },
      spec: {
        minAvailable: 1,
        selector: { matchLabels: { app: "loopzero-api" } },
      },
    },
    { provider },
  );

  return new k8s.apps.v1.Deployment(
    "loopzero-api",
    {
      metadata: {
        name: "loopzero-api",
        namespace: PLATFORM_NAMESPACE,
        labels: { app: "loopzero-api" },
        annotations: { "pulumi.com/skipAwait": "true" },
      },
      spec: {
        replicas: 2,
        selector: { matchLabels: { app: "loopzero-api" } },
        template: {
          metadata: {
            labels: { app: "loopzero-api" },
            annotations: {
              "prometheus.io/scrape": "true",
              "prometheus.io/port": "9090",
            },
          },
          spec: {
            serviceAccountName: "loopzero-api",
            initContainers: [
              {
                name: "db-migrate",
                image: pulumi.interpolate`${infra.registryEndpoint}/loopzero-api:${config.environment}`,
                command: ["/bin/sh", "-c"],
                args: [
                  [
                    'DIRTY=$(migrate -path=/migrations -database "$DATABASE_URL" version 2>&1 || true);',
                    'if echo "$DIRTY" | grep -q "dirty"; then',
                    '  VER=$(echo "$DIRTY" | grep -oE "[0-9]+");',
                    '  echo "Dirty database at version $VER — auto-recovering";',
                    '  migrate -path=/migrations -database "$DATABASE_URL" force "$VER";',
                    "fi;",
                    'migrate -path=/migrations -database "$DATABASE_URL" up',
                  ].join(" "),
                ],
                env: [
                  {
                    name: "DATABASE_URL",
                    valueFrom: { secretKeyRef: { name: "loopzero-api-secrets", key: "DATABASE_URL" } },
                  },
                ],
                resources: {
                  requests: { cpu: "50m", memory: "64Mi" },
                  limits: { cpu: "100m", memory: "128Mi" },
                },
              },
            ],
            volumes: [
              { name: "kubeconfig", secret: { secretName: "loopzero-api-kubeconfig" } },
              { name: "api-migrations", configMap: { name: "loopzero-api-migrations" } },
            ],
            containers: [
              {
                name: "api",
                image: pulumi.interpolate`${infra.registryEndpoint}/loopzero-api:${config.environment}`,
                imagePullPolicy: "Always",
                ports: [
                  { name: "http", containerPort: 8080 },
                  { name: "metrics", containerPort: 9090 },
                ],
                envFrom: [
                  { configMapRef: { name: "loopzero-api-config" } },
                  { secretRef: { name: "loopzero-api-secrets" } },
                  { secretRef: { name: "loopzero-platform-tokens" } },
                  ...(config.cfApiToken ? [{ secretRef: { name: "loopzero-cf-credentials" } }] : []),
                  { secretRef: { name: "loopzero-github-app", optional: true } },
                ],
                env: [
                  ...(config.cfApiToken ? [{ name: "CF_ZONE_MANAGEMENT", value: "true" }] : []),
                  {
                    name: "OTEL_EXPORTER_OTLP_ENDPOINT",
                    value: `otel-collector.${PLATFORM_NAMESPACE}.svc:4317`,
                  },
                ],
                volumeMounts: [
                  { name: "kubeconfig", mountPath: "/etc/loopzero", readOnly: true },
                ],
                resources: {
                  requests: { cpu: "200m", memory: "512Mi" },
                  limits: { cpu: "1", memory: "1Gi" },
                },
                startupProbe: {
                  httpGet: { path: "/api/v1/health", port: 8080 },
                  initialDelaySeconds: 5,
                  periodSeconds: 10,
                  failureThreshold: 12,
                },
                livenessProbe: {
                  httpGet: { path: "/api/v1/health", port: 8080 },
                  initialDelaySeconds: 15,
                  periodSeconds: 10,
                },
                readinessProbe: {
                  httpGet: { path: "/api/v1/health", port: 8080 },
                  initialDelaySeconds: 5,
                  periodSeconds: 5,
                },
              },
            ],
          },
        },
      },
    },
    { provider, dependsOn: [ns, secret, kubeconfigSecret, migrationsConfigMap, configMap, svc, apiSA] },
  );
}
