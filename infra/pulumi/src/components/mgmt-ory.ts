// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Extracted from mgmt-bootstrap.ts — Ory Kratos + Hydra installation.

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import * as random from "@pulumi/random";
import * as fs from "fs";
import * as path from "path";
import { InfraOutputs } from "./infra";
import { PlatformStackConfig } from "../config/stack";
import { PLATFORM_NAMESPACE, HELM_VERSIONS } from "./bootstrap-shared";

/**
 * Ory Kratos — Identity management (signup, login, recovery, verification).
 * Uses the managed PostgreSQL instance with a dedicated `kratos` database.
 * Configured with email+password and OIDC (GitHub, Google) identity methods.
 */
export function installOryKratos(
  provider: k8s.Provider,
  ns: k8s.core.v1.Namespace,
  infra: InfraOutputs,
  config: PlatformStackConfig,
  cookieSecret: random.RandomPassword,
): k8s.helm.v3.Release {
  const kratosValuesPath = path.resolve(
    __dirname,
    "../../../helm/ory-kratos/values.yaml",
  );
  const kratosValues = fs.existsSync(kratosValuesPath)
    ? kratosValuesPath
    : undefined;

  const kratosDsn = infra.database
    ? pulumi.interpolate`postgres://${infra.database.user}:${infra.database.password}@${infra.database.privateHost}:${infra.database.port}/kratos?sslmode=require`
    : undefined;

  const release = new k8s.helm.v3.Release(
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
                        body: "base64://ZnVuY3Rpb24oY3R4KSB7CiAgZnJvbTogIlRoZW8gPG5vcmVwbHlAdXNldGhlby5kZXY+IiwKICB0bzogW2N0eC5yZWNpcGllbnRdLAogIHN1YmplY3Q6IGN0eC5zdWJqZWN0LAogIGh0bWw6IGN0eC5odG1sX2JvZHksCn0=",
                      },
                    },
                  },
                }
              : {}),
            serve: {
              public: {
                base_url: `https://cloud.${config.domain}/`,
              },
            },
            cookies: {
              domain: `.${config.domain}`,
              same_site: "Lax",
            },
            session: {
              cookie: {
                domain: `.${config.domain}`,
                same_site: "Lax",
              },
              lifespan: "24h",
            },
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

  // Kratos Self-Service UI
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
                { name: "KRATOS_PUBLIC_URL", value: `http://ory-kratos-public.${PLATFORM_NAMESPACE}.svc.cluster.local:80` },
                { name: "HYDRA_ADMIN_URL", value: `http://ory-hydra-admin.${PLATFORM_NAMESPACE}.svc.cluster.local:4445` },
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

  return release;
}

/**
 * Ory Hydra — OAuth2 / OpenID Connect server.
 * Issues JWT access tokens (RFC 8628 Device Code Flow for CLI, PKCE for dashboard).
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

  const hydraDsn = infra.database
    ? pulumi.interpolate`postgres://${infra.database.user}:${infra.database.password}@${infra.database.privateHost}:${infra.database.port}/hydra?sslmode=require`
    : undefined;

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
              dev: false,
              config: {
                ...(hydraDsn ? { dsn: hydraDsn } : {}),
                urls: {
                  self: { issuer: issuerUrl },
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
    { provider, dependsOn: [ns, kratos] },
  );
}
