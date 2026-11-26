// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as aws from "@pulumi/aws";
import * as pulumi from "@pulumi/pulumi";

/**
 * AWS KMS + IAM resources for OpenBao auto-unseal (F1.1 of plan
 * 2026-06-02-openbao-adoption v1.2).
 *
 * ADR: decisions/2026-06-02-openbao-adoption.md (Q3 = AWS KMS dedicated
 * LoopRuntime account).
 * MEET-ASYNC: meetings/MEET-ASYNC-AMENDMENT-2026-06-02-001-openbao-f1-execution.md
 * (Q2-F1 = long-lived access key + us-east-1).
 *
 * Resources created:
 *   1. aws.kms.Key — symmetric AES-256-GCM key for OpenBao Raft seal.
 *      EC-02 mandatory: protect:true (Pulumi-level destroy block) +
 *      deletionWindowInDays:30 (max allowed) + enableKeyRotation:true.
 *      Accidental destroy of this key permanently invalidates all DO
 *      Spaces snapshots (which are themselves encrypted by this seal
 *      key) — catastrophic data loss path.
 *   2. aws.iam.User "openbao-unseal" — single-purpose, persistent IAM
 *      user that OpenBao pods authenticate as for KMS calls.
 *   3. aws.iam.UserPolicy — inline policy granting ONLY the 3 KMS
 *      actions required for Vault/OpenBao AWS auto-unseal:
 *      kms:Encrypt + kms:Decrypt + kms:DescribeKey (per OpenBao/Vault
 *      docs; DescribeKey is sanity-check, Decrypt is the steady-state
 *      operation, Encrypt is periodic re-wrap of seal data).
 *   4. aws.iam.AccessKey — programmatic creds the OpenBao pod env
 *      consumes (AWS_ACCESS_KEY_ID + AWS_SECRET_ACCESS_KEY via Helmfile
 *      L2 wiring).
 *
 * Opt-in via `pulumi config set openbaoEnabled true`. Default false
 * keeps the stack inert until the operator is ready to bootstrap F1.1.
 * Pattern mirrors components/service-auth-ed25519.ts (Q5 ADR pre-existing).
 *
 * Outputs (exported in index.ts):
 *   - awsKmsKeyArn          (string, public)
 *   - awsKmsKeyId           (string, public)
 *   - awsKmsKeyAlias        (string, public, default "alias/loop-runtime-openbao-{stack}")
 *   - awsUnsealUserName     (string, public)
 *   - awsUnsealAccessKeyId  (secret)
 *   - awsUnsealSecretKey    (secret)
 *
 * The bootstrap user "openbao-pulumi-bootstrap" (with permissions to
 * CREATE these resources) is operator-time, NOT Pulumi-managed —
 * created manually in console OR via the operator's openbao-bootstrap-iam-setup.sh
 * per F1 grill Q2-F1 decision. After this component runs successfully,
 * the bootstrap user is revoked/degraded per the MEET-ASYNC record.
 */

/** Inputs to the aws-kms-openbao component. */
export interface AwsKmsOpenbaoArgs {
  /** Stack name for output namespacing + tag value (dev/prod/etc). */
  readonly stackName: string;

  /**
   * AWS region for the KMS key. Defaults to us-east-1 per Q2-F1
   * (proximity to the NYC3 DO Spaces backup destination; latency irrelevant
   * for boot-only KMS calls).
   */
  readonly region?: string;

  /**
   * Human-readable description on the KMS key. Defaults to the
   * "DO NOT DELETE" warning that operators see in AWS Console.
   */
  readonly description?: string;

  /**
   * KMS key alias suffix. Final alias is "alias/loop-runtime-openbao-{suffix}".
   * Defaults to stackName.
   */
  readonly aliasSuffix?: string;
}

/** Outputs from the aws-kms-openbao component. */
export interface AwsKmsOpenbaoOutputs {
  readonly kmsKeyArn: pulumi.Output<string>;
  readonly kmsKeyId: pulumi.Output<string>;
  readonly kmsKeyAlias: pulumi.Output<string>;
  readonly unsealUserName: pulumi.Output<string>;
  readonly unsealUserArn: pulumi.Output<string>;
  readonly unsealAccessKeyId: pulumi.Output<string>;
  readonly unsealSecretKey: pulumi.Output<string>;
}

/** Pure render helpers (testable, no Pulumi side-effects). */

/** Tag map applied to KMS key + IAM user for audit/ownership. */
export function buildResourceTags(stackName: string): Record<string, string> {
  return {
    Project: "loop-runtime",
    Component: "openbao",
    Stack: stackName,
    Purpose: "openbao-auto-unseal",
    Plan: "2026-06-02-openbao-adoption-F1.1",
    ManagedBy: "pulumi",
  };
}

/**
 * Renders the IAM inline policy granting OpenBao unseal permissions on
 * the specific KMS key (no wildcard). Returns canonical JSON string.
 *
 * Per OpenBao/Vault AWS auto-unseal docs: kms:Encrypt (re-wrap), Decrypt
 * (steady-state), DescribeKey (sanity check). Single statement, single
 * resource (the specific KMS key ARN).
 */
export function renderUnsealPolicyDocument(kmsKeyArn: string): string {
  if (!kmsKeyArn || typeof kmsKeyArn !== "string") {
    throw new Error("renderUnsealPolicyDocument: kmsKeyArn must be non-empty string");
  }
  const doc = {
    Version: "2012-10-17",
    Statement: [
      {
        Sid: "OpenBaoAutoUnseal",
        Effect: "Allow",
        Action: ["kms:Encrypt", "kms:Decrypt", "kms:DescribeKey"],
        Resource: kmsKeyArn,
      },
    ],
  };
  return JSON.stringify(doc, null, 2);
}

/**
 * Validates AwsKmsOpenbaoArgs at component-create time. Throws on
 * invalid config (region not whitelisted, stackName empty, etc).
 * Pure function — testable without Pulumi runtime.
 */
export function validateArgs(args: AwsKmsOpenbaoArgs): void {
  if (!args.stackName || args.stackName.trim().length === 0) {
    throw new Error("AwsKmsOpenbaoArgs.stackName required (non-empty)");
  }
  const region = args.region ?? "us-east-1";
  const allowedRegions = [
    "us-east-1",
    "us-east-2",
    "us-west-1",
    "us-west-2",
    "eu-west-1",
    "eu-central-1",
    "sa-east-1",
  ];
  if (!allowedRegions.includes(region)) {
    throw new Error(
      `AwsKmsOpenbaoArgs.region '${region}' not in allowlist: ${allowedRegions.join(", ")}. ` +
        `Per Q2-F1 decision (us-east-1 default for NYC3 DO Spaces proximity).`,
    );
  }
}

/**
 * Creates the AWS KMS key + IAM user + access key for OpenBao auto-unseal.
 *
 * @param args component configuration
 * @returns AwsKmsOpenbaoOutputs (Pulumi outputs ready to export)
 */
export function createAwsKmsOpenbao(args: AwsKmsOpenbaoArgs): AwsKmsOpenbaoOutputs {
  validateArgs(args);

  const region = args.region ?? "us-east-1";
  const aliasSuffix = args.aliasSuffix ?? args.stackName;
  const description =
    args.description ??
    "OpenBao Raft seal key — DO NOT DELETE. Loss permanently invalidates DO Spaces snapshots.";
  const tags = buildResourceTags(args.stackName);

  // The provider must be explicitly regional — the default loop-runtime-infra
  // Pulumi run uses DigitalOcean provider, so AWS resources need their
  // own provider instance scoped to the configured region.
  //
  // Credentials precedence (per F1.7 Helmfile env-var wiring):
  //   1. Pulumi config `aws:accessKey`/`aws:secretKey` (set per-stack
  //      via `pulumi config set --secret aws:accessKey ...`)
  //   2. AWS_PROFILE env if (1) absent
  //   3. AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY env if (2) absent
  //
  // Pulumi state backend (DO Spaces) consumes AWS_ACCESS_KEY_ID env
  // vars too — to avoid clash, we override the AWS Provider explicitly
  // with the Pulumi config values (which point to the openbao-pulumi-
  // bootstrap IAM user per MEET-ASYNC Q2-F1).
  const awsCfg = new pulumi.Config("aws");
  const providerAccessKey = awsCfg.getSecret("accessKey");
  const providerSecretKey = awsCfg.getSecret("secretKey");

  const awsProvider = new aws.Provider(
    `aws-openbao-${args.stackName}`,
    {
      region: region as aws.Region,
      ...(providerAccessKey && providerSecretKey
        ? { accessKey: providerAccessKey, secretKey: providerSecretKey }
        : {}),
    },
  );

  // 1. KMS key (EC-02: protect:true + 30d deletion window + rotation)
  const kmsKey = new aws.kms.Key(
    "openbao-unseal-key",
    {
      description,
      keyUsage: "ENCRYPT_DECRYPT",
      customerMasterKeySpec: "SYMMETRIC_DEFAULT",
      enableKeyRotation: true,
      deletionWindowInDays: 30,
      tags,
    },
    { provider: awsProvider, protect: true },
  );

  // KMS alias (operator-friendly reference; rotates by re-pointing alias)
  const kmsAlias = new aws.kms.Alias(
    "openbao-unseal-alias",
    {
      name: `alias/loop-runtime-openbao-${aliasSuffix}`,
      targetKeyId: kmsKey.keyId,
    },
    { provider: awsProvider },
  );

  // 2. IAM user (single-purpose, persistent).
  // Per-stack naming: IAM users are global within AWS account, so
  // per-env isolation requires distinct names. F1.1 originally used
  // "openbao-unseal" globally; refactored to "openbao-unseal-<stack>"
  // to allow dev+staging+prod to coexist in same AWS account.
  const unsealUser = new aws.iam.User(
    "openbao-unseal-user",
    {
      name: `openbao-unseal-${args.stackName}`,
      path: "/loop-runtime/",
      tags,
    },
    { provider: awsProvider },
  );

  // 3. Inline policy (3 actions, scoped to the specific key ARN)
  const _userPolicy = new aws.iam.UserPolicy(
    "openbao-unseal-policy",
    {
      name: `openbao-unseal-policy-${args.stackName}`,
      user: unsealUser.name,
      policy: kmsKey.arn.apply((arn) => renderUnsealPolicyDocument(arn)),
    },
    { provider: awsProvider },
  );

  // 4. Access key (Pulumi-managed; rotated via `pulumi up` after marking
  //    current key inactive in console + creating new — operator runbook)
  const accessKey = new aws.iam.AccessKey(
    "openbao-unseal-access-key",
    {
      user: unsealUser.name,
    },
    { provider: awsProvider },
  );

  return {
    kmsKeyArn: kmsKey.arn,
    kmsKeyId: kmsKey.keyId,
    kmsKeyAlias: kmsAlias.name,
    unsealUserName: unsealUser.name,
    unsealUserArn: unsealUser.arn,
    unsealAccessKeyId: accessKey.id,
    unsealSecretKey: accessKey.secret,
  };
}
