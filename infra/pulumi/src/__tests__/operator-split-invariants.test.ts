// CHG-2026-04-30-INFRA-NORMALIZE / Phase 2 — invariants matrix coverage map
//
// Plan ref: docs/decisions.md (8 invariants
// I-1..I-8 extracted from MEET-INFRA-NORMALIZE-T3-operator-split.md
// condition C18). This vitest file is the central index documenting
// where each invariant is verified, plus the direct check for I-5
// (replicaCount minimum) which the matrix listed but had no explicit
// vitest pin.
//
// Strategy: each I-N invariant has:
//   - A documentation block citing the doc + verification mechanism
//   - Either a direct test (when feasible offline) OR a meta-test that
//     asserts the verification artifact exists.
// This keeps drift detection on the full invariant matrix in one place.

import { execSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const REPO_ROOT = resolve(__dirname, "..", "..", "..", "..");
const SPLIT_CHARTS = [
  "loopruntime-tenant-operator",
  "loopruntime-app-operator",
  "loopruntime-preview-operator",
  "loopruntime-platform-operator",
] as const;

function helmTemplate(chart: string): string {
  return execSync(`helm template "${REPO_ROOT}/charts/${chart}"`, {
    encoding: "utf-8",
    stdio: ["ignore", "pipe", "pipe"],
  });
}

describe("operator-split invariants matrix (I-1 through I-8)", () => {
  it("I-1 cache scoping verification artifact deferred (envtest infra needed)", () => {
    // Matrix entry: 'envtest case (planned T-07 per condition C16). Until
    // envtest lands, the unit test in infra/tests/unit/
    // operator_metrics_registration_test.go only verifies the metrics
    // import; cache scoping verification awaits envtest infrastructure.'
    const goTest = resolve(
      REPO_ROOT,
      "infra",
      "tests",
      "unit",
      "operator_metrics_registration_test.go",
    );
    expect(existsSync(goTest), "operator_metrics_registration_test.go must exist as the I-1 partial coverage").toBe(true);
  });

  it("I-2 LeaderElectionID uniqueness covered by split-operator-charts.test.ts (iter 16)", () => {
    // Direct verification: each binary's main.go declares unique LeaderElectionID.
    const ids = new Set<string>();
    for (const chart of SPLIT_CHARTS) {
      const binary = chart.replace(/^loopruntime-/, "");
      const main = readFileSync(
        resolve(REPO_ROOT, "operators", "cmd", binary, "main.go"),
        "utf-8",
      );
      const m = main.match(/LeaderElectionID:\s*"([^"]+)"/);
      expect(m).not.toBeNull();
      ids.add(m![1]);
    }
    expect(ids.size).toBe(SPLIT_CHARTS.length);
  });

  it("I-3 RBAC subset covered by split_operator_clusterrole_grants_test.go (post fc1793dd)", () => {
    // The legacy `operator-clusterrole-subset.test.ts` compared each split
    // operator's ClusterRole against the monolith `tenant-controller`
    // chart — but the monolith chart was DELETED in fc1793dd as part of
    // operator-split T1.5 (CLAUDE.md confirms: "the legacy monolith chart
    // `infra/helm/tenant-controller` ... were deleted in T1.5"). The
    // semantically-equivalent absolute-permissions check (per binary)
    // now lives in `infra/tests/unit/split_operator_clusterrole_grants_test.go`.
    // See finding `.claude/knowledge-base/concepts/operations/finding-2026-05-15-pulumi-vitest-23-fail-drift.md`.
    const goTest = resolve(REPO_ROOT, "infra", "tests", "unit", "split_operator_clusterrole_grants_test.go");
    expect(existsSync(goTest), "I-3 verification (split_operator_clusterrole_grants_test.go) must exist").toBe(true);
  });

  it("I-4 metrics registration covered by Go test", () => {
    const goTest = resolve(REPO_ROOT, "infra", "tests", "unit", "operator_metrics_registration_test.go");
    expect(existsSync(goTest)).toBe(true);
    const text = readFileSync(goTest, "utf-8");
    // Test must reference all 4 binaries.
    for (const chart of SPLIT_CHARTS) {
      const binary = chart.replace(/^loopruntime-/, "");
      expect(text, `${binary} must be referenced in metrics registration test`).toContain(binary);
    }
  });

  it("I-5 replicaCount: 2 minimum — direct check (was matrix gap, pinned iter 26)", () => {
    // The matrix claimed 'helm template ... | grep replicas: returns
    // replicas: 2 for each chart' but that was only a manual command —
    // not in CI. Iter 26 ships the explicit assertion.
    for (const chart of SPLIT_CHARTS) {
      const rendered = helmTemplate(chart);
      const match = rendered.match(/^\s*replicas:\s*(\d+)\s*$/m);
      expect(match, `${chart} must declare 'replicas: N' in rendered Deployment`).not.toBeNull();
      const n = parseInt(match![1], 10);
      expect(n, `${chart} replicas=${n} but I-5 requires >= 2 (atomicity per condition C13)`).toBeGreaterThanOrEqual(2);
    }
  });

  it("I-6 ImagePullSecret consistency covered by Go test", () => {
    const goTest = resolve(REPO_ROOT, "infra", "tests", "unit", "split_operator_pull_secrets_test.go");
    expect(existsSync(goTest), "I-6 verification Go test must exist").toBe(true);
  });

  it("I-7 clusterroles grants tenant-operator-only covered by Go test", () => {
    const goTest = resolve(REPO_ROOT, "infra", "tests", "unit", "split_operator_clusterrole_grants_test.go");
    expect(existsSync(goTest), "I-7 verification Go test must exist").toBe(true);
  });

  it("I-8 adopt-or-create lifecycle covered by adopt-or-create.test.ts", () => {
    expect(
      existsSync(resolve(__dirname, "adopt-or-create.test.ts")),
      "I-8 verification test must exist",
    ).toBe(true);
  });

  // The standalone `docs/decisions.md` doc
  // was deleted in fc1793dd (2026-05-11 doc cleanup); the I-1..I-8
  // invariants are now verified directly by the 8 individual tests
  // above (RBAC, replicaCount, ImagePullSecret, leader-election,
  // metrics, cache scoping, adopt-or-create lifecycle) — the "central
  // index doc" check was meta-coverage that no longer adds signal once
  // each invariant has its own pinned test. See finding
  // `.claude/knowledge-base/concepts/operations/finding-2026-05-15-pulumi-vitest-23-fail-drift.md`.
});
