// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Control-plane services extracted from monolithic mgmt-bootstrap.ts.
// Contains: LoopZero API, Cloud Service, Tenant Controller, Ory Kratos/Hydra,
// IngressRoutes, NetworkPolicies, runtime cluster registration, scoped kubeconfigs.

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import * as random from "@pulumi/random";
import * as fs from "fs";
import * as path from "path";

const PLATFORM_NAMESPACE = "platform-system";

// Type aliases for migration — avoids importing from monolith
type InfraOutputs = any;
type ClusterOutputs = any;
type PlatformStackConfig = any;
type CloudflareOutputs = any;

// Helm chart versions — must match platform project
const HELM_VERSIONS = {
  oryKratos: "0.61.0",
  oryHydra: "0.61.0",
} as const;

// IngressRoute definitions for platform services
const PLATFORM_INGRESS_ROUTES = [
  { subdomain: "argocd", serviceName: "argocd-server", servicePort: 80 },
  { subdomain: "git", serviceName: "gitea-http", servicePort: 3000 },
  { subdomain: "grafana", serviceName: "grafana", servicePort: 3000 },
] as const;

// RBAC rule interface for scoped kubeconfig creation
interface RbacRule {
  apiGroups: string[];
  resources: string[];
  verbs: string[];
}

/** Parse DOKS kubeconfig YAML to extract bearer token and CA certificate */
function parseKubeconfig(raw: string): { token: string; certificateAuthorityData: string; server: string } {
  let token = "";
  let certificateAuthorityData = "";
  let server = "";
  for (const line of raw.split("\n")) {
    const trimmed = line.trim();
    if (trimmed.startsWith("token:")) token = trimmed.split("token:")[1]?.trim() ?? "";
    if (trimmed.startsWith("certificate-authority-data:")) certificateAuthorityData = trimmed.split("certificate-authority-data:")[1]?.trim() ?? "";
    if (trimmed.startsWith("server:")) server = trimmed.split("server:")[1]?.trim() ?? "";
  }
  return { token, certificateAuthorityData, server };
}

// H12-fix: Service auth secret auto-generated when not provided via config.
// Shared between Cloud service and Engine API — must be the same value.
// Persisted in Pulumi state, stable across deployments.
const serviceAuthSecret = new random.RandomPassword("service-auth-secret", {
  length: 48,
  special: false,
});

// --- Control Plane Services ---

/**
 * (see original source for full JSDoc)
 */
export function installOryKratos(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  config: PlatformStackConfig,
): k8s.helm.v3.Release {
  // C1-fix: Generate cookie secret dynamically instead of hardcoded string.
  // Persisted in Pulumi state, rotated only on explicit `pulumi destroy` + recreate.
  const cookieSecret = new random.RandomPassword("kratos-ui-cookie-secret", {
    length: 32,
    special: false,
  });

  const kratosValuesPath = path.resolve(
    __dirname,
    "../../../helm/ory-kratos/values.yaml",
  );
  const kratosValues = fs.existsSync(kratosValuesPath)
    ? kratosValuesPath
    : undefined;

  // Build DSN for Kratos database.
  // Production: managed PostgreSQL (sslmode=require).
  // Local dev: falls back to values.yaml default.
  const kratosDsn = infra.database
    ? pulumi.interpolate`postgres://${infra.database.user}:${infra.database.password}@${infra.database.privateHost}:${infra.database.port}/kratos?sslmode=require`
    : undefined;

  return new k8s.helm.v3.Release(
    "ory-kratos",
    {
      name: "ory-kratos",
      chart: "kratos",
      version: HELM_VERSIONS.oryKratos,
      repositoryOpts: {
        repo: "https://k8s.ory.sh/helm/charts",
      },
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      ...(kratosValues ? { valueYamlFiles: [new pulumi.asset.FileAsset(kratosValues)] } : {}),
      values: {
        fullnameOverride: "ory-kratos",
        kratos: {
          config: {
            ...(kratosDsn ? { dsn: kratosDsn } : {}),
            // MEET-2026-04-06-036: Email via Resend HTTP API (SMTP blocked by DO).
            // Kratos HTTP courier calls Resend directly — no SMTP needed.
            ...(config.resendApiKey
              ? {
                  courier: {
                    delivery_strategy: "http",
                    http: {
                      request_config: {
                        url: "https://api.resend.com/emails",
                        method: "POST",
                        headers: {
                          "Content-Type": "application/json",
                          Authorization: pulumi.interpolate`Bearer ${config.resendApiKey}`,
                        },
                        // Jsonnet: ctx.recipient, ctx.subject, ctx.html_body (lowercase snake_case)
                        body: "base64://ZnVuY3Rpb24oY3R4KSB7CiAgZnJvbTogIlRoZW8gPG5vcmVwbHlAdXNldGhlby5kZXY+IiwKICB0bzogW2N0eC5yZWNpcGllbnRdLAogIHN1YmplY3Q6IGN0eC5zdWJqZWN0LAogIGh0bWw6IGN0eC5odG1sX2JvZHksCn0=",
                      },
                    },
                  },
                }
              : {}),
            // Kratos must know its public URL for CSRF, return_to validation, etc.
            serve: {
              public: {
                base_url: `https://cloud.${config.domain}/`,
              },
            },
            // Cookies must be accessible across subdomains (dashboard, accounts, api)
            // so the Kratos session cookie is sent with fetch requests from the dashboard.
            cookies: {
              domain: `.${config.domain}`,
              same_site: "Lax",
            },
            // Session cookie must also use the parent domain so the dashboard
            // (cloud.dev.loopzero.example) can send it on /sessions/whoami requests.
            // Without this, the session cookie defaults to the exact request host,
            // which breaks after OIDC redirect chains.
            session: {
              cookie: {
                domain: `.${config.domain}`,
                same_site: "Lax",
              },
              lifespan: "24h",
            },
            // Allow return_to URLs from dashboard and API (consent handler).
            // MEET-2026-04-05-032: Kratos UI at accounts.{domain} (same-origin, no CORS).
            // Dashboard is a pure PKCE client — never renders Kratos flows.
            selfservice: {
              default_browser_return_url: `https://cloud.${config.domain}/`,
              allowed_return_urls: [
                `https://cloud.${config.domain}/`,
              ],
              flows: {
                login: { ui_url: `https://cloud.${config.domain}/login` },
                registration: { ui_url: `https://cloud.${config.domain}/register` },
                verification: { ui_url: `https://cloud.${config.domain}/verification`, enabled: true, use: 'code' },
                recovery: { ui_url: `https://cloud.${config.domain}/recovery`, enabled: true },
                settings: { ui_url: `https://cloud.${config.domain}/settings` },
                error: { ui_url: `https://cloud.${config.domain}/error` },
                logout: { after: { default_browser_return_url: `https://cloud.${config.domain}/login` } },
              },
              // Social login providers — only included when secrets are configured.
              ...(config.githubOAuthClientId || config.googleOAuthClientId
                ? {
                    methods: {
                      oidc: {
                        enabled: true,
                        config: {
                          base_redirect_uri: `https://cloud.${config.domain}/`,
                          providers: [
                            ...(config.githubOAuthClientId
                              ? [
                                  {
                                    id: "github",
                                    provider: "github",
                                    client_id: config.githubOAuthClientId,
                                    client_secret: config.githubOAuthClientSecret,
                                    mapper_url:
                                      "base64://bG9jYWwgY2xhaW1zID0gc3RkLmV4dFZhcignY2xhaW1zJyk7CnsKICBpZGVudGl0eTogewogICAgdHJhaXRzOiB7CiAgICAgIGVtYWlsOiBjbGFpbXMuZW1haWwsCiAgICAgIFtpZiBzdGQub2JqZWN0SGFzKGNsYWltcywgJ25hbWUnKSAmJiBjbGFpbXMubmFtZSAhPSBudWxsIHRoZW4gJ25hbWUnXTogY2xhaW1zLm5hbWUsCiAgICB9LAogIH0sCn0K",
                                    scope: ["user:email"],
                                  },
                                ]
                              : []),
                            ...(config.googleOAuthClientId
                              ? [
                                  {
                                    id: "google",
                                    provider: "google",
                                    client_id: config.googleOAuthClientId,
                                    client_secret: config.googleOAuthClientSecret,
                                    mapper_url:
                                      "base64://bG9jYWwgY2xhaW1zID0gc3RkLmV4dFZhcignY2xhaW1zJyk7CnsKICBpZGVudGl0eTogewogICAgdHJhaXRzOiB7CiAgICAgIGVtYWlsOiBjbGFpbXMuZW1haWwsCiAgICAgIFtpZiBzdGQub2JqZWN0SGFzKGNsYWltcywgJ25hbWUnKSAmJiBjbGFpbXMubmFtZSAhPSBudWxsIHRoZW4gJ25hbWUnXTogY2xhaW1zLm5hbWUsCiAgICB9LAogIH0sCn0K",
                                    scope: ["email", "profile"],
                                  },
                                ]
                              : []),
                          ],
                        },
                      },
                    },
                  }
                : {}),
            },
          },
        },
        resources: {
          requests: { cpu: "50m", memory: "128Mi" },
          limits: { cpu: "300m", memory: "256Mi" },
        },
      },
    },
    { provider, dependsOn: [ns] },
  );

  // MEET-2026-04-05-032: Kratos Self-Service UI — renders login/registration/recovery forms.
  // Deployed at accounts.{domain} (same origin as Kratos API — no CORS issues).
  // Uses ory/kratos-selfservice-ui-node (official, maintained by Ory).
  const kratosUiImage = "oryd/kratos-selfservice-ui-node:v1.3.0";

  new k8s.core.v1.Service(
    "kratos-ui",
    {
      metadata: { name: "kratos-ui", namespace: PLATFORM_NAMESPACE },
      spec: {
        selector: { app: "kratos-ui" },
        ports: [{ name: "http", port: 3000, targetPort: 3000 }],
      },
    },
    { provider, dependsOn: [ns] },
  );

  new k8s.apps.v1.Deployment(
    "kratos-ui",
    {
      metadata: { name: "kratos-ui", namespace: PLATFORM_NAMESPACE },
      spec: {
        replicas: 1,
        selector: { matchLabels: { app: "kratos-ui" } },
        template: {
          metadata: { labels: { app: "kratos-ui" } },
          spec: {
            containers: [{
              name: "ui",
              image: kratosUiImage,
              ports: [{ containerPort: 3000 }],
              env: [
                // Kratos public URL (internal — same cluster, no TLS needed)
                { name: "KRATOS_PUBLIC_URL", value: `http://ory-kratos-public.${PLATFORM_NAMESPACE}.svc.cluster.local:80` },
                // Hydra admin URL (for consent/login challenge resolution)
                { name: "HYDRA_ADMIN_URL", value: `http://ory-hydra-admin.${PLATFORM_NAMESPACE}.svc.cluster.local:4445` },
                // Base URL for CSRF cookie domain
                { name: "KRATOS_BROWSER_URL", value: `https://accounts.${config.domain}` },
                { name: "PORT", value: "3000" },
                { name: "SECURITY_MODE", value: "" },
                { name: "COOKIE_SECRET", value: cookieSecret.result },
                { name: "CSRF_COOKIE_NAME", value: "loopzero_csrf" },
              ],
              resources: {
                requests: { cpu: "20m", memory: "32Mi" },
                limits: { cpu: "100m", memory: "64Mi" },
              },
              readinessProbe: {
                httpGet: { path: "/health/alive", port: 3000 },
                initialDelaySeconds: 5,
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

/**
 * (see original source for full JSDoc)
 */
export function installOryHydra(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  config: PlatformStackConfig,
  kratos: k8s.helm.v3.Release,
): k8s.helm.v3.Release {
  const hydraValuesPath = path.resolve(
    __dirname,
    "../../../helm/ory-hydra/values.yaml",
  );
  const hydraValues = fs.existsSync(hydraValuesPath)
    ? hydraValuesPath
    : undefined;

  // Build DSN for Hydra database.
  const hydraDsn = infra.database
    ? pulumi.interpolate`postgres://${infra.database.user}:${infra.database.password}@${infra.database.privateHost}:${infra.database.port}/hydra?sslmode=require`
    : undefined;

  // Issuer URL must match the public-facing auth domain.
  // Always set — dev mode uses auth.dev.loopzero.example, prod uses auth.loopzero.example.
  // Hydra issuer URL must match the browser-facing domain where Hydra is reachable.
  const authDomain = config.domain || "dev.loopzero.example";
  const issuerUrl = `https://auth.${authDomain}/`;

  return new k8s.helm.v3.Release(
    "ory-hydra",
    {
      name: "ory-hydra",
      chart: "hydra",
      version: HELM_VERSIONS.oryHydra,
      repositoryOpts: {
        repo: "https://k8s.ory.sh/helm/charts",
      },
      namespace: PLATFORM_NAMESPACE,
      createNamespace: false,
      ...(hydraValues ? { valueYamlFiles: [new pulumi.asset.FileAsset(hydraValues)] } : {}),
      values: {
        fullnameOverride: "ory-hydra",
        hydra: {
              dev: false, // Disable dev mode — use real HTTPS issuer
              config: {
                ...(hydraDsn ? { dsn: hydraDsn } : {}),
                urls: {
                  self: { issuer: issuerUrl },
                  // Cloud service handles login/consent/logout flows
                  // Consent handler is at api.{domain}/auth/* via API gateway
                  login: `https://api.${authDomain}/auth/login`,
                  consent: `https://api.${authDomain}/auth/consent`,
                  logout: `https://api.${authDomain}/auth/logout`,
                  device: {
                    verification: `https://cloud.${authDomain}/auth/device`,
                    success: `https://cloud.${authDomain}/device-success`,
                  },
                },
                serve: {
                  public: {
                    cors: {
                      enabled: true,
                      allowed_origins: [
                        `https://cloud.${authDomain}`,
                      ],
                      allowed_methods: ["GET", "POST", "PUT", "DELETE", "OPTIONS"],
                      allowed_headers: ["Authorization", "Content-Type", "Cookie"],
                      allow_credentials: true,
                    },
                  },
                },
              },
            },
        resources: {
          requests: { cpu: "50m", memory: "128Mi" },
          limits: { cpu: "300m", memory: "256Mi" },
        },
      },
    },
    // Hydra depends on Kratos being deployed first (same auth system)
    { provider, dependsOn: [ns, kratos] },
  );
}

// ---------------------------------------------------------------------------
// Cloud Auth Service (OAuth2 + Billing + Dashboard)
// ---------------------------------------------------------------------------

export function installCloudService(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  config: PlatformStackConfig,
): void {
  const registrySecretName = "loopzero-infra-registry";
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
        // Cloud→Engine proxy auth (MEET-2026-04-07-039). Must match engine's value.
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
              image: `registry.digitalocean.com/loopzero-infra-registry/loopzero-cloud:${imageTag}`,
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
              image: `registry.digitalocean.com/loopzero-infra-registry/loopzero-dashboard:${imageTag}`,
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

// ---------------------------------------------------------------------------
// LoopZero API
// ---------------------------------------------------------------------------

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
  // When not set, falls back to admin user (acceptable for dev only).
  // PG keepalive parameters: detect dead connections in ~90s (MEET-042 P0).
  // Default tcp_keepalives_idle on DO managed PG is 7200s (2 hours).
  // This causes stale advisory locks after pod kills, blocking leader election.
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
        // MEET-2026-04-09-045: Same RandomPassword used in ArgoCD notifications ConfigMap.
        // Single source of truth — no drift between ArgoCD and API.
        LOOPZERO_ARGO_CALLBACK_SECRET: callbackToken.result,
        // Spaces credentials (S3-compatible) — used by S3 uploader for source bundles
        MINIO_ACCESS_KEY: config.spacesAccessKeyId,
        MINIO_SECRET_KEY: config.spacesSecretAccessKey,
        // Cloud→Engine proxy auth (MEET-2026-04-07-039). Must match cloud's value.
        // Engine is RSA-only — no SESSION_TOKEN_SECRET needed (HMAC dropped).
        LOOPZERO_SERVICE_AUTH_SECRET: config.serviceAuthSecret || serviceAuthSecret.result,
      },
    },
    { provider, dependsOn: [ns] },
  );

  // Cloudflare credentials for static frontend delivery (Sprint 10).
  // Only created when CF config values are provided AND KV namespace exists.
  // cfKvNamespaceId comes from createStaticDelivery output (single source of truth),
  // NOT from Pulumi config — eliminates dual source of truth bug.
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

  // Database migrations — read SQL files and embed in a ConfigMap.
  // The init container (migrate/migrate) mounts this and applies them.
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

  // REVIEW-2026-03-30: Fixes CRIT-02 — was storing full admin kubeconfigs.
  // Now uses scoped ServiceAccount tokens with minimal RBAC per cluster.
  // API needs: submit/watch Workflows (build), get/list pods (runtime), Secrets (mgmt).
  // The kubeconfigs still use the cluster endpoints from Phase 1, but the auth
  // tokens are scoped ServiceAccount tokens instead of DOKS admin tokens.
  const buildScopedKubeconfig = createScopedKubeconfig(
    "build",
    infra.buildCluster,
    "argo",
    [
      // Submit, get, list, watch, delete Argo Workflows
      { apiGroups: ["argoproj.io"], resources: ["workflows"], verbs: ["create", "get", "list", "watch", "delete"] },
      // Create/get ConfigMaps (workflow parameters)
      { apiGroups: [""], resources: ["configmaps"], verbs: ["create", "get", "list"] },
      // Get pods (for build status)
      { apiGroups: [""], resources: ["pods", "pods/log"], verbs: ["get", "list", "watch"] },
    ],
  );

  const runtimeScopedKubeconfig = createScopedKubeconfig(
    "runtime",
    infra.runtimeCluster,
    PLATFORM_NAMESPACE,
    [
      // Get pods and services (health check, status)
      { apiGroups: [""], resources: ["pods", "services", "endpoints"], verbs: ["get", "list", "watch"] },
      // Manage namespaces (for tenant namespaces — read only for API)
      { apiGroups: [""], resources: ["namespaces"], verbs: ["get", "list"] },
    ],
  );

  const kubeconfigSecret = new k8s.core.v1.Secret(
    "loopzero-api-kubeconfig",
    {
      metadata: { name: "loopzero-api-kubeconfig", namespace: PLATFORM_NAMESPACE },
      stringData: {
        // mgmt: API runs here — uses in-cluster SA (no kubeconfig needed for mgmt).
        // But some code paths reference it, so provide a minimal config.
        "mgmt-kubeconfig": infra.mgmtCluster.kubeconfig,
        "build-kubeconfig": buildScopedKubeconfig,
        "runtime-kubeconfig": runtimeScopedKubeconfig,
      },
    },
    { provider, dependsOn: [ns] },
  );

  // ServiceAccount for loopzero-api — provides RBAC for Tenant CR management.
  // The API needs to create/update Tenant CRs on the mgmt cluster when users
  // run `loopzero init` or `loopzero deploy` for the first time. Without this SA,
  // the API uses the default SA which has no permissions for loopzero.io CRDs.
  //
  // imagePullSecrets: doctl kubernetes cluster registry add configures the
  // default SA in the default namespace, not custom SAs in platform-system.
  // Without this, API pods get 401 Unauthorized pulling from DO registry.
  const registrySecretName = "loopzero-infra-registry";
  const apiSA = new k8s.core.v1.ServiceAccount(
    "loopzero-api-sa",
    {
      metadata: {
        name: "loopzero-api",
        namespace: PLATFORM_NAMESPACE,
      },
      imagePullSecrets: [{ name: registrySecretName }],
    },
    { provider, dependsOn: [ns] },
  );

  // ClusterRole for loopzero-api — minimal RBAC for Tenant CR operations.
  // Only get/create/update/patch — no delete (tenant deletion goes through operator).
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
        {
          kind: "ServiceAccount",
          name: "loopzero-api",
          namespace: PLATFORM_NAMESPACE,
        },
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
        // Tenant CRs live in the same namespace as the platform services.
        // Local dev uses loopzero-system; production uses platform-system.
        TENANT_NAMESPACE: PLATFORM_NAMESPACE,
        LOOPZERO_ENV: config.environment === "prod" ? "production" : "development",
        BASE_DOMAIN: config.domain,
        WORKLOAD_INGRESS_URL: `https://${config.domain}`,
        GITEA_URL: `http://gitea-http.${PLATFORM_NAMESPACE}.svc.cluster.local:3000`,
        ARGOCD_URL: `http://argocd-server.${PLATFORM_NAMESPACE}.svc.cluster.local:80`,
        ARGOCD_INSECURE: "true",
        // JWT validation for dashboard session tokens + CLI Hydra JWTs
        JWKS_URL: `https://auth.${config.domain}/.well-known/jwks.json`,
        JWT_ISSUER: `https://auth.${config.domain}/`,
        ARGO_NAMESPACE: "argo",
        MGMT_KUBECONFIG: "/etc/loopzero/mgmt-kubeconfig",
        // Mgmt uses the DOKS admin kubeconfig (API runs on mgmt cluster).
        // Build/runtime use scoped kubeconfigs with role-based context names.
        MGMT_KUBE_CONTEXT: pulumi.interpolate`do-${config.region}-${infra.mgmtCluster.clusterName}`,
        BUILD_KUBECONFIG: "/etc/loopzero/build-kubeconfig",
        BUILD_KUBE_CONTEXT: "build",
        RUNTIME_KUBECONFIG: "/etc/loopzero/runtime-kubeconfig",
        RUNTIME_KUBE_CONTEXT: "runtime",
        MINIO_ENDPOINT: infra.spacesEndpoint,
        REGISTRY_URL: infra.registryEndpoint,
        REGISTRY_INSECURE: "false",
        ARGOCD_NAMESPACE: PLATFORM_NAMESPACE,
        // Release reaper PENDING timeout must exceed ArgoCD sync time.
        // On DO, sync can take 3-5 min (image pull + pod scheduling).
        // Default 2m is too short — reaper kills releases before sync completes.
        // AUDIT-2026-03-28: raised to 10m to match RELEASING timeout.
        RELEASE_PENDING_TIMEOUT: "10m",
        // Loki runs on build cluster. API accesses via cross-cluster NodePort 30100.
        // Node IP resolved dynamically from build cluster node list.
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

  // REVIEW-2026-03-30: Fixes HIGH-02 — API had no PDB, both pods could be evicted during drain.
  new k8s.policy.v1.PodDisruptionBudget(
    "loopzero-api-pdb",
    {
      metadata: {
        name: "loopzero-api",
        namespace: PLATFORM_NAMESPACE,
      },
      spec: {
        minAvailable: 1,
        selector: {
          matchLabels: { app: "loopzero-api" },
        },
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
        annotations: {
          "pulumi.com/skipAwait": "true",
        },
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
            // Database migrations run before API starts via init container.
            // The API image includes migration SQL files at /migrations/.
            initContainers: [
              {
                name: "db-migrate",
                // Uses the SAME image as the API container — migrations are always in sync.
                // No more ConfigMap desync: the image that runs the API also runs migrations.
                // The migrate binary is included in the API image (Dockerfile multi-stage).
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
                    valueFrom: {
                      secretKeyRef: {
                        name: "loopzero-api-secrets",
                        key: "DATABASE_URL",
                      },
                    },
                  },
                ],
                // No volumeMounts needed — migrations are baked into the API image at /migrations/
                resources: {
                  requests: { cpu: "50m", memory: "64Mi" },
                  limits: { cpu: "100m", memory: "128Mi" },
                },
              },
            ],
            volumes: [
              {
                name: "kubeconfig",
                secret: { secretName: "loopzero-api-kubeconfig" },
              },
              {
                name: "api-migrations",
                configMap: { name: "loopzero-api-migrations" },
              },
            ],
            containers: [
              {
                name: "api",
                // REVIEW-2026-03-30: Fixes HIGH-08 — uses infra output instead of hardcoded constant.
                image: pulumi.interpolate`${infra.registryEndpoint}/loopzero-api:${config.environment}`,
                imagePullPolicy: "Always",
                ports: [
                  { name: "http", containerPort: 8080 },
                  { name: "metrics", containerPort: 9090 },
                ],
                envFrom: [
                  { configMapRef: { name: "loopzero-api-config" } },
                  { secretRef: { name: "loopzero-api-secrets" } },
                  // Gitea token + ArgoCD password — created by gitea-bootstrap Job
                  { secretRef: { name: "loopzero-platform-tokens" } },
                  // Cloudflare KV credentials for static frontend routing (Sprint 10).
                  // Optional: secret only exists when CF config is set.
                  ...(config.cfApiToken ? [{ secretRef: { name: "loopzero-cf-credentials" } }] : []),
                  // GitHub App credentials for auto-deploy on push (MEET-042 P0).
                  // Optional: secret only exists when GitHub App is configured.
                  { secretRef: { name: "loopzero-github-app", optional: true } },
                ],
                env: [
                  // CF Zone Management for custom domains v2 (MEET-042 P0).
                  ...(config.cfApiToken ? [{
                    name: "CF_ZONE_MANAGEMENT",
                    value: "true",
                  }] : []),
                  // OpenTelemetry: send traces to OTEL Collector for sampling + batching.
                  // Collector forwards to Tempo. If collector is down, API degrades gracefully
                  // (traces dropped, no impact on deploy flow).
                  {
                    name: "OTEL_EXPORTER_OTLP_ENDPOINT",
                    value: `otel-collector.${PLATFORM_NAMESPACE}.svc:4317`,
                  },
                ],
                volumeMounts: [
                  {
                    name: "kubeconfig",
                    mountPath: "/etc/loopzero",
                    readOnly: true,
                  },
                ],
                resources: {
                  requests: { cpu: "200m", memory: "512Mi" },
                  limits: { cpu: "1", memory: "1Gi" },
                },
                // startupProbe: covers leader election (up to 60s for non-leaders).
                // Without this, liveness kills the pod before election completes.
                // MEET-045: was kubectl patch — now permanent in Pulumi.
                startupProbe: {
                  httpGet: { path: "/api/v1/health", port: 8080 },
                  initialDelaySeconds: 5,
                  periodSeconds: 10,
                  failureThreshold: 12, // 5s + 12×10s = 125s max startup
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

// ---------------------------------------------------------------------------
// Tenant Controller
// ---------------------------------------------------------------------------

export function installTenantController(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  config: PlatformStackConfig,
): k8s.apps.v1.Deployment {
  // CRDs — LoopZero custom resource definitions.
  // Path is relative to infra/pulumi/ (the Pulumi working directory).
  const crds = new k8s.yaml.ConfigGroup(
    "loopzero-crds",
    {
      files: ["../../operators/config/crd/bases/*.yaml"],
    },
    { provider },
  );

  // ServiceAccount for the controller
  const sa = new k8s.core.v1.ServiceAccount(
    "tenant-controller-sa",
    {
      metadata: {
        name: "tenant-controller",
        namespace: PLATFORM_NAMESPACE,
      },
      imagePullSecrets: [{ name: "loopzero-infra-registry" }],
    },
    { provider, dependsOn: [ns] },
  );

  // ClusterRole — permissions for tenant lifecycle management
  const role = new k8s.rbac.v1.ClusterRole(
    "tenant-controller-role",
    {
      metadata: { name: "tenant-controller" },
      rules: [
        // LoopZero CRDs — explicitly enumerated for least-privilege.
        // When adding a new CRD, add it here and review access requirements.
        {
          apiGroups: ["loopzero.io"],
          resources: [
            "tenants", "environments", "loopzeroapplications",
            "exposedroutes", "platformbootstraps", "gitrepositorybindings",
            "clustercredentials", "appsecrets", "previewenvironments", "reconciliationreports",
          ],
          verbs: ["get", "list", "watch", "create", "update", "patch", "delete"],
        },
        {
          apiGroups: ["loopzero.io"],
          resources: [
            "tenants/status", "tenants/finalizers",
            "environments/status", "environments/finalizers",
            "loopzeroapplications/status", "loopzeroapplications/finalizers",
            "exposedroutes/status", "exposedroutes/finalizers",
            "platformbootstraps/status", "platformbootstraps/finalizers",
            "gitrepositorybindings/status", "gitrepositorybindings/finalizers",
            "clustercredentials/status", "clustercredentials/finalizers",
            "appsecrets/status", "appsecrets/finalizers",
            "previewenvironments/status", "previewenvironments/finalizers",
            "reconciliationreports/status", "reconciliationreports/finalizers",
          ],
          verbs: ["get", "update", "patch"],
        },
        // Namespace lifecycle
        {
          apiGroups: [""],
          resources: ["namespaces"],
          verbs: ["get", "list", "watch", "create", "update", "patch"],
        },
        // Resource management per tenant namespace
        {
          apiGroups: [""],
          resources: ["resourcequotas", "limitranges", "secrets", "serviceaccounts"],
          verbs: ["get", "list", "watch", "create", "update", "patch"],
        },
        // Network isolation
        {
          apiGroups: ["networking.k8s.io"],
          resources: ["networkpolicies"],
          verbs: ["get", "list", "watch", "create", "update", "patch"],
        },
        // RBAC per tenant
        {
          apiGroups: ["rbac.authorization.k8s.io"],
          resources: ["roles", "rolebindings", "clusterroles", "clusterrolebindings"],
          verbs: ["get", "list", "watch", "create", "update", "patch", "bind"],
        },
        // ArgoCD resources (AppProjects + Applications per tenant)
        {
          apiGroups: ["argoproj.io"],
          resources: ["appprojects", "applications"],
          verbs: ["get", "list", "watch", "create", "update", "patch", "delete"],
        },
        // Leader election
        {
          apiGroups: ["coordination.k8s.io"],
          resources: ["leases"],
          verbs: ["get", "list", "watch", "create", "update", "patch"],
        },
        // Events
        {
          apiGroups: [""],
          resources: ["events"],
          verbs: ["create", "patch"],
        },
      ],
    },
    { provider },
  );

  const binding = new k8s.rbac.v1.ClusterRoleBinding(
    "tenant-controller-binding",
    {
      metadata: { name: "tenant-controller" },
      subjects: [
        {
          kind: "ServiceAccount",
          name: "tenant-controller",
          namespace: PLATFORM_NAMESPACE,
        },
      ],
      roleRef: {
        kind: "ClusterRole",
        name: "tenant-controller",
        apiGroup: "rbac.authorization.k8s.io",
      },
    },
    { provider, dependsOn: [role, sa] },
  );

  // REVIEW-2026-03-30: Fixes CRIT-02 — controller kubeconfigs now use scoped tokens.
  // Controller needs: create namespaces + RBAC + quotas (runtime), manage Secrets (build).
  const controllerBuildKubeconfig = createScopedKubeconfig(
    "ctrl-build",
    infra.buildCluster,
    "argo",
    [
      // Tenant controller creates per-tenant build namespaces with quotas, limits, RBAC,
      // network policies, and Argo workflow executor roles.
      // Must match runtime cluster RBAC (below) — both clusters need the same resource access.
      { apiGroups: [""], resources: ["namespaces", "secrets", "serviceaccounts", "resourcequotas", "limitranges"], verbs: ["get", "list", "create", "update", "patch"] },
      { apiGroups: ["networking.k8s.io"], resources: ["networkpolicies"], verbs: ["get", "list", "create", "update", "patch"] },
      { apiGroups: ["rbac.authorization.k8s.io"], resources: ["roles", "rolebindings"], verbs: ["get", "list", "create", "update", "patch", "bind"] },
    ],
  );
  const controllerRuntimeKubeconfig = createScopedKubeconfig(
    "ctrl-runtime",
    infra.runtimeCluster,
    PLATFORM_NAMESPACE,
    [
      { apiGroups: [""], resources: ["namespaces", "secrets", "serviceaccounts", "resourcequotas", "limitranges"], verbs: ["get", "list", "watch", "create", "update", "patch"] },
      { apiGroups: ["networking.k8s.io"], resources: ["networkpolicies"], verbs: ["get", "list", "create", "update", "patch"] },
      { apiGroups: ["rbac.authorization.k8s.io"], resources: ["roles", "rolebindings"], verbs: ["get", "list", "create", "update", "patch", "bind"] },
      // PriorityClasses are cluster-scoped. Tenant controller creates per-plan classes
      // (loopzero-tenant-starter, loopzero-tenant-growth, loopzero-tenant-enterprise) on the runtime cluster.
      { apiGroups: ["scheduling.k8s.io"], resources: ["priorityclasses"], verbs: ["get", "list", "create", "update", "patch"] },
    ],
  );
  const controllerKubeconfig = new k8s.core.v1.Secret(
    "tenant-controller-kubeconfig",
    {
      metadata: {
        name: "tenant-controller-kubeconfig",
        namespace: PLATFORM_NAMESPACE,
      },
      stringData: {
        "build-kubeconfig": controllerBuildKubeconfig,
        "runtime-kubeconfig": controllerRuntimeKubeconfig,
      },
    },
    { provider, dependsOn: [ns] },
  );

  // Secret with cloud provider credentials for the tenant controller.
  // These are used to create registry credentials and Spaces access in build namespaces.
  // Stored as Secret (not plain env vars) to prevent exposure via K8s API.
  const controllerCredentials = new k8s.core.v1.Secret(
    "tenant-controller-credentials",
    {
      metadata: {
        name: "tenant-controller-credentials",
        namespace: PLATFORM_NAMESPACE,
      },
      stringData: {
        SPACES_ACCESS_KEY_ID: config.spacesAccessKeyId,
        SPACES_SECRET_ACCESS_KEY: config.spacesSecretAccessKey,
        DIGITALOCEAN_TOKEN: config.digitaloceanToken,
      },
    },
    { provider, dependsOn: [ns] },
  );

  const deploy = new k8s.apps.v1.Deployment(
    "tenant-controller",
    {
      metadata: {
        name: "tenant-controller",
        namespace: PLATFORM_NAMESPACE,
        labels: { app: "tenant-controller" },
        annotations: {
          // skipAwait: don't block on pod readiness. On fresh deploys,
          // images don't exist yet (built in Step 2). Step 3 converges.
          "pulumi.com/skipAwait": "true",
        },
      },
      spec: {
        replicas: 1,
        selector: { matchLabels: { app: "tenant-controller" } },
        template: {
          metadata: {
            labels: { app: "tenant-controller" },
            annotations: {
              "prometheus.io/scrape": "true",
              "prometheus.io/port": "8080",
            },
          },
          spec: {
            serviceAccountName: "tenant-controller",
            securityContext: {
              runAsNonRoot: true,
              runAsUser: 1000,
              fsGroup: 1000,
            },
            imagePullSecrets: [{ name: "loopzero-infra-registry" }],
            volumes: [
              {
                name: "kubeconfigs",
                secret: { secretName: "tenant-controller-kubeconfig" },
              },
            ],
            containers: [
              {
                name: "controller",
                image: pulumi.interpolate`${infra.registryEndpoint}/tenant-controller:${config.environment}`,
                imagePullPolicy: "Always",
                args: ["--leader-elect=true"],
                ports: [
                  { name: "metrics", containerPort: 8080 },
                  { name: "health", containerPort: 8081 },
                ],
                volumeMounts: [
                  {
                    name: "kubeconfigs",
                    mountPath: "/etc/loopzero",
                    readOnly: true,
                  },
                ],
                env: [
                  {
                    name: "GITEA_URL",
                    value: `http://gitea-http.${PLATFORM_NAMESPACE}.svc.cluster.local:3000`,
                  },
                  {
                    name: "GITEA_TOKEN",
                    valueFrom: {
                      secretKeyRef: {
                        name: "loopzero-platform-tokens",
                        key: "GITEA_TOKEN",
                      },
                    },
                  },
                  {
                    name: "BUILD_KUBECONFIG",
                    value: "/etc/loopzero/build-kubeconfig",
                  },
                  {
                    name: "RUNTIME_KUBECONFIG",
                    value: "/etc/loopzero/runtime-kubeconfig",
                  },
                  {
                    name: "WORKLOAD_CLUSTER_URL",
                    value: pulumi.interpolate`${infra.runtimeCluster.endpoint}`,
                  },
                  {
                    name: "ARGOCD_NAMESPACE",
                    value: PLATFORM_NAMESPACE,
                  },
                  {
                    name: "BASE_DOMAIN",
                    value: config.domain,
                  },
                  // Object storage endpoint — S3-compatible (DO Spaces).
                  {
                    name: "MINIO_ENDPOINT",
                    value: infra.spacesEndpoint,
                  },
                  {
                    name: "SPACES_ACCESS_KEY_ID",
                    valueFrom: {
                      secretKeyRef: {
                        name: "tenant-controller-credentials",
                        key: "SPACES_ACCESS_KEY_ID",
                      },
                    },
                  },
                  {
                    name: "SPACES_SECRET_ACCESS_KEY",
                    valueFrom: {
                      secretKeyRef: {
                        name: "tenant-controller-credentials",
                        key: "SPACES_SECRET_ACCESS_KEY",
                      },
                    },
                  },
                  // DO Container Registry auth — BuildKit needs this for image push.
                  {
                    name: "DIGITALOCEAN_TOKEN",
                    valueFrom: {
                      secretKeyRef: {
                        name: "tenant-controller-credentials",
                        key: "DIGITALOCEAN_TOKEN",
                      },
                    },
                  },
                ],
                resources: {
                  requests: { cpu: "100m", memory: "128Mi" },
                  limits: { cpu: "500m", memory: "256Mi" },
                },
                livenessProbe: {
                  httpGet: { path: "/healthz", port: 8081 },
                  initialDelaySeconds: 15,
                },
                readinessProbe: {
                  httpGet: { path: "/readyz", port: 8081 },
                  initialDelaySeconds: 5,
                },
                securityContext: {
                  runAsNonRoot: true,
                  allowPrivilegeEscalation: false,
                  readOnlyRootFilesystem: true,
                  capabilities: { drop: ["ALL"] },
                },
              },
            ],
          },
        },
      },
    },
    {
      provider,
      dependsOn: [ns, crds, binding, controllerKubeconfig, controllerCredentials],
    },
  );

  // Service — exposes metrics and health ports for Prometheus scraping and readiness checks.
  new k8s.core.v1.Service(
    "tenant-controller-svc",
    {
      metadata: {
        name: "tenant-controller",
        namespace: PLATFORM_NAMESPACE,
        labels: { app: "tenant-controller" },
      },
      spec: {
        selector: { app: "tenant-controller" },
        ports: [
          { name: "metrics", port: 8080, targetPort: 8080 },
          { name: "health", port: 8081, targetPort: 8081 },
        ],
      },
    },
    { provider, dependsOn: [deploy] },
  );

  // PodDisruptionBudget — ensures at least 1 controller pod stays available during
  // voluntary disruptions (node drain, cluster upgrade).
  new k8s.policy.v1.PodDisruptionBudget(
    "tenant-controller-pdb",
    {
      metadata: {
        name: "tenant-controller",
        namespace: PLATFORM_NAMESPACE,
      },
      spec: {
        minAvailable: 1,
        selector: {
          matchLabels: { app: "tenant-controller" },
        },
      },
    },
    { provider },
  );

  return deploy;
}

export function registerRuntimeCluster(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  argocd: k8s.helm.v3.Release,
): void {
  // Extract bearerToken and caData from the DOKS kubeconfig.
  // DOKS kubeconfigs embed: clusters[0].cluster.certificate-authority-data + users[0].user.token
  const argocdConfig = infra.runtimeCluster.kubeconfig.apply((kc: string) => {
    // infra-arch-005: use typed parser instead of fragile regex
    const parsed = parseKubeconfig(kc);
    return JSON.stringify({
      bearerToken: parsed.token,
      tlsClientConfig: {
        caData: parsed.certificateAuthorityData,
      },
    });
  });

  new k8s.core.v1.Secret(
    "argocd-runtime-cluster",
    {
      metadata: {
        name: "runtime-cluster",
        namespace: PLATFORM_NAMESPACE,
        labels: {
          "argocd.argoproj.io/secret-type": "cluster",
        },
      },
      stringData: {
        name: "runtime",
        server: infra.runtimeCluster.endpoint,
        config: argocdConfig,
      },
    },
    { provider, dependsOn: [ns, argocd] },
  );
}

export function createAPIGatewayRoute(
  provider: k8s.Provider,
  config: PlatformStackConfig,
  traefik: k8s.helm.v3.Release,
  clusterIssuer: k8s.apiextensions.CustomResource,
): void {
  const fqdn = `api.${config.domain}`;
  const certName = "tls-api";

  // TLS Certificate
  const cert = new k8s.apiextensions.CustomResource(
    "cert-api",
    {
      apiVersion: "cert-manager.io/v1",
      kind: "Certificate",
      metadata: { name: certName, namespace: PLATFORM_NAMESPACE },
      spec: {
        secretName: `${certName}-secret`,
        issuerRef: { name: "letsencrypt-prod", kind: "ClusterIssuer" },
        dnsNames: [fqdn],
      },
    },
    { provider, dependsOn: [clusterIssuer] },
  );

  // IngressRoute with path-based routing (priority: specific paths first)
  new k8s.apiextensions.CustomResource(
    "ingress-api-gateway",
    {
      apiVersion: "traefik.io/v1alpha1",
      kind: "IngressRoute",
      metadata: {
        name: "api-gateway",
        namespace: PLATFORM_NAMESPACE,
        labels: {
          "app.kubernetes.io/managed-by": "pulumi",
          "app.kubernetes.io/part-of": "loopzero-platform",
        },
      },
      spec: {
        entryPoints: ["websecure"],
        routes: [
          // Cloud auth service routes (higher priority — matched first)
          {
            match: `Host(\`${fqdn}\`) && (PathPrefix(\`/v1/auth\`) || PathPrefix(\`/v1/billing\`) || PathPrefix(\`/v1/webhooks\`) || PathPrefix(\`/auth\`))`,
            kind: "Rule",
            priority: 100,
            services: [{ name: "loopzero-cloud", port: 13002, namespace: PLATFORM_NAMESPACE }],
          },
          // Default: all other routes go to loopzero-api
          {
            match: `Host(\`${fqdn}\`)`,
            kind: "Rule",
            priority: 1,
            services: [{ name: "loopzero-api", port: 8080, namespace: PLATFORM_NAMESPACE }],
          },
        ],
        tls: { secretName: `${certName}-secret` },
      },
    },
    { provider, dependsOn: [cert, traefik] },
  );
}

/**
 * (see original source for full JSDoc)
 */
export function createCloudGatewayRoute(
  provider: k8s.Provider,
  config: PlatformStackConfig,
  traefik: k8s.helm.v3.Release,
  clusterIssuer: k8s.apiextensions.CustomResource,
): void {
  const fqdn = `cloud.${config.domain}`;
  const certName = "tls-cloud";

  const cert = new k8s.apiextensions.CustomResource(
    "cert-cloud",
    {
      apiVersion: "cert-manager.io/v1",
      kind: "Certificate",
      metadata: { name: certName, namespace: PLATFORM_NAMESPACE },
      spec: {
        secretName: `${certName}-secret`,
        issuerRef: { name: "letsencrypt-prod", kind: "ClusterIssuer" },
        dnsNames: [fqdn],
      },
    },
    { provider, dependsOn: [clusterIssuer] },
  );

  new k8s.apiextensions.CustomResource(
    "ingress-cloud-gateway",
    {
      apiVersion: "traefik.io/v1alpha1",
      kind: "IngressRoute",
      metadata: {
        name: "cloud-gateway",
        namespace: PLATFORM_NAMESPACE,
        labels: {
          "app.kubernetes.io/managed-by": "pulumi",
          "app.kubernetes.io/part-of": "loopzero-platform",
        },
      },
      spec: {
        entryPoints: ["websecure"],
        routes: [
          {
            match: `Host(\`${fqdn}\`) && (PathPrefix(\`/oauth2\`) || PathPrefix(\`/.well-known\`))`,
            kind: "Rule",
            priority: 300,
            services: [{ name: "ory-hydra-public", port: 4444, namespace: PLATFORM_NAMESPACE }],
          },
          {
            match: `Host(\`${fqdn}\`) && (PathPrefix(\`/self-service\`) || PathPrefix(\`/sessions\`) || PathPrefix(\`/schemas\`))`,
            kind: "Rule",
            priority: 200,
            services: [{ name: "ory-kratos-public", port: 80, namespace: PLATFORM_NAMESPACE }],
          },
          {
            match: `Host(\`${fqdn}\`) && (PathPrefix(\`/v1/auth\`) || PathPrefix(\`/v1/billing\`) || PathPrefix(\`/v1/webhooks\`) || PathPrefix(\`/auth\`) || PathPrefix(\`/engine\`) || PathPrefix(\`/metrics\`) || PathPrefix(\`/health\`))`,
            kind: "Rule",
            priority: 100,
            services: [{ name: "loopzero-cloud", port: 13002, namespace: PLATFORM_NAMESPACE }],
          },
          {
            match: `Host(\`${fqdn}\`)`,
            kind: "Rule",
            priority: 1,
            services: [{ name: "loopzero-dashboard", port: 80, namespace: PLATFORM_NAMESPACE }],
          },
        ],
        tls: { secretName: `${certName}-secret` },
      },
    },
    { provider, dependsOn: [cert, traefik] },
  );
}

export function createPlatformIngressRoutes(
  provider: k8s.Provider,
  config: PlatformStackConfig,
  traefik: k8s.helm.v3.Release,
  clusterIssuer: k8s.apiextensions.CustomResource,
): pulumi.Output<string[]> {
  const createdRoutes: pulumi.Output<string>[] = [];

  for (const route of PLATFORM_INGRESS_ROUTES) {
    const fqdn =
      config.domain === config.dnsZone
        ? `${route.subdomain}.${config.domain}`
        : `${route.subdomain}.${config.domain}`;

    const certName = `tls-${route.subdomain}`;

    // Certificate resource — cert-manager watches this and issues a cert
    const cert = new k8s.apiextensions.CustomResource(
      `cert-${route.subdomain}`,
      {
        apiVersion: "cert-manager.io/v1",
        kind: "Certificate",
        metadata: {
          name: certName,
          namespace: PLATFORM_NAMESPACE,
        },
        spec: {
          secretName: `${certName}-secret`,
          issuerRef: {
            name: "letsencrypt-prod",
            kind: "ClusterIssuer",
          },
          dnsNames: [fqdn],
        },
      },
      { provider, dependsOn: [clusterIssuer] },
    );

    // IngressRoute — Traefik CRD for routing with TLS
    const ingressRoute = new k8s.apiextensions.CustomResource(
      `ingress-${route.subdomain}`,
      {
        apiVersion: "traefik.io/v1alpha1",
        kind: "IngressRoute",
        metadata: {
          name: `${route.subdomain}-ingress`,
          namespace: PLATFORM_NAMESPACE,
          labels: {
            "app.kubernetes.io/managed-by": "pulumi",
            "app.kubernetes.io/part-of": "loopzero-platform",
          },
        },
        spec: {
          entryPoints: ["websecure"],
          routes: [
            {
              match: `Host(\`${fqdn}\`)`,
              kind: "Rule",
              services: [
                {
                  name: route.serviceName,
                  port: route.servicePort,
                  namespace: PLATFORM_NAMESPACE,
                },
              ],
            },
          ],
          tls: {
            secretName: `${certName}-secret`,
          },
        },
      },
      { provider, dependsOn: [cert, traefik] },
    );

    createdRoutes.push(ingressRoute.metadata.name);
  }

  return pulumi.all(createdRoutes);
}

export function createPlatformNetworkPolicies(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
): void {
  // Default deny all ingress
  // Network isolation: allow all intra-namespace traffic + external webhook access.
  //
  // The platform namespace runs trusted services (cert-manager, ArgoCD, Gitea, MinIO,
  // Prometheus, Grafana, Traefik, loopzero-api, tenant-controller) that need to communicate
  // freely. Helm post-install hooks (Jobs) also need access to their parent services.
  //
  // Previous approach (default-deny + per-service allow-list) was brittle: every new
  // service or Helm hook required a new NetworkPolicy. Missing policies caused silent
  // failures (cert-manager webhook blocked, MinIO hook blocked).
  //
  // Current approach: allow all namespace-internal traffic + external access for
  // services that need it (webhooks from K8s API, Traefik from LoadBalancer).
  // Workload namespaces (tenant apps) use stricter isolation.
  new k8s.networking.v1.NetworkPolicy(
    "platform-network-policy",
    {
      metadata: {
        name: "platform-network-policy",
        namespace: PLATFORM_NAMESPACE,
      },
      spec: {
        podSelector: {},
        policyTypes: ["Ingress"],
        ingress: [
          {
            // Allow all traffic from pods within the same namespace
            from: [{ podSelector: {} }],
          },
          {
            // Allow external ingress on specific ports:
            // - 10250: cert-manager webhook (K8s API server calls)
            // - 8443: ArgoCD webhook
            // - 8000/8443: Traefik (LoadBalancer → ingress controller)
            // - 8080: loopzero-api, ArgoCD server (Traefik → backends)
            // - 3000: Gitea (ArgoCD, cross-namespace access)
            // - 9090/9402: Prometheus metrics scraping
            ports: [
              { port: 10250, protocol: "TCP" },
              { port: 8443, protocol: "TCP" },
              { port: 8000, protocol: "TCP" },
              { port: 8080, protocol: "TCP" },
              { port: 3000, protocol: "TCP" },
              { port: 9090, protocol: "TCP" },
              { port: 9402, protocol: "TCP" },
            ],
          },
        ],
      },
    },
    { provider, dependsOn: [ns] },
  );

  // Egress: allow all pods to reach external services.
  // Platform services need: DNS, K8s API, PostgreSQL (managed DO), HTTPS (GitHub,
  // DO Spaces, CF, Let's Encrypt), cross-cluster (build K8s API, Loki NodePort).
  new k8s.networking.v1.NetworkPolicy(
    "platform-egress-policy",
    {
      metadata: {
        name: "platform-egress-policy",
        namespace: PLATFORM_NAMESPACE,
      },
      spec: {
        podSelector: {},
        policyTypes: ["Egress"],
        egress: [
          // DNS
          { ports: [{ port: 53, protocol: "UDP" }, { port: 53, protocol: "TCP" }] },
          // K8s API server (DOKS managed — external, uses 443 or 6443)
          { ports: [{ port: 443, protocol: "TCP" }, { port: 6443, protocol: "TCP" }] },
          // PostgreSQL (managed DO database — port 25060)
          { ports: [{ port: 25060, protocol: "TCP" }] },
          // HTTP (Gitea internal, MinIO, Loki cross-cluster on NodePort)
          { ports: [{ port: 80, protocol: "TCP" }, { port: 8080, protocol: "TCP" }, { port: 9000, protocol: "TCP" }] },
          // Cross-cluster (build K8s API, Loki NodePort 30100, Argo WF metrics NodePort 30090)
          { ports: [{ port: 30100, protocol: "TCP" }, { port: 30090, protocol: "TCP" }] },
          // Intra-namespace (all pods can talk to each other)
          { to: [{ podSelector: {} }] },
        ],
      },
    },
    { provider, dependsOn: [ns] },
  );

  // MEET-2026-04-06-036: SMTP NetworkPolicy removed — DO blocks all SMTP ports.
  // Email delivery uses Resend HTTP API (port 443, already allowed).
}

export function createScopedKubeconfig(
  role: string,
  cluster: ClusterOutputs,
  namespace: string,
  rules: RbacRule[],
): pulumi.Output<string> {
  const k8sProvider = new k8s.Provider(`${role}-scoped-k8s`, {
    kubeconfig: cluster.kubeconfig,
  });

  const saName = `loopzero-${role}-sa`;

  const sa = new k8s.core.v1.ServiceAccount(
    `${role}-scoped-sa`,
    {
      metadata: { name: saName, namespace },
    },
    { provider: k8sProvider },
  );

  const clusterRole = new k8s.rbac.v1.ClusterRole(
    `${role}-scoped-role`,
    {
      metadata: { name: saName },
      rules: rules.map((r) => ({
        apiGroups: r.apiGroups,
        resources: r.resources,
        verbs: r.verbs,
      })),
    },
    { provider: k8sProvider },
  );

  new k8s.rbac.v1.ClusterRoleBinding(
    `${role}-scoped-binding`,
    {
      metadata: { name: saName },
      subjects: [{ kind: "ServiceAccount", name: saName, namespace }],
      roleRef: {
        kind: "ClusterRole",
        name: saName,
        apiGroup: "rbac.authorization.k8s.io",
      },
    },
    { provider: k8sProvider, dependsOn: [clusterRole, sa] },
  );

  // Long-lived token Secret — K8s 1.24+ requires explicit creation for SA tokens.
  const tokenSecret = new k8s.core.v1.Secret(
    `${role}-scoped-token`,
    {
      metadata: {
        name: `${saName}-token`,
        namespace,
        annotations: { "kubernetes.io/service-account.name": saName },
      },
      type: "kubernetes.io/service-account-token",
    },
    { provider: k8sProvider, dependsOn: [sa] },
  );

  // Build kubeconfig using scoped SA token instead of admin credentials.
  return pulumi
    .all([cluster.endpoint, tokenSecret.data])
    .apply(([endpoint, data]) => {
      // Pulumi returns Secret data as base64-encoded strings.
      // token: decode to UTF-8 for bearer auth.
      // ca.crt: already base64 — use directly as certificate-authority-data
      // (kubeconfig expects base64-encoded PEM in that field).
      const token = data?.["token"]
        ? Buffer.from(data["token"], "base64").toString("utf-8")
        : "";
      const ca = data?.["ca.crt"] ?? "";

      return `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: ${endpoint}
    certificate-authority-data: ${ca}
  name: ${role}
contexts:
- context:
    cluster: ${role}
    user: ${saName}
    namespace: ${namespace}
  name: ${role}
current-context: ${role}
users:
- name: ${saName}
  user:
    token: ${token}`;
    });
}
