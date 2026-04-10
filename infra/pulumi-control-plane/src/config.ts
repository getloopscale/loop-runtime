// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as pulumi from "@pulumi/pulumi";

/**
 * Control-plane configuration — application service secrets.
 * Infrastructure and platform configs are consumed via StackReference.
 */
export interface ControlPlaneConfig {
  readonly environment: string;
  readonly domain: string;

  // Database role separation
  readonly loopzeroApiDbPassword?: pulumi.Output<string>;

  // Stripe Billing
  readonly stripeApiKey?: pulumi.Output<string>;
  readonly stripeWebhookSecret?: pulumi.Output<string>;
  readonly stripePriceProId?: string;
  readonly stripePriceTeamId?: string;

  // Session/auth
  readonly sessionTokenSecret?: pulumi.Output<string>;
  readonly serviceAuthSecret?: pulumi.Output<string>;

  // Email
  readonly resendApiKey?: pulumi.Output<string>;

  // OAuth — GitHub
  readonly githubOAuthClientId?: pulumi.Output<string>;
  readonly githubOAuthClientSecret?: pulumi.Output<string>;

  // OAuth — Google
  readonly googleOAuthClientId?: pulumi.Output<string>;
  readonly googleOAuthClientSecret?: pulumi.Output<string>;

  // Cloudflare (for API CF credentials secret)
  readonly cfApiToken?: pulumi.Output<string>;
  readonly cfAccountId?: string;

  // Spaces credentials (for API S3 uploader)
  readonly spacesAccessKeyId: pulumi.Output<string>;
  readonly spacesSecretAccessKey: pulumi.Output<string>;
}

export function loadControlPlaneConfig(): ControlPlaneConfig {
  const config = new pulumi.Config();

  return {
    environment: config.require("environment"),
    domain: config.require("domain"),

    loopzeroApiDbPassword: config.getSecret("loopzeroApiDbPassword") ?? undefined,

    stripeApiKey: process.env.LOOPZERO_STRIPE_API_KEY
      ? pulumi.output(process.env.LOOPZERO_STRIPE_API_KEY)
      : config.getSecret("stripeApiKey") ?? undefined,
    stripeWebhookSecret: process.env.LOOPZERO_STRIPE_WEBHOOK_SECRET
      ? pulumi.output(process.env.LOOPZERO_STRIPE_WEBHOOK_SECRET)
      : config.getSecret("stripeWebhookSecret") ?? undefined,
    stripePriceProId: config.get("stripePriceProId") ?? undefined,
    stripePriceTeamId: config.get("stripePriceTeamId") ?? undefined,

    sessionTokenSecret: process.env.LOOPZERO_SESSION_TOKEN_SECRET
      ? pulumi.output(process.env.LOOPZERO_SESSION_TOKEN_SECRET)
      : config.getSecret("sessionTokenSecret") ?? undefined,
    serviceAuthSecret: process.env.LOOPZERO_SERVICE_AUTH_SECRET
      ? pulumi.output(process.env.LOOPZERO_SERVICE_AUTH_SECRET)
      : config.getSecret("serviceAuthSecret") ?? undefined,

    resendApiKey: process.env.LOOPZERO_RESEND_API_KEY
      ? pulumi.output(process.env.LOOPZERO_RESEND_API_KEY)
      : config.getSecret("resendApiKey") ?? undefined,

    githubOAuthClientId: process.env.LOOPZERO_GITHUB_CLIENT_ID
      ? pulumi.output(process.env.LOOPZERO_GITHUB_CLIENT_ID)
      : config.getSecret("githubOAuthClientId") ?? undefined,
    githubOAuthClientSecret: process.env.LOOPZERO_GITHUB_CLIENT_SECRET
      ? pulumi.output(process.env.LOOPZERO_GITHUB_CLIENT_SECRET)
      : config.getSecret("githubOAuthClientSecret") ?? undefined,

    googleOAuthClientId: process.env.LOOPZERO_GOOGLE_CLIENT_ID
      ? pulumi.output(process.env.LOOPZERO_GOOGLE_CLIENT_ID)
      : config.getSecret("googleOAuthClientId") ?? undefined,
    googleOAuthClientSecret: process.env.LOOPZERO_GOOGLE_CLIENT_SECRET
      ? pulumi.output(process.env.LOOPZERO_GOOGLE_CLIENT_SECRET)
      : config.getSecret("googleOAuthClientSecret") ?? undefined,

    cfApiToken: config.getSecret("cfApiToken") ?? undefined,
    cfAccountId: config.get("cfAccountId") ?? undefined,

    spacesAccessKeyId: config.requireSecret("spacesAccessKeyId"),
    spacesSecretAccessKey: config.requireSecret("spacesSecretAccessKey"),
  };
}
