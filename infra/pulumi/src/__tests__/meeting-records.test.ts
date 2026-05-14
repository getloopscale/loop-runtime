// CHG-2026-04-30-INFRA-NORMALIZE / T-1.1 + T-1.2 + T-1.3 — meeting record invariants
//
// Translates plan TDD pseudocode names to real vitest:
//   T-1.1: test_meeting_record_has_decision, test_meeting_record_lists_alternatives,
//          test_meeting_record_captures_vetoes, test_meeting_record_exists
//   T-1.2: test_meeting_includes_gitops_review (gitops-engineer review captured)
//
// These records (3 governance DRs from Phase -1) are the canonical
// authority that gates Phase 2/3/5/7 implementation per
// .claude/rules/pre-release-governance.md. If a future DR drops the
// alternatives section, vetoes section, or verdict line, these tests
// catch it before merge — preventing the "decision recorded but no
// audit trail" failure mode the plan explicitly flags.

import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const REPO_ROOT = resolve(__dirname, "..", "..", "..", "..");
// 2026-05-11 doc cleanup (fc1793dd) moved governance records from
// docs/decisions/ to .claude/knowledge-base/meetings/. See finding
// `.claude/knowledge-base/concepts/operations/finding-2026-05-15-pulumi-vitest-23-fail-drift.md`.
const DR_DIR = resolve(REPO_ROOT, ".claude", "knowledge-base", "meetings");

const RECORDS = [
  {
    file: "MEET-INFRA-NORMALIZE-T3-operator-split.md",
    topic: "operator topology",
    requiresGitopsReview: false,
  },
  {
    file: "MEET-INFRA-NORMALIZE-T3-pulumi-consolidation.md",
    topic: "Pulumi consolidation",
    requiresGitopsReview: true,
  },
  {
    file: "MEET-INFRA-NORMALIZE-T3-multi-cell.md",
    topic: "multi-cell activation",
    requiresGitopsReview: false,
  },
] as const;

describe("Phase -1 governance decision records (T-1.1 + T-1.2 + T-1.3)", () => {
  for (const rec of RECORDS) {
    describe(rec.file, () => {
      const path = resolve(DR_DIR, rec.file);

      it("exists at canonical location", () => {
        expect(existsSync(path), `${rec.file} must exist in .claude/knowledge-base/meetings/`).toBe(true);
      });

      const text = existsSync(path) ? readFileSync(path, "utf-8") : "";

      it("has explicit Verdict line", () => {
        // Plan T-1.x acceptance: Status ∈ {APPROVED, APPROVED_WITH_CONDITIONS, REJECTED}
        expect(text).toMatch(
          /\*\*Verdict:?\*\*[^\n]*\b(APPROVED|APPROVED_WITH_CONDITIONS|REJECTED)\b/i,
        );
      });

      it("lists alternatives (Option A/B/C pattern)", () => {
        // Plan: "Each decision gets alternatives comparison"
        // Captures the canonical Option A/B/C structure from
        // decision-record-moderator template.
        expect(text).toMatch(/\bOption\s+[ABC]\b/);
      });

      it("captures vetoes / veto criteria", () => {
        // Plan T-1.x veto criterion: every reviewer with veto power
        // must declare their veto criterion explicitly.
        expect(text).toMatch(/\b(veto|Veto)\b/);
      });

      it("references topic in title or body", () => {
        // Sanity check — record matches its declared topic.
        const re = new RegExp(rec.topic.replace(/\s+/g, "\\s+"), "i");
        expect(text).toMatch(re);
      });

      if (rec.requiresGitopsReview) {
        it("includes gitops-engineer review (T-1.2 specific)", () => {
          expect(text).toMatch(/gitops[- ]engineer/i);
        });
      }
    });
  }

  it("all 3 DRs are committed (no missing files)", () => {
    const missing = RECORDS.filter((r) => !existsSync(resolve(DR_DIR, r.file)));
    expect(missing.length, `missing DRs: ${missing.map((m) => m.file).join(", ")}`).toBe(0);
  });
});
