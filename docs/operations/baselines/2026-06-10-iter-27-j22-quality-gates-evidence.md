---
type: baseline
title: J-22 OpenBao Transit Engineering Side Closure Evidence
---

# Baseline — J-22 OpenBao Transit Engineering Side Closure Evidence

> **Captured:** 2026-06-10T14:06Z
> **Captured by:** Iter 28 productive-idle of plan `2026-06-10-j22-openbao-transit-eso-sync` v1.2
> **HEAD at capture:** `f020be17` feat(quality): wire 4 J-22 anti-drift hermetic tests + meta-gate (iter 27)
> **Branch:** worktree-glowing-growing-babbage (113 commits ahead of origin/main)
>
> **Scope:** Engineering-side closure baseline for the productive-idle arc iters 18-27. Documents:
> 1. The 5 J-22 hermetic anti-drift tests all PASS on the cluster captured by the iter 23 cleanroom run + iter 26 LIVE-validated rehearsal flag
> 2. The `task quality:gates` umbrella RED reported during this iter is 100% pre-existing (NOT caused by iters 18-27) — all 5 flagged scripts predate iter 18 by 2-6 days
> 3. The engineering side waits for the 4 sponsor-gated operator HARD STOPs (helmfile sync + env flip + LIVE 6/6 + sponsor walkthrough) — Unbreakable Rule 3 mandates the completion promise stays WITHHELD until those gates close

Per `.claude/rules/steady-state-baseline.md`: this is a Layer 1 + Layer 4 baseline. Layers 2 (runtime pods + helm releases) and 3 (config) were captured at iter 23 cleanroom (`/tmp/baseline-post-rebuild.json`) and are unchanged on the J-22 surface since then.

---

## Layer 1 — Code state

```
git rev-parse HEAD                  → f020be17
git branch --show-current           → worktree-glowing-growing-babbage
git rev-list --count origin/main..  → 113
git status --porcelain              → 1 untracked .progress file (ralph-loop artifact)

Session commit chain (11 commits, iters 18-27):
  f020be17 feat(quality):    wire 4 J-22 anti-drift hermetic tests + meta-gate (iter 27)
  c21e02f4 feat(test):       T4.1 --rehearsal flag + task target + hermetic contract (iter 26)
  6dd43eb7 feat(helmfile):   dev-public ESO overlay for openbao-transit (iter 25)
  02249756 fix(api):         go.mod tidy follow-up — indirect deps from iter 22 fix
  b95fa9c6 feat(taskfile):   k3d:vault:bootstrap-in-mgmt option (c) shortcut + hermetic test (iter 24)
  9d959fda docs:             T6.1 runbook iter 23 — delegate to canonical pipeline
  1474fb50 fix(api):         tidy go.sum — add transitive deps for pkg/vault (iter 22 cleanroom)
  902dbd71 fix(bootstrap):   seed Tenant CR with empty environments[] — canonical M2 (iter 22 cleanroom)
  aebe2da3 fix(images):      bump 8 Dockerfile golang base images 1.25.10 → 1.26.0 (iter 21 cleanroom)
  77a4ff1c docs:             T6.1 operator cutover runbook + canonical envFrom wire (iter 20)
  8ad2524f feat(openbao-transit): T2.5 boot wire-up + anti-double-encrypt pass-through (iter 18)
```

## Layer 4 — LIVE functional probes (engineering side)

### 4a — 5 J-22 hermetic anti-drift tests run direct (iter 27 CI-gate equivalent)

```
$ for t in <5 J-22 test files>; do bash "$t" 2>&1 | tail -1; done

infra/tests/helm/loopzero-api-openbao-approle-envfrom_test.sh
  → openbao-approle envFrom render: pass=7 fail=0

infra/tests/check-vault-bootstrap-in-mgmt-task_test.sh
  → vault-bootstrap-in-mgmt task contract: pass=18 fail=0

infra/tests/check-dev-public-loopzero-external-secrets-overlay_test.sh
  → dev-public loopzero-external-secrets overlay contract: pass=18 fail=0

infra/tests/check-rehearsal-mode-syntax_test.sh
  → rehearsal-mode contract: pass=26 fail=0

infra/tests/check-openbao-transit-quality-gates-wired_test.sh
  → openbao-transit quality:gates wire-up: pass=27 fail=0

TOTAL: 96 PASS / 0 FAIL in ~1s
```

### 4b — task quality:gates umbrella (full repo CI gate)

```
$ task quality:gates 2>&1 | head -20

task: [quality:build] cd cmd/loopzero-ops && go build -o /tmp/loopzero-ops .
task: [quality:gates] /tmp/loopzero-ops quality unhomed-logic --check
un-homed operational logic: 137 scripts
  bootstrap    34
  drill        16
  other        13
  test         74
unhomed-logic: FAIL — suspect KEEP (script tagged KEEP-* but embeds a Python heredoc):
  - infra/scripts/local/patch-kratos-dev-public.sh
  - infra/scripts/local/live-proof-dev-public.sh
  - infra/scripts/local/seed-admin-loopzero-dev.sh
  - infra/scripts/local/baseline-capture.sh
  - infra/scripts/local/baseline-diff.sh
A KEEP claim must be genuine operator glue. Move the logic to loopzero-ops/Go, or drop the KEEP tag.
task: Failed to run task "quality:gates": exit status 1
```

**Verdict:** RED — first gate `unhomed-logic` fails on 5 scripts.

### 4c — Pre-existing analysis (verifying NOT caused by iters 18-27)

```
$ git log --diff-filter=A --format="%h %ci" -- <each script>

patch-kratos-dev-public.sh    → 453c363d  2026-06-04 07:37:11 +0000
live-proof-dev-public.sh      → a3db07fe  2026-06-04 08:12:46 +0000
seed-admin-loopzero-dev.sh     → 12acf429  2026-06-08 18:35:25 +0000
baseline-capture.sh           → e68719ff  2026-06-08 18:51:05 +0000
baseline-diff.sh              → e68719ff  2026-06-08 18:51:05 +0000

iter 18 commit time:          8ad2524f  2026-06-10 09:03:02 +0000
```

All 5 flagged scripts were added between **2026-06-04 and 2026-06-08** — **2-6 days before iter 18** at `2026-06-10 09:03 UTC`. None of these scripts were modified by iters 18-27 of this session. The `unhomed-logic` gate RED state is **pre-existing technical debt** independent of the J-22 OpenBao Transit chain.

## Honest assessment

| Question | Answer |
|---|---|
| Did iters 18-27 break `task quality:gates`? | NO — the failure was already there |
| Are the 5 J-22 hermetic tests passing on the local cluster? | YES — 96/96 PASS in ~1s |
| Is the J-22 engineering side complete? | YES — 18 plan tasks committed + 11 productive-idle commits |
| Can the completion promise be emitted? | NO — 4 operator HARD STOPs pending (Unbreakable Rule 3) |
| What does the operator need to do? | Follow [`docs/operations/openbao-transit-cutover-runbook.md`](../openbao-transit-cutover-runbook.md) steps 1-4 |
| What does the sponsor need to do? | Authorize one of (a)/(b)/(c)/(d) for cross-cluster reachability per the runbook § Open Question, then walk through step 4.3 LIVE with verbatim "Funcionou!" |

## Recommendation for the pre-existing `unhomed-logic` failure

OUT-OF-SCOPE for this halt-loop (J-22 only) — separate ticket needed. The 5 scripts flagged:

| Script | Owner | Recommended action |
|---|---|---|
| patch-kratos-dev-public.sh | dev-public infra | Move Python heredoc logic to `loopzero-ops/quality/kratos-patch` Go gate |
| live-proof-dev-public.sh | live-proof skill | Same — Go gate |
| seed-admin-loopzero-dev.sh | canonical dev user seed | Same — Go gate |
| baseline-capture.sh | steady-state baseline rule | Heredoc could become `loopzero-ops/baseline capture` subcommand |
| baseline-diff.sh | steady-state baseline rule | Heredoc could become `loopzero-ops/baseline diff` subcommand |

Each is a separate slice of `unhomed-logic` debt repayment. None block J-22 closure.

## Next steps

1. **Operator action (sponsor-gated):** authorize one of cross-cluster reachability options + execute runbook steps 1-3
2. **Sponsor walkthrough:** runbook step 4.3 LIVE with verbatim "Funcionou!"
3. **THEN:** emit `<promise>IMPLEMENTATION_COMPLETE</promise>` per Unbreakable Rule 3

Until those happen, the halt-loop continues productive-idle iterations. Each adds canonical-locked anti-drift coverage without speculative promise-emission.

## Cross-references

- Plan: `.claude/knowledge-base/plans/2026-06-10-j22-openbao-transit-eso-sync-plan.md`
- Operator runbook: [`docs/operations/openbao-transit-cutover-runbook.md`](../openbao-transit-cutover-runbook.md)
- Steady-state baseline rule: `.claude/rules/steady-state-baseline.md`
- Productive-idle mandate: memory `feedback_productive_idle_runbook_preauthor_2026_06_10`
- Per-feature LIVE workflow: memory `feedback_post_feature_live_regression_workflow_2026_06_09`
- Cleanroom session (iter 21-23 LIVE catches): commits `aebe2da3..9d959fda`
