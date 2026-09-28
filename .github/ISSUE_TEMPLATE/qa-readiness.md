---
name: QA-readiness milestone closure
about: Records the post-execution closure of a QA-readiness milestone (M7-class — Cleanroom validation + joint sponsor walkthrough).
title: "QA-readiness YYYY-MM-DD complete"
labels: qa-readiness
assignees: ''
---

<!--
Canonical GH issue template for QA-readiness milestone closure.
Per ADR 2026-06-23-validation-milestone-sponsor-hard-stop.md § Decision Step 4.
Operator runs `gh issue create` from this template AFTER the joint sponsor walkthrough (Step 7) PASSES.
-->

## Milestone metadata

- **Milestone:** M7 of the QA-readiness roadmap (the `.claude/knowledge-base/` tree that held it no longer exists; `.claude/` is gitignored)
- **Date:** YYYY-MM-DD
- **Operator:** <name>
- **Sponsor present:** <name(s)>
- **Plan:** the M7 cleanroom-validation readiness plan (same removed tree)
- **ADR:** validation-milestone sponsor hard stop, 2026-06-23 (same removed tree)
- **Runbook followed:** `git show 4f728eb7a:docs/operations/m7-cleanroom-validation-runbook.md`
- **Baseline captured:** _(path — `docs/operations/baselines/` was removed in the 2026-09-11 consolidation)_

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
| 6.5 | Baseline capture | ~5min | <DONE> | filed where baselines are now kept (`docs/operations/baselines/` was removed 2026-09-11) |
| 7 | Joint sponsor browser walkthrough | ~30min | <PASS/FAIL> | sponsor verbatim quote below |

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
- Run-file: the M7 roadmap run (the `.claude/knowledge-base/` tree that held it no longer exists)
- Unbreakable mandate (sponsor verbatim 2026-06-10 + 2026-06-16): Cleanroom TOTAL → UP → seeds → DOGFOOD FULL → LIVE TEST → QA Red Team; warm-infra results are not LIVE-valid. The rule file `.claude/rules/cleanroom-up-live-test-mandate.md` no longer exists.
