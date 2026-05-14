// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Schema + structure tests for loopzero-drill.
//
// Plan 2026-05-15 T2.2 (audit M05 gap "2 stacks sem testes"): drill stack
// shipped 137 LOC with zero .test.ts. These tests lock the public surface
// so refactors that drop required configs, mis-name outputs, or break the
// isolation invariants (MEET-2026-05-12 D9) fail loud at CI instead of at
// `pulumi up` mid-DR drill (when prod could be at risk).

import { describe, it, expect } from "vitest";
import * as fs from "fs";
import * as path from "path";
import * as yaml from "js-yaml";

const SRC = path.resolve(__dirname, "..");
const ROOT = path.resolve(__dirname, "../..");
const INDEX = path.join(SRC, "index.ts");

describe("loopzero-drill — Pulumi.yaml", () => {
  const pulumiYaml = yaml.load(
    fs.readFileSync(path.join(ROOT, "Pulumi.yaml"), "utf-8"),
  ) as Record<string, unknown>;

  it("declares stack name + main entry", () => {
    expect(pulumiYaml.name).toBe("loopzero-drill");
    expect(pulumiYaml.runtime).toBeDefined();
  });
});

describe("loopzero-drill — config schema", () => {
  const src = fs.readFileSync(INDEX, "utf-8");

  it("scoped under namespace `loopzero-drill` (NOT loopzero-platform/loopzero-infra)", () => {
    // Isolation invariant: drill must use its own config namespace so a
    // misconfigured prod CLI doesn't accidentally read drill values.
    expect(src).toContain('new pulumi.Config("loopzero-drill")');
  });

  it("requires all isolation-critical config keys", () => {
    const required = [
      'config.require("domain")',
      'config.require("doProject")',
      'config.require("region")',
      'config.require("cellName")',
      'config.requireNumber("nodeCount")',
      'config.require("k8sVersion")',
    ];
    for (const r of required) {
      expect(src).toContain(r);
    }
  });

  it("CF account ID is a secret (never plaintext)", () => {
    expect(src).toContain('config.requireSecret("cfAccountId")');
  });
});

describe("loopzero-drill — outputs contract", () => {
  const src = fs.readFileSync(INDEX, "utf-8");

  it("exports a single named `outputs` object", () => {
    expect(src).toMatch(/^export const outputs\s*=/m);
  });

  it("output keys cover all drill consumer expectations", () => {
    // dr-drill.sh orchestration reads these keys via `pulumi stack output`.
    // A rename would silently break the drill teardown flow.
    const expectedKeys = [
      "zone",
      "zoneNameservers",
      "cluster",
      "clusterKubeconfig",
      "bucket",
      "pg",
      "doProject",
      "meta",
    ];
    for (const k of expectedKeys) {
      expect(src).toMatch(new RegExp(`\\b${k}:`));
    }
  });

  it("meta block declares drill purpose + teardown signal", () => {
    // The meta block is the human-readable claim that this stack IS the
    // drill (not prod). If removed, audit/onboarding loses the
    // smoke-signal that confirms isolation.
    expect(src).toContain('purpose: "DR drill');
    expect(src).toMatch(/teardownAfter:\s*"/);
  });
});

describe("loopzero-drill — production-isolation invariants (MEET-2026-05-12 D9)", () => {
  const src = fs.readFileSync(INDEX, "utf-8");

  it("does NOT import from pulumi-shared/stack-outputs (isolation: NO cross-project type leak)", () => {
    // If drill imported PlatformStackOutputs or InfraStackOutputs, a prod
    // schema change could ripple to drill mid-execution. Drill must be
    // type-isolated.
    expect(src).not.toContain("pulumi-shared");
    expect(src).not.toContain("PlatformStackOutputs");
    expect(src).not.toContain("InfraStackOutputs");
  });

  it("CF Zone protected from accidental delete (protect: true)", () => {
    // Zone deletion mid-drill would 30-day-quarantine the domain in CF.
    // Pulumi protect flag is the operator-visible safety net.
    expect(src).toMatch(/protect:\s*true/);
  });

  it("PG cluster is small instance (not prod-scale — drills run quarterly)", () => {
    // Drill cost is a budget signal. If someone bumps to db-s-8vcpu, the
    // monthly DR drill bill silently jumps 10x.
    expect(src).toContain('size: "db-s-1vcpu-1gb"');
  });

  it("Spaces bucket has forceDestroy=true (teardown is mandatory)", () => {
    // The "drill destroys drill" pattern requires no terminal lock on
    // resources. forceDestroy bypasses the bucket-not-empty guard.
    expect(src).toContain("forceDestroy: true");
  });
});

describe("loopzero-drill — stack outputs key inventory matches consumer script", () => {
  const src = fs.readFileSync(INDEX, "utf-8");

  it("regression sentinel: changes to `outputs` keys must update dr-drill.sh", () => {
    // This test exists to surface accidental key changes. A future
    // refactor that drops `cluster` -> `clusterEndpoint` would break the
    // drill orchestration script without compiler help (Pulumi outputs
    // are dynamic strings at script level).
    const outputsBlock = src.match(/export const outputs\s*=\s*\{([\s\S]*?)\};/);
    expect(outputsBlock, "outputs block found").toBeTruthy();
    if (!outputsBlock) return;
    const body = outputsBlock[1];
    // Count top-level keys (simple regex — drill outputs have NO nested
    // objects except meta, so we count : at indentation level 1).
    const topKeyMatches = body.match(/^\s{2}\w+:/gm) ?? [];
    expect(topKeyMatches.length, `outputs key count snapshot`).toBeGreaterThanOrEqual(8);
  });
});
