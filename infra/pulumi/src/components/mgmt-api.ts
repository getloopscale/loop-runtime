// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Extracted from mgmt-bootstrap.ts — LoopZero API + LoopZero Controller deployments.
// These two functions share secrets and SA, so they belong together.

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import * as random from "@pulumi/random";
import * as fs from "fs";
import * as path from "path";
import { InfraOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";
import { PLATFORM_NAMESPACE } from "./bootstrap-shared";
import { CloudflareOutputs } from "./bootstrap-shared";
import { createScopedKubeconfig } from "./mgmt-scoped-kubeconfig";

export function installLoopZeroAPI(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  config: PlatformStackConfig,
  lokiEndpoint: pulumi.Output<string>,
  callbackToken: random.RandomPassword,
  cf: CloudflareOutputs | undefined,
  serviceAuthSecret: random.RandomPassword,
): k8s.apps.v1.Deployment {
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

  const buildScopedKubeconfig = createScopedKubeconfig(
    "build", infra.buildCluster, "argo",
    [
      { apiGroups: ["argoproj.io"], resources: ["workflows"], verbs: ["create", "get", "list", "watch", "delete"] },
      { apiGroups: [""], resources: ["configmaps"], verbs: ["create", "get", "list"] },
      { apiGroups: [""], resources: ["pods", "pods/log"], verbs: ["get", "list", "watch"] },
    ],
  );

  const apiRuntimeRbacRules = [
    { apiGroups: [""], resources: ["pods", "services", "endpoints"], verbs: ["get", "list", "watch"] },
    { apiGroups: [""], resources: ["namespaces"], verbs: ["get", "list"] },
  ];

  const runtimeScopedKubeconfig = createScopedKubeconfig("runtime", infra.runtimeCluster, PLATFORM_NAMESPACE, apiRuntimeRbacRules);
  const runtime2ScopedKubeconfig = createScopedKubeconfig("api-runtime-2", infra.runtimeCluster2, PLATFORM_NAMESPACE, apiRuntimeRbacRules);

  const kubeconfigSecret = new k8s.core.v1.Secret(
    "loopzero-api-kubeconfig",
    {
      metadata: { name: "loopzero-api-kubeconfig", namespace: PLATFORM_NAMESPACE },
      stringData: {
        "mgmt-kubeconfig": infra.mgmtCluster.kubeconfig,
        "build-kubeconfig": buildScopedKubeconfig,
        "runtime-kubeconfig": runtimeScopedKubeconfig,
        "runtime-2-kubeconfig": runtime2ScopedKubeconfig,
      },
    },
    { provider, dependsOn: [ns] },
  );

  const registrySecretName = "loopzero-infra-registry";
  const apiSA = new k8s.core.v1.ServiceAccount(
    "loopzero-api-sa",
    {
      metadata: { name: "loopzero-api", namespace: PLATFORM_NAMESPACE },
      imagePullSecrets: [{ name: registrySecretName }],
    },
    { provider, dependsOn: [ns] },
  );

  // Operator read-only ClusterRole
  new k8s.rbac.v1.ClusterRole(
    "loopzero-operator-readonly",
    {
      metadata: { name: "loopzero-operator" },
      rules: [
        { apiGroups: ["*"], resources: ["*"], verbs: ["get", "list", "watch"] },
        { apiGroups: ["apps"], resources: ["deployments"], verbs: ["patch"], resourceNames: [] },
        { apiGroups: [""], resources: ["pods"], verbs: ["delete"] },
        { apiGroups: [""], resources: ["pods/portforward"], verbs: ["create"] },
        { apiGroups: [""], resources: ["pods/exec"], verbs: ["create"] },
        { apiGroups: [""], resources: ["pods/log"], verbs: ["get"] },
      ],
    },
    { provider },
  );

  const apiClusterRole = new k8s.rbac.v1.ClusterRole(
    "loopzero-api",
    {
      metadata: { name: "loopzero-api" },
      rules: [
        { apiGroups: ["loopzero.io"], resources: ["tenants"], verbs: ["get", "list", "create", "update", "patch"] },
        { apiGroups: ["loopzero.io"], resources: ["loopzeroapplications", "exposedroutes", "environments"], verbs: ["get", "list", "watch", "create", "update", "patch", "delete"] },
        { apiGroups: ["loopzero.io"], resources: ["clustercredentials"], verbs: ["get", "list", "watch"] },
      ],
    },
    { provider },
  );

  new k8s.rbac.v1.ClusterRoleBinding(
    "loopzero-api",
    {
      metadata: { name: "loopzero-api" },
      roleRef: { apiGroup: "rbac.authorization.k8s.io", kind: "ClusterRole", name: "loopzero-api" },
      subjects: [{ kind: "ServiceAccount", name: "loopzero-api", namespace: PLATFORM_NAMESPACE }],
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
        ...(config.harborExternalUrl ? { HARBOR_REGISTRY_URL: config.harborExternalUrl } : {}),
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

  const apiDeployment = new k8s.apps.v1.Deployment(
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
            annotations: { "prometheus.io/scrape": "true", "prometheus.io/port": "9090" },
          },
          spec: {
            serviceAccountName: "loopzero-api",
            securityContext: {
              runAsNonRoot: true, runAsUser: 1000, runAsGroup: 1000, fsGroup: 1000,
              seccompProfile: { type: "RuntimeDefault" },
            },
            initContainers: [{
              name: "db-migrate",
              image: pulumi.interpolate`${infra.registryEndpoint}/loopzero-api:${config.environment}`,
              command: ["/bin/sh", "-c"],
              args: [[
                'DIRTY=$(migrate -path=/migrations -database "$DATABASE_URL" version 2>&1 || true);',
                'if echo "$DIRTY" | grep -q "dirty"; then',
                '  VER=$(echo "$DIRTY" | grep -oE "[0-9]+");',
                '  echo "Dirty database at version $VER — auto-recovering";',
                '  migrate -path=/migrations -database "$DATABASE_URL" force "$VER";',
                "fi;",
                'migrate -path=/migrations -database "$DATABASE_URL" up',
              ].join(" ")],
              env: [{ name: "DATABASE_URL", valueFrom: { secretKeyRef: { name: "loopzero-api-secrets", key: "DATABASE_URL" } } }],
              resources: { requests: { cpu: "50m", memory: "64Mi" }, limits: { cpu: "100m", memory: "128Mi" } },
              securityContext: { allowPrivilegeEscalation: false, capabilities: { drop: ["ALL"] } },
            }],
            volumes: [
              { name: "kubeconfig", secret: { secretName: "loopzero-api-kubeconfig" } },
              { name: "api-migrations", configMap: { name: "loopzero-api-migrations" } },
            ],
            containers: [{
              name: "api",
              image: pulumi.interpolate`${infra.registryEndpoint}/loopzero-api:${config.environment}`,
              imagePullPolicy: "Always",
              ports: [{ name: "http", containerPort: 8080 }, { name: "metrics", containerPort: 9090 }],
              envFrom: [
                { configMapRef: { name: "loopzero-api-config" } },
                { secretRef: { name: "loopzero-api-secrets" } },
                { secretRef: { name: "loopzero-platform-tokens" } },
                ...(config.cfApiToken ? [{ secretRef: { name: "loopzero-cf-credentials" } }] : []),
                { secretRef: { name: "loopzero-github-app", optional: true } },
              ],
              env: [
                ...(config.cfApiToken ? [{ name: "CF_ZONE_MANAGEMENT", value: "true" }] : []),
                { name: "OTEL_EXPORTER_OTLP_ENDPOINT", value: `otel-collector.${PLATFORM_NAMESPACE}.svc:4317` },
              ],
              volumeMounts: [{ name: "kubeconfig", mountPath: "/etc/loopzero", readOnly: true }],
              resources: { requests: { cpu: "200m", memory: "512Mi" }, limits: { cpu: "1", memory: "1Gi" } },
              startupProbe: { httpGet: { path: "/api/v1/health", port: 8080 }, initialDelaySeconds: 5, periodSeconds: 10, failureThreshold: 12 },
              livenessProbe: { httpGet: { path: "/api/v1/health", port: 8080 }, initialDelaySeconds: 15, periodSeconds: 10 },
              readinessProbe: { httpGet: { path: "/api/v1/health", port: 8080 }, initialDelaySeconds: 5, periodSeconds: 5 },
              securityContext: { allowPrivilegeEscalation: false, capabilities: { drop: ["ALL"] } },
            }],
          },
        },
      },
    },
    { provider, dependsOn: [ns, secret, kubeconfigSecret, migrationsConfigMap, configMap, svc, apiSA] },
  );

  new k8s.autoscaling.v2.HorizontalPodAutoscaler(
    "loopzero-api-hpa",
    {
      metadata: { name: "loopzero-api", namespace: PLATFORM_NAMESPACE },
      spec: {
        scaleTargetRef: { apiVersion: "apps/v1", kind: "Deployment", name: "loopzero-api" },
        minReplicas: 2,
        maxReplicas: 10,
        metrics: [{ type: "Resource", resource: { name: "cpu", target: { type: "Utilization", averageUtilization: 70 } } }],
        behavior: {
          scaleDown: { stabilizationWindowSeconds: 300, policies: [{ type: "Percent", value: 25, periodSeconds: 60 }] },
          scaleUp: { stabilizationWindowSeconds: 30, policies: [{ type: "Percent", value: 100, periodSeconds: 30 }] },
        },
      },
    },
    { provider },
  );

  return apiDeployment;
}

export function installLoopZeroController(
  provider: k8s.Provider,
  apiDeployment: k8s.apps.v1.Deployment,
  config: PlatformStackConfig,
  infra: InfraOutputs,
  lokiEndpoint: pulumi.Output<string>,
): k8s.apps.v1.Deployment {
  return new k8s.apps.v1.Deployment(
    "loopzero-controller",
    {
      metadata: {
        name: "loopzero-controller",
        namespace: PLATFORM_NAMESPACE,
        labels: { app: "loopzero-controller", "app.kubernetes.io/name": "loopzero-controller", "app.kubernetes.io/part-of": "loopzero", "loopzero.io/roadmap": "02" },
        annotations: { "pulumi.com/skipAwait": "true" },
      },
      spec: {
        replicas: 1,
        strategy: { type: "Recreate" },
        selector: { matchLabels: { app: "loopzero-controller" } },
        template: {
          metadata: { labels: { app: "loopzero-controller" } },
          spec: {
            serviceAccountName: "loopzero-api",
            securityContext: { runAsNonRoot: true, runAsUser: 1000, runAsGroup: 1000, fsGroup: 1000, seccompProfile: { type: "RuntimeDefault" } },
            containers: [{
              name: "controller",
              image: apiDeployment.spec.template.spec.containers[0].image,
              imagePullPolicy: "IfNotPresent",
              command: ["/loopzero-controller"],
              ports: [{ containerPort: 9091, name: "health" }],
              env: [
                { name: "LOOPZERO_CELL_ID", value: "cell-default" },
                { name: "TENANT_NAMESPACE", value: PLATFORM_NAMESPACE },
                { name: "REGISTRY_URL", value: infra.registryEndpoint },
                { name: "REGISTRY_INSECURE", value: "false" },
                { name: "HARBOR_URL", value: `https://harbor.${config.domain}` },
                { name: "HARBOR_ADMIN_USERNAME", value: "admin" },
                { name: "HARBOR_ADMIN_PASSWORD", value: config.harborAdminPassword ?? "Harbor12345" },
                { name: "BASE_DOMAIN", value: config.domain },
                { name: "BUILD_KUBECONFIG", value: "/etc/loopzero/build-kubeconfig" },
                { name: "BUILD_KUBE_CONTEXT", value: "build" },
                { name: "MGMT_KUBECONFIG", value: "/etc/loopzero/mgmt-kubeconfig" },
                { name: "MGMT_KUBE_CONTEXT", value: pulumi.interpolate`do-${config.region}-${infra.mgmtCluster.clusterName}` },
                { name: "RUNTIME_KUBECONFIG", value: "/etc/loopzero/runtime-kubeconfig" },
                { name: "RUNTIME_KUBE_CONTEXT", value: "runtime" },
                { name: "ARGOCD_URL", value: `http://argocd-server.${PLATFORM_NAMESPACE}.svc.cluster.local:80` },
                { name: "ARGOCD_INSECURE", value: "true" },
                { name: "GITEA_URL", value: `http://gitea-http.${PLATFORM_NAMESPACE}.svc.cluster.local:3000` },
                { name: "MINIO_ENDPOINT", value: infra.spacesEndpoint },
                { name: "LOKI_URL", value: lokiEndpoint },
                { name: "OTEL_EXPORTER_OTLP_ENDPOINT", value: `otel-collector.${PLATFORM_NAMESPACE}.svc:4317` },
                { name: "HOSTNAME", valueFrom: { fieldRef: { fieldPath: "metadata.name" } } },
              ],
              envFrom: [
                { secretRef: { name: "loopzero-api-secrets" } },
                { secretRef: { name: "loopzero-platform-tokens" } },
                { secretRef: { name: "loopzero-cf-credentials", optional: true } },
              ],
              volumeMounts: [{ name: "kubeconfigs", mountPath: "/etc/loopzero", readOnly: true }],
              livenessProbe: { httpGet: { path: "/healthz", port: 9091 }, initialDelaySeconds: 15, periodSeconds: 20, timeoutSeconds: 3, failureThreshold: 3 },
              readinessProbe: { httpGet: { path: "/readyz", port: 9091 }, initialDelaySeconds: 5, periodSeconds: 10, timeoutSeconds: 3, failureThreshold: 3 },
              resources: { requests: { cpu: "100m", memory: "128Mi" }, limits: { memory: "512Mi" } },
              securityContext: { allowPrivilegeEscalation: false, capabilities: { drop: ["ALL"] } },
            }],
            volumes: [{
              name: "kubeconfigs",
              secret: {
                secretName: "loopzero-api-kubeconfig",
                items: [
                  { key: "mgmt-kubeconfig", path: "mgmt-kubeconfig" },
                  { key: "build-kubeconfig", path: "build-kubeconfig" },
                  { key: "runtime-kubeconfig", path: "runtime-kubeconfig" },
                ],
              },
            }],
          },
        },
      },
    },
    { provider, dependsOn: [apiDeployment] },
  );
}
