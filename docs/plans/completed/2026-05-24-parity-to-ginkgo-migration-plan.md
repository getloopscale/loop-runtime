---
type: plan
title: "Plan: Track B — Parity Scripts → Ginkgo Specs"
status: deprecated
---

# Plan: Track B — Parity Scripts → Ginkgo Specs

> **Version 1.0** — Migrate the ~26 `infra/tests/parity/` LIVE prod-parity validation scripts to Ginkgo specs in `infra/tests/e2e/` (the existing, maintained test home), re-pointing the regression matrix, and deleting the bash only after the parity suite passes LIVE with output parity. This is **Track B of the Script Minimization Program** ([`2026-05-24-script-minimization-program-plan.md`](./2026-05-24-script-minimization-program-plan.md)) — the largest clean yield (~20 scripts) at the lowest architectural risk, so it runs FIRST. Starts after the program Phase 0 T0.1 (script-count gate) lands.

## Context

`infra/tests/parity/` holds 26 `.sh` validating prod-parity install + cell-2 activation:
- **Install matrix:** `k3d-virgin-install.sh`, `doks-install.sh`, `droplet-install.sh`, `droplet-cloud-init.sh`, `preflight.sh` — assert the platform installs identically across k3d / DOKS / droplet.
- **Cell-2 activation:** `cell2-activation-gates.sh` (entrypoint) + `lib/cell2-gate-c1..c12.sh` (glob-sourced gate functions) + `cell2-gate-postflip.sh` + `cell2-canary-stage1/2.sh` + `cell2-rollback-slo.sh`.
- **Audits:** `audit-regression-matrix.sh` (matrix-integrity gate), `audit-golden-test.sh`, `audit-argocd-drift.sh`.
- `validate-matrix.sh` — master matrix runner.

**Why migrate:** these are unwired-or-operator-invoked bash that the team's strategy (`2026-05-22-shell-script-strategic-plan` D5) wants as Ginkgo (`MIGRATE-GINKGO` disposition). The Ginkgo e2e suite (`infra/tests/e2e/journey_*.go`, validated 128/0 on 2026-05-24) is the maintained home with real assertions, JUnit output, and the `LOOPZERO_E2E` gate. Several are referenced as verification tokens in [`docs/runbooks/bootstrap-regression-matrix.md`](../../runbooks/bootstrap-regression-matrix.md) (the `audit-regression-matrix` gate blocked deleting `cell2-canary-stage1.sh` / `audit-argocd-drift.sh` — they must be re-pointed, not orphaned).

**Constraint:** `audit-regression-matrix.sh` is itself the matrix-integrity GATE — it cannot be naively migrated to Ginkgo without the matrix-token resolution logic moving too. It is the LAST item in this track and may become a Go gate (`loopzero-ops quality regression-matrix`) rather than a Ginkgo spec.

## Objective

**Done = all parity bash that can be a Ginkgo spec IS one (with output parity), the regression matrix re-points to the new specs, the parity suite passes LIVE, and the bash is deleted.** Target: `infra/tests/parity/*.sh` 26 → ≤ 4 (the irreducible LIVE-install shellouts + the matrix gate if kept as orchestrator).

Measurable:
- ≥ 18 parity `.sh` removed (→ Ginkgo specs under `infra/tests/e2e/parity_*_test.go`).
- `audit-regression-matrix` GREEN (tokens re-pointed).
- The parity suite (`task test:parity:*`) passes LIVE before each bash deletion.
- `script-count` baseline ratcheted by the count removed.

## ADRs

- **D1 — Parity assertions become labeled Ginkgo specs (`Label("parity")`), NOT new bash.**
  - *Decision:* Each parity check → an `It()` in `infra/tests/e2e/parity_{install,cell2}_test.go`, gated `LOOPZERO_E2E=1`, using the existing `kubectlWorkload`/`apiURL`/`seed` helpers.
  - *Rationale:* Ginkgo is the maintained home (128/0 journey run). Real assertions + JUnit + the suite gate. Avoids re-introducing bash (`no-new-bash`).
  - *Consequences:* Parity validation runs via `ginkgo --label-filter=parity` alongside journeys. The cell2-gate glob-source pattern (12 lib files) collapses into one ordered `Describe` with 12 `It()`s.

- **D2 — Output parity proven before delete: run bash + Ginkgo on the same LIVE env, diff the assertions.**
  - *Decision:* For each migrated script, run the original bash AND the new Ginkgo spec against the same cluster; both must reach the same verdict on the same conditions before the bash is deleted.
  - *Rationale:* Faithful-port precedent (`925e34c0` "preserves RED state"). A migration that changes what's asserted is a regression, not a port.
  - *Consequences:* A one-time dual-run per script during the track. Documented in the PR.

- **D3 — The install-matrix scripts that shell out to `doctl`/`pulumi`/`droplet cloud-init` stay as thin LIVE shellouts OR become Ginkgo specs that exec the real CLI.**
  - *Decision:* `doks-install.sh` / `droplet-install.sh` / `droplet-cloud-init.sh` genuinely provision cloud infra (doctl/cloud-init). Their ASSERTIONS migrate to Ginkgo (`exec.Command("doctl", ...)` + assert), but if a step is pure cloud-init YAML rendering it stays declarative. Irreducible cloud-CLI orchestration that Ginkgo would only shell-wrap stays bash + KEEP-OPERATOR.
  - *Rationale:* Per the program D2 + the no-shell-wrap principle — don't port a 1-shellout script to Go/Ginkgo just to `exec` it. Assertions migrate; pure provisioning orchestration may stay.
  - *Consequences:* Realistic yield is "most assertions" not "100% of scripts". `droplet-cloud-init.sh` (renders a cloud-init file) likely stays.

- **D4 — `audit-regression-matrix` is migrated LAST and may become a Go gate, not a Ginkgo spec.**
  - *Decision:* The matrix-integrity check (every matrix verification token resolves in the repo) is pure-logic file parsing → fits `pkg/quality/regressionmatrix` Go gate better than a Ginkgo spec. Migrate after all token-referenced scripts are re-pointed, so it stays GREEN.
  - *Rationale:* It's a static-analysis gate, not a LIVE behavior test. Go gate is the right home (joins the other `loopzero-ops quality` gates).
  - *Consequences:* `audit-regression-matrix.sh` → `loopzero-ops quality regression-matrix`; wired into `task quality:gates`.

- **D5 — Matrix re-point ORDER within a PR (EC-2, MUST FIX): spec-first, token-second, delete-third.**
  - *Decision:* Inside every migration PR the order is strict: (1) create + `go vet`-compile the new Ginkgo spec, (2) confirm the file exists, (3) ONLY THEN re-point the regression-matrix token to it, (4) finally delete the bash + drop allowlist + ratchet.
  - *Rationale:* Re-pointing a token to a not-yet-existing or mistyped spec name makes `audit-regression-matrix` resolve nothing → RED, blocking the PR; or the spec is later renamed and the token dangles again. Spec-first guarantees the token target exists and compiles before it's referenced.
  - *Consequences:* The matrix gate stays GREEN through the entire PR; no transient RED window. Applies to TB1.1, TB2.1, TB3.1, TB4.1.

## Dependency Graph

```
Program T0.1 (script-count gate)
        │
        ▼
B0: parity Ginkgo scaffolding (parity_test.go skeleton + Label + helpers reuse)
        │
        ├──▶ B1: cell-2 cluster (cell2-activation-gates + 12 gate libs + postflip) → parity_cell2_test.go
        │
        ├──▶ B2: cell-2 canary/rollback (canary-stage1/2 + rollback-slo) → parity_cell2_canary_test.go
        │
        ├──▶ B3: install matrix (k3d-virgin + preflight; doks/droplet assertions) → parity_install_test.go
        │
        └──▶ B4: audits (audit-golden-test + audit-argocd-drift) → specs
        │
        ▼  (B1-B4 each: re-point matrix tokens → dual-run parity → delete bash)
B5: audit-regression-matrix → loopzero-ops quality regression-matrix (Go gate) [LAST]
        │
        ▼
Track B DoD + parity LIVE GREEN → unblocks Track C
```
B1–B4 are parallel-capable within the track (independent script clusters); B5 is the sequential blocker (must be last so it stays GREEN while tokens re-point).

---

## Phase B0: Ginkgo Parity Scaffolding

### TB0.1 — Parity spec skeleton + label + helper reuse

#### Objective
Create the `parity` Ginkgo label, the spec files, and confirm the existing e2e helpers (`kubectlWorkload`, `apiURL`, `seed`, `skipIfNotE2E`) cover parity needs — so each subsequent task adds `It()`s, not infrastructure.

#### Evidence
The journey suite (`infra/tests/e2e/journey_*.go`) already has the helper layer + `LOOPZERO_E2E` gate + JUnit (validated 128/0). Parity specs reuse it. No new harness needed.

#### Files to edit
```
infra/tests/e2e/parity_install_test.go (NEW) — Describe("Parity: Install Matrix", Label("parity"))
infra/tests/e2e/parity_cell2_test.go (NEW) — Describe("Parity: Cell-2 Activation", Ordered, Label("parity"))
infra/tests/e2e/parity_cell2_canary_test.go (NEW) — Describe("Parity: Cell-2 Canary/Rollback", Ordered, Label("parity"))
taskfiles/test.yml — add e2e:parity target (ginkgo --label-filter=parity)
```

#### Deep file dependency analysis
- The NEW `parity_*_test.go` live in the same package as `journey_*_test.go`, reusing `helpers_test.go` (`kubectlWorkload`, `apiURL`, `seed`, `skipError`). Downstream: `task e2e:parity` + the eventual matrix re-point.
- `taskfiles/test.yml`: add `e2e:parity` mirroring `e2e:journey` with `--label-filter=parity`.

#### Deep Dives
- **Invariant:** parity specs MUST be gated `skipIfNotE2E()` (LOOPZERO_E2E) like journeys — they need a LIVE cluster.
- **Edge case:** cell-2 gates are an ORDERED container (c1→c12 must run in sequence, like the bash `for g in $(seq 1 12)`). Use Ginkgo `Ordered` + `It()` per gate.

#### Tasks
1. Write skeleton spec files with empty `Describe`/`It` stubs (RED — they reference helpers, must compile).
2. Add `e2e:parity` task target.
3. `go vet ./infra/tests/e2e/` (compile gate).

#### TDD
```
RED:     TestBuildGate_ParitySpecsCompile — go vet ./infra/tests/e2e/ exit 0 with new files
RED:     parity specs are discoverable: ginkgo --label-filter=parity --dry-run lists them
GREEN:   create skeleton + task target
REFACTOR: none
VERIFY:  go vet ./infra/tests/e2e/ && ginkgo --dry-run --label-filter=parity ./infra/tests/e2e/
```

#### Acceptance Criteria
- [ ] `go vet ./infra/tests/e2e/` clean with new files.
- [ ] `ginkgo --dry-run --label-filter=parity` lists the new (empty) containers.
- [ ] `task e2e:parity` resolves (taskfile-interface PASS).
- [ ] Pass: /code-audit lint 0, file <= 500 lines.

#### DoD
- [ ] Skeleton compiles; label discoverable; task wired; no bash deleted yet.

---

## Phase B1: Cell-2 Activation Gates → Ginkgo

### TB1.1 — Migrate cell2-activation-gates + 12 gate libs + postflip

#### Objective
Collapse `cell2-activation-gates.sh` + `lib/cell2-gate-c1..c12.sh` + `cell2-gate-postflip.sh` (14 files) into one Ordered `Describe("Parity: Cell-2 Activation")` with 12+1 `It()`s, re-point the matrix token, dual-run for parity, delete the 14 bash files.

#### Evidence
14 of the 26 parity scripts are this cluster. `cell2-activation-gates.sh` sources `lib/cell2-gate-${g}.sh` in a loop (glob-source — the false-orphan pattern). Migrating collapses 14 bash → 1 Go spec file. Biggest single yield in the track. cell-2 is ACTIVATED + LIVE-validated (CLAUDE.md), so the gate logic is stable (low churn).

#### Files to edit
```
infra/tests/e2e/parity_cell2_test.go — 12 It() (one per gate c1..c12) + postflip It(), ported assertions
docs/runbooks/bootstrap-regression-matrix.md — re-point entry #155 token (cell2-canary-stage1.sh) and any cell2-gate token to the Ginkgo spec
infra/tests/parity/cell2-activation-gates.sh — DELETE (after dual-run)
infra/tests/parity/lib/cell2-gate-c1..c12.sh — DELETE (12 files, after dual-run)
infra/tests/parity/cell2-gate-postflip.sh — DELETE (after dual-run)
.claude/rules/shell-scripts-allowlist.md — drop 14 rows
infra/quality/unhomed-baseline.json + script-count-baseline.json — ratchet
```

#### Deep file dependency analysis
- Each `cell2-gate-cN.sh` defines a `gate_cN()` function asserting one activation criterion (e.g., ClusterCredential connected, DynamicRouter count, namespace cell-aware reconcile). Read each, port its assertion to an `It("C{N}: ...")`. `cell2-rollback-slo.sh` is sourced by c4 → its logic folds into the C4 `It()` (so rollback-slo is covered here, not in B2).
- `cell2-activation-gates.sh` is wired (Taskfile.yml) — the Taskfile target must re-point to `task e2e:parity --label-filter=...` or be removed; rewire in this PR (taskfile-interface gate enforces).

#### Deep Dives
- **Invariant:** the 12 gates run ORDERED (c1 precondition for c2, etc.) — Ginkgo `Ordered` container; a failed early `It` skips the rest (matches bash `set -e` chain).
- **Edge case:** cell-2 must be ACTIVATED in the test env for these to pass — `BeforeAll` asserts `kubectl get clustercredential cell-2 status.connected=true` (or Skip with the H14 heal hint, mirroring the bash preflight at `cell2-canary-stage1.sh:91-95`).
- **Matrix re-point:** entry #155 cites `cell2-canary-stage1.sh:91-95`; after migration the token becomes `parity_cell2_test.go` (the preflight `BeforeAll`). Update in the same PR or `audit-regression-matrix` fails.

#### Tasks
1. Read all 14 bash files; tabulate each gate's assertion.
2. Write 13 `It()` (RED — fail until ported) in `parity_cell2_test.go`; `go vet` confirms it compiles (D5 step 1-2 — spec exists BEFORE token re-point).
3. Port assertions (GREEN).
4. Dual-run: `task test:parity:cell2-activation` (bash) + `ginkgo --label-filter=parity` (Ginkgo) on the same LIVE cell-2 env; confirm same verdicts (D2).
5. Re-point matrix tokens (D5 step 3 — ONLY after the spec compiles); run `audit-regression-matrix` GREEN.
6. Delete 14 bash + 14 allowlist rows + rewire/remove the Taskfile target (D5 step 4).
7. Ratchet baselines; `dangling-script-refs` GREEN.

#### TDD
```
RED:     parity_cell2_test.go It("C1".."C12") + It("postflip") — assert each activation criterion (fail pre-port)
RED:     BeforeAll skips when cell-2 not connected (parity with bash preflight)
RED:     Test_cell2_ordered_skips_after_failure (EC-4) — if C3 fails, C4..C12 report SKIPPED not FAILED (Ordered container mirrors bash set -e; one root-cause FAIL, no cascade noise)
GREEN:   port the 13 assertions from the bash gate functions
REFACTOR: collapse shared kubectl calls into local helpers
VERIFY:  LOOPZERO_E2E=1 ginkgo -v --label-filter=parity ./infra/tests/e2e/ (cell-2 specs PASS)
         + bash dual-run same verdict + audit-regression-matrix exit 0
```

#### Acceptance Criteria
- [ ] 13 cell-2 `It()` pass LIVE on an activated cell-2 env.
- [ ] Dual-run verdict parity vs bash (D2).
- [ ] `audit-regression-matrix` GREEN (tokens re-pointed).
- [ ] 14 bash deleted; `dangling-script-refs` GREEN; allowlist rows dropped.
- [ ] Pass: /code-audit complexity <= 10, file <= 500 lines (split if needed).

#### DoD
- [ ] `LOOPZERO_E2E=1 ginkgo --label-filter=parity` cell-2 specs GREEN.
- [ ] Matrix GREEN. 14 bash gone. Baselines ratcheted.
- [ ] `task quality:all` GREEN.

---

## Phase B2: Cell-2 Canary/Rollback → Ginkgo

### TB2.1 — Migrate cell2-canary-stage1/2 (rollback-slo folded into B1.C4)

#### Objective
Port `cell2-canary-stage1.sh` + `cell2-canary-stage2.sh` canary-weight/health assertions to `parity_cell2_canary_test.go`, re-point matrix entry #155, dual-run, delete.

#### Evidence
2 scripts; canary stages validate weighted TraefikService progression for cell-2 (the local/drill canary; prod canary stages 4-5 are separate future work). Matrix entry #155 references `cell2-canary-stage1.sh:91-95`.

#### Files to edit
```
infra/tests/e2e/parity_cell2_canary_test.go — canary stage1/2 weight + health It()
docs/runbooks/bootstrap-regression-matrix.md — entry #155 token → parity_cell2_canary_test.go
infra/tests/parity/cell2-canary-stage1.sh + cell2-canary-stage2.sh — DELETE after dual-run
.claude/rules/shell-scripts-allowlist.md — drop 2 rows; baselines ratchet
```

#### Deep Dives
- **Edge case:** local k3d canary HTTP probes report HTTP 000 in `--smoke` (CLAUDE.md known issue) — the spec must treat probe-informational-only in local, assert weights via `kubectl get traefikservice` (deterministic) like the journey-13 fix this session.

#### Tasks / TDD / Acceptance / DoD
1. Read both; port weight/health assertions to `It()` (RED→GREEN). 2. Dual-run parity. 3. Re-point #155. 4. Delete 2 bash + rows + ratchet.
```
RED:     It("stage1: canary weight progresses") + It("stage2: ...") — assert TraefikService weights
GREEN:   port; assert via kubectl (not HTTP probe, per known issue)
VERIFY:  LOOPZERO_E2E=1 ginkgo --label-filter=parity + audit-regression-matrix exit 0
```
- [ ] 2 canary `It()` GREEN; matrix GREEN; 2 bash gone; baselines ratcheted; `task quality:all` GREEN.

---

## Phase B3: Install Matrix → Ginkgo

### TB3.1 — Migrate k3d-virgin-install + preflight + doks/droplet ASSERTIONS

#### Objective
Port the install-parity ASSERTIONS (k3d-virgin install converges; preflight checks; doks/droplet post-install health) to `parity_install_test.go`. Per D3, irreducible cloud-provisioning orchestration (`droplet-cloud-init.sh` rendering, raw `doctl`/`pulumi up`) stays bash + KEEP-OPERATOR; only the assertions migrate.

#### Evidence
`k3d-virgin-install.sh`, `preflight.sh`, `doks-install.sh`, `droplet-install.sh`, `droplet-cloud-init.sh` (5). k3d-virgin + preflight are local-assertable → full migrate. doks/droplet provision real cloud → assertions migrate, provisioning stays.

#### Files to edit
```
infra/tests/e2e/parity_install_test.go — k3d-virgin convergence + preflight It(); doks/droplet post-install health It() (LIVE-gated)
infra/tests/parity/k3d-virgin-install.sh — DELETE (fully migrated, local)
infra/tests/parity/preflight.sh — DELETE if fully assertion (else KEEP thin)
infra/tests/parity/doks-install.sh / droplet-install.sh — reduce to provisioning-only OR KEEP-OPERATOR; assertions → Ginkgo
infra/tests/parity/droplet-cloud-init.sh — KEEP (renders cloud-init YAML — declarative, not assertion)
taskfiles/test.yml — repoint parity:k3d / parity:doks / parity:droplet targets
docs/runbooks/bootstrap-regression-matrix.md — re-point install tokens
```

#### Deep Dives
- **D3 boundary:** if after extracting assertions a script is ONLY `doctl/pulumi up` orchestration with no logic, it stays KEEP-OPERATOR (a Ginkgo `exec.Command("doctl"...)` wrapper would be a shell-wrap — forbidden by no-shell-wrap spirit). Document the boundary per script.
- **Edge case:** doks/droplet specs need cloud creds + are $-costing → gate behind a separate label (`Label("parity","cloud")`) so local `--label-filter=parity` skips them; only the operator runs cloud parity.

#### Tasks / TDD / Acceptance / DoD
1. k3d-virgin + preflight → full Ginkgo It() (local, RED→GREEN, delete bash). 2. doks/droplet → extract assertions to `Label("parity","cloud")` It(); reduce/keep provisioning bash per D3. 3. Re-point tokens. 4. Dual-run k3d-virgin locally; cloud dual-run is operator-LIVE.
```
RED:     It("k3d-virgin: platform converges 41/0") + It("preflight: ...") (local)
RED:     It("doks: post-install health", Label("cloud")) + It("droplet: ...")
GREEN:   port assertions; keep provisioning bash where irreducible (D3)
VERIFY:  LOOPZERO_E2E=1 ginkgo --label-filter=parity (local) GREEN; operator: --label-filter=parity,cloud on DOKS
```
- [ ] k3d-virgin + preflight migrated + bash deleted; doks/droplet assertions in Ginkgo; provisioning bash KEEP-documented; matrix GREEN; baselines ratcheted.

---

## Phase B4: Audits → Ginkgo

### TB4.1 — Migrate audit-golden-test + audit-argocd-drift

#### Objective
Port `audit-golden-test.sh` (helm golden-output drift) and `audit-argocd-drift.sh` (ArgoCD app classification A/B/C) to Ginkgo specs; re-point matrix entries #164/#165 (which cite `audit-argocd-drift.sh` as the verification method).

#### Evidence
2 scripts. audit-argocd-drift is matrix-referenced (#164/#165 "SIM in CI"). audit-golden-test validates helm render hasn't drifted (overlaps the Terratest render tests from the 2026-05-20 plan — check for redundancy; if the Terratest suite already covers golden output, audit-golden-test → DELETE redundant, not migrate).

#### Files to edit
```
infra/tests/e2e/parity_audit_test.go — argocd-drift classification It() (or loopzero-ops gate if pure-logic)
docs/runbooks/bootstrap-regression-matrix.md — entries #164/#165 token → new home
infra/tests/parity/audit-argocd-drift.sh — DELETE after re-point + dual-run
infra/tests/parity/audit-golden-test.sh — DELETE if redundant with Terratest render tests, else migrate
```

#### Deep Dives
- **Redundancy check:** the 2026-05-20 Terratest render-time tests (`infra/tests/helm/`) already assert helm output structure. If `audit-golden-test` is subsumed → DELETE (not migrate). Verify before porting.
- **argocd-drift:** classifies apps into A(config)/B(missing)/C(informational). Pure kubectl+logic → Ginkgo `It` asserting "zero Class A apps" (the matrix's documented criterion).

#### Tasks / TDD / Acceptance / DoD
1. Redundancy-check audit-golden-test vs Terratest → delete-or-migrate. 2. Port argocd-drift classification to It(). 3. Re-point #164/#165. 4. Dual-run; delete bash.
```
RED:     It("argocd-drift: zero Class A apps") — classify + assert
GREEN:   port classification logic
VERIFY:  LOOPZERO_E2E=1 ginkgo --label-filter=parity + audit-regression-matrix exit 0
```
- [ ] argocd-drift migrated; golden-test resolved (deleted-redundant or migrated); matrix GREEN; baselines ratcheted.

---

## Phase B5: Matrix Gate → Go (LAST)

### TB5.1 — audit-regression-matrix.sh → loopzero-ops quality regression-matrix

#### Objective
Migrate the matrix-integrity gate (every verification token in [`bootstrap-regression-matrix.md`](../../runbooks/bootstrap-regression-matrix.md) resolves in the repo) to a `pkg/quality/regressionmatrix` Go gate + `loopzero-ops` subcommand, wired into `task quality:gates`. MUST be last so all token re-points (B1–B4) are done and it stays GREEN.

#### Evidence
`audit-regression-matrix.sh` is pure file-parsing (read the matrix MD, extract tokens, check each resolves). Pure-logic → Go gate (joins the `loopzero-ops quality` family), not a Ginkgo spec (no LIVE behavior). It currently exits 2 (pre-existing-RED on some token) — the migration must reproduce the SAME verdict (faithful port, per `925e34c0` precedent), then the pre-existing failures are a separate fix.

#### Files to edit
```
pkg/quality/regressionmatrix/check.go (NEW) + check_test.go (NEW) — parse matrix MD, resolve tokens
cmd/loopzero-ops/quality_subcmd.go — add regression-matrix subcommand (flag.FlagSet pattern, no backtick help)
taskfiles/quality.yml — add /tmp/loopzero-ops quality regression-matrix to gates
taskfiles/test.yml — repoint parity:audit target to the Go gate
infra/tests/parity/audit-regression-matrix.sh — DELETE after parity-proven
infra/tests/parity/validate-matrix.sh — DELETE if it only orchestrates audit-regression-matrix
.claude/rules/shell-scripts-allowlist.md + baselines — drop rows, ratchet
```

#### Deep Dives
- **Faithful port (D2/precedent):** the Go gate MUST produce the same exit code as the bash on the same matrix file (including the pre-existing exit=2). Parity test: run both, diff exit + flagged tokens. Then fixing the pre-existing failures is a follow-up, NOT part of the port.
- **Token grammar:** the bash resolves `Test*` / `*_test_*` / `*.test.ts` / `*.sh` tokens against the repo. Port the exact grammar (read the bash regex).
- **Edge case:** after B1–B4 re-pointed tokens to Ginkgo specs (`parity_*_test.go`), the Go gate must resolve `parity_cell2_test.go`-style tokens — confirm the grammar covers `*_test.go`.

#### Tasks / TDD / Acceptance / DoD
1. Read bash token grammar. 2. Write Go gate + tests (RED). 3. Implement (GREEN), parity-proven vs bash (same exit). 4. Wire loopzero-ops + task. 5. Delete audit-regression-matrix.sh + validate-matrix.sh; ratchet.
```
RED:     TestRegressionMatrix_TokenResolves / _MissingTokenFails / _SameVerdictAsBash
GREEN:   implement parse + resolve matching bash grammar
REFACTOR: none
VERIFY:  cd cmd/loopzero-ops && go test ./... ; /tmp/loopzero-ops quality regression-matrix == bash exit
```
- [x] **DONE 2026-05-24** (commit `a6a3d46a`). Go gate `pkg/quality/regressionmatrix` + `loopzero-ops quality regression-matrix` wired into `task quality:gates`; golden test folded into `TestGolden_*`; 2 bash deleted; allowlist rows dropped; script-count 126→124 + unhomed 90→88 ratcheted; `task quality:gates` GREEN; `dangling-script-refs` PASS.
  - **DEVIATION from "faithful port of exit=2":** the plan's Evidence assumed exit=2 was "pre-existing-RED on some token". It was NOT — the bash exited 2 via a `set -e` abort on a helper `return 2` BEFORE scanning (a crash, never a token verdict) — `finding-2026-05-24-audit-regression-matrix-set-e-abort`. Reproducing a crash is nonsensical; the Go gate implements the INTENDED scan (166 entries, miss=0). Documented in finding + commit + log (not a silent amendment).
  - **validate-matrix.sh RETAINED** (plan line 314 condition false): it is a 16-gate master runner, not a pure orchestrator of the matrix audit. Only its 2 matrix-audit invocations were re-pointed to the Go gate.

---

## Coverage Matrix

> **AMENDED 2026-05-24** (MEET-ASYNC-AMENDMENT-2026-05-24-001, RATIFIED): the
> "~20–22 migrate" projection was wrong. Actual Track B removal = **16** (14 cell-2
> gates in TB1.1 + 2 audit scripts in TB5). The other 10 parity scripts have no
> routine invariant left to migrate (operator procedures / cloud provisioning /
> orchestrators / diagnostic) → reclassified **KEEP-OPERATOR**, not migrated.

| # | Parity script(s) | Task | Resolution |
|---|---|---|---|
| 1 | cell2-activation-gates + c1..c12 + postflip (14) | TB1.1 | ✅ DONE → parity_cell2_test.go (5 It + Skips), 14 bash deleted |
| 2 | cell2-canary-stage1/2 + rollback-slo (3) | TB2.1 | **KEEP-OPERATOR** — mutating canary/SLO procedures (invariants already in TB1.1) |
| 3 | k3d-virgin-install + preflight (2) | TB3.1 | **KEEP-OPERATOR** — cleanroom install + 16-gate meta-orchestrator |
| 4 | doks-install + droplet-install (2) | TB3.1 | **KEEP-OPERATOR** — cloud provisioning (per D3) |
| 5 | droplet-cloud-init (1) | TB3.1 | **KEEP-OPERATOR** — declarative cloud-init render (per D3) |
| 6 | audit-golden-test (1) | TB5 | ✅ DONE — was the matrix-auditor self-test (not helm-render); folded into TestGolden_*, bash deleted |
| 7 | audit-argocd-drift (1) | TB4.1 | **KEEP-OPERATOR** — drift triage diagnostic (no routine invariant) |
| 8 | audit-regression-matrix + validate-matrix (2) | TB5.1 | ✅ DONE (audit-regression-matrix → loopzero-ops Go gate, deleted); validate-matrix **KEEP-OPERATOR** (16-gate master runner) |

**Actual removal: 16 scripts (TB1.1: 14 + TB5: 2). Remaining 10 = KEEP-OPERATOR (ratified).**

**Coverage: 26/26 parity scripts addressed. Expected removal: ~20–22 (≤4 KEEP: droplet-cloud-init + irreducible doks/droplet provisioning).**

## Global Definition of Done

- [ ] All B0–B5 phases complete
- [ ] ≥18 parity `.sh` deleted; `infra/tests/parity/*.sh` ≤ 4
- [ ] All parity assertions exist as Ginkgo specs (`Label("parity")`) or the regression-matrix Go gate
- [x] `loopzero-ops quality regression-matrix` GREEN (tokens re-pointed) — DONE 2026-05-24 (`a6a3d46a`)
- [ ] `LOOPZERO_E2E=1 ginkgo --label-filter=parity` GREEN LIVE
- [ ] `dangling-script-refs` + `taskfile-interface` + `script-count` GREEN; baselines ratcheted
- [ ] `/cross-validation 2026-05-24-parity-to-ginkgo-migration` PASSED
- [ ] N/A — does not affect maturity scores
- [ ] **`/dogfood full` PASSED** (program-level, after Track A; this track's LIVE gate is the parity suite)

---

## Dogfood Gate (MANDATORY)

> Track B's LIVE gate is the **parity suite** (`LOOPZERO_E2E=1 ginkgo --label-filter=parity` + operator cloud parity). The program `/dogfood full` runs after the final track.

### Execution
```bash
task clean:all && task setup && tilt up && task doctor
# cell-2 must be activated for B1/B2:
task k3d:heal   # H14 bootstraps cell-2 ClusterCredential
LOOPZERO_E2E=1 ginkgo -v --label-filter=parity --timeout=60m ./infra/tests/e2e/
# operator cloud parity (B3 doks/droplet): --label-filter=parity,cloud on DOKS/droplet
```

### Pass Criteria
- [ ] Parity Ginkgo suite GREEN LIVE (local) before each bash deletion
- [ ] Dual-run verdict parity (bash == Ginkgo) per migrated script (D2)
- [ ] Zero parity regressions; matrix GREEN
- [ ] `script-count` ratcheted by the count removed

### On Failure
A migrated spec disagreeing with its bash = a port regression. Restore the bash from git for that unit, fix the spec to match the bash verdict, re-dual-run, then delete. Never delete a bash whose Ginkgo spec hasn't matched its verdict LIVE.
