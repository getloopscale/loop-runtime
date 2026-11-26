// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

/**
 * Cloudflare observability — log export and analytics.
 *
 * NOT WIRED (measured 2026-09-07): `createLogpushJobs` is exported and its
 * only occurrence in the pulumi source trees is its own declaration. Nothing
 * pushes anything today; the paragraph below is the intended design.
 *
 * Once wired, pushes CF edge logs (HTTP requests, firewall events, access
 * logs) to DO Spaces for analysis via Loki or direct query.
 */

import * as cloudflare from "@pulumi/cloudflare";

export interface CfObservabilityConfig {
  /** Cloudflare zone ID */
  readonly zoneId: string;
  /** Environment name */
  readonly environment: string;
  /** S3-compatible destination for logs (e.g., DO Spaces bucket) */
  readonly logsBucketEndpoint: string;
  /** Spaces access key */
  readonly spacesAccessKeyId: string;
  /** Spaces secret key */
  readonly spacesSecretAccessKey: string;
}

/**
 * Push CF HTTP request logs to S3-compatible storage.
 * Enables analysis of: latency, cache hit rate, WAF blocks, bot traffic.
 */
export function createLogpushJobs(config: CfObservabilityConfig): void {
  // HTTP request logs — the most valuable dataset for platform operators
  new cloudflare.LogpushJob(`logpush-http-${config.environment}`, {
    zoneId: config.zoneId,
    name: `loopruntime-${config.environment}-http-logs`,
    enabled: true,
    dataset: "http_requests",
    destinationConf: `s3://${config.logsBucketEndpoint}/cf-logs/http?region=us-east-1&access-key-id=${config.spacesAccessKeyId}&secret-access-key=${config.spacesSecretAccessKey}&endpoint=https://sfo3.digitaloceanspaces.com`,
    outputOptions: {
      fieldNames: [
        "ClientIP",
        "ClientRequestHost",
        "ClientRequestMethod",
        "ClientRequestURI",
        "EdgeResponseStatus",
        "EdgeStartTimestamp",
        "EdgeEndTimestamp",
        "OriginResponseStatus",
        "CacheCacheStatus",
        "WAFAction",
        "WAFRuleID",
        "BotScore",
        "ClientCountry",
      ],
      timestampFormat: "rfc3339",
    },
  });

  // Firewall events — WAF blocks, rate limit triggers
  new cloudflare.LogpushJob(`logpush-firewall-${config.environment}`, {
    zoneId: config.zoneId,
    name: `loopruntime-${config.environment}-firewall-logs`,
    enabled: true,
    dataset: "firewall_events",
    destinationConf: `s3://${config.logsBucketEndpoint}/cf-logs/firewall?region=us-east-1&access-key-id=${config.spacesAccessKeyId}&secret-access-key=${config.spacesSecretAccessKey}&endpoint=https://sfo3.digitaloceanspaces.com`,
    outputOptions: {
      timestampFormat: "rfc3339",
    },
  });
}
