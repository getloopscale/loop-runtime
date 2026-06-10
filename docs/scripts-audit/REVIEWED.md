# Scripts Audit Review Log

Per **T0.1 acceptance criteria** of `docs/plans/completed/2026-05-22-shell-script-strategic-plan.md`:
operator-review (HUMAN GATE) of the baseline CSV before Phase 1 (delete) executes.

This file is **append-only**. Each entry is a timestamped review of a specific
CSV emission with operator signature, summary of decisions, and red-flag findings.

---

## Review 2026-05-22 — baseline emission

**CSV:** `docs/scripts-audit/2026-05-22.csv`
**Branch:** `refactor/shell-scripts-strategic`
**Reviewer:** (pending operator signature)
**Status:** PENDING OPERATOR REVIEW (Ralph engineering side complete; operator action required)

### Inventory snapshot

| Metric | Value | Notes |
|--------|-------|-------|
| Total scripts | 257 | Branch is pre-migration (commit `10d9389b`); develop has more |
| Total LOC | 42,793 | Close to plan baseline 44,586 (which counted develop) |
| Scripts with callers | 183 / 257 (71%) | 74 zero-caller scripts are review candidates |
| Distinct dispositions | 9 | Heuristic categorization functional |

### Disposition distribution

| Disposition | Count | % | Phase ownership |
|-------------|-------|----|-----------------|
| REVIEW-NEEDED | 118 | 46% | Operator decides case-by-case (Phase 1.1) |
| AUTO-DIE | 63 | 25% | Phase 1 / D6 — die with parent script |
| MIGRATE-GINKGO | 35 | 14% | Phase 4 — test orchestrators |
| MIGRATE-GO-PUREFN | 23 | 9% | Phase 5 — check-/validate-/audit-*.sh |
| DELEGATE-HELMFILE | 9 | 4% | Phase 2 — bare-helm wrappers |
| MIGRATE-GENERATOR | 4 | 2% | Phase 5 — gen-/generate-*.sh |
| MIGRATE-GO-SDK | 3 | 1% | Phase 5 — pulumi-*.sh |
| KEEP-OPERATOR | 1 | <1% | Allowlist (Phase 6) |
| DELEGATE-ARGOCD | 1 | <1% | Phase 2 |
| **DELETE-DEAD** | **0** | **0%** | Heuristic: 0 callers + last-mod > 90d. Branch is recent so no script meets the date threshold — operator must identify dead scripts via runbook references / sprint completion / git log analysis (T1.1 manual validation) |

### Red flags for operator attention

1. **0 DELETE-DEAD** — Heuristic threshold (last-mod > 90d) returns empty because branch
   was created recently and stat() reflects post-checkout times. **Operator action:**
   T1.1 validation must use `git log --follow --diff-filter=A` to identify scripts
   added in completed sprints and never re-touched, NOT rely solely on file mtime.

2. **REVIEW-NEEDED is plurality (46%)** — Expected for first audit. Heuristics are
   conservative: anything ambiguous defaults to REVIEW-NEEDED. Operator categorizes
   per D4 criteria (Necessary + Simple + Operator-time + Allowlisted).

3. **AUTO-DIE = 63** — Largest concrete category. These are `*_test.sh` files that
   automatically die when their parent script is deleted/migrated (D6). Track these
   as **byproduct cleanup** of Phases 2-5, not standalone Phase 1.2 work.

4. **DELEGATE-HELMFILE = 9** — Includes `bootstrap-patch-admission-policies.sh`,
   `bootstrap-patch-sigstore-policy-controller.sh` and 7 others. These are exactly
   the bug-A/bug-B class scripts that motivated the plan (cleanroom drift).
   **Phase 2 prime target.**

5. **MIGRATE-GO-SDK = 3** — `pulumi-archive.sh`, `pulumi-output.sh`, etc. Plan T5.1
   targets these via Pulumi Automation API (NOT shell-wrap per D5).

### Reviewer Sign-Off Template

```
Reviewed by: <operator-initials>
Date: <YYYY-MM-DD>
Decisions:
  - Spot-checked 10 random REVIEW-NEEDED entries: <observations>
  - Confirmed DELEGATE-HELMFILE list aligns with Phase 2 scope: <yes/no>
  - Adjusted disposition for these scripts: <list>
Next action: proceed to Phase 1.1 (T1.1 dead-set validation)
```

(operator fills above when reviewing the baseline)

---

## Review 2026-05-22 (iter 11) — T1.1 dead-set validation

**Performed by:** ralph-loop-iter11 (engineering validation; operator sign-off still pending)
**Output:** `docs/scripts-audit/2026-05-22-dead-set.md`

**Finding: validated dead-set = 0 scripts.**

The 73 raw zero-caller scripts collapse to 0 safely-deletable after applying
T1.1.a (EC-03 dynamic-invocation grep) + orphan-test-of-live-code check:
- 13 `cell2-gate-c*.sh` are dynamically sourced via `$LIB_DIR/cell2-gate-${g}.sh`
  (EC-03 catch — would have been a destructive false-positive delete).
- 2 hermetic tests (`dry-run-lib_test.sh`, `values_gate_test.sh`) test LIVE code
  → WIRE-CANDIDATE, not delete.
- 1 (`sanitize.sh`) pairs with render-drill.sh snapshot harness → REVIEW-NEEDED.

**Conclusion:** No T1.2 bulk-delete this branch. Big reductions come from Phase 4
(Ginkgo migration of parity/dogfood test orchestrators) + Phase 5 (pure-logic
Go migrations) + operator-reviewed dead-set with git-history context.

This validation PREVENTED a 15-script destructive delete. That is its purpose.
