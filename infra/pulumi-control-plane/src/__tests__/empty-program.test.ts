// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Guard-rail tests for the intentionally-empty pulumi-control-plane program.
// Audit gap M05 #9 (active-execution-plan §6.E.3): the audit reported this
// project at 1735 LOC with 0 tests. T1.8 of Wave 1 deleted 1538 LOC (the 8
// orphaned sibling implementation files); the program is now intentionally
// empty per MEET-2026-05-12 D3 (bipartite SoT — control-plane services live
// in L2-helmfile, NOT L1-pulumi).
//
// These tests are guard-rails that fail loud if a future contributor
// reintroduces the rejected unified-Pulumi path (installLoopZeroAPI,
// installCloudService, etc.) without first superseding D3 via a new T3
// meeting per pre-release governance.

import { describe, it, expect } from "vitest";
import * as fs from "fs";
import * as path from "path";

const SRC_DIR = path.resolve(__dirname, "..");

describe("pulumi-control-plane — intentional empty program guard", () => {
  it("src/ contains exactly one TypeScript file: index.ts", () => {
    // The 8 orphaned sibling files (cp-api-service.ts, cp-cloud-service.ts,
    // cp-ingress.ts, cp-network.ts, cp-ory-hydra.ts, cp-ory-kratos.ts,
    // cp-tenant-controller.ts, mgmt-control-plane-services.ts) were
    // deleted in T1.8. If any of them — or any new sibling — appears in
    // src/, this test fails because a unified-Pulumi reintroduction must
    // first supersede MEET-2026-05-12 D3.
    const entries = fs.readdirSync(SRC_DIR).filter((e) => e.endsWith(".ts"));
    expect(entries).toEqual(["index.ts"]);
  });

  it("index.ts exports the bipartite SoT marker output", () => {
    // The empty program preserves `controlPlane` stack output so consumers
    // that read it (orchestration scripts in infra/scripts/) don't break.
    // The shape must include `sot: "L2-helmfile"` and reference D3.
    const src = fs.readFileSync(path.join(SRC_DIR, "index.ts"), "utf-8");
    expect(src).toContain('export const controlPlane = pulumi.output({');
    expect(src).toContain('sot: "L2-helmfile"');
    expect(src).toContain("MEET-2026-05-12 D3");
  });

  it("index.ts explicitly documents the rejected unified-Pulumi path", () => {
    // The comment block records WHY the program is empty so a future
    // contributor reading the file doesn't think it was forgotten.
    const src = fs.readFileSync(path.join(SRC_DIR, "index.ts"), "utf-8");
    expect(src).toContain("BIPARTITE SoT");
    expect(src).toContain("rejected_path");
    // The binding-until line names the protocol for unwinding the decision.
    expect(src).toContain("next T3 meeting supersedes D3");
  });

  it("index.ts does NOT contain any installXxx function implementations", () => {
    // Guard against reintroduction of the deleted TODO functions.
    // The 10 names below are the exact functions deleted in T1.8.
    const src = fs.readFileSync(path.join(SRC_DIR, "index.ts"), "utf-8");
    const forbidden = [
      "installLoopZeroAPI",
      "installCloudService",
      "installTenantController",
      "installOryKratos",
      "installOryHydra",
      "installArgoCD",
      "installGitea",
      "installMinIO",
      "installCertManager",
      "installTraefik",
    ];
    for (const fn of forbidden) {
      // Allow these names to appear ONLY in the explanatory comment block
      // that lists them as historically-deleted. The signature `function
      // <name>(` or `const <name> =` would be the regression we're guarding.
      expect(src).not.toMatch(new RegExp(`function\\s+${fn}\\s*\\(`));
      expect(src).not.toMatch(new RegExp(`const\\s+${fn}\\s*=`));
    }
  });

  it("LOC stays bounded (≤100 lines) — defends the empty-program invariant", () => {
    // The current file is 64 lines. A bound of 100 lines leaves headroom for
    // doc-comment expansion (e.g., extending the references section) while
    // catching a regression where someone starts adding real Pulumi resources.
    // If the empty-program decision is ever unwound via a new T3 meeting, this
    // bound moves to the new ceiling.
    const src = fs.readFileSync(path.join(SRC_DIR, "index.ts"), "utf-8");
    const lines = src.split("\n").length;
    expect(lines).toBeLessThanOrEqual(100);
  });
});
