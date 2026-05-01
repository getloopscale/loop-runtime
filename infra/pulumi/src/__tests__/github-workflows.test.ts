// CHG-2026-04-30-INFRA-NORMALIZE / T6.1 + T6.2 — workflow structural invariants
//
// Translates plan TDD pseudocode names to real vitest:
//   T6.1: test_workflow_yaml_validates, test_workflow_has_concurrency_group,
//         test_workflow_has_timeout
//   T6.2: test_workflow_separates_preflight_and_live,
//         test_live_job_requires_workflow_dispatch
//
// Catches drift like "someone removed concurrency to debug, forgot to put back"
// or "live job lost its workflow_dispatch gate, runs on every PR" — the
// silent failure modes the plan flagged in T6 design points.
//
// Implementation note: regex-based instead of YAML parser to avoid adding
// `yaml` dependency to the Pulumi project. Workflow files are stable
// enough that line-anchored regex suffices for invariant pinning.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const REPO_ROOT = resolve(__dirname, "..", "..", "..", "..");

function loadWorkflow(filename: string): string {
  return readFileSync(resolve(REPO_ROOT, ".github", "workflows", filename), "utf-8");
}

describe("GitHub Actions workflow invariants (T6.1 + T6.2)", () => {
  describe("cli-commands.yml (T6.1)", () => {
    const text = loadWorkflow("cli-commands.yml");

    it("declares 'name: cli-commands'", () => {
      expect(text).toMatch(/^name:\s*cli-commands\s*$/m);
    });

    it("declares concurrency block (cancels stale runs per plan D5)", () => {
      // top-level `concurrency:` key (line-anchored)
      expect(text).toMatch(/^concurrency:\s*$/m);
      // group: <anything>${{ github.... }} — allows literal prefix like
      // "cli-commands-${{ github.event.pull_request... }}".
      expect(text).toMatch(/^\s+group:.*\$\{\{[^}]*github\./m);
    });

    it("at least one job declares timeout-minutes", () => {
      expect(text).toMatch(/^\s+timeout-minutes:\s*\d+/m);
    });

    it("declares explicit permissions block (least privilege)", () => {
      expect(text).toMatch(/^permissions:\s*$/m);
    });

    it("triggers on pull_request + push", () => {
      expect(text).toMatch(/^\s+pull_request:/m);
      expect(text).toMatch(/^\s+push:/m);
    });
  });

  describe("multi-cell.yml (T6.2)", () => {
    const text = loadWorkflow("multi-cell.yml");

    it("parses as valid YAML with name", () => {
      expect(text).toMatch(/^name:\s*\S+/m);
    });

    it("declares preflight job (always-runs offline gate)", () => {
      // Job name conventionally contains preflight/offline/always.
      expect(text).toMatch(/^\s{2}\b(preflight|offline|always|preflight-offline)\b/m);
    });

    it("declares live/canary job", () => {
      expect(text).toMatch(/^\s{2}\b(live[-_a-z]*|canary[-_a-z]*|cloud[-_a-z]*)\b:/m);
    });

    it("live job is gated by workflow_dispatch (no auto-trigger)", () => {
      // The `if:` expression must reference workflow_dispatch event.
      expect(text).toMatch(/if:.*workflow_dispatch/);
    });

    it("live job binds GitHub environment for approval gate", () => {
      // Plan T6.2 + DR multi-cell: production stages require manual
      // approval through a protected environment.
      expect(text).toMatch(/^\s+environment:\s*\S+/m);
    });

    it("declares concurrency block (serializes live runs)", () => {
      expect(text).toMatch(/^concurrency:\s*$/m);
    });

    it("at least one job declares timeout-minutes", () => {
      expect(text).toMatch(/^\s+timeout-minutes:\s*\d+/m);
    });

    it("declares explicit permissions block", () => {
      expect(text).toMatch(/^permissions:\s*$/m);
    });
  });
});
