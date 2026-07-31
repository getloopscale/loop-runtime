# ADR — sqlc migration rollback playbook (operator-witnessed criteria + atomic rollback mechanism)

**Date:** 2026-05-29
**Status:** Accepted (APPROVED WITH CONDITIONS — MEET-2026-05-29-001) · **Sub-amended 2026-05-29** per `MEET-ASYNC-AMENDMENT-2026-05-29-001-sqlc-develop-direct.md` (D8 → D8' supersession): rollback mechanism changes from single squash-merge inverse to per-commit-range inversion. See § 2.2 sub-amendment block.
**Tier:** T3 (architecture, stack, critical flow — coupled to ADR-1)
**Authors:** Paulo (sponsor + operator) · Ralph (AI executor)
**Meeting:** MEET-2026-05-29-001 (12 specialists) + `MEET-ASYNC-AMENDMENT-2026-05-29-001-sqlc-develop-direct.md` (sponsor-direct)
**Coupled ADRs:** [`2026-05-29-sqlc-adoption.md`](./2026-05-29-sqlc-adoption.md) · [`2026-05-29-schema-gen-sql-render.md`](./2026-05-29-schema-gen-sql-render.md)

> **Path note (added 2026-07-30, body unchanged).** This ADR refers to quality gates under
> `pkg/quality/…`. That package was relocated to **`cmd/loopzero-ops/internal/quality/…`** at M9
> (ROADMAP steps 3+4) — the gate names are unchanged, only the import path moved. The body below is
> preserved as written on its date; do not rewrite it to match the present tree.


---

## 1. Context

### 1.1 Why this ADR exists

The sqlc migration in ADR-1 is **big-bang** — 44 store files deleted and replaced in a single squash-merge to `develop`. The SRE reviewer at MEET-2026-05-29-001 flagged that **the absence of a documented rollback path is the single largest production-fitness risk** of the migration.

This ADR exists to convert that risk from "operator discovers we need to roll back, then improvises" to "operator follows a pre-authorized playbook with explicit triggers and pre-validated mechanism." Pre-authorization is essential because rollback decisions made under post-incident time pressure are exactly the conditions in which operators make incomplete reverts that ship half-broken states.

### 1.2 Why pre-release does NOT relax this

LoopZero is pre-release per `.claude/rules/pre-release-governance.md`. Pre-release means backward compatibility is not required and clean redesigns are preferred. It does **not** mean rollback discipline is optional. Pre-release just shifts what "production" means — DigitalOcean infrastructure is real, customer trials may be scheduled in the future, and operational incidents have real cost (operator time, ADR debt, infra spend on extra drill iterations).

### 1.3 Coupling to ADR-1

This ADR is a **precondition** of ADR-1. M-6 of ADR-1 explicitly requires that this ADR be pre-authored before any sqlc migration code lands. The rollback authorization captured here is the basis on which both VETO holders APPROVE conditional in MEET-2026-05-29-001.

---

## 2. Decision

### 2.1 Rollback trigger criteria (operator-locked)

**ANY of these triggers an immediate rollback evaluation by Paulo (operator).** The default response to a triggered condition is rollback — investigation while keeping the migration merged is NOT the default during the 7-day observation window.

| # | Trigger | Time window | Owner of detection |
|---|---|---|---|
| T-1 | 1 × P0 production incident traceable to `api/internal/db/` | 7-day observation window post-merge | SRE / Paulo |
| T-2 | 2 × P1 production incidents traceable to `api/internal/db/` | Same 7-day window | SRE / Paulo |
| T-3 | `task quality:gates` advisory-lock gate RED | ≥3 consecutive CI runs | CI |
| T-4 | `task quality:gates` sqlcsync gate RED | Cannot be resolved via re-render in <2h | CI / Paulo |
| T-5 | `task dogfood:full` RED on cleanroom-fresh infra | Any post-merge run (V9 must have passed pre-merge) | Paulo (operator) |
| T-6 | Drill iter ≥57 RED | Post-merge drill must also pass; iter 56 is the pre-merge gate per ADR-1 M-7 / V10 | Paulo (operator) |

"Traceable to `api/internal/db/`" means the incident root cause is (a) sqlc-generated code, (b) a query semantic regression introduced during reshape, (c) a missing column / Scan / INSERT triple in a sqlc query file, or (d) a transactional invariant (`withEnvironmentLock`, `pg_advisory_xact_lock`, `Transition()`) violated by sqlc's emitted code. Incidents in unrelated modules (operators, CLI, infra) are out of scope.

### 2.2 Rollback mechanism

> **Sub-amendment 2026-05-29 (CURRENT mechanism — supersedes original below):**
>
> Per `MEET-ASYNC-AMENDMENT-2026-05-29-001-sqlc-develop-direct.md`, the migration now lands as **per-commit incremental** on `develop` (no squash-merge event per D8'). The rollback mechanism therefore changes from single-SHA atomic inversion to **per-commit-range inversion**:
>
> 1. **Identify the sqlc commit range** via `git log --grep="sqlc" --oneline` (every substantive sqlc-migration commit carries `sqlc` in its subject line per the canonical commit convention adopted for this migration). Verify range is contiguous; flag any non-sqlc commits interleaved (these need separate review).
> 2. **Capture range as variables:** `FIRST=$(git log --grep="sqlc" --oneline | tail -1 | awk '{print $1}')` and `LAST=$(git log --grep="sqlc" --oneline | head -1 | awk '{print $1}')`.
> 3. **Sequential per-commit inversion in chronological-reverse order** (newest sqlc commit first, oldest last) — use Git's standard inverse-commit operation against each SHA in the range. Single combined inversion commit avoids polluting history with N revert commits: produce a working-tree result via stash-pop pattern + commit as one "rollback ADR-3 §2.2-sub-amendment" message referencing the range bounds.
> 4. **Run `task test` + `task quality:gates` + `task dogfood:full`** to confirm the inverted state is GREEN before push.
> 5. **Push the single rollback commit to develop.**
>
> Operator-witnessed only (Ralph cannot execute per `.claude/rules/workflow.md` § Git Rules; the operator-witnessed carve-out from § 2.6 of this ADR still applies). The rollback commit subject MUST reference the sub-amendment + the range bounds + the triggering T-1..T-6 condition.
>
> **Risks specific to per-commit-range inversion (vs original squash):**
> - Interleaved non-sqlc commits in the range → require manual triage (skip non-sqlc, invert only sqlc); higher cognitive load on operator
> - Merge conflicts during inversion if parallel work touched the same files → operator handles per-conflict per standard merge conflict workflow
> - Loss of single rollback-SHA shorthand → audit trail now references "rollback commit + range bounds" tuple instead of "rollback SHA"
>
> **Mitigation:** M-15 daily diary explicitly tracks the contiguous sqlc commit range; any interleaved commits are flagged in the diary at write time so the rollback range is pre-computed.

---

**Original mechanism (preserved for audit trail; superseded by sub-amendment above):**

The migration merges to `develop` via squash-commit (per ADR-1 M-8 — **superseded by D8'**). This makes the rollback mechanism atomic:

1. Identify the squash-merge SHA (call it `<sqlc-merge-sha>`).
2. Create a new commit on `develop` that inverts `<sqlc-merge-sha>` using the standard Git inverse-commit operation. The new commit is created on `develop` directly.
3. Run `task test` + `task quality:gates` + `task dogfood:full` to confirm the inverted state is GREEN.
4. Push the new commit to `develop`.

**Note on `.claude/rules/workflow.md`:** LoopZero's workflow rules prohibit the version-control "switch branches" and "create inverse commit" operations mid-session as a default discipline (to prevent accidental work loss in feature branches). The rollback mechanism here is the **explicit exception** carved out by this ADR — the inverse commit is created by Paulo (operator), with full pre-authorization from MEET-2026-05-29-001, against a known sqlc-migration-merge SHA, on the `develop` branch only, and never to amend history. This is the documented operator-witnessed escape from the general rule.

The inverted commit:

- Restores the 44 hand-rolled `*_store.go` files.
- Restores `schema_mirror_test.go` to active CI participation (ADR-1 M-10 step 2 cannot have happened yet — it's gated by 7-day clean — so the mirror file is still in-tree).
- Restores all callers that switched to `sqlcgen` package imports.
- Removes the generated sqlcgen package files from the tree.
- Removes `schema.gen.sql` from the tree.

Squash-merge atomicity is what makes this work. A merge-commit-with-history would require N inverse commits or a complex range inversion. ADR-1 M-8 makes the squash mandatory precisely so this inversion is one commit.

### 2.3 Post-rollback state

After rollback completes and CI is GREEN, the repository state has these properties:

- **44 hand-rolled `*_store.go` files restored** — code is back to pre-sqlc behavior.
- **`schema_mirror_test.go` restored** to active CI participation — detection-time safety net is back.
- **`pkg/quality/sqlcsync/`, `pkg/quality/sqlctxprop/`, and `pkg/quality/tenantpredicate/` gates remain dormant** — the gate code is present in the tree but unregistered from `task quality:gates` (i.e., `taskfiles/quality.yml` does not invoke them post-rollback). The gates are dormant so we can do forensic analysis on them without re-litigating their value.
- **`schema.gen.sql` archived** — file is removed from `api/internal/db/` but a copy is preserved under `.claude/knowledge-base/audits/2026-05-29-sqlc-rollback/` for forensic reference.
- **Render job disabled** — `infra/scripts/ci/render-schema-gen-sql.sh` is removed from `taskfiles/quality.yml` and from any CI workflow that invoked it.
- **ADR-1 status flipped to "Superseded by rollback MEET-YYYY-MM-DD-NNN"** with reference to the rollback meeting that triggered T-1..T-6.

### 2.4 Post-rollback decision tree

Within 24 hours of rollback completion, three roles convene: Paulo (operator/sponsor), the sre-engineer reviewer (Security & Blast Radius VETO holder from ADR-1), and verification-architect (Production Design VETO holder from ADR-1). The convened group classifies the root cause into one of three outcomes:

| Outcome | Root cause class | Next step |
|---|---|---|
| (a) sqlc parsing edge case | sqlc emitted incorrect code for a specific PG construct (e.g., LATERAL, recursive CTE, specific cast) | Fix the `queries.sql` file to use a sqlc-friendly equivalent; re-attempt migration under same plan (new branch off `develop`); same ADRs apply; no new meeting required |
| (b) Process gap | A missed pair-review (M-2), missed AST gate trigger (M-9), missed `withEnvironmentLock` audit (M-17), or missed `Transition()` audit (M-18) | Fix the process gap (e.g., automate the missed audit); re-attempt migration under same plan; new T2 meeting documents the process correction |
| (c) Fundamental sqlc unsuitability for LoopZero | sqlc cannot type a critical query pattern LoopZero depends on; raw escape would dominate the package | Re-evaluate alternatives (jet, hand-rolled + more gates, custom code generator); new T3 meeting; ADR-1 + ADR-2 superseded with explicit reference to this rollback meeting |

The three outcomes are mutually exclusive. Convergence on one outcome within 24h is the gate that closes the rollback-active state.

### 2.5 Observation window

| Property | Value |
|---|---|
| Default rollback window | 7 days post-merge |
| Default response inside window | Rollback (not investigate-while-merged) |
| Window expiry behavior | After 7 days clean, this ADR transitions from "active rollback authorization" to "historical record" |
| Post-window rollback requirement | New T3 meeting required; this ADR no longer pre-authorizes rollback outside the window |

The 7-day window is calibrated to match the `task dogfood:full` cycle on cleanroom-fresh infra + drill iter cadence + Paulo's M-15 daily diary checkpoint. Inside 7 days, rollback is cheap (one inverse commit, one re-validation cycle). Outside 7 days, downstream commits accumulate that would conflict with the inversion, so rollback becomes a coordinated multi-PR operation — that's a new T3 decision, not this ADR's authorization.

### 2.6 Ownership during observation window

| Role | Owner |
|---|---|
| Final rollback decision | Paulo (operator) |
| Rollback execution (creates the inverse commit per § 2.2) | Ralph (AI executor) — operating under explicit operator authorization for this single carved-out exception |
| Evidence capture (incident timeline, failing CI logs, dogfood/drill outputs) | Ralph |
| Daily check-in | Paulo via M-15 diary (ADR-1) — already in place during migration |
| Post-rollback convening | Paulo + sre-engineer + verification-architect within 24h |

Paulo's authority to invoke rollback is pre-granted by MEET-2026-05-29-001. No additional meeting is required to execute § 2.2 once a T-1..T-6 trigger fires.

---

## 3. Alternatives Considered

| Option | Rationale | Why rejected |
|---|---|---|
| **No formal rollback ADR; trust operator judgment in the moment** | Less ceremony | SRE reviewer flagged this as the single largest production-fitness risk. Operator judgment under post-incident time pressure produces incomplete inversions. |
| **Forward-fix-only (no rollback authorization)** | Pre-release means we can fix-forward | Disregards SRE veto. Forward-fix could compound the underlying drift problem if root cause is a sqlc edge case — every forward-fix pushes more sqlc-generated code into production. |
| **Time-boxed rollback window of 14 days** (matches branch lifetime cap) | Symmetry with D3 | 14 days post-merge means downstream commits accumulate that conflict with the inversion. 7 days is the empirically-calibrated window where rollback is one-commit-clean. |
| **Rollback triggered by quantitative SLO thresholds only** | Avoids judgment calls | Rejected: P0 incidents are too severe to wait for quantitative thresholds. 1 × P0 is the trigger, not "1 × P0 + N minutes". |
| **Auto-rollback on T-3/T-4/T-5/T-6 without operator** | Faster recovery | Rejected: rollback inverts 44 file changes; auto-execution risks silent data inconsistencies if the migration left state mid-transition. Operator-witnessed only. |
| **Allow non-squash merge to develop, plan a per-commit inversion** | More granular rollback | Rejected: complicates the rollback mechanism (N inverse commits, ordering risk); squash makes it one atomic operation. ADR-1 M-8 enforces squash specifically to enable this ADR's mechanism. |

---

## 4. Trade-offs Accepted

| Trade-off | Why accepted | Mitigation |
|---|---|---|
| Pre-authorized rollback bypasses `.claude/rules/workflow.md` default workflow prohibition | Discipline-level rule must yield to operationally-authorized exception | This ADR is the explicit carve-out; operator-witnessed; on `develop` only; never to amend history |
| 7-day window means a P0 on day 8 requires a new T3 meeting | Window must be finite to avoid open-ended pre-authorization | 7 days matches dogfood + drill + diary cadences; any post-window incident is a new decision |
| Dormant-gate state in post-rollback tree | Could confuse future maintainers ("why is this code unreferenced?") | Forensic analysis is the explicit purpose; dormant state documented in ADR-1 update post-rollback; cleanup PR follows convening (§ 2.4) |
| 24h convening pressure post-rollback | Hard to schedule reviewers | Same VETO holders as MEET-2026-05-29-001; agenda is pre-structured (a/b/c classification) |
| `schema.gen.sql` and gate code remain in tree post-rollback | Some "dead code" accumulates | Forensic value > cleanliness; cleanup is a separate PR post-convening |

---

## 5. Consequences

### 5.1 Positive

- **Pre-authorized, time-bounded rollback path.** Operators don't improvise under pressure.
- **Atomic mechanism.** Single inverse commit against the squash-merge SHA. CI revalidates the inverted state.
- **Convening discipline.** 24h post-rollback root-cause classification prevents re-litigation in subsequent attempts.
- **VETO-holder alignment.** Both Security & Blast Radius and Production Design APPROVE conditional precisely because this ADR exists and pre-authorizes rollback.
- **Sponsor + Ralph operating model preserved.** Same committee that executed MEET-001 closure handles rollback if needed.

### 5.2 Negative (accepted)

- **Workflow-rule exception.** `.claude/rules/workflow.md` default prohibition is bypassed here; documented as a carve-out, not a precedent.
- **Forensic clutter.** Dormant gates + archived `schema.gen.sql` add tree state that's only meaningful in convening context.
- **24h convening pressure.** Same-day reviewer scheduling is hard.
- **Window is finite.** Day-8 incidents fall outside pre-authorization.

### 5.3 Risks (explicit, with mitigations)

| Risk | Severity | Mitigation |
|---|---|---|
| Trigger fires but operator delays rollback | High | M-15 diary forces daily Paulo check-in; 7-day window expiry forces decision |
| Rollback inverse commit has merge conflicts (downstream commits between sqlc merge and rollback) | High | Daily merges to `develop` post-sqlc-merge must be tested via `task dogfood:full`; if conflicts appear, rollback PR includes coordinated fix |
| Re-validation after rollback fails (inversion leaves tree in unbuildable state) | High | § 2.2 step 3 mandates re-validation BEFORE push to `develop`; if RED, escalate to convening immediately |
| Convening cannot classify into (a/b/c) within 24h | Medium | Default to (c) "fundamental sqlc unsuitability" if classification stalls; defer to new T3 meeting |
| Window-expiry rule is invoked aggressively (day-8 incident dismissed because "out of window") | Medium | New T3 meeting is the gate, not dismissal; window expiry means rollback is no longer pre-authorized, not that incidents are ignored |
| Forensic archival of `schema.gen.sql` consumes disk in `.claude/knowledge-base/` | Low | File is small (<200 KB); knowledge-base already absorbs audit snapshots |

---

## 6. Validation Plan

Validation of this ADR happens BEFORE ADR-1 implementation begins:

| Phase | Description | Gate it satisfies |
|---|---|---|
| Pre-V1 | This ADR is committed to `docs/decisions/` BEFORE the first sqlc migration commit lands. Verified by reading the file at the sqlc branch's first commit. | ADR-1 M-6 |
| Pre-V1 | The rollback mechanism (§ 2.2) is dry-run against a synthetic squash-merge SHA on a scratch branch (no actual rollback) to prove the steps work. | Rehearsal |
| Post-merge | Paulo's M-15 daily diary explicitly checks trigger conditions T-1..T-6 daily for 7 days. | T-1..T-6 detection |
| Post-merge | At day 7 clean, the ADR transitions to "historical record" status; transition is explicit (not implicit). | § 2.5 |

---

## 7. Mandatory Actions (inherited from ADR-1)

ADR-3 specifically depends on these ADR-1 mandatory actions:

| ID | Action | Why ADR-3 needs it |
|---|---|---|
| M-6 | Pre-author ADR-3 before any code lands | THIS ADR is M-6's output |
| M-8 | Squash-merge enforced | Makes § 2.2 atomic |
| M-15 | Daily diary by Paulo | Detection mechanism for T-1..T-6 |
| M-21 | `task quality:gates` advisory-lock gate must stay GREEN; any RED triggers ADR-3 rollback evaluation per § 2.1 T-3 | Operationalizes T-3 |
| M-7 | Drill iter 55 GREEN baseline; iter ≥56 GREEN post-merge | Operationalizes T-6 |

No new mandatory actions are introduced by ADR-3 beyond these.

---

## 8. Cross-references

- **Meeting record:** MEET-2026-05-29-001 (12 specialists; both VETO holders APPROVE conditional — conditional on THIS ADR existing pre-implementation)
- **Coupled ADRs:**
  - [`2026-05-29-sqlc-adoption.md`](./2026-05-29-sqlc-adoption.md) (ADR-1 — the migration this rolls back)
  - [`2026-05-29-schema-gen-sql-render.md`](./2026-05-29-schema-gen-sql-render.md) (ADR-2 — render pipeline; rollback removes the pipeline)
- **Governance rules:**
  - `.claude/rules/pre-release-governance.md` (T3 rollback discipline; pre-release does not relax rollback authorization)
  - `.claude/rules/workflow.md` (general workflow prohibition; this ADR is the documented carve-out)
- **Operator-action precedent:**
  - Memory `feedback_ralph_loop_engineering_completion_pattern` (HARD STOP semantics — Ralph cannot self-trigger T-1..T-6; operator-witnessed only)
  - Memory `feedback_full_execution_authorization` (sponsor-direct authorization model precedent)
- **Code paths affected by rollback execution:**
  - `develop` branch (single inverse commit lands here)
  - `api/internal/db/` (restored to pre-merge state)
  - `taskfiles/quality.yml` (gate de-registration: `sqlcsync`, `schemarender`)
  - `api/internal/db/schema.gen.sql` (removed; archived to `.claude/knowledge-base/audits/2026-05-29-sqlc-rollback/`)
  - `infra/scripts/ci/render-schema-gen-sql.sh` (removed; allowlist entry M-2A removed in same PR)

---

## 9. Decision Record metadata

| Field | Value |
|---|---|
| Sponsor | Paulo (operator) |
| Challenger | sre-engineer (Security & Blast Radius VETO holder); production-design reviewer (Production Design VETO holder) |
| Both VETO holders | APPROVE WITH CONDITIONS — the primary condition is THIS ADR existing pre-implementation |
| Decision date | 2026-05-29 |
| Ratification status | RATIFIED (operator-locked at meeting; pre-authorizes rollback per § 2.1 triggers) |
| Owner of rollback decision | Paulo (operator) |
| Owner of rollback execution | Ralph (AI executor) under operator-witnessed carve-out |
| Observation window | 7 days post-merge |
| Authorization scope | Single squash-merge SHA on `develop`; new T3 meeting required outside window |
