// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// velero-backup-dr-plan T0.1 — invariants for the Velero backups bucket.
//
// Source-text checks (consistent with the rest of the pulumi __tests__ suite,
// e.g. heal-script-H12.test.ts) — instantiating the Pulumi program requires
// real cloud credentials and a stack context, so we assert against the
// authored TypeScript instead.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const REPO_ROOT = resolve(__dirname, "..", "..", "..", "..");
const INFRA_TS = resolve(REPO_ROOT, "infra", "pulumi", "src", "components", "infra.ts");
const STORAGE_CONTRACT_TS = resolve(REPO_ROOT, "infra", "pulumi", "src", "contracts", "storage.ts");
const DO_STORAGE_TS = resolve(
  REPO_ROOT,
  "infra",
  "pulumi",
  "src",
  "providers",
  "digitalocean",
  "storage.ts",
);

describe("velero-backup-dr-plan T0.1 — bucket invariants", () => {
  const infraSrc = readFileSync(INFRA_TS, "utf-8");
  const contractSrc = readFileSync(STORAGE_CONTRACT_TS, "utf-8");
  const doStorageSrc = readFileSync(DO_STORAGE_TS, "utf-8");

  it("ObjectStorageSpec exposes optional protect + forceDestroy fields", () => {
    // The plan needs protect:isProd and forceDestroy:!isProd. Both flags
    // must be optional on the spec so existing callers don't break.
    expect(contractSrc).toMatch(/readonly\s+protect\?:\s*boolean/);
    expect(contractSrc).toMatch(/readonly\s+forceDestroy\?:\s*boolean/);
  });

  it("DO storage provider passes protect through to ResourceOptions", () => {
    // SpacesBucket must accept { protect: true } as the third argument
    // when spec.protect is set, otherwise pulumi destroy can wipe backups.
    expect(doStorageSrc).toMatch(/spec\.protect\s*\?\s*\{\s*protect:\s*true\s*\}/);
  });

  it("DO storage provider honors spec.forceDestroy with sensible default", () => {
    // forceDestroy default true (back-compat with the 5 existing buckets);
    // backup bucket overrides to false in prod.
    expect(doStorageSrc).toMatch(/forceDestroy:\s*spec\.forceDestroy\s*\?\?\s*true/);
  });

  it("infra.ts creates a velero-backups bucket via factory.createObjectStorage", () => {
    // The bucket creation must call createObjectStorage with suffix
    // "velero-backups" so naming.bucketName produces the canonical name.
    expect(infraSrc).toMatch(
      /factory\.createObjectStorage\(\{[\s\S]{0,400}suffix:\s*["']velero-backups["']/,
    );
  });

  it("velero bucket sets versioning=true (protect against accidental delete)", () => {
    // Block scoped to the velero bucket — versioning MUST be true.
    const block = infraSrc.match(
      /veleroBackupsBucket\s*=\s*factory\.createObjectStorage\(\{[\s\S]{0,500}\}\);/,
    )?.[0] ?? "";
    expect(block).not.toEqual("");
    expect(block).toMatch(/versioning:\s*true/);
  });

  it("velero bucket protect is gated by isProd", () => {
    const block = infraSrc.match(
      /veleroBackupsBucket\s*=\s*factory\.createObjectStorage\(\{[\s\S]{0,500}\}\);/,
    )?.[0] ?? "";
    expect(block).toMatch(/protect:\s*isProd/);
  });

  it("velero bucket forceDestroy disabled in prod", () => {
    const block = infraSrc.match(
      /veleroBackupsBucket\s*=\s*factory\.createObjectStorage\(\{[\s\S]{0,500}\}\);/,
    )?.[0] ?? "";
    expect(block).toMatch(/forceDestroy:\s*!isProd/);
  });

  it("InfraOutputs exposes veleroBackupsBucketName", () => {
    // Downstream Helmfile env values consume this output to template the
    // BackupStorageLocation bucket name on Velero.
    expect(infraSrc).toMatch(/readonly\s+veleroBackupsBucketName:\s*pulumi\.Output<string>/);
  });

  it("createInfra return value includes veleroBackupsBucketName", () => {
    expect(infraSrc).toMatch(
      /veleroBackupsBucketName:\s*veleroBackupsBucket\.bucketName/,
    );
  });
});
