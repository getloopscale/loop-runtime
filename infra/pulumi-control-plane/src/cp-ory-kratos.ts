// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Ory Kratos installation + Kratos Self-Service UI deployment.

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import * as random from "@pulumi/random";
import * as fs from "fs";
import * as path from "path";
import { PLATFORM_NAMESPACE, HELM_VERSIONS, InfraOutputs, PlatformStackConfig } from "./mgmt-control-plane-services";

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
              // SB-1: TOTP MFA + recovery codes (lookup_secret).
              // Enabled for all users. Enforcement via selfservice.flows.settings.
              methods: {
                totp: {
                  enabled: true,
                  config: {
                    issuer: "LoopZero Platform",
                  },
                },
                lookup_secret: {
                  enabled: true, // Recovery codes as MFA backup
                },
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
