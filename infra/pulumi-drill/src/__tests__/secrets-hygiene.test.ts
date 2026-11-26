// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// T0.4 (plan 2026-05-21-deep-review-remediation) — guard test asserting
// that every Pulumi output embedding a sensitive Output<string>
// (specifically: drillPG.password in DSNs) is wrapped in `pulumi.secret()`.
//
// Background: prior to commit applied 2026-05-21, the drill DSN exports
// kratosDsn / hydraDsn / giteaDsn / loopRuntimeApiDsn were declared as plain
// `pulumi.interpolate` — the DB password leaked in plaintext to
// `pulumi stack output` and to the state file.
//
// Per ADR D3 of the deep-review-remediation plan: any Pulumi output that
// embeds an Output<string> of a sensitive value MUST be pulumi.secret()-wrapped.
//
// This is a grep-based source assertion (consistent with structure.test.ts
// pattern) so it runs without requiring a live Pulumi stack.

import { describe, it, expect } from "vitest";
import * as fs from "fs";
import * as path from "path";

const SRC = path.resolve(__dirname, "..");
const INDEX = path.join(SRC, "index.ts");

describe("loop-runtime-drill — DSN exports must be pulumi.secret-wrapped (T0.4)", () => {
  const src = fs.readFileSync(INDEX, "utf-8");

  // The 4 DSN exports that embed ${drillPG.password}
  const sensitiveExports = ["kratosDsn", "hydraDsn", "giteaDsn", "loopRuntimeApiDsn"];

  for (const exportName of sensitiveExports) {
    it(`exports ${exportName} wrapped in pulumi.secret()`, () => {
      // Match either:
      //   export const X = pulumi.secret(pulumi.interpolate`...`);
      //   export const X = pulumi.secret(...);
      // and reject the bare:
      //   export const X = pulumi.interpolate`...`;
      const wrappedPattern = new RegExp(
        `export const ${exportName}\\s*=\\s*pulumi\\.secret\\(`,
      );
      const barePattern = new RegExp(
        `export const ${exportName}\\s*=\\s*pulumi\\.interpolate`,
      );

      expect(src).toMatch(wrappedPattern);
      expect(src).not.toMatch(barePattern);
    });
  }

  it("no DSN export embeds drillPG.password without secret wrap", () => {
    // Find every export const line that contains drillPG.password.
    const lines = src.split("\n");
    const offendingLines: string[] = [];
    for (const line of lines) {
      if (!line.includes("export const")) continue;
      if (!line.includes("drillPG.password")) continue;
      // Must be wrapped in pulumi.secret(
      if (!line.includes("pulumi.secret(")) {
        offendingLines.push(line.trim());
      }
    }
    expect(offendingLines).toEqual([]);
  });
});
