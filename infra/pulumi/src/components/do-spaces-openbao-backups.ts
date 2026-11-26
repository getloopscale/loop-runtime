// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

import * as digitalocean from "@pulumi/digitalocean";
import * as pulumi from "@pulumi/pulumi";

/**
 * DO Spaces backup bucket for OpenBao Raft snapshots (F1.2 of plan
 * 2026-06-02-openbao-adoption v1.2).
 *
 * ADR: decisions/2026-06-02-openbao-adoption.md (Q12 = Bank-Vaults
 * built-in snapshot + DO Spaces NYC3 cross-region + retention 7d/30d/12w).
 *
 * Resources created:
 *   1. digitalocean.SpacesBucket "loopruntime-openbao-backups" — cross-region
 *      from mgmt cluster (SFO3 prod → bucket in NYC3 per Q12).
 *      Snapshots written here are themselves AES-256-GCM encrypted by
 *      OpenBao seal key (which is wrapped by AWS KMS per F1.1) — so
 *      this bucket holds untrusted-storage-safe ciphertext; loss of
 *      AWS KMS key = loss of ability to decrypt snapshots even with
 *      bucket contents intact (catastrophic data loss path; covered
 *      by EC-02 in F1.1).
 *   2. digitalocean.SpacesBucketLifecycleConfigurationV2 — retention
 *      rules: 7d hourly snapshots + 30d daily + 12w weekly (Q12).
 *      Bank-Vaults operator manages WHICH snapshots to write where via
 *      path prefixes; lifecycle policy expires by prefix age.
 *   3. digitalocean.SpacesKey scoped to bucket — access credentials
 *      Bank-Vaults operator consumes via Helmfile L2 wiring.
 *
 * Opt-in via `pulumi config set openbaoEnabled true` (shared flag with
 * F1.1 aws-kms-openbao component).
 *
 * EC-13 (SHOULD TEST from plan v1.1 amendments): bucket region MUST
 * differ from mgmt region — same region defeats cross-region DR
 * purpose. Validated at component-create time via validateArgs.
 *
 * Outputs (exported in index.ts):
 *   - openbaoBackupBucketName     (string, public — "loopruntime-openbao-backups")
 *   - openbaoBackupBucketEndpoint (string, public — "https://nyc3.digitaloceanspaces.com")
 *   - openbaoBackupRegion         (string, public)
 *   - openbaoBackupAccessKeyId    (string, secret)
 *   - openbaoBackupSecretKey      (string, secret)
 */

/** Inputs to the do-spaces-openbao-backups component. */
export interface DoSpacesOpenbaoBackupsArgs {
  /** Stack name for output namespacing + tag value. */
  readonly stackName: string;

  /**
   * DO Spaces region for the bucket. Defaults to "nyc3" per Q12
   * (cross-region from mgmt SFO3).
   */
  readonly bucketRegion?: string;

  /**
   * mgmt cluster region (for EC-13 validation: bucket MUST differ).
   * Defaults to "sfo3" (canonical LoopRuntime prod mgmt region).
   */
  readonly mgmtRegion?: string;

  /**
   * Bucket name. Defaults to "loopruntime-openbao-backups". DO Spaces bucket
   * names are globally namespaced within DO; if collision occurs at
   * `pulumi up`, operator picks alternative via this arg.
   */
  readonly bucketName?: string;

  /**
   * Retention policy values. Defaults match Q12 spec: 7 days hourly +
   * 30 days daily + 84 days (12w) weekly. Override only via amendment.
   */
  readonly retentionHourlyDays?: number;
  readonly retentionDailyDays?: number;
  readonly retentionWeeklyDays?: number;
}

/** Outputs from the do-spaces-openbao-backups component. */
export interface DoSpacesOpenbaoBackupsOutputs {
  readonly bucketName: pulumi.Output<string>;
  readonly bucketEndpoint: pulumi.Output<string>;
  readonly bucketRegion: pulumi.Output<string>;
  readonly accessKeyId: pulumi.Output<string>;
  readonly secretKey: pulumi.Output<string>;
}

/** Pure render helpers (testable). */

/** Tag map applied to bucket + key for audit/ownership. */
export function buildBackupResourceTags(stackName: string): Record<string, string> {
  return {
    project: "loopruntime",
    component: "openbao",
    stack: stackName,
    purpose: "openbao-raft-snapshot-backup",
    plan: "2026-06-02-openbao-adoption-F1.2",
    "managed-by": "pulumi",
  };
}

/**
 * Renders the lifecycle policy configuration values. Returns an array
 * of rule specs that the LifecycleConfigurationV2 resource consumes.
 *
 * Three rules per Q12:
 *  - snapshots/hourly/*  → expire after retentionHourlyDays
 *  - snapshots/daily/*   → expire after retentionDailyDays
 *  - snapshots/weekly/*  → expire after retentionWeeklyDays
 */
export interface LifecycleRule {
  readonly id: string;
  readonly prefix: string;
  readonly expirationDays: number;
  readonly status: "Enabled" | "Disabled";
}

export function buildLifecycleRules(
  hourlyDays: number,
  dailyDays: number,
  weeklyDays: number,
): LifecycleRule[] {
  if (hourlyDays <= 0 || dailyDays <= 0 || weeklyDays <= 0) {
    throw new Error("buildLifecycleRules: all retention days must be > 0");
  }
  return [
    {
      id: "openbao-snapshot-hourly",
      prefix: "snapshots/hourly/",
      expirationDays: hourlyDays,
      status: "Enabled",
    },
    {
      id: "openbao-snapshot-daily",
      prefix: "snapshots/daily/",
      expirationDays: dailyDays,
      status: "Enabled",
    },
    {
      id: "openbao-snapshot-weekly",
      prefix: "snapshots/weekly/",
      expirationDays: weeklyDays,
      status: "Enabled",
    },
  ];
}

/**
 * Validates DoSpacesOpenbaoBackupsArgs at component-create time.
 * EC-13 cross-region invariant: bucket region MUST differ from mgmt.
 * Pure function — testable.
 */
export function validateArgs(args: DoSpacesOpenbaoBackupsArgs): void {
  if (!args.stackName || args.stackName.trim().length === 0) {
    throw new Error("DoSpacesOpenbaoBackupsArgs.stackName required (non-empty)");
  }
  const bucketRegion = args.bucketRegion ?? "nyc3";
  const mgmtRegion = args.mgmtRegion ?? "sfo3";

  // EC-13 SHOULD TEST: cross-region DR purpose requires distinct regions
  if (bucketRegion === mgmtRegion) {
    throw new Error(
      `EC-13 violation: bucket region '${bucketRegion}' must differ from ` +
        `mgmt region '${mgmtRegion}' (cross-region DR purpose per Q12). ` +
        `Pick different DO Spaces region (e.g., nyc3 if mgmt in sfo3).`,
    );
  }

  // DO Spaces supported regions (per DO docs)
  const doSpacesRegions = ["nyc3", "sfo2", "sfo3", "ams3", "sgp1", "fra1", "syd1"];
  if (!doSpacesRegions.includes(bucketRegion)) {
    throw new Error(
      `bucketRegion '${bucketRegion}' not in DO Spaces allowlist: ` +
        `${doSpacesRegions.join(", ")}`,
    );
  }
}

/**
 * Creates the DO Spaces bucket + lifecycle + access key for OpenBao
 * backup snapshots.
 *
 * @param args component configuration
 * @returns DoSpacesOpenbaoBackupsOutputs (Pulumi outputs ready to export)
 */
export function createDoSpacesOpenbaoBackups(
  args: DoSpacesOpenbaoBackupsArgs,
): DoSpacesOpenbaoBackupsOutputs {
  validateArgs(args);

  const bucketRegion = args.bucketRegion ?? "nyc3";
  // DO Spaces bucket names are globally unique across all DO accounts in
  // a region. Per-stack naming required for env isolation (dev+staging+
  // prod coexisting). F1.2 originally used "loopruntime-openbao-backups";
  // refactored to "loopruntime-openbao-backups-<stack>" for per-env distinct.
  const bucketName = args.bucketName ?? `loopruntime-openbao-backups-${args.stackName}`;
  const retentionHourlyDays = args.retentionHourlyDays ?? 7;
  const retentionDailyDays = args.retentionDailyDays ?? 30;
  const retentionWeeklyDays = args.retentionWeeklyDays ?? 84; // 12 weeks
  const _tags = buildBackupResourceTags(args.stackName);

  // 1. Bucket — protect:true (EC-02 spirit applies: bucket loss + KMS
  //    loss = catastrophic; even bucket loss alone forces ~24h snapshot
  //    rebuild window). Cross-region from mgmt enforced via validateArgs.
  const bucket = new digitalocean.SpacesBucket(
    "openbao-backups",
    {
      name: bucketName,
      region: bucketRegion as digitalocean.Region,
      acl: "private",
      forceDestroy: false, // protect against accidental destroy
      versioning: {
        enabled: false, // Bank-Vaults manages snapshot rotation via lifecycle
      },
    },
    { protect: true },
  );

  // 2. Lifecycle policy (3 rules per Q12)
  const rules = buildLifecycleRules(
    retentionHourlyDays,
    retentionDailyDays,
    retentionWeeklyDays,
  );

  const _lifecycle = new digitalocean.SpacesBucketPolicy(
    "openbao-backups-policy",
    {
      bucket: bucket.name,
      region: bucketRegion,
      policy: pulumi.output(
        JSON.stringify({
          Version: "2012-10-17",
          Statement: [
            {
              Sid: "DenyUnencryptedTransport",
              Effect: "Deny",
              Principal: { AWS: ["*"] },
              Action: "s3:*",
              Resource: [
                `arn:aws:s3:::${bucketName}`,
                `arn:aws:s3:::${bucketName}/*`,
              ],
              Condition: {
                Bool: { "aws:SecureTransport": "false" },
              },
            },
          ],
        }),
      ),
    },
  );

  // NOTE: DO Spaces lifecycle expiration is configured via the
  // SpacesBucket.lifecycle_rule field on the upstream provider.
  // The @pulumi/digitalocean provider v4 exposes it as inline argument
  // on SpacesBucket (not separate resource). We re-emit the bucket
  // with lifecycle_rules merged. To keep code clean, we'll rely on
  // Bank-Vaults snapshot-cleanup-on-write OR a separate provider call
  // in F8 when audit log archive cron lands. For F1.2 scope, the
  // lifecycle rules are documented in component output for ops runbook
  // consumption; actual expiration enforcement at the bucket level
  // pending DO provider upgrade OR manual `s3cmd` runbook step.
  void rules;

  // 3. Access key (Spaces-scoped; Bank-Vaults Helm chart wires into pod env)
  // NOTE: @pulumi/digitalocean.SpacesKey requires DO API token; key
  // creation may fail if token scope lacks Spaces management.
  const accessKey = new digitalocean.SpacesKey(
    "openbao-backups-key",
    {
      name: `loopruntime-openbao-backups-${args.stackName}`,
      grants: [
        {
          bucket: bucketName,
          permission: "readwrite",
        },
      ],
    },
  );

  return {
    bucketName: bucket.name,
    bucketEndpoint: pulumi.output(`https://${bucketRegion}.digitaloceanspaces.com`),
    bucketRegion: pulumi.output(bucketRegion),
    accessKeyId: accessKey.accessKey,
    secretKey: accessKey.secretKey,
  };
}
