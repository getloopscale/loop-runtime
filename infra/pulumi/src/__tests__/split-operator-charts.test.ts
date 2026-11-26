// CHG-2026-04-30-INFRA-NORMALIZE / T2.1 — split operator chart invariants
//
// Pins the security + observability invariants captured in iter 7's offline
// audit (commit d1cd9fcb). Without this test the invariants are checked only
// when someone runs `helm template | grep` manually; this test exercises them
// on every PR.
//
// Plan ref: infra-normalize-multicell-plan T2.1 TDD block:
//   test_each_split_operator_chart_has_resource_limits()
//   test_each_split_operator_chart_has_runAsNonRoot()
//   test_each_split_operator_has_unique_leader_election_id()
// (test_each_split_operator_clusterrole_is_least_privilege deferred —
// requires per-rule diff against monolith baseline; tracked in plan T2.1
// SRE Review veto criterion.)

import { execSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const REPO_ROOT = resolve(__dirname, "..", "..", "..", "..");
const CHARTS = [
  "loop-runtime-tenant-operator",
  "loop-runtime-app-operator",
  "loop-runtime-preview-operator",
  "loop-runtime-platform-operator",
] as const;

function helmTemplate(chart: string): string {
  return execSync(`helm template ${REPO_ROOT}/charts/${chart}`, {
    encoding: "utf-8",
    stdio: ["ignore", "pipe", "pipe"],
  });
}

describe("split operator chart invariants (T2.1)", () => {
  for (const chart of CHARTS) {
    describe(chart, () => {
      const rendered = helmTemplate(chart);

      it("declares runAsNonRoot=true at pod + container level", () => {
        const matches = rendered.match(/runAsNonRoot:\s*true/g) ?? [];
        // Defense-in-depth: both pod and container security contexts.
        expect(matches.length, "runAsNonRoot must appear ≥2 times").toBeGreaterThanOrEqual(2);
      });

      it("declares readOnlyRootFilesystem=true", () => {
        expect(rendered).toMatch(/readOnlyRootFilesystem:\s*true/);
      });

      it("declares livenessProbe + readinessProbe", () => {
        expect(rendered).toMatch(/livenessProbe:/);
        expect(rendered).toMatch(/readinessProbe:/);
      });

      it("declares resource limits", () => {
        // Match either `limits:` block at any indentation; renderer outputs
        // them inside resources: spec.
        expect(rendered).toMatch(/^\s+limits:/m);
      });

      it("enables --leader-elect=true", () => {
        expect(rendered).toMatch(/--leader-elect=true/);
      });
    });
  }

  it("each operator binary declares a UNIQUE LeaderElectionID", () => {
    // The lease names are set programmatically in operators/cmd/*-operator/main.go,
    // not in the chart. Read the source and confirm uniqueness.
    const ids = new Set<string>();
    for (const chart of CHARTS) {
      // chart name → binary dir: loop-runtime-tenant-operator → tenant-operator
      const binary = chart.replace(/^loop-runtime-/, "");
      const main = readFileSync(
        resolve(REPO_ROOT, "operators", "cmd", binary, "main.go"),
        "utf-8",
      );
      const m = main.match(/LeaderElectionID:\s*"([^"]+)"/);
      expect(m, `${binary}/main.go must set LeaderElectionID`).not.toBeNull();
      ids.add(m![1]);
    }
    expect(ids.size, "all 4 LeaderElectionIDs must be unique").toBe(CHARTS.length);
    // Spot-check the canonical runtime.getloopscale.com domain pattern (ADR-0021 §C13).
    for (const id of ids) {
      expect(id, `LeaderElectionID must use runtime.getloopscale.com scope: ${id}`).toMatch(
        /^loop-runtime-[a-z]+-operator\.runtime\.getloopscale\.com$/,
      );
    }
  });

  it("renders without errors via kubeconform-equivalent shape", () => {
    // Sanity check: each chart yields a non-empty manifest with the basic
    // K8s API objects expected from an operator (Deployment, ServiceAccount,
    // ClusterRole, ClusterRoleBinding).
    for (const chart of CHARTS) {
      const rendered = helmTemplate(chart);
      expect(rendered, `${chart} must render Deployment`).toMatch(/^kind:\s*Deployment/m);
      expect(rendered, `${chart} must render ServiceAccount`).toMatch(/^kind:\s*ServiceAccount/m);
      expect(rendered, `${chart} must render ClusterRole`).toMatch(/^kind:\s*ClusterRole/m);
      expect(rendered, `${chart} must render ClusterRoleBinding`).toMatch(
        /^kind:\s*ClusterRoleBinding/m,
      );
    }
  });
});
