# Plan: Operational-Logic Capability Architecture (the "other way")

> **Version 1.0** — Reframe the shell-script problem from "rewrite 192 scripts in Go (target ≤30)" to "**home every operational capability in the right professional layer**". Operational logic stops being a flat pile of `.sh` files and lives in one of four typed/tested/discoverable homes: **declarative config** (install), **`loopzero-ops` CLI** (operator + CI imperative ops), **controllers** (continuous enforcement), **Go/Ginkgo** (validation). Bash count drops as a *byproduct*, not as the goal. The KPI becomes **"zero un-homed operational logic"**, measurable and FAANG-grade.

## Context

The predecessor plan (`2026-05-23-incremental-bash-to-go-migration-plan.md`) migrated 8 pure-logic gates + adopted `pkg/shellrun` + built the `no-dangling-script-refs` backstop (N(bash) 209→192). That work is the correct *foundation* — but the script-by-script frame hit three walls:

- **governance/* (4 scripts)** are bash-wrapped Python heredocs → Go migration = reimplementing governance-correctness logic (risk: false-green gates).
- **drill/dr (37 scripts)** are LIVE-gated → bash deletion needs real DOKS validation.
- **test orchestration (93 scripts, the biggest pocket)** were never the target — they're *tests wearing a bash costume*.

The wall is the signal that the frame is wrong. "≤30 scripts" measures the symptom, not the disease. The disease (per the original motivation, 2026-05-22: *"scripts shell são difíceis de manter para humanos"*) is **un-homed operational logic** — imperative glue with no type, no test, no contract, no discoverability.

**What FAANG actually does** (the "other way"):
- **Google** can keep large internal tooling because SRE treats *toil* as something to engineer away — into binaries/services/reconcilers — or eliminate. Scale + Blaze make it viable.
- **Vercel / AWS** do *not* expose shell sprawl in their product. Not via script-by-script rewrite — by **architecture**: the `vercel` CLI is a real program; infra is declarative; reconciliation is continuous. Loose scripts are a *smell* of capability without a home.
- **The unit of work is the *capability*, not the `.sh` file.** Ask "where should this capability LIVE in a professional system?" Often the answer is "nowhere imperative — it should be declarative or a reconciler" (→ delete the script), or "a tested CLI subcommand" (→ `loopzero-ops`).

## Objective

"Done" = every operational capability lives in its correct home; no imperative logic exists as un-homed, untested shell glue. Concretely:
- Install/bootstrap → declarative (Helmfile/ArgoCD/ESO/operator).
- Operator + CI imperative ops → `loopzero-ops` subcommands (typed, tested, `--json`, `--help`, exit codes).
- Continuous enforcement → controllers.
- Validation → Go/Ginkgo tests.
- The few genuinely-justified operator scripts (break-glass) → small, allowlisted, OR `loopzero-ops admin`.

**KPI redefinition:** measure **un-homed operational logic = 0**, not N(bash). A `.sh` is "homed" if it is (a) a thin entrypoint delegating to `loopzero-ops`/declarative/test-framework, (b) a genuinely-justified operator break-glass on the allowlist, or (c) a Claude/CI harness hook. Anything else is un-homed and must be relocated or deleted.

## ADRs

### D1 — Capability is the unit of work, not the script
- **Decision:** Migration is organized by the 5 capability domains, each assigned a single professional home. We never again ask "how do I rewrite script X" — we ask "which home owns this capability, and does the script even need to exist there?"
- **Rationale:** Script-by-script grind hit walls (python-wrappers, LIVE-gating) because it ignored *where the capability belongs*. Homing makes those walls disappear (a python-wrapper governance script isn't "migrate to Go" — it's "does this audit capability belong in `loopzero-ops audit` or is it deletable toil?").
- **Consequences:** Reframes ~192 problems into 5 domain decisions. Some scripts get deleted (capability belongs to a reconciler/declarative), not migrated.

### D2 — `loopzero-ops` is the single imperative entry point (like `kubectl`/`gh`/`vercel`)
- **Decision:** Every imperative operator/CI task lives as a `loopzero-ops <domain> <verb>` subcommand: `quality`, `pulumi`, `drill`, `admin`. Typed flags, `--json`, `--help`, permanent exit codes, unit-tested logic (mock-injected shell-out via `pkg/shellrun`).
- **Rationale:** One discoverable, tested, documented binary is the professional pattern. Bash disappears because there is no longer anywhere for un-homed imperative logic to live.
- **Consequences:** `loopzero-ops` grows from `quality`+`pulumi` to also own `drill` + `admin`. CI/Taskfile/SKILLs call `loopzero-ops`, never `bash X.sh`.

### D3 — Tests are code: the 93 test scripts move to Go/Ginkgo, not to Go gates
- **Decision:** `infra/tests/**` (93 scripts, 48 of them `*_test.sh` hermetic harnesses) are validation logic. They become `go test` / Ginkgo specs in the existing `infra/tests/e2e` + `pkg/**/*_test.go`, NOT `loopzero-ops` subcommands.
- **Rationale:** This is the largest pocket and the most clearly mis-homed — bash test harnesses re-implement assert/setup/teardown that `testing`/Ginkgo give for free. As `loopzero-ops` subcommands replace the *scripts under test*, their bash `_test.sh` companions die (already proven: each gate migration deleted its `_test.sh`).
- **Consequences:** The 48 `_test.sh` are coupled to D2 progress (they die when their target becomes a `loopzero-ops` subcommand). The remaining ~45 (parity/dogfood orchestrators) become Ginkgo specs — operator-LIVE-gated where they drive real clusters.

### D4 — Install/bootstrap is declarative, not procedural
- **Decision:** The 34 `helmfile/scripts` + `bootstrap-*` scripts represent *desired state*, not a procedure. They move into Helmfile hooks / ArgoCD / ESO / the platform operator. Secret *generation* → `loopzero-ops` generators or ESO; secret *wiring* → declarative.
- **Rationale:** Bootstrap-as-script is the anti-pattern Vercel/AWS avoided — you converge to desired state, you don't run a setup script. Local must equal production (Rule 10) — same declarative path.
- **Consequences:** T2/T3 (touches Helmfile/operator). Highest-risk domain; phased last with LIVE validation.

### D5 — KPI is "un-homed = 0", and the no-new-bash + no-dangling gates enforce it
- **Decision:** Replace the "≤30 scripts" target with the un-homed-logic KPI. The existing `no-new-bash` gate (forbids new un-homed scripts) + `no-dangling-script-refs` backstop (forbids broken deletions) already enforce the invariant. Add a classification: every allowlisted script must be tagged `ENTRYPOINT` / `OPERATOR-BREAKGLASS` / `HARNESS-HOOK` — anything else is a migration target.
- **Rationale:** A number target invites gaming (merge scripts to hit 30). A capability-home invariant is honest and auditable.
- **Consequences:** The allowlist becomes a *classification ledger*, not a grandfather list. Supersedes the ≤30 target of the predecessor plan.

### D6 — Risk-tiered, salvage-first, LIVE-gated where real
- **Decision:** Order by (a) salvageable green Go from `develop-bkp` first, (b) then authored-with-TDD, (c) declarative/controller relocations last (highest blast radius). Anything touching real infra (drill, bootstrap) requires operator LIVE validation before bash deletion (D4 of predecessor plan still binding).
- **Rationale:** Maximize proven-code reuse, minimize from-scratch governance-correctness risk, never delete a LIVE-path script without LIVE proof.

## Capability Map (the whole 192, homed)

| Domain | Count | Home (D2-D4) | Tier | Notes |
|---|---|---|---|---|
| **CI quality gates** | 6 bash left | `loopzero-ops quality` (Go) | A — autonomous | 8 already migrated this session; salvage/author remainder |
| **Test orchestration** | 93 | Go/Ginkgo (D3) | B — 48 coupled to D2, ~45 LIVE | biggest pocket; `_test.sh` die with their targets |
| **Drill / DR / cleanroom** | 37 | `loopzero-ops drill` (Go + `pkg/shellrun`) | C — LIVE-gated | `pkg/shellrun` foundation ready; the real maintenance pain |
| **Install / bootstrap** | 34 | Declarative (D4) | D — T2/T3, LIVE | converge, don't run setup scripts |
| **Chaos** | 5 | `loopzero-ops chaos` OR Ginkgo | C — LIVE-gated | pairs with chaos-mesh manifests |
| **Governance/audit** | 4 | `loopzero-ops audit` OR delete (python-wrappers) | B — focused TDD | reimplement Python logic carefully OR retire toil |
| **Operator break-glass / harness hooks** | ~13 | KEEP (allowlist-classified) | — | genuinely justified; tag ENTRYPOINT/OPERATOR/HOOK |

## Dependency Graph

```
Foundation (DONE this session: loopzero-ops quality + pkg/shellrun + no-dangling backstop)
        │
        ├──▶ Phase 1: Quality domain finish (Tier A — autonomous)
        │
        ├──▶ Phase 2: Test domain → Ginkgo (Tier B; 48 coupled to D2, ~45 LIVE)
        │
        ├──▶ Phase 3: loopzero-ops drill (Tier C — port autonomous, LIVE-gate before delete)
        │            │
        │            └──▶ Phase 3.1: chaos (after drill pattern proven)
        │
        └──▶ Phase 4: Install → declarative (Tier D — T2/T3, LIVE, last)

Phase 5 (cross-cutting): allowlist → classification ledger (D5) + KPI dashboard
```

---

## Phase 0 (DONE) — Foundation

Already shipped this session and validated: `no-dangling-script-refs` backstop gate + `task ops:build` (D6 of predecessor) + `pkg/shellrun` adoption + 8 quality gates migrated. This plan builds on it; no rework.

## Phase 1 — Quality domain finish (Tier A, autonomous)

**Objective:** every CI quality gate is a tested `loopzero-ops quality` subcommand.

Remaining bash quality scripts (6): salvage from `develop-bkp` where green (TDD re-validate), author where isolated + pure (like `spotcheck-dimension`). governance python-wrappers handled in Phase 2 (D3 — decide audit-CLI vs retire). Each: 4-surface rewire + `no-dangling` clean + delete bash + classify allowlist row.

## Phase 2 — Test domain → Ginkgo (Tier B)

**Objective:** validation logic lives in `go test`/Ginkgo, not bash harnesses.

- **48 `*_test.sh`**: die automatically as their script-under-test becomes a `loopzero-ops` subcommand (D2). No standalone work — coupled to Phases 1/3.
- **~45 parity/dogfood orchestrators**: become Ginkgo specs in `infra/tests/e2e`. Hermetic parts autonomous; cluster-driving parts operator-LIVE-gated (run the spec on real infra before deleting the bash).

## Phase 3 — `loopzero-ops drill` (Tier C, LIVE-gated)

**Objective:** the 37 drill/dr scripts (the real maintenance pain) become one tested `loopzero-ops drill <verb>` program.

- Salvage `pkg/drill` from `develop-bkp` (preflight, teardown-validator, state-reset, watchdog, cost-report, evidence-capture, jwt-diagnose) — green Go, mock-tested via `pkg/shellrun`.
- The 3 orchestrators (cleanroom/dogfood/chaos) become `loopzero-ops drill cleanroom|dogfood|chaos` OR stay skill-invoked Go.
- **LIVE gate (binding):** the drill CI crons (`drill-prod-identical-soak.yml` etc.) drive real DOKS — rewire + bash-delete ONLY after a real drill run proves the Go path equivalent (operator HARD STOP, ~$2-15).
- SKILL rewrite (the 4 drill SKILLs) gated by the `skill-paths` test (T0.2).

### Phase 3.1 — chaos (5 scripts)
After the drill pattern is proven: `loopzero-ops chaos` OR Ginkgo, pairing with the chaos-mesh manifests. LIVE-gated.

## Phase 4 — Install → declarative (Tier D, T2/T3, last)

**Objective:** the 34 bootstrap scripts become declarative convergence.

- Secret *generation* → `loopzero-ops` generator subcommands OR ESO; *wiring* → Helmfile hooks / ArgoCD / operator.
- **Highest blast radius** (touches every install) → T2/T3 meeting + full LIVE cleanroom validation. Phased last, smallest steps, each reversible.

## Phase 5 — Allowlist → classification ledger (D5)

**Objective:** the KPI is auditable.

- Re-tag every allowlist row: `ENTRYPOINT` (thin delegator), `OPERATOR-BREAKGLASS` (justified manual), `HARNESS-HOOK` (Claude/CI), or `MIGRATE-<domain>` (target).
- A gate (`loopzero-ops quality unhomed-logic`) fails if any `.sh` is neither classified-keep nor a thin delegator → un-homed count must be 0 at completion.

---

## Coverage Matrix

| # | Capability domain | Scripts | Home | Phase |
|---|---|---|---|---|
| 1 | CI quality gates | 6 | `loopzero-ops quality` | 1 |
| 2 | Test orchestration | 93 | Go/Ginkgo | 2 |
| 3 | Drill / DR | 37 | `loopzero-ops drill` | 3 |
| 4 | Chaos | 5 | `loopzero-ops chaos`/Ginkgo | 3.1 |
| 5 | Install / bootstrap | 34 | Declarative | 4 |
| 6 | Governance/audit | 4 | `loopzero-ops audit`/retire | 1-2 |
| 7 | Break-glass / hooks | ~13 | KEEP (classified) | 5 |

**Coverage: 192/192 homed (100%)**

## Global Definition of Done

- [ ] Every imperative op is a `loopzero-ops` subcommand (typed, tested, `--json`, `--help`, exit codes) OR declarative OR a controller OR a Ginkgo spec.
- [ ] **Un-homed operational logic = 0** (D5 ledger + gate); N(bash) is reported but not the target.
- [ ] `loopzero-ops` owns quality + pulumi + drill + admin domains.
- [ ] `no-new-bash` + `no-dangling-script-refs` + `skill-paths` gates green on every PR.
- [ ] Zero golangci-lint / ESLint warnings; full `go test ./...` green.
- [ ] **`/dogfood full` + `/cleanroom` + `/chaos-test --full` PASSED** on fresh infra for Tier-C/D (LIVE gate, operator-run — see predecessor Dogfood Gate).

## Governance

This is a **T3** reframe (new operational entry-point architecture + reclassifying 93 test scripts to Ginkgo + KPI change). It supersedes the *KPI and frame* of `2026-05-23-incremental-bash-to-go-migration-plan.md` (whose mechanical migrations remain valid as Phase-1 work). Requires sponsor ratification (the user, who requested the FAANG reframe) OR a Change Governor meeting before Phase 4 (declarative bootstrap = highest blast radius).
