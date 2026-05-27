# Plan: Script Cleanup — Reduce bash from 221 → ≤30 by migration, not deletion

> **Version 1.0** — A focused, evidence-grounded cleanup plan that reduces the repo's shell-script
> count from **221** to the strategic target **≤30**. The key finding from the 2026-05-22 LIVE
> session is that **naive deletion is unsafe** — a reference-graph analysis proved the "unreferenced"
> scripts are almost all genuinely used via mechanisms grep misses (dynamic sourcing, CI glob-runners,
> Dockerfile ENTRYPOINT, operator-manual pre-flights). Dead-code deletion is **saturated** (~1 truly
> dead script found across the whole campaign). Therefore the reduction comes from **replacing bash
> with native orchestration** (helmfile / Ginkgo / Go `pkg/quality`), each validated before the bash
> is deleted. This plan separates the **autonomous-safe** slice (pure-logic Go gates) from the
> **operator-gated LIVE** slice (Phase 2 helmfile + Phase 4 Ginkgo) so neither blocks the other and
> neither risks the cleanroom that currently passes dogfood (106/0) + chaos (10/10).

## Context

The parent plan `docs/plans/2026-05-22-shell-script-strategic-plan.md` (v1.3) set the strategic target
N(bash) ≤ 30. Its status tracker (`...-status.md` iter 41) records the current reality:

- **221 `.sh` files** remain (down from 269; ~46 deleted cumulatively, mostly `_legacy/` + read-verified dead).
- **Dead-code detection is saturated** — iter 38-40 tried two heuristics (orphan-test, zero-reference);
  both produced false positives. Exactly **1** conclusively-dead script was found (`regenerate-status.sh`).
- **30 Go quality gates** already migrated to `pkg/quality/*` (T5.3 partial).
- **3 LIVE gates GREEN** this session: cleanroom converged, `/dogfood full` 106/0, `/chaos-test --full` 10/10.

Fresh reference-graph evidence gathered this session (path-based, excluding bookkeeping docs):

| "Orphan" group | Count | Why it's NOT dead (grep misses it) |
|---|---|---|
| `infra/tests/parity/lib/cell2-gate-c*.sh` | 13 | Dynamically sourced: `lib_file="$LIB_DIR/cell2-gate-${g}.sh"` (`cell2-activation-gates.sh:76`) |
| `infra/tests/**/*_test.sh` | ~18 | Run by CI glob-runners (`drill-hermetic-tests.yml` globs `infra/tests/scripts/drill-*_test.sh`; quality-gates.yml, coverage-report.yml, helm-charts.yml, taskfiles/test.yml) |
| `cloud/dashboard/docker-entrypoint.sh` | 1 | `ENTRYPOINT` in `cloud/dashboard/Dockerfile` (container runtime) |
| `infra/scripts/check-prod-vap-readiness.sh` | 1 | KEEP-OPERATOR — manual prod pre-flight (allowlist) |

The remaining ~221 break down (approx, from allowlist dispositions):

| Category | ~Qty | Reduction path | Risk |
|---|---|---|---|
| `*_test.sh` testing other `.sh` | ~55 | D6: die WITH the `.sh` they test | None (auto-die) |
| parity/dogfood orchestrators | ~30 | **Phase 4** → Ginkgo specs + repoint CI | LIVE (multi-cell/drill validation) |
| helmfile/bootstrap | ~25 | **Phase 2** → `helmfile sync` | LIVE (cleanroom rebuild) |
| dr/drill orchestration | ~20 | LIVE by nature — KEEP | N/A |
| pure-logic quality gates | ~20 | **T5.3 cont.** → `pkg/quality/*` Go | None (hermetic) |
| chaos / cleanroom orchestration | ~15 | operator/skill tooling — KEEP | N/A |
| genuinely operator/KEEP | ~30 | D4-justified — KEEP in allowlist | N/A |

## Objective

**Done = N(bash) ≤ 30, every deletion preceded by a validated native replacement, zero CI/cleanroom
regression.** Measurable goals:

1. Autonomous-safe slice complete: all remaining **pure-logic** gates migrated to `pkg/quality/*` Go
   (data-level parity proven by hermetic test), their `.sh` + `*_test.sh` deleted.
2. Operator-gated LIVE slice complete: parity/dogfood orchestrators replaced by Ginkgo (Phase 4) and
   bootstrap scripts delegated to helmfile (Phase 2), each validated LIVE before bash deletion.
3. `.claude/rules/shell-scripts-allowlist.md` shrunk to ≤30 entries; `no-new-bash` gate GREEN.
4. No regression: `/dogfood full` 106/0 + `/chaos-test --full` 10/10 still pass after cleanup.

## ADRs

### D1 — Migration, not deletion, is the reduction mechanism
- **Decision:** Reduce N(bash) by replacing each script's functionality with native orchestration
  (helmfile / Ginkgo / Go), deleting the bash only after the replacement is validated. Do NOT bulk-delete
  by heuristic.
- **Rationale:** A LIVE reference-graph analysis (2026-05-22) proved the "unreferenced" set is dominated
  by false positives (dynamic sourcing, glob-runners, Dockerfile, operator-manual). Two prior heuristic
  campaigns saturated at ~1 truly-dead script. Bulk deletion would break CI and the cleanroom.
- **Consequences:** Enables a safe, reversible reduction. Constrains the pace — LIVE-validated migration
  is slower than `git rm`, and the big levers (Phase 2/4) require operator supervision.

### D2 — Autonomous slice and LIVE slice are decoupled
- **Decision:** Pure-logic Go-gate migration (Phase 1) runs autonomously; helmfile/Ginkgo migration
  (Phases 2-3) is operator-gated LIVE. Neither blocks the other.
- **Rationale:** Pure-logic migration has hermetic parity tests (no infra). LIVE migration risks the
  cleanroom that currently passes dogfood/chaos. Decoupling lets autonomous progress continue without
  gambling validated infra.
- **Consequences:** Phase 1 ships incrementally; Phases 2-3 wait for a supervised session.

### D3 — D6 auto-die governs test scripts
- **Decision:** A `*_test.sh` is deleted in the SAME change as the `.sh` it tests — never before, never
  orphaned.
- **Rationale:** Per the parent plan's D6. Deleting a test before its target leaves the target untested;
  deleting after leaves an orphan test referencing a missing file.
- **Consequences:** ~55 test scripts are not independently deletable; they reduce as their targets migrate.

### D4 — Replacement must prove data-level parity before deletion
- **Decision:** A bash gate/orchestrator is deleted only after its Go/Ginkgo replacement is shown to
  produce the **same classification/result** on the same inputs (hermetic for gates; LIVE for orchestrators).
- **Rationale:** "No masking" rule — a replacement that passes its own tests but diverges from the bash
  behavior is a silent regression. Parity is the contract.
- **Consequences:** Each migration task carries an explicit parity check in its DoD.

### D5 — KEEP set is explicit and allowlist-pinned
- **Decision:** Scripts that are operator-manual (break-glass, prod pre-flight), DR-by-nature, container
  entrypoints, or harness hooks are KEPT and pinned in the allowlist with a D4-justification.
- **Rationale:** These have no native equivalent (they orchestrate LIVE infra or external CLIs per D7 of
  the parent plan). Forcing them into Go would be a forbidden shell-wrap (parent D5).
- **Consequences:** The ≤30 target is the KEEP set + irreducible orchestration, not zero.

## Dependency Graph

```
Phase 0 (refresh inventory, autonomous)
   │
   ├──▶ Phase 1 (pure-logic gates → Go, AUTONOMOUS) ──▶ Phase 5 (lock + allowlist shrink)
   │                                                          ▲
   └──▶ Phase 2 (helmfile delegation, OPERATOR-GATED LIVE) ───┤
        Phase 3 (parity/dogfood → Ginkgo, OPERATOR-GATED LIVE)┘
              (Phase 2 and Phase 3 are independent; both LIVE; serialize per operator availability)
```

- Phase 0 → 1 sequential (need fresh disposition data).
- Phase 1 is fully autonomous (hermetic).
- Phases 2 + 3 are operator-gated LIVE; independent of each other and of Phase 1.
- Phase 5 (final lock) waits for all deletions.

---

## Phase 0: Refresh Inventory + Classify (autonomous)

**Objective:** Produce a fresh, false-positive-aware disposition list so later phases delete only
validated-replaceable scripts.

### T0.1 — Re-run `scripts-audit` + reference-graph with dynamic-sourcing/glob awareness

#### Objective
Regenerate the disposition CSV with a reference graph that recognizes dynamic sourcing
(`source "$VAR/x.sh"`), CI glob-runners (`*_test.sh` discovered by `find`/path-glob), Dockerfile
`ENTRYPOINT`, and harness hooks — so "orphan" no longer false-positives.

#### Evidence
This session's naive path-grep flagged 42 "orphans"; verification proved ~41 are live (cell2-gate
dynamic source, `*_test.sh` glob-runners, Dockerfile entrypoint, operator pre-flight). The existing
`pkg/quality/scriptsaudit` CSV predates this insight.

#### Files to edit
```
pkg/quality/scriptsaudit/*.go — extend caller-detection: recognize `source "$DIR/${var}.sh"` dynamic
  sourcing, CI glob patterns (find -name '*_test.sh', path globs in workflow `paths:`), Dockerfile
  ENTRYPOINT/CMD, and .claude/settings*.json hook registration as VALID references
pkg/quality/scriptsaudit/*_test.go — add fixtures for each false-positive class
docs/scripts-audit/2026-05-22.csv — regenerated disposition column (KEEP / MIGRATE-GO / MIGRATE-GINKGO
  / DELEGATE-HELMFILE / DELETE-DEAD)
```

#### Deep file dependency analysis
- `scriptsaudit` today greps for `bash <path>` / basename invocation; it does not model dynamic
  sourcing or glob discovery, so it over-reports orphans. This task adds those detectors. Downstream:
  the CSV `disposition` column drives every later phase's task list; T6.4 of the parent plan and the
  allowlist consume it.

#### Deep Dives
- Dynamic-source detector: regex `source\s+"?\$\{?[A-Za-z_]+\}?/[^"]*\$\{?[A-Za-z_]+\}?\.sh` →
  resolve the glob `lib/cell2-gate-*.sh` as referenced.
- Glob-runner detector: parse workflow `paths:` and `run:` blocks for `*_test.sh` / `find ... -name '*_test.sh'`.
- Invariant: every `.sh` gets exactly one disposition; KEEP set carries a D4-justification string.
- Edge case: a script both dynamically-sourced AND in allowlist → KEEP (not DELETE).

#### Tasks
1. Add the 4 reference-class detectors to `scriptsaudit`.
2. Add hermetic fixtures (one per class) under `pkg/quality/scriptsaudit/testdata/`.
3. Regenerate `docs/scripts-audit/2026-05-22.csv`.
4. Diff against the prior CSV; confirm cell2-gate/`*_test.sh`/entrypoint flip from orphan→KEEP/MIGRATE.

#### TDD
```
RED:     TestDetectsDynamicSourcedLib() — cell2-gate-c5.sh classified referenced (not orphan)
RED:     TestDetectsGlobRunnerTestScripts() — drill-*_test.sh classified referenced
RED:     TestDetectsDockerfileEntrypoint() — docker-entrypoint.sh classified referenced
RED:     TestDetectsHookRegistration() — .claude/hooks/*.sh classified referenced
GREEN:   Implement the 4 detectors
REFACTOR: extract a `referenceSources []Resolver` slice
VERIFY:  go test ./pkg/quality/scriptsaudit/...
```

#### Acceptance Criteria
- [ ] 4 false-positive classes classified as referenced
- [ ] CSV regenerated; `DELETE-DEAD` count is the read-verified set only
- [ ] Pass: /code-audit complexity (CCN ≤ 10), coverage ≥ 90%, lint zero, size ≤ 500 lines

#### DoD
- [ ] `go test ./pkg/quality/scriptsaudit/...` green
- [ ] CSV committed
- [ ] Zero golangci-lint warnings

---

## Phase 1: Pure-Logic Gates → Go (AUTONOMOUS)

**Objective:** Migrate every remaining `MIGRATE-GO-PUREFN` script to `pkg/quality/*` with hermetic
data-level parity, then delete the `.sh` + its `*_test.sh` together (D3).

### T1.1 — Migrate the remaining pure-logic gates (batch, per-script parity)

#### Objective
For each script the refreshed CSV marks `MIGRATE-GO` and confirms is pure-logic (no `kubectl`/`helm`/
`pulumi`/`aws` invocation — else it's shell-wrap-forbidden per parent D5 and stays bash), port to a Go
`pkg/quality/<gate>` package with a `cmd/loopzero-ops quality <gate>` subcommand, prove parity, repoint the
CI workflow, delete the `.sh` + `*_test.sh`.

#### Evidence
30 gates already migrated this way (status doc iters 36-37: `securitycontext`, `clusterimagepolicymode`,
etc.). The pattern is proven; remaining pure-logic candidates are enumerated by the CSV. The MIGRATE-GO-PUREFN
backlog was reported "exhausted" at iter 38 — Phase 0 re-derivation will confirm or surface a small remainder.

#### Files to edit
```
pkg/quality/<gate>/*.go — (NEW per gate) pure-logic port of the .sh decision
pkg/quality/<gate>/*_test.go — (NEW) hermetic tests incl. a PARITY test vs the bash on shared fixtures
cmd/loopzero-ops/*.go — register the `<gate>` subcommand
.github/workflows/<wf>.yml — repoint the job from `bash infra/scripts/.../<gate>.sh` to `loopzero-ops quality <gate>`
infra/scripts/.../<gate>.sh — DELETE (after parity proven)
infra/tests/.../<gate>_test.sh — DELETE in same commit (D3 auto-die)
.claude/rules/shell-scripts-allowlist.md — remove both deleted entries
```

#### Deep file dependency analysis
- Each gate `.sh` is invoked by exactly one CI job (the CSV records which). The Go port must produce the
  same exit code (0 PASS / non-zero FAIL) and same findings. Downstream: the workflow job + the allowlist
  rows. The `no-new-bash` gate reads the allowlist, so allowlist rows must be removed in the same change
  to keep the gate consistent.

#### Deep Dives
- Parity test pattern: run the Go gate and the (still-present) bash on identical `testdata/` fixtures;
  assert identical classification. Only after green, delete the bash.
- Invariant: a script that invokes any external CLI is NOT pure-logic → leave as bash (parent D5). The
  CSV must reflect this (`KEEP` not `MIGRATE-GO`).
- Edge case: a gate with a `--gate` and a `report` mode (e.g. securitycontext) — both modes ported.

#### Tasks
1. For each `MIGRATE-GO` pure-logic script in the CSV: port → Go, write parity test, repoint CI, delete pair, shrink allowlist.
2. Run the full `loopzero-ops quality` subcommand suite + the repointed workflow locally (act/dry-run where possible).

#### TDD
```
RED:     Test<Gate>_ParityWithBash() — Go output == bash output on shared fixtures (per gate)
RED:     Test<Gate>_ParityWithBash_EdgeFixtures() — EC-3: parity on edge fixtures (zero matches,
         multiple matches, comment-that-looks-like-code, paths with spaces) — divergence shows at borders
RED:     Test<Gate>_GateFailsOnViolation() — exit non-zero on a known-bad fixture
GREEN:   Implement each pkg/quality/<gate>
REFACTOR: share fixture loaders across gate packages
VERIFY:  go test ./pkg/quality/... && go run ./cmd/loopzero-ops quality <gate>
```

#### Acceptance Criteria
- [ ] Each migrated gate: Go output == bash output on shared fixtures (parity, D4)
- [ ] CI workflow repointed; job green
- [ ] `.sh` + `*_test.sh` deleted together; allowlist rows removed
- [ ] Pass: /code-audit complexity ≤ 10, coverage ≥ 90%, lint zero, size ≤ 500 lines per package

#### DoD
- [ ] `go test ./pkg/quality/...` green
- [ ] `go run ./cmd/loopzero-ops quality no-new-bash --base-ref origin/main --head-ref HEAD` PASS
- [ ] Zero golangci-lint warnings
- [ ] Script count decreased by 2× (gate + test) per migration
- [ ] **EC-1 (MUST FIX) — full-repo grep before delete:** `git grep -F "<basename>"` (incl. Makefile,
      `*.go` go:generate, `*.mk`, cron) returns only allowlist/CSV/self before `git rm`. Blocking.
- [ ] **EC-2 (MUST FIX) — gate actually runs after repoint:** the repointed CI job output contains the
      gate's anchor line (e.g. `no-new-bash: PASS`); no `continue-on-error`, path-filter fires. Verify via
      `act` or local run — exit 0 alone is NOT sufficient (silent no-op gate guard).

---

## Phase 2: Bootstrap Scripts → Helmfile (OPERATOR-GATED LIVE)

**Objective:** Delegate `infra:install:*` bare-helm + secret-injection bash to `helmfile sync`, deleting
the bootstrap scripts once a fresh cleanroom converges on the helmfile path.

> **OPERATOR HARD STOP:** This phase rewrites the chain that currently makes `task setup` converge and
> passes dogfood/chaos. It MUST be validated by `task clean:all && task setup` on a fresh cluster with the
> operator present, because a regression breaks the cleanroom. Not autonomous.

### T2.1 — Map bare-helm/bootstrap bash → helmfile releases (audit, autonomous prep)

#### Objective
Produce the exact mapping from each `infra/helmfile/scripts/*.sh` + `infra:install:*` bare-helm call to
the helmfile release that should own it (with `needs:` ordering + secret `set:` blocks).

#### Evidence
The parent plan's T2.1-T2.3 (Coverage gaps 4-6) identified bare-helm in Taskfile + secret-injection bash
as the cleanroom-blocking layer. Helmfile already declares the releases; the bash duplicates ordering/secrets.

#### Files to edit
```
docs/operations/helmfile-delegation-map.md — (NEW) per-script → release mapping + needs: + set: blocks
```

#### Deep file dependency analysis
- Read-only audit; produces a doc the LIVE tasks consume. No infra change, so autonomous.

#### Deep Dives
- For each bootstrap script: which release, which Secret keys it injects, what ordering (`needs:`) it enforces.
- Invariant: every secret the bash injects must be expressed as a helmfile `set:`/`secrets:` or a pre-sync hook.

#### Tasks
1. Enumerate `infra/helmfile/scripts/*.sh` + `infra:install:*` bare-helm.
2. Map each to a release + needed `set:`/`needs:`.
3. Flag any that have NO helmfile equivalent (→ stays bash, KEEP).

#### TDD
```
RED:     N/A (documentation-only audit; verified by review)
GREEN:   Map complete; every bootstrap script classified
VERIFY:  manual review + cross-check against infra/helmfile/*.gotmpl release names
```

#### Acceptance Criteria
- [ ] Every bootstrap/bare-helm call mapped or flagged KEEP
- [ ] Mapping references real release names in `infra/helmfile/*.gotmpl`

#### DoD
- [ ] Map doc committed
- [ ] Reviewed for completeness

### T2.2 — Implement helmfile delegation + delete bootstrap bash (LIVE)

#### Objective
Move the mapped ordering + secret injection into helmfile releases, rewrite `infra:install:*` to
`helmfile sync`, and delete the now-redundant bootstrap scripts after a fresh cleanroom converges.

#### Evidence
T2.1 map. The goal is to remove ~25 helmfile/bootstrap scripts.

#### Files to edit
```
infra/helmfile/*.gotmpl — add set:/needs:/pre-sync hooks per the T2.1 map
taskfiles/infra.yml — rewrite infra:install:* to delegate to helmfile sync
infra/helmfile/scripts/*.sh — DELETE the ones fully absorbed by helmfile
.claude/rules/shell-scripts-allowlist.md — remove deleted rows
```

#### Deep file dependency analysis
- `taskfiles/infra.yml` `infra:install:*` is what `task setup` calls. Rewriting it changes the cleanroom
  bring-up path. Downstream: `task setup`, the dogfood/chaos cleanroom. A bug here breaks everything —
  hence LIVE validation mandatory.

#### Deep Dives
- Invariant: post-rewrite, `task clean:all && task setup` must exit 0 and `task doctor` FAIL=0 — identical
  to the bash path.
- Edge case (parent EC-01): postgresql↔gitea race — the master helmfile sync must respect `needs:`.
- Edge case (parent EC-02): `base64 -d` macOS portability if any secret decode moves into a hook.

#### Tasks
1. Add set:/needs:/hooks to helmfile per map.
2. Rewrite `infra:install:*`.
3. **LIVE:** `task clean:all && task setup && task doctor` — converge.
4. Delete absorbed bootstrap scripts; shrink allowlist.
5. **LIVE re-validate:** `/dogfood full` + `/chaos-test --full` still green.

#### TDD
```
RED:     infra/tests/scripts/... existing helmfile render tests must still pass + extend for new set: blocks
GREEN:   helmfile renders with injected secrets; task setup converges LIVE
REFACTOR: remove dead bootstrap invocations from Taskfile
VERIFY:  task clean:all && task setup && task doctor (LIVE, operator-present)
```

#### Acceptance Criteria
- [ ] `task setup` exit 0 + `task doctor` FAIL=0 on fresh cluster (LIVE)
- [ ] ~25 bootstrap scripts deleted; allowlist shrunk
- [ ] `/dogfood full` 106/0 + `/chaos-test --full` 10/10 still pass (no regression)

#### DoD
- [ ] LIVE cleanroom converges on helmfile path
- [ ] Dogfood + chaos re-validated green
- [ ] Deleted scripts removed from allowlist
- [ ] **EC-1 (MUST FIX) — full-repo grep before delete:** `git grep -F "<basename>"` returns only
      bookkeeping/self before `git rm` each bootstrap script. Blocking.
- [ ] **EC-6 (DOCUMENT) — rollback path explicit:** if LIVE `task setup` regresses, rollback is
      `git revert` of the Taskfile rewrite (bash chain preserved in history) — do NOT repair a
      deleted bash chain under pressure.

---

## Phase 3: Parity/Dogfood Orchestrators → Ginkgo (OPERATOR-GATED LIVE)

**Objective:** Replace the ~30 parity/dogfood bash test orchestrators with Ginkgo specs, repoint the CI
workflows, and delete the bash after the Ginkgo specs prove equivalent coverage on LIVE infra.

> **OPERATOR HARD STOP:** parity scripts validate multi-cell/cell2/canary/drill behavior that needs a real
> multi-cell or drill stack to exercise. Ginkgo equivalents cannot be validated without that LIVE infra.
> Not autonomous.

### T3.1 — Map parity/dogfood scripts → Ginkgo Describe blocks (audit, autonomous prep)

#### Objective
Map each `infra/tests/parity/*.sh` + `infra/tests/dogfood/*.sh` to an existing-or-new Ginkgo
`Describe`/`It`, and identify which CI workflows (multi-cell.yml, parity-validation.yml,
drill-prod-identical-soak.yml, dogfood-slo-gate.yml) invoke them.

#### Evidence
This session confirmed these scripts are LIVE CI orchestration (invoked by 4+ workflows). The e2e Ginkgo
journey suite already covers much deploy/rollback/secrets behavior (106 specs pass), so some parity checks
may already have Ginkgo equivalents.

#### Files to edit
```
docs/operations/ginkgo-migration-map.md — (NEW) per-script → Describe/It + which CI job + existing coverage?
```

#### Deep file dependency analysis
- Read-only; produces the migration contract. The cell2-gate-c*.sh dynamic-source set (13) collapses into
  one Ginkgo Describe with table-driven It blocks.

#### Deep Dives
- Identify scripts whose behavior the e2e journey suite ALREADY covers (delete-after-confirm) vs those
  needing new specs.
- Invariant: every CI job that invokes a parity script must have a Ginkgo target before the bash is deleted.

#### Tasks
1. Enumerate parity/dogfood scripts + their CI callers.
2. Map each to Ginkgo (existing or new) + note coverage gap.
3. Flag LIVE-only scenarios (cell2 activation, canary SLO) that need a drill stack.

#### TDD
```
RED:     N/A (audit; verified by review against existing infra/tests/e2e specs)
GREEN:   Map complete
VERIFY:  cross-check CI workflow `run:` blocks for every parity/dogfood .sh
```

#### Acceptance Criteria
- [ ] Every parity/dogfood script mapped to a Ginkgo target + CI caller identified
- [ ] LIVE-only scenarios flagged

#### DoD
- [ ] Map doc committed + reviewed

### T3.2 — Convert + delete in validated batches (LIVE)

#### Objective
For each batch: write/extend Ginkgo specs, repoint the CI workflow to invoke Ginkgo, validate equivalent
results on LIVE infra (multi-cell/drill where required), then delete the bash + `*_test.sh`.

#### Evidence
T3.1 map. Target: remove ~30 parity/dogfood scripts + the 13 cell2-gate libs they source.

#### Files to edit
```
infra/tests/e2e/*_test.go — (NEW/extend) Ginkgo specs replacing parity/dogfood orchestration
.github/workflows/{multi-cell,parity-validation,drill-prod-identical-soak,dogfood-slo-gate}.yml — repoint to Ginkgo
infra/tests/parity/*.sh, infra/tests/parity/lib/cell2-gate-c*.sh, infra/tests/dogfood/*.sh — DELETE post-validation
.claude/rules/shell-scripts-allowlist.md — remove deleted rows
```

#### Deep file dependency analysis
- The 4 CI workflows are the consumers; each must invoke the Ginkgo label-filter equivalent before the
  bash is removed. The cell2-gate-c*.sh are sourced by cell2-activation-gates.sh — delete the parent + libs
  together.

#### Deep Dives
- Invariant (D4): the Ginkgo spec must assert the SAME gate conditions as the bash (e.g. cell2 gate C1-C12
  semantics) — proven by running both against the same LIVE drill stack and comparing pass/fail per gate.
- Edge case: drill-prod-identical-soak.yml runs on a cron against an ephemeral stack — the Ginkgo repoint
  must preserve the EXPIRES_IN/teardown contract.

#### Tasks
1. Per batch: write Ginkgo specs, repoint CI, **LIVE-validate equivalence** (multi-cell/drill), delete bash.
2. After all batches: confirm the 4 workflows are fully Ginkgo-driven.

#### TDD
```
RED:     Ginkgo It blocks asserting each parity gate condition (cell2 C1-C12, canary SLO, rollback SLO)
GREEN:   Specs pass on LIVE multi-cell/drill stack with same verdicts as bash
REFACTOR: table-drive the cell2-gate It blocks
VERIFY:  go run ginkgo --label-filter=<parity|multicell> ./infra/tests/e2e/ (LIVE)
```

#### Acceptance Criteria
- [ ] Each parity gate: Ginkgo verdict == bash verdict on LIVE stack (D4 parity)
- [ ] 4 CI workflows repointed + green
- [ ] ~43 scripts (30 orchestrators + 13 cell2 libs) deleted; allowlist shrunk

#### DoD
- [ ] Ginkgo specs green on LIVE infra
- [ ] CI workflows green post-repoint
- [ ] Deleted scripts removed from allowlist
- [ ] **EC-1 (MUST FIX) — full-repo grep before delete:** `git grep -F "<basename>"` returns only
      bookkeeping/self before `git rm` each parity/dogfood/cell2-gate script. Blocking.
- [ ] **EC-2 (MUST FIX) — gate actually runs after repoint:** each of the 4 repointed workflows shows
      the Ginkgo run actually executed (spec count in output), no `continue-on-error`/dead path-filter.
- [ ] **EC-4 (SHOULD TEST) — no orphan test:** `no-new-bash`/allowlist gate fails if a `*_test.sh`
      references a deleted `.sh` (D3 auto-die enforced, not convention).

---

## Phase 5: Lock the Door + Verify ≤30 (autonomous, after all deletions)

**Objective:** Confirm the target is met and the allowlist reflects only the irreducible KEEP set.

### T5.1 — Re-audit, shrink allowlist to ≤30, confirm gates green

#### Objective
Re-run the inventory, confirm N(bash) ≤ 30, ensure the allowlist contains only D4-justified KEEP entries,
and confirm `no-new-bash` + `no-shell-wrap` gates are green.

#### Evidence
Global DoD of the parent plan: N(bash) ≤ 30, allowlist 100% coverage, both gates ATIVO.

#### Files to edit
```
.claude/rules/shell-scripts-allowlist.md — final state: ≤30 rows, each with D4 justification
docs/scripts-audit/2026-05-22.csv — final disposition (all non-KEEP = deleted)
```

#### Deep file dependency analysis
- `no-new-bash` parses the allowlist; it must match the on-disk `.sh` set exactly (no stale rows, no
  unlisted scripts).

#### Deep Dives
- Invariant: `find . -name '*.sh' (excl node_modules/.git/worktrees) | wc -l` ≤ 30, and every one is in the
  allowlist with a KEEP justification.
- **EC-5 (DOCUMENT):** if the minimal D4-justified KEEP set (operator-manual + DR + container entrypoint +
  harness hooks + irreducible orchestration) measures > 30, the literal ≤30 target is NOT achievable
  without a forbidden shell-wrap (D5). In that case the target becomes "minimal justified KEEP set" and a
  MEET is required to adjust the parent plan's numeric goal — do NOT force an invalid migration to hit 30.

#### Tasks
1. Re-run `loopzero-ops quality scripts-audit`.
2. Confirm count ≤ 30.
3. Prune allowlist to match; every row D4-justified.
4. Run both gates.

#### TDD
```
RED:     infra/tests/.../no-new-bash parity: allowlist == on-disk .sh set
GREEN:   count ≤ 30; gates green
VERIFY:  go run ./cmd/loopzero-ops quality no-new-bash && ... no-shell-wrap
```

#### Acceptance Criteria
- [ ] N(bash) ≤ 30
- [ ] allowlist == on-disk set, every row D4-justified
- [ ] `no-new-bash` + `no-shell-wrap` gates green

#### DoD
- [ ] Count verified ≤ 30
- [ ] Both gates PASS
- [ ] allowlist committed

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | False-positive-aware inventory | T0.1 | scriptsaudit detects dynamic-source/glob/entrypoint/hook |
| 2 | Remaining pure-logic gates → Go (autonomous) | T1.1 | pkg/quality/* with hermetic parity, delete pair |
| 3 | Bootstrap bash → helmfile (LIVE) | T2.1 + T2.2 | helmfile sync owns ordering+secrets; cleanroom validates |
| 4 | Parity/dogfood → Ginkgo (LIVE) | T3.1 + T3.2 | Ginkgo specs + CI repoint, validate on multi-cell/drill |
| 5 | cell2-gate dynamic-source libs | T3.2 | collapse into table-driven Ginkgo Describe, delete 13 |
| 6 | `*_test.sh` reduction | T1.1 + T2.2 + T3.2 | D3 auto-die with their migrated target |
| 7 | N(bash) ≤ 30 verified | T5.1 | re-audit + allowlist == on-disk |
| 8 | No CI/cleanroom regression | T2.2 + T3.2 | dogfood 106/0 + chaos 10/10 re-validated LIVE |

**Coverage: 8/8 gaps covered (100%)**

## Global Definition of Done

- [ ] All phases completed (Phase 1 autonomous; Phases 2-3 operator-gated LIVE)
- [ ] All tests passing (Go testify + Ginkgo)
- [ ] Zero golangci-lint warnings in new `pkg/quality/*` packages
- [ ] Backward compat NOT required (pre-release; deletes deliberate after parity)
- [ ] code-audit checks passing across modified packages
- [ ] **`/cross-validation 2026-05-22-script-cleanup` PASSED** — zero CRITICO, zero GRAVE
- [ ] Plan-specific:
  - [ ] N(bash) ≤ 30
  - [ ] allowlist == on-disk set, every row D4-justified
  - [ ] `no-new-bash` + `no-shell-wrap` gates ATIVO + green
  - [ ] every deletion preceded by a validated native replacement (D4 parity)
- [ ] **Evidence:** N/A — internal tooling/infra refactor; does not affect product maturity score
- [ ] **`/dogfood full` PASSED** + **`/chaos-test --full` 10/10** (no regression — see Dogfood Gate)

---

## Dogfood Gate (MANDATORY — NON-NEGOTIABLE)

> No plan is DONE without `/dogfood full` on clean infrastructure. For this cleanup, the gate also requires
> `/chaos-test --full` 10/10 because Phase 2 rewrites the cleanroom bring-up and Phase 3 rewrites test
> orchestration — both must prove zero regression against the baseline this session established.

### Execution

```bash
# 1. Fresh infrastructure
task clean:all && task setup && tilt up &
task doctor

# 2. Full dogfood + chaos
/dogfood full
/chaos-test --full
```

### Pass Criteria

- [ ] `/dogfood full` executed on clean infrastructure
- [ ] Pre-Flight Check: ALL checks PASS
- [ ] Zero S0 findings
- [ ] Zero S1 findings related to cleanup scope (helmfile delegation, Ginkgo repoint, gate migration)
- [ ] Prova de Vida passes for every deploy; Prova de Morte for every delete
- [ ] No regressions: Ginkgo journey 106/0 + chaos 10/10 still green
- [ ] Observability check passes
- [ ] Rule 3 clean: zero internal terms in CLI output

### On Failure

1. Fix ALL S0/S1 (root-cause, no workarounds)
2. Re-run `/dogfood full` + `/chaos-test --full` on fresh infrastructure
3. Repeat until zero S0/S1

S2-S4 documented but non-blocking.

---

## Cross-Validation

Mandatory before `/dogfood`. Per `.claude/skills/cross-validation`:

```
/cross-validation 2026-05-22-script-cleanup
```

Must return APROVADO: zero CRITICO, zero GRAVE; every DoD checkbox verified; every test name verified in
code; every file path verified edited; D1-D5 verified (D1 by migration-not-deletion evidence, D4 by parity
tests present, D5 by allowlist KEEP justifications). REPROVADO blocks dogfood.
