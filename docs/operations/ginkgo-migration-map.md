---
type: operational-runbook
title: Ginkgo Migration Map (Phase 3 T3.1)
---

# Ginkgo Migration Map (Phase 3 T3.1)

> Autonomous prep audit for [`docs/plans/completed/2026-05-22-script-cleanup-plan.md`](../plans/completed/2026-05-22-script-cleanup-plan.md) Phase 3.
> Maps each `MIGRATE-GINKGO`-classified script to an existing-or-new Ginkgo
> `Describe`/`It`, identifies the CI workflow that invokes it, and flags
> LIVE-only scenarios. Read-only — the LIVE conversion (T3.2) is an OPERATOR
> HARD STOP (needs a real multi-cell / drill stack).

**Generated:** 2026-05-22 · **Source:** `docs/scripts-audit/2026-05-22.csv` (disposition=MIGRATE-GINKGO, 36 scripts)

## CI invocation reality (the load-bearing finding)

Only **4 workflows** touch parity/dogfood scripts, and they invoke a SMALL subset:

| Workflow | Invokes |
|---|---|
| `multi-cell.yml` | `cell2-canary-stage1.sh`, `cell2-canary-stage2.sh`, `parity/preflight.sh` |
| `drill-prod-identical-soak.yml` | `cell2-canary-stage1.sh`, `cell2-canary-stage2.sh` |
| `parity-validation.yml` | `parity/preflight.sh` |
| `dogfood-slo-gate.yml` | (none directly — already Ginkgo-driven) |

**So of the 36 scripts, only ~4 are CI-invoked.** The other ~32 are invoked by
OTHER scripts (drill orchestrators, the cleanroom loop), run manually by an
operator, or are orphaned. caller_count from the CSV separates them.

## Migration buckets

### Bucket A — CI-invoked LIVE multi-cell (HARD STOP, real Ginkgo target needed)

| Script | caller_count | Ginkgo target | LIVE infra needed |
|---|---:|---|---|
| `cell2-canary-stage1.sh` | 3 | NEW `Describe("cell2 canary stage1", Label("multicell"))` | multi-cell (cell-2 active) |
| `cell2-canary-stage2.sh` | 4 | NEW `Describe("cell2 canary stage2", Label("multicell"))` | multi-cell + 24h soak window |
| `cell2-rollback-slo.sh` | 1 | NEW `It("rollback SLO P95 < 705ms")` under multicell Describe | multi-cell |
| `cell2-activation-gates.sh` + 13 `lib/cell2-gate-c*.sh` | 2 (+1 each) | ONE table-driven `Describe("cell2 activation gates", func(){ DescribeTable(C1..C12+postflip) })` — **14 scripts collapse to 1 spec file** | multi-cell |
| `parity/preflight.sh` | 23 | extend existing `BeforeSuite` preflight | any |

The 13 `cell2-gate-c*.sh` libs are dynamically sourced by `cell2-activation-gates.sh`
(`source "$LIB_DIR/cell2-gate-${g}.sh"`, line 76) — the T0.1 detector now credits
each with caller_count=1. They are NOT orphans; they collapse into a single
`DescribeTable`. This is the biggest single reduction available: **14 → 1**.

### Bucket B — install/parity matrices (LIVE, provider-specific)

| Script | caller_count | Notes |
|---|---:|---|
| `doks-install.sh` | 3 | DOKS provider install parity — LIVE cloud |
| `droplet-install.sh` / `droplet-cloud-init.sh` | 3 / 1 | Droplet provider parity — LIVE cloud |
| `k3d-virgin-install.sh` | 6 | local virgin install — could be a `Label("install")` spec |
| `validate-matrix.sh` | 2 | the master parity matrix runner — orchestrates the others |
| `bootstrap-stage2-tenants.sh` | 1 | stage-2 tenant seeding for soak |

These validate install-parity across providers — Ginkgo equivalents need the
respective LIVE stack. NOT autonomous.

### Bucket C — audit/drift checks (candidate: may already be covered by e2e journey)

| Script | caller_count | Recommendation |
|---|---:|---|
| `audit-golden-test.sh` | 4 | check vs existing `Label("journey")` golden assertions before porting |
| `audit-regression-matrix.sh` | 4 | likely overlaps journey regression specs |
| `audit-argocd-drift.sh` | 0 | **EC-1: grep-before-delete** — zero callers; confirm manual-only vs orphan |
| `audit-stale-loopzeroapps.sh` | 0 | **EC-1: grep-before-delete** — zero callers |
| `run-adr-soaks-bg.sh` | 0 | **EC-1: grep-before-delete** — background soak runner; may be orphaned |

### Bucket D — dogfood orchestrators (LIVE; some are the dogfood loop itself)

| Script | caller_count | Notes |
|---|---:|---|
| `dogfood/preflight.sh` | 23 | the dogfood pre-flight — heavily referenced; KEEP or careful extend of BeforeSuite |
| `dogfood/full-loop.sh` | 6 | the dogfood journey loop — overlaps e2e journey suite (106 specs) |
| `dogfood/cli-coverage.sh` | 7 | CLI-by-CLI coverage — overlaps journey |
| ~~`dogfood/repro-findings.sh`~~ | ~~1~~ | ~~finding-repro harness~~ — MIGRATED to Ginkgo at `infra/tests/e2e/dogfood_repro_test.go` (M18 T2.1, v0.62.0 2026-07-10) |
| ~~`dogfood/scripts/cleanup-terminating-namespaces.sh`~~ | 0 | MIGRATED — folded as `cleanupTerminatingNamespaces` helper in `dogfood_repro_test.go` (M18 T2.1) |
| `dogfood/scripts/cleanup-*.sh` (remaining) | 0-1 | remaining cleanup helpers — **EC-1 grep-before-delete** |
| `dogfood/setup-monorepo-turbo.sh` | 0 | **EC-1 grep-before-delete** — fixture setup, may be orphan |

## Existing Ginkgo coverage

The e2e suite has `Label("journey")` (17 Describe sites, ~106 specs) + `Label("edge-case")`
(7 sites). It already covers deploy / rollback / scale / secrets / promote.
It does **NOT** cover: cell-2 activation gates (C1-C12), canary SLO, rollback
SLO, multi-provider install parity — those are the genuine NEW Ginkgo work.

## Conclusions

1. **The single biggest win is the cell2-gate collapse: 14 scripts → 1 table-driven
   spec.** That alone removes 14 of the 36. Requires multi-cell LIVE to validate.
2. **~7 scripts are EC-1 grep-before-delete candidates** (caller_count=0): confirm
   they are not invoked by any Makefile/go:generate/cron/other-script before deleting.
   Some may be deletable WITHOUT a Ginkgo replacement (orphaned).
3. **Bucket A + B (install/canary/SLO) need LIVE multi-cell/drill** — NOT autonomous.
4. **Bucket C/D overlap the existing journey suite** — port only the delta; some
   are deletable after confirming journey covers them.
5. **Honest Phase 3 reduction estimate: up to ~30 scripts** (14 cell2-gate collapse
   + ~7 EC-1 orphans + ~9 journey-overlap deletes), BUT every delete is LIVE-gated
   (multi-cell verdict parity per D4) or EC-1-gated (grep proof). None autonomous.

## Operator next steps (LIVE, T3.2)

1. Write the table-driven `cell2 activation gates` Ginkgo Describe (C1-C12 +
   postflip), validate verdicts == bash on a LIVE multi-cell stack, then delete
   `cell2-activation-gates.sh` + 13 libs together (D3 + EC-1).
2. Repoint `multi-cell.yml` + `drill-prod-identical-soak.yml` to the Ginkgo
   `Label("multicell")` filter; confirm spec count in CI output (EC-2).
3. For each caller_count=0 script: `git grep -F "<basename>"` full-repo (Makefile,
   *.mk, *.go go:generate, cron) — delete if only bookkeeping/self refs (EC-1).
4. Re-validate `/dogfood full` + `/chaos-test --full` after each batch (no regression).
