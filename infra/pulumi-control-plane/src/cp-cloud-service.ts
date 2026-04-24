// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Cloud Auth Service (OAuth2 + Billing + Dashboard).

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import { PLATFORM_NAMESPACE, InfraOutputs, PlatformStackConfig, serviceAuthSecret } from "./mgmt-control-plane-services";

export function installCloudService(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  config: PlatformStackConfig,
): void {
  const registrySecretName = "loopzero-system";
  const imageTag = config.environment || "dev";

  // ConfigMap — non-sensitive environment variables
  const configMap = new k8s.core.v1.ConfigMap(
    "loopzero-cloud-config",
    {
      metadata: { name: "loopzero-cloud-config", namespace: PLATFORM_NAMESPACE },
      data: {
        CLOUD_LISTEN_ADDR: ":13002",
        LOOPZERO_ENV: config.environment || "dev",
        // Kratos public URL — served via cloud gateway (same domain as dashboard, no CORS).
        KRATOS_PUBLIC_URL: `https://cloud.${config.domain || "dev.loopzero.example"}`,
        HYDRA_PUBLIC_URL: `https://auth.${config.domain || "dev.loopzero.example"}`,
        // Internal URLs (server-to-server, never exposed to browser)
        KRATOS_ADMIN_URL: `http://ory-kratos-admin.${PLATFORM_NAMESPACE}.svc.cluster.local:80`,
        HYDRA_ADMIN_URL: `http://ory-hydra-admin.${PLATFORM_NAMESPACE}.svc.cluster.local:4445`,
        HYDRA_CLIENT_ID: "loopzero-cli",
        ENGINE_API_URL: `http://loopzero-api.${PLATFORM_NAMESPACE}.svc.cluster.local:8080`,
        // Dashboard URL — used for OAuth2 redirect_uri registration
        BILLING_RETURN_URL: `https://cloud.${config.domain || "dev.loopzero.example"}`,
        KRATOS_INTERNAL_URL: `http://ory-kratos-public.${PLATFORM_NAMESPACE}.svc.cluster.local:80`,
        // Stripe price IDs (MEET-038 Phase 6)
        STRIPE_PRICE_PRO: config.stripePriceProId || "",
        STRIPE_PRICE_TEAM: config.stripePriceTeamId || "",
      },
    },
    { provider, dependsOn: [ns] },
  );

  // Database URL — same DB as engine API (cloud service does NOT run migrations)
  const cloudDatabaseUrl = config.loopzeroApiDbPassword
    ? pulumi.interpolate`postgres://loopzero_api:${config.loopzeroApiDbPassword}@${infra.database.privateHost}:${infra.database.port}/loopzero?sslmode=require`
    : pulumi.interpolate`postgres://${infra.database.user}:${infra.database.password}@${infra.database.privateHost}:${infra.database.port}/loopzero?sslmode=require`;

  // Secret — Stripe API keys + DATABASE_URL
  const secret = new k8s.core.v1.Secret(
    "loopzero-cloud-secrets",
    {
      metadata: { name: "loopzero-cloud-secrets", namespace: PLATFORM_NAMESPACE },
      stringData: {
        STRIPE_API_KEY: config.stripeApiKey || "",
        STRIPE_WEBHOOK_SECRET: config.stripeWebhookSecret || "",
        DATABASE_URL: cloudDatabaseUrl,
        SESSION_TOKEN_SECRET: config.sessionTokenSecret || "",
        // Cloud->Engine proxy auth (MEET-2026-04-07-039). Must match engine's value.
        LOOPZERO_SERVICE_AUTH_SECRET: config.serviceAuthSecret || serviceAuthSecret.result,
      },
    },
    { provider, dependsOn: [ns] },
  );

  // Service
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

  // Deployment
  new k8s.apps.v1.Deployment(
    "loopzero-cloud",
    {
      metadata: { name: "loopzero-cloud", namespace: PLATFORM_NAMESPACE },
      spec: {
        replicas: 1,
        selector: { matchLabels: { app: "loopzero-cloud" } },
        template: {
          metadata: { labels: { app: "loopzero-cloud" } },
          spec: {
            serviceAccountName: "default",
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
        ports: [{ name: "http", port: 80, targetPort: 80 }],
      },
    },
    { provider, dependsOn: [ns] },
  );

  new k8s.apps.v1.Deployment(
    "loopzero-dashboard",
    {
      metadata: { name: "loopzero-dashboard", namespace: PLATFORM_NAMESPACE },
      spec: {
        replicas: 1,
        selector: { matchLabels: { app: "loopzero-dashboard" } },
        template: {
          metadata: { labels: { app: "loopzero-dashboard" } },
          spec: {
            imagePullSecrets: [{ name: registrySecretName }],
            containers: [{
              name: "dashboard",
              image: `registry.digitalocean.com/loopzero-system/loopzero-dashboard:${imageTag}`,
              imagePullPolicy: "Always",
              ports: [{ containerPort: 80 }],
              resources: {
                requests: { cpu: "10m", memory: "16Mi" },
                limits: { cpu: "100m", memory: "64Mi" },
              },
              readinessProbe: {
                httpGet: { path: "/", port: 80 },
                initialDelaySeconds: 2,
                periodSeconds: 10,
              },
            }],
          },
        },
      },
    },
    { provider, dependsOn: [ns] },
  );
}
