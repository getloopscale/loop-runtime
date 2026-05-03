// cell-2-activation-plan T0.2 / H14 — invariants for k3d-cross-cluster-heal.sh
// H14 block. Two modes:
//
//   Mode A (refresh) — CR `cell-2` (spec.clusterName=loopzero-runtime-2) exists
//     but disconnected because SA UID rotated in runtime-2. H14 mints fresh
//     SA token via `kubectl --context k3d-loopzero-runtime-2 create token default`,
//     rebuilds kubeconfig YAML preserving CA + apiServer, patches the
//     cell-2-kubeconfig Secret. Mirrors H13 pattern for cell-default.
//
//   Mode B (bootstrap) — CR doesn't exist. H14 generates scoped kubeconfig
//     with cluster_name=runtime-2 (matches k3d-loopzero-runtime-2-server-0 docker
//     container per B82) and invokes clustercredential-create.sh cell-2
//     runtime-2 <scoped> with INGRESS_DOMAIN=loopzero.local (ADR C7).
//
// Test catches drift like 'someone tidied the script back to runtime-2 → workload-2'
// or 'switched detection to CR name' or 'forgot the SA-token-refresh path'.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const REPO_ROOT = resolve(__dirname, "..", "..", "..", "..");
const SCRIPT = resolve(REPO_ROOT, "infra", "scripts", "k3d-cross-cluster-heal.sh");

describe("k3d-cross-cluster-heal.sh H14 invariants (matrix #155)", () => {
  const text = readFileSync(SCRIPT, "utf-8");
  // Extract from H14 impl 'log "H14:' line until the next '# ----' section
  // divider. The header docstring also contains "H14 —" but we want impl.
  const implMatch = text.match(/log "H14: ensuring[\s\S]+?(?=\n# ---|$)/);
  const h14Block = implMatch?.[0] ?? "";

  it("script exists and contains H14 block", () => {
    expect(text).toMatch(/^log "H14: ensuring ClusterCredential cell-2/m);
  });

  it("H14 documents matrix entry 155 in header comment", () => {
    expect(text).toMatch(/H14 —[\s\S]{0,800}matrix entry 155/);
  });

  it("detection queries by spec.cellID (NOT CR name) — MEET-053 B9", () => {
    // Catches drift back to: kubectl get clustercredential cell-2 -o ...
    expect(h14Block).toMatch(/items\[\?\(@\.spec\.cellID==["']cell-2["']\)\]/);
  });

  // ----- Mode A (SA token refresh) invariants -----

  it("Mode A: detects existing CR + disconnected branch", () => {
    expect(h14Block).toMatch(/CELL2_CR_EXISTS/);
    expect(h14Block).toMatch(/refreshing SA token/);
  });

  it("Mode A: probes RT2_CTX before minting token", () => {
    expect(h14Block).toMatch(/kubectl --context "\$RT2_CTX"[\s\S]{0,200}cluster-info/);
  });

  it("Mode A: mints fresh 24h SA token in runtime-2 (kube-system/default)", () => {
    expect(h14Block).toMatch(/kubectl --context "\$RT2_CTX" -n kube-system create token default --duration=24h/);
  });

  it("Mode A: preserves CA from existing Secret (no regen)", () => {
    expect(h14Block).toMatch(/secret cell-2-kubeconfig[\s\S]{0,200}certificate-authority-data/);
  });

  it("Mode A: reads apiServer from existing CR spec (preserves docker bridge IP)", () => {
    expect(h14Block).toMatch(/clustercredential cell-2[\s\S]{0,200}spec\.apiServer/);
  });

  it("Mode A: patches Secret data.kubeconfig (NOT recreates)", () => {
    expect(h14Block).toMatch(/patch secret cell-2-kubeconfig[\s\S]{0,200}--type=merge/);
  });

  it("Mode A: builds in-cluster kubeconfig YAML referencing loopzero-runtime-2 context", () => {
    expect(h14Block).toMatch(/name:\s*loopzero-runtime-2/);
  });

  // ----- Mode B (bootstrap) invariants -----

  it("Mode B: bootstrap branch when CR absent", () => {
    expect(h14Block).toMatch(/cell-2 CR absent — bootstrapping/);
  });

  it("Mode B: cluster-name argument is 'runtime-2' (NOT 'workload-2')", () => {
    // The cc-create call: clustercredential-create.sh cell-2 <name> <kc>.
    // Must be 'runtime-2' (matches k3d-loopzero-runtime-2-server-0 container).
    expect(h14Block).toMatch(/cell-2\s+runtime-2\b/);
    // Defensive: workload-2 form MUST NOT appear inside H14.
    expect(h14Block).not.toMatch(/cc-create.*cell-2\s+workload-2\b/);
  });

  it("Mode B: scoped-kubeconfig.sh first arg is 'runtime-2' (cluster id)", () => {
    expect(h14Block).toMatch(/SCOPED_LIB_H14.*\bruntime-2\b/);
  });

  it("Mode B: applies INGRESS_DOMAIN=loopzero.local for ADR C7 (shared domain)", () => {
    expect(h14Block).toMatch(/INGRESS_DOMAIN="loopzero\.local".*cell-2 runtime-2/);
  });

  it("Mode B: uses trap EXIT for temp file cleanup (NOT trap RETURN)", () => {
    // H14 is top-level so EXIT is required for cleanup to actually run.
    expect(h14Block).toMatch(/trap\s+'rm\s+-f\s+"\$H14_TMP"'\s+EXIT/);
    expect(h14Block).not.toMatch(/trap\s+["'][^"']+["']\s+RETURN/);
  });

  it("Mode B: k3d kubeconfig source is loopzero-runtime-2 (the real cluster)", () => {
    expect(h14Block).toMatch(/k3d\s+kubeconfig\s+get\s+loopzero-runtime-2/);
  });

  it("Mode B: references CC_LIB_H14 and SCOPED_LIB_H14 to lib helpers", () => {
    expect(text).toMatch(/CC_LIB_H14="[^"]*lib\/clustercredential-create\.sh"/);
    expect(text).toMatch(/SCOPED_LIB_H14="[^"]*lib\/scoped-kubeconfig\.sh"/);
  });

  it("Mode B: SKIP path warns + cites matrix entry 155 fix path (b)", () => {
    expect(h14Block).toMatch(/matrix entry 155 fix path \(b\)/);
  });

  // ----- Common invariants (apply to both modes) -----

  it("idempotent skip when cell-2 already connected (no-op fast path)", () => {
    expect(h14Block).toMatch(/CELL2_CONNECTED.*==.*"true"[\s\S]{0,200}skip/);
  });

  it("preflight loop includes RT2_CTX (4-context probe)", () => {
    expect(text).toMatch(/for ctx in .*\$RT2_CTX/);
  });

  it("RT2_CTX defaults to k3d-loopzero-runtime-2", () => {
    expect(text).toMatch(/RT2_CTX="\$\{RT2_CTX:-k3d-loopzero-runtime-2\}"/);
  });

  it("H1 attach loop includes runtime-2 containers", () => {
    expect(text).toMatch(/k3d-loopzero-runtime-2-server-0/);
    expect(text).toMatch(/k3d-loopzero-runtime-2-agent-0/);
    expect(text).toMatch(/k3d-loopzero-runtime-2-serverlb/);
  });

  it("final log line reports H1-H15 (NOT 9 hacks) — incrementing counter", () => {
    expect(text).toMatch(/H1-H15 codified/);
  });

  it("does NOT reference loopzero-workload-2 anywhere (drift guard)", () => {
    expect(text).not.toMatch(/workload-2/);
  });
});
