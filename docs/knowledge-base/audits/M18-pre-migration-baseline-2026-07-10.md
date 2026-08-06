# M18 Pre-Migration Baseline (2026-07-10)

**Purpose:** Anchor for M18 DoD verification. Per `.claude/rules/steady-state-baseline.md`.

## Metrics

**entries_count:** 120

Command: `grep -cE '^\| .*\.sh ' .claude/rules/shell-scripts-allowlist.md`
Output: `120` (post-M15 v0.61.0 release)

**Delta expected post-M18 v0.62.0:** -2 files
- T2.1: repro-findings.sh + cleanup-terminating-namespaces.sh (Ginkgo migration)
- T2.2: pulumi-output.sh reclassify (row stays, Justificativa updated — NOT counted as removed)

**Target post-M18:** 118 allowlist entries

## Cross-references

- Plan: `knowledge-base/plans/m18-operator-glue-remainder-plan.md` v1.0 SHIPPABLE 95.2
- Blueprint: `knowledge-base/discoveries/blueprints/m18-operator-glue-remainder-blueprint.md` v1.0 SHIPPABLE 99.7
- ROADMAP.md M18 (from M15 remainder)
