// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as pulumi from "@pulumi/pulumi";
import { CloudProvider, Environment, NodeClass } from "../core/types";

export interface PlatformStackConfig {
  readonly project: string;
  readonly environment: Environment;
  readonly provider: CloudProvider;
  readonly region: string;
  readonly domain: string;
  /** Root DNS zone managed by the provider (e.g. loopzero.example). Defaults to domain. */
  readonly dnsZone: string;
  readonly infraVersion: string;

  // Management cluster — runs the platform (ArgoCD, API, Traefik, cert-manager)
  readonly mgmtK8sVersion?: string;
  readonly mgmtNodeClass: NodeClass;
  readonly mgmtMinNodes: number;
  readonly mgmtMaxNodes?: number;

  // Runtime cluster — runs user applications only
  readonly runtimeK8sVersion?: string;
  readonly runtimeNodeClass: NodeClass;
  readonly runtimeMinNodes: number;
  readonly runtimeMaxNodes?: number;

  // Build cluster — runs Argo Workflows + BuildKit (isolated compute for builds)
  readonly buildK8sVersion?: string;
  readonly buildNodeClass: NodeClass;
  readonly buildMinNodes: number;
  readonly buildMaxNodes?: number;

  // Database — managed PostgreSQL for platform state
  readonly dbNodeClass: NodeClass;
  readonly dbVersion: string;
  readonly dbNodeCount: number;

  // SSH keys for VM access (optional — omit for dev if keys not set up yet)
  readonly sshKeyIds?: string[];

  // Secrets — stored encrypted in Pulumi state via `pulumi config set --secret`
  readonly spacesAccessKeyId: pulumi.Output<string>;
  readonly spacesSecretAccessKey: pulumi.Output<string>;
  readonly digitaloceanToken: pulumi.Output<string>;

  // Cloudflare — static frontend delivery (Sprint 10)
  // Optional: when set, enables CF Worker + KV routing for static frontends.
  // When absent, static apps fall back to nginx-s3-gateway in cluster.
  readonly cfApiToken?: pulumi.Output<string>;
  readonly cfAccountId?: string;
  readonly cfKvNamespaceId?: string;
  /** Cloudflare Zone ID for the DNS zone (dnsZone). Required when NS is Cloudflare. */
  readonly cfZoneId?: string;

  // Service passwords — override defaults for production.
  // When not set, falls back to dev defaults from Helm values (only acceptable in dev).
  // Set via: pulumi config set --secret giteaAdminPassword <password>
  readonly giteaAdminPassword?: pulumi.Output<string>;
  readonly minioRootPassword?: pulumi.Output<string>;

}

/**
 * Loads stack configuration from Pulumi config.
 * All required values fail-fast if missing — no silent defaults.
 */
export function loadStackConfig(): PlatformStackConfig {
  const config = new pulumi.Config();

  return {
    project: pulumi.getProject(),
    environment: config.require("environment") as Environment,
    provider: config.require("provider") as CloudProvider,
    region: config.require("region"),
    domain: config.require("domain"),
    dnsZone: config.get("dnsZone") ?? config.require("domain"),
    infraVersion: config.get("infraVersion") ?? "v1",

    // Management cluster
    mgmtK8sVersion: config.get("mgmtK8sVersion") ?? undefined,
    mgmtNodeClass: config.require("mgmtNodeClass") as NodeClass,
    mgmtMinNodes: config.requireNumber("mgmtMinNodes"),
    mgmtMaxNodes: config.getNumber("mgmtMaxNodes") ?? undefined,

    // Runtime cluster
    runtimeK8sVersion: config.get("runtimeK8sVersion") ?? undefined,
    runtimeNodeClass: config.require("runtimeNodeClass") as NodeClass,
    runtimeMinNodes: config.requireNumber("runtimeMinNodes"),
    runtimeMaxNodes: config.getNumber("runtimeMaxNodes") ?? undefined,

    // Build cluster
    buildK8sVersion: config.get("buildK8sVersion") ?? undefined,
    buildNodeClass: (config.get("buildNodeClass") ?? "small") as NodeClass,
    buildMinNodes: config.getNumber("buildMinNodes") ?? 2,
    buildMaxNodes: config.getNumber("buildMaxNodes") ?? undefined,

    // Database
    dbNodeClass: (config.get("dbNodeClass") ?? "small") as NodeClass,
    dbVersion: config.get("dbVersion") ?? "16",
    dbNodeCount: config.getNumber("dbNodeCount") ?? 1,

    // SSH keys
    sshKeyIds: config.getObject<string[]>("sshKeyIds") ?? undefined,

    // Secrets — must be set via: pulumi config set --secret <key> <value>
    spacesAccessKeyId: config.requireSecret("spacesAccessKeyId"),
    spacesSecretAccessKey: config.requireSecret("spacesSecretAccessKey"),
    digitaloceanToken: config.requireSecret("digitaloceanToken"),

    // Cloudflare — optional, enables static frontend delivery via CF Worker
    cfApiToken: config.getSecret("cfApiToken") ?? undefined,
    cfAccountId: config.get("cfAccountId") ?? undefined,
    cfKvNamespaceId: config.get("cfKvNamespaceId") ?? undefined,
    cfZoneId: config.get("cfZoneId") ?? undefined,

    // Service passwords — optional, override Helm defaults for production.
    // Dev uses defaults from values.yaml; prod MUST set these via --secret.
    giteaAdminPassword: config.getSecret("giteaAdminPassword") ?? undefined,
    minioRootPassword: config.getSecret("minioRootPassword") ?? undefined,
  };
}
