// CHG-2026-04-30-INFRA-NORMALIZE / T5.6 + T5.7 — canary script invariants
//
// Iter 6 (commit cd0b65a5) shipped the cell-2 canary scripts but had no
// vitest pinning their structure. Iter 25 closes that gap — same pattern
// as iter 18 (workflow YAML) and iter 24 (CC-bootstrap-lib).
//
// Pins:
//   - Both scripts exist + executable
//   - Both declare exit codes 0-5
//   - Both support --dry-run flag
//   - Both reference operator-only loopzero admin tenants pin command
//   - Both consume cell-1 EB metric for halt criterion
//   - Stage 1 has 1h soak default
//   - Stage 2 has 24h soak default + 10% sample default
//   - Stage 1 has trap on EXIT for synthetic cleanup
//
// Pre-LIVE-execution invariants only (HTTP probe + soak require cell-2
// cloud cluster, blocked).

import { existsSync, readFileSync, statSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const REPO_ROOT = resolve(__dirname, "..", "..", "..", "..");
const PARITY_DIR = resolve(REPO_ROOT, "infra", "tests", "parity");

function isExecutable(path: string): boolean {
  if (!existsSync(path)) return false;
  return (statSync(path).mode & 0o111) !== 0;
}

describe("cell-2 canary scripts (T5.6 + T5.7) — offline invariants", () => {
  describe("cell2-canary-stage1.sh (T5.6 — 1h synthetic soak)", () => {
    const path = resolve(PARITY_DIR, "cell2-canary-stage1.sh");
    const text = existsSync(path) ? readFileSync(path, "utf-8") : "";

    it("exists at canonical location", () => {
      expect(existsSync(path), "stage1 script must exist").toBe(true);
    });

    it("is executable", () => {
      expect(isExecutable(path)).toBe(true);
    });

    it("declares CHG ref + plan T5.6 in header", () => {
      expect(text).toMatch(/CHG-2026-04-30-INFRA-NORMALIZE/);
      expect(text).toMatch(/T5\.6/);
    });

    it("supports --dry-run flag (offline preflight)", () => {
      expect(text).toMatch(/--dry-run/);
    });

    it("documents exit codes 0-5 in header", () => {
      // 0 GREEN, 1 preflight, 2 pin failed, 3 HTTP probe failed, 4 EB violation, 5 orchestration
      for (const code of ["0", "1", "2", "3", "4", "5"]) {
        expect(text, `exit code ${code} must be documented`).toMatch(
          new RegExp(`^#\\s*${code}\\s+[-—]`, "m"),
        );
      }
    });

    it("uses operator-only command 'loopzero admin tenants pin' (T5.5 superseded path)", () => {
      // Stage 1 must use the audit-trail operator command, not deploy --cell.
      expect(text).toMatch(/loopzero\s+admin\s+tenants\s+pin/);
    });

    it("default soak is 1h (3600s) per plan T5.6", () => {
      expect(text).toMatch(/SOAK_SECONDS[:=][^}]*3600/);
    });

    it("monitors cell-1 EB threshold 0.1% (tighter than stage 2)", () => {
      // Stage 1 is synthetic — tighter threshold than stage 2's 1.0%.
      expect(text).toMatch(/EB_THRESHOLD_PCT[:=][^}]*0\.1/);
    });

    it("declares trap on EXIT for synthetic cleanup", () => {
      expect(text).toMatch(/trap\s+'?rollback'?\s+EXIT/);
    });

    it("verifies cell-2 ClusterCredential connected in preflight", () => {
      expect(text).toMatch(/clustercredential.*cell-2|cell-2.*connected/i);
    });

    it("verifies loopzero_cell_discovery_count >= 2 in preflight", () => {
      expect(text).toMatch(/loopzero_cell_discovery_count/);
    });
  });

  describe("cell2-canary-stage2.sh (T5.7 — 24h 10% soak)", () => {
    const path = resolve(PARITY_DIR, "cell2-canary-stage2.sh");
    const text = existsSync(path) ? readFileSync(path, "utf-8") : "";

    it("exists at canonical location", () => {
      expect(existsSync(path)).toBe(true);
    });

    it("is executable", () => {
      expect(isExecutable(path)).toBe(true);
    });

    it("declares CHG ref + plan T5.7 in header", () => {
      expect(text).toMatch(/CHG-2026-04-30-INFRA-NORMALIZE/);
      expect(text).toMatch(/T5\.7/);
    });

    it("supports --dry-run + --soak-seconds + --percent flags", () => {
      expect(text).toMatch(/--dry-run/);
      expect(text).toMatch(/--soak-seconds/);
      expect(text).toMatch(/--percent/);
    });

    it("documents exit codes 0-5 in header", () => {
      for (const code of ["0", "1", "2", "3", "4", "5"]) {
        expect(text, `exit code ${code} must be documented`).toMatch(
          new RegExp(`^#\\s*${code}\\s+[-—]`, "m"),
        );
      }
    });

    it("requires T5.6 stage 1 closure runbook before running", () => {
      // Plan T5.7 acceptance: 'BLOCKED BY T5.6 GREEN'. Script asserts
      // multi-cell-canary.md exists (T5.6 deliverable) before sample.
      expect(text).toMatch(/multi-cell-canary\.md/);
    });

    it("default soak is 24h (86400s) per plan T5.7", () => {
      expect(text).toMatch(/SOAK_SECONDS[:=][^}]*86400/);
    });

    it("default sample is 10% per plan T5.7", () => {
      // Bash default-substitution form: ${PERCENT:-10}
      expect(text).toMatch(/PERCENT[^}]*:-10\}/);
    });

    it("EB threshold is 1.0% (looser than stage 1 — real traffic variance)", () => {
      expect(text).toMatch(/EB_THRESHOLD_PCT[:=][^}]*1\.0/);
    });

    it("uses operator-only command 'loopzero admin tenants pin' for batch", () => {
      expect(text).toMatch(/loopzero\s+admin\s+tenants\s+pin/);
    });

    it("does NOT auto-rollback on success (10% stays on cell-2)", () => {
      // Plan T5.7 design: stage 2 GREEN exits with 10% on cell-2 as the
      // desired post-state going into T7.1. trap on EXIT must NOT call
      // rollback (only HALT branches do).
      expect(text).not.toMatch(/trap\s+'?rollback'?\s+EXIT/);
    });
  });

  describe("multi-cell-canary.md sibling runbook (T5.6 + T5.7)", () => {
    const path = resolve(REPO_ROOT, "docs", "runbooks", "multi-cell-canary.md");
    const text = existsSync(path) ? readFileSync(path, "utf-8") : "";

    it("exists at canonical location", () => {
      expect(existsSync(path)).toBe(true);
    });

    it("documents both stages (1h synthetic + 24h 10%)", () => {
      expect(text).toMatch(/Stage\s+1.*T5\.6/i);
      expect(text).toMatch(/Stage\s+2.*T5\.7/i);
    });

    it("references both canary scripts", () => {
      expect(text).toMatch(/cell2-canary-stage1\.sh/);
      expect(text).toMatch(/cell2-canary-stage2\.sh/);
    });

    it("documents manual rollback playbook", () => {
      expect(text).toMatch(/##\s+Rollback\s+playbook/i);
      expect(text).toMatch(/loopzero\s+admin\s+tenants\s+pin/);
    });
  });
});
