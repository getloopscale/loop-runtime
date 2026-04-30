# Operator Cutover Time-Box Discipline (T2.3)

> Plan reference: `docs/plans/infra-normalize-multicell-plan.md` Phase 2.
> Source decision: `docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md` (condition C21).
> Last updated: 2026-04-30.
> Audience: cutover executor + change-governor + SRE.

This runbook codifies condition C21: a 3-iteration time-box for T2.3 (live cutover validation per cluster). Without explicit time-box discipline, post-cutover debugging can drag on indefinitely while the partial-state cluster accumulates issues.

---

## What's an "iteration" here?

**One iteration = one full attempt at executing `infra/scripts/operator-cutover-execute.sh <ctx>` against a target cluster, from pre-flight through post-validation, regardless of outcome.** A successful iteration ends with reconcile activity confirmed in the 4 split operators. A failed iteration ends with `operator-cutover-rollback.sh` having restored the monolith state.

Each iteration produces a log file at `/tmp/operator-cutover-<timestamp>.log` capturing pre-flight, sync, watcher, post-validation. The change-governor archives these logs after the cutover process closes (success or escalation).

---

## The 3-iteration cap

For each cluster (dev → standalone-multi → standalone → prod-canary):

```
ITERATION 1 — primary attempt
  Run: bash infra/scripts/operator-cutover-execute.sh <ctx>
  Outcome paths:
    success    → proceed to soak (Step 5 of operator-cutover-runbook.md)
    failure A  → auto-rollback fired; investigate root cause; proceed to ITER 2
    failure B  → auto-rollback partially failed; investigate cluster state;
                 may need iter 2 OR direct escalation depending on damage

ITERATION 2 — informed retry
  Required entry condition: ITER 1 failure was diagnosed; specific fix
  applied (chart bump, allowlist update, kubectl manual cleanup of
  dangling resources, etc).
  Run: same as ITER 1.
  Outcome paths:
    success    → proceed to soak
    failure    → proceed to ITER 3 OR ESCALATE (operator's call)

ITERATION 3 — last automatic attempt
  Required entry condition: ITER 2 failure was diagnosed AND the fix
  delta is judged sufficient by SRE on-call (NOT by the executor alone).
  Run: same.
  Outcome paths:
    success    → proceed to soak; flag the cluster for closer post-cutover
                 monitoring (3 iters indicates the chart isn't well-tuned
                 for this env)
    failure    → ESCALATE — supplementary T3 meeting

ITERATION 3+ — TIME-BOX VIOLATED
  Per condition C21, do NOT run a 4th attempt. Escalate to:
    - SRE on-call (paged)
    - change-governor
    - platform-architect
  The supplementary T3 meeting must happen within 2 business days.
  Document outcomes in `docs/decisions/MEET-INFRA-NORMALIZE-T3-cutover-escalation-N.md`.
```

---

## Why a time-box?

Three reasons:

1. **Bounded executor cognitive load.** A cutover that fails 4+ times in a row is no longer a routine operation; the executor's mental model of the failure mode degrades, and they start making increasingly speculative changes between attempts.

2. **Bounded cluster state damage.** Each rollback eats ~5 min of cluster state churn. Six rollbacks (3 cutover attempts × 2 directions if any auto-rollback ALSO fails) is 30 min of state churn. Tenants on the cluster have been impacted enough by then that further iteration is opaque to operations.

3. **Forced reflection.** Three failures means the chart, the script, OR the cluster is in a state the original meeting didn't anticipate. A supplementary T3 meeting forces the team to observe that.

---

## What to capture before escalating

When approaching ITER 3 (i.e., ITER 2 failed), pre-write:

1. **Failure mode summary** (1 paragraph): what went wrong in each iter.
2. **Diff applied between iters**: exactly what changed (config flag flipped, allowlist row added, manual kubectl cleanup performed).
3. **Cluster state snapshot**: `kubectl get pods,deploy,leases -n loopzero-system -o yaml > /tmp/snap-iter-N.yaml` at the start AND end of each iter.
4. **Watcher log**: `/tmp/op-cutover-watcher-*.log` — shows every zero-Ready event observed during the attempt.
5. **Hypothesis**: what the executor thinks is wrong, even if speculative. The supplementary meeting validates or rejects the hypothesis.

This is the input to the escalation meeting, not output. Pre-writing forces the executor to articulate what they've observed before the meeting starts.

---

## Implication for ADR D1 forward-only

Forward-only migration (ADR D1) does NOT mean "force success no matter what". If 3 iterations on a cluster fail, it's evidence the SHIPPED state of the migration is wrong for that cluster's environment shape. Forward-only is about ROLLBACK MECHANICS (use git history, not feature flags), not about "the cutover must succeed first try".

A 3-iter failure on dev cluster might surface a chart bug that, once fixed, lets every subsequent cluster cut over cleanly on iter 1. The 3-iter cap is a circuit breaker, not a guarantee of success.

---

## Cross-references

- `docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md` — condition C21 (this doc is the deliverable)
- `docs/decisions/operator-split-invariants.md` — invariants the cutover must preserve
- `docs/runbooks/operator-cutover-runbook.md` — the per-iteration procedure
- `infra/scripts/operator-cutover-execute.sh` — the orchestrator
- `infra/scripts/operator-cutover-rollback.sh` — the rollback used between iters
- `.claude/rules/workflow.md` — invariant: forbidden git ops (rollback uses `git show`)
