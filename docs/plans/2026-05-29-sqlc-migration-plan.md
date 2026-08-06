---
type: plan
title: "Plan: sqlc Migration — Big-Bang Replacement of Hand-Rolled SQL Layer"
---

# Plan: sqlc Migration — Big-Bang Replacement of Hand-Rolled SQL Layer

> **Version 1.3** (sponsor mandate 2026-05-29 verbatim: *"AO FINAL DE TODO O PLANO DEVE RODAR O CLEANROON, DOGFOOD E CHAOS-TEST FUNCIIONAIS FULL. SEM ISSO NAO ACEITO A ENTREGA. QUANDO EXECUTAR CORRIGIR SEM WORAROUNDS POSSIVEIS PROBLEMAS QUE ENCONTRAR"* — V8/V9/V10 reframed from "operator HARD STOP" to **Ralph-executable** via multi-Bash-call sequence; only V11 (7-day wall-clock) remains operator-bound; no-workaround discipline binds the Final Phase: any failure during V8/V9/V10 must be fixed at root, no gate disable / spec skip / threshold relaxation; empirically grounded in MEET-001 closure precedent earlier this same session — audits `.claude/knowledge-base/audits/2026-05-29-dogfood-cleanroom-fresh-evidence.md` + `2026-05-29-chaos-test-full-cleanroom-fresh-evidence.md`. Memory `feedback_ralph_loop_engineering_completion_pattern.md` SELF-AMENDMENT 2026-05-29 documents the precedent reversal.) **Version 1.2** (sponsor-direct async amendment `.claude/knowledge-base/meetings/MEET-ASYNC-AMENDMENT-2026-05-29-001-sqlc-develop-direct.md` 2026-05-29: D8 replaced by D8' — work direct on develop instead of `feat/sqlc-migration` feature branch + squash-merge; per-commit incremental with develop staying GREEN after every substantive commit; ADR-3 §"Mecanismo" sub-amendment pending separate commit) — Carries v1.1 folds (5 MUST FIX from `/edge-case-plan` 2026-05-29: EC-1 sqlc CLI in task doctor; EC-2 sqlc parse pre-flight against existing DDL; EC-3 sqlc emits snake_case JSON tags per Rule 5; EC-4 render script trap signals; EC-5 T4.2 explicit "green cycle" attestation criteria) — Replace all hand-rolled SQL in `api/internal/db/` (44 stores, 26 manual `.Scan()` sites, 17 pgx callsites) with `sqlc` v1.31.1 codegen using `pgx/v5` driver and a CI-rendered `schema.gen.sql` from `golang-migrate` replay against ephemeral PostgreSQL 16. The migration eliminates the systemic `SELECT cols → Scan(&fields) → INSERT (cols) VALUES (...)` fragility class at **compile time** rather than test time, closing the bug family that produced the M07 Rekor URL persistence 3-layer bug (commit `b3375ed0`). Big-bang scope, no backward compatibility, `schema_mirror_test.go` deleted in two-step. Outcome: 44 hand-rolled stores → sqlc-generated typed Go funcs + thin Go wrappers for state-machine + transaction propagation; `withEnvironmentLock` + `pg_advisory_xact_lock` + state-machine `Transition()` discipline preserved by construction.

## Context

**What exists today:**
- `api/internal/db/` contains 44 `.go` files (mostly `*_store.go` pattern) — measured 2026-05-29.
- 17 direct pgx callsites + 26 manual `.Scan()` / `.QueryRow()` binding sites.
- 19 forward-only golang-migrate `.up.sql` migrations under `api/internal/db/migrations/`.
- Zero ORM / query-builder / codegen — fully hand-rolled.
- Defensive test `schema_mirror_test.go` (506 LOC, commit `838f5898` + extensions `9e47759f` + `f6e146a0`) catches schema↔struct↔SELECT↔INSERT↔Scan drift at **test time** for 3 M07 tables (releases, builds, release_app_attestations) — but not at compile time, and not for the other 41 stores.

**What is broken (concrete bug evidence, last 72h):**
- **M07 Rekor URL persistence 3-layer bug** (commit `b3375ed0`, 2026-05-28; memory `reference_m07_rekor_url_3layer_bug_2026_05_28`): migration 000020 + `Release` struct + `BuildReconcileComplete` writer were ALL correct, but `release_store.go` INSERT (line 137) + 13 SELECTs + `scanReleaseFields` ALL silently omitted the `image_attestation_rekor_url` column. Took 3 commits + new defensive test to close DoD item 9 of M07 plan v1.7.
- **T2.2 of MEET-2026-05-28-001 closure** (commit `838f5898` + `9e47759f` + `f6e146a0`): the team already invested defensive engineering to catch this class. Confirms the threat is real enough to spend gates on. sqlc moves the guarantee from detection (test time) to prevention (compile time).
- **44 stores × per-table defensive tests is unsustainable** — the audit-grind pattern doesn't scale; sqlc eliminates the bug class at root.

**Motivating governance:**
- MEET-2026-05-28-001 demonstrated that production-readiness now requires defensive consistency gates as a first-class deliverable (T2.2 schema-mirror test was a mandatory CR-9 closure).
- ADR [`docs/decisions/2026-05-29-sqlc-adoption.md`](../decisions/2026-05-29-sqlc-adoption.md) (and its 2 coupled ADRs) ratifies the architectural decision; this plan operationalizes it.

**What evidence justifies the big-bang scope:**
- Per pre-release-governance, incremental phased migration creates the exact "implemented but not wired" pattern documented in CLAUDE.md § Known Systemic Patterns.
- Operator-locked decision per MEET-2026-05-29-001: big-bang scope, no backward compatibility shims.
- 14-day branch lifetime cap (D3) creates a forced-completion constraint that prevents indefinite parallel-store divergence.

**Links:**
- ADR-1: [`docs/decisions/2026-05-29-sqlc-adoption.md`](../decisions/2026-05-29-sqlc-adoption.md)
- ADR-2: [`docs/decisions/2026-05-29-schema-gen-sql-render.md`](../decisions/2026-05-29-schema-gen-sql-render.md)
- ADR-3: [`docs/decisions/2026-05-29-sqlc-migration-rollback.md`](../decisions/2026-05-29-sqlc-migration-rollback.md)
- Meeting record: MEET-2026-05-29-001 (12 specialists, APPROVED-CONDITIONAL)
- Governance gate: `.claude/governance-gate.local.md` (expires 2026-06-12 23:59 UTC)
- Motivating bug audit: `.claude/knowledge-base/audits/2026-05-28-keyless-GREEN/07-build-pipeline.md`

## Objective

**Done means:** Every hand-rolled SQL statement in `api/internal/db/` is replaced by sqlc-generated typed Go code; `schema_mirror_test.go` is deleted; `task quality:gates` runs 11 gates (10 → 11) including 3 new gates that enforce sqlc invariants; full Final Phase (cleanroom + dogfood-full + chaos-test-full + drill iter 56 + prod-snapshot smoke) passes GREEN.

**Measurable goals:**
1. 44 `*_store.go` files DELETED from `api/internal/db/`.
2. `api/internal/db/queries/*.sql` directory created (1 file per domain object, hand-written SQL).
3. `api/internal/db/generated/` directory created (sqlc output, byte-identical on regen).
4. `api/internal/db/raw/` subpackage created (escape hatch, ≤5% of all queries, AST-gated against sensitive tables).
5. `api/internal/db/schema.gen.sql` committed (rendered from golang-migrate replay; diff-empty gate active in CI).
6. `pkg/quality/sqlcsync/` + `pkg/quality/sqlctxprop/` + `pkg/quality/tenantpredicate/` gates shipped and registered in `task quality:gates` (10 → 11+ gates).
7. `infra/scripts/ci/render-schema-gen-sql.sh` shipped + allowlisted in `.claude/rules/shell-scripts-allowlist.md`.
8. All 24 mandatory actions M-1..M-24 (from MEET-2026-05-29-001) closed with evidence.
9. All 11 validation phases V1..V11 (from MEET-2026-05-29-001) passed.
10. Engineering-side cross-validation `APROVADO`.
11. Final Phase: cleanroom-fresh dogfood `0 functional FAIL` + chaos-test `10/10 PASS` + drill iter 56 `GREEN` + prod-snapshot smoke `GREEN`.

## ADRs

> Plan-local ADRs (D1..D8). They cross-reference the 3 docs/decisions/ ADRs ratified by MEET-2026-05-29-001 but capture **plan-execution** decisions, not the meta architectural decisions.

### D1 — sqlc version pinning at v1.31.1
- **Decision:** Pin sqlc CLI to `v1.31.1` (April 2026 release) in CI render job AND in [`docs/operations/dev-loop.md`](../operations/dev-loop.md) for local developer regeneration.
- **Rationale:** Version drift between developer-local and CI = inconsistent generated code = schema.gen.sql diff false positives. Pinning forces synchronization.
- **Consequences:** Enables: reproducible codegen. Constrains: sqlc upgrades become explicit T2 changes (config update + verify generated diff is empty after upgrade).

### D2 — pgx/v5 driver canonical
- **Decision:** sqlc.yaml sets `sql_package: pgx/v5`; no `database/sql` fallback supported.
- **Rationale:** sqlc dropped pgx/v4 in v1.25.0 (Jan 2024); pgx/v5 actively bumped in v1.30.0 + v1.31.0. LoopZero's existing pgx pool is pgx/v5 already (per `api/internal/db/postgres.go`).
- **Consequences:** Enables: pgx-native types, no `sql.NullString` translation layer. Constrains: connection pool changes require sqlc-config awareness.

### D3 — sqlc emits tx-required form only
- **Decision:** sqlc.yaml sets `emit_methods_with_db_argument: true` (or equivalent); pool-bound auto-generated form is disabled.
- **Rationale:** Per MEET-2026-05-29-001 distributed-systems-engineer R-DS-1 — pool-bound form risks bypassing `withEnvironmentLock` because caller may invoke on a different connection than the lock holder. Tx-required form forces caller to thread `pgx.Tx` explicitly.
- **Consequences:** Enables: Rule 6 + Rule 7 invariant preservation by construction. Constrains: every caller must allocate or receive a `pgx.Tx`; no convenience pool-bound shortcuts.

### D4 — State-machine wrappers live in api/internal/db, not in handlers
- **Decision:** Every state-machine call site (`releaseMachine.SafeTransition`, `buildMachine.Transition`, etc.) lives in thin Go wrappers AT `api/internal/db/` level (NOT in `api/internal/routes/` or `api/internal/services/`).
- **Rationale:** Per MEET-2026-05-29-001 state-guardian R-STATE-1+R-STATE-3 — moving state-machine calls into handlers would lose the "single source of truth at store layer" architecture invariant.
- **Consequences:** Enables: state-machine discipline preserved. Constrains: each wrapper is a small Go file (~30-60 LOC) that calls `Queries.SelectX → machine.SafeTransition → Queries.UpdateX` inside one transaction.

### D5 — Raw-query escape hatch lives in api/internal/db/raw/
- **Decision:** Queries that sqlc cannot generate (LATERAL, recursive CTE, dynamic WHERE) live in a separate `api/internal/db/raw/` Go subpackage with a README declaring scope and a sensitive-table exclusion list enforced by AST gate.
- **Rationale:** Per MEET-2026-05-29-001 Conflict 3 resolution — Option (a) separate subpackage chosen unanimous.
- **Consequences:** Enables: testable boundary; security gate; ≤5% of queries. Constrains: raw queries cannot touch `secret`, `audit_log`, `bootstrap`, `tenant_deletion`, `cell` tables (AST gate enforces).

### D6 — schema.gen.sql is the source-of-truth for sqlc; migrations are NOT consumed directly
- **Decision:** sqlc.yaml `schema: "api/internal/db/schema.gen.sql"` (single file); NOT `schema: "api/internal/db/migrations/"`.
- **Rationale:** Per ADR-2 + MEET-2026-05-29-001 pipeline-reviewer R-PIPE-1 — sqlc's parser may misinterpret PG-specific DDL (partial indexes, custom types, trigger functions); replay against real PG gives ground truth.
- **Consequences:** Enables: schema render via `pg_dump --schema-only` (proven, mature, deterministic). Constrains: developers must `task db:render-schema` after migration changes; CI gate enforces.

### D7 — Two-step DELETE of schema_mirror_test.go
- **Decision:** schema_mirror_test.go is DELETED in a follow-up commit AFTER `pkg/quality/sqlcsync/` ships AND passes one full CI cycle GREEN.
- **Rationale:** Per MEET-2026-05-29-001 Conflict 2 resolution — preserves bug-class coverage during transition; SRE veto-backed.
- **Consequences:** Enables: zero-coverage-gap migration. Constrains: 2 separate commits required (cannot squash these two), with explicit sequence enforcement.

### D8' — Work direct on develop (supersedes D8 per async amendment 2026-05-29)
- **Decision:** Work direct on `develop` branch; per-commit incremental landings; **no feature branch, no squash-merge event**. Develop must stay GREEN (CI gates pass) after every substantive commit. (Supersedes prior D8 which mandated `feat/sqlc-migration` off develop + squash-merge + ≤14-day lifetime.)
- **Rationale:** Per sponsor-direct async amendment `MEET-ASYNC-AMENDMENT-2026-05-29-001-sqlc-develop-direct.md` 2026-05-29. Sponsor (Paulo) directive: "Voce deve trabalhar na develop atualizada". Reduces multi-day RED window risk (R-SRE-1) — develop staying GREEN per commit forces smaller reviewable units. Aligns with parallel work threads already on develop (dashboard backend integration v1.1).
- **Consequences:** Enables: immediate visibility to all develop consumers; no rebase debt against parallel threads; smaller per-commit blast radius; CI green discipline gate. Constrains: ADR-3 §"Mecanismo" rollback playbook requires sub-amendment from single-SHA atomic revert to per-commit-range revert (`git log --grep="sqlc" --oneline` to identify range; sequential `git revert <first>..<last>`). M-15 daily diary anchored to **progress %** instead of branch lifetime. CR-CONSENSUS-1 burnout mitigation still active via M-15.
- **Superseded D8 archived in:** `MEET-ASYNC-AMENDMENT-2026-05-29-001-sqlc-develop-direct.md` § "Decision Synthesized" table comparison.

## Dependency Graph

```
Phase 0 (Foundation) ──▶ Phase 1 (Pilot 5 stores) ──▶ Phase 2 (39 stores in 4 batches) ──▶ Phase 4 (raw/ + DELETE)
                              │                                     │                            │
                              ▼                                     │                            │
                        Phase 3 (Operator audit + rewire)  ◀────────┘                            │
                              │                                                                  │
                              └──────────────────────────────────────┬───────────────────────────┘
                                                                     ▼
                                                          Final Phase (V1-V11 + Dogfood QA)
```

**Sequencing rules:**
- Phase 0 BLOCKS all subsequent phases (tooling + gates required).
- Phase 1 BLOCKS Phase 2 (pilot validates patterns).
- Phase 2 BLOCKS Phase 4 (full conversion must complete before raw/ escape hatch is exercised).
- Phase 3 (operator-binary rewire) can run in PARALLEL with Phase 2 once import-graph audit (T3.1) finishes.
- Phase 4 BLOCKS Final Phase (schema_mirror DELETE requires sqlcsync gate green per D7).
- Final Phase requires ALL of V8 + V9 + V10 (Ralph-executable per v1.3 sponsor mandate 2026-05-29) + V11 (operator wall-clock).

---

## Phase 0: Foundation — Tooling + Gates + Render Pipeline

**Objective:** Land all the infrastructure that subsequent phases depend on: sqlc.yaml, render pipeline, 3 quality gates, taskfile wiring, tenant-table list, reshape matrix. No store conversion in this phase.

### T0.1 — Land sqlc.yaml + queries/ + generated/ directories

#### Objective
Create the sqlc configuration that locks `pgx/v5`, tx-required form, and `schema.gen.sql` as source-of-truth.

#### Evidence
ADR-1 §4 mandates sqlc adoption; ADR-1 §6 D2 + D3 + D6 lock the config shape. Zero sqlc files exist in repo (verified by Rule 12 grep precheck at MEET-2026-05-29-001).

#### Files to edit
```
api/internal/db/sqlc.yaml — (NEW) sqlc v2 config: schema=schema.gen.sql, queries=queries/, gen.go.out=generated/, sql_package=pgx/v5, emit_methods_with_db_argument=true
api/internal/db/queries/.gitkeep — (NEW) directory placeholder until queries land in Phase 1+
api/internal/db/generated/.gitkeep — (NEW) directory placeholder
docs/operations/dev-loop.md — (EDIT) add "Local sqlc workflow" section: install sqlc v1.31.1, run task db:generate after queries.sql changes
```

#### Deep file dependency analysis
- `sqlc.yaml` is the codegen contract; its `gen.go.package` field determines the import path for generated code (`api/internal/db/generated`). Every Phase 1+ store conversion imports from this package.
- [`dev-loop.md`](../operations/dev-loop.md) is the canonical operator runbook. Adding sqlc to it makes the local workflow self-documenting.
- The `.gitkeep` placeholders allow PRs to land empty directories under version control.

#### Deep Dives
- **sqlc.yaml v2 schema:** `version: "2"`, `sql.[0].engine: postgresql`, `sql.[0].schema: api/internal/db/schema.gen.sql`, `sql.[0].queries: api/internal/db/queries/`, `sql.[0].gen.go.out: api/internal/db/generated/`, `sql.[0].gen.go.package: generated`, `sql.[0].gen.go.sql_package: pgx/v5`, `sql.[0].gen.go.emit_methods_with_db_argument: true`, `sql.[0].gen.go.emit_pointers_for_null_types: true`, `sql.[0].gen.go.emit_json_tags: true` (EC-3 Rule 5 stability), `sql.[0].gen.go.json_tags_case_style: snake` (EC-3 preserves current snake_case CLI/API contract).
- **Invariants:** sqlc.yaml MUST NOT enable `emit_db_tags`, `emit_json_tags` (LoopZero stores already define these via struct tags in wrappers); MUST set `sql_package: pgx/v5` (not database/sql); MUST set `emit_methods_with_db_argument: true` per D3.
- **Edge case:** If a developer regenerates locally with a different sqlc version, schema.gen.sql may not match. The sqlcsync gate (T0.3) catches this.

#### Tasks
1. Create `api/internal/db/sqlc.yaml` with the v2 config locked per ADR-1 D2 + D3 + D6 + EC-3 (emit_json_tags + snake_case).
2. Create `api/internal/db/queries/.gitkeep` + `api/internal/db/generated/.gitkeep`.
3. Add "Local sqlc workflow" section to [`docs/operations/dev-loop.md`](../operations/dev-loop.md) with install + regenerate commands.
4. Run `sqlc compile` (Phase 0 has no queries, so this is a syntax validation only).
5. Verify `sqlc.yaml` matches the documented shape via JSON schema check (`sqlc compile --no-remote` exits 0).
6. **EC-1:** Add sqlc CLI check to `taskfiles/dev.yml` § "=== Required Tools ===" — `cmd: command -v sqlc >/dev/null && [ "$(sqlc version)" = "v1.31.1" ] && echo "[PASS] sqlc v1.31.1" || echo "[FAIL] sqlc — install v1.31.1 from github.com/sqlc-dev/sqlc/releases"`. `task doctor` must fail (FAIL count >0) when sqlc absent or wrong version.

#### TDD
```
RED:     test_sqlc_yaml_locks_pgx_v5() — assert `sqlc.yaml` contains `sql_package: pgx/v5`
RED:     test_sqlc_yaml_emits_db_argument() — assert `emit_methods_with_db_argument: true`
RED:     test_sqlc_yaml_schema_is_rendered_file() — assert `schema:` points to `schema.gen.sql`, NOT `migrations/`
RED:     test_sqlc_yaml_disables_pool_bound() — assert no `emit_pool_bound_methods` or equivalent
RED:     test_sqlc_yaml_emits_snake_case_json_tags() — EC-3 — assert `emit_json_tags: true` AND `json_tags_case_style: snake` (Rule 5 contract preservation)
RED:     test_task_doctor_fails_without_sqlc() — EC-1 — simulate sqlc absent; `task doctor` exit code reports FAIL on sqlc row
RED:     test_task_doctor_fails_with_wrong_sqlc_version() — EC-1 — simulate sqlc v1.30.0 (downrev); `task doctor` reports FAIL
GREEN:   Write sqlc.yaml + taskfiles/dev.yml entry that passes all 7 assertions
REFACTOR: None expected
VERIFY:  cd api/internal/db && sqlc compile && go test ./internal/db/sqlc_config_test.go && task doctor 2>&1 | grep -E "PASS.*sqlc"
```

#### Acceptance Criteria
- [ ] `api/internal/db/sqlc.yaml` exists and parses without error
- [ ] `sqlc compile` exits 0 (no queries yet, but config is valid)
- [ ] [`docs/operations/dev-loop.md`](../operations/dev-loop.md) has the sqlc workflow section
- [ ] Pass: `/code-audit` size check (sqlc.yaml ≤ 100 lines)
- [ ] Pass: `/code-audit` lint check (zero warnings)
- [ ] sqlc.yaml v2 schema compliance verified
- [ ] **EC-3:** sqlc-emitted struct has `json:"snake_case"` tag for every field (verify via `sqlc generate` on a dummy query → grep `json:"` in output)
- [ ] **EC-1:** `task doctor` includes sqlc v1.31.1 in Required Tools; PASS row when installed correctly; FAIL row when absent or version mismatch

#### DoD
- [ ] All tasks 1-5 completed
- [ ] 4 unit tests GREEN
- [ ] `sqlc compile` exits 0
- [ ] `task lint` passes
- [ ] Documentation entry in dev-loop.md complete

---

### T0.2 — Land render-schema-gen-sql.sh + initial schema.gen.sql

#### Objective
Ship the CI render script that produces `schema.gen.sql` from golang-migrate replay against ephemeral PG 16, with the 5 reproducibility requirements from ADR-2.

#### Evidence
ADR-2 §3 + §4 lock the render pipeline shape; M-17 mandates the script be allowlisted in `.claude/rules/shell-scripts-allowlist.md` before commit; M-18/M-19/M-20 mandate version pinning.

#### Files to edit
```
infra/scripts/ci/render-schema-gen-sql.sh — (NEW) render script: spin ephemeral PG 16, run golang-migrate up, pg_dump --no-owner --no-privileges --schema-only, post-process, write to api/internal/db/schema.gen.sql
api/internal/db/schema.gen.sql — (NEW, rendered) initial schema dump from 19 existing migrations
taskfiles/db.yml — (EDIT) add task `db:render-schema` that invokes the render script locally (using `docker run postgres:16`)
.claude/rules/shell-scripts-allowlist.md — (EDIT) add entry for render-schema-gen-sql.sh per D3 of plan 2026-05-22-shell-script-strategic
docs/operations/dev-loop.md — (EDIT) add "Re-rendering schema.gen.sql" section
```

#### Deep file dependency analysis
- `render-schema-gen-sql.sh` is the load-bearing piece of the new CI pipeline (per pipeline-reviewer R-PIPE-1). It MUST be deterministic across Linux/Darwin runners.
- `schema.gen.sql` is the SoT for sqlc (per D6). Its diff being empty is the signal that migrations + render are self-consistent.
- `taskfiles/db.yml` exposes the render to developers via `task db:render-schema`.
- The allowlist entry is mandatory per `.claude/rules/shell-scripts-allowlist.md` (T0.1 enforcement: new .sh files forbidden by default).

#### Deep Dives
- **Render algorithm:**
  0. **EC-4 trap (first line of script):** `set -euo pipefail; PG_CID=""; trap 'if [ -n "$PG_CID" ]; then docker rm -f "$PG_CID" 2>/dev/null || true; fi' EXIT INT TERM` (guarantees cleanup on abort/SIGINT)
  1. `PG_CID=$(docker run --rm -d --publish 0:5432 -e POSTGRES_PASSWORD=test -e POSTGRES_DB=loopzero postgres:16)` (pin tag, NOT `:latest`; capture container ID for trap; EC-17 publish 0:5432 = random host port avoids local k3d PG conflict at 15432)
  2. `HOST_PORT=$(docker port "$PG_CID" 5432 | cut -d: -f2)` — resolve random host port
  3. Wait for PG ready via `pg_isready -h localhost -p "$HOST_PORT"` with 30s budget
  4. `golang-migrate -path api/internal/db/migrations -database "postgres://postgres:test@localhost:$HOST_PORT/loopzero?sslmode=disable" up` (pinned golang-migrate version)
  5. `pg_dump --no-owner --no-privileges --schema-only --no-comments "postgres://postgres:test@localhost:$HOST_PORT/loopzero" > /tmp/raw-schema.sql`
  6. Post-process: strip `ALTER ... OWNER`, normalize whitespace, sort CREATE statements by table name, strip serial generation comments
  7. Write to `api/internal/db/schema.gen.sql`
  8. **EC-2 sqlc parse pre-flight:** run `sqlc compile` against the rendered schema.gen.sql + 1 dummy query per existing table (`SELECT 1 FROM <table>`); if any DDL pattern (TRIGGER from 000020, partial index, recursive CTE) errors, ABORT with explicit message + add gap to ADR-1 §"Known sqlc limitations". This catches sqlc/DDL compatibility BEFORE Phase 1 commits.
  9. `trap` (step 0) handles container teardown on normal exit + signal abort.
- **Invariants:** byte-identical output across runs; PG version pinned; golang-migrate version pinned; post-process is deterministic (sort + canonical formatting).
- **Edge case:** if a migration uses `IF NOT EXISTS` and a prior render left state, replay must be ephemeral (fresh container each run). The `docker run --rm` flag ensures this.
- **Security (per platform-security-engineer condition 5 M-5):** the PG role used for migration is NOT superuser (use `--username loopzero` not `postgres`). Output strip removes any `OWNER TO postgres` clauses.

#### Tasks
1. Add entry to `.claude/rules/shell-scripts-allowlist.md` for render-schema-gen-sql.sh (allowlist BEFORE commit per LoopZero gate).
2. Create `infra/scripts/ci/render-schema-gen-sql.sh` per the render algorithm above (includes EC-4 trap + EC-17 random-port + EC-2 sqlc parse pre-flight at step 8).
3. Add `task db:render-schema` to `taskfiles/db.yml`.
4. Run `task db:render-schema` locally; verify output is non-empty + deterministic (run 3x, assert byte-identical).
5. Commit the initial `api/internal/db/schema.gen.sql` to repo.
6. Add [`docs/operations/dev-loop.md`](../operations/dev-loop.md) section explaining when to re-render.
7. **EC-2:** Execute `sqlc compile` against rendered schema.gen.sql + dummy queries; if any of 19 existing migrations' DDL fails to parse (TRIGGER, partial index, recursive CTE, custom types), document gap in ADR-1 §"Known sqlc limitations" with workaround (e.g., raw/ escape hatch for that table family). If 000020 trigger fails, mark as ADR-1 KSL-1 known limitation; raw/ subpackage will handle update-with-trigger paths.

#### TDD
```
RED:     test_render_script_is_in_allowlist() — assert `.claude/rules/shell-scripts-allowlist.md` mentions render-schema-gen-sql.sh
RED:     test_render_pins_pg16() — assert script contains `postgres:16` (not `:latest`)
RED:     test_render_pins_golang_migrate() — assert script pins golang-migrate version
RED:     test_render_strips_owner_clauses() — assert pg_dump invocation uses --no-owner
RED:     test_render_strips_privileges() — assert pg_dump invocation uses --no-privileges
RED:     test_render_uses_non_superuser_role() — assert script creates a non-superuser PG role for migration
RED:     test_render_deterministic_output() — run render 3x, assert byte-identical output
RED:     test_render_traps_signals_for_cleanup() — EC-4 — assert script has `trap ... EXIT INT TERM` in first 10 lines; simulate SIGINT mid-run via `timeout -s INT 2`, verify `docker ps -f label=...` returns zero orphan containers
RED:     test_render_uses_random_host_port() — EC-17 — assert script uses `--publish 0:5432` (not hardcoded `5432:5432` or `15432:5432`)
RED:     test_sqlc_parses_all_existing_migrations_ddl() — EC-2 — given rendered schema.gen.sql + dummy `SELECT 1 FROM <table>` per existing table, `sqlc compile` exits 0; any failure documented in ADR-1 §"Known sqlc limitations" with table + DDL pattern + workaround
GREEN:   Write render script + initial schema.gen.sql that passes all 10 assertions
REFACTOR: None expected
VERIFY:  task db:render-schema && git diff --exit-code api/internal/db/schema.gen.sql && sqlc compile -f api/internal/db/sqlc.yaml
```

#### Acceptance Criteria
- [ ] `infra/scripts/ci/render-schema-gen-sql.sh` exists and is executable
- [ ] `.claude/rules/shell-scripts-allowlist.md` lists render-schema-gen-sql.sh with justificativa
- [ ] `task db:render-schema` exits 0 and produces non-empty `schema.gen.sql`
- [ ] 3 consecutive runs produce byte-identical schema.gen.sql
- [ ] schema.gen.sql contains zero `ALTER ... OWNER` statements
- [ ] schema.gen.sql contains all 19 migrations applied (verify by grep table names from each .up.sql)
- [ ] Pass: `/code-audit` size check (render script ≤ 200 lines per .claude/rules/shell-scripts-allowlist.md guidance)
- [ ] Pass: shellcheck on render script
- [ ] **EC-4:** Render script has `trap` cleanup at line ≤10; SIGINT mid-render leaves zero orphan PG containers (`docker ps -f ancestor=postgres:16` returns empty)
- [ ] **EC-17:** Render script uses `--publish 0:5432` (random host port); zero clash with local k3d PG at 15432
- [ ] **EC-2:** `sqlc compile` against schema.gen.sql succeeds for every existing table; ANY DDL pattern that fails (TRIGGER, partial index, recursive CTE) documented in ADR-1 §"Known sqlc limitations" with workaround

#### DoD
- [ ] Tasks 1-6 completed
- [ ] 7 RED tests GREEN
- [ ] Render script committed + allowlisted
- [ ] schema.gen.sql committed (initial render)
- [ ] Dev-loop docs updated
- [ ] `task lint` passes including shellcheck

---

### T0.3 — Land pkg/quality/sqlcsync/ gate

#### Objective
Ship the gate that fails CI when sqlc-generated code is not in sync with `schema.gen.sql` — successor to the deleted `schema_mirror_test.go`.

#### Evidence
Per MEET-2026-05-29-001 verification-architect condition 2 + principal-architect condition 3 + sre-engineer condition 5 (two-step delete sequence requires sqlcsync to ship first). Per M-10 + M-16.

#### Files to edit
```
pkg/quality/sqlcsync/check.go — (NEW) gate: regenerate sqlc output in temp dir, diff against committed generated/, fail on non-empty diff
pkg/quality/sqlcsync/check_test.go — (NEW) unit tests for gate logic
cmd/loopzero-ops/main.go — (EDIT) register `quality sqlc-sync` subcommand
taskfiles/quality.yml — (EDIT) add `sqlc-sync` gate to `task quality:gates` (10 → 11)
```

#### Deep file dependency analysis
- `pkg/quality/` is the canonical home for pure-logic gates per `.claude/rules/shell-scripts-allowlist.md` Anti-Pattern #8 (no new bash for pure-logic).
- `cmd/loopzero-ops/main.go` is the CLI dispatcher for all `quality:*` gates.
- `taskfiles/quality.yml` registers the gate in `task quality:gates` (the canonical CI gate runner).

#### Deep Dives
- **Gate algorithm:**
  1. Read `api/internal/db/sqlc.yaml`
  2. Locate `schema.gen.sql` + `queries/` + `generated/`
  3. Spawn `sqlc generate` into a tempdir
  4. Walk the tempdir output + diff each .go file against committed `generated/` counterpart
  5. Non-empty diff → exit 1 with actionable message: "Run `task db:generate && git add api/internal/db/generated && git commit`"
- **Invariants:** gate is hermetic (no live DB needed; sqlc generate is offline); gate is deterministic (same inputs = same outputs); gate execution time ≤ 30s.
- **Edge case:** sqlc CLI not installed locally → gate prints clear install instruction (sqlc v1.31.1 binary download URL).

#### Tasks
1. Create `pkg/quality/sqlcsync/check.go` implementing the gate algorithm.
2. Write unit tests for the gate logic (mocked sqlc generate output).
3. Register `quality sqlc-sync` subcommand in `cmd/loopzero-ops/main.go`.
4. Add gate to `taskfiles/quality.yml` `quality:gates` task list.
5. Run `task quality:gates` locally; verify the new gate executes and exits 0 (Phase 0 has no generated code yet, so trivially passes).

#### TDD
```
RED:     test_sqlcsync_detects_drift_via_mocked_diff() — given a mocked diff, assert exit 1
RED:     test_sqlcsync_passes_when_no_drift() — given empty diff, assert exit 0
RED:     test_sqlcsync_actionable_error_message() — assert error message contains `task db:generate`
RED:     test_sqlcsync_registered_in_quality_gates() — grep taskfiles/quality.yml for sqlc-sync entry
GREEN:   Implement check.go + main.go subcommand + taskfile entry
REFACTOR: extract diff comparison into pkg/quality/internal/godiff if reused elsewhere
VERIFY:  go test ./pkg/quality/sqlcsync/ && task quality:gates 2>&1 | grep sqlc-sync
```

#### Acceptance Criteria
- [ ] `pkg/quality/sqlcsync/check.go` exists, ≤ 200 LOC
- [ ] 4 unit tests GREEN
- [ ] `task quality:gates` runs and includes sqlc-sync
- [ ] Gate execution time ≤ 30s in CI
- [ ] Actionable error message tested
- [ ] Pass: `/code-audit` complexity check (cyclomatic ≤ 10 per function)

#### DoD
- [ ] Tasks 1-5 completed
- [ ] 4 RED tests GREEN
- [ ] Gate registered in `task quality:gates`
- [ ] `task lint` passes
- [ ] Gate exit 0 on Phase 0 baseline (no generated code yet)

---

### T0.4 — Land pkg/quality/sqlctxprop/ gate

#### Objective
Ship the gate that verifies every sqlc-generated call inside `withEnvironmentLock` uses `WithTx(tx)` form — preserves Rule 6 + Rule 7 invariants.

#### Evidence
Per MEET-2026-05-29-001 distributed-systems-engineer R-DS-1 + condition 2. Per M-11 + M-12.

#### Files to edit
```
pkg/quality/sqlctxprop/check.go — (NEW) AST gate: walk api/internal/routes/ + api/internal/services/, find withEnvironmentLock blocks, verify sqlc calls inside use tx variant
pkg/quality/sqlctxprop/check_test.go — (NEW) unit tests with fixture .go files
cmd/loopzero-ops/main.go — (EDIT) register `quality sqlctxprop` subcommand
taskfiles/quality.yml — (EDIT) add `sqlctxprop` gate
```

#### Deep file dependency analysis
- AST-level analysis (using `go/parser` + `go/ast`) is more rigorous than regex; this gate inspects actual function call graphs.
- The gate is hermetic; no live DB needed.
- Phase 0 baseline: no sqlc calls exist yet, so the gate trivially passes.

#### Deep Dives
- **Gate algorithm:**
  1. Parse all `.go` files under `api/internal/routes/` + `api/internal/services/` via `go/parser`
  2. Find `withEnvironmentLock` invocations
  3. For each, walk the closure body via AST
  4. Find any `generated.Queries{...}.<method>(...)` calls inside
  5. Verify each such call has `.WithTx(tx)` adapter applied (not the pool-bound form)
  6. Non-compliant calls → exit 1 with file:line of violation
- **Invariants:** gate is hermetic; gate execution time ≤ 60s for entire api/ tree.
- **Edge case:** sqlc-generated `Queries` type has both pool-bound and tx-bound forms; tx-bound is the only acceptable variant inside `withEnvironmentLock`.

#### Tasks
1. Create `pkg/quality/sqlctxprop/check.go` implementing the AST gate.
2. Write unit tests with fixture `.go` files (pass + fail cases).
3. Register subcommand in `cmd/loopzero-ops/main.go`.
4. Add gate to `taskfiles/quality.yml`.
5. Run baseline: verify gate exits 0 on Phase 0 (no sqlc calls yet).

#### TDD
```
RED:     test_sqlctxprop_detects_pool_bound_inside_lock() — given fixture with pool-bound call inside withEnvironmentLock, assert exit 1
RED:     test_sqlctxprop_passes_tx_bound_inside_lock() — given fixture with WithTx(tx) call, assert exit 0
RED:     test_sqlctxprop_ignores_calls_outside_lock() — pool-bound OK outside lock context
RED:     test_sqlctxprop_actionable_violation_message() — error message includes file:line + suggested fix
GREEN:   Implement AST walker + checks
REFACTOR: extract AST helpers into internal package if reused
VERIFY:  go test ./pkg/quality/sqlctxprop/ && task quality:gates 2>&1 | grep sqlctxprop
```

#### Acceptance Criteria
- [ ] `pkg/quality/sqlctxprop/check.go` exists, ≤ 300 LOC
- [ ] 4 unit tests GREEN
- [ ] `task quality:gates` includes sqlctxprop
- [ ] Gate execution time ≤ 60s
- [ ] Pass: `/code-audit` complexity check

#### DoD
- [ ] Tasks 1-5 completed
- [ ] 4 RED tests GREEN
- [ ] Gate registered and exits 0 on Phase 0 baseline
- [ ] `task lint` passes

---

### T0.5 — Land pkg/quality/tenantpredicate/ gate

#### Objective
Ship the AST gate that verifies every sqlc-generated query touching a tenant-owned table contains `tenant_id = $N` predicate — preserves multi-tenant isolation.

#### Evidence
Per MEET-2026-05-29-001 platform-security-engineer condition 1 (veto-backed) + tenant-guardian R-TENANT-1. Per M-1 + M-2.

#### Files to edit
```
pkg/quality/tenantpredicate/check.go — (NEW) gate: parse queries.sql, identify tenant-table queries, verify tenant_id predicate
pkg/quality/tenantpredicate/check_test.go — (NEW) unit tests
pkg/quality/tenantpredicate/tenant_tables.go — (NEW) committed list of tenant-owned tables (reviewed by tenant-guardian + security)
cmd/loopzero-ops/main.go — (EDIT) register `quality tenant-predicate` subcommand
taskfiles/quality.yml — (EDIT) add `tenant-predicate` gate
```

#### Deep file dependency analysis
- `tenant_tables.go` is a security-critical static list reviewed by tenant-guardian + platform-security-engineer; changes require re-review.
- The gate operates on `queries.sql` (post-Phase 1+), so Phase 0 baseline trivially passes.

#### Deep Dives
- **Tenant-owned tables (initial list, subject to security review):**
  - `tenants`, `projects`, `environments`, `builds`, `releases`, `secrets`, `audit_log`, `cells`, `scaling_configs`, `custom_domains`, `release_app_attestations`, `environment_targets`, `idempotency_keys`, `memberships`, `repositories`, `preview_environments`, `teams`, `active_teams`, `tenant_deletion`
- **Gate algorithm:**
  1. Parse all `.sql` files under `api/internal/db/queries/`
  2. For each SELECT/UPDATE/DELETE on a tenant-owned table, verify presence of `WHERE ... tenant_id = ...` clause (or equivalent JOIN-based filter)
  3. INSERT statements on tenant-owned tables must specify `tenant_id` as a column
  4. Violations → exit 1 with query name + table + missing predicate
- **Invariants:** gate is hermetic; tenant_tables.go is the SoT for what tables require the predicate.
- **Edge case:** queries operating on the `tenants` table itself may not have `tenant_id = $N` (the row IS the tenant). Allowlist via comment annotation `-- tenantpredicate:exempt` per query.

#### Tasks
1. Create `pkg/quality/tenantpredicate/tenant_tables.go` with the initial list; have security + tenant-guardian sign off.
2. Create `pkg/quality/tenantpredicate/check.go` implementing the gate.
3. Write unit tests with fixture .sql files.
4. Register subcommand + taskfile entry.
5. Run baseline: gate exits 0 on Phase 0 (no queries yet).

#### TDD
```
RED:     test_tenant_predicate_detects_missing_where_clause() — fixture query SELECT * FROM projects without WHERE tenant_id, assert exit 1
RED:     test_tenant_predicate_passes_when_where_clause_present() — query with WHERE tenant_id = $1, assert exit 0
RED:     test_tenant_predicate_allows_tenants_table_itself() — query on `tenants` table allowlisted
RED:     test_tenant_predicate_allows_explicit_exemption_comment() — query with `-- tenantpredicate:exempt`, assert exit 0
RED:     test_tenant_predicate_insert_requires_tenant_id_column() — INSERT into projects without tenant_id col, assert exit 1
GREEN:   Implement check.go
REFACTOR: extract SQL parser helpers
VERIFY:  go test ./pkg/quality/tenantpredicate/ && task quality:gates 2>&1 | grep tenant-predicate
```

#### Acceptance Criteria
- [ ] `tenant_tables.go` reviewed + signed off by platform-security-engineer + tenant-guardian
- [ ] `check.go` ≤ 300 LOC
- [ ] 5 unit tests GREEN
- [ ] Gate registered in `task quality:gates`
- [ ] Gate execution time ≤ 30s
- [ ] Pass: `/code-audit` complexity check

#### DoD
- [ ] Tasks 1-5 completed
- [ ] 5 RED tests GREEN
- [ ] tenant_tables.go committed with sign-off in commit message
- [ ] Gate exit 0 on Phase 0 baseline
- [ ] `task lint` passes

---

### T0.6 — Reshape matrix landed in ADR-1 Appendix A

#### Objective
Classify all 10+ `*_store_test.go` files into KEEP-INVARIANT, RESHAPE-INTEGRATION, DELETE-SCHEMA-MIRROR-DUPLICATE categories — owner reference for Phase 1+ test work.

#### Evidence
Per MEET-2026-05-29-001 verification-architect condition 1 (M-14). ADR-1 Appendix A has the skeleton; this task fills it.

#### Files to edit
```
docs/decisions/2026-05-29-sqlc-adoption.md — (EDIT) Appendix A: classify each *_store_test.go
docs/plans/2026-05-29-sqlc-migration-plan.md — (EDIT, self) update Coverage Matrix when classification is final
```

#### Deep file dependency analysis
- ADR-1 is the source-of-truth for the reshape decision; this task populates it.
- Test reshape decisions cascade into Phase 1+ test rewrite scope.

#### Deep Dives
- **Classification rubric:**
  - **KEEP-INVARIANT:** Tests assert observable behaviors not derivable from schema (concurrent-deploy 1×202+1×409, idempotency replay, lock release on crash, state-machine panic). MUST survive in equivalent form against sqlc-backed code.
  - **RESHAPE-INTEGRATION:** Tests use real PG via `LOOPZERO_E2E_DATABASE_URL` and exercise multi-statement transactions. Reshape to use new sqlc wrappers; keep assertions.
  - **DELETE-SCHEMA-MIRROR-DUPLICATE:** Tests assert "field X exists in struct AND column" — exactly what sqlc proves at compile time. DELETE after sqlcsync gate ships (D7).
- **Process:** classify by reading each test file (10+ files) and categorizing each test function (not the whole file — files mix categories).

#### Tasks
1. Read every `*_store_test.go` file under `api/internal/db/`.
2. Categorize each `Test*` function into KEEP / RESHAPE / DELETE.
3. Populate ADR-1 Appendix A with the full matrix table.
4. Update Coverage Matrix in this plan with the categorization.

#### TDD
```
RED:     test_reshape_matrix_has_entry_per_test_file() — grep ADR-1 Appendix A for every *_store_test.go filename
RED:     test_reshape_matrix_categories_are_valid() — each entry is exactly one of KEEP / RESHAPE / DELETE
GREEN:   Fill matrix
REFACTOR: None expected
VERIFY:  grep -c "KEEP\|RESHAPE\|DELETE" docs/decisions/2026-05-29-sqlc-adoption.md should match test file count
```

#### Acceptance Criteria
- [ ] Every `*_store_test.go` file appears in the matrix
- [ ] Every test function classified
- [ ] No "TBD" entries
- [ ] Coverage Matrix in this plan updated

#### DoD
- [ ] Tasks 1-4 completed
- [ ] 2 RED tests GREEN
- [ ] ADR-1 Appendix A complete
- [ ] Coverage Matrix updated

---

### T0.7 — Operator-binary import-graph audit

#### Objective
Catalog which of the 4 operator binaries (tenant/app/preview/platform) imports which symbols from `api/internal/db/`, so Phase 3 rewire is scoped correctly.

#### Evidence
Per MEET-2026-05-29-001 contract-api-operators R-CONT-1 + condition 1 (M-22).

#### Files to edit
```
docs/decisions/2026-05-29-sqlc-adoption.md — (EDIT) Appendix B: import-graph audit table per operator binary
```

#### Deep file dependency analysis
- Operators consume `api/internal/db/` symbols at compile time; symbol renames cascade.
- The audit drives Phase 3 task list (one Phase 3 task per affected operator).

#### Deep Dives
- **Audit process:**
  1. For each of 4 operators (`operators/cmd/tenant`, `operators/cmd/app`, `operators/cmd/preview`, `operators/cmd/platform`):
  2. Grep imports of `api/internal/db/...`
  3. Grep usage of imported symbols
  4. Record in table: operator → symbols imported → store source file
- **Output shape:** Markdown table with columns `Operator | Imported Symbols | Source Stores | Rewire Effort`.

#### Tasks
1. Grep imports for each operator binary.
2. Compile the import-graph table.
3. Estimate rewire effort per operator (low/medium/high).
4. Populate ADR-1 Appendix B.

#### TDD
```
RED:     test_operator_audit_has_all_4_binaries() — grep ADR-1 Appendix B for tenant/app/preview/platform
RED:     test_operator_audit_lists_concrete_symbols() — entries are not "TBD"
GREEN:   Fill table
REFACTOR: None expected
VERIFY:  grep -c "operator" docs/decisions/2026-05-29-sqlc-adoption.md returns ≥ 4
```

#### Acceptance Criteria
- [ ] All 4 operator binaries listed
- [ ] Concrete symbols per operator
- [ ] Rewire effort estimated

#### DoD
- [ ] Tasks 1-4 completed
- [ ] 2 RED tests GREEN
- [ ] ADR-1 Appendix B complete

---

### T0.8 — CI workflow updates (taskfiles/db.yml + quality wiring)

#### Objective
Wire all Phase 0 outputs into the canonical `task` runners so CI executes the new gates and render pipeline.

#### Evidence
Per ADR-2 §4 (render script wiring) + M-16 (sqlcsync gate registration) + M-19 (pinning).

#### Files to edit
```
taskfiles/db.yml — (EDIT) add task db:generate (sqlc generate), task db:render-schema (calls render script), task db:verify-sync (calls sqlcsync gate)
taskfiles/quality.yml — (EDIT) ensure all 3 new gates are part of `task quality:gates` invocation
Taskfile.yml — (EDIT) include taskfiles/db.yml if not already included
```

#### Deep file dependency analysis
- `taskfiles/db.yml` is the local runner for developer regen workflow.
- `taskfiles/quality.yml` is the CI gate runner.

#### Deep Dives
- **task db:generate:** `cd api/internal/db && sqlc generate` (uses local sqlc CLI; version check inside script).
- **task db:render-schema:** invokes `infra/scripts/ci/render-schema-gen-sql.sh`.
- **task db:verify-sync:** invokes `pkg/quality/sqlcsync/` gate.

#### Tasks
1. Create or edit `taskfiles/db.yml` with the 3 new tasks.
2. Verify `taskfiles/quality.yml` `quality:gates` task includes sqlcsync + sqlctxprop + tenantpredicate.
3. Verify `Taskfile.yml` includes db.yml.
4. Run `task --list` and verify all new tasks appear.
5. Run `task quality:gates` and verify 13 gates total run (was 10 + 3 new = 13).

#### TDD
```
RED:     test_taskfile_db_has_3_new_tasks() — grep db.yml for db:generate, db:render-schema, db:verify-sync
RED:     test_quality_gates_includes_3_new() — grep quality.yml for sqlc-sync, sqlctxprop, tenant-predicate
RED:     test_taskfile_root_includes_db_yml() — grep Taskfile.yml for include db.yml
GREEN:   Wire tasks
REFACTOR: None expected
VERIFY:  task --list 2>&1 | grep -c "db:" should return ≥ 3; task quality:gates 2>&1 | grep -c "✓\|✗" should return 13
```

#### Acceptance Criteria
- [ ] task db:generate works
- [ ] task db:render-schema works (calls render script)
- [ ] task db:verify-sync works (calls sqlcsync gate)
- [ ] task quality:gates includes 3 new gates
- [ ] task --list shows new tasks

#### DoD
- [ ] Tasks 1-5 completed
- [ ] 3 RED tests GREEN
- [ ] task quality:gates runs 13 gates total
- [ ] `task lint` passes

---

### T0.9 — Phase 0 sanity: end-to-end Phase 0 baseline GREEN

#### Objective
Verify that all Phase 0 deliverables compose correctly with no regressions before Phase 1 begins.

#### Evidence
End-to-end sanity check is mandatory before opening the Phase 1 store conversion scope.

#### Files to edit
```
(no files; verification task only)
```

#### Deep file dependency analysis
- This task verifies Phase 0 outputs as a system; no new files.

#### Deep Dives
- **Verification checklist:**
  1. `sqlc compile` exits 0
  2. `task db:render-schema` produces schema.gen.sql byte-identical to committed version
  3. `task quality:gates` exits 0 (13 gates)
  4. `task test` exits 0 (no regressions; Phase 0 added no new prod code)
  5. `task lint` exits 0
  6. `task validate:manifests` exits 0
  7. `git status` clean (no uncommitted changes)
  8. ADR-1 Appendix A + B complete

#### Tasks
1. Run all 8 verification checks.
2. Document results in CHANGELOG.md `[Unreleased]` under Added (sqlc tooling, 3 gates, render pipeline).
3. ~~Open feature branch `feat/sqlc-migration` off develop.~~ **Superseded by D8' async amendment (2026-05-29):** work direct on develop; no feature branch.
4. Commit Phase 0 deliverables **direct to develop** (per D8'); each substantive commit must keep develop CI GREEN.

#### TDD
```
RED:     test_phase0_baseline_passes() — run the 8-check script, assert exit 0
GREEN:   Fix any failures via earlier tasks T0.1-T0.8
REFACTOR: None expected
VERIFY:  bash docs/plans/scripts/phase0-sanity.sh (or similar runner)
```

#### Acceptance Criteria
- [ ] All 8 verification checks GREEN
- [ ] CHANGELOG.md [Unreleased] updated
- [ ] ~~Feature branch open~~ N/A per D8'
- [ ] Phase 0 commit landed **on develop** with CI GREEN

#### DoD
- [ ] All Phase 0 deliverables verified GREEN as a system
- [ ] CHANGELOG entries committed
- [ ] Phase 1 unblocked

---

## Phase 1: Pilot — 5 Critical Stores

**Objective:** Convert the 5 most critical (and most-bug-prone) stores first to validate the migration pattern before scaling: `release_store` (M07 bug site), `build_store`, `app_attestation_store`, `tenant_store`, `audit_store`.

**Pair-review required for security-sensitive stores per M-4:** `audit_store` (and Phase 2 `secret_store`, `bootstrap_store`, `tenant_deletion_store`, `cell_store`).

### T1.1 — Convert release_store (M07 bug site)

#### Objective
Replace `api/internal/db/release_store.go` (637 LOC, 6 INSERT/SELECT statements, scanReleaseFields) with sqlc-generated typed funcs + thin Go wrapper for state-machine integration.

#### Evidence
M07 Rekor URL persistence 3-layer bug (b3375ed0) lived in this exact file. Highest-value conversion; pilot validates the pattern.

#### Files to edit
```
api/internal/db/queries/releases.sql — (NEW) hand-written queries: ListReleasesByEnv, GetReleaseByID, InsertRelease, UpdateReleaseStatus, ArchiveRelease, GetActiveByEnv, etc. (~13 queries)
api/internal/db/release_wrapper.go — (NEW) thin Go wrapper: ReleaseStore struct with state-machine integration; methods receive pgx.Tx; call generated.Queries.WithTx(tx).*; preserve typed state-machine wrapper logic
api/internal/db/release_store.go — DELETE
api/internal/db/release_store_test.go — RESHAPE per T0.6 matrix (KEEP-INVARIANT tests reshape to new wrapper)
api/internal/services/build_reconcile_complete.go — (EDIT) update call sites to use new ReleaseStore wrapper
api/internal/services/release_inspector.go — (EDIT) update call sites
api/internal/db/generated/* — (REGENERATED) sqlc generates Queries struct + Scan funcs
api/internal/db/schema.gen.sql — (NO CHANGE expected; if change occurs, render gate flags)
```

#### Deep file dependency analysis
- `release_store.go` is consumed by `services/build_reconcile_complete.go` + `services/release_inspector.go` + `services/build_orchestrator_submit.go` + others. All call sites must update.
- `release_wrapper.go` provides the state-machine integration (`releaseMachine.SafeTransition`) per D4.
- Generated code under `api/internal/db/generated/` is the canonical Queries surface; never edit by hand.

#### Deep Dives
- **Query shape (releases.sql):** named queries with sqlc annotations:
  ```sql
  -- name: GetReleaseByID :one
  SELECT * FROM releases WHERE id = $1 AND tenant_id = $2;

  -- name: InsertRelease :one
  INSERT INTO releases (id, build_id, tenant_id, project_id, environment, status, ...)
  VALUES ($1, $2, $3, $4, $5, $6, ...)
  RETURNING *;

  -- name: UpdateReleaseStatus :one
  UPDATE releases SET status = $1, ... WHERE id = $2 AND tenant_id = $3 RETURNING *;
  ```
- **Wrapper pattern:** `ReleaseStore.UpdateStatus(ctx, tx, id, event)` calls `generated.New(tx).GetReleaseByID(ctx, params)` → `releaseMachine.SafeTransition(currentPhase, event)` → `generated.New(tx).UpdateReleaseStatus(ctx, newParams)` — all inside the caller's transaction.
- **Tenant predicate:** every query on `releases` includes `tenant_id = $N` per T0.5 tenant-predicate gate.
- **Rekor URL column:** `image_attestation_rekor_url` IS in the queries (this is the M07 fix that motivated the whole migration; sqlc enforces it at compile time).
- **Invariants:** every call site that previously called `release_store.Foo(...)` now calls `ReleaseStore.Foo(...)` (capital R) on the wrapper; signatures equivalent + accept `pgx.Tx`.

#### Tasks
1. Write `queries/releases.sql` with ~13 named queries covering all current release_store operations.
2. Run `task db:generate` to produce `generated/releases.sql.go`.
3. Write `release_wrapper.go` with state-machine integration.
4. Reshape `release_store_test.go` (KEEP-INVARIANT tests adapted to new wrapper).
5. Update all callers in services/ to use new wrapper.
6. Delete `release_store.go`.
7. Verify `task test ./api/...` GREEN.
8. Verify `task quality:gates` GREEN (sqlcsync + sqlctxprop + tenantpredicate all pass).

#### TDD
```
RED:     test_release_store_get_by_id_tenant_isolation() — assert query rejects cross-tenant access
RED:     test_release_store_insert_with_rekor_url() — assert Rekor URL persists (M07 regression test)
RED:     test_release_store_update_status_transitions_via_state_machine() — assert SafeTransition called
RED:     test_release_store_update_status_panics_on_invalid_transition() — invalid event panics (programming error)
RED:     test_release_store_archive_atomic() — archive within single tx
RED:     test_release_store_concurrent_inserts_within_lock() — 2 concurrent inserts inside withEnvironmentLock rejected
GREEN:   Implement queries.sql + wrapper.go + delete old store
REFACTOR: None expected (pilot is conservative)
VERIFY:  go test ./api/internal/db/release_wrapper_test.go && task quality:gates
```

#### Acceptance Criteria
- [ ] release_store.go DELETED
- [ ] queries/releases.sql + generated/releases.sql.go + release_wrapper.go exist
- [ ] All 6 RED tests GREEN
- [ ] M07 Rekor URL regression test passes (image_attestation_rekor_url persists round-trip)
- [ ] All services/ callers updated (no stale `release_store.` imports)
- [ ] `task test` GREEN
- [ ] `task quality:gates` 13 gates GREEN
- [ ] Pass: `/code-audit` complexity check (wrapper ≤ 200 LOC; ≤ 10 cyclomatic per method)
- [ ] Pass: `/code-audit` size check (release_wrapper.go ≤ 300 LOC)

#### DoD
- [ ] Tasks 1-8 completed
- [ ] 6 RED tests GREEN
- [ ] release_store.go DELETED (verified by `git status`)
- [ ] All callers updated
- [ ] task test + lint + quality:gates GREEN
- [ ] M07 regression captured

---

### T1.2 — Convert build_store

#### Objective
Replace `api/internal/db/build_store.go` with sqlc-generated funcs + wrapper. Critical because `build_orchestrator_submit.go` depends on it for the concurrent-build invariant.

#### Evidence
build_store carries the `HasActiveBuild` invariant inside the env lock; Rule 6 invariant.

#### Files to edit
```
api/internal/db/queries/builds.sql — (NEW) ~15 queries
api/internal/db/build_wrapper.go — (NEW) wrapper with state-machine + active-build check
api/internal/db/build_store.go — DELETE
api/internal/db/build_store_test.go — RESHAPE
api/internal/services/build_orchestrator_submit.go — (EDIT) update call sites
api/internal/services/build_orchestrator.go — (EDIT)
api/internal/routes/builds.go — (EDIT) update call sites (indirect via service)
```

#### Deep file dependency analysis
- `build_orchestrator_submit.go` calls `buildStore.HasActiveBuild(ctx, tx, ...)` INSIDE `withEnvironmentLock` — Rule 6 invariant. Wrapper must preserve `pgx.Tx` propagation per D3.
- Build state machine has 7 states; wrapper handles transitions per D4.

#### Deep Dives
- **Query shape:** name queries for HasActiveBuild, GetBuildByID, ListBuildsByProject, InsertBuild, UpdateBuildStatus, SetWorkflowID, SetAppsSpec, MarkFailed, MarkCancelled, etc.
- **Wrapper pattern:** identical to T1.1 (release_wrapper).
- **Invariant:** HasActiveBuild query MUST run inside the same tx as InsertBuild (lock semantics depend on this).

#### Tasks
1. Write queries/builds.sql.
2. Run task db:generate.
3. Write build_wrapper.go.
4. Reshape build_store_test.go.
5. Update callers in services/ + routes/.
6. Delete build_store.go.
7. Verify task test + quality:gates GREEN.

#### TDD
```
RED:     test_build_store_has_active_build_inside_tx() — assert HasActiveBuild + Insert atomic
RED:     test_build_store_concurrent_submit_rejected() — 1×202+1×409 invariant preserved
RED:     test_build_store_state_machine_transitions() — QUEUED→RUNNING→SUCCEEDED
RED:     test_build_store_failed_queued_transition() — failQueuedBuild path
RED:     test_build_store_cancel_in_running_phase() — RUNNING→CANCELLING→CANCELLED
GREEN:   Implement
REFACTOR: extract apps-spec persistence into helper
VERIFY:  go test ./api/internal/db/build_wrapper_test.go && task quality:gates
```

#### Acceptance Criteria
- [ ] build_store.go DELETED
- [ ] 5 RED tests GREEN
- [ ] Concurrent-deploy invariant preserved (Rule 6)
- [ ] task test + quality:gates GREEN

#### DoD
- [ ] Tasks 1-7 completed
- [ ] 5 RED tests GREEN
- [ ] All callers updated
- [ ] task test + lint + quality:gates GREEN

---

### T1.3 — Convert app_attestation_store

#### Objective
Replace `api/internal/db/app_attestation_store.go` (+ `app_attestation_store_tx_test.go`) — preserves attestation persistence for M07 keyless signing.

#### Evidence
M07 attestation flow writes to release_app_attestations table; tested under tx in `app_attestation_store_tx_test.go`.

#### Files to edit
```
api/internal/db/queries/app_attestations.sql — (NEW) ~8 queries
api/internal/db/app_attestation_wrapper.go — (NEW)
api/internal/db/app_attestation_store.go — DELETE
api/internal/db/app_attestation_store_test.go — RESHAPE
api/internal/db/app_attestation_store_tx_test.go — RESHAPE (tx tests are KEEP-INVARIANT)
api/internal/services/build_reconcile_complete.go — (EDIT)
api/internal/services/build_orchestrator.go — (EDIT)
```

#### Deep file dependency analysis
- Attestation tx tests are critical: multi-row attestation persistence must atomically commit.
- M07 keyless flow depends on `release_app_attestations.image_signature` + `image_attestation_rekor_url` persistence.

#### Deep Dives
- **Query shape:** UpsertAttestation, GetAttestationsByReleaseID, ListAttestationsForBuild, etc.
- **Invariant:** all attestation writes are batched in a single tx per release.

#### Tasks
1. Write queries/app_attestations.sql.
2. Generate + wrapper + delete old store.
3. Reshape both _test.go files.
4. Update callers.
5. Verify task test + quality:gates GREEN.

#### TDD
```
RED:     test_attestation_batch_atomic() — N attestations in one tx, all-or-nothing
RED:     test_attestation_rekor_url_persists() — M07 regression
RED:     test_attestation_signature_persists() — M07 keyless regression
RED:     test_attestation_upsert_idempotent() — second call with same key is no-op
GREEN:   Implement
REFACTOR: None expected
VERIFY:  go test + quality:gates
```

#### Acceptance Criteria
- [ ] app_attestation_store.go DELETED
- [ ] 4 RED tests GREEN
- [ ] M07 attestation regression captured
- [ ] task test + quality:gates GREEN

#### DoD
- [ ] Tasks 1-5 completed
- [ ] 4 RED tests GREEN

---

### T1.4 — Convert tenant_store (multi-tenant SoT)

#### Objective
Replace `api/internal/db/` tenant lifecycle stores (tenant_store, tenant_deletion_store) — tenant table is the multi-tenant SoT.

#### Evidence
Tenant lifecycle touches placement, deletion, restore — critical paths.

#### Files to edit
```
api/internal/db/queries/tenants.sql — (NEW)
api/internal/db/tenant_wrapper.go — (NEW)
api/internal/db/tenant_store.go — DELETE
api/internal/db/tenant_deletion_store.go — DELETE
api/internal/db/tenant_store_test.go — RESHAPE
api/internal/services/* — (EDIT) update callers
api/internal/routes/* — (EDIT) update callers (admin endpoints)
```

#### Deep file dependency analysis
- Tenant placement (cell_id) is written by `RoundRobinEngine.AssignAndExecute` inside a SERIALIZABLE tx with pg_advisory_xact_lock; wrapper must preserve.
- Soft-delete + restore flow tested under tx.

#### Deep Dives
- **Pair-review required (M-4) — sensitive table.**
- **Tenant table itself is allowlisted in tenantpredicate gate per T0.5 edge case.**

#### Tasks
1. Pair-review with platform-security-engineer scheduled.
2. Write queries/tenants.sql.
3. Generate + wrapper + delete old stores.
4. Reshape tests.
5. Update callers.
6. Verify task test + quality:gates GREEN.

#### TDD
```
RED:     test_tenant_placement_serializable_tx() — placement write inside serializable tx
RED:     test_tenant_soft_delete_then_restore() — 24h grace period preserves data
RED:     test_tenant_hard_delete_cascades_no_orphans() — hard delete leaves no orphan projects/builds
RED:     test_tenant_cell_pin_updates_db_and_cr_atomically() — CellPinner pattern preserved
GREEN:   Implement
REFACTOR: None expected
VERIFY:  go test + quality:gates
```

#### Acceptance Criteria
- [ ] tenant_store.go + tenant_deletion_store.go DELETED
- [ ] 4 RED tests GREEN
- [ ] Pair-review documented in commit message (M-4 evidence)
- [ ] task test + quality:gates GREEN

#### DoD
- [ ] Tasks 1-6 completed
- [ ] 4 RED tests GREEN
- [ ] Pair-review commit signature in log

---

### T1.5 — Convert audit_store (sensitive, security-paired)

#### Objective
Replace `api/internal/db/audit_store.go` — audit log is the durable evidence trail for credential ops, tenant pin/evict, break-glass.

#### Evidence
Per platform-security-engineer R-SEC-1 (CRITICAL) + M-3 (audit-log call-site preservation proof) + M-4 (pair-review).

#### Files to edit
```
api/internal/db/queries/audit_log.sql — (NEW)
api/internal/db/audit_wrapper.go — (NEW)
api/internal/db/audit_store.go — DELETE
api/internal/db/audit_store_test.go — RESHAPE
infra/scripts/sqlc-migration-audit-callsite-diff.sh — (NEW, ephemeral) compares pre/post call-site lists; deleted after migration
api/internal/services/* — (EDIT) update audit writers (7 paths currently)
api/internal/routes/admin_*.go — (EDIT) admin endpoint audit writers
```

#### Deep file dependency analysis
- Audit writes happen from 7 paths (build submit, tenant pin, project delete, secret set/delete, deploy promote, rollback, admin actions). All must update symmetrically.
- M-3 requires explicit pre/post diff script to prove no call site lost.

#### Deep Dives
- **Pair-review required (M-4) — sensitive table.**
- **M-3 evidence:** run diff script before merge; output committed as evidence.

#### Tasks
1. Pair-review with platform-security-engineer scheduled.
2. Inventory all current audit_log write call sites (grep pre-migration).
3. Write queries/audit_log.sql.
4. Generate + wrapper + delete old store.
5. Update all 7 call sites + admin endpoints.
6. Run audit-callsite-diff.sh; verify zero loss.
7. Verify task test + quality:gates GREEN.

#### TDD
```
RED:     test_audit_write_persists_user_id() — actor identity preserved
RED:     test_audit_write_persists_correlation_id() — request trace preserved
RED:     test_audit_query_by_action_filterable() — admin filter endpoint works
RED:     test_audit_immutable() — UPDATE on audit_log fails (DB CHECK)
RED:     test_audit_callsite_diff_empty() — diff script exits 0 (M-3 evidence)
GREEN:   Implement
REFACTOR: None expected
VERIFY:  go test + quality:gates + diff script
```

#### Acceptance Criteria
- [ ] audit_store.go DELETED
- [ ] 5 RED tests GREEN
- [ ] Audit-callsite-diff script committed + run output committed as M-3 evidence
- [ ] Pair-review commit signature
- [ ] task test + quality:gates GREEN

#### DoD
- [ ] Tasks 1-7 completed
- [ ] 5 RED tests GREEN
- [ ] M-3 evidence committed
- [ ] M-4 pair-review documented

---

### T1.6 — Phase 1 sanity: end-to-end Phase 1 baseline GREEN

#### Objective
Verify all 5 pilot stores integrate as a system before opening Phase 2 (39 more stores).

#### Files to edit
```
(no files; verification task)
```

#### Tasks
1. `task test ./api/...` GREEN
2. `task quality:gates` 13 gates GREEN
3. `task lint` GREEN
4. `task validate:manifests` GREEN
5. Sample integration test: end-to-end deploy on cleanroom k3d (smoke `loopzero deploy` lifecycle)
6. Commit Phase 1 deliverables to develop branch (per D8').
7. CHANGELOG.md [Unreleased] updated with 5 store conversions.
8. Owner daily diary entry per M-15.

#### Acceptance Criteria
- [ ] All 5 sanity checks GREEN
- [ ] CHANGELOG updated
- [ ] Phase 2 unblocked

#### DoD
- [ ] All 5 verifications GREEN
- [ ] Daily diary updated

---

## Phase 2: Remaining Stores (39 in 4 Batches of ~10)

**Objective:** Convert the remaining 39 stores in 4 batches grouped by domain coupling. Each batch follows the T1.x pattern (queries.sql → wrapper.go → callers → tests → delete).

### T2.1 — Batch A: Project/Environment Domain (10 stores)

#### Objective
Convert: `project_store`, `environment_store`, `environment_target_store`, `domain_store`, `custom_domain_store`, `scaling_store`, `repository_store`, `idempotency_store`, `team_store`, `active_team_store`.

#### Files to edit (per store, repeated 10×)
```
api/internal/db/queries/{domain}.sql — (NEW)
api/internal/db/{domain}_wrapper.go — (NEW)
api/internal/db/{domain}_store.go — DELETE
api/internal/db/{domain}_store_test.go — RESHAPE
api/internal/services/* — (EDIT) update callers
```

#### Tasks
1. For each of 10 stores: write queries.sql, generate, wrapper, reshape tests, update callers, delete old store.
2. Verify task test + quality:gates GREEN after each store (incremental green).
3. Commit batch to develop branch (per D8').

#### TDD (per store)
```
RED:     test_{domain}_tenant_isolation() — assert WHERE tenant_id predicate
RED:     test_{domain}_crud_operations() — basic CRUD
RED:     test_{domain}_state_machine_transitions() — if applicable
GREEN:   Implement
VERIFY:  go test + quality:gates
```

#### Acceptance Criteria
- [ ] All 10 stores DELETED + wrappers shipped
- [ ] 30+ unit tests GREEN (3 per store minimum)
- [ ] task test + quality:gates GREEN

#### DoD
- [ ] Batch A complete
- [ ] CHANGELOG updated
- [ ] Daily diary updated per M-15

---

### T2.2 — Batch B: Preview/Application/Cell (10 stores)

#### Objective
Convert: `preview_store`, `application_store`, `cell_store` (security-paired), `clustercredential_store`, `bootstrap_store` (security-paired), `bsl_store`, `cost_store`, `quota_store`, `gc_store`, `build_gc_store`.

#### Special: pair-review for `cell_store` + `bootstrap_store` per M-4.

#### Tasks (per store, repeated 10×)
Same pattern as T2.1.

#### TDD
Same pattern as T2.1; sensitive stores get additional security tests.

#### Acceptance Criteria
- [ ] All 10 stores DELETED + wrappers
- [ ] 30+ unit tests GREEN
- [ ] Pair-review documented for cell_store + bootstrap_store
- [ ] task test + quality:gates GREEN

#### DoD
- [ ] Batch B complete
- [ ] M-4 evidence for 2 sensitive stores
- [ ] CHANGELOG + diary updated

---

### T2.3 — Batch C: Secrets/Memberships/Routing (10 stores)

#### Objective
Convert: `secret_store` (security-paired), `membership_store`, `role_store`, `routing_store`, `source_store`, `route_store`, `coverage_store`, `coverage_boost_store`, `placement_store`, `cell_scoped_queries`.

#### Special: pair-review for `secret_store` per M-4 (5th sensitive store).

#### Tasks
Same pattern.

#### Acceptance Criteria
- [ ] All 10 stores DELETED
- [ ] secret_store pair-review documented
- [ ] task test + quality:gates GREEN

#### DoD
- [ ] Batch C complete
- [ ] M-4 evidence for secret_store

---

### T2.4 — Batch D: Misc + Cleanup (9 stores)

#### Objective
Convert remaining 9 stores: `domain_store_param_guard`, `audit_query_helpers`, plus any stores discovered during prior batches that didn't fit other categories.

#### Tasks
Same pattern. Final batch closes the 44-store accounting.

#### Acceptance Criteria
- [ ] All remaining stores converted (44 total accounting verified)
- [ ] Final `*_store.go` deletion complete
- [ ] task test + quality:gates GREEN

#### DoD
- [ ] Batch D complete
- [ ] `find api/internal/db -name '*_store.go'` returns empty
- [ ] CHANGELOG marks "Removed: all hand-rolled *_store.go"

---

## Phase 3: Operator Binary Rewire (M-22)

**Objective:** Per ADR-1 Appendix B (T0.7 import-graph audit), update the 4 operator binaries (tenant, app, preview, platform) to import from new wrappers / generated package instead of deleted stores.

### T3.1 — Tenant operator rewire

#### Objective
Update `operators/cmd/tenant/main.go` + `operators/internal/tenant/` to use new wrapper imports.

#### Files to edit
```
operators/cmd/tenant/main.go — (EDIT) update imports
operators/internal/tenant/* — (EDIT) update store usage
operators/internal/tenant/*_test.go — (RESHAPE) Ginkgo specs adapted
```

#### Tasks
1. Update imports per T0.7 audit.
2. Update reconcile loop call sites.
3. Reshape Ginkgo envtest specs.
4. Verify `go test ./operators/internal/tenant/...` GREEN.

#### TDD
```
RED:     test_tenant_operator_compiles_against_new_db_pkg() — go build ./operators/cmd/tenant/ exits 0
RED:     test_tenant_reconcile_uses_tenant_wrapper() — Ginkgo spec verifies wrapper invocation
GREEN:   Update imports
VERIFY:  go test ./operators/internal/tenant/ && go build ./operators/cmd/tenant/
```

#### Acceptance Criteria
- [ ] operators/cmd/tenant/main.go compiles
- [ ] All envtest specs GREEN
- [ ] No stale imports of deleted stores

#### DoD
- [ ] Tasks 1-4 complete
- [ ] 2 RED tests GREEN

---

### T3.2 — App operator rewire

Same pattern as T3.1 for `operators/cmd/app/`.

### T3.3 — Preview operator rewire

Same pattern for `operators/cmd/preview/`.

### T3.4 — Platform operator rewire

Same pattern for `operators/cmd/platform/`.

### T3.5 — Operators sanity: all 4 binaries build + test GREEN

#### Tasks
1. `go build ./operators/cmd/{tenant,app,preview,platform}/` all exit 0.
2. `go test ./operators/...` GREEN.
3. CHANGELOG updated.
4. Daily diary updated.

#### Acceptance Criteria
- [ ] All 4 binaries compile
- [ ] All operator tests GREEN

#### DoD
- [ ] Phase 3 complete

---

## Phase 4: Raw Subpackage + schema_mirror_test DELETE (two-step)

**Objective:** Ship `api/internal/db/raw/` escape hatch for ≤5% queries that sqlc can't generate (LATERAL, recursive CTE, dynamic WHERE), then DELETE schema_mirror_test.go per D7 two-step protocol.

### T4.1 — Raw subpackage scaffold + AST exclusion gate

#### Objective
Create `api/internal/db/raw/` with README declaring scope, sensitive-table exclusion list, and AST gate that blocks raw queries against sensitive tables.

#### Files to edit
```
api/internal/db/raw/README.md — (NEW) declares scope ≤5%, sensitive-table exclusion list, when to add raw query
api/internal/db/raw/sensitive_tables.go — (NEW) committed exclusion list (secret, audit_log, bootstrap, tenant_deletion, cell)
api/internal/db/raw/*.go — (NEW, as needed during conversion) raw query implementations
pkg/quality/tenantpredicate/check.go — (EDIT) extend gate to also check raw/ subpackage against sensitive_tables list
```

#### Deep file dependency analysis
- Raw subpackage is opt-in; queries land only when an existing Phase 1/2 conversion identifies a query sqlc cannot generate.

#### Deep Dives
- **Allowed query shapes (per ADR-1 §6 D5):** LATERAL joins, recursive CTEs, dynamic WHERE clauses with `pgx.QueryRow` directly.
- **Forbidden tables (enforced by AST gate):** secrets, audit_log, bootstrap, tenant_deletion, cell.

#### Tasks
1. Create raw/README.md.
2. Create raw/sensitive_tables.go.
3. Extend tenantpredicate gate to enforce.
4. Identify any Phase 1/2 conversions that needed raw queries; land them in raw/.
5. Verify task quality:gates GREEN.

#### TDD
```
RED:     test_raw_excludes_sensitive_tables() — fixture raw query on secrets table, assert exit 1
RED:     test_raw_allows_non_sensitive_tables() — fixture raw query on builds table, assert exit 0
GREEN:   Implement
VERIFY:  go test + quality:gates
```

#### Acceptance Criteria
- [ ] raw/README.md declares scope
- [ ] sensitive_tables.go committed
- [ ] tenantpredicate gate extended
- [ ] 2 RED tests GREEN

#### DoD
- [ ] Tasks 1-5 complete
- [ ] Raw subpackage exists and is gate-protected

---

### T4.2 — schema_mirror_test DELETE (step 1 of D7 two-step)

#### Objective
Per D7: ship sqlcsync gate + 1 full CI green cycle BEFORE deleting schema_mirror_test.go. Step 1 = ensure 1 full CI cycle has been GREEN on the feature branch with sqlcsync registered **AND with non-trivial coverage that exercises real codegen** (EC-5 explicit attestation — prevents trivially-green cycles from un-gating the delete).

#### Tasks
1. Verify last CI run on develop is GREEN on the substantive commit (per D8') with sqlcsync gate active.
2. **EC-5 attestation:** verify that GREEN CI run is on a commit where (a) `pkg/quality/sqlcsync/` is registered in `task quality:gates`, AND (b) `api/internal/db/queries/` contains ≥30 .sql files (proves Phase 1+2 query authoring is substantive), AND (c) `api/internal/db/generated/` contains ≥30 .go files (proves codegen ran on real input), AND (d) `find api/internal/db -name '*_store.go' | wc -l` returns ≤14 (proves ≥30/44 stores have been converted). The "green cycle" cannot be trivially passing on Phase 0 baseline alone.
3. Open a separate commit on develop that deletes schema_mirror_test.go (per D8').
4. Verify CI is still GREEN after the delete commit.

#### TDD
```
RED:     test_attestation_requires_substantive_codegen() — given a commit at Phase 0 baseline only (zero queries, zero generated files), attestation script exits 1 with explicit message "EC-5: sqlcsync green cycle requires ≥30 queries + ≥30 generated files + ≤14 remaining stores"
RED:     test_attestation_passes_at_phase2_completion() — given commit with full Phase 2 done (~44 queries, ~44 generated, 0 stores remaining), attestation script exits 0
RED:     test_delete_blocked_without_attestation() — git pre-commit hook (or CI gate) blocks schema_mirror_test.go DELETE if attestation script exits non-zero
GREEN:   Implement attestation script + integrate in CI/precommit
REFACTOR: None expected
VERIFY:  bash infra/scripts/ci/sqlcsync-green-cycle-attestation.sh (returns 0 only when substantive)
```

#### Acceptance Criteria
- [ ] sqlcsync gate has 1+ green CI run
- [ ] **EC-5:** GREEN CI run is on a commit where (a) sqlcsync registered, (b) ≥30 queries.sql files, (c) ≥30 generated/*.go files, (d) ≤14 remaining `*_store.go` files. Attestation script `infra/scripts/ci/sqlcsync-green-cycle-attestation.sh` exits 0 against that commit.
- [ ] schema_mirror_test.go DELETED in separate commit
- [ ] CI post-delete is GREEN
- [ ] 3 EC-5 RED tests GREEN

#### DoD
- [ ] D7 two-step delete complete
- [ ] EC-5 attestation script shipped + integrated in pre-commit/CI
- [ ] CHANGELOG "Removed: schema_mirror_test.go (superseded by pkg/quality/sqlcsync per ADR-1, attested by EC-5 substantive-coverage gate)"

---

### T4.3 — Phase 4 sanity

#### Tasks
1. `task test` GREEN
2. `task quality:gates` 13 gates GREEN
3. `task lint` GREEN
4. `find api/internal/db -name '*_store.go'` returns empty (all deleted)
5. `api/internal/db/schema_mirror_test.go` does not exist
6. CHANGELOG complete

#### DoD
- [ ] All 6 verifications GREEN
- [ ] Phase 4 closure documented

---

## Final Phase: Validation V1-V11 + Dogfood QA (MANDATORY)

> This phase runs AFTER Phases 0-4 complete. The plan is NOT done until ALL validation phases V1-V11 + Dogfood QA pass.

**Objective:** Validate that the implemented changes work end-to-end against cleanroom-fresh infrastructure + drill iter 56 + prod-snapshot smoke. Per sponsor mandate 2026-05-29 ("AO FINAL DE TODO O PLANO DEVE RODAR O CLEANROON, DOGFOOD E CHAOS-TEST FUNCIIONAIS FULL. SEM ISSO NAO ACEITO A ENTREGA. QUANDO EXECUTAR CORRIGIR SEM WORAROUNDS POSSIVEIS PROBLEMAS QUE ENCONTRAR"), Final Phase V8/V9/V10 are **Ralph-executable** via multi-Bash-call sequence (proven precedent: MEET-001 engineering closure 2026-05-29, audits `2026-05-29-dogfood-cleanroom-fresh-evidence.md` + `2026-05-29-chaos-test-full-cleanroom-fresh-evidence.md`). Only V11 (7-day wall-clock) remains operator-bound. **No-workaround discipline:** when Ralph hits a problem during execution, fix at root — never disable the gate, skip the spec, raise the timeout, or accept a partial PASS.

### V1: sqlc codegen reproducibility

#### Objective
Verify same queries.sql + same schema.gen.sql produces byte-identical Go output across 3 runs.

#### Tasks
1. Run `task db:generate` 3 times in CI matrix.
2. Diff generated/ output between runs.
3. Assert byte-identical.

#### Acceptance Criteria
- [ ] 3 runs byte-identical

#### DoD
- [ ] V1 GREEN, evidence in CI logs

---

### V2: schema.gen.sql replay determinism

Same as V1 but for the render script: run `task db:render-schema` 3 times, byte-identical output.

#### DoD
- [ ] V2 GREEN

---

### V3: NULL-handling parity

#### Objective
Per SRE condition 4 (R-SRE-4): verify every `*T` field in generated structs has paired test asserting NULL handling matches pre-migration `sql.NullString` semantics.

#### Tasks
1. Enumerate all `*T` fields in generated/ structs.
2. Write parity test per field.
3. Verify all tests GREEN.

#### Acceptance Criteria
- [ ] Per-field parity tests GREEN

#### DoD
- [ ] V3 GREEN

---

### V4: Tenant-predicate AST check

Already enforced by T0.5 tenantpredicate gate. V4 = verify gate runs on every CI build.

#### DoD
- [ ] V4 GREEN

---

### V5: Audit-log call-site preservation

Already produced by T1.5 (audit-callsite-diff.sh). V5 = verify diff output is empty.

#### DoD
- [ ] V5 GREEN

---

### V6: Advisory-lock tx propagation

Already enforced by T0.4 sqlctxprop gate. V6 = verify gate runs.

#### DoD
- [ ] V6 GREEN

---

### V7: Behavioral invariant tests GREEN

#### Objective
Verify all KEEP-INVARIANT tests from T0.6 matrix pass against new sqlc-backed code.

#### Tasks
1. Run `task test ./api/internal/db/...` against new code.
2. Verify invariant tests: concurrent-deploy 1×202+1×409, idempotency replay, lock-release-on-crash, state-machine invalid-transition panic.

#### Acceptance Criteria
- [ ] All KEEP-INVARIANT tests GREEN

#### DoD
- [ ] V7 GREEN

---

### V8: Drill iter 56 LIVE GREEN (Ralph-executable)

#### Objective
Per SRE condition 2 (M-7): drill prod-identical 4-cluster stack runs sqlc-migrated code; L2 harness PASS.

#### Tasks (Ralph-executable per v1.3 sponsor mandate; sponsor must grant `TODAS AS PERMISSOES` for destructive doctl operations)
1. Foreground: `task drill:state-reset` (~3min) — clean prior drill stack remnants.
2. Background via `nohup setsid`: `task drill:provision` — provision 4-cluster prod-identical (cost-tracked $0.32/hr, watchdog `task drill:watchdog` auto-teardown 6h).
3. Background poll: `task drill:helmfile-bootstrap` until PASS=42 FAIL=0 on doctor against drill stack.
4. Foreground: `task drill:l2-scale` (L0+L1+L2 harness, ~20min, evidence to `/tmp/drill-iter-56-evidence/`).
5. Foreground: `task drill:evidence-capture` (kubectl-gather pre-teardown).
6. Foreground: `task drill:teardown-validator` (mandatory cleanup; verifies doctl resource count = 0).
7. Commit evidence to `.claude/knowledge-base/audits/2026-05-29-sqlc-drill-iter-56-evidence.md`.
8. **No-workaround discipline:** if success_rate < 100% OR p95 > 10min, fix at root (no threshold relaxation, no `--skip-flaky` flag) → re-run iter 57.

#### Acceptance Criteria
- [ ] Drill iter 56 GREEN (success_rate=100%, p95 ≤ 10min)
- [ ] Evidence audit committed
- [ ] Teardown validator GREEN (zero orphan cloud resources)

#### DoD
- [ ] V8 GREEN (Ralph-executed, evidence in audit doc)

---

### V9: Dogfood full GREEN on cleanroom-fresh (Ralph-executable)

#### Objective
Per SRE condition (V9 of validation plan): `/dogfood full` on cleanroom-fresh infra returns 0 functional FAIL.

#### Tasks (Ralph-executable; proven via MEET-001 closure 2026-05-29)
1. Foreground: `task clean:all` (~3-5min) — destroy 4 k3d clusters + registry.
2. Background via `nohup setsid`: `task setup` (~15-25min) — recreate clusters + helmfile sync + seed JWT.
3. Background: `nohup setsid tilt up > /tmp/v9-tilt.log 2>&1 &` — dev-loop hot reload.
4. Poll: `task doctor` until PASS=42 FAIL=0 (mgmt API Running, 4 split operators Running).
5. Mint fresh JWT: `HYDRA_NAMESPACE=loopzero-system bash infra/helmfile/scripts/seed-local-user.sh` (TTL=1h).
6. Background: `LOOPZERO_E2E=1 LOOPZERO_TOKEN=$(cat ~/.loopzero/local-dogfood-token.txt) go run github.com/onsi/ginkgo/v2/ginkgo -v --label-filter=journey --timeout=60m ./infra/tests/e2e/` (~60min).
7. Capture evidence per dogfood skill protocol → audit doc `2026-05-29-sqlc-dogfood-cleanroom-fresh-evidence.md`.
8. **No-workaround discipline:** any FAIL → root-cause + commit fix + re-run from step 1 (cleanroom-fresh re-validation; no warm re-run accepted).

#### Acceptance Criteria
- [ ] `task doctor` PASS=42 FAIL=0 on cleanroom-fresh
- [ ] Dogfood full ≥ 100 RUNNABLE PASS / 0 functional FAIL
- [ ] CLI Prova de Vida + Prova de Morte both GREEN

#### DoD
- [ ] V9 GREEN (Ralph-executed, evidence in audit doc)

---

### V10: Production-snapshot smoke (Ralph-executable; sponsor mandate)

#### Objective
Per production-design-reviewer condition 3 (M-21): smoke `loopzero deploy` against PG-snapshot-of-prod schema + chaos-test full on cleanroom-fresh stack (per sponsor 2026-05-29 mandate the chaos-test full IS part of acceptance).

#### Tasks (Ralph-executable)
1. After V9 cleanroom-fresh stack remains alive: apply prod PG schema snapshot via `psql < .claude/knowledge-base/audits/prod-schema-snapshot.sql` (or replay golang-migrate on PG-snapshot if snapshot doc-only).
2. Foreground: smoke deploy via `$LOOPZERO deploy --json` against the prod-schema PG; assert HTTP 200 + JWT auth flow + release ACTIVE.
3. Background: `bash infra/scripts/test/run-full.sh --baseline-duration 1m` — chaos-test FULL (10 scenarios C01-C10, ~52min). Per sponsor mandate this is part of V10.
4. Capture chaos evidence → audit `2026-05-29-sqlc-chaos-test-full-cleanroom-fresh-evidence.md`.
5. Cleanup: `kubectl delete podchaos,networkchaos,iochaos,stresschaos --all -A` (mandatory zero-orphan invariant).
6. **No-workaround discipline:** any SLO violation during chaos window OR smoke deploy FAIL → fix at root (no `--skip-scenario` flag, no extended recovery budget, no mock).

#### Acceptance Criteria
- [ ] Smoke deploy GREEN against prod-schema PG
- [ ] chaos-test --full 10/10 PASS (C03 may SKIP if no deployed tenant pod available, per MEET-001 precedent)
- [ ] SLO held entire chaos window (each scenario's `loopzero.io/slo-query` annotation)
- [ ] Zero orphan chaos resources post-cleanup

#### DoD
- [ ] V10 GREEN (Ralph-executed, both audits committed)

---

### V11: 7-day observation window (post-merge ongoing)

#### Objective
Per ADR-3: 7-day post-merge window; any P0/P1 traceable to db/ triggers revert.

#### Tasks
1. **Per D8' there is no merge event** — substantive commits land directly on develop incrementally. V11 starts when the last sqlc-migration substantive commit lands on develop.
2. Monitor for 7 days; daily check-in per M-15 diary.
3. If any rollback trigger fires, execute ADR-3 rollback playbook.
4. If 7 days clean, ADR-1 transitions from "active monitoring" to "historical record".

#### Acceptance Criteria
- [ ] 7 days clean OR rollback executed
- [ ] Daily diary entries

#### DoD
- [ ] V11 closure documented

---

### Final Phase: Dogfood QA (mandatory per to-plan skill)

#### Execution
Run `/dogfood full`. Already covered by V9 (Ralph-executed per v1.3 sponsor mandate).

#### Acceptance Criteria
- [ ] Health score ≥ 70/100 (V9)
- [ ] Zero CRITICAL issues introduced
- [ ] Zero HIGH issues in modified surface

#### If Dogfood Fails
1. Identify which issues are caused by sqlc migration vs pre-existing
2. Fix all plan-caused issues before declaring complete
3. Re-run /dogfood full
4. Pre-existing issues logged, do NOT block

---

## Coverage Matrix

| # | Gap / Requirement (MEET-2026-05-29-001) | Task(s) | Resolution |
|---|---|---|---|
| 1 | M-1 Tenant-owned-tables list + AST gate | T0.5 | tenant_tables.go + tenantpredicate gate |
| 2 | M-2 AST gate for tenant-predicate (security veto) | T0.5 | tenantpredicate gate ships in Phase 0 |
| 3 | M-3 Audit-log call-site preservation script | T1.5 | audit-callsite-diff.sh + zero loss evidence |
| 4 | M-4 Pair-review on 5 sensitive stores | T1.4 (tenant), T1.5 (audit), T2.2 (cell, bootstrap), T2.3 (secret) | Pair-review commit signatures |
| 5 | M-5 CI replay role non-superuser + sanitization | T0.2 | render script uses non-superuser; strips ALTER OWNER |
| 6 | M-6 Rollback playbook committed | (already done) | ADR-3 committed pre-Phase-0 |
| 7 | M-7 Drill iter 56 GREEN before merge | V8 | Ralph-executed per v1.3 sponsor mandate (multi-Bash drill:state-reset → drill:provision → drill:helmfile-bootstrap → drill:l2-scale → evidence-capture → teardown-validator) |
| 8 | M-8 Sprint 9 schedule deconflict (operator) | (D1 resolved IRRELEVANT) | No trial scheduled |
| 9 | M-9 NULL-handling parity tests | V3 | per-field parity tests |
| 10 | M-10 Two-step delete of schema_mirror_test | T4.2 | sqlcsync green → delete in separate commit |
| 11 | M-11 sqlc emits tx-required form | T0.1 | sqlc.yaml emit_methods_with_db_argument: true |
| 12 | M-12 sqlctxprop gate verifies tx propagation | T0.4 | sqlctxprop gate ships in Phase 0 |
| 13 | M-13 OQ #7 RESOLVED-BY-CODE-READ documented | T0.1 (sqlc.yaml comment) + ADR-1 | Already in ADR-1 |
| 14 | M-14 Test reshape matrix | T0.6 | ADR-1 Appendix A |
| 15 | M-15 Owner daily diary + sponsor checkpoint | T0.9, T1.6, T2.x, T3.5, T4.3 (after each phase) | Daily entries by Paulo |
| 16 | M-16 pkg/quality/sqlcsync gate + register | T0.3 | sqlcsync gate ships in Phase 0 |
| 17 | M-17 Render script allowlisted | T0.2 (Task 1) | shell-scripts-allowlist.md entry |
| 18 | M-18 golang-migrate version pinned | T0.2 | pin in render script + prod migrator |
| 19 | M-19 PG 16 + sqlc CLI version pinned | T0.2 + dev-loop.md | pins in CI render + dev-loop docs |
| 20 | M-20 quality:gates execution-time budget | T0.8 | measured pre/post in CI logs |
| 21 | M-21 Production-snapshot smoke + chaos-test full | V10 | Ralph-executed per sponsor mandate v1.3 |
| 22 | M-22 Operator import-graph audit | T0.7 | ADR-1 Appendix B |
| 23 | M-23 Raw subpackage + AST exclusion | T4.1 | raw/ + sensitive_tables.go + extended gate |
| 24 | M-24 State-machine wrapper pattern | T1.1, T1.2, T1.3, T1.4, T2.x | thin Go wrappers per D4 |
| 25 | V1 sqlc codegen reproducibility | V1 | 3-run byte-identical |
| 26 | V2 schema.gen.sql replay determinism | V2 | 3-run byte-identical |
| 27 | V3 NULL-handling parity | V3 | per-field parity tests |
| 28 | V4 Tenant-predicate AST | V4 | tenantpredicate gate CI |
| 29 | V5 Audit-log call-site preservation | V5 | diff script empty |
| 30 | V6 Advisory-lock tx propagation | V6 | sqlctxprop gate CI |
| 31 | V7 Behavioral invariant tests | V7 | KEEP-INVARIANT tests GREEN |
| 32 | V8 Drill iter 56 LIVE | V8 | Ralph-executed (sponsor mandate v1.3) |
| 33 | V9 Dogfood full + cleanroom-fresh | V9 | Ralph-executed (precedent: MEET-001 closure 2026-05-29) |
| 34 | V10 Production-snapshot smoke + chaos-test full | V10 | Ralph-executed (precedent: chaos-test full 10/10 PASS 2026-05-29) |
| 35 | V11 7-day observation | V11 | operator wall-clock (post-merge ongoing) |

**Coverage: 35/35 (100%)**

## Global Definition of Done

- [ ] All phases (0, 1, 2, 3, 4, Final) completed
- [ ] All 25 tasks (T0.1-T0.9, T1.1-T1.6, T2.1-T2.4, T3.1-T3.5, T4.1-T4.3) have DoD met
- [ ] All 11 validation phases V1-V11 GREEN (V8/V9/V10 Ralph-executed per v1.3 sponsor mandate; V11 operator wall-clock)
- [ ] All 24 mandatory actions M-1..M-24 closed with evidence
- [ ] task test passes (api/ + operators/ + pkg/)
- [ ] task lint passes
- [ ] task validate:manifests passes
- [ ] task quality:gates passes (13 gates including 3 new)
- [ ] task dogfood:full GREEN (V9)
- [ ] Drill iter 56 GREEN (V8, Ralph-executed, evidence audit committed)
- [ ] Production-snapshot smoke + chaos-test full 10/10 GREEN (V10, Ralph-executed, evidence audits committed)
- [ ] CHANGELOG.md [Unreleased] entries: Added (sqlc, 3 gates, render pipeline, raw/), Changed (db layer, services consumers), Removed (44 hand-rolled *_store.go, schema_mirror_test.go)
- [ ] All operator binaries (4) build and pass tests against new db/
- [ ] No internal terms in CLI output (Rule 3 — unchanged surface but re-verified)
- [ ] withEnvironmentLock invariant preserved on every mutating route (sqlctxprop gate enforces)
- [ ] No pg_advisory_lock( (session-scoped) introduced (advisorylock gate enforces)
- [ ] State machine Transition()-only discipline preserved (D4 wrappers; state-guardian verification)
- [ ] Sponsor checkpoint signed off (M-15 daily diary clean across migration window)
- [ ] PR description references MEET-2026-05-29-001 + ADR-1 + ADR-2 + ADR-3
- [ ] Plan-specific runtime-metric proof: zero "implemented but not wired" tests (sqlctxprop + tenantpredicate gates run on every CI build; verified non-zero gate evaluations in CI logs)

---

## Final Phase: Dogfood QA (MANDATORY per to-plan skill)

> This phase runs AFTER all implementation phases are complete. Already covered by V9 + V10 above.

**Objective:** Validate that the implemented changes work as a real user would experience them, not just as unit tests assert.

### Execution

Run `/dogfood full`. Always full. No shortcuts. Already Ralph-executed in V9 per v1.3 sponsor mandate.

### Acceptance Criteria

- [ ] Health score >= 70/100
- [ ] Zero CRITICAL issues introduced by this plan's changes
- [ ] Zero HIGH issues in commands/features modified by this plan
- [ ] Any pre-existing issues documented (not caused by this plan)

### If Dogfood Fails

1. Identify which issues are caused by this plan's changes vs pre-existing
2. Fix all plan-caused CRITICAL and HIGH issues before declaring the plan complete
3. Re-run `/dogfood full` to confirm fixes
4. Pre-existing issues are logged but do NOT block plan completion

---

## Appendix C — Edge Case Fold Log (v1.0 → v1.1)

> Folded from `/edge-case-plan 2026-05-29-sqlc-migration` review report dated 2026-05-29. All 5 MUST FIX items absorbed into the affected tasks; 1 bonus SHOULD TEST (EC-17 port conflict) folded with EC-4. The 11 remaining SHOULD TEST + 7 DOCUMENT items are captured here for owner reference during execution (not auto-applied — operator decides per-batch whether to fold).

### MUST FIX folded into plan v1.1

| EC | Family | Task | Change applied |
|---|---|---|---|
| **EC-1** | Resource (tooling) | T0.1 | Added Task #6 + 2 RED tests + 1 acceptance criterion: `task doctor` enforces sqlc v1.31.1 presence in Required Tools |
| **EC-2** | Format (DDL parsing) | T0.2 | Added Task #7 + step 8 in render algorithm + 1 RED test + 1 acceptance criterion: `sqlc compile` pre-flight against rendered schema.gen.sql + dummy queries per existing table; gaps documented in ADR-1 §"Known sqlc limitations" |
| **EC-3** | Integração (Rule 5) | T0.1 | sqlc.yaml v2 config gains `emit_json_tags: true` + `json_tags_case_style: snake` + 1 RED test + 1 acceptance criterion |
| **EC-4** | Resource (ephemeral cleanup) | T0.2 | Render algorithm step 0: `trap '...' EXIT INT TERM` for guaranteed PG container teardown on script abort or SIGINT; + 1 RED test + 1 acceptance criterion |
| **EC-5** | Timing (sequence enforcement) | T4.2 | T4.2 acceptance + DoD gain explicit "substantive coverage" attestation: green CI cycle must include ≥30 queries + ≥30 generated files + ≤14 remaining stores; + 3 RED tests + attestation script as deliverable |

### Bonus SHOULD TEST folded with EC-4

| EC | Family | Task | Change applied |
|---|---|---|---|
| **EC-17** | Resource (port conflict) | T0.2 | Render algorithm step 1 uses `--publish 0:5432` (random host port); + 1 RED test + 1 acceptance criterion (folded along with EC-4 since same render script) |

### SHOULD TEST not auto-folded (owner discretion during execution)

These are good-practice tests that owner should add inline during the relevant phase. Each is a candidate for inclusion in TDD lists but not blocking acceptance:

- EC-6: Concurrent Phase 1 conversions `generated/` regeneration test (during T1.x)
- EC-7: Wrapper recursion same-tx test (during T1.1, T1.4)
- EC-8: CHANGELOG.md categorized entry consistency test (during T2.x batches)
- EC-9: Phase 1 sponsor pattern-review sign-off documentation (T1.6 DoD)
- EC-10: `task quality:gates` execution-time fail-threshold (during T0.8 / M-20 measurement)
- EC-11: `tenant_tables.go` staleness CI guard (during T0.5)
- EC-12: Test helper orphan detection post-reshape (during T0.6 + T1.x reshape)
- EC-13: Operator binary transitive-import CI build matrix (during T0.7 / Phase 3 prep)
- EC-14: Generated code goimports formatting test (during T0.3)
- EC-15: Per-phase commit count on feature branch (T0.9, T1.6, T2.x phase sanity)
- EC-16: Phase 2 batch order foreign-key dependency note (T2.x ordering docs)

### DOCUMENT items (acceptable risks)

- EC-18: Line-ending drift cross-OS — LoopZero is Linux/macOS-only per CLAUDE.md
- EC-19: Empty queries.sql — sqlc handles gracefully
- EC-20: Docker-in-CI requirement — pre-existing dependency
- EC-21: State-machine `Transition()` panic in non-handler goroutine — controller-runtime recovers + re-queues
- EC-22: Drill iter 56 cost surprise > 4h — drill stack ~$0.32/hr + watchdog cron destroys after 6h
- EC-23: 14d branch lifetime cap tight vs Phase 2 ~12d estimate — M-15 escalation handles
- EC-24: ADR-3 references squash-merge SHA not yet known — operator fills SHA at revert time per ADR-3 §"Mecanismo"

### Veredict on edge case review

Plan v1.1 incorporates all 5 MUST FIX from the edge case review. SHOULD TEST items are documented for owner-discretion folding during the relevant phase. DOCUMENT items are explicit risk acceptances.

**Edge case coverage after fold:** 24/24 items addressed (5 MUST FIX = fixed in plan, 12 SHOULD TEST = documented for inline test addition, 7 DOCUMENT = risk acceptance). Plan v1.1 ready for Phase 0 implementation.
