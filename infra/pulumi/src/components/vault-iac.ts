// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

/**
 * T9 of plan 2026-06-02-openbao-adoption v1.3 — Pulumi Vault provider for
 * IaC secrets. Replaces `secure: v1:...` inline encrypted entries in
 * Pulumi.*.yaml with `vault.generic.getSecret` data source reads.
 *
 * Benefits:
 *   - Secrets visible in Vault audit log (every read by Pulumi tracked)
 *   - Rotation via Vault KV write — Pulumi picks up next plan/apply
 *   - No shared passphrase between team members
 *   - Compatible with all existing Pulumi providers (AWS, CF, DO)
 *
 * Operator-time bootstrap: see docs/runbooks.md.
 *
 * Migration pattern (T9.4):
 *   BEFORE (Pulumi.dev.yaml):
 *     config:
 *       loopruntime:cf-api-token:
 *         secure: v1:AAAA==
 *
 *   AFTER (Pulumi.dev.yaml):
 *     config:
 *       loopruntime:cf-api-token-vault-path: api/secrets/cloudflare/api-token
 *
 *   AFTER (TS code):
 *     const cfToken = await VaultSecretReader.read("api/secrets/cloudflare/api-token");
 *
 * The TS reader hides the @pulumi/vault dep behind a domain interface so
 * tests can substitute a mock provider (DIP).
 */

import * as pulumi from "@pulumi/pulumi";

/**
 * Minimal contract for Vault KV-v2 secret reads from Pulumi.
 *
 * The Pulumi vault provider returns an Output wrapping a SecretReadResult
 * — we expose the wrapper so downstream resources compose with Pulumi's
 * dependency graph (.apply chains).
 */
export interface VaultSecretSource {
  /**
   * Read a single secret value at the given KV-v2 path with named key.
   *
   * @param path  Full Vault path (e.g., `secret/data/loopruntime/dev/api/secrets/cloudflare/api-token`)
   * @param key   Sub-key within the kv-v2 secret data map
   */
  read(path: string, key: string): pulumi.Output<string>;
}

/**
 * VaultSecretReader is the canonical implementation backed by @pulumi/vault.
 *
 * Late-binds the @pulumi/vault import so the package is only required when
 * Vault provider is actually configured (avoids dependency on every dev
 * stack that doesn't yet have Vault active).
 */
export class VaultSecretReader implements VaultSecretSource {
  private readonly providerName: string;

  constructor(providerName = "vault-default") {
    this.providerName = providerName;
  }

  read(path: string, key: string): pulumi.Output<string> {
    // Late-bind import: only fails if Pulumi.stack.yaml configured the
    // vault provider but the package is missing — fail-fast with clear
    // error rather than silent runtime crash.
    let vault: any;
    try {
      // eslint-disable-next-line @typescript-eslint/no-var-requires
      vault = require("@pulumi/vault");
    } catch (e: any) {
      throw new Error(
        `[VaultSecretReader] @pulumi/vault not installed. Add to package.json ` +
          `dependencies or remove vault: provider from Pulumi.<stack>.yaml. ` +
          `Original error: ${e.message}`,
      );
    }
    // KV-v2 getSecret takes the FULL Vault path (incl. 'secret/data/...' prefix).
    // The result.data map contains the kv-v2 secret keys.
    const secret = vault.generic.getSecretOutput(
      { path },
      { provider: this.getProvider(vault) },
    );
    return secret.data.apply((data: Record<string, any>) => {
      const value = data[key];
      if (value === undefined || value === null) {
        throw new Error(
          `[VaultSecretReader] missing key '${key}' at path '${path}'. ` +
            `Verify the secret was written with this key in Vault KV-v2.`,
        );
      }
      return String(value);
    });
  }

  private getProvider(vault: any): any {
    // Look up the configured Vault provider by name. Pulumi auto-wires
    // the provider when Pulumi.<stack>.yaml has `vault:address` set.
    return vault.Provider.get(this.providerName, this.providerName);
  }
}

/**
 * StaticVaultSecretSource is a test/dev fallback that returns hardcoded
 * values WITHOUT consulting Vault. Used in unit tests and for stacks
 * that have not yet migrated. NEVER use in production.
 */
export class StaticVaultSecretSource implements VaultSecretSource {
  constructor(private readonly fixture: Record<string, Record<string, string>>) {}

  read(path: string, key: string): pulumi.Output<string> {
    const pathMap = this.fixture[path];
    if (!pathMap) {
      throw new Error(`[StaticVaultSecretSource] path '${path}' not in fixture`);
    }
    const value = pathMap[key];
    if (value === undefined) {
      throw new Error(`[StaticVaultSecretSource] key '${key}' missing under path '${path}'`);
    }
    return pulumi.output(value);
  }
}
