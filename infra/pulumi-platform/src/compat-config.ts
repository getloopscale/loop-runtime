// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as pulumi from "@pulumi/pulumi";
import { PlatformConfig } from "./config";
import type { InfraStackOutputs } from "./contracts";

/**
 * Compatibility adapter — constructs an object that satisfies the
 * PlatformStackConfig interface used by all bootstrap functions.
 *
 * This avoids rewriting every function signature during migration.
 * The adapter merges PlatformConfig (local) + InfraStackOutputs (StackReference)
 * into the shape that bootstrap functions expect.
 *
 * Fields used ONLY by control-plane are set to dummy values — those functions
 * will be removed from this project in the control-plane extraction.
 */
export function buildCompatConfig(
  platform: PlatformConfig,
  infraRef: Record<string, pulumi.Output<any>>,
): Record<string, any> {
  return {
    // From PlatformConfig (local)
    environment: platform.environment,
    domain: platform.domain,
    region: platform.region,
    dnsZone: platform.dnsZone,
    project: "loopzero-infra", // used by naming/tags — matches infra project name

    spacesAccessKeyId: platform.spacesAccessKeyId,
    spacesSecretAccessKey: platform.spacesSecretAccessKey,

    giteaAdminPassword: platform.giteaAdminPassword,
    minioRootPassword: platform.minioRootPassword,

    cfApiToken: platform.cfApiToken,
    cfAccountId: platform.cfAccountId,
    cfZoneId: platform.cfZoneId,

    lokiPushUrl: platform.lokiPushUrl,
    tempoStorageBucket: platform.tempoStorageBucket,

    // From InfraStackOutputs (StackReference) — fields used by bootstrap
    infraVersion: infraRef["infraVersion"] ?? pulumi.output("v3"),

    // Control-plane fields — dummy values (functions using these will be
    // extracted to pulumi-control-plane and removed from this project).
    loopzeroApiDbPassword: undefined,
    stripeApiKey: undefined,
    stripeWebhookSecret: undefined,
    stripePriceProId: undefined,
    stripePriceTeamId: undefined,
    sessionTokenSecret: undefined,
    serviceAuthSecret: undefined,
    resendApiKey: undefined,
    githubOAuthClientId: undefined,
    githubOAuthClientSecret: undefined,
    googleOAuthClientId: undefined,
    googleOAuthClientSecret: undefined,
    digitaloceanToken: pulumi.output("from-stack-ref"),
  };
}
