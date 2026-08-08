---
type: plan
title: "Operator Handoff — Shell Script Strategic Plan (Phase 5 autonomous saturation)"
status: deprecated
---

# Operator Handoff — Shell Script Strategic Plan (Phase 5 autonomous saturation)

**Date:** 2026-05-22
**Branch:** `refactor/shell-scripts-strategic`
**Author:** Ralph loop (autonomous), iters 19-38 of plan execution
**Status of this doc:** the autonomously-safe (no-LIVE, low-risk) work of
Phase 5 T5.3 is **complete/saturated**. This handoff maps what remains, which
is operator-gated, and records two dead-script-detection caveats so future
iterations don't repeat them.

---

## What is DONE (Ralph-safe, merged on this branch)

**Phase 5 T5.3 — pure-logic gate migrations: COMPLETE.**

- **30 pure-Go quality gates** under `pkg/quality/*`, each wired via
  `cmd/loopzero-ops quality <gate>`, each ≥90% test coverage, each parity-verified
  against the bash it replaced.
- **35 shell scripts deleted** (allowlist 257 → 224); zero CI behavior
  regressions (verdict-level parity preserved on every migration).
- Gates added this campaign (iters 19-37): action-pinning (+composite actions,
  consolidated check_actions_sha_pinned), prod-image-tags, no-migrate-force,
  cosign-cutover + service-auth-rotation (shared deadlineratchet core),
  stack-coverage (was unwired — now blocking), decisions-deletion,
  drill-outputs-consumers, cli-bundle-size, agpl-isolation, orphan-alerts,
  alert-runbooks, base-image-digest, clusterimagepolicy-mode, security-context.
- **Dead subsystem deleted:** validate-evidence.sh + 3 taskfile tasks +
  health-check section (operated on docs/evidence/ deleted 2026-05-11).
- **Findings raised (NOT masked):** alert-runbooks (33 violations) and
  base-image-digest (7 violations) are genuinely RED on the live repo (real
  backlogs the gates correctly surface). See the two finding docs. The
  migrations preserved the red state faithfully; remediation is operator-owned.

---

## What REMAINS (operator-gated — Ralph cannot complete autonomously)

| Item | Why Ralph can't | Operator action |
|------|-----------------|-----------------|
| **Phase 4 — ~35 MIGRATE-GINKGO scripts** (parity/*, dogfood/*) | Need a LIVE k3d cluster; they orchestrate real deploys | Run on cleanroom infra; convert to Ginkgo specs incrementally |
| **Phase 2 — DELEGATE-HELMFILE scripts** (bootstrap-*, chaos run-scenario) | Need LIVE `helmfile sync` validation | Move logic into `infra/helmfile/` declarative releases; verify on cleanroom |
| **T5.1 — 3 pulumi-* (MIGRATE-GO-SDK)** (pulumi-output, pulumi-archive, pulumi-orphan-rm) | Pulumi Automation API = heavy dep + LIVE stack state | Implement via pulumi-go SDK; validate against a drill stack |
| **check-drill-credentials-isolation.sh** | Read-access blocked by a permission rule on the "credentials" path | Operator reads + classifies (likely KEEP-OPERATOR — uses pulumi live) |
| **T6.4 — final inventory validation** | Gated by Phases 2+4 completion | Re-run scripts-audit after Phases 2+4 |
| **4 LIVE HARD STOPs** — cleanroom, `/dogfood full`, `/chaos-test --full`, `/cross-validation` | All require LIVE clusters | Run per the Dogfood Gate before declaring the plan DONE |

**Target ≤30 scripts is structurally Phase-2+Phase-4 work** — the pure-logic
Phase 5 lever Ralph could pull is now exhausted at 224.

---

## CAVEAT — dead-script detection heuristics are UNRELIABLE (do not mass-delete)

Two heuristics were tried and both produced false positives. **Per-script
read-verification is mandatory before any deletion.**

1. **Orphan-test heuristic** (`*_test.sh` whose `<name>.sh` parent is gone):
   false-positives on standalone assertion harnesses that test workflows/docs/Go
   (not a sibling script) — e.g. `skill-paths_test.sh`, `credentials-sot_test.sh`.
2. **Zero-reference heuristic** (no grep hit in consumers): false-positives on
   - `.claude/hooks/*` — invoked by the Claude harness (settings.json), not grep-able.
   - `infra/tests/parity/lib/cell2-gate-c*.sh` — sourced **dynamically** via
     `$LIB_DIR/cell2-gate-${g}.sh` (the iter-11 trap); literal-basename grep misses it.
   - many `*_test.sh` — run by CI test runners / `find`-based dispatch.

A genuinely-dead script must be confirmed by: reading it + checking for dynamic
(`$VAR`-interpolated) sourcing + checking harness/skill invocation + checking
CI test-runner globs. Heuristic lists are a STARTING POINT, never an action list.

### Dead-deletion outcomes (read-verified)

- **CONCLUSIVELY DEAD → deleted (iter 39):** `regenerate-status.sh` — its own
  header declares it STALE (regenerated `docs/STATUS.md`, deleted 2026-05-11) and
  it no-ops when the target is absent. Zero live consumers. This is the *only*
  smoking-gun-dead script found.
- **FUNCTIONAL-BUT-UNWIRED → NOT deleted (operator decides, iter 40):** four
  zero-ref scripts were read-verified as *functional* operator/skill tooling that
  is merely not grep-wired — deleting them is a judgment call, not auto-safe:
  - `infra/scripts/chaos/run-full.sh` — bash sequential chaos runner; the suite
    uses the Go framework (`go test ./chaos/...`) and the skill uses
    `run-scenario.sh` per-scenario, so this is redundant but still works.
  - `infra/scripts/quality/flaky-track.sh` — retry-event logger; producer for the
    flaky-aggregator cron, but no test wrapper currently calls it ("implemented
    but not wired").
  - `infra/scripts/verify-doctl-resolution.sh` — drill doctl-resolution check;
    plausibly drill-skill/operator-invoked.
  - `infra/tests/dogfood/cli-coverage.sh` — dogfood Phase 6 coverage helper;
    plausibly dogfood-skill-invoked.

  **Operator action:** for each, decide KEEP-OPERATOR (it's a real convenience →
  document why) OR delete (truly superseded). Do NOT auto-delete — chaos/dogfood
  tooling is operator-critical.

**Takeaway:** conclusively-dead scripts are RARE (1 of the ~10 zero-ref
candidates). The rest are functional-but-unwired — retiring them is operator
judgment, not autonomous deletion.

---

## Tool-classification caveat (for the remaining REVIEW-NEEDED triage)

The iter-25 disposition audit used a grep heuristic that produced **7
false-positives** across the migrated batch (e.g. `\bhelm\b` matched the
`infra/helm/` path, not a helm invocation; `pulumi`/`cosign`/`kubectl` matched
pattern-strings or comments, not executions). Every one was caught by
read-verify before action. **When classifying a remaining script as
pure-logic vs shell-wrap, READ it — do not trust the grep heuristic.**
