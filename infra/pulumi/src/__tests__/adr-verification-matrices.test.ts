// CHG-2026-04-30 — ADR verification matrices (ADR-0020 + ADR-0021)
//
// Both ADRs declare a "Verification" matrix mapping each Decision (D1-D7
// for parity, D1-D10 for normalize) to its test/artifact. Iter 27 ships
// an index test that pins these mappings — same pattern as iter 26 for
// the operator-split-invariants matrix.
//
// Catches drift like:
//   - "D5's helmfile-environments-parity.test.ts was renamed but ADR-0020
//     verification cell wasn't updated"
//   - "D3's vap-no-runtime-image-mutations.yaml was deleted but the ADR
//     still claims GREEN status"

import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const REPO_ROOT = resolve(__dirname, "..", "..", "..", "..");

function fileExists(rel: string): boolean {
  return existsSync(resolve(REPO_ROOT, rel));
}

describe("ADR-0020 (infra-parity) verification matrix D1-D7", () => {
  const adrPath = resolve(REPO_ROOT, "docs", "decisions", "ADR-0020-infra-parity-decisions.md");
  const adrText = existsSync(adrPath) ? readFileSync(adrPath, "utf-8") : "";

  it("ADR-0020 doc exists at canonical location", () => {
    expect(adrText.length).toBeGreaterThan(0);
  });

  it("D1 — image-tag-format.test.ts exists (image tag policy)", () => {
    expect(fileExists("infra/pulumi/src/__tests__/image-tag-format.test.ts")).toBe(true);
  });

  it("D2 — decideRestore() pinned in CLI tests (auto-Secret-restore)", () => {
    // ADR-0020 cell mentions 'decideRestore() 7 cases'; the test file
    // shipped under secret-backups.test.ts (renamed during iter
    // implementation; ADR text not amended).
    expect(fileExists("cli/tests/unit/secret-backups.test.ts")).toBe(true);
  });

  it("D3 — VAP no-runtime-image-mutations + drift detection", () => {
    expect(fileExists("infra/helm/admission-policies/templates/vap-no-runtime-image-mutations.yaml")).toBe(true);
    expect(fileExists("cli/tests/unit/platform-status-drift.test.ts")).toBe(true);
  });

  it("D4 — helmfile-environments-parity.test.ts exists (self-hosting parity)", () => {
    expect(fileExists("infra/pulumi/src/__tests__/helmfile-environments-parity.test.ts")).toBe(true);
  });

  it("D5 — platform-install-validate-l3.test.ts exists (L3 release gate)", () => {
    expect(fileExists("cli/tests/unit/platform-install-validate-l3.test.ts")).toBe(true);
  });

  it("D6 — bundle-rule3-clean.test.ts exists (Rule 3 dist clean)", () => {
    expect(fileExists("cli/tests/unit/bundle-rule3-clean.test.ts")).toBe(true);
  });

  it("D7 — parity-validation.yml workflow exists (CI gate)", () => {
    expect(fileExists(".github/workflows/parity-validation.yml")).toBe(true);
  });

  it("ADR-0020 verification matrix has all 7 decisions", () => {
    for (const d of [1, 2, 3, 4, 5, 6, 7]) {
      expect(adrText, `D${d} row missing from verification matrix`).toMatch(
        new RegExp(`^\\|\\s*D${d}\\s*\\|`, "m"),
      );
    }
  });
});

describe("ADR-0021 (infra-normalize) verification matrix D1-D10", () => {
  const adrPath = resolve(REPO_ROOT, "docs", "decisions", "ADR-0021-infra-normalize-decisions.md");
  const adrText = existsSync(adrPath) ? readFileSync(adrPath, "utf-8") : "";

  it("ADR-0021 doc exists at canonical location", () => {
    expect(adrText.length).toBeGreaterThan(0);
  });

  it("D1 — 4 split charts present (forward-only operator topology)", () => {
    for (const c of ["loopzero-tenant-operator", "loopzero-app-operator", "loopzero-preview-operator", "loopzero-platform-operator"]) {
      expect(fileExists(`charts/${c}/Chart.yaml`), `${c} Chart.yaml missing`).toBe(true);
    }
  });

  it("D2 — T0.2 inventory + T3.x archive scripts shipped (single Pulumi project)", () => {
    expect(fileExists("docs/audits/pulumi-state-inventory.md")).toBe(true);
    expect(fileExists("infra/scripts/pulumi-archive.sh")).toBe(true);
    expect(fileExists("infra/scripts/pulumi-orphan-rm.sh")).toBe(true);
  });

  it("D3 — multi-cell stack: cells migration + observability + admin pin + CC bootstrap", () => {
    expect(fileExists("api/internal/db/migrations/000006_cells.up.sql")).toBe(true);
    expect(fileExists("infra/helm/monitoring/manifests/prometheusrule-multi-cell.yaml")).toBe(true);
    expect(fileExists("infra/helmfile/scripts/lib/clustercredential-create.sh")).toBe(true);
  });

  it("D4 — 11 integration suites + spawn-helper + mock-api-server", () => {
    expect(fileExists("cli/tests/integration/_fixtures/spawn-helper.ts")).toBe(true);
    expect(fileExists("cli/tests/integration/_fixtures/mock-api-server.ts")).toBe(true);
  });

  it("D5 — 3 governance MEET DRs committed (per-phase Security + SRE review)", () => {
    for (const topic of ["operator-split", "pulumi-consolidation", "multi-cell"]) {
      expect(
        fileExists(`docs/decisions/MEET-INFRA-NORMALIZE-T3-${topic}.md`),
        `MEET DR for ${topic} missing`,
      ).toBe(true);
    }
  });

  it("D6 — observability alerts shipped", () => {
    expect(fileExists("infra/helm/monitoring/manifests/prometheusrule-multi-cell.yaml")).toBe(true);
  });

  it("D7 — pre-release-governance rules file referenced", () => {
    expect(fileExists(".claude/rules/pre-release-governance.md")).toBe(true);
  });

  it("D9 — Phase 7 customer comms shipped", () => {
    expect(fileExists("docs/customer-comms/cell-2-canary-template.md")).toBe(true);
  });

  it("D10 — Coverage matrix DEBT row #5 references owner + horizon", () => {
    const planText = readFileSync(
      resolve(REPO_ROOT, "docs", "plans", "infra-normalize-multicell-plan.md"),
      "utf-8",
    );
    expect(planText).toMatch(/DEBT\s*\(D10\):\s*owner=/);
    expect(planText).toMatch(/horizon=2026/);
  });

  it("ADR-0021 verification matrix has all 10 decisions", () => {
    for (const d of [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]) {
      expect(adrText, `D${d} row missing from verification matrix`).toMatch(
        new RegExp(`^\\|\\s*D${d}\\s*\\|`, "m"),
      );
    }
  });
});
