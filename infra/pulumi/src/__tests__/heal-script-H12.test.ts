// CHG-2026-05-01 / H12 — invariants for k3d-cross-cluster-heal.sh H12 block.
//
// Iter 33+34 added H12 to fix the cell-0 ClusterCredential trap (matrix
// entry 153). Two subtle decisions are load-bearing:
//   (1) Detection queries by spec.cellID, NOT CR name — because
//       K8sCellStore lookup uses spec.cellID per MEET-053 B9.
//   (2) cluster-name=cell-0, NOT runtime — because k3d local cluster is
//       loop-runtime-cell-0; cluster-name=cell-0 would resolve docker container
//       k3d-loop-runtime-cell-0-server-0 which doesn't exist (B82 fallback to
//       0.0.0.0:6444 unreachable).
//
// Test catches drift like 'someone tidied the script back to runtime' or
// 'switched detection to CR name'. Both would silently re-introduce the
// trap captured in matrix entry 153.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const REPO_ROOT = resolve(__dirname, "..", "..", "..", "..");
const SCRIPT = resolve(REPO_ROOT, "infra", "scripts", "k3d-cross-cluster-heal.sh");

describe("k3d-cross-cluster-heal.sh H12 invariants (matrix #153)", () => {
  const text = readFileSync(SCRIPT, "utf-8");
  // Extract from the H12 impl 'log "H12:' line until the next '# ----'
  // section divider OR end of file. The header docstring at top of script
  // (line ~36) ALSO contains "# H12 —" but we want the implementation.
  const implMatch = text.match(/log "H12: ensuring[\s\S]+?(?=\n# ---|$)/);
  const h12Block = implMatch?.[0] ?? "";

  it("script exists and contains H12 block", () => {
    expect(text).toMatch(/^log "H12: ensuring ClusterCredential cell-0/m);
  });

  it("H12 documents matrix entry 153 in header comment", () => {
    expect(text).toMatch(/H12 —[\s\S]{0,300}matrix entry 153/);
  });

  it("detection queries by spec.cellID (NOT CR name) — MEET-053 B9", () => {
    // Catches drift back to: kubectl get clustercredential cell-0 -o ...
    // The right form filters by jsonpath on items[?(@.spec.cellID==...)]
    expect(h12Block).toMatch(/items\[\?\(@\.spec\.cellID==["']cell-0["']\)\]/);
  });

  it("cluster-name argument is 'workload' (NOT 'runtime')", () => {
    // The cc-create call: clustercredential-create.sh cell-0 <name> <kc>
    // Must be 'workload' (matches k3d-loop-runtime-cell-0-server-0 container).
    expect(h12Block).toMatch(/cell-0\s+workload\b/);
    // Defensive: the buggy form (cell-0 runtime) MUST NOT appear inside H12.
    expect(h12Block).not.toMatch(/cc-create.*cell-0\s+runtime\b/);
  });

  it("scoped-kubeconfig.sh argument is 'workload' too", () => {
    // The scoped-kubeconfig.sh first arg becomes CLUSTER_ID and resolves
    // docker container k3d-loop-runtime-${CLUSTER_ID}-server-0.
    expect(h12Block).toMatch(/SCOPED_LIB.*\bworkload\b/);
  });

  it("uses trap EXIT for temp file cleanup (NOT trap RETURN)", () => {
    // Iter 33 bug: trap RETURN only fires inside functions. H12 is
    // top-level, so EXIT is required for the cleanup to actually run.
    expect(h12Block).toMatch(/trap\s+'rm\s+-f\s+"\$H12_TMP"'\s+EXIT/);
    expect(h12Block).not.toMatch(/trap\s+["'][^"']+["']\s+RETURN/);
  });

  it("k3d kubeconfig source is loop-runtime-cell-0 (the real cluster)", () => {
    expect(h12Block).toMatch(/k3d\s+kubeconfig\s+get\s+loop-runtime-cell-0/);
  });

  it("references CC_LIB and SCOPED_LIB to lib helpers", () => {
    // The lib path conventions must remain stable.
    expect(text).toMatch(/CC_LIB="[^"]*lib\/clustercredential-create\.sh"/);
    expect(text).toMatch(/SCOPED_LIB="[^"]*lib\/scoped-kubeconfig\.sh"/);
  });

  it("SKIP path: warns + cites matrix entry 153 fix path (b)", () => {
    // When cc-create fails, the warn message must point operator at the
    // matrix entry for context (per .claude/rules workflow.md fail-clear).
    expect(h12Block).toMatch(/matrix entry 153 fix path \(b\)/);
  });

  it("idempotent skip when cell-0 already connected (no-op fast path)", () => {
    // Detect must short-circuit when CELL1_CONNECTED=true so re-runs are
    // cheap. Catches drift back to unconditional cc-create.
    expect(h12Block).toMatch(/CELL1_CONNECTED.*==.*"true"[\s\S]{0,200}skip/);
  });
});

describe("k3d-cross-cluster-heal.sh H13 invariants (matrix #154)", () => {
  const text = readFileSync(SCRIPT, "utf-8");
  const h13Match = text.match(/log "H13: ensuring[\s\S]+?(?=\n# ---|$)/);
  const h13Block = h13Match?.[0] ?? "";

  it("script contains H13 block", () => {
    expect(text).toMatch(/^log "H13: ensuring/m);
  });

  it("H13 documents matrix entry 154 in header", () => {
    expect(text).toMatch(/H13 —[\s\S]{0,400}matrix entry 154/);
  });

  it("detection queries spec.cellID=='cell-0' (consistency with H12)", () => {
    expect(h13Block).toMatch(/items\[\?\(@\.spec\.cellID==["']cell-0["']\)\]/);
  });

  it("mints fresh SA token via 'kubectl create token loop-runtime-system/default'", () => {
    expect(h13Block).toMatch(/create token default --duration=24h/);
  });

  it("preserves CA from existing Secret (no regen)", () => {
    expect(h13Block).toMatch(/certificate-authority-data.*\$H13_CA/s);
  });

  it("uses in-cluster server URL (kubernetes.default.svc:443)", () => {
    expect(h13Block).toMatch(/https:\/\/kubernetes\.default\.svc:443/);
  });

  it("patches Secret data.kubeconfig (NOT recreates)", () => {
    // Bash line-continuation backslash + newline + spaces between args.
    expect(h13Block).toMatch(/patch secret cell-0-kubeconfig[\s\\]+--type=merge/);
  });

  it("idempotent skip when cell-0 already connected", () => {
    expect(h13Block).toMatch(/CELL_DEFAULT_CONNECTED.*==.*"true"[\s\S]{0,200}skip/);
  });

  it("warn-cites matrix entry 154 on patch failure (fail-clear)", () => {
    expect(h13Block).toMatch(/matrix entry 154/);
  });
});
