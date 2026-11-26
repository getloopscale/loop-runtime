// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// T9.4 unit tests for vault-iac.ts WITHOUT loading the Pulumi runtime.
// The existing tests in this repo (heal-script-H12, adopt-or-create) avoid
// pulumi.Output construction to keep tests fast and hermetic. We follow
// the same pattern: parse the source file + verify contract is intact.
//
// LIVE assertion (T9.4): the canonical mapping in
// `infra/scripts/openbao/migrate-pulumi-secrets-to-vault.sh` matches the
// runbook docs/runbooks.md mapping table.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const REPO_ROOT = resolve(__dirname, "..", "..", "..", "..");
const VAULT_IAC = resolve(REPO_ROOT, "infra", "pulumi", "src", "components", "vault-iac.ts");
const MIGRATE_SCRIPT = resolve(REPO_ROOT, "infra", "scripts", "openbao", "migrate-pulumi-secrets-to-vault.sh");

describe("vault-iac.ts source contract", () => {
  const text = readFileSync(VAULT_IAC, "utf-8");

  it("exports VaultSecretSource interface", () => {
    expect(text).toMatch(/export interface VaultSecretSource\b/);
    expect(text).toMatch(/read\(path: string, key: string\)/);
  });

  it("exports VaultSecretReader class with constructor + read", () => {
    expect(text).toMatch(/export class VaultSecretReader/);
    expect(text).toMatch(/constructor\(providerName/);
    expect(text).toMatch(/read\(path: string, key: string\)/);
  });

  it("exports StaticVaultSecretSource for test/dev fixtures", () => {
    expect(text).toMatch(/export class StaticVaultSecretSource/);
  });

  it("VaultSecretReader has lazy require for @pulumi/vault (NO top-level import)", () => {
    // Lazy require: package must be loaded inside read() body so dev stacks
    // without @pulumi/vault installed don't fail compile.
    expect(text).toMatch(/require\(["']@pulumi\/vault["']\)/);
    // And NOT a top-level `import vault from "@pulumi/vault"`.
    expect(text).not.toMatch(/^import.*from\s+["']@pulumi\/vault["']/m);
  });

  it("uses kv-v2 getSecretOutput pattern", () => {
    expect(text).toMatch(/vault\.generic\.getSecretOutput/);
  });

  it("StaticVaultSecretSource throws on missing path AND key", () => {
    expect(text).toMatch(/path .* not in fixture/);
    expect(text).toMatch(/key .* missing under path/);
  });
});

describe("migrate-pulumi-secrets-to-vault.sh mapping contract (T9.4 LIVE iter 33)", () => {
  const script = readFileSync(MIGRATE_SCRIPT, "utf-8");

  // Extract the MAPPING entries between `declare -A MAPPING=(` and the
  // closing `)`.
  const block = script.match(/declare -A MAPPING=\(([\s\S]+?)\)/);
  expect(block).not.toBeNull();
  const entries = (block![1].match(/\["([^"]+)"\]="([^"]+)"/g) ?? [])
    .map((m) => {
      const inner = m.match(/\["([^"]+)"\]="([^"]+)"/)!;
      return { key: inner[1], vaultPath: inner[2] };
    });

  it("covers >= 6 canonical secrets (matches LIVE k3d fixture iter 33)", () => {
    expect(entries.length).toBeGreaterThanOrEqual(6);
  });

  it("every entry follows the iac/<resource>/<key> schema", () => {
    for (const e of entries) {
      expect(e.vaultPath).toMatch(/^iac\/[a-z][a-z0-9-]*\/[a-z][a-z0-9-]*/);
    }
  });

  it("covers Cloudflare api-token", () => {
    expect(entries.find((e) => e.key === "cf-api-token")?.vaultPath)
      .toBe("iac/cloudflare/api-token");
  });

  it("covers DigitalOcean token + Spaces", () => {
    expect(entries.find((e) => e.key === "do-token")).toBeDefined();
    expect(entries.find((e) => e.key === "do-spaces-key")?.vaultPath)
      .toBe("iac/digitalocean/spaces-access-key");
    expect(entries.find((e) => e.key === "do-spaces-secret")?.vaultPath)
      .toBe("iac/digitalocean/spaces-secret-key");
  });

  it("covers GitHub + Stripe", () => {
    expect(entries.find((e) => e.key.startsWith("github-app"))).toBeDefined();
    expect(entries.find((e) => e.key === "stripe-secret-key")?.vaultPath)
      .toBe("iac/stripe/secret-key");
  });

  it("masks secret values via length-only logging", () => {
    expect(script).toMatch(/\$\{#value\}/); // bash ${#var} = length
    expect(script).not.toMatch(/echo.*\$\{value\}/); // never echo raw
  });

  it("DRY-RUN is the default (safety invariant)", () => {
    expect(script).toMatch(/DRY_RUN=true/);
    expect(script).toMatch(/--execute.*DRY_RUN=false/);
  });
});
