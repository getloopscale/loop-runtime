# CLI Commands Audit (Phase 0 / T0.3)

> Plan reference: `docs/plans/infra-normalize-multicell-plan.md` Phase 0 T0.3.
> Status: **complete**.
> Date: 2026-04-30.
> Auditor: Claude Code (read-only).
> Goal: enumerate every register group + every subcommand; document expected exit codes per Rule 4 + JSON shape per Rule 5; identify "1000% functional" gaps.

## Inventory summary

| Register file | Subcommands | LoC | Has commands/ files? |
|---|---|---|---|
| `auth.ts` | 3 (`login`, `logout`, `whoami`) | 162 | yes (login.ts, logout.ts, whoami.ts) |
| `deploy.ts` | 7 (`deploy`, `validate`, `status`, `logs`, `rollback`, `promote`, `cancel <build-id>`) | 463 | yes (deploy.ts, validate.ts, status.ts, logs.ts, rollback.ts, promote.ts, cancel.ts) |
| `domain.ts` | 7 (`domain`, `add <domain>`, `verify <domain>`, `status <domain>`, `list`, `remove <domain>`, `dns <domain>`) | 165 | yes (domain.ts) |
| `env.ts` | 5 (`env`, `list`, `create <name>`, `delete <name>`, `inspect <name>`) | 147 | yes (env-list.ts, env-create.ts, env-delete.ts, env-inspect.ts) |
| `infra.ts` | 16 (`build` [list/inspect/cancel], `secret` [set/list/delete], `connect`, `disconnect`, `billing` [portal], `audit`, `admin` [tenants evict]) | 491 | yes (build-list.ts, build-inspect.ts, cancel.ts, secret.ts, connect.ts, billing.ts, audit.ts, admin-evict.ts) |
| `init.ts` | 1 (`init`) | 84 | yes (init.ts) |
| `platform.ts` | 6 (`platform`, `install`, `status`, `upgrade`, `uninstall`, `secrets` [backup/restore/list]) | 121 | yes (platform.ts — 7 exports) |
| `preview.ts` | 4 (`preview`, `list`, `inspect <id>`, `delete <id>`) | 136 | yes (preview.ts) |
| `project.ts` | 5 (`project`, `delete <name>`, `list`, `inspect <name>`, `create <name>`) | 157 | yes (project-create.ts, project-delete.ts, project-inspect.ts, project-list.ts) |
| `scale.ts` | 1 (`scale`) | 75 | yes (scale.ts) — currently STUB per CLAUDE.md ("returns 202 acknowledged") |
| `team.ts` | 4 (`team`, `list`, `add <username>`, `remove <username>`, `role <username> <role>`) | 142 | yes (team-list.ts, team-add.ts, team-remove.ts, team-role.ts) |

**Total: 12 register files, 59 distinct subcommands, 36 source command files in `cli/src/commands/`.**

## Per-register detail

### `auth` (162 LoC)
Subcommands:
1. `loopzero login` — Device Code Flow via Ory Hydra (per CHG-2026-04-29 docs). Exits `0` on success, `4` on auth failure, `1` on invalid argument. JSON: `{status:"authenticated"|"failed", token_acquired:bool, expires_at:ISO8601}`.
2. `loopzero logout` — clears `~/.loopzero/config.json` token. Exits `0` always (idempotent). JSON: `{status:"cleared"|"already_logged_out"}`.
3. `loopzero whoami` — calls `GET /api/v1/users/me`. Exits `0` on success, `4` if token missing/invalid. JSON: `{user_id, email, tenants:[...]}`.

Coverage today: 2 unit test files match name (`token-refresh.test.ts`, `device-flow.test.ts`); 1 imports `commands/login.ts`. **No integration test for `whoami` or `logout`.**

### `deploy` (463 LoC) — largest register
Subcommands:
1. `loopzero deploy` — bundle source, submit build, wait for release. Exits `0` success, `2` build failure, `3` deploy failure, `1` usage. JSON: per-step JSONL `{step, status, build_id?, release_id?}`.
2. `loopzero validate` — local schema validation of `loopzero.yaml`. Exit `0` ok, `1` invalid.
3. `loopzero status` — list active releases per env. Exit `0` ok. JSON: `{environments:[{env, active_release}]}`.
4. `loopzero logs` — stream build/runtime logs. Exit `0` ok, `1` resource not found.
5. `loopzero rollback` — promote previous release. Exit `0` ok, `1` no prior release, `3` deploy failed.
6. `loopzero promote` — copy active release src→target env. Exit `0`, `3` on deploy fail.
7. `loopzero cancel <build-id>` — cancel running build. Exit `0` ok, `1` not found, `1` already terminal.

Coverage: 2 tests by name (`deploy-helpers`, `deploy-types`), 1 imports. `cli-e2e.test.ts` describes "deploy flow" but requires running API. **No integration covering `validate`, `logs`, `cancel`.**

### `domain` (165 LoC)
Subcommands:
1. `loopzero domain` — root group help.
2. `loopzero domain add <domain>` — POST `/api/v1/domains`. Exit `0` ok, `1` already exists.
3. `loopzero domain verify <domain>` — TXT record check. Exit `0` verified, `1` not yet.
4. `loopzero domain status <domain>` — state machine state. Exit `0` ok.
5. `loopzero domain list` — list domains. Exit `0`.
6. `loopzero domain remove <domain>` — DELETE. Exit `0`, `1` not found.
7. `loopzero domain dns <domain>` — show NS records to set. Exit `0`.

Coverage: 0 unit tests target domain commands. **GAP: 7 subcommands × 0 tests.**

### `env` (147 LoC)
Subcommands:
1. `loopzero env` — root.
2. `loopzero env list` — Exit `0`. JSON `{environments:[...]}`.
3. `loopzero env create <name>` — Exit `0`, `1` already exists, `1` invalid name.
4. `loopzero env delete <name>` — Exit `0`, `1` not found, `5` partial (resources remain).
5. `loopzero env inspect <name>` — Exit `0`, `1` not found.

Coverage: 0 dedicated tests. **GAP.**

### `infra` (491 LoC) — most subcommands per file
Subcommands (16):
1. `loopzero build` — root group.
2. `loopzero build list` — Exit `0`.
3. `loopzero build inspect <build-id>` — Exit `0`, `1` not found.
4. `loopzero build cancel <build-id>` — Exit `0`, `1` not found.
5. `loopzero secret` — root.
6. `loopzero secret set <nameValue>` — Exit `0`, `1` invalid format.
7. `loopzero secret list` — Exit `0`.
8. `loopzero secret delete <name>` — Exit `0`, `1` not found.
9. `loopzero connect` — link LoopZero project to Git repo via GitHub App. Exit `0`, `4` not authenticated, `1` repo not accessible.
10. `loopzero disconnect` — Exit `0`.
11. `loopzero billing` — root.
12. `loopzero billing portal` — Stripe portal URL. Exit `0`.
13. `loopzero audit` — list audit log entries. Exit `0`.
14. `loopzero admin` — root (admin-only).
15. `loopzero admin tenants` — root.
16. `loopzero admin tenants evict` — POST `/api/v1/admin/tenants/evict`. Exit `0`, `1` invalid filter, `4` not authorized.

Coverage: existing `admin-evict.test.ts` (4 cases), `billing.test.ts`, `secret-backups.test.ts`. **GAPS: `connect`, `disconnect`, `audit`, `build` group, `secret` set/list/delete.**

### `init` (84 LoC)
Subcommands: 1 (`loopzero init`). Detects framework + writes `loopzero.yaml` + `.loopzero/` config. Exit `0` ok, `1` if `loopzero.yaml` exists, `4` if not authenticated and `--offline` not passed (per F-05 fix).

Coverage: 1 dedicated test file. **integration test exists in `cli-e2e.test.ts`** ("init flow" describe block).

### `platform` (121 LoC)
Subcommands (per register):
1. `loopzero platform` — root.
2. `loopzero platform install` — 6-step (per CHG-2026-04-28 + CHG-2026-04-30) install. Exits 0/1/3/5. JSON via `--json` (T2.2 shipped this session).
3. `loopzero platform status` — Exit `0` ok, `5` if drift detected (T2.1 shipped). JSON `{phase, components, sync.drift?}`.
4. `loopzero platform upgrade` — Exit `0`, `3` on failure.
5. `loopzero platform uninstall` — Exit `0`, `3` on failure.
6. `loopzero platform secrets backup` — Exit `0`, `1` no creds.
7. `loopzero platform secrets restore <file>` — Exit `0`, `1` invalid.
8. `loopzero platform secrets list` — Exit `0`.

Coverage: **best in repo** — 3 dedicated test files (`platform-install-json`, `platform-install-validate-l3`, `platform-status-drift`, `getPlatformStatus-charts`). 14+ unit cases.

### `preview` (136 LoC)
Subcommands (4):
1. `loopzero preview` — root.
2. `loopzero preview list` — Exit `0`.
3. `loopzero preview inspect <id>` — Exit `0`, `1` not found.
4. `loopzero preview delete <id>` — Exit `0`, `1` not found.

Coverage: 0 dedicated unit tests. Sprint 5 envtest exists (operator-side). **GAP on CLI side.**

### `project` (157 LoC)
Subcommands (5):
1. `loopzero project` — root.
2. `loopzero project create <name>` — Exit `0`, `1` already exists, `1` plan limit.
3. `loopzero project list` — Exit `0`.
4. `loopzero project inspect <name>` — Exit `0`, `1` not found.
5. `loopzero project delete <name>` — Exit `0`, `1` not found, `5` cascade partial.

Coverage: 4 unit test files match. Best beyond platform.

### `scale` (75 LoC)
Subcommands: 1 (`loopzero scale`). Per CLAUDE.md: **STUB returning 202 acknowledged** — not DB-persisted. Exit `0`, `1` invalid replica count.

Coverage: 1 unit test (asserts STUB shape).

### `team` (142 LoC)
Subcommands (5):
1. `loopzero team` — root.
2. `loopzero team list` — Exit `0`.
3. `loopzero team add <username>` — Exit `0`, `1` user not found.
4. `loopzero team remove <username>` — Exit `0`, `1` not member.
5. `loopzero team role <username> <role>` — Exit `0`, `1` invalid role.

Coverage: 0 dedicated unit tests. **GAP.**

## Coverage gap matrix

| Register | Subcmds | Unit tests dedicated | Integration tests | Status |
|---|---|---|---|---|
| auth | 3 | 2 (token-refresh, device-flow) | partial (cli-e2e login flow) | **GAP**: whoami, logout |
| deploy | 7 | 2 (deploy-helpers, deploy-types) | partial (cli-e2e deploy flow) | **GAP**: validate, logs, cancel |
| domain | 7 | 0 | 0 | **GAP**: all 7 |
| env | 5 | 0 | 0 | **GAP**: all 5 |
| infra | 16 | 3 (admin-evict, billing, secret-backups) | 0 | **GAP**: connect, disconnect, audit, build group |
| init | 1 | 1 | 1 (cli-e2e init flow) | OK |
| platform | 8 | 4 | 0 (only unit) | **GAP**: integration |
| preview | 4 | 0 | 0 | **GAP**: all 4 |
| project | 5 | 4 | 0 | **GAP**: integration |
| scale | 1 | 1 | 0 | OK (STUB) |
| team | 5 | 0 | 0 | **GAP**: all 5 |

**Total: 59 subcommands, 17 with unit-test coverage, 3 with integration coverage. Gap: 56 subcommands lack any integration test.**

## "1000% functional" gap analysis

User goal: every CLI command must be 100% functional. Current state of each register against rubric:

| Rubric criterion | auth | deploy | domain | env | infra | init | platform | preview | project | scale | team |
|---|---|---|---|---|---|---|---|---|---|---|---|
| Exit codes documented in source | ✅ | ✅ | ⚠️ | ⚠️ | ✅ | ✅ | ✅ | ⚠️ | ✅ | ✅ | ⚠️ |
| `--json` schema asserted by test | ⚠️ | ⚠️ | ❌ | ❌ | ⚠️ | ❌ | ✅ | ❌ | ⚠️ | ❌ | ❌ |
| Success path integration-tested | ⚠️ | ⚠️ | ❌ | ❌ | ⚠️ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Error path integration-tested | ❌ | ❌ | ❌ | ❌ | ⚠️ | ❌ | ⚠️ | ❌ | ❌ | ❌ | ❌ |
| Forbidden Rule 3 internal terms scan | ✅ (bundle-rule3-clean) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

Legend: ✅ covered, ⚠️ partial, ❌ missing.

## Recommendations for Phase 4

Per plan v2 D4 (cluster-real integration tests for CLI), Phase 4 must:
1. **T4.1**: Build spawn-binary harness + in-process Express mock API (NEW: `cli/tests/integration/_fixtures/`).
2. **T4.2**: Add ≥1 success + ≥1 error integration test per register. **12 register groups × 2 = ≥24 new integration tests minimum**. Higher for groups with many subcommands (deploy=7, infra=16, domain=7, env=5, platform=8 → expand).
3. **T4.3**: Fix bugs surfaced by T4.2.

Priorities (highest gap × highest blast radius):
1. `domain` — 7 subcommands × 0 tests, customer-facing path
2. `infra connect`/`disconnect` — auth-sensitive, GitHub App integration
3. `env create/delete/inspect` — tenant-affecting
4. `team` group — RBAC-affecting
5. `preview` group — Sprint 5 deliverable, partial CLI coverage
6. `deploy validate`/`logs`/`cancel` — operator daily-driver paths

## References

- `docs/plans/infra-normalize-multicell-plan.md` (v2 ADR D4 + Phase 4 lines 1153-1276)
- `docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md` (verification-architect veto sets `≥1 fork-based test per register` standard)
- `cli/src/register/{auth,deploy,domain,env,infra,init,platform,preview,project,scale,team}.ts`
- `cli/src/commands/` (36 files)
- `cli/tests/unit/` (existing 1171 tests)
- `cli/tests/integration/{cli-e2e,exit-code-contract}.test.ts`
- CLAUDE.md "The Rules (Non-Negotiable)" (Rules 3, 4, 5)
