// CHG-2026-04-30-INFRA-NORMALIZE / T5.3b (T5.1 superseded path)
//
// T5.1 was [N/A]-superseded (iter 4). The implementation went via
// CC-bootstrap-lib (commit f7c6dd0a):
//   - infra/helmfile/scripts/wire-clusters.sh (orchestrator)
//   - infra/helmfile/scripts/lib/clustercredential-create.sh (per-cell)
//   - infra/helmfile/scripts/lib/scoped-kubeconfig.sh (least-privilege)
//   - infra/helmfile/scripts/lib/argocd-register.sh (ArgoCD cluster Secret)
//
// Iter 23 found that [N/A]-superseded marks shipped without tests on
// the successor. Iter 24 closes T5.1's gap (T5.5 already covered by
// admin-tenants-pin Go + integration tests).
//
// This test pins:
//   - wire-clusters.sh exists + executable + invokes the 3 lib scripts
//   - clustercredential-create.sh exists + has documented usage/exit codes
//   - cell-id validation regex matches ClusterCredential CRD pattern
//   - audit Gap 2 fix is wired (wire-clusters.sh calls cc-create per cell)

import { existsSync, readFileSync, statSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const REPO_ROOT = resolve(__dirname, "..", "..", "..", "..");
const SCRIPTS_DIR = resolve(REPO_ROOT, "infra", "helmfile", "scripts");
const LIB_DIR = resolve(SCRIPTS_DIR, "lib");

function isExecutable(path: string): boolean {
  if (!existsSync(path)) return false;
  // mode bits 0o111 = any execute bit
  return (statSync(path).mode & 0o111) !== 0;
}

describe("CC-bootstrap-lib (T5.1 superseded path) — T5.3b", () => {
  describe("wire-clusters.sh orchestrator", () => {
    const path = resolve(SCRIPTS_DIR, "wire-clusters.sh");
    const text = existsSync(path) ? readFileSync(path, "utf-8") : "";

    it("exists at canonical location", () => {
      expect(existsSync(path), "wire-clusters.sh must exist").toBe(true);
    });

    it("is executable", () => {
      expect(isExecutable(path), "wire-clusters.sh must have execute bit").toBe(true);
    });

    it("invokes clustercredential-create.sh (audit Gap 2 fix)", () => {
      // Plan T5.3b + audit Gap 2: 'wire-clusters.sh must create a
      // ClusterCredential resource alongside the ArgoCD registration'.
      expect(text).toMatch(/clustercredential-create\.sh/);
    });

    it("invokes argocd-register.sh (cluster Secret on mgmt)", () => {
      expect(text).toMatch(/argocd-register\.sh/);
    });

    it("invokes scoped-kubeconfig.sh (least-privilege SA-backed kubeconfig)", () => {
      expect(text).toMatch(/scoped-kubeconfig\.sh/);
    });

    it("references both runtime + runtime-2 wiring (multi-cell)", () => {
      // Cells canonical: runtime → cell-1, runtime-2 → cell-2 per
      // helmfile-cell-metadata.test.ts invariants. Wire-clusters must
      // touch both clusters for multi-cell to exist.
      expect(text).toMatch(/runtime[-_]?2|cell-2/);
    });
  });

  describe("clustercredential-create.sh lib", () => {
    const path = resolve(LIB_DIR, "clustercredential-create.sh");
    const text = existsSync(path) ? readFileSync(path, "utf-8") : "";

    it("exists at canonical location", () => {
      expect(existsSync(path), "clustercredential-create.sh must exist").toBe(true);
    });

    it("is executable", () => {
      expect(isExecutable(path)).toBe(true);
    });

    it("declares 3 positional args (cell-id, cluster-name, kubeconfig-path)", () => {
      expect(text).toMatch(/\$\{?1\b/);
      expect(text).toMatch(/\$\{?2\b/);
      expect(text).toMatch(/\$\{?3\b/);
    });

    it("validates cell-id format ^cell-[a-z0-9-]+$ (matches CRD)", () => {
      // The CRD validation pattern lives in operators/api/v1; the script
      // must enforce the same regex client-side.
      expect(text).toMatch(/cell-\[a-z0-9-?\]/);
    });

    it("documents exit codes 0-4 in header", () => {
      // Plan exit code contract: 0 success, 1 usage, 2 kubectl/mgmt,
      // 3 kubeconfig missing, 4 apply failed.
      for (const code of ["0", "1", "2", "3", "4"]) {
        expect(text, `exit code ${code} must be documented`).toMatch(
          new RegExp(`^#\\s*${code}\\s+[-—]`, "m"),
        );
      }
    });

    it("declares idempotent semantics in header", () => {
      // Plan: 'kubectl apply default semantics; SAFE_PREFIXES in
      // decision.file' — script header asserts idempotency contract.
      expect(text).toMatch(/idempotent/i);
    });

    it("creates kubeconfig Secret on mgmt cluster", () => {
      // Per audit Gap 2 fix: must produce both Secret (kubeconfig data)
      // and ClusterCredential CR.
      expect(text).toMatch(/kind:\s*Secret|kubectl\s+.*\bsecret\b/i);
    });

    it("creates ClusterCredential CR", () => {
      expect(text).toMatch(/ClusterCredential|clustercredential/i);
    });

    it("references K8sCellStore observation latency (60s)", () => {
      // Header contract: 'will be observed by K8sCellStore within 60s'.
      // This load-bearing claim is what makes the wiring async-safe.
      expect(text).toMatch(/60\s*s|K8sCellStore|60\s*seconds/i);
    });
  });

  describe("scoped-kubeconfig.sh sibling", () => {
    const path = resolve(LIB_DIR, "scoped-kubeconfig.sh");

    it("exists at canonical location", () => {
      expect(existsSync(path)).toBe(true);
    });

    it("is executable", () => {
      expect(isExecutable(path)).toBe(true);
    });
  });

  describe("argocd-register.sh sibling", () => {
    const path = resolve(LIB_DIR, "argocd-register.sh");

    it("exists at canonical location", () => {
      expect(existsSync(path)).toBe(true);
    });

    it("is executable", () => {
      expect(isExecutable(path)).toBe(true);
    });
  });
});
