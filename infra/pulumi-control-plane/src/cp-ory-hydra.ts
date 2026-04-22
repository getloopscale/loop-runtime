// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Ory Hydra installation (OAuth2 provider).

import * as pulumi from "@pulumi/pulumi";
import * as k8s from "@pulumi/kubernetes";
import * as fs from "fs";
import * as path from "path";
import { PLATFORM_NAMESPACE, HELM_VERSIONS, InfraOutputs, PlatformStackConfig } from "./mgmt-control-plane-services";

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
