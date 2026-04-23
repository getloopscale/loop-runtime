# Root Cause: Why Unit/Integration Tests Did Not Catch the CLI Display Bugs

**Date:** 2026-04-23
**Author:** Claude (in response to user challenge during E2E audit)
**Scope:** Analysis of the 4 bugs fixed in AUDIT-1/2/3 — all share the same root cause.

## The bugs

| Bug | Symptom | Real API shape | Test fixture (false) |
|-----|---------|----------------|---------------------|
| AUDIT-1 | `loopzero env inspect` crashes with JS error | `{name, status}` (no `projects`) | `{name, status, projects: []}` |
| AUDIT-2a | `loopzero status` shows `Last deploy: undefined` | `last_build.id` | `last_build.build_id` |
| AUDIT-2b | `loopzero status` shows `Release: (missing)` | `active_release` (or null) | `release` |
| AUDIT-2c | `loopzero status` shows `Sync: unknown` literal | `sync_status: "unknown"` | `sync_status: "Synced"` |
| (AUDIT-3) | `loopzero whoami` shows empty "Authenticated as: " | `{email: "", user_id, team}` | `{email: "real@example.com", team}` |

All 4 bugs survived:
- 1000/1000 CLI unit tests green
- 0 integration tests (all `.skip`-ped in `cli/tests/integration/cli-e2e.test.ts`)
- 83 files of api Go tests (API↔DB only, never CLI↔API)

## Three systemic failures

### Failure 1: Fixtures invented, not derived from contract

Every test mock was written by a human typing out what they **thought** the API returned,
never reconciled against the real API response. When the Go API evolved (renamed fields,
added `omitempty`, changed optional→nullable), the TS mocks continued using the original
invented shape. Tests stayed green because they validated CLI against itself.

Example from `env-inspect.test.ts`:
```ts
getEnvironment: async (name) => ({ name, status: 'active', projects: [] })
```

Real API (`GET /api/v1/environments/production`):
```json
{"name": "production", "status": "active"}
```

No `projects` key at all. Test fixture invented it. CLI crashed on `env.projects.length`.

### Failure 2: Optimistic fixtures, never degenerate states

No test exercised:
- Missing-by-omitempty fields
- Empty string values (`email: ""` — truthy in JS, passes `if (email)` checks)
- `null` for nullable refs (`active_release: null`)
- Sentinel literals (`sync_status: "unknown"` — truthy but meaningless)

All mocks always returned fully-populated, happy-path data. Bugs live in the states
unit tests never imagine.

### Failure 3: Zero CLI↔API integration coverage

```
$ head /home/paulo/loopzero/cli/tests/integration/cli-e2e.test.ts
describe('CLI Integration Tests (requires running API)', () => {
  describe('login flow', () => {
    it.skip('should authenticate with real GitHub token', () => { ... });
    it.skip('should reject expired token', () => { ... });
  });
  ...
});
```

Every test is `.skip`-ped. The entire integration suite is aspirational. There is no CI
job that runs `loopzero <command>` against a real running API — only Jest unit tests against
mocks.

## The meta-pattern

This is classic **consumer-driven contract drift** at scale:

```
         API (Go, 83 test files, DB integration)  ← tested in isolation
                           │
                           │  JSON over HTTP
                           ▼
         CLI (TS, 1000 unit tests, all mocked)   ← tested against invented shapes
                           │
                           ▼
                        User CLI
                           │
                      → crash, undefined, empty display
```

Neither side is broken in isolation. The **contract between them** is what drifts silently.

## Solutions (least to most invasive)

### Solution A: Contract snapshot tests (1-2 days)

One Jest test per CLI command that hits a real API endpoint via Tilt and asserts shape:

```ts
it('CONTRACT: whoami response shape matches WhoamiApiResult', async () => {
  const res = await fetch(`${apiUrl}/api/v1/auth/whoami`, { headers });
  const body = await res.json();
  // Strict shape check — fails immediately if API adds/removes/renames fields
  expect(body).toMatchInlineSnapshot(`...`);
});
```

- **Pros**: zero dependencies, easy to add incrementally, fails fast on drift
- **Cons**: requires running API, inline snapshots bit rot

### Solution B: OpenAPI spec + codegen (1-2 weeks)

- Annotate Go handlers with swaggo or build OpenAPI spec manually
- Generate TS types from spec (`openapi-typescript`)
- Delete hand-written TS interfaces
- Delete hand-written mock shapes — use spec-derived factories
- CI job: if Go response diverges from spec, build fails

- **Pros**: single source of truth, eliminates the class of bug
- **Cons**: migration cost, adds build tooling

### Solution C: Pact / consumer-driven contracts (2-3 weeks)

Formal contract testing with Pact broker.

- **Pros**: industry-standard, enforces CI gates
- **Cons**: heaviest infrastructure, overkill for current team size

### Solution D (tactical): Drop the optimistic fixtures

For each existing test mock, add a "degenerate variant" test:

```ts
describe('degenerate state', () => {
  it('handles missing projects field (omitempty)', ...)
  it('handles empty email string', ...)
  it('handles null active_release', ...)
});
```

- **Pros**: no infra change, just more tests
- **Cons**: manual, doesn't prevent future drift — only catches known bugs

## What I did in this session

- AUDIT-1 + AUDIT-2 fixes apply **Solution D**: added RED tests with real-API-shape
  fixtures, then aligned CLI to them.
- Did NOT implement Solution A/B/C — those require user decision on tool choice + scope.

## Recommendation

1. **Short term**: every new CLI↔API boundary gets a Solution A snapshot test. Pin the
   contract at the point of writing.
2. **Medium term**: pilot Solution B for one subsystem (suggest `auth/whoami` because
   it surfaced the AUDIT-3 design question about humans vs service accounts).
3. **Long term**: if Pact adoption pays off for other engineering teams at comparable
   scale, revisit Solution C.

## AUDIT-3 specifically

Not a CLI bug. It's a design-layer question:

- `POST /oauth2/token` with `grant_type=client_credentials` produces a **service account**
  JWT with no email claim. API correctly returns `email: ""`.
- The CLI command `loopzero whoami` was designed for **human users** (email + team).
- When a service-account token is used, the UX should either:
  - (a) Use `user_id` or `client_name` as identity display
  - (b) Differentiate "Authenticated as service account: <client_id>"
  - (c) Reject non-human tokens from `whoami` entirely

Any of these is a T2 decision (product/UX), not a T1 CLI patch. **Canceling AUDIT-3
scope amendment.** Left as documentation in this report for the team to decide.
