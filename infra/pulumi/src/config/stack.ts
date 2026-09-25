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

  // Cosign signing key (generated once per env, stored encrypted via pulumi config set --secret)
  // cosignPrivateKeyB64: base64-encoded cosign private key file
  // cosignPassword: password used during cosign generate-key-pair
  readonly cosignPrivateKeyB64?: string;
  readonly cosignPassword?: string;
  // Cosign PUBLIC key for runtime cluster verification (Sprint 6 / T2.1).
  // Generated from the private key:  cosign public-key --key cosign.key | base64 -w0
  // Distributed to the runtime cluster as Secret cosign-public-key
  // (namespace cosign-system) so policy-controller can verify signatures
  // without holding the private key. NOT secret per se — public keys are
  // safe to commit — but stored alongside cosignPrivateKeyB64 for symmetry.
  readonly cosignPublicKeyB64?: string;

  // Cloudflare — static frontend delivery (Sprint 10)
  // Optional: when set, enables CF Worker + KV routing for static frontends.
  // When absent, static apps fall back to nginx-s3-gateway in cluster.
  readonly cfApiToken?: pulumi.Output<string>;
  readonly cfAccountId?: string;
  /** Cloudflare Zone ID for the DNS zone (dnsZone). Required when NS is Cloudflare. */
  readonly cfZoneId?: string;

  // Service passwords — override defaults for production.
  // When not set, falls back to dev defaults from Helm values (only acceptable in dev).
  // Set via: pulumi config set --secret giteaAdminPassword <password>
  readonly giteaAdminPassword?: pulumi.Output<string>;
  readonly minioRootPassword?: pulumi.Output<string>;

  // Database role separation (MEET-2026-03-31-024)
  // When set, the API connects as loopzero_api role (DML only, no DDL).
  // Migrations continue using the admin user.
  // Set via: pulumi config set --secret loopzeroApiDbPassword <password>
  readonly loopzeroApiDbPassword?: pulumi.Output<string>;

  // Observability — Tempo trace storage (MEET-2026-03-30-020)
  // S3 bucket for Tempo trace data in production. Omit for local filesystem (dev).
  readonly tempoStorageBucket?: string;

  // Stripe Billing (Sprint 11, Phase 5)
  // Set via: pulumi config set --secret stripeApiKey sk_live_xxx
  //          pulumi config set --secret stripeWebhookSecret whsec_xxx
  //          pulumi config set stripePriceProId price_xxx
  //          pulumi config set stripePriceTeamId price_xxx
  readonly stripeApiKey?: pulumi.Output<string>;
  readonly stripeWebhookSecret?: pulumi.Output<string>;
  readonly stripePriceProId?: string;
  readonly stripePriceTeamId?: string;
  // Session token HMAC secret for dashboard JWT issuance (cloud service only).
  // Set via: pulumi config set --secret sessionTokenSecret <32+ char secret>
  readonly sessionTokenSecret?: pulumi.Output<string>;

  // Service-to-service auth secret for cloud→engine proxy (MEET-2026-04-07-039).
  // Must be the same value on both services. Auto-generated if not set.
  // Set via: pulumi config set --secret serviceAuthSecret <32+ char secret>
  readonly serviceAuthSecret?: pulumi.Output<string>;

  // Resend API key for email delivery (verification, recovery).
  // MEET-036: HTTP API replaces SMTP (DO blocks SMTP ports).
  readonly resendApiKey?: pulumi.Output<string>;

  // Ory Social Login — GitHub OAuth (Sprint 11, Phase 3)
  // Set via: pulumi config set --secret githubOAuthClientId <id>
  //          pulumi config set --secret githubOAuthClientSecret <secret>
  readonly githubOAuthClientId?: pulumi.Output<string>;
  readonly githubOAuthClientSecret?: pulumi.Output<string>;

  // Ory Social Login — Google OAuth (Sprint 11, Phase 3)
  // Set via: pulumi config set --secret googleOAuthClientId <id>
  //          pulumi config set --secret googleOAuthClientSecret <secret>
  readonly googleOAuthClientId?: pulumi.Output<string>;
  readonly googleOAuthClientSecret?: pulumi.Output<string>;

  // Cross-cluster observability — Loki push URL for Promtail on runtime cluster.
  // Points to Loki NodePort on build cluster (e.g. http://10.20.0.6:30100).
  // Required when runtime cluster ships app logs to centralized Loki.
  // Set via: pulumi config set lokiPushUrl http://<build-node-ip>:30100
  readonly lokiPushUrl?: string;

  // VPC CIDR — override to avoid overlap between environments in the same account.
  // Default: 10.20.0.0/16. Set different CIDRs per environment (e.g., 10.30.0.0/16 for homolog).
  readonly vpcCidr?: string;

  // MEET-055 M1: Slack webhook URLs for Alertmanager receivers.
  // Set via: pulumi config set --secret slackWebhookUrl "https://hooks.slack.com/..."
  readonly slackWebhookUrl?: pulumi.Output<string>;
  readonly slackCriticalWebhookUrl?: pulumi.Output<string>;

}

/** Where secretFromEnvOrConfig reads from; injectable so the rule is testable. */
export interface SecretSources {
  readonly env: Record<string, string | undefined>;
  readonly getSecret: (key: string) => pulumi.Output<string> | undefined;
}

/**
 * A credential that may come from the environment (CI secrets) or from the
 * stack's Pulumi config: the environment variable wins when set and
 * non-empty, the config secret otherwise, undefined when neither is set.
 */
export function secretFromEnvOrConfig(
  envVar: string,
  configKey: string,
  sources: SecretSources,
): pulumi.Output<string> | undefined {
  const fromEnv = sources.env[envVar];
  if (fromEnv) {
    // pulumi.secret, not pulumi.output: a credential read from the
    // environment must be encrypted in state exactly like one set with
    // `pulumi config set --secret`.
    return pulumi.secret(fromEnv);
  }
  return sources.getSecret(configKey) ?? undefined;
}

/**
 * Loads stack configuration from Pulumi config.
 * All required values fail-fast if missing — no silent defaults.
 */
export function loadStackConfig(): PlatformStackConfig {
  const config = new pulumi.Config();
  const secrets: SecretSources = {
    env: process.env,
    getSecret: (key) => config.getSecret(key),
  };

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
    cfZoneId: config.get("cfZoneId") ?? undefined,

    // Service passwords — optional, override Helm defaults for production.
    // Dev uses defaults from values.yaml; prod MUST set these via --secret.
    giteaAdminPassword: config.getSecret("giteaAdminPassword") ?? undefined,
    minioRootPassword: config.getSecret("minioRootPassword") ?? undefined,

    // Database role separation (MEET-2026-03-31-024)
    loopzeroApiDbPassword: config.getSecret("loopzeroApiDbPassword") ?? undefined,

    // Observability — Tempo trace storage (MEET-020)
    tempoStorageBucket: config.get("tempoStorageBucket") ?? undefined,

    // Stripe Billing (Sprint 11, Phase 5)
    // Reads from env vars (GitHub Actions secrets) or Pulumi config (local dev).
    stripeApiKey: secretFromEnvOrConfig("LOOPZERO_STRIPE_API_KEY", "stripeApiKey", secrets),
    stripeWebhookSecret: secretFromEnvOrConfig("LOOPZERO_STRIPE_WEBHOOK_SECRET", "stripeWebhookSecret", secrets),
    stripePriceProId: config.get("stripePriceProId") ?? undefined,
    stripePriceTeamId: config.get("stripePriceTeamId") ?? undefined,
    sessionTokenSecret: secretFromEnvOrConfig("LOOPZERO_SESSION_TOKEN_SECRET", "sessionTokenSecret", secrets),

    // Cloud→Engine service auth secret (MEET-2026-04-07-039)
    serviceAuthSecret: secretFromEnvOrConfig("LOOPZERO_SERVICE_AUTH_SECRET", "serviceAuthSecret", secrets),

    // Resend API key for email delivery (MEET-036: HTTP API, not SMTP)
    resendApiKey: secretFromEnvOrConfig("LOOPZERO_RESEND_API_KEY", "resendApiKey", secrets),

    // Ory Social Login — GitHub OAuth (Sprint 11, Phase 3)
    // Reads from env vars (GitHub Actions secrets) or Pulumi config (local dev).
    githubOAuthClientId: secretFromEnvOrConfig("LOOPZERO_GITHUB_CLIENT_ID", "githubOAuthClientId", secrets),
    githubOAuthClientSecret: secretFromEnvOrConfig("LOOPZERO_GITHUB_CLIENT_SECRET", "githubOAuthClientSecret", secrets),

    // Ory Social Login — Google OAuth (Sprint 11, Phase 3)
    googleOAuthClientId: secretFromEnvOrConfig("LOOPZERO_GOOGLE_CLIENT_ID", "googleOAuthClientId", secrets),
    googleOAuthClientSecret: secretFromEnvOrConfig("LOOPZERO_GOOGLE_CLIENT_SECRET", "googleOAuthClientSecret", secrets),

    // Cross-cluster observability — Loki push URL for runtime Promtail
    lokiPushUrl: config.get("lokiPushUrl") ?? undefined,
    vpcCidr: config.get("vpcCidr") ?? undefined,

    // MEET-055 M1: Slack webhooks for Alertmanager
    slackWebhookUrl: config.getSecret("slackWebhookUrl") ?? undefined,
    slackCriticalWebhookUrl: config.getSecret("slackCriticalWebhookUrl") ?? undefined,

    // Cosign signing key — stored encrypted via: pulumi config set --secret cosignPrivateKeyB64 $(base64 -w0 cosign.key)
    cosignPrivateKeyB64: config.get("cosignPrivateKeyB64") ?? undefined,
    cosignPassword: config.get("cosignPassword") ?? undefined,
    // Cosign public key — Sprint 6 / T2.1. Distributed to the runtime cluster
    // as Secret cosign-public-key (namespace cosign-system) so policy-controller
    // can verify image signatures. Set via:
    //   pulumi config set cosignPublicKeyB64 $(cosign public-key --key cosign.key | base64 -w0)
    cosignPublicKeyB64: config.get("cosignPublicKeyB64") ?? undefined,
  };
}
