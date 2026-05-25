# Drill / Chaos Migration Map (Track C — PURE-LOGIC vs ORCHESTRATION)

> Classification audit for `docs/plans/2026-05-24-drill-consolidation-migration-plan.md`
> Phase C0 (TC0.1). Mirror of `docs/operations/helmfile-delegation-map.md`. **Honest-yield
> gate before any porting** — per the program's "claim-25-deliver-8" guardrail.
>
> Created 2026-05-24. Read-only audit (no code changed).

## Method

Each of the 21 in-scope scripts (`infra/scripts/dr/*`, `infra/scripts/drill-*`,
`infra/scripts/lib/drill-*`, `infra/scripts/chaos/*`) was read and classified:

- **PURE-LOGIC** — validation / arithmetic / JSON-shaping / decision logic with no
  load-bearing provisioning call → migratable to a `loopzero-ops` Go subcommand (testable).
- **ORCHESTRATION** — `pulumi`/`doctl`/`helm`/`kubectl <verb>`/`curl`-LIVE/`tilt` or
  skill-sequencing → KEEP-OPERATOR (no-shell-wrap: a Go wrapper around `pulumi up` adds
  no value and loses testability — ADR D1).
- **SPLITTABLE** — a pure-logic core embedded in an orchestration script; the core
  migrates, the gathering/injection stays bash.

**Measurement caveat:** an initial `grep -cE 'kubectl (apply|exec|...)'` UNDERCOUNTED
scripts that call `kubectl --context <ctx> <verb>` (flags between `kubectl` and the verb),
e.g. `jwt-diagnose.sh` showed 0 but actually does `kubectl --context ... exec`. Every row
below was confirmed by READING, not just the grep count.

## Corrections to the plan's premises (the reason C0 exists)

| Plan assumption | Reality (evidence) | Correction |
|---|---|---|
| TC1.1: `drill-jwt-gate.sh` = pure JWT TTL/claim decode | It is `verify_jwt_auth_e2e`: mints a JWT **and `curl`s loopzero-api `/projects` LIVE**, asserts 2xx (`drill-jwt-gate.sh:42-67`). | **ORCHESTRATION** (live HTTP e2e), NOT pure-logic. No TTL-decode to port. |
| TC3.1: `chaos/report.sh` computes the SLO verdict | `report.sh` **receives** `--verdict` as an arg and formats markdown (`report.sh:12,149`). The verdict is COMPUTED in `run-scenario.sh:235-257`. | SLO-compare pure-logic is in **run-scenario.sh**, not report.sh. |
| TC3.1: `chaos/baseline.sh` has pure-logic diffing | `baseline.sh` only SAMPLES LIVE (`/health` curl + kubectl) → writes `baseline.json` (`baseline.sh:200`). | **ORCHESTRATION** (LIVE sampling), no diff logic. |

## Classification (21 scripts)

### PURE-LOGIC / SPLITTABLE → migratable to loopzero-ops (the real yield)

| Script | Pure-logic core (lines) | Orchestration that stays | Target |
|---|---|---|---|
| `dr/cost-report.sh` (159) | cost arithmetic + markdown given `--start/--end` (105-159) | `--auto` pulumi-output read (68-98) | `loopzero-ops drill cost` |
| `chaos/run-scenario.sh` (289) — SPLIT | SLO verdict: drift/budget over baseline/during/post JSON (138-257) | chaos-mesh `kubectl apply` injection + sampling | `loopzero-ops chaos slo-compare` |
**Confirmed migratable: 2 (cost-report, run-scenario SLO-compare) — both PORTED 2026-05-24.**

### The 3 "probable" candidates — RECLASSIFIED KEEP-OPERATOR after reading (2026-05-24)

| Script | Why NOT migratable (reading evidence) |
|---|---|
| `dr/teardown-validator.sh` (282) | Every step is `doctl <type> list` (gathering) + `apply doctl <type> delete` (force-delete) + flock + audit-trail writes. The residual COUNT is incremented inline as doctl lists return (lines 118-119,149-150,179-180); the retry-backoff is interleaved with `apply doctl vpcs delete` (210-224). Only a trivial 3-way verdict is pure (263-279: CLEAN/RECOVERED/PARTIAL from 3 counts → exit 0/0/3) — not worth a Go round-trip (the risky doctl work stays untested either way). KEEP-OPERATOR. |
| `dr/evidence-capture.sh` (298) | 21 `kubectl`/`doctl`/`pulumi` gather calls vs 4 jq lines. Phases: audit skeleton write, pulumi-outputs gather, kubectl pod-manifests+metrics gather per cluster, git-commit forensic anchor. No separable pure shaper. KEEP-OPERATOR. |
| `drill-mint-jwt.sh` (300) | The TTL assertion (EC-07, ≥7200s) was ALREADY consolidated into `lib/hydra-mint.sh:mint_jwt` (lines 232,237,245) — embedded in the port-forward+curl minting flow, NOT a separable pure decode. KEEP-OPERATOR. |

**FINAL honest yield: 2 SPLITs (logic extracted to loopzero-ops), ~0 full script deletes** (cost-report keeps its `--auto` Pulumi gathering; run-scenario keeps chaos-mesh injection). The 3 "probable" all collapsed to KEEP-OPERATOR on reading — the same over-count pattern as Track B. See `finding-2026-05-24-track-c-yield-is-two-splits.md`.

### ORCHESTRATION → KEEP-OPERATOR (no routine pure-logic to extract)

| Script | LOC | Why KEEP |
|---|---|---|
| `dr/cancel-refresh.sh` | 142 | pulumi cancel + state refresh (17 orch calls) |
| `dr/chaos-orchestrator.sh` | 275 | skill-driven chaos sequencing |
| `dr/cleanroom-orchestrator.sh` | 257 | skill-driven cleanroom sequencing |
| `dr/dogfood-orchestrator.sh` | 290 | skill-driven dogfood sequencing ($LOOPZERO CLI) |
| `dr/jwt-diagnose.sh` | 372 | LIVE read-only diagnostic (`kubectl exec`/`logs`/`curl` loopzero-api, 134-167) — like audit-argocd-drift |
| `dr/preflight.sh` | 381 | mostly cluster-reachability (kubectl); credential-isolation CHECK could split to loopzero-ops in C1 (SPLIT-candidate, low yield) |
| `dr/state-reset.sh` | 340 | 42 orch calls (pulumi/doctl state mutation) |
| `dr/watchdog.sh` | 150 | pulumi-destroy cron watchdog (17 orch) |
| `lib/drill-jwt-gate.sh` | 68 | **live curl e2e** (plan premise corrected) |
| `lib/drill-skill-common.sh` | 464 | skill-shared orchestration helpers |
| `lib/drill-dogfood-journeys.sh` | 242 | $LOOPZERO CLI journey orchestration |
| `drill-helmfile-bootstrap.sh` | 1126 | the prod-identical provisioning orchestrator (ADR D1 explicit KEEP) |
| `chaos/baseline.sh` | 228 | LIVE `/health` + kubectl sampling |
| `chaos/report.sh` | 197 | markdown formatter (receives `--verdict`; presentation only — trivial/low-value to port) |
| `chaos/run-full.sh` | 181 | chaos suite sequencing |
| `chaos/cleanup.sh` | 113 | chaos-mesh resource cleanup orchestration |

## Honest yield estimate

- **Confirmed PURE-LOGIC migratable: 2** (cost arithmetic, chaos SLO-compare).
- **Probable (line-range confirm in C1/C2): 3** (teardown-validate, evidence-shape, mint-jwt TTL).
- **Realistic delete-after-C4: ~2–4 scripts** — NOT the plan's "≥8". The big orchestration
  set (16 scripts incl. the 1126-LOC bootstrap) is irreducibly KEEP-OPERATOR. Most "drill
  scripts" are LIVE provisioning/diagnostic/skill-sequencing, not extractable logic.
- This is the SAME pattern as Track B: the coarse `MIGRATE-*` disposition over-counted;
  reading reveals the real logic-vs-orchestration split is smaller.

> **Note:** even when a pure-logic core is extracted (e.g. cost arithmetic), the host bash
> script often does NOT get deleted — it keeps the gathering half and calls the new
> `loopzero-ops` subcommand. Net `.sh` reduction comes only from scripts whose logic is 100%
> pure (cost-report is the strongest full-delete candidate; the SPLITs reduce LOC, not count).

## Next (per plan, autonomous portion)

- **C1/C2/C3:** port the 2 confirmed + confirm-then-port the 3 probable → loopzero-ops Go
  subcommands + hermetic tests + rewire the 4 SKILLs (D2). Bash deletion DEFERRED to C5.
- **C4 (OPERATOR HARD STOP):** `task drill:l2-scale` DOKS GREEN with new wiring before any delete.
