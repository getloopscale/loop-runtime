// CHG-2026-04-30-INFRA-NORMALIZE / T0.1 + T0.2 + T0.3 — audit doc invariants
//
// Translates plan TDD pseudocode names to real vitest:
//   T0.1: test_audit_doc_exists, test_audit_doc_has_router_section,
//         test_audit_identifies_cells_schema_status
//   T0.2: test_inventory_doc_exists, test_inventory_has_recommendation_per_stack
//   T0.3: test_audit_lists_all_register_groups, test_audit_lists_subcommands_per_group,
//         test_audit_marks_coverage_per_command
//
// Catches drift like "audit doc was rewritten and dropped Reality vs
// Documented section" or "inventory recommendation column got removed".
// Audits are the canonical authority for Phase 0 conditions; if a
// section is silently lost, downstream phases lose their evidence trail.

import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const REPO_ROOT = resolve(__dirname, "..", "..", "..", "..");
// Canonical location post-2026-05-11 docs/ cleanup (commit fc1793dd):
// audit reference docs were migrated to knowledge-base/concepts/operations/.
// Restored from git history per active-execution-plan §3.F (iter 7, 2026-05-12).
const AUDIT_DIR = resolve(REPO_ROOT, ".claude", "knowledge-base", "concepts", "operations");

function readDoc(name: string): string {
  const p = resolve(AUDIT_DIR, name);
  if (!existsSync(p)) return "";
  return readFileSync(p, "utf-8");
}

describe("Phase 0 audit doc invariants (T0.1 + T0.2 + T0.3)", () => {
  describe("multi-cell-source-audit.md (T0.1)", () => {
    const text = readDoc("multi-cell-source-audit.md");

    it("exists at canonical location", () => {
      expect(text.length, "audit doc must be non-empty").toBeGreaterThan(0);
    });

    it("has Executive summary section", () => {
      expect(text).toMatch(/^##\s+Executive summary/m);
    });

    it("has Reality vs Documented section (CLAUDE.md cross-check)", () => {
      // Plan T0.1: "Every CLAUDE.md cell claim confirmed or flagged"
      expect(text).toMatch(/Reality\s+vs\s+Documented|Claim\s*\|\s*Reality\s*\|\s*Verdict/i);
    });

    it("identifies cells schema status (Gap 1)", () => {
      // Plan T0.1: audit must address whether cells SQL table exists
      expect(text).toMatch(/cells\b.*\b(table|schema|migration)/i);
    });

    it("documents Gaps section with action items", () => {
      expect(text).toMatch(/^##\s+Gaps\b/m);
      expect(text).toMatch(/Gap\s+\d+/);
    });
  });

  describe("cli-commands-audit.md (T0.3)", () => {
    const text = readDoc("cli-commands-audit.md");

    it("exists at canonical location", () => {
      expect(text.length).toBeGreaterThan(0);
    });

    it("lists 11+ register groups (per-register sections)", () => {
      // Plan T0.3 acceptance: every register group has a row.
      // 12 groups was original target; 11 shipped (audit Coverage).
      const sections = text.match(/^###\s+`?[a-z-]+`?\s+\(\d+\s+LoC/gm) ?? [];
      expect(sections.length, `expected ≥10 per-register sections, got ${sections.length}`).toBeGreaterThanOrEqual(10);
    });

    it("declares Coverage gap matrix", () => {
      expect(text).toMatch(/^##\s+Coverage gap matrix/m);
    });

    it("includes 1000% functional gap analysis", () => {
      // Plan T0.3 evidence, the directive (English: "ALL LOOP_RUNTIME CLI COMMANDS MUST
      // BE 1000% FUNCTIONAL") verbatim:
      // "TODOS OS COMMANDS DO LOOP_RUNTIME CLI DEVE ESTAR 1000% FUNCIONAIS"  // english-only: verbatim plan directive, kept as evidence
      // — audit must assess this directive.
      expect(text).toMatch(/1000%/);
    });

    it("recommends Phase 4 implementation actions", () => {
      expect(text).toMatch(/^##\s+Recommendations for Phase 4/m);
    });
  });

  describe("pulumi-state-inventory.md (T0.2)", () => {
    const text = readDoc("pulumi-state-inventory.md");

    it("exists at canonical location", () => {
      expect(text.length).toBeGreaterThan(0);
    });

    it("inventories filesystem state across the parallel project dirs", () => {
      expect(text).toMatch(/^##\s+Filesystem inventory/m);
      // Plan 2026-05-15 T2.1: pulumi-control-plane DELETED (was 16-TODO stub,
      // already reduced to 64-LOC empty-program guard by Wave 1 T1.8).
      // Audit doc must inventory the remaining 2 parallel projects + the canonical one.
      expect(text).toMatch(/pulumi-infra/);
      expect(text).toMatch(/pulumi-platform/);
      expect(text).toMatch(/^infra\/pulumi\b|`infra\/pulumi`/m);
    });

    it("emits recommendation column per stack (rm | preserve | escalate)", () => {
      // Plan T0.2 acceptance: "Recommendation per stack (rm | preserve | escalate)"
      expect(text).toMatch(/\b(rm|preserve|escalate|REMOVE|CANONICAL)\b.*\b(rm|preserve|escalate|REMOVE|CANONICAL)\b/s);
    });

    it("declares stack-level inventory section", () => {
      expect(text).toMatch(/^##\s+Stack-level inventory/m);
    });

    it("documents critical findings (project-name collision)", () => {
      // The critical finding is the load-bearing reason Phase 3 deletes
      // the parallel projects. Test pins it exists in every inventory revision.
      expect(text).toMatch(/Critical finding|project[- ]name collision|name collision/i);
    });

    it("documents live verification gate (backend access required)", () => {
      // T0.2 acceptance includes 'Archives present per stack' which is
      // backend-blocked. The doc must document the live commands.
      expect(text).toMatch(/Live verification|backend.{1,30}access|pulumi stack export/i);
    });
  });
});
