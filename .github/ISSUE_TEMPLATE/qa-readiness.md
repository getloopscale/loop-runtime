---
name: QA-readiness milestone closure
about: Records the post-execution closure of a QA-readiness milestone (M7-class — Cleanroom validation + sponsor JUNTOS walkthrough).
title: "QA-readiness YYYY-MM-DD complete"
labels: qa-readiness
assignees: ''
---

<!--
Canonical GH issue template for QA-readiness milestone closure.
Per ADR 2026-06-23-validation-milestone-sponsor-hard-stop.md § Decision Step 4.
Operator runs `gh issue create` from this template AFTER sponsor JUNTOS Step 7 PASS.
-->

## Milestone metadata

- **Milestone:** M7 of `.claude/knowledge-base/plans/qa-readiness-roadmap-2026-06-23.md`
- **Date:** YYYY-MM-DD
- **Operator:** <name>
- **Sponsor present:** <name(s)>
- **Plan:** `.claude/knowledge-base/plans/m7-cleanroom-validation-readiness-plan.md`
- **ADR:** `.claude/knowledge-base/decisions/2026-06-23-validation-milestone-sponsor-hard-stop.md`
- **Runbook followed:** `docs/operations/m7-cleanroom-validation-runbook.md`
- **Baseline captured:** `docs/operations/baselines/YYYY-MM-DD-qa-readiness-complete.md`

## Chain step results

| # | Step | Wall-clock | Result | Notes |
|---|---|---|---|---|
| 0 | Pre-flight gate (`go test -run TestM7Preflight`) | ~10s | <PASS/FAIL> | 7/7 subtests |
| 1 | Cleanroom TOTAL (`task clean:all && task setup`) | ~Xmin | <PASS/FAIL> | 4 clusters Running |
| 2 | UP (`tilt up + task doctor`) | ~Xmin | <PASS/FAIL> | PASS=<N> FAIL=<N> |
| 3 | seed-post-cleanroom-all.sh --verify | ~Xmin | <PASS/FAIL> | 12 steps |
| 4 | dogfood-all.sh --live-only --json | ~Xmin | <X/13 PASS> | per-wrapper log: /tmp/dogfood-all-*.log |
| 5 | live-proof-dev-public.sh | ~30s | <X/9 PASS> | layer 1-3 probes |
| 6 | run-chaos.sh × 2 | ~Xmin | <X/2 PASS> | gitea-outage + pod-kill-api-leader |
| 6.5 | Baseline capture | ~5min | <DONE> | filed at `docs/operations/baselines/YYYY-MM-DD-qa-readiness-complete.md` |
| 7 | Sponsor JUNTOS browser walkthrough | ~30min | <PASS/FAIL> | sponsor verbatim quote below |

## Sponsor verbatim quote (HARD STOP marker #4)

> <Quote sponsor exact words verbatim here — e.g., "Funcionou!" or equivalent green-light phrase>

## Per-journey walkthrough notes

- [ ] J-01 Login (Google OIDC) — <observation>
- [ ] J-09 Project connect (`getloopscale/loopzero-repotest`) — <observation>
- [ ] J-17 Deployment (fresh PR → live URL) — <observation>
- [ ] J-20 Scale (dashboard → K8s converges) — <observation>
- [ ] Audit log (cross-cutting — no blank WHEN/ACTOR, no SQL fragments) — <observation>
- [ ] J-10 Promote (preview → production) — <observation>
- [ ] J-19 Rollback (UI honesty under transitional state) — <observation>

## Release artifacts

- **PR:** <link to release PR>
- **Merge commit:** <SHA>
- **Tag:** v<X.Y.Z>
- **GitHub release:** <link>
- **Roadmap M7 status:** flipped from `🟢 PREPARATION_COMPLETED` to `✅ COMPLETED` at commit <SHA>

## Followups (carried forward)

<!--
If any non-blocker drift was observed during Step 7, file followup issues here with `qa-readiness-followup` label.
M7 itself remains ✅ COMPLETED for this dated run; followups are tracked separately.
-->

- [ ] <followup 1>
- [ ] <followup 2>

## Cross-references

- Closes M7 of `qa-readiness-roadmap-2026-06-23.md`
- Closes the canonical pattern established by ADR `2026-06-23-validation-milestone-sponsor-hard-stop.md` § Worked example
- Run-file: `.claude/knowledge-base/roadmap-runs/M7-YYYY-MM-DD.md`
- Mandate INQUEBRÁVEL: `.claude/rules/cleanroom-up-live-test-mandate.md` (sponsor verbatim 2026-06-10 + 2026-06-16)
