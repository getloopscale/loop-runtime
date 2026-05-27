# Plan: Deep Review Remediation — Naming + Credentials + Toolchain

> **Version 1.1** (edge-case review pass — 5 MUST FIX absorbed from `2026-05-21-deep-review-remediation-edge-cases.md`: EC-01 extended T1.3 to 11 files, EC-02 added empty-token fail-fast to T1.2, EC-03 extended exempt scope in T6.1, EC-04 made T7.5 alias mandatory, EC-05 added factual namespace verification pre-condition to T3.5). Comprehensive remediation of the 2026-05-21 file-by-file deep review (`/home/paulo/loopzero/.claude/knowledge-base/reviews/2026-05-21-deep-review-naming-credentials-toolchain.md`) across 376 reviewed files. The review identified **7 PROBLEM workflows, 10 PROBLEM scripts, 3 PROBLEM Pulumi/Helm artifacts, 1 unexecutable skill (`drill-test`), 10 confirmed false-positives, 33 distinct credentials with no SoT, 18 charts without `values.schema.json`, 10 hardcoded `loopzero-dev-password` instances, 8 honor-system rules that look enforced, and Pulumi DSN exports leaking secrets in plaintext**. This plan ships 8 phases (T0 quick-wins → T7 naming sweep) totaling ~52 tasks across ~80h of engineering work, lifting weighted system grade from **2.6/4.0 → ~3.5/4.0**. Each phase is independently committable; T0 is sponsor-direct quick-wins; T6 introduces CI hooks that prevent regression of the entire class of anti-patterns. Outcome: a system that is **honest** (no advisory gates pretending to enforce), **safe** (no plaintext secrets in Pulumi outputs, no hardcoded dev passwords reaching prod manifests), and **easy to manage** (single SoT for credentials, schema validation everywhere, skill docs matching code).

## Context

The 2026-05-21 user-triggered `/goal` audit produced an exhaustive file-by-file review covering 51 GitHub Actions workflows + 131 shell scripts + 47 Helm charts + 9 Pulumi stack configs + 5 helmfile env files + 100 Pulumi `src/` TS files + 43 skills + 7 governance rules + 10 Taskfiles = **376 top-level files**. Evidence is recorded in:

- `.claude/knowledge-base/reviews/2026-05-21-deep-review-naming-credentials-toolchain.md` (v1 sampled, ~3000 words)
- 4 parallel `general-purpose` agent reports retained in conversation context (file-by-file enumeration)

**Headline grades (v2 exhaustive):**

| Domain | v2 grade | Worst evidence |
|---|---|---|
| Credentials / env management | 1.9/4.0 | 33 distinct creds; 10× `loopzero-dev-password` literal; `Pulumi.prod.yaml` has ZERO `secure:` keys; ESO source-of-truth is in Go code, invisible in YAML |
| Scripts hygiene | 1.8/4.0 | ~85% of 131 scripts lack `set -euo pipefail`; 16 state-writers without `--dry-run`; D7' violation in `dr/preflight.sh:220,249` |
| Naming hygiene | 2.4/4.0 | `cell2-soaks.yml` propagates as cron-anchor in 6 other workflows; `phase6-dogfood-gate.yml`; `runtime2` camelCase numeric suffix in 11 Pulumi exports |
| CI workflows | 2.4/4.0 | 7 PROBLEM workflows; 4 still use `KUBECONFIG_DEV[_MGMT]` (same multilayer blocker cell2-soaks already fixed); Sunday 02:00 UTC triple-collision |
| Skills | 2.5/4.0 | `drill-test/SKILL.md` has 6 broken script paths — skill not executable |
| Helm dual system | 2.8/4.0 | 18 of 34 `infra/helm/` charts without `values.schema.json`; `loopzero-cloud` bootstrap vs published diverge on Ory namespace (`loopzero-auth` vs `ory`) |
| Taskfiles | 2.8/4.0 | `task doctor` is 298 LOC bash god-task; B-patch comment block stale (lists B9-B24 only, missing B14/15/17-19/22-23/25-39 which are real) |
| Rules enforcement | 3.0/4.0 | 8 honor-system rules that look enforced (exit codes 0-5, `withEnvironmentLock`, `pg_advisory_xact_lock`, T2/T3 self-classification, 95% confidence, AAA, SOLID, max 3 docs/task) |
| Pulumi src code | 3.2/4.0 | drill DSN exports `kratosDsn/hydraDsn/giteaDsn/loopzeroApiDsn` at `pulumi-drill/src/index.ts:478-481` NOT wrapped in `pulumi.secret()`; `GITEA_ADMIN_PASSWORD_DEFAULT = "loopzero-gitea-dev-2026"` hardcoded fallback in `mgmt-platform-services.ts:21` |

**Weighted overall: 2.6/4.0** (down from v1's 2.85 not because the system regressed but because exhaustive enumeration found more debt than sampling did).

## Objective

**"Done" means:** all 7 PROBLEM workflows fixed or deleted, all 10 PROBLEM scripts hardened, all 33 credentials enumerated in a single SoT with documented rotation, 0 hardcoded `loopzero-dev-password` literals reaching rendered manifests, 0 Pulumi outputs containing plaintext sensitive values, `drill-test` skill executable end-to-end, 8 honor-system rules either enforced by CI or downgraded to advisory-with-graduation-date, and weighted system grade ≥ **3.5/4.0**.

**Measurable goals:**

1. 7 PROBLEM workflows → renamed/rewritten/deleted (verified via filename + content scan)
2. 16 state-writing scripts → `--dry-run` flag supported (verified via `--dry-run` invocation test)
3. ~110 scripts missing `set -euo pipefail` → all add it (verified via grep gate in CI)
4. 33 credentials → single `docs/operations/credentials-required.md` SoT (verified via hermetic test that asserts every Secret in code is listed)
5. 10× `loopzero-dev-password` literal occurrences → 0 (verified via render-time gate: `helm template … | grep loopzero-dev-password` must return empty)
6. 18 charts missing `values.schema.json` → all 18 have schemas (verified via `find infra/helm -name Chart.yaml -exec dirname {} \; | xargs -I{} test -f {}/values.schema.json`)
7. drill DSN exports → wrapped in `pulumi.secret()` (verified via `pulumi stack output --json | jq` showing `[secret]` masking)
8. drill-test SKILL.md → 6 broken paths fixed (verified via hermetic test that scans skill docs and asserts every referenced script exists)
9. Weighted grade → ≥ 3.5/4.0 (re-run the deep-review audit, compare to baseline)

## ADRs

- **D1 — Single credentials SoT lives in `docs/operations/credentials-required.md`, NOT in the knowledge-base.**
  - **Rationale:** Operators read `docs/` first; `.claude/knowledge-base/` is internal AI context. Choosing `docs/operations/` makes the file user-visible. Alternative considered (rejected): nest under `.claude/knowledge-base/concepts/operations/` — rejected because operators don't naturally browse the knowledge-base.
  - **Consequences:** Enables hermetic test (`docs/operations/credentials-required.md` MUST list every Secret created by scripts). Constrains future credential additions to require a doc update before merge.

- **D2 — Hardcoded `loopzero-dev-password` literals are removed via fail-fast template variables.**
  - **Rationale:** Comments like `# LOCAL DEV ONLY` are hope, not enforcement. Each chart with the literal becomes `required` in values.schema.json + helm template fails if undefined. Standalone install gets an explicit user prompt; production gets Pulumi-injected value. Alternative considered (rejected): keep literal + add render-time grep gate — rejected because gate can be bypassed.
  - **Consequences:** Self-hosted standalone installs (the documented path in README) now require explicit password setup. Breaking change for any current user (zero, pre-release) but worth the safety.

- **D3 — All Pulumi outputs embedding `pulumi.Output<string>` of sensitive values MUST be wrapped in `pulumi.secret()`.**
  - **Rationale:** State export in plaintext is the difference between a state-file leak being inconvenient and being catastrophic. Drill DSN exports at `pulumi-drill/src/index.ts:478-481` currently expose `${drillPG.password}` in `kratosDsn`/`hydraDsn`/`giteaDsn`/`loopzeroApiDsn`.
  - **Consequences:** Add CI gate that lints Pulumi outputs against an allowlist of names that may NOT be wrapped (e.g., region, IDs). All other interpolations must be `pulumi.secret(pulumi.interpolate\`…\`)`.

- **D4 — Advisory gates without a graduation date are forbidden.**
  - **Rationale:** Per memory `feedback_broad_verification_post_engineering`, advisory mode is honor-system. Either gate enforces or it doesn't. If we can't graduate yet, the file must carry an explicit deadline comment + GitHub issue link tracking it.
  - **Consequences:** `audit-helm-drift.sh`, the Conftest "graduated text vs advisory code" inconsistency in `helm-charts.yml:138 vs L156`, and `check-cosign-cutover-deadline.sh` DORMANT branch all get either (a) graduated to blocking, or (b) graduation date + issue ID. CI gate scans for `continue-on-error: true` without a sibling comment containing `graduation: <ISO date>` and `issue: #N`.

- **D5 — Skill documentation paths are CI-validated against the filesystem.**
  - **Rationale:** `drill-test/SKILL.md` has 6 broken paths — skill is not executable. `dogfood/SKILL.md` references `GH_TOKEN` flow that code doesn't implement. `to-plan/SKILL.md` tells operators to run `task teardown` which doesn't exist. Memory `feedback_skill_md_outdated_vs_code` is the canonical case; this elevates it to systemic enforcement.
  - **Consequences:** New CI job greps `.claude/skills/*/SKILL.md` for paths matching `infra/scripts/`, `task ` invocations, and env var references — fails if path doesn't exist OR task doesn't exist OR env var name isn't found anywhere in code.

- **D6 — DEV-cluster pattern (`KUBECONFIG_DEV*`) is dead. Workflows depending on it must convert to drill pattern or delete.**
  - **Rationale:** `cell2-soaks.yml` already did this conversion in 2026-05-16 per finding-2026-05-16-gate-a-cell2-soaks-multilayer-blocker.md. Four remaining workflows (`operator-split-soak`, `pkg-soak`, `platform-health-7day-watch`) still use the dead pattern.
  - **Consequences:** `platform-health-7day-watch.yml` is deleted (gate dropped per CLAUDE.md 2026-05-03). `operator-split-soak` and `pkg-soak` are either rewritten to drill-stack pattern OR converted to `.yml.disabled` with rewrite ticket.

- **D7 — Naming convention is ratified into a single ADR.**
  - **Rationale:** 9 distinct conventions across the review (workflows, scripts, Pulumi outputs, Taskfile verbs, etc.). Without ratification, future drift recreates the same problems.
  - **Consequences:** New ADR file in `docs/decisions/` enumerates the 9 rules; PR template includes naming-convention checkbox; CI gate fails new files matching banned patterns (numeric suffix without semantic context, cell/sprint/phase identifiers, etc.).

- **D8 — Honor-system rules are honest. If automation can't enforce a rule, the rule file says so.**
  - **Rationale:** Per memory `feedback_anti_lying_workaround_pattern`, "looking enforced" is worse than "honest advisory". 8 rules in `.claude/rules/` are currently honor-system but read as binding.
  - **Consequences:** Each `.claude/rules/*.md` file gets a "Enforcement" section at top: `Enforced by: <hook path>` OR `HONOR-SYSTEM — no automation`. Phase 6 then implements CI gates for the 6 rules that ARE automatable (exit codes, withEnvironmentLock, pg_advisory_xact_lock, pulumi.secret wrap, set -euo, SKILL.md drift).

- **D9 — Phase 4 (workflows) is **independent** of Phase 1 (credentials), Phase 2 (scripts), Phase 3 (helm). All four can run in parallel. Phase 5 (taskfiles) depends only on Phase 2 outcomes. Phase 6 (CI gates) depends on Phases 1-5 being complete (gates are codified preventions, not new behavior).**
  - **Rationale:** Maximizes ralph/parallel-agent throughput. Quick-wins (T0) are sponsor-direct + serial; main work parallelizes.
  - **Consequences:** Operator can dispatch 4 implementer agents simultaneously after T0 + ADR ratification.

## Dependency Graph

```
Phase 0 (Quick wins, sponsor-direct)
    ├──▶ Phase 1 (Credentials SoT + hardening)        ──┐
    ├──▶ Phase 2 (Scripts hygiene + dry-run sweep)    ──┤
    ├──▶ Phase 3 (Helm schemas + drift + graduation)  ──┤
    └──▶ Phase 4 (Workflows refactor + DEV deletion)  ──┤
                                                        │
                                                        ▼
                              Phase 5 (Taskfile refactor — depends on T2)
                                                        │
                                                        ▼
                              Phase 6 (CI gates — depends on P1-5 done)
                                                        │
                                                        ▼
                              Phase 7 (Naming sweep + ADR ratification)
                                                        │
                                                        ▼
                              Phase 8 (External tool evaluation — optional)
```

Phases 1, 2, 3, 4 can run **in parallel** after T0 lands. Phase 5 depends on Phase 2 (uses the hardened scripts as canonical refs). Phase 6 finalizes the system by adding CI gates that prevent regression of P1-5 fixes. Phase 7 + 8 are independent strategic work.

---

## Phase 0: Quick Wins (sponsor-direct, ~6h total)

**Objective:** Ship the highest-ROI, lowest-effort fixes that already passed deep-review scrutiny and have no T2/T3 governance triggers.

### T0.1 — Fix drill-test SKILL.md broken paths

#### Objective
`drill-test/SKILL.md` has 6 broken paths — skill is NOT executable as written. Critical operator surface; the file claims to be a runnable spec but cites scripts at non-existent paths.

#### Evidence
- `.claude/skills/drill-test/SKILL.md:26-28, 114, 188, 222` — references `infra/scripts/drill-mint-jwt.sh` (OK), `infra/scripts/bootstrap-cell-*-cc-drill.sh` (NOT FOUND), `infra/scripts/drill-teardown-validator.sh` (moved to `dr/`), `infra/scripts/drill-cancel-refresh.sh` (moved), `infra/scripts/drill/provision.sh` (NEVER EXISTED), `infra/scripts/drill/teardown.sh` (NEVER EXISTED)

#### Files to edit
```
.claude/skills/drill-test/SKILL.md — replace 6 broken paths with canonical locations
```

#### Deep file dependency analysis
- File today: 250+ LOC procedural spec. Used by operator + Claude via the `drill-test` skill invocation.
- Change: search-and-replace 6 path strings. No semantic change.
- Downstream: no code consumers; only operator/Claude reads.

#### Deep Dives
- Path map:
  - `infra/scripts/drill-cancel-refresh.sh` → `infra/scripts/dr/cancel-refresh.sh`
  - `infra/scripts/drill-teardown-validator.sh` → `infra/scripts/dr/teardown-validator.sh`
  - `infra/scripts/drill/provision.sh` → `task drill:up` (taskfiles/drill.yml:37)
  - `infra/scripts/drill/teardown.sh` → `task drill:down` (taskfiles/drill.yml:78)
  - `infra/scripts/bootstrap-cell-1-cc-drill.sh` + `bootstrap-cell-2-cc-drill.sh` → `infra/scripts/drill-helmfile-bootstrap.sh` (consolidated 2026-05-20)

#### Tasks
1. Read `.claude/skills/drill-test/SKILL.md` fully.
2. Map every absolute/relative `infra/scripts/` path to current location.
3. Apply Edit operations for each broken path.
4. Add a section at top of SKILL.md: `## Paths last verified: 2026-05-21 (against canonical script tree)`.

#### TDD
```
RED:     test_skill_md_drill_test_paths_all_exist() — hermetic test in
         infra/tests/skills/skill-paths_test.sh that greps all script paths
         from drill-test/SKILL.md and asserts each exists. FAILS today.
GREEN:   Apply Edit operations.
REFACTOR: None expected.
VERIFY:  bash infra/tests/skills/skill-paths_test.sh
```

#### Acceptance Criteria
- [ ] Hermetic test `skill-paths_test.sh` PASSES for drill-test
- [ ] Manual scan: every `infra/scripts/` path in drill-test/SKILL.md exists on filesystem
- [ ] Manual scan: every `task X` invocation in SKILL.md exists in Taskfile

#### DoD
- [ ] Edit operations committed
- [ ] Hermetic test added + GREEN
- [ ] No new internal terms introduced

---

### T0.2 — Fix dogfood SKILL.md GH_TOKEN reference

#### Objective
`dogfood/SKILL.md:154-163` claims `GH_TOKEN` is read from `.env`. Actual code (`infra/tests/dogfood/full-loop.sh` + `preflight.sh`) reads `~/.loopzero/local-dogfood-token.txt`. Per memory `feedback_skill_md_outdated_vs_code`.

#### Files to edit
```
.claude/skills/dogfood/SKILL.md — fix Etapa 0 auth instructions
```

#### Tasks
1. Replace `.env` + `GH_TOKEN` references with `~/.loopzero/local-dogfood-token.txt` flow.
2. Remove any "set GH_TOKEN in .env" guidance.

#### TDD
```
RED:     test_dogfood_skill_no_gh_token_in_env() — assert SKILL.md does NOT mention reading GH_TOKEN from .env
GREEN:   Apply Edit.
VERIFY:  bash infra/tests/skills/skill-paths_test.sh dogfood
```

#### Acceptance Criteria
- [ ] grep -i "GH_TOKEN" .claude/skills/dogfood/SKILL.md returns 0 matches
- [ ] grep -i "local-dogfood-token" returns the canonical reference

#### DoD
- [ ] Edit committed + hermetic test extended to cover this assertion

---

### T0.3 — Fix to-plan SKILL.md `task teardown` reference

#### Objective
`to-plan/SKILL.md:194` instructs `task teardown && task setup && tilt up`. `task teardown` does NOT exist. Canonical is `task clean:all` (taskfiles/dev.yml:564).

#### Files to edit
```
.claude/skills/to-plan/SKILL.md — replace `task teardown` with `task clean:all`
```

#### Acceptance Criteria
- [ ] `grep -E "task teardown" .claude/skills/to-plan/SKILL.md` returns 0 matches
- [ ] Hermetic test asserts every `task X` invocation in SKILL.md exists in Taskfile

#### DoD
- [ ] Edit committed

---

### T0.4 — Wrap drill DSN exports in `pulumi.secret()`

#### Objective
`infra/pulumi-drill/src/index.ts:478-481` exports `kratosDsn`, `hydraDsn`, `giteaDsn`, `loopzeroApiDsn` via `pulumi.interpolate` embedding `${drillPG.password}`. These outputs are NOT wrapped in `pulumi.secret()`, so the DB password leaks in plaintext to `pulumi stack output` and to the state file.

#### Evidence
```ts
// infra/pulumi-drill/src/index.ts:478
export const kratosDsn = pulumi.interpolate`postgres://kratos:${drillPG.password}@${drillPG.host}:${drillPG.port}/kratos?sslmode=require`;
```

`pulumi.interpolate` returns `Output<string>` whose contents include the secret — but the secret marker is lost in serialization.

#### Files to edit
```
infra/pulumi-drill/src/index.ts:478-481 — wrap 4 DSN exports in pulumi.secret()
```

#### Tasks
1. Change `export const kratosDsn = pulumi.interpolate\`…\`` to `export const kratosDsn = pulumi.secret(pulumi.interpolate\`…\`)`.
2. Same for hydraDsn, giteaDsn, loopzeroApiDsn.
3. Run `cd infra/pulumi-drill && npx tsc --noEmit` to verify types.

#### TDD
```
RED:     test_drill_dsn_exports_are_secret() in infra/pulumi-drill/src/__tests__/secrets-hygiene.test.ts —
         imports each DSN export, asserts isSecret() returns true. FAILS today.
GREEN:   Add pulumi.secret() wrap.
VERIFY:  npx vitest run --root infra/pulumi-drill
```

#### Acceptance Criteria
- [ ] All 4 DSN exports wrapped
- [ ] vitest test asserts `isSecret(output) === true` for each
- [ ] `pulumi stack output --stack drill kratosDsn` shows `[secret]` (manual LIVE verification when drill stack provisioned)

#### DoD
- [ ] Edit committed
- [ ] vitest GREEN

---

### T0.5 — Fix dr/preflight.sh D7' violation

#### Objective
`infra/scripts/dr/preflight.sh:220, 249` still references deprecated `DRILL_DO_TOKEN`. Per D7' sponsor override 2026-05-16, the model is single `DIGITALOCEAN_TOKEN` shared between prod + drill with isolation via project tag + state bucket.

#### Files to edit
```
infra/scripts/dr/preflight.sh — remove DRILL_DO_TOKEN references, keep DIGITALOCEAN_TOKEN check
```

#### TDD
```
RED:     test_preflight_no_drill_do_token_ref() — assert grep -E "DRILL_DO_TOKEN" returns 0 matches
GREEN:   Edit.
VERIFY:  bash infra/tests/scripts/preflight_test.sh
```

#### Acceptance Criteria
- [ ] grep -E "DRILL_DO_TOKEN" infra/scripts/dr/preflight.sh returns 0
- [ ] grep -E "DIGITALOCEAN_TOKEN" still returns >= 1

#### DoD
- [ ] Edit committed
- [ ] Hermetic test added

---

### T0.6 — Delete 3 phantom workflows

#### Objective
Three workflows reference deleted paths and produce no useful signal:
1. `platform-health-7day-watch.yml` — gate explicitly dropped per CLAUDE.md 2026-05-03; still fires hourly with broken `KUBECONFIG_DEV_MGMT`.
2. `status-pr-gate.yml` — path filter `docs/evidence/**` matches no extant tree.
3. `status-refresh.yml` — self-admits stale L3 (docs/STATUS.md removed); daily no-op.

#### Files to edit
```
.github/workflows/platform-health-7day-watch.yml — DELETE
.github/workflows/status-pr-gate.yml — DELETE
.github/workflows/status-refresh.yml — DELETE
```

#### Tasks
1. Search workflows for any `workflow_call` or `needs:` reference to these — confirm none.
2. Search docs for any reference — update if needed.
3. `git rm` each file.

#### Acceptance Criteria
- [ ] 3 files deleted
- [ ] CI continues to pass on develop
- [ ] No workflow_call references broken

#### DoD
- [ ] Deletion committed
- [ ] CHANGELOG entry under `### Removed`

---

### T0.7 — Rename cell2-soaks.yml + update 6 cross-refs

#### Objective
`cell2-soaks.yml` was originally cell-2-specific; today provisions a generic 4-cluster drill stack. The name LIES. Six other workflows reference it by name as a cron-anchor.

#### Files to edit
```
.github/workflows/cell2-soaks.yml — RENAME to drill-prod-identical-soak.yml
.github/workflows/operator-split-soak.yml:14 — update comment ref
.github/workflows/pkg-soak.yml:17,32 — update comment refs
.github/workflows/decay-scan.yml:10 — update + fix stale timing comment
.github/workflows/chaos-suite.yml:17 — update comment ref
.github/workflows/drill-l2-scale.yml:25 — update comment ref
.github/workflows/drill-auto-destroy-watchdog.yml:13 — update comment ref
```

#### Deep file dependency analysis
- Concurrency group `cell2-soaks-${{ github.ref }}` → `drill-prod-identical-soak-${{ github.ref }}`
- Auto-issue label `cell-2-canary` → `drill-prod-identical`
- README.md + CLAUDE.md mentions (3 occurrences) — update

#### TDD
```
RED:     test_no_cell2_soaks_refs() — grep -rE "cell2-soaks" --include="*.yml" --include="*.md" excludes CHANGELOG.md, returns 0
GREEN:   git mv + sed across the 6 files.
VERIFY:  grep -rE "cell2-soaks" --include="*.yml" --include="*.md" | grep -v CHANGELOG
```

#### Acceptance Criteria
- [ ] 0 refs to `cell2-soaks` in active code/workflows (CHANGELOG.md exempt)
- [ ] CI runs the renamed workflow successfully on next cron

#### DoD
- [ ] Rename + 6 cross-ref updates committed
- [ ] CHANGELOG entry

---

### T0.8 — Atomic README.md verify

#### Objective
README.md:515 was patched inline in this session removing `pulumi-control-plane/`. Verify the fix landed + extend to other stale CLAUDE.md numeric claims.

#### Files to edit
```
README.md — verify line 515 + check workflow count (says 35, real is 51)
CLAUDE.md — workflow count (says 40, real is 51)
```

#### Acceptance Criteria
- [ ] grep "pulumi-control-plane" README.md returns 0
- [ ] README.md + CLAUDE.md state correct workflow count (51 as of 2026-05-21)

#### DoD
- [ ] Edits committed

---

## Phase 1: Credentials SoT + hardening (parallel after T0)

**Objective:** Establish single source of truth for all 33 credentials, eliminate hardcoded `loopzero-dev-password`, fix Pulumi.prod.yaml/staging.yaml empty-secret risk.

### T1.1 — Create credentials SoT document

#### Objective
A new `docs/operations/credentials-required.md` enumerates all 33 credentials with: name, source (Pulumi config / generate-secrets / generate-runtime-secrets / generate-build-secrets / bootstrap-oauth-clients / chart hardcoded), consumers (file:line × N), rotation cadence, and standalone/drill/prod applicability.

#### Files to edit
```
docs/operations/credentials-required.md (NEW)
docs/operations/README.md (NEW or existing — index)
infra/tests/docs/credentials-sot_test.sh (NEW) — hermetic test asserting every K8s Secret created by scripts appears in the SoT
```

#### Deep Dives
- Table columns: `name | source-of-truth | consumer-files | rotation | standalone? | drill? | prod?`
- Test strategy: grep `kubectl create secret ... \| kubectl apply -f - <<<.*kind: Secret` across all scripts; collect names; cross-reference against doc. Fail if any code-created Secret missing from doc.

#### TDD
```
RED:     test_credentials_sot_lists_all_secrets() — discovers every Secret name in scripts/charts, asserts it appears in credentials-required.md. FAILS today (file doesn't exist).
GREEN:   Author the doc + run test.
REFACTOR: None expected.
VERIFY:  bash infra/tests/docs/credentials-sot_test.sh
```

#### Acceptance Criteria
- [ ] Doc lists 33+ credentials
- [ ] Hermetic test PASSES (no code-created Secret missing from doc)
- [ ] Doc has table of contents + per-credential rotation playbook

#### DoD
- [ ] Doc + test committed
- [ ] CHANGELOG entry under `### Added`

---

### T1.2 — Eliminate dual Gitea token generation (B30 patch perpétuo)

#### Objective
Today `generate-secrets.sh:158` and `provision-api-tokens.sh:48` both generate `GITEA_TOKEN`. The B30 fix in `Taskfile.yml:244-251` reconciles the two via kubectl patch — a documented-but-kludgy workaround.

#### Files to edit
```
infra/helmfile/scripts/generate-secrets.sh — REMOVE GITEA_TOKEN generation; skip the key
infra/scripts/provision-api-tokens.sh — REMAIN canonical source for GITEA_TOKEN
Taskfile.yml:244-251 — REMOVE B30 kubectl patch (no longer needed)
```

#### Deep Dives
- `provision-api-tokens.sh` runs AFTER `generate-secrets.sh` in `bootstrap:standalone` (Taskfile.yml:233 then :234). If `generate-secrets.sh` skips GITEA_TOKEN, the Secret will be incomplete after step 1; step 2 must populate it.
- Test sequence: assert that immediately after `generate-secrets.sh`, the `loopzero-api-tokens` Secret has all keys EXCEPT `gitea-token`. After `provision-api-tokens.sh`, all keys including `gitea-token` are present.

#### TDD
```
RED:     test_generate_secrets_skips_gitea_token() — runs generate-secrets.sh in hermetic env, asserts gitea-token key absent
RED:     test_provision_api_tokens_populates_gitea_token() — runs provision then asserts key present
RED:     test_no_b30_kubectl_patch_in_taskfile() — grep Taskfile for the literal B30 patch, must be 0
RED:     test_provision_api_tokens_fails_on_empty_gitea_token() (EC-02) — mock curl returns {} (no sha1), assert script exits non-zero
GREEN:   Edit 3 files. Add after line 51 of provision-api-tokens.sh: `[[ -z "$GITEA_TOKEN" ]] && { echo "ERROR: Gitea token empty"; exit 1; }`
REFACTOR: None.
VERIFY:  task setup (LIVE) — confirm bootstrap still ends GREEN
```

#### Acceptance Criteria
- [ ] generate-secrets.sh no longer touches gitea-token
- [ ] provision-api-tokens.sh is sole writer
- [ ] Taskfile B30 patch removed
- [ ] LIVE `task setup` GREEN on fresh k3d

#### DoD
- [ ] 3-file edit committed
- [ ] Hermetic tests + LIVE setup GREEN

---

### T1.3 — Remove 11× hardcoded credential literals (10× `loopzero-dev-password` + 1× `loopzero-gitea-dev-2026`)

#### Objective
Hardcoded credential literals appear in **11 places** (EC-01 found 1 missing from initial sweep). Each is a known-default credential that can reach a real environment via `helmfile -e standalone sync`.

#### Files to edit
```
infra/helm/postgresql/values.yaml:19 — required field, no default
infra/helm/minio/values.yaml:7 — required, no default
infra/helm/loki/values.yaml:25 — required
infra/helm/monitoring/values.yaml:57 — required
infra/helm/gitea/values.yaml:19 — required
infra/helm/ory-hydra/values.yaml.gotmpl:18 — DSN constructed from required vars
infra/helm/ory-kratos/values.yaml.gotmpl:14 — DSN constructed
infra/helmfile/scripts/generate-secrets.sh:140 — exit 1 if LOOPZERO_DB_PASSWORD unset
infra/helmfile/scripts/generate-runtime-secrets.sh:119 — exit 1 if S3_SECRET unset
infra/helmfile/scripts/seed-local-user.sh:75 — read from PG Secret, not hardcoded
infra/scripts/provision-api-tokens.sh:23 — replace ${GITEA_PASS:-loopzero-gitea-dev-2026} with ${GITEA_PASS:?required} (EC-01)
```

#### Render-time grep gate (extended per EC-01)
Gate scans rendered manifests for BOTH literals:
- `loopzero-dev-password` (10 sites)
- `loopzero-gitea-dev-2026` (1 site — and the matching Pulumi removal in T1.4)
Must return 0 matches.

#### Deep Dives
- For chart values: change pattern from `password: loopzero-dev-password` (literal) to `password: {{ required "missing required value: postgresql.password" .Values.postgresql.password }}`. Helm template fails fast at render time if undefined.
- For scripts: replace fallback `${VAR:-loopzero-dev-password}` with `${VAR:?VAR is required for this script}`.
- Render-time gate: new CI step that runs `helm template` on every chart + greps rendered manifests for `loopzero-dev-password` — must return 0 matches.

#### TDD
```
RED:     test_no_loopzero_dev_password_in_rendered_manifests() — helm template all charts → grep -c loopzero-dev-password = 0
RED:     test_chart_fails_without_required_password() — helm template chart without value → exit 1 with "missing required value"
GREEN:   Edit 10 sites.
REFACTOR: None.
VERIFY:  task helm:test:all-charts (LIVE)
```

#### Acceptance Criteria
- [ ] grep -rn "loopzero-dev-password" infra/helm/ infra/helmfile/scripts/ returns 0
- [ ] Render-time gate PASSES (added to helm-charts.yml workflow)
- [ ] LIVE `task setup` still GREEN (now requires LOOPZERO_DB_PASSWORD + similar set in env)

#### DoD
- [ ] 10-file edit committed
- [ ] Render-time gate added to CI
- [ ] CHANGELOG entry under `### Security`

---

### T1.4 — Remove GITEA_ADMIN_PASSWORD_DEFAULT hardcoded fallback

#### Objective
`infra/pulumi-platform/src/components/mgmt-platform-services.ts:21` declares `GITEA_ADMIN_PASSWORD_DEFAULT = "loopzero-gitea-dev-2026"`. Used as `?? GITEA_ADMIN_PASSWORD_DEFAULT` in 2 places. No env-guard prevents prod fall-through.

#### Files to edit
```
infra/pulumi-platform/src/components/mgmt-platform-services.ts:21 — DELETE constant; require config
infra/pulumi-platform/src/components/mgmt-gitea-bootstrap.ts:112 — propagate failure
```

#### TDD
```
RED:     test_no_hardcoded_gitea_password_default() in pulumi-platform tests — grep src/ for "loopzero-gitea-dev-2026" returns 0
GREEN:   Replace `?? GITEA_ADMIN_PASSWORD_DEFAULT` with `?? (() => { throw new Error('gitea password required') })()`
VERIFY:  npx tsc --noEmit + vitest
```

#### Acceptance Criteria
- [ ] grep "loopzero-gitea-dev-2026" infra/pulumi-platform/src returns 0
- [ ] Compile passes
- [ ] vitest GREEN

#### DoD
- [ ] Edit committed
- [ ] Hermetic test added

---

### T1.5 — Commit placeholder `secure:` entries to Pulumi.prod/staging.yaml

#### Objective
`Pulumi.prod.yaml` and `Pulumi.staging.yaml` ship with ZERO secure keys. Required secrets are documented only in comments. `pulumi preview --stack prod` doesn't fail-fast — it discovers missing secrets deep in resource creation.

#### Files to edit
```
infra/pulumi/Pulumi.prod.yaml — add 8 secure: keys with placeholder ciphertext (e.g., a stack-passphrase-encrypted "PLACEHOLDER")
infra/pulumi/Pulumi.staging.yaml — same 5 keys
infra/pulumi-platform/Pulumi.dev.yaml — same audit
```

#### Deep Dives
- Pulumi secrets are encrypted with stack passphrase. Committing `secure: <ciphertext>` of `"REQUIRED_PLACEHOLDER"` means `pulumi preview` resolves the key as the literal string, which downstream resources will then reject (DNS API call with token="REQUIRED_PLACEHOLDER" fails with 401, an explicit-named-failure vs current deep-stack provider error).
- Alternative considered: `pulumi config require <key>` — Pulumi has no such gate. Workaround is committing placeholder + adding our own preview-gate script.

#### TDD
```
RED:     test_pulumi_prod_has_all_required_secrets() — preview the stack with stack-passphrase set, expect error to name MISSING key
GREEN:   Commit placeholder secure: entries.
VERIFY:  npx pulumi preview --stack prod (LIVE) — error should now say "secret X = REQUIRED_PLACEHOLDER" not "DigitalOcean 401"
```

#### Acceptance Criteria
- [ ] Pulumi.prod.yaml has 8+ `secure:` entries (one per documented requirement)
- [ ] Pulumi.staging.yaml has 5+
- [ ] Preview-gate script `infra/scripts/pulumi-preview-gate.sh` validates no entry is literal `REQUIRED_PLACEHOLDER`

#### DoD
- [ ] Edits committed
- [ ] Preview gate script + hermetic test

---

### T1.6 — ESO ExternalSecret CRs visible in YAML

#### Objective
ESO source-of-truth is currently in Go code (`api/internal/k8s/secret_k8s_manager.go`). No `kind: ExternalSecret` exists in `infra/helm/` or `charts/` templates. Operator reviewing YAML can't see what ESO syncs.

#### Files to edit
```
charts/loopzero-api/templates/external-secret-tokens.yaml (NEW) — declare the runtime-created ExternalSecret as Helm-managed
api/internal/k8s/secret_k8s_manager.go — remove runtime CR creation; just wait for ESO-synced Secret to exist
```

#### Deep Dives
- This is a partial T2 change (controller reconcile logic) — but limited to migrating from imperative to declarative. Need to verify no semantic behavior change.
- Likely needs `meeting-orchestrator` invocation per `.claude/rules/pre-release-governance.md`. Mark as T2.

#### Tasks
1. Diagnose what Go code creates ExternalSecret today
2. Author governance request
3. Get approval before code change
4. Move to chart template
5. Remove Go runtime creation

#### TDD
```
RED:     test_external_secret_in_chart_template() — chart-render finds kind: ExternalSecret
RED:     test_no_runtime_external_secret_creation() — grep api/ for "ExternalSecret" returns 0
GREEN:   Chart edit + Go removal
VERIFY:  task helm:test:all-charts + LIVE dogfood
```

#### Acceptance Criteria
- [ ] ExternalSecret CR in chart template
- [ ] Go code doesn't create it
- [ ] LIVE dogfood shows ESO sync working

#### DoD
- [ ] Governance approval recorded
- [ ] Edits committed
- [ ] LIVE dogfood GREEN

---

## Phase 2: Scripts hygiene + dry-run sweep

**Objective:** Add `set -euo pipefail` to ~110 scripts; add `--dry-run` to 16 state-writers; delete confirmed orphans; consolidate duplicates.

### T2.1 — Add `set -euo pipefail` to all ~110 scripts missing it

#### Files to edit
~110 .sh files across infra/scripts/, infra/scripts/dr/, infra/scripts/chaos/, infra/scripts/lib/, infra/scripts/quality/, infra/scripts/governance/, infra/helmfile/scripts/ (per agent report Table 1-7)

#### Deep Dives
- Sourced libs (`lib/*.sh`): MUST NOT add `set -e` at top (propagates to caller). Add `set -u` (nounset) only, or wrap in function.
- The 20 scripts already with `set -euo pipefail` are model citizens; copy pattern.

#### TDD
```
RED:     test_all_scripts_have_set_euo_pipefail() — grep first 5 lines of each script; fails if missing
GREEN:   Sed insert at top of each.
REFACTOR: Some scripts will break — these had silent failures masked. Fix each.
VERIFY:  bash infra/tests/scripts/shell-hygiene_test.sh
```

#### Acceptance Criteria
- [ ] 110 of ~110 non-lib scripts have `set -euo pipefail` in top 5 lines
- [ ] All hermetic tests still GREEN after the sweep
- [ ] LIVE `task setup` GREEN

#### DoD
- [ ] Sweep committed (likely 1 large commit or batched per-dir)
- [ ] CI gate added to fail new scripts without the line

---

### T2.2 — Add `--dry-run` to 16 state-writing scripts

#### Files to edit
```
infra/scripts/bootstrap-cluster.sh
infra/scripts/bootstrap-platform.sh
infra/scripts/bootstrap-seed-tenant-cr.sh
infra/scripts/build-and-push-dockerfile-lint.sh
infra/scripts/build-and-push-loopzero-packs.sh
infra/scripts/dr-drill.sh
infra/scripts/dr/teardown-validator.sh
infra/scripts/dr/watchdog.sh
infra/scripts/drill-mint-jwt.sh
infra/scripts/dr/jwt-diagnose.sh
infra/scripts/dr/preflight.sh
infra/helmfile/scripts/bootstrap-oauth-clients.sh
infra/helmfile/scripts/seed-local-user.sh
infra/helmfile/scripts/wire-clusters.sh
infra/scripts/run-migrations.sh
infra/scripts/install:sops (Taskfile.yml:87)
```

#### Deep Dives
- Pattern: introduce `DRY_RUN=${DRY_RUN:-0}` + helper function `apply() { if [ "$DRY_RUN" = "1" ]; then echo "DRY_RUN: $*"; else "$@"; fi }` + replace `kubectl apply -f X` with `apply kubectl apply -f X`.
- For destructive scripts (teardown-validator, watchdog), `--dry-run` MUST log every doctl/pulumi destroy intent without executing.

#### TDD
```
RED:     test_state_writers_support_dry_run() — invoke each with --dry-run; assert no kubectl create, no doctl delete actually fired
GREEN:   Add flag handling.
VERIFY:  bash infra/tests/scripts/dry-run_test.sh
```

#### Acceptance Criteria
- [ ] All 16 scripts accept `--dry-run` flag
- [ ] All 16 scripts log what they WOULD do without doing it
- [ ] Hermetic test passes

#### DoD
- [ ] 16-file edit committed
- [ ] Hermetic test
- [ ] CI gate: state-write scripts MUST have `--dry-run` support (new contributor enforcement)

---

### T2.3 — Eval usage audit + sanitization

#### Files to edit
```
infra/scripts/clean-incremental.sh:24 — replace eval "$@" with array expansion
infra/scripts/lib/drill-dogfood-journeys.sh:207 — replace eval "$cmd" with bash -c "$cmd" or function call
```

#### TDD
```
RED:     test_no_eval_in_scripts() — grep -rE "^[^#]*\beval\s" infra/scripts/ excludes annotated SAFE-eval lines
GREEN:   Replace.
VERIFY:  hermetic test
```

#### Acceptance Criteria
- [ ] All 10 eval sites either marked `# eval-safe: <justification>` OR replaced
- [ ] Hermetic gate prevents new unannotated evals

---

### T2.4 — Delete 21 confirmed orphan scripts

#### Files to edit
```
DELETE (after stakeholder confirm):
- audit-helm-drift.test.sh (self-test, not wired into CI)
- check-prod-vap-readiness.sh
- check-stack-coverage.sh
- cleanup-orphan-chaos.sh
- crd-semantic-diff.test.sh
- dr/cost-report.sh
- ensure-pg.sh
- governance/run-triangulation.sh
- migration_test_coverage.sh
- pkg-usage-matrix.sh
- provision-homolog.sh
- quality/baseline-compare.sh
- quality/flaky-track.sh
- validate-infra.sh
- lib/bootstrap-cell-cc-drill-common.sh
- lib/sops.sh
- helmfile/scripts/migrate-database.sh
- helmfile/scripts/regression-checks.sh
- file-corrections-item-I-upstream.sh
```

#### Deep Dives
- DO NOT DELETE: `velero-dr-drill.sh` (per memory `feedback_velero_must_be_in_drill` MUST be wired in); `operator-cutover-execute.sh` and `pulumi-orphan-rm.sh` (need owner triage — break-glass tools).

#### Acceptance Criteria
- [ ] 19 files deleted (operator confirms each)
- [ ] No callers broken
- [ ] CHANGELOG entry

---

### T2.5 — Consolidate duplicate `health-check.sh` (3 instances)

#### Files to edit
```
infra/scripts/health-check.sh — rename to health-check-platform.sh
infra/scripts/governance/health-check.sh — rename to health-check-governance.sh
infra/helmfile/scripts/health-check.sh — KEEP as health-check-postsync.sh
```

#### Acceptance Criteria
- [ ] 3 distinct names, distinct purposes
- [ ] All callers updated

---

### T2.6 — Consolidate JWT-related scripts

#### Files to edit
```
infra/scripts/drill-mint-jwt.sh — consolidate into infra/helmfile/scripts/lib/hydra-mint.sh
infra/scripts/dr/jwt-diagnose.sh — KEEP as canonical diagnostic
```

#### Acceptance Criteria
- [ ] 1 minter (lib/hydra-mint.sh), 1 diagnostic (dr/jwt-diagnose.sh)
- [ ] drill-mint-jwt.sh deleted or stubbed

---

## Phase 3: Helm schemas + drift + graduation

### T3.1 — Add `values.schema.json` to 18 charts missing it

#### Files to edit
18 NEW schema files under:
```
infra/helm/{argo-rollouts, blackbox-exporter, external-secrets, k6-operator, keda, keda-http-addon, loki, minio, monitoring, nfs-server-provisioner, otel, policy-controller, promtail, redis, tempo, traefik}/values.schema.json (NEW × 16)
```

#### Deep Dives
- Use `helm-schema` (dadav) tool to auto-generate from values.yaml comments (per awesome-helm recommendation).
- Required keys for credential-bearing charts: postgresql.password, minio.rootPassword, loki.secret_access_key, monitoring.adminPassword (after T1.3 made these required).

#### TDD
```
RED:     test_all_infra_helm_charts_have_schema() — for each chart dir, assert values.schema.json exists
RED:     test_chart_renders_with_minimal_required_values() — helm template fails without required, succeeds with them
GREEN:   Generate schemas.
VERIFY:  task helm:lint --strict (must pass)
```

#### Acceptance Criteria
- [ ] 34 of 34 infra/helm/ charts have values.schema.json
- [ ] helm lint --strict GREEN for all

---

### T3.2 — Graduate audit-helm-drift.sh advisory → blocking

#### Files to edit
```
infra/scripts/audit-helm-drift.sh:19 — remove "advisory mode" notice
.github/workflows/charts-drift-cron.yml — fail on drift not in APPROVED_DRIFTS
```

#### Acceptance Criteria
- [ ] Drift gate FAILS CI on unapproved drift
- [ ] Pattern matches commit a1b39a3b (skip-link + mock-budget graduation)

---

### T3.3 — Graduate Conftest gate advisory → blocking

#### Files to edit
```
.github/workflows/helm-charts.yml:138-156 — resolve text-vs-code contradiction; either graduate or document why advisory remains
```

#### Acceptance Criteria
- [ ] Either gate is BLOCKING (with green LIVE evidence) OR file has `graduation: <date>` + `issue: #N` markers

---

### T3.4 — Diff-test loopzero-api/loopzero-cloud/admission-policies dual charts

#### Files to edit
```
infra/tests/helm/dual-chart-drift_test.sh (NEW) — compare bootstrap vs published for the 3 charts
```

#### Deep Dives
- For each of 3 charts, render both bootstrap + published with same input, diff the rendered YAMLs, classify diff as INTENTIONAL (in approved list) or REGRESSION.
- Approved diff today: admission-policies mode=enforce vs warn (per CLAUDE.md), loopzero-api dev vs prod image tag.

#### Acceptance Criteria
- [ ] Diff test PASSES with approved-only differences
- [ ] FAILS on novel divergence

---

### T3.5 — Resolve loopzero-cloud namespace divergence

#### Files to edit
```
infra/helm/loopzero-cloud/values.yaml — align Ory namespace assumption with charts/loopzero-cloud/
charts/loopzero-cloud/values.yaml — align with bootstrap chart per EC-05 factual finding
```

#### Deep Dives — EC-05 mandatory pre-condition before editing ANY file
**Critical context:** `infra/helm/loopzero-cloud/values.yaml:47-50` comment **B49** says verbatim:
> "Default values pointed to `loopzero-system` which fails DNS resolution."

This means `loopzero-system` is the value that **already broke** in 2026-04-28. The v1.0 plan suggested standardizing on `loopzero-system` — that would re-introduce the exact regression B49 fixed.

#### Mandatory pre-condition (DoD step 0)
Before editing ANY chart, operator MUST run:
```bash
task setup  # fresh k3d standalone
kubectl get ns | grep -E 'loopzero-auth|ory|loopzero-system'
kubectl -n <discovered-ns> get pods -l app.kubernetes.io/name=hydra
```
Then EITHER:
- If `loopzero-auth` is the real namespace → standardize BOTH charts on `loopzero-auth`
- If `ory` is the real namespace → standardize BOTH charts on `ory`
- If `loopzero-system` somehow is the real namespace AND Hydra runs there → re-evaluate B49 deprecation
- **DO NOT** choose a namespace by reading the values.yaml alone

#### Deep Dives — namespace decision matrix
- `infra/helm/loopzero-cloud/values.yaml`: currently `loopzero-auth` (per B49 fix, post-2026-04-28)
- `charts/loopzero-cloud/values.yaml`: currently `ory` (untouched by B49)
- CLAUDE.md B35: mentions `SEED_HYDRA_NS=loopzero-system` for k3d standalone — but that's for a SEED script env var, NOT necessarily where Hydra pods live
- Per CLAUDE.md helmfile env files (`infra/helmfile/environments/standalone.yaml`): definitive answer lives here — must read

#### Acceptance Criteria
- [ ] EC-05 pre-condition: `kubectl get ns` evidence captured + recorded in `.claude/knowledge-base/concepts/operations/finding-2026-05-21-ory-namespace.md` BEFORE any edit
- [ ] Decision documented with file:line evidence
- [ ] Both charts use same Ory namespace (whichever proved factual)
- [ ] LIVE setup still GREEN (no auth regression, especially Hydra → API token mint flow)
- [ ] B49 comment updated to reflect the final decision (don't leave stale "fails DNS" warning if canonical changed)

---

## Phase 4: Workflows refactor

### T4.1 — Rewrite operator-split-soak.yml + pkg-soak.yml to drill pattern

#### Files to edit
```
.github/workflows/operator-split-soak.yml — replace KUBECONFIG_DEV usage with drill stack provision
.github/workflows/pkg-soak.yml — same
```

#### Deep Dives
- Pattern reference: how `cell2-soaks.yml` was rewritten in 2026-05-16 (drill stack provision → soak → teardown).
- Cost: each rewritten workflow will cost ~$0.30-2.00/run on cron. Need cost guard via env-approval if total >$10/month.

#### Acceptance Criteria
- [ ] No KUBECONFIG_DEV* secret references
- [ ] Workflows provision drill stack + soak + teardown
- [ ] Cron runs GREEN on next scheduled fire

---

### T4.2 — Repoint adversarial-spotcheck + cross-validation-weekly to .claude/knowledge-base/

#### Files to edit
```
.github/workflows/adversarial-spotcheck.yml — change output path from docs/governance/ to .claude/knowledge-base/audits/
.github/workflows/cross-validation-weekly.yml — change scan paths from docs/plans/, docs/evidence/ to .claude/knowledge-base/plans/, .claude/knowledge-base/audits/
```

#### Acceptance Criteria
- [ ] No grep hits for `docs/governance/`, `docs/evidence/` in active workflows
- [ ] Workflows produce real output on next run

---

### T4.3 — Pin 6 floating action versions to SHA

#### Files to edit
```
.github/workflows/ci.yml: golangci/golangci-lint-action@v7 → SHA
.github/workflows/helm-charts.yml: helm/chart-testing-action@v2.6.1, helm/kind-action@v1.10.0 → SHA
.github/workflows/infra-preview.yml + pulumi-archive.yml: pulumi/actions@v6 → SHA
.github/workflows/velero-backup-check.yml: azure/setup-kubectl@v4 → SHA
.github/workflows/reusable-setup-helm.yml: azure/setup-helm@v4 → SHA
```

#### Acceptance Criteria
- [ ] All actions SHA-pinned
- [ ] Action-pinning gate in CI passes

---

### T4.4 — Add `timeout-minutes` to all jobs missing it

#### Files to edit
```
.github/workflows/{helm-charts, quality-gates, release, cosign-rotation-drill, infra-gates, pulumi-validate, pulumi-reconstruction-drill, flaky-aggregator}.yml — add timeout-minutes
```

#### Acceptance Criteria
- [ ] Every job has explicit `timeout-minutes`
- [ ] Gate prevents new jobs without timeout

---

### T4.5 — Approval gate on expensive workflows

#### Files to edit
```
.github/workflows/load-test-pr.yml — add `if:` gate restricting to trusted PR authors OR require label
.github/workflows/drill-l2-scale.yml — add environment with approval for non-cron runs
```

#### Acceptance Criteria
- [ ] Untrusted PR cannot trigger expensive workflow runs
- [ ] Manual dispatches require approval

---

### T4.6 — Stagger Sunday 02:00 UTC cron triple-collision

#### Files to edit
```
.github/workflows/cell2-soaks.yml (now drill-prod-identical-soak.yml) — keep at Sun 02:00 UTC
.github/workflows/decay-scan.yml — move to Sun 02:30 UTC
.github/workflows/drill-skills-cron.yml — move to Sun 03:30 UTC
```

#### Acceptance Criteria
- [ ] No more than 1 workflow at any 5-min UTC window
- [ ] Cross-references in comments updated

---

### T4.7 — Audit + classify all `continue-on-error: true`

#### Files to edit
Every workflow with `continue-on-error: true` gets a comment block adjacent:
```
# graduation: 2026-MM-DD
# issue: #N
# rationale: <one line>
```

OR remove the flag.

#### Acceptance Criteria
- [ ] Every `continue-on-error: true` has the annotation OR is removed
- [ ] CI gate enforces this (D4)

---

## Phase 5: Taskfile refactor

### T5.1 — Extract `task doctor` (298 LOC bash) to Go binary

#### Files to edit
```
tools/loopzero-doctor/main.go (NEW)
tools/loopzero-doctor/checks/*.go (NEW per check family)
taskfiles/dev.yml:7 — `doctor: cmds: [go run ./tools/loopzero-doctor]`
```

#### Deep Dives
- Output formats: human (default) + `--json` for programmatic.
- 38+ checks should be table-driven, not 298 LOC inline bash.

#### Acceptance Criteria
- [ ] task doctor invokes Go binary
- [ ] Same 38 checks (PASS/FAIL parity)
- [ ] `--json` output is stable contract
- [ ] Easier to test (Go unit tests)

---

### T5.2 — Split test.yml (103 tasks, 1241 LOC)

#### Files to edit
```
taskfiles/test.yml — KEEP only test:* tasks
taskfiles/validate.yml (NEW) — extract validate:*
taskfiles/load.yml (NEW) — extract load:*
taskfiles/chaos.yml (NEW) — extract chaos:*
taskfiles/governance.yml (NEW) — extract governance:*
taskfiles/coverage.yml (NEW) — extract coverage:* + quality:*
Taskfile.yml — register new sub-files
```

#### Acceptance Criteria
- [ ] No file >500 LOC
- [ ] Task discovery (`task -l`) shows same set
- [ ] All existing invocations work

---

### T5.3 — Extract B9-B30 patches from bootstrap:standalone

#### Files to edit
```
infra/helmfile/scripts/bootstrap-patch-eso.sh (NEW) — B10
infra/helmfile/scripts/bootstrap-patch-oauth.sh (NEW) — B12
infra/helmfile/scripts/bootstrap-patch-sigstore.sh (NEW) — B16
infra/helmfile/scripts/bootstrap-patch-policies.sh (NEW) — B13 admission
infra/helmfile/scripts/bootstrap-patch-cells.sh (NEW) — B24
infra/helmfile/scripts/bootstrap-patch-argocd-gitea-creds.sh (NEW) — B30
Taskfile.yml:208 — replace inline patches with task: calls
```

#### Acceptance Criteria
- [ ] task bootstrap:standalone uses named sub-tasks
- [ ] Each patch independently re-runnable
- [ ] B-patch comment block updated to point at scripts

---

### T5.4 — Extract Python heredocs

#### Files to edit
```
infra/scripts/validate-helm-charts.py (NEW) — extracted from test.yml:630
infra/scripts/validate-k8s-versions.py (NEW) — extracted from test.yml:910
taskfiles/validate.yml (or test.yml) — invoke via python3 instead of inline
```

#### Acceptance Criteria
- [ ] No multi-language heredocs in Taskfile
- [ ] Python scripts linted by ruff in CI

---

### T5.5 — Update stale B-patch comment block

#### Files to edit
```
Taskfile.yml:214-228 — replace narrative with reference to CHANGELOG.md "Cleanroom Dogfood Loop" section
```

#### Acceptance Criteria
- [ ] No stale B-patch enumeration in code
- [ ] Single SoT in CHANGELOG

---

### T5.6 — Split `images:build` into composable per-image tasks

#### Files to edit
```
taskfiles/infra.yml:704 — split into images:build:api, :operators, :clone-runner, :packs-runner, :lint, :syft, :cosign + composite parent
```

#### Acceptance Criteria
- [ ] Each image rebuildable independently (~3min)
- [ ] Composite parent rebuilds all
- [ ] CI dev loop faster

---

### T5.7 — Canonicalize verb taxonomy

#### Files to edit
```
taskfiles/dev.yml — rename cleanup:incremental → clean:incremental
Taskfile.yml — add task teardown alias to clean:all (since to-plan SKILL refs it)
.claude/skills/to-plan/SKILL.md — already fixed in T0.3 (uses clean:all)
```

#### Acceptance Criteria
- [ ] Single `clean:*` family
- [ ] No `cleanup:*` orphan
- [ ] task teardown either exists OR no skill references it

---

## Phase 6: CI gates (codify all of P1-5 as enforcement)

**Objective:** Prevent regression of every anti-pattern we just fixed.

### T6.1 — CI gate: scripts must have `set -euo pipefail`

#### Files to edit
```
infra/scripts/quality/check-shell-hygiene.sh (NEW)
.github/workflows/quality-gates.yml — add hygiene job
```

#### Acceptance Criteria
- [ ] Gate fails PR that adds .sh without `set -euo pipefail` in top 5 lines
- [ ] Exempt: sourced libs — path matches `**/lib/*.sh` (covers both `infra/scripts/lib/` AND `infra/helmfile/scripts/lib/`, per EC-03)
- [ ] Auto-detection fallback: scripts containing `BASH_SOURCE[0]` guard are recognized as sourced even outside `lib/` dirs

---

### T6.2 — CI gate: state-writers must support `--dry-run`

#### Files to edit
```
infra/scripts/quality/check-dry-run-support.sh (NEW)
```

#### Acceptance Criteria
- [ ] Gate detects scripts with kubectl apply / helm install / pulumi up / doctl / git push
- [ ] Asserts each has `--dry-run` flag handling
- [ ] FAILS on missing

---

### T6.3 — CI gate: Pulumi outputs sensitive values must be `pulumi.secret()`-wrapped

#### Files to edit
```
infra/scripts/quality/check-pulumi-secrets.sh (NEW) — AST-grep Pulumi src/index.ts for outputs embedding $.password / $.token / $.key / $.secret without pulumi.secret() wrap
```

#### Acceptance Criteria
- [ ] Gate flags new unwrapped sensitive outputs
- [ ] Allowlist for non-sensitive interpolations (e.g., regions)

---

### T6.4 — CI gate: SKILL.md paths must exist

#### Files to edit
```
infra/scripts/quality/check-skill-doc-drift.sh (NEW)
.github/workflows/quality-gates.yml — add gate
```

#### Acceptance Criteria
- [ ] Gate scans .claude/skills/*/SKILL.md
- [ ] Asserts every `infra/scripts/X` path exists
- [ ] Asserts every `task X` invocation exists in Taskfile

---

### T6.5 — CI gate: `withEnvironmentLock` on mutating API routes

#### Files to edit
```
infra/scripts/quality/check-environment-lock.sh (NEW)
```

#### Deep Dives
- AST-grep api/internal/routes/*.go for HTTP methods POST/PUT/PATCH/DELETE
- Assert each handler invokes `withEnvironmentLock` or is in an `EXEMPT_ROUTES` allowlist

#### Acceptance Criteria
- [ ] Gate detects new mutating route without lock
- [ ] Existing routes pass

---

### T6.6 — CI gate: no `pg_advisory_lock(` (session-scoped) usage

#### Files to edit
```
infra/scripts/quality/check-advisory-lock-scope.sh (NEW)
```

#### Acceptance Criteria
- [ ] Gate fails on `pg_advisory_lock(` without `_xact_`
- [ ] Current code passes

---

### T6.7 — CI gate: exit codes 0-5 only in CLI

#### Files to edit
```
infra/scripts/quality/check-cli-exit-codes.sh (NEW)
```

#### Acceptance Criteria
- [ ] Gate AST-greps cli/src/ for `process.exit(N)`
- [ ] FAILS if N > 5
- [ ] Allowlist for test scaffolding

---

### T6.8 — CI gate: advisory mode without graduation date forbidden

#### Files to edit
```
infra/scripts/quality/check-advisory-mode-annotated.sh (NEW)
```

#### Acceptance Criteria
- [ ] Gate scans workflows for `continue-on-error: true`
- [ ] Asserts sibling comment has `graduation:` + `issue:` tags
- [ ] FAILS on un-annotated advisory

---

### T6.9 — Update .claude/rules/ files with explicit Enforcement section

#### Files to edit
```
.claude/rules/auto-docs.md — add "Enforced by: …" or "HONOR-SYSTEM"
.claude/rules/cli-output.md — same
.claude/rules/design-principles.md — same (HONOR-SYSTEM)
.claude/rules/pre-release-governance.md — same
.claude/rules/tdd-workflow.md — same (partial enforcement via test-first.py)
.claude/rules/loopzero-design.md — same
.claude/rules/workflow.md — same
```

#### Acceptance Criteria
- [ ] Every rule file has explicit Enforcement section
- [ ] Operators know which rules are auto vs honor

---

## Phase 7: Naming sweep + ADR

### T7.1 — Ratify naming convention ADR

#### Files to edit
```
docs/decisions/2026-05-21-naming-conventions-adr.md (NEW) — codify 9 rules
```

#### Deep Dives
The 9 rules:
1. Filenames don't carry historical identifiers
2. Scripts open with `set -euo pipefail`
3. State-writers support `--dry-run`
4. `pulumi.secret()` wrap mandatory for sensitive outputs
5. `values.schema.json` mandatory in new charts
6. Hardcoded credential defaults forbidden
7. `.env.example` linted against code
8. SKILL.md vs implementation drift gate
9. `Pulumi.<env>.yaml` must commit placeholder `secure:` for required secrets

#### Acceptance Criteria
- [ ] ADR committed
- [ ] PR template references it

---

### T7.2 — Rename phase6-dogfood-gate → dogfood-slo-gate

#### Files to edit
```
.github/workflows/phase6-dogfood-gate.yml → dogfood-slo-gate.yml
```

#### Acceptance Criteria
- [ ] No `phase6` in workflow filenames

---

### T7.3 — Deprecate provision-homolog.sh

#### Files to edit
```
infra/scripts/provision-homolog.sh — DELETE (per T2.4 orphan list)
```

---

### T7.4 — Rename file-corrections-item-*.sh

#### Files to edit
```
infra/scripts/file-corrections-item-I-upstream.sh → DELETE (already in T2.4 orphan list)
infra/scripts/verify-corrections-item-C.sh → KEEP but rename to verify-upstream-corrections.sh
```

---

### T7.5 — Pulumi `runtime2` → `runtimeCell2` (coordinated rename)

#### Files to edit
```
infra/pulumi/src/index.ts:125-127 — runtime2ClusterName → runtimeCell2ClusterName etc.
infra/pulumi/src/components/infra.ts:37, 170 — same
infra/pulumi-drill/src/index.ts:436-439 — same
infra/pulumi-platform/src/refs.ts — update consumers
infra/pulumi-infra/src/topology/* — update consumers
infra/helmfile/scripts/lib/pulumi-output.sh — update key reads
.github/workflows/* — update if any reference the output name
```

#### Deep Dives
- Coordinated rename across 5 projects. **EC-04: alias is MANDATORY in the SAME PR as the rename** (not "sugestão"): `export const runtime2ClusterName = runtimeCell2ClusterName;` must coexist with the new name for one release window. Removal of the alias happens in a separate, subsequent PR.
- Reason: `infra/helmfile/scripts/lib/pulumi-output.sh:77` suppresses stderr with `2>/dev/null` — missing keys silently return empty string instead of failing. Without alias, any consumer between TS rename and stack redeploy would silently read empty KUBECONFIG → kubectl errors deep downstream.

#### Acceptance Criteria
- [ ] No `runtime2` numeric-suffix exports (after the alias-removal PR)
- [ ] Alias coexists for one release window (per EC-04 — verifiable: `git log -p` shows both names committed together)
- [ ] All consumers updated
- [ ] LIVE drill stack still works
- [ ] `pulumi-output.sh:77` either fails on missing key OR alias guarantees no missing key during migration

---

## Phase 8: External tooling evaluation (optional)

### T8.1 — Evaluate helm-chartsnap for dual-chart drift

#### Tasks
1. Add as dev dep
2. Snapshot-test the 3 overlap charts
3. Compare snapshots in CI

### T8.2 — Evaluate helm-schema (dadav) for auto-gen

#### Tasks
1. Run on all 34 infra/helm/ charts
2. Compare generated to manually-authored (where exists)

### T8.3 — Evaluate @pulumi/command for teardown-validator

#### Tasks
1. POC replacing the bespoke doctl scan with Pulumi resource
2. Compare cost + reliability

### T8.4 — Evaluate pulumi-local (LocalStack)

#### Tasks
1. Stand up hermetic test for Pulumi S3 state backend

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | `cell2-soaks.yml` lies about scope | T0.7 | Rename to drill-prod-identical-soak.yml + update 6 cross-refs |
| 2 | `drill-test/SKILL.md` 6 broken paths | T0.1 | Fix paths + hermetic test |
| 3 | `dogfood/SKILL.md` GH_TOKEN false reference | T0.2 | Remove + replace |
| 4 | `to-plan/SKILL.md` `task teardown` non-existent | T0.3 | Replace with clean:all |
| 5 | drill DSN exports leak password | T0.4 | Wrap in pulumi.secret() |
| 6 | dr/preflight.sh D7' violation | T0.5 | Remove DRILL_DO_TOKEN refs |
| 7 | platform-health-7day-watch.yml dead | T0.6 | Delete |
| 8 | status-pr-gate.yml dead | T0.6 | Delete |
| 9 | status-refresh.yml dead | T0.6 | Delete |
| 10 | README.md:515 stale | T0.8 | Verified (fixed inline) |
| 11 | No single credentials SoT | T1.1 | Create credentials-required.md + hermetic test |
| 12 | Dual Gitea token generation | T1.2 | Eliminate; provision-api-tokens.sh canonical |
| 13 | 10× `loopzero-dev-password` literals | T1.3 | Replace with required template vars |
| 14 | GITEA_ADMIN_PASSWORD_DEFAULT hardcoded | T1.4 | Remove fallback |
| 15 | Pulumi.prod.yaml + staging.yaml zero secure | T1.5 | Commit placeholder secure: entries |
| 16 | ESO source-of-truth in Go code | T1.6 | Move to chart template (T2 governance) |
| 17 | ~110 scripts lack set -euo pipefail | T2.1 | Sweep + CI gate |
| 18 | 16 state-writers lack --dry-run | T2.2 | Add flag + CI gate |
| 19 | eval usage unaudited | T2.3 | Audit + sanitize |
| 20 | 19 orphan scripts | T2.4 | Delete |
| 21 | 3× health-check.sh ambiguous | T2.5 | Rename per purpose |
| 22 | 3× JWT-related scripts | T2.6 | Consolidate |
| 23 | 18 charts missing values.schema.json | T3.1 | Add schemas |
| 24 | audit-helm-drift advisory | T3.2 | Graduate to blocking |
| 25 | Conftest gate text vs code contradiction | T3.3 | Resolve |
| 26 | No diff-test for dual charts | T3.4 | Add hermetic diff |
| 27 | loopzero-cloud namespace divergence | T3.5 | Standardize |
| 28 | operator-split-soak.yml DEV cluster | T4.1 | Rewrite to drill |
| 29 | pkg-soak.yml DEV cluster | T4.1 | Rewrite to drill |
| 30 | adversarial-spotcheck writes deleted path | T4.2 | Repoint |
| 31 | cross-validation-weekly scans deleted paths | T4.2 | Repoint |
| 32 | 6 floating action versions | T4.3 | Pin to SHA |
| 33 | Many jobs missing timeout-minutes | T4.4 | Add timeouts |
| 34 | load-test-pr no approval gate | T4.5 | Add gate |
| 35 | Sunday 02:00 UTC triple-collision | T4.6 | Stagger |
| 36 | Advisory mode without graduation | T4.7 | Annotate or remove |
| 37 | task doctor 298 LOC bash god-task | T5.1 | Extract to Go |
| 38 | test.yml 103 tasks/1241 LOC | T5.2 | Split |
| 39 | B-patches embedded in Taskfile | T5.3 | Extract to scripts |
| 40 | Python heredocs in YAML | T5.4 | Extract |
| 41 | B-patch comment block stale | T5.5 | Replace with CHANGELOG ref |
| 42 | images:build is god-task | T5.6 | Split per image |
| 43 | clean/cleanup/teardown taxonomy | T5.7 | Canonicalize |
| 44 | 8 honor-system rules look enforced | T6.1-6.9 | Codify or annotate |
| 45 | Naming convention not ratified | T7.1 | New ADR |
| 46 | phase6-dogfood-gate stale | T7.2 | Rename |
| 47 | provision-homolog.sh dead concept | T7.3 | Delete (via T2.4) |
| 48 | corrections-item-*.sh external refs | T7.4 | Rename |
| 49 | Pulumi runtime2 numeric suffix | T7.5 | Rename runtimeCell2 |
| 50 | helm-chartsnap evaluation | T8.1 | POC |
| 51 | helm-schema evaluation | T8.2 | POC |
| 52 | @pulumi/command evaluation | T8.3 | POC |
| 53 | pulumi-local evaluation | T8.4 | POC |

**Coverage: 53/53 gaps covered (100%)**

## Global Definition of Done

- [ ] All 8 phases completed
- [ ] All tests passing (Go + TypeScript + bash hermetic)
- [ ] Zero lint warnings (golangci-lint + ESLint + shellcheck)
- [ ] Backward compatibility preserved OR justified pre-release break (T1.3 + T3.5 are intentional breaks documented in CHANGELOG)
- [ ] code-audit checks passing across all modified packages
- [ ] **`/cross-validation 2026-05-21-deep-review-remediation` PASSED** — zero CRITICO, zero GRAVE
- [ ] Plan-specific criteria:
  - [ ] Re-run deep-review audit weighted score ≥ 3.5/4.0
  - [ ] 0 `loopzero-dev-password` literals in rendered manifests
  - [ ] 0 Pulumi outputs with unwrapped sensitive interpolation
  - [ ] 34/34 infra/helm/ charts have values.schema.json
  - [ ] 0 broken SKILL.md paths (drift gate GREEN)
  - [ ] All 8 CI gates (T6.1-6.8) GREEN on develop
- [ ] **Evidence** (CONDITIONAL):
  - [ ] N/A — this plan does not directly affect maturity scores baseline (3.49), it cleans up ergonomic + safety debt
- [ ] **`/dogfood full` PASSED** on clean k3d (see Dogfood Gate below)

---

## Dogfood Gate (MANDATORY)

After ALL phases complete + all tests pass:

```bash
# 1. Fresh infrastructure
task clean:all && task setup && tilt up
task doctor  # verify health

# 2. Run full dogfood
/dogfood full
```

### Pass Criteria
- [ ] `/dogfood full` on clean infrastructure
- [ ] Pre-Flight Check ALL PASS
- [ ] Zero S0 / Zero S1 findings in plan scope
- [ ] Prova de Vida + Prova de Morte for every operation
- [ ] No regressions
- [ ] Observability GREEN
- [ ] Rule 3 clean (zero internal terms in CLI output)
- [ ] After T1.3 lands: NO standalone install succeeds without explicit password env vars (verification: `task clean:all && LOOPZERO_DB_PASSWORD= task setup` should FAIL fast with named missing var)

### On Failure
S0/S1 → fix and re-run on fresh infrastructure. S2-S4 documented but don't block.

---

## Cross-Validation Gate

Before `/dogfood`, run `/cross-validation 2026-05-21-deep-review-remediation`. Must return APROVADO (0 CRITICO, 0 GRAVE). Verifies every task, every DoD checkbox, every test name against actual code.
