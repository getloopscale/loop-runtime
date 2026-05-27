# Plan: Script Minimization Program (140 → ≤30) — Master Sequencing

> **Version 1.0** — Master program plan that sequences three migration tracks to drive the canonical shell-script count from **140 → ≤30** WITHOUT destroying coverage or operational capability. Deletion of dead-weight is exhausted (36 scripts removed 2026-05-24, every remaining orphan candidate proved to be a false positive or regression-matrix-referenced). The only remaining lever is **migration**: move operational logic to its strategic home (helmfile declarative / Go gates / Ginkgo specs / loopzero-ops CLI), validate LIVE, then delete the bash. This master plan defines the order, the gates between tracks, and the program-level KPI; the three child plans (`*-parity-to-ginkgo-*`, `*-drill-consolidation-*`, `*-bootstrap-helmfile-eso-*`) hold the per-track tasks.

## Context

After the 2026-05-24 necessity audit + trash-validate-delete cycle (commits `19d0171c`, `131091bb`, `4d7d4be3`, `61722a84`, `83fecb2e`), the repo is at **140 canonical `.sh` files** (excluding `.claude/worktrees/`). The `unhomed-logic` tracker reports **104 un-homed** scripts (`bootstrap=32, drill=20, chaos=5, test=46, other=1`). The team's documented target (`docs/plans/2026-05-22-shell-script-strategic-plan.md` D3) is **≤30**.

**Evidence that deletion is exhausted:**
- A repo-wide 0-execution-ref sweep flagged 67 candidates; per-script verification proved ALL were false positives: glob-sourced libs (`cell2-gate-c*.sh` via `source "$LIB_DIR/cell2-gate-${g}.sh"`), harness hooks (`.claude/hooks/loop-stop.sh`), Ginkgo-invoked (`velero-dr-drill.sh`), skill-invoked (`dr/*-orchestrator.sh`), KEEP-OPERATOR tools, and **regression-matrix-referenced** verification tokens (`cell2-canary-stage1.sh:91-95`, `audit-argocd-drift.sh` — the `audit-regression-matrix` gate blocked their deletion).
- The 36 already-removed were genuinely dead: 1 superseded orchestrator, 33 unwired hermetic tests (no runner since GitHub Actions removal), 1 over-strict gate, plus reference cleanup.

**The blocker the delegation map already surfaced** (`docs/operations/helmfile-delegation-map.md`, task #176): of the 5 `DELEGATE-HELMFILE` bootstrap scripts, **only 1 is a clean delegation**; 3 are secret-generators blocked on an **ESO-generator T2/T3 architecture decision** (must preserve the determinism contract that the 2026-05-22 cleanroom proved load-bearing). So "bootstrap → helmfile" is the SMALLEST and HIGHEST-RISK track, not the largest — this master plan corrects the initial ordering assumption with that evidence.

## Objective

**Done = canonical `.sh` ≤ 30, with zero loss of operational capability or test/security coverage, every removal LIVE-validated.**

Measurable goals:
- Program KPI: `find . -name '*.sh' -not -path '*/.claude/worktrees/*' | wc -l` ≤ 30.
- `unhomed-logic` baseline ratcheted monotonically down to ≤ 20 (un-homed operational logic).
- Each track: zero regression in `task quality:all`, `/cross-validation`, and the track's LIVE gate (parity suite / DOKS drill / cleanroom) before its bash is deleted.
- No coverage lost: every migrated script's behavior is preserved as a Ginkgo spec, Go gate, helmfile value, or loopzero-ops subcommand BEFORE the bash is removed.

## ADRs

- **D1 — Migration, not deletion, is the only remaining lever.**
  - *Decision:* From 140, reduce by MOVING logic to its strategic home (Go/Ginkgo/helmfile/loopzero-ops) and deleting bash only after the new home is LIVE-validated. No more bulk deletion.
  - *Rationale:* The trash-validate-delete loop + regression-matrix gate proved there is no remaining safe dead-weight. Deleting functional operational logic without migrating breaks setup/drill/chaos.
  - *Consequences:* Each reduction now costs a port + LIVE validation. Slower but safe. Enables the ≤30 target without coverage loss.

- **D2 — Strangler-fig, one unit per PR, LIVE gate before delete.**
  - *Decision:* Per script (or tight cluster): (1) port logic to the new home, (2) prove parity vs bash, (3) rewire ALL caller surfaces (Taskfile / skill / helmfile / go-test), (4) run the track's LIVE gate, (5) delete bash, (6) drop allowlist row + ratchet baseline, (7) `dangling-script-refs` clean.
  - *Rationale:* The `develop-bkp` big-bang failure (deleted 11 dr/ scripts, left 4 SKILLs dangling) proved big-bang migration fails at the integration boundary. The `danglingrefs`/`danglingdata` backstops + per-PR discipline prevent recurrence.
  - *Consequences:* Many small PRs. Each independently revertible. The backstop gates enforce the rewire.

- **D3 — Track order = biggest clean win first, highest arch-risk last.**
  - *Decision:* Execute **Track B (parity → Ginkgo)** first, then **Track C (drill → Go/consolidation)**, then **Track A (bootstrap → helmfile/ESO)** last.
  - *Rationale:* Evidence (delegation map) shows Track A yields only ~1–4 scripts and is blocked on an ESO-generator T2/T3 MEET + the load-bearing determinism contract — smallest yield, highest risk. Track B has the largest clean yield (~20 scripts) with an existing home (the Ginkgo e2e suite) and a self-contained LIVE gate (the parity suite). Track C is moderate. Do the high-yield/low-risk work first so the program shows ≤30 progress before touching the risky credential path.
  - *Consequences:* The user's initial bootstrap-first ordering is overridden by evidence; Track A may be partially deferred (the 3 secret-generators) pending the ESO MEET without blocking the ≤30 goal if B+C suffice.

- **D4 — A track's child plan STARTS only when the prior track's DoD + LIVE gate are GREEN.**
  - *Decision:* Sequential gating. Track C does not begin until Track B's parity LIVE gate passes and its bash is deleted. Track A does not begin until Track C's DOKS drill gate passes.
  - *Rationale:* Each track mutates the same `unhomed-baseline` + allowlist + cleanroom path; concurrent tracks would race the baseline ratchet and confound LIVE validation.
  - *Consequences:* No parallelism across tracks. Within a track, independent scripts may batch.

- **D5 — Regression-matrix + coverage are preserved across migration.**
  - *Decision:* When a bash script referenced by `docs/runbooks/bootstrap-regression-matrix.md` is migrated, the matrix entry's verification token is updated to point at the new home (Ginkgo spec / Go gate) in the SAME PR. `audit-regression-matrix` must stay GREEN.
  - *Rationale:* The matrix gate blocked deleting `cell2-canary-stage1.sh` / `audit-argocd-drift.sh` because they are documented verification methods. Migration must re-point, not orphan, those references.
  - *Consequences:* Matrix edits are part of each migration PR, not a follow-up.

## Dependency Graph

```
        ┌─────────────────────────────────────────────────────────┐
        │  PROGRAM KPI: canonical .sh ≤ 30  (currently 140)         │
        └─────────────────────────────────────────────────────────┘
                                  │
   Track B (parity → Ginkgo)  ~20 scripts  ── LIVE gate: parity suite GREEN
   docs/plans/2026-05-24-parity-to-ginkgo-migration-plan.md
                                  │  (DoD + parity LIVE GREEN + bash deleted)
                                  ▼
   Track C (drill → Go/consolidation)  ~20 scripts ── LIVE gate: DOKS drill GREEN
   docs/plans/2026-05-24-drill-consolidation-migration-plan.md
                                  │  (DoD + drill LIVE GREEN + bash deleted)
                                  ▼
   Track A (bootstrap → helmfile/ESO)  ~4 + ESO-MEET-gated ── LIVE gate: cleanroom GREEN
   docs/plans/2026-05-24-bootstrap-helmfile-eso-migration-plan.md
                                  │  (ESO-generator MEET → cleanroom LIVE GREEN)
                                  ▼
        ┌─────────────────────────────────────────────────────────┐
        │  Program close-out: ≤30 confirmed + unhomed ≤20 + dogfood │
        └─────────────────────────────────────────────────────────┘
```

Tracks are **strictly sequential** (D4). Within each track, independent scripts batch (parallel within-track).

---

## Autonomous Progress Ledger (2026-05-24) — operator resume point

> Scope-refinement record (not a DoD change). Captures exactly what the Ralph loop completed autonomously and where the operator gates begin, so a resume is O(1).

**DONE autonomously + LIVE-validated + pushed (`origin/develop`):**

| Item | Evidence | Commits |
|---|---|---|
| Phase 0 T0.1 — `script-count` gate | live; baseline 140→**124** | (earlier) + ratchets |
| Track B TB1.1 — cell-2 gates → `parity_cell2_test.go` | 14 bash deleted; LIVE 5 Passed/0 Failed | `6f351e5e` `5c1f832b` |
| Track B TB5 — matrix audit → `loopzero-ops quality regression-matrix` | 2 bash deleted; gate now scans (166 entries, miss=0); golden→`TestGolden_*` | `a6a3d46a` `3bd625fa` |
| Findings (3) — C1 secret fragility, set-e abort, parity-scripts-are-procedures | 3 finding docs | `7734dc87` `d2a68501` `bedd510b` |

**State now:** canonical `.sh` = **124** · unhomed = **88** · `task quality:gates` GREEN · `pkg/quality/...` GREEN · e2e compiles.

**Track B residue — reclassification PENDING operator ratification** (`MEET-ASYNC-AMENDMENT-2026-05-24-001`): TB2/TB3/TB4 (cell2-canary-stage1/2, cell2-rollback-slo, audit-argocd-drift, k3d-virgin-install) are operator procedures/diagnostics with **no routine invariant left to migrate** (the invariants were absorbed by TB1.1+TB5) → reclassify KEEP-OPERATOR, do NOT author hollow Ginkgo specs. Track B expected-removal 20–22 → **16 (actual)**.

**OPERATOR-GATED — cannot proceed autonomously (the loop's hard ceiling):**
1. **Ratify** `MEET-ASYNC-AMENDMENT-2026-05-24-001` (Track B KEEP-OPERATOR reclassification) → then edit the 5 allowlist disposition rows + parity Coverage Matrix.
2. **D4 gate:** full Track B DoD (parity suite LIVE + `/cross-validation`) before Track C may begin.
3. **Track C** (Phase 2): autonomous PORT of pure-logic drill pieces is possible, but its LIVE gate is `task drill:l2-scale` (**DOKS, real money — operator-run**); D4 forbids starting it before #2.
4. **Track A** (Phase 3): blocked on the **ESO-generator governance MEET** (determinism contract) + D4.
5. **Program close-out:** `task clean:all && task setup && tilt up` + `/dogfood full` (operator destroys/recreates infra).

The Ralph completion promise is therefore **unreachable autonomously** — the ≤30 KPI depends on Tracks C+A, both operator/governance-gated.

---

## Phase 0: Program Bootstrap (this plan)

**Objective:** Establish the program KPI, the per-track gate contract, and the cross-track invariants before any track starts.

### T0.1 — Program KPI gate + per-track baseline contract

#### Objective
Define and wire a program-level check that the canonical `.sh` count trends monotonically toward ≤30 and never regresses, and document the per-track LIVE-gate contract each child plan must satisfy.

#### Evidence
`unhomed-logic` already enforces a monotonic per-domain ratchet (`infra/quality/unhomed-baseline.json`, total 104). The program needs the SAME discipline on the raw canonical `.sh` count so a migration that adds a Go gate but forgets to delete the bash (the "half-migration leftover" pattern from `reference_bash_to_go_migration`) is caught.

#### Files to edit
```
infra/quality/script-count-baseline.json (NEW) — { "canonical_sh": 140 } monotonic ceiling
cmd/loopzero-ops/quality_subcmd.go — add `script-count` gate (count canonical .sh, fail if > baseline)
pkg/quality/scriptcount/check.go (NEW) + check_test.go (NEW) — pure-logic count + ceiling compare
taskfiles/quality.yml — add `/tmp/loopzero-ops quality script-count` to the `gates` target
docs/plans/2026-05-24-script-minimization-program-plan.md — this file (the contract of record)
```

#### Deep file dependency analysis
- `pkg/quality/scriptcount/check.go` (NEW): mirrors the `unhomed-logic` ceiling pattern — walk the repo, exclude `.claude/worktrees/`, `node_modules`, `.git`; count `*.sh`; read baseline JSON; fail if count > baseline. Downstream: `cmd/loopzero-ops/quality_subcmd.go` dispatches it; `taskfiles/quality.yml` runs it.
- `cmd/loopzero-ops/quality_subcmd.go`: add the dispatch case + runner following the existing `runX(args []string) int` + `flag.FlagSet` pattern (NOT the develop-bkp `runQuality...(_ *slog.Logger) error` framework — see `reference_bash_to_go_migration`). Backtick-in-help pitfall applies.
- `infra/quality/script-count-baseline.json` (NEW): the monotonic ceiling. Each track PR that deletes bash ratchets it down.

#### Deep Dives
- **Invariant:** a migration PR may NEVER increase `canonical_sh`. It either holds (port-only, pre-delete) or decreases (post-delete). This catches the half-migration leftover (Go gate added, bash not deleted) at the program level even when `unhomed-logic` (which counts allowlist dispositions) would not.
- **Edge case:** the gate must exclude `.claude/worktrees/` (separate git checkouts) — the same exclusion the necessity audit used. A worktree with 45 dup scripts would otherwise corrupt the count.
- **Edge case (EC-1, MUST FIX):** the gate MUST ALSO exclude `trash/` (`-not -path '*/trash/*'`). During a migration, a script `git mv`'d to `trash/` is still tracked → `find` would still count it, giving a false verdict in the trash→delete window. Baseline ratchet happens ONLY after `trash/` is deleted (never on the intermediate state).
- **Edge case:** NEW `.sh` are already forbidden by `no-new-bash`; `script-count` is the complementary ceiling (no-new-bash blocks additions not in the allowlist; script-count blocks net-count regression).

#### Tasks
1. Write `pkg/quality/scriptcount/check_test.go` (RED) — table tests: count==baseline PASS, count<baseline PASS+ratchet-hint, count>baseline FAIL, worktree-excluded, **trash-excluded (EC-1)**.
2. Implement `pkg/quality/scriptcount/check.go` (GREEN).
3. Add `script-count` subcommand to `cmd/loopzero-ops/quality_subcmd.go` (dispatch + runner + help, no backticks in help).
4. Create `infra/quality/script-count-baseline.json` = `{"canonical_sh":140}`.
5. Wire `/tmp/loopzero-ops quality script-count` into `taskfiles/quality.yml` `gates`.
6. `go build ./...` both modules; `task quality:gates`.

#### TDD
```
RED:     TestScriptCount_EqualsBaseline_Pass — count==140, baseline 140 → exit 0
RED:     TestScriptCount_BelowBaseline_Pass — count==139, baseline 140 → exit 0 (ratchet hint)
RED:     TestScriptCount_AboveBaseline_Fail — count==141, baseline 140 → exit 5
RED:     TestScriptCount_ExcludesWorktrees — worktree .sh not counted
RED:     TestScriptCount_ExcludesTrash — trash/ .sh not counted (EC-1: git-mv'd-but-tracked window)
GREEN:   implement scriptcount.Check + loopzero-ops dispatch
REFACTOR: none expected
VERIFY:  cd cmd/loopzero-ops && go test ./... && /tmp/loopzero-ops quality script-count
```

#### Acceptance Criteria
- [x] `script-count` gate PASS (now at **124**, ratcheted from 140); FAIL if a PR adds a net `.sh`.
- [x] Wired into `task quality:gates`.
- [x] Worktrees excluded (+ `trash/` per EC-1).
- [x] Pass: gate file `pkg/quality/scriptcount/check.go` < 500 lines, 7 tests GREEN.

#### DoD
- [x] `pkg/quality/scriptcount` tests green (7/7).
- [x] `go build` both modules.
- [x] `task quality:gates` green (baseline ratcheted 140→126→124 as Track B deleted scripts).
- [x] `dangling-script-refs` + `taskfile-interface` green.

> **DONE 2026-05-24** — gate live (`pkg/quality/scriptcount` + `loopzero-ops quality script-count`).

---

## Phase 1 → Track B: Parity → Ginkgo (CHILD PLAN)

**Objective:** Migrate the ~20 `infra/tests/parity/` LIVE prod-parity validation scripts to Ginkgo specs in `infra/tests/e2e/` (the existing home), re-pointing the regression matrix, deleting the bash after the parity suite passes LIVE. **Largest clean yield, lowest arch risk — runs FIRST.**

> Full tasks in `docs/plans/2026-05-24-parity-to-ginkgo-migration-plan.md`. Starts immediately after Phase 0 T0.1. LIVE gate: the parity suite (k3d-virgin / doks-install / droplet-install / cell2-activation) must pass as Ginkgo specs with parity to the bash output BEFORE the bash is deleted.

---

## Phase 2 → Track C: Drill → Go/Consolidation (CHILD PLAN)

**Objective:** Consolidate the ~20 `infra/scripts/dr/` + `drill-*` + `chaos/` orchestrators — port pure-logic pieces to Go/loopzero-ops, consolidate duplicated orchestration, rewire the 4 drill skills, delete bash after a DOKS drill passes LIVE.

> Full tasks in `docs/plans/2026-05-24-drill-consolidation-migration-plan.md`. Starts ONLY after Track B DoD + parity LIVE GREEN. LIVE gate: `task drill:l2-scale` (DOKS prod-identical) GREEN with the new orchestration BEFORE bash deletion. HARD STOP: real-money DOKS provisioning = operator-run.

---

## Phase 3 → Track A: Bootstrap → Helmfile/ESO (CHILD PLAN)

**Objective:** Delegate the 1 clean bootstrap script (`bootstrap-patch-admission-policies` → chart values) and, gated on an ESO-generator T2/T3 MEET, migrate the 3 secret-generators to ESO `Password` generator CRDs preserving the determinism contract. Smallest yield, highest risk — runs LAST.

> Full tasks in `docs/plans/2026-05-24-bootstrap-helmfile-eso-migration-plan.md`. Starts ONLY after Track C DoD + DOKS drill GREEN. Phase 3.1 (the 3 secret-generators) is BLOCKED until the ESO-generator MEET ratifies the determinism-preserving design. LIVE gate: `/cleanroom` + `/dogfood full` GREEN (credential determinism is the dominant cleanroom-regression theme).

---

## Coverage Matrix

| # | Gap / Requirement | Track / Task | Resolution |
|---|---|---|---|
| 1 | 140 → ≤30 canonical scripts | Program KPI T0.1 + B/C/A | Migrate logic to strategic home, delete bash post-LIVE |
| 2 | parity scripts (~20) un-homed | Track B | → Ginkgo e2e specs |
| 3 | drill scripts (~20) un-homed | Track C | → Go/loopzero-ops + consolidation |
| 4 | bootstrap scripts (~32, ~1–4 delegatable) | Track A | → helmfile values (1) + ESO generators (3, MEET-gated); rest KEEP-OPERATOR |
| 5 | no half-migration leftover (Go added, bash kept) | T0.1 script-count ceiling | program-level count gate |
| 6 | regression-matrix references not orphaned | D5 + per-track | re-point matrix tokens in same PR |
| 7 | no coverage/capability loss | D2 + per-track LIVE gates | new home validated BEFORE bash delete |

**Coverage: 7/7 program requirements covered (100%)**

## Global Definition of Done

- [ ] All three track plans completed and their LIVE gates GREEN
- [ ] `script-count` ≤ 30 (program KPI)
- [ ] `unhomed-logic` baseline ≤ 20
- [ ] All tests passing (Go + TS + Ginkgo)
- [ ] Zero lint warnings
- [ ] `dangling-script-refs` + `dangling-data-refs` + `taskfile-interface` + `audit-regression-matrix` GREEN
- [ ] code-audit checks passing
- [ ] **`/cross-validation` PASSED for each track** — zero CRITICO, zero GRAVE
- [ ] N/A — this program plan does not itself affect maturity scores (it is tooling hygiene)
- [ ] **`/dogfood full` PASSED** after the final track (see Dogfood Gate)

---

## Dogfood Gate (MANDATORY — NON-NEGOTIABLE)

> No track is DONE without its LIVE gate, and the PROGRAM is not DONE without a final `/dogfood full` on clean infrastructure proving the migrated tooling still brings up + operates the platform end-to-end.

### Execution

```bash
task clean:all && task setup && tilt up
task doctor   # verify health
/dogfood full
```

### Pass Criteria

- [ ] `/dogfood full` on clean infra after the final track
- [ ] Pre-Flight: ALL PASS (the migrated bootstrap/parity/drill tooling brought up the platform)
- [ ] Zero S0; zero S1 in program scope
- [ ] Prova de Vida / Prova de Morte pass
- [ ] No regressions in `task quality:all`
- [ ] `script-count` ≤ 30 confirmed on the clean tree

### On Failure

If `/dogfood full` finds S0/S1: a migration dropped capability. Restore the bash from git for the offending unit, re-open the track, fix the port, re-run the track LIVE gate, then re-run `/dogfood full`. Repeat until zero S0/S1.
