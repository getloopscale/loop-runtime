// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as pulumi from "@pulumi/pulumi";

/**
 * Platform stack configuration — infrastructure services.
 * Secrets that belong to control-plane (Stripe, OAuth, etc.) are NOT here.
 */
export interface PlatformConfig {
  readonly environment: string;
  readonly domain: string;
  readonly region: string;
  readonly dnsZone: string;

  // Spaces credentials (needed for Loki S3 backend, MinIO credentials)
  readonly spacesAccessKeyId: pulumi.Output<string>;
  readonly spacesSecretAccessKey: pulumi.Output<string>;

  // Service passwords
  readonly giteaAdminPassword?: pulumi.Output<string>;
  readonly minioRootPassword?: pulumi.Output<string>;

  // Cloudflare (optional)
  readonly cfApiToken?: pulumi.Output<string>;
  readonly cfAccountId?: string;
  readonly cfZoneId?: string;

  // Cross-cluster observability
  readonly lokiPushUrl?: string;
  readonly tempoStorageBucket?: string;
}

export function loadPlatformConfig(): PlatformConfig {
  const config = new pulumi.Config();

  return {
    environment: config.require("environment"),
    domain: config.require("domain"),
    region: config.require("region"),
    dnsZone: config.get("dnsZone") ?? config.require("domain"),

    spacesAccessKeyId: config.requireSecret("spacesAccessKeyId"),
    spacesSecretAccessKey: config.requireSecret("spacesSecretAccessKey"),

    giteaAdminPassword: config.getSecret("giteaAdminPassword") ?? undefined,
    minioRootPassword: config.getSecret("minioRootPassword") ?? undefined,

    cfApiToken: config.getSecret("cfApiToken") ?? undefined,
    cfAccountId: config.get("cfAccountId") ?? undefined,
    cfZoneId: config.get("cfZoneId") ?? undefined,

    lokiPushUrl: config.get("lokiPushUrl") ?? undefined,
    tempoStorageBucket: config.get("tempoStorageBucket") ?? undefined,
  };
}
