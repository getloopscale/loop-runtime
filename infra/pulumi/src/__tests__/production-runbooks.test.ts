// CHG-2026-04-30 — production runbook structural invariants
//
// Two production-side runbooks were shipped offline (T7.1+T7.2 deliverables)
// but have no test pinning structure. If a future edit drops the rollback
// procedure or removes a stage section, on-call would discover the gap
// only DURING an incident — too late.
//
// Pins:
//   - multi-cell-canary-prod.md: pre-conditions, 4 stage sections (4/5a/5b/5c),
//     incident-rollback, observability dashboards, prometheus alerts gate
//   - multi-cell-rca-template.md (C17 mandatory): all 9 sections required by
//     governance review (header, timeline, root cause, dev-canary-gap-why,
//     action items, rollback verification, customer impact, related artifacts,
//     sign-off)

import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const REPO_ROOT = resolve(__dirname, "..", "..", "..", "..");
const RUNBOOK_DIR = resolve(REPO_ROOT, "docs", "runbooks");

function readDoc(name: string): string {
  const p = resolve(RUNBOOK_DIR, name);
  return existsSync(p) ? readFileSync(p, "utf-8") : "";
}

describe("multi-cell-canary-prod.md (T7.1+T7.2)", () => {
  const text = readDoc("multi-cell-canary-prod.md");

  it("exists at canonical location", () => {
    expect(text.length).toBeGreaterThan(0);
  });

  it("declares pre-conditions section", () => {
    expect(text).toMatch(/^##\s+Pre-conditions/m);
  });

  it("declares Stage 4 (T7.1) — 1 customer, 7-day hold", () => {
    expect(text).toMatch(/^##\s+Stage\s+4\s+\(T7\.1\).*1\s+customer.*7-day/m);
  });

  it("declares all 3 production rollout stages 5a/5b/5c", () => {
    expect(text).toMatch(/^##\s+Stage\s+5a/m);
    expect(text).toMatch(/^##\s+Stage\s+5b/m);
    expect(text).toMatch(/^##\s+Stage\s+5c/m);
  });

  it("documents Incident → Rollback procedure", () => {
    expect(text).toMatch(/^##\s+Incident.*Rollback/m);
  });

  it("documents observability dashboards required", () => {
    expect(text).toMatch(/^##\s+Observability.*required dashboards/m);
  });

  it("documents Prometheus alerts gate (must be live before stage 4)", () => {
    expect(text).toMatch(/##\s+(Prom[ehu]+us|Prometheus)\s+alerts.*before\s+stage\s+4/i);
  });

  it("references loopzero admin tenants pin operator command", () => {
    expect(text).toMatch(/loopzero\s+admin\s+tenants\s+pin/);
  });

  it("references customer comms template", () => {
    expect(text).toMatch(/cell-2-canary-template\.md/);
  });

  it("references RCA template (C17 mandatory)", () => {
    expect(text).toMatch(/multi-cell-rca-template\.md/);
  });
});

describe("multi-cell-rca-template.md (C17 mandatory)", () => {
  const text = readDoc("multi-cell-rca-template.md");

  it("exists at canonical location", () => {
    expect(text.length).toBeGreaterThan(0);
  });

  it("documents Header section (fill on T+15min)", () => {
    expect(text).toMatch(/^##\s+Header.*T\+15min/m);
  });

  it("documents Timeline section (fill chronologically)", () => {
    expect(text).toMatch(/^##\s+Timeline/m);
  });

  it("documents Root cause categorization C17 (pick exactly ONE)", () => {
    // C17 (per MEET-INFRA-NORMALIZE-T3-multi-cell condition C17)
    // requires single-categorization to prevent kitchen-sink RCAs.
    expect(text).toMatch(/^##\s+Root cause categorization.*pick exactly ONE/m);
  });

  it("documents 'Why was this not caught in dev canary' section", () => {
    // Anti-pattern catch: prod incidents that were also reproducible in
    // dev canary indicate stage gating is broken.
    expect(text).toMatch(/^##\s+Why was this not caught in dev canary/m);
  });

  it("documents Action items section (owner + horizon required)", () => {
    expect(text).toMatch(/^##\s+Action items.*owner.*horizon/m);
  });

  it("documents Rollback verification (C17)", () => {
    expect(text).toMatch(/^##\s+Rollback verification/m);
  });

  it("documents Customer impact section", () => {
    expect(text).toMatch(/^##\s+Customer impact/m);
  });

  it("documents Related artifacts cross-link section", () => {
    expect(text).toMatch(/^##\s+Related artifacts/m);
  });

  it("documents Sign-off section", () => {
    expect(text).toMatch(/^##\s+Sign-off/m);
  });

  it("documents Append to RCA index instruction", () => {
    // The template must remind the operator to append the completed
    // RCA into the canonical index (preserves audit trail per C15).
    expect(text).toMatch(/^##\s+Append to RCA index/m);
  });
});

describe("cross-runbook links (T7.x cohesion)", () => {
  const canaryProd = readDoc("multi-cell-canary-prod.md");
  const rca = readDoc("multi-cell-rca-template.md");

  it("canary-prod runbook references RCA template (incident path)", () => {
    expect(canaryProd).toMatch(/multi-cell-rca-template\.md/);
  });

  it("RCA template references canary stage gates (T5.6/T5.7)", () => {
    // The 'Why was this not caught in dev canary' section must reference
    // the stages that should have caught the issue.
    expect(rca).toMatch(/T5\.[67]/);
  });
});
