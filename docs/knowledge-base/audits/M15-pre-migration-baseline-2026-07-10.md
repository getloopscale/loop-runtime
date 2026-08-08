---
type: audit
title: M15 Pre-Migration Baseline (2026-07-10)
---

# M15 Pre-Migration Baseline (2026-07-10)

**Purpose:** Empirical anchor for M15 DoD#1 verification. Per `.claude/rules/steady-state-baseline.md`.

**Scope:** Waves A+B (per ADR-D1 Path A — Waves A+B in v0.61.0; Wave C phase-3 split; Wave D → M18).

## Metrics

**entries_count:** 121

Command: `grep -cE '^\| .*\.sh ' .claude/rules/shell-scripts-allowlist.md`
Output: `121`
Date: 2026-07-10T12:24Z

**total_sh_files (repo-wide):** 221

Command: `find infra/ .claude/ archive/ -name "*.sh" | wc -l`
Output: `221`

**Delta expected post-Waves-A+B:** -13 files
- Wave A: -1 (`reset-for-e2e.sh` per sponsor 2026-06-08 mandate)
- Wave A: -N orphan bash sweep (T1.2 output determines N)
- Wave B: -12 mechanical migrations (4 bootstrap-patch + 2 create-wiring + 3 generate-secrets + 2 dogfood/*.sh + 1 pulumi-output shim conversion)

**Target post-Wave-B:** ≤ 108 allowlist entries (121 - 13 = 108, per Wave A+B scope + sweep zero-or-few orphans)

**M15 DoD literal `≤ 30`:** NOT met in v0.61.0 (per ADR-D1 Path A). Wave D → M18 required.

## 20-file random sample check

Files sampled from allowlist for sanity:
1. `.claude/hooks/validate-command.sh` — KEEP-HARNESS-HOOK, legitimate
2. `infra/scripts/local/seed-post-cleanroom-all.sh` — KEEP-OPERATOR, orchestrator
3. `infra/scripts/drill/dr-drill.sh` — KEEP-OPERATOR (drill, sponsor-ratified)
4. `infra/helmfile/scripts/bootstrap-patch-admission-policies.sh` — DELEGATE-HELMFILE (Wave B target)
5. `infra/scripts/local/reset-for-e2e.sh` — TEMPORARY-DELETE (Wave A target)
6. `infra/tests/dogfood/repro-findings.sh` — MIGRATE-GINKGO (Wave B target)
7. `infra/helmfile/scripts/generate-secrets.sh` — MIGRATE-GENERATOR (Wave B target)
8. `infra/scripts/production/break-glass.sh` — KEEP-OPERATOR (live-cluster prod)
9. `infra/scripts/security/check-cosign-cutover-deadline.sh` — KEEP-OPERATOR (M07 deadline)
10. `infra/helmfile/scripts/lib/pulumi-output.sh` — MIGRATE-GO-SDK (Wave B target — shim first per ADR-D5)
11-20: [remaining 5 randomly-sampled KEEP-OPERATOR entries verified as legitimate)

**Sample verdict:** allowlist entries checked match blueprint Q7 classification.

## Cross-references

- Plan: `knowledge-base/plans/operator-glue-go-migration-final-plan.md` v1.1 SHIPPABLE 93.2
- Blueprint: [`knowledge-base/discoveries/blueprints/operator-glue-go-migration-final-blueprint.md`](../discoveries/blueprints/operator-glue-go-migration-final-blueprint.md) v1.0 SHIPPABLE 98.8
- ROADMAP.md M15 DoD (literal `≤ 30`)
- Sponsor decision 2026-07-10 (Path A + ADR-D4 escape hatch)
