# Plan: Track C — Drill Scripts → Go/loopzero-ops + Consolidation

> **Version 1.0** — Reduce the ~20 drill/DR/chaos orchestration scripts (`infra/scripts/dr/*`, `infra/scripts/drill-*`, `infra/scripts/chaos/*`, `infra/scripts/lib/drill-*`) by (a) porting pure-logic pieces to `loopzero-ops`/Go, (b) consolidating duplicated orchestration, and (c) leaving irreducible LIVE DOKS orchestration as KEEP-OPERATOR. The 4 drill SKILLs (`cleanroom-drill`, `dogfood-drill`, `chaos-test-drill`, `drill-test`) are rewired to the new homes BEFORE any bash is deleted. **Track C of the Script Minimization Program** (`2026-05-24-script-minimization-program-plan.md`) — moderate yield, DOKS-drill-LIVE-gated. Starts ONLY after Track B DoD + parity LIVE GREEN (program D4).

## Context

The drill domain (`unhomed-logic` drill=20 + chaos=5) drives real DOKS prod-identical drills + local chaos:
- **DR orchestrators (`infra/scripts/dr/`):** `cancel-refresh`, `chaos-orchestrator`, `cleanroom-orchestrator`, `cost-report`, `dogfood-orchestrator`, `evidence-capture`, `jwt-diagnose`, `preflight`, `state-reset`, `teardown-validator`, `watchdog` (11) — invoked BY the 4 drill SKILL.md files.
- **Drill core (`infra/scripts/`):** `drill-helmfile-bootstrap.sh` (1126 LOC — the prod-identical bootstrap), `drill-mint-jwt.sh`.
- **Drill libs (`infra/scripts/lib/`):** `drill-skill-common.sh`, `drill-dogfood-journeys.sh`, `drill-jwt-gate.sh`.
- **Chaos orchestrators (`infra/scripts/chaos/`):** `baseline`, `cleanup`, `report`, `run-full`, `run-scenario` (5) — invoked by the `chaos-test` skill (used LIVE 2026-05-23, 10/10).

**Why this is hard:** these scripts (1) provision/teardown real DOKS infra (`pulumi up/destroy`, `doctl`), (2) are skill-invoked (the SKILL.md instructs `bash infra/scripts/dr/<x>.sh`), and (3) the `develop-bkp` big-bang FAILED exactly here — it deleted `infra/scripts/dr/` and left the 4 SKILLs dangling (`reference_bash_to_go_migration`). So Track C is the highest-rewire-risk track and is **DOKS-drill-LIVE-gated** (real-money provisioning = operator-run; the autonomous loop CANNOT validate it).

**What IS migratable:** the pure-logic pieces — JWT TTL validation (`drill-jwt-gate`), evidence JSON shaping (`evidence-capture`), teardown residual-resource validation (`teardown-validator`), cost arithmetic (`cost-report`), SLO/baseline diffing (chaos `baseline`/`report`). These have hermetic tests already (the deleted `*_test.sh` tested them) and can become `loopzero-ops` subcommands. The PROVISIONING/orchestration (pulumi up, doctl, helm sync, skill sequencing) stays bash + KEEP-OPERATOR (no-shell-wrap: a Go wrapper around `pulumi up` is forbidden).

## Objective

**Done = pure-logic drill/chaos pieces are loopzero-ops Go gates/subcommands (parity-proven), duplicated orchestration is consolidated, the 4 SKILLs are rewired, irreducible LIVE orchestration is KEEP-OPERATOR-documented, and a DOKS drill passes LIVE with the new wiring before any bash is deleted.** Target: drill+chaos 25 → ≤ 12.

Measurable:
- Pure-logic pieces (JWT-gate, evidence-shape, teardown-validate, cost-report, SLO-diff) → loopzero-ops subcommands with Go tests.
- 4 drill SKILLs updated to call the new homes; `skill-paths` gate GREEN; `danglingrefs` GREEN.
- ≥ 8 drill/chaos `.sh` removed.
- `task drill:l2-scale` (DOKS prod-identical) GREEN with new wiring before bash deletion (operator-run).

## ADRs

- **D1 — Only PURE-LOGIC drill pieces migrate to Go; PROVISIONING/orchestration stays bash + KEEP-OPERATOR.**
  - *Decision:* Classify each of the 25 as PURE-LOGIC (validation/arithmetic/JSON-shaping → Go) vs ORCHESTRATION (pulumi/doctl/helm/skill-sequencing → bash KEEP). Migrate only PURE-LOGIC.
  - *Rationale:* no-shell-wrap principle — a Go function that just `exec`s `pulumi up` adds no value and loses testability. The `develop-bkp` failure was deleting orchestration without a home. The delegation-map precedent (k3d-cross-cluster-heal KEEP-OPERATOR: "one real helm line among LIVE recovery logic").
  - *Consequences:* Realistic yield ~8–13 scripts, not 25. The big `drill-helmfile-bootstrap.sh` (1126 LOC) STAYS (it's pure provisioning orchestration).

- **D2 — Rewire ALL 4 SKILLs in the SAME PR as each bash deletion (the develop-bkp anti-pattern fix).**
  - *Decision:* When a dr/ script becomes a loopzero-ops subcommand, the SKILL.md(s) that instruct `bash infra/scripts/dr/<x>.sh` are updated to `loopzero-ops drill <x>` in the same PR; `skill-paths` + `danglingrefs` gates enforce.
  - *Rationale:* The develop-bkp big-bang deleted dr/ and left 4 SKILLs dangling — the exact failure to prevent. The `danglingrefs` backstop (built Phase 0 of the bash→Go plan) catches it.
  - *Consequences:* Each migration PR touches the relevant SKILL.md. The 4 skills: cleanroom-drill, dogfood-drill, chaos-test-drill, drill-test.

- **D3 — DOKS-drill LIVE gate is an OPERATOR HARD STOP; the autonomous loop ports + parity-proves hermetically but CANNOT delete bash until the operator runs the drill GREEN.**
  - *Decision:* Autonomous work: port pure-logic + Go tests + rewire skills + hermetic parity. Bash deletion + baseline ratchet happen ONLY after the operator runs `task drill:l2-scale` (DOKS, ~$0.35) GREEN with the new wiring.
  - *Rationale:* Real-money DOKS provisioning + the drill is the prod-readiness gate (maturity gate a). Deleting drill bash on an unvalidated Go port would break the drill (per `reference_bash_to_go_migration` HARD STOP boundary).
  - *Consequences:* This track has an operator checkpoint mid-flight. The autonomous portion ends at "ported + rewired + hermetic-parity-proven; PENDING operator DOKS drill".

- **D4 — Chaos pure-logic (baseline/report SLO-diff) → loopzero-ops; chaos orchestration (run-scenario/run-full/cleanup) stays bash (chaos-mesh apply + skill-driven).**
  - *Decision:* `chaos/baseline.sh` (samples SLO) + `chaos/report.sh` (diffs baseline/during/post) have pure-logic diffing → a `loopzero-ops chaos slo-compare` subcommand; `run-scenario`/`run-full`/`cleanup` (kubectl apply chaos-mesh manifests, skill-driven) stay bash.
  - *Rationale:* The SLO comparison is testable pure-logic; the chaos injection is kubectl orchestration (no-shell-wrap). chaos-test skill used LIVE 2026-05-23 (10/10) — the orchestration works, don't break it.
  - *Consequences:* ~1–2 chaos scripts shrink (logic extracted); the 3 orchestrators stay.

## Dependency Graph

```
Track B DoD + parity LIVE GREEN
        │
        ▼
C0: classify 25 drill/chaos scripts PURE-LOGIC vs ORCHESTRATION (audit doc)
        │
        ├──▶ C1: JWT pure-logic (drill-jwt-gate + drill-mint-jwt TTL-validate) → loopzero-ops drill jwt-validate
        ├──▶ C2: evidence/teardown/cost pure-logic → loopzero-ops drill {evidence-shape,teardown-validate,cost}
        ├──▶ C3: chaos SLO-diff (baseline/report) → loopzero-ops chaos slo-compare
        │         (each: rewire skills → hermetic parity)
        ▼
C4: OPERATOR HARD STOP — task drill:l2-scale (DOKS) GREEN with new wiring
        │  (operator runs; on GREEN →)
        ▼
C5: delete migrated bash + drop allowlist rows + ratchet baselines
        │
        ▼
Track C DoD → unblocks Track A
```
C1–C3 parallel-capable (independent pure-logic). C4 is the operator gate. C5 only after C4 GREEN.

---

## Phase C0: Classification Audit

### TC0.1 — Classify 25 drill/chaos scripts: PURE-LOGIC vs ORCHESTRATION

#### Objective
Produce `docs/operations/drill-migration-map.md` (mirror of the helmfile-delegation-map): for each of the 25, mark PURE-LOGIC (→ loopzero-ops, with the line ranges) vs ORCHESTRATION (KEEP-OPERATOR, with the pulumi/doctl/kubectl evidence). This is the honest-yield gate before any porting.

#### Evidence
The helmfile-delegation-map (task #176) prevented over-claiming on bootstrap (showed only 1/5 delegatable). The same read-only audit is needed here so Track C doesn't repeat the "claim 25, deliver 8" error. The deleted hermetic `*_test.sh` (drill-jwt-gate_test, drill-teardown-validator_test, etc.) prove which pieces ARE pure-logic (they had hermetic tests).

#### Files to edit
```
docs/operations/drill-migration-map.md (NEW) — per-script PURE-LOGIC/ORCHESTRATION + target loopzero-ops subcommand + skill callers
```

#### Deep file dependency analysis
- Read each of the 25; grep for `pulumi |doctl |helm |kubectl apply|kubectl create` (orchestration markers) vs pure parsing/arithmetic. For each PURE-LOGIC, identify the line range + the consuming SKILL.md.
- Downstream: C1–C3 implement only the PURE-LOGIC rows.

#### Deep Dives
- **Invariant:** a script with EVEN ONE load-bearing provisioning call that can't be separated → ORCHESTRATION KEEP (don't half-port). e.g., `drill-helmfile-bootstrap.sh` (1126 LOC, pulumi-output→helmfile injection) = ORCHESTRATION, KEEP.
- **Edge case:** `preflight.sh` (dr/) mixes credential-isolation CHECK (pure-logic, → loopzero-ops) with cluster-reachability (kubectl, orchestration). Split: the isolation check migrates, the reachability stays.

#### Tasks / TDD / Acceptance / DoD
1. Read 25; classify; write the map. (Doc-only task — TDD = the map's classifications are evidence-cited with line ranges + grep proof.)
```
RED:     N/A (audit doc) — verification: every PURE-LOGIC row cites line range + a hermetic-test precedent; every ORCHESTRATION row cites a pulumi/doctl/kubectl line
GREEN:   drill-migration-map.md complete, 25/25 classified
VERIFY:  grep -c "PURE-LOGIC\|ORCHESTRATION" drill-migration-map.md == 25
```
- [x] **DONE 2026-05-24** — `docs/operations/drill-migration-map.md`. 21/21 scripts classified (actual count; "25" was the loose unhomed drill=20+chaos=5 estimate). Evidence-cited with line ranges + 3 corrections to the plan's pure-logic premises.
- [x] No code changed (read-only audit).

> **HONEST YIELD (corrected down from ~8–13):** only **2 confirmed** PURE-LOGIC migratable (cost arithmetic, chaos SLO-compare) + **3 probable** (teardown-validate, evidence-shape, mint-jwt TTL). Realistic delete-after-C4 ≈ 2–4 scripts. Key premise errors caught: `drill-jwt-gate.sh` is a LIVE curl e2e (NOT TTL decode); the chaos SLO verdict lives in `run-scenario.sh:235-257` (NOT `report.sh`, which only formats); `chaos/baseline.sh` only samples LIVE. Same over-count pattern as Track B.

---

## Phase C1: JWT Pure-Logic → loopzero-ops

### TC1.1 — drill-jwt-gate + drill-mint-jwt TTL-validation → loopzero-ops drill jwt-validate

#### Objective
Port the JWT TTL/claim validation (the part `drill-jwt-gate.sh` + the validation half of `drill-mint-jwt.sh` do — decode JWT, assert TTL ≥ threshold, assert claims) to a `loopzero-ops drill jwt-validate` Go subcommand; rewire the drill SKILLs; keep the MINTING (hydra exec) as bash.

#### Evidence
`drill-jwt-gate.sh` (68 LOC) + the bootstrap-time JWT validation gate (task #103) are pure decode+assert — they HAD a hermetic test (`drill-jwt-gate_test.sh`, deleted). The minting (`kubectl exec hydra ...`) is orchestration; the VALIDATION is pure-logic (base64-decode + exp/claim check), exactly the kind already in `pkg/jwt`.

#### Files to edit
```
pkg/quality/drilljwt/check.go (NEW) + check_test.go (NEW) — decode JWT, assert TTL>=min + required claims
cmd/loopzero-ops/quality_subcmd.go OR a new drill subcommand group — wire jwt-validate
infra/scripts/lib/drill-jwt-gate.sh — DELETE after rewire (pure-logic fully ported)
infra/scripts/drill-mint-jwt.sh — KEEP minting; replace its inline TTL-check with a call to loopzero-ops drill jwt-validate
.claude/skills/{drill-test,dogfood-drill,cleanroom-drill}/SKILL.md — repoint jwt-gate refs (D2)
```

#### Deep Dives
- **Reuse `pkg/jwt`:** LoopZero already has a JWT parser (`pkg/jwt`) — the gate uses it (don't reinvent; design-principles "Don't Reinvent"). The new gate is decode + TTL/claim assertion only.
- **Invariant:** parity with `drill-jwt-gate.sh` exit codes (0 OK / 2 blocked / stderr "JWT TTL: <s>s") — faithful port.
- **Edge case:** heredoc-stdin-shadowing trap (`feedback_heredoc_stdin_trap`) — the bash mint reads JWT from a file; the Go gate reads from arg/stdin cleanly.

#### Tasks / TDD / Acceptance / DoD
1. Write `drilljwt/check_test.go` (RED — valid-TTL pass, expired fail, missing-claim fail). 2. Implement using `pkg/jwt` (GREEN). 3. Wire loopzero-ops subcommand. 4. Replace `drill-mint-jwt.sh` inline TTL-check with the subcommand call; rewire 3 SKILLs. 5. Hermetic parity vs bash. 6. (delete drill-jwt-gate.sh at C5, after C4 DOKS GREEN).
```
RED:     TestDrillJWT_ValidTTL_Pass / _ExpiredTTL_Fail / _MissingClaim_Fail / _ParityWithBashExit
GREEN:   implement via pkg/jwt
VERIFY:  cd cmd/loopzero-ops && go test ./...; loopzero-ops drill jwt-validate <token> == bash exit
```
- [ ] Go gate parity with bash; 3 SKILLs rewired; `skill-paths` GREEN; bash deletion DEFERRED to C5.

---

## Phase C2: Evidence / Teardown / Cost Pure-Logic → loopzero-ops

### TC2.1 — evidence-capture (JSON shape) + teardown-validator (residual count) + cost-report (arithmetic) → loopzero-ops drill {evidence-shape, teardown-validate, cost}

#### Objective
Port the pure-logic halves: `evidence-capture.sh` JSON-shaping (the fault-tolerant evidence assembly, not the kubectl gathering), `teardown-validator.sh` residual-resource counting + VPC-retry decision logic, `cost-report.sh` cost arithmetic — to `loopzero-ops drill` subcommands.

#### Evidence
All three had hermetic tests (`drill-evidence-capture_test.sh`, `drill-teardown-validator_test.sh` — deleted). teardown-validator's doctl-fallback decision (T4.1.bis EC-05) is pure-logic; cost-report is arithmetic over pulumi state timestamps. The GATHERING (doctl/kubectl) stays bash; the SHAPING/DECISION migrates.

#### Files to edit
```
pkg/quality/drillevidence/ + drillteardown/ + drillcost/ (NEW Go + tests)
cmd/loopzero-ops/quality_subcmd.go — wire 3 drill subcommands
infra/scripts/dr/evidence-capture.sh / teardown-validator.sh / cost-report.sh — reduce to gathering + call loopzero-ops for shaping/decision; DELETE if fully pure-logic (evidence/cost likely fully; teardown keeps doctl gathering)
.claude/skills/{drill-test,*-drill}/SKILL.md — repoint (D2)
```

#### Deep Dives
- **teardown-validator split:** `doctl databases list / kubernetes cluster list` (gathering, bash) → pipe to `loopzero-ops drill teardown-validate` (counts residuals, decides retry-with-backoff). The DECISION is pure-logic + testable; the gathering stays.
- **cost-report:** reads pulumi-output timestamps (provision-start, teardown-end) → computes $ burn. Pure arithmetic → fully Go; the bash wrapper deletes.
- **Edge case:** evidence-capture is "fault-tolerant" (EC-03) — must not fail the drill if a gather step errors. The Go shaper must tolerate partial input (missing keys → null, not panic).

#### Tasks / TDD / Acceptance / DoD
Per subcommand: RED tests (shape/count/arithmetic + parity) → GREEN → wire → rewire skills → hermetic parity. Deletion at C5.
```
RED:     TestDrillEvidence_PartialInput_NoPanic / TestDrillTeardown_ResidualCount / TestDrillCost_BurnArithmetic / *_ParityWithBash
GREEN:   implement 3 subcommands
VERIFY:  go test ./...; each loopzero-ops drill X == bash exit/output
```
- [ ] 3 subcommands parity-proven; skills rewired; cost+evidence bash fully ported (delete C5); teardown reduced to gathering+call.
  - [x] **cost DONE 2026-05-24** — `pkg/quality/drillcost` + `loopzero-ops drill cost`; 5 Go tests; **byte-identical parity** vs `cost-report.sh --start/--end`; bash rewired to delegate (keeps `--auto` Pulumi gathering); no SKILL `bash` caller (only back-compat comments). Bash deletion DEFERRED to C5 (after C4 DOKS). evidence-shape + teardown-validate: PENDING.

---

## Phase C3: Chaos SLO-Diff → loopzero-ops

### TC3.1 — chaos baseline/report SLO comparison → loopzero-ops chaos slo-compare

#### Objective
Port the SLO comparison logic (baseline vs during vs post: p95 drift, success-rate delta, recovery-within-budget verdict) from `chaos/baseline.sh` + `chaos/report.sh` to `loopzero-ops chaos slo-compare`; keep `run-scenario`/`run-full`/`cleanup` (chaos-mesh kubectl orchestration) as bash.

#### Evidence
chaos-test ran LIVE 2026-05-23 (10/10). The SLO verdict (p95 within 5% drift, recovery < budget) is pure arithmetic over JSON samples — testable Go. The sampling (`curl /health`, kubectl) + chaos injection (kubectl apply chaos-mesh) is orchestration (no-shell-wrap, D4).

#### Files to edit
```
pkg/quality/chaosslo/check.go (NEW) + check_test.go (NEW) — compare baseline/during/post JSON → PASS/FAIL/INCONCLUSIVE
cmd/loopzero-ops — wire chaos slo-compare
infra/scripts/chaos/report.sh — reduce to call loopzero-ops chaos slo-compare for the verdict; keep markdown emit OR fully port if pure
infra/scripts/chaos/baseline.sh — KEEP (samples LIVE /health + kubectl — orchestration)
infra/scripts/chaos/run-scenario.sh + run-full.sh + cleanup.sh — KEEP (chaos-mesh orchestration)
.claude/skills/chaos-test{,-drill}/SKILL.md — repoint report's verdict step
```

#### Deep Dives
- **Verdict logic:** PASS = SLO held during + recovered within budget; FAIL = SLO breach or over-budget; INCONCLUSIVE = no baseline. Pure function over 3 JSON files → fully testable.
- **Edge case:** INCONCLUSIVE when baseline missing — the Go gate must return a distinct exit (not FAIL) so the operator re-runs (matches the skill's INCONCLUSIVE handling).

#### Tasks / TDD / Acceptance / DoD
```
RED:     TestChaosSLO_HeldAndRecovered_Pass / _Breach_Fail / _NoBaseline_Inconclusive / _ParityWithBash
GREEN:   implement slo-compare
VERIFY:  go test ./...; loopzero-ops chaos slo-compare baseline.json during.json post.json == report.sh verdict
```
- [ ] slo-compare parity with report.sh verdict; chaos-test skill rewired for the verdict; baseline/run-*/cleanup KEEP-documented.

---

## Phase C4: OPERATOR HARD STOP — DOKS Drill LIVE Gate

### TC4.1 — Operator runs task drill:l2-scale with new wiring; GREEN unblocks deletion

#### Objective
Operator runs the DOKS prod-identical drill with the rewired skills + loopzero-ops subcommands in place (bash NOT yet deleted — both paths coexist for the validation run); confirm the drill provisions, bootstraps, runs L0-L2 harness, captures evidence (via the new loopzero-ops evidence/cost), and tears down CLEAN with the new teardown-validate.

#### Evidence
Per `reference_bash_to_go_migration` HARD STOP: dr/* drive real DOKS crons; rewiring to an unvalidated Go port breaks them. The drill is maturity gate (a). Real-money (~$0.35 L2) → operator-run.

#### Files to edit
```
(none — this is a LIVE validation run; produces evidence in .claude/knowledge-base/audits/)
```

#### Tasks / TDD / Acceptance / DoD
1. Operator: `HYDRA_NAMESPACE=... bash infra/helmfile/scripts/seed-local-user.sh` (mint), `task drill:l2-scale` with new wiring. 2. Confirm loopzero-ops drill jwt-validate/evidence/cost/teardown-validate ran in-flight. 3. Capture evidence.
```
RED:     N/A (LIVE operator gate)
VERIFY:  task drill:l2-scale exit 0; L2 success_rate>=90% p95<=10min; teardown CLEAN (0 residual via loopzero-ops teardown-validate); evidence JSON well-formed via loopzero-ops evidence-shape
```
- [ ] DOKS drill GREEN with new wiring; teardown CLEAN; evidence captured. **OPERATOR HARD STOP — autonomous loop pauses here.**

---

## Phase C5: Delete Migrated Bash (post-C4)

### TC5.1 — Delete fully-ported bash + ratchet baselines

#### Objective
After C4 GREEN, delete the bash whose logic fully moved to loopzero-ops (drill-jwt-gate, cost-report, evidence shaper if full, chaos report verdict if full), drop allowlist rows, ratchet baselines, `danglingrefs` GREEN.

#### Files to edit
```
infra/scripts/lib/drill-jwt-gate.sh + dr/cost-report.sh + (evidence-capture.sh/report.sh if fully ported) — DELETE
.claude/rules/shell-scripts-allowlist.md — drop rows
infra/quality/unhomed-baseline.json + script-count-baseline.json — ratchet
```

#### Tasks / TDD / Acceptance / DoD
1. Delete fully-ported bash (only those whose logic 100% moved + C4 proved the new path). 2. Drop rows; ratchet. 3. `dangling-script-refs` + `skill-paths` + `taskfile-interface` GREEN.
```
RED:     N/A
VERIFY:  dangling-script-refs PASS; skill-paths PASS; task quality:all GREEN; unhomed ratcheted
```
- [ ] ≥8 drill/chaos bash deleted; skills resolve; baselines ratcheted; `task quality:all` GREEN.

---

## Coverage Matrix

| # | Drill/chaos script(s) | Task | Resolution |
|---|---|---|---|
| 1 | drill-jwt-gate + drill-mint-jwt TTL-check | TC1.1 | → loopzero-ops drill jwt-validate (mint stays bash) |
| 2 | evidence-capture + teardown-validator + cost-report | TC2.1 | shaping/decision/arithmetic → loopzero-ops (gathering stays) |
| 3 | chaos baseline + report | TC3.1 | SLO-diff → loopzero-ops chaos slo-compare (sampling stays) |
| 4 | drill-helmfile-bootstrap (1126 LOC) | C0 classify | ORCHESTRATION → KEEP-OPERATOR |
| 5 | dr/{cleanroom,dogfood,chaos}-orchestrator + run-scenario/run-full/cleanup | C0 classify | skill-driven ORCHESTRATION → KEEP |
| 6 | dr/{cancel-refresh,state-reset,watchdog,preflight,jwt-diagnose} | C0 classify | mostly ORCHESTRATION; preflight isolation-check splits to C1/loopzero-ops |
| 7 | 4 drill SKILLs dangling risk | D2 + per-task | rewire in same PR; danglingrefs+skill-paths gates |
| 8 | DOKS drill unvalidated-port risk | C4 | operator LIVE gate before deletion |

**Coverage: 25/25 drill+chaos scripts addressed. Expected removal: ~8–13 (orchestration + 1126-LOC bootstrap KEEP).**

## Global Definition of Done

- [ ] C0 classification map complete (25/25)
- [ ] Pure-logic pieces (JWT/evidence/teardown/cost/SLO) → loopzero-ops, parity-proven, Go-tested
- [ ] 4 drill SKILLs rewired; `skill-paths` + `dangling-script-refs` GREEN
- [ ] **C4 DOKS drill (`task drill:l2-scale`) GREEN with new wiring (OPERATOR)**
- [ ] ≥8 drill/chaos bash deleted; baselines ratcheted; `script-count` down
- [ ] `/cross-validation 2026-05-24-drill-consolidation-migration` PASSED
- [ ] N/A — does not affect maturity scores (it is the drill TOOLING; the drill GATE itself is unchanged)
- [ ] **`/dogfood full` PASSED** (program-level, after Track A)

---

## Dogfood Gate (MANDATORY)

> Track C's LIVE gate is the **DOKS drill** (`task drill:l2-scale` GREEN with new wiring — C4, operator-run, ~$0.35). The program `/dogfood full` runs after the final track.

### Execution
```bash
# Operator (C4): provision + bootstrap + L2 harness + teardown, with new loopzero-ops wiring
task drill:l2-scale   # DOKS prod-identical; ~$0.35; MANDATORY teardown
# Confirm loopzero-ops drill {jwt-validate,evidence-shape,cost,teardown-validate} ran in-flight
```

### Pass Criteria
- [ ] DOKS drill GREEN: L2 success_rate≥90%, p95≤10min, teardown CLEAN (0 residual via loopzero-ops teardown-validate)
- [ ] No drill SKILL dangling (skill-paths GREEN)
- [ ] Evidence JSON well-formed via the ported shaper
- [ ] Bash deleted ONLY after this gate is GREEN

### On Failure
A drill failure with new wiring = the port broke the drill. Restore the bash for the offending piece, fix the loopzero-ops subcommand, re-run the DOKS drill. Never delete drill bash before the DOKS drill is GREEN with the new path (the develop-bkp lesson).
