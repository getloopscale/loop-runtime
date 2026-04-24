// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Extracted from mgmt-bootstrap.ts — Cloud Auth Service + Dashboard.

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import * as random from "@pulumi/random";
import { InfraOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";
import { PLATFORM_NAMESPACE } from "./bootstrap-shared";

export function installCloudService(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  config: PlatformStackConfig,
  serviceAuthSecret: random.RandomPassword,
): void {
  const registrySecretName = "loopzero-system";
  const imageTag = config.environment || "dev";

  const configMap = new k8s.core.v1.ConfigMap(
    "loopzero-cloud-config",
    {
      metadata: { name: "loopzero-cloud-config", namespace: PLATFORM_NAMESPACE },
      data: {
        CLOUD_LISTEN_ADDR: ":13002",
        LOOPZERO_ENV: config.environment || "dev",
        KRATOS_PUBLIC_URL: `https://cloud.${config.domain || "dev.loopzero.example"}`,
        HYDRA_PUBLIC_URL: `https://auth.${config.domain || "dev.loopzero.example"}`,
        KRATOS_ADMIN_URL: `http://ory-kratos-admin.${PLATFORM_NAMESPACE}.svc.cluster.local:80`,
        HYDRA_ADMIN_URL: `http://ory-hydra-admin.${PLATFORM_NAMESPACE}.svc.cluster.local:4445`,
        HYDRA_CLIENT_ID: "loopzero-cli",
        ENGINE_API_URL: `http://loopzero-api.${PLATFORM_NAMESPACE}.svc.cluster.local:8080`,
        BILLING_RETURN_URL: `https://cloud.${config.domain || "dev.loopzero.example"}`,
        KRATOS_INTERNAL_URL: `http://ory-kratos-public.${PLATFORM_NAMESPACE}.svc.cluster.local:80`,
        STRIPE_PRICE_PRO: config.stripePriceProId || "",
        STRIPE_PRICE_TEAM: config.stripePriceTeamId || "",
      },
    },
    { provider, dependsOn: [ns] },
  );

  const cloudDatabaseUrl = config.loopzeroApiDbPassword
    ? pulumi.interpolate`postgres://loopzero_api:${config.loopzeroApiDbPassword}@${infra.database.privateHost}:${infra.database.port}/loopzero?sslmode=require`
    : pulumi.interpolate`postgres://${infra.database.user}:${infra.database.password}@${infra.database.privateHost}:${infra.database.port}/loopzero?sslmode=require`;

  const secret = new k8s.core.v1.Secret(
    "loopzero-cloud-secrets",
    {
      metadata: { name: "loopzero-cloud-secrets", namespace: PLATFORM_NAMESPACE },
      stringData: {
        STRIPE_API_KEY: config.stripeApiKey || "",
        STRIPE_WEBHOOK_SECRET: config.stripeWebhookSecret || "",
        DATABASE_URL: cloudDatabaseUrl,
        SESSION_TOKEN_SECRET: config.sessionTokenSecret || "",
        LOOPZERO_SERVICE_AUTH_SECRET: config.serviceAuthSecret || serviceAuthSecret.result,
      },
    },
    { provider, dependsOn: [ns] },
  );

  new k8s.core.v1.Service(
    "loopzero-cloud",
    {
      metadata: { name: "loopzero-cloud", namespace: PLATFORM_NAMESPACE },
      spec: {
        selector: { app: "loopzero-cloud" },
        ports: [{ name: "http", port: 13002, targetPort: 13002 }],
      },
    },
    { provider, dependsOn: [ns] },
  );

  new k8s.apps.v1.Deployment(
    "loopzero-cloud",
    {
      metadata: {
        name: "loopzero-cloud",
        namespace: PLATFORM_NAMESPACE,
        annotations: { "pulumi.com/skipAwait": "true" },
      },
      spec: {
        replicas: 1,
        selector: { matchLabels: { app: "loopzero-cloud" } },
        template: {
          metadata: { labels: { app: "loopzero-cloud" } },
          spec: {
            serviceAccountName: "default",
            securityContext: {
              runAsNonRoot: true,
              runAsUser: 1000,
              runAsGroup: 1000,
              fsGroup: 1000,
              seccompProfile: { type: "RuntimeDefault" },
            },
            imagePullSecrets: [{ name: registrySecretName }],
            containers: [{
              name: "cloud",
              image: `registry.digitalocean.com/loopzero-system/loopzero-cloud:${imageTag}`,
              imagePullPolicy: "Always",
              ports: [{ containerPort: 13002 }],
              envFrom: [
                { configMapRef: { name: "loopzero-cloud-config" } },
                { secretRef: { name: "loopzero-cloud-secrets" } },
              ],
              resources: {
                requests: { cpu: "50m", memory: "64Mi" },
                limits: { cpu: "200m", memory: "128Mi" },
              },
              readinessProbe: {
                httpGet: { path: "/health", port: 13002 },
                initialDelaySeconds: 5,
                periodSeconds: 10,
              },
              livenessProbe: {
                httpGet: { path: "/health", port: 13002 },
                initialDelaySeconds: 10,
                periodSeconds: 30,
              },
              securityContext: {
                allowPrivilegeEscalation: false,
                capabilities: { drop: ["ALL"] },
              },
            }],
          },
        },
      },
    },
    { provider, dependsOn: [ns, configMap, secret] },
  );

  // Dashboard — React SPA served by nginx
  new k8s.core.v1.Service(
    "loopzero-dashboard",
    {
      metadata: { name: "loopzero-dashboard", namespace: PLATFORM_NAMESPACE },
      spec: {
        selector: { app: "loopzero-dashboard" },
        ports: [{ name: "http", port: 80, targetPort: 8080 }],
      },
    },
    { provider, dependsOn: [ns] },
  );

  new k8s.apps.v1.Deployment(
    "loopzero-dashboard",
    {
      metadata: {
        name: "loopzero-dashboard",
        namespace: PLATFORM_NAMESPACE,
        annotations: { "pulumi.com/skipAwait": "true" },
      },
      spec: {
        replicas: 1,
        selector: { matchLabels: { app: "loopzero-dashboard" } },
        template: {
          metadata: { labels: { app: "loopzero-dashboard" } },
          spec: {
            securityContext: {
              runAsNonRoot: true,
              runAsUser: 101,
              runAsGroup: 101,
              fsGroup: 101,
              seccompProfile: { type: "RuntimeDefault" },
            },
            imagePullSecrets: [{ name: registrySecretName }],
            containers: [{
              name: "dashboard",
              image: `registry.digitalocean.com/loopzero-system/loopzero-dashboard:${imageTag}`,
              imagePullPolicy: "Always",
              ports: [{ containerPort: 8080 }],
              resources: {
                requests: { cpu: "10m", memory: "16Mi" },
                limits: { cpu: "100m", memory: "64Mi" },
              },
              readinessProbe: {
                httpGet: { path: "/", port: 8080 },
                initialDelaySeconds: 2,
                periodSeconds: 10,
              },
              securityContext: {
                allowPrivilegeEscalation: false,
                capabilities: { drop: ["ALL"] },
              },
            }],
          },
        },
      },
    },
    { provider, dependsOn: [ns] },
  );

  // HPAs
  new k8s.autoscaling.v2.HorizontalPodAutoscaler(
    "loopzero-cloud-hpa",
    {
      metadata: { name: "loopzero-cloud", namespace: PLATFORM_NAMESPACE },
      spec: {
        scaleTargetRef: { apiVersion: "apps/v1", kind: "Deployment", name: "loopzero-cloud" },
        minReplicas: 1,
        maxReplicas: 5,
        metrics: [{
          type: "Resource",
          resource: { name: "cpu", target: { type: "Utilization", averageUtilization: 70 } },
        }],
        behavior: {
          scaleDown: { stabilizationWindowSeconds: 300, policies: [{ type: "Percent", value: 25, periodSeconds: 60 }] },
          scaleUp: { stabilizationWindowSeconds: 30, policies: [{ type: "Percent", value: 100, periodSeconds: 30 }] },
        },
      },
    },
    { provider },
  );

  new k8s.autoscaling.v2.HorizontalPodAutoscaler(
    "loopzero-dashboard-hpa",
    {
      metadata: { name: "loopzero-dashboard", namespace: PLATFORM_NAMESPACE },
      spec: {
        scaleTargetRef: { apiVersion: "apps/v1", kind: "Deployment", name: "loopzero-dashboard" },
        minReplicas: 1,
        maxReplicas: 3,
        metrics: [{
          type: "Resource",
          resource: { name: "cpu", target: { type: "Utilization", averageUtilization: 80 } },
        }],
      },
    },
    { provider },
  );
}
