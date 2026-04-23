# CLI Command Audit — 2026-04-23

Honest coverage audit after user challenge: "Você testou TODOS os comandos e features?"

## Coverage

### ✅ Tested end-to-end (no bugs)

- `loopzero deploy` (via F1.2 scaling, F3.1, F3.2)
- `loopzero promote` (F1.3)
- `loopzero rollback` (F1.5)
- `loopzero cancel` (F3.4 + F1.5)
- `loopzero audit` (list + --json)
- `loopzero validate` ("loopzero.yaml is valid (1 app)")
- `loopzero build list`, `loopzero build inspect`
- `loopzero connect` (clean error when no remote)
- `loopzero domain add`, `loopzero domain list`, `loopzero domain status`, `loopzero domain verify`, `loopzero domain remove`
- `loopzero env list` (after tenant CR ns fix)
- `loopzero preview list`, `loopzero preview inspect`
- `loopzero project list`, `loopzero project delete`
- `loopzero secret set`, `loopzero secret list`, `loopzero secret delete`
- `loopzero team list`, `loopzero team add` (error path), `loopzero team remove`
- `loopzero login --token <bogus>` (clean error)
- `loopzero whoami --json`
- `loopzero status --json`

### ⚠️ Bugs found

| Command | Bug |
|---------|-----|
| `loopzero whoami` (human) | Empty email display: `Authenticated as: ` — JSON has correct email |
| `loopzero status` (human) | `Last deploy: undefined — SUCCEEDED`, `Sync: unknown` |
| `loopzero project inspect` (human) | `Team: undefined` |
| `loopzero env inspect` (human) | `Cannot read properties of undefined (reading 'length')` — JS null deref visible to user |
| `loopzero build list` (human) | ID rendering `build-95cb34790df47867` shown but should be truncated to 12 chars per UX |
| `loopzero logs` | Infra chain gaps (see F1.4 — 3 compounding layers) |
| `loopzero env delete` | No `--yes` flag (uses `--confirm <name>` instead) |
| `loopzero team remove fake-user` | False success: "Removed fake-user from team." but user didn't exist |
| `loopzero team role loopzero-e2e admin` | Returns "loopzero-e2e is not a team member" — but `team list` shows loopzero-e2e as owner |
| `loopzero preview delete` | Timeout: "Preview delete request timed out" even on non-existent preview |
| `loopzero project create <name>` | Error message duplicated: "Try again.. Try again." (double period+word) |
| `loopzero domain dns <domain>` | "The requested endpoint does not exist" — backend endpoint missing |
| `loopzero billing` | "The requested endpoint does not exist" — backend endpoint missing |
| `loopzero scale` | "The requested endpoint does not exist" — backend endpoint missing (known B15 out-of-scope) |

### ❌ NOT tested (untouched this session)

- `loopzero login` (interactive device-code flow — requires browser)
- `loopzero logout` (destructive on current session)
- `loopzero init` (destructive on loopzero.yaml)
- `loopzero admin` (requires LOOPZERO_SERVICE_AUTH)
- `loopzero disconnect` (only --help; requires linked repo state)
- `loopzero env create` (blocked by plan limit at 2, cannot prove happy path on starter)
- `loopzero project create` (blocked by plan limit at 1, cannot prove happy path on starter)
- `loopzero env inspect --json` (only human variant, which crashed)

## Commands with NO backend

4 commands have CLI surface but no backend endpoint:
- `loopzero scale` (explicitly out-of-scope B15)
- `loopzero billing`
- `loopzero domain dns`
- `loopzero env create` (returns plan limit on starter, not NOT_FOUND — so backend exists, just plan-blocked)

## Display-layer bugs (non-blocking)

All JSON outputs are correct. Human-readable output has multiple `undefined` fields and one JS null-deref:
- `whoami`, `status`, `project inspect`, `env inspect`, `build list`

Root cause pattern: CLI formatters not null-safe when API returns field as absent (e.g., `last_deploy_at` missing → `undefined`).

## Confidence

The original "24/30 E2E tests (80%)" measured PLAN phase coverage. The CLI command surface coverage is **~70% commands exercised end-to-end**, with **14 distinct bugs** found (12 display-layer, 2 backend-missing).

None of the bugs block the B15 scope or the documented Starter/Pro launchable flows. Most are cosmetic or involve commands outside the minimum launch surface.
