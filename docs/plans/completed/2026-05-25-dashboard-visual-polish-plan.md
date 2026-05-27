# Dashboard Visual Polish Plan — Post-Validation Findings

> **Slug:** `2026-05-25-dashboard-visual-polish`
> **Status:** DRAFT (awaiting sponsor approval for Phase A bug fixes)
> **Origin:** Visual validation tour 2026-05-25 (commit `2af9133c`,
> mock-layer-enabled, 13/13 routes captured via headless Chrome)
> **Companion to:** Plan `2026-05-24-dashboard-visual-redesign-mockup-aligned-plan.md`
> v1.2 (whose T-H.1 visual smoke was deferred via MEET-ASYNC-001 and
> partially closed by the mock layer this plan extends)
> **Edge-case review:** `docs/plans/2026-05-25-dashboard-visual-polish-edge-cases.md`
> (14 edges integrated below — EC-1..EC-14 inline in AC/TDD/Deep Dive)
> **Plan version:** v1.1 (was v1.0 — bumped 2026-05-25 to integrate
> edge-case review findings; commit `6b9f8a6e`)

---

## Context (what triggered this plan)

The visual validation tour after shipping `@loopzero/ui@0.10.0-next.0`
(commit `c3974557`) + mock layer (commit `2af9133c`) revealed **2
real bugs** + **5 polish issues** that vitest jsdom tests cannot
detect by design (jsdom doesn't render visually; behavioral assertions
only).

The mock layer covers **8/13 routes (62%)** because:
- 5 auth pages (login, register, recovery, verification, verification
  banner unverified state) require either `VITE_MOCK_LOGGED_OUT=true`
  (not yet implemented) OR real Kratos flow JSON fixtures

This plan closes both: fixes the visible bugs and extends the mock
layer to 13/13 coverage.

### Scope boundaries (what this plan does NOT do)

- Does NOT change @loopzero/ui upstream primitives (those are Brief #5/#6 territory)
- Does NOT add Storybook / Chromatic / Playwright visual regression
  (deferred to MEET-003 Lighthouse CI follow-up plan)
- Does NOT touch the JWT/Kratos/CLAUDE.md "NÃO TOCAR" boundaries
  beyond the existing top-of-function mock guards
- Does NOT renegotiate any DoD/threshold from MEET-001/MEET-002
  (those amendments stand)

---

## Findings inventory (consolidated)

### Bugs (require code fix, blocking proper visual fidelity)

| ID | Severity | Surface | Root cause hypothesis |
|----|----------|---------|----------------------|
| **B-1** | 🔴 HIGH | TopBar in 5 routes (`/`, `/projects`, `/environments`, `/domains`, `/team`) renders project list as inline text instead of Select dropdown | `top-bar.tsx` passes `<option>` children directly to `<Select>` without `<Select.Trigger>` wrapper — @loopzero/ui Select expects compound structure |
| **B-2** | 🟠 MEDIUM | Overview usage card shows `0 / 100 GB` hardcoded instead of fixture values | `overview.tsx` doesn't call `useCloudApi<BillingResponse>('/v1/billing')`; UsageMeter props hardcoded for "shape demo" |

### Polish (cross-cutting consistency)

| ID | Severity | Surface | What's wrong |
|----|----------|---------|--------------|
| **P-1** | 🟡 MEDIUM | Date format drift across 4 surfaces | Billing: `DD/MM/YYYY`. Domains: `DD/MM/YYYY` ("Verified 10/04/2026"). Overview: ISO `2026-05-25T06:42:00Z`. Team: ISO short `2026-01-15`. Three formats for one concept. |
| **P-2** | 🟡 MEDIUM | Number formatting (no separators) | Billing Edge Requests: `1240000 / 10000000`. Should be `1.24M / 10M` or `1,240,000 / 10,000,000`. |
| **P-3** | 🟢 LOW | Email mask aggressiveness | Profile: `demo@loopzero.example` → `de***@us***.dev`. Domain ofuscado mata sinal "que conta estou logado". |
| **P-4** | 🟢 LOW | Environment status capitalization | Environments table: `live`/`building`/`failed` lowercase. Domains: `Active`/`Pending DNS` capitalized. Inconsistent. |
| **P-5** | 🟢 LOW | Avatar shape verification | Profile avatar fallback "D" — visually appears slightly square at 1440px. Possibly `rounded-md` instead of `rounded-full`. Verify + fix if needed. |

### Mock coverage gap (not a bug, deferred from Phase 1)

| ID | Surface | What's missing |
|----|---------|----------------|
| **M-1** | Mock layer cannot render auth pages because mock session is always `active: true` | Need `VITE_MOCK_LOGGED_OUT=true` flag that makes `getMockSession()` return `null` while keeping API fixtures active |
| **M-2** | Mock layer cannot render Kratos flow forms | `recovery.tsx` + `verification.tsx` + `login.tsx` + `register.tsx` fetch flow JSON from `/self-service/{flow}/browser` — need mock fixtures for those endpoints OR mock the `kratos.ts` helpers |

---

## Phase A — Critical bug fixes (BLOCKING for visual signoff)

**Objective:** Restore TopBar + Overview to mockup-aligned state. These
are the 2 visible bugs that make the dashboard look "wrong" at first
glance.

### T-A.1 — Fix TopBar project selector rendering

#### Files to edit

```
cloud/dashboard/src/components/layout/top-bar.tsx — fix Select compound structure
cloud/dashboard/src/components/layout/top-bar.test.tsx — add regression test
```

#### Deep dive

Current rendered HTML (from screenshot tour):

```
<header>
  All Projects | acme-web | acme-payments | acme-marketing |
  acme-internal-tools | acme-experiments | Overview
</header>
```

Expected: dropdown trigger button labeled "All Projects" with caret
icon; click opens popover listing the 5 projects + "All Projects"
sentinel. Pattern from `@loopzero/ui` Select (Brief #1):

```tsx
<Select value={selected} onValueChange={setSelected}>
  <Select.Trigger aria-label="Select project">
    <Select.Value placeholder="All Projects" />
  </Select.Trigger>
  <Select.Content>
    <Select.Item value="all">All Projects</Select.Item>
    {projects.map(p => (
      <Select.Item key={p.name} value={p.name}>{p.name}</Select.Item>
    ))}
  </Select.Content>
</Select>
```

Verify exact compound API by reading `node_modules/@loopzero/ui/dist/primitives/select/`
or `.llms.txt` line ~150-200.

**EC-12 note (DOCUMENT):** `@loopzero/ui` Select.Item handles long-name
overflow natively via CSS `text-overflow: ellipsis`. Project names ≤63
chars (K8s limit) may visually truncate as `acme-very-long-project-...`
in the popover — this is design-intended, not a bug. No action.

#### TDD

```
RED:     test_top_bar_select_renders_as_dropdown_trigger_not_inline_list
RED:     test_top_bar_select_handles_empty_projects                       (EC-4)
RED:     test_top_bar_select_handles_long_project_name                    (EC-11)
GREEN:   Wrap Select with Trigger + Content + Item compound structure
VERIFY:  npm test -- top-bar && headless Chrome screenshot diff
```

Assertions:
- `test_top_bar_select_renders_as_dropdown_trigger_not_inline_list` —
  `screen.getByRole('combobox', { name: /select project/i })` exists
  AND `screen.queryAllByText(/acme-/)` returns at most 1
  (currently 5 because every project name renders as text node).
- `test_top_bar_select_handles_empty_projects` (EC-4) — mock
  `useProjects()` returning `{projects: [], loading: false}`, assert
  Select trigger present + popover only shows "All Projects" item
  (no `acme-*` items).
- `test_top_bar_select_handles_long_project_name` (EC-11) — fixture
  with project `name: 'this-is-a-very-long-project-name-near-k8s-limit-of-63-chars'`,
  assert Select.Item renders + has CSS ellipsis class
  (`overflow-hidden` / `truncate` on Tailwind).

#### Acceptance criteria

- [ ] TopBar in `/`, `/projects`, `/environments`, `/domains`, `/team` shows ONE pill labeled "All Projects" + caret, not 5 inline project names
- [ ] Click on pill opens popover listing 5 projects + "All Projects" sentinel
- [ ] Selecting a project updates the URL or filter (preserve existing onValueChange behavior if any)
- [ ] Existing 5 top-bar.test.tsx tests still pass
- [ ] +3 regression tests (compound structure + empty + long name per EC-4/EC-11)

#### DoD

- [ ] Commit `fix(dashboard): T-A.1 TopBar Select compound structure (B-1, EC-4, EC-11)`
- [ ] Re-screenshot `/` + `/projects` + `/team` and link in commit message
- [ ] All 94+ vitest tests pass

---

### T-A.2 — Wire Overview usage card to /v1/billing

#### Files to edit

```
cloud/dashboard/src/pages/overview.tsx — add useCloudApi<BillingResponse>('/v1/billing')
cloud/dashboard/src/pages/overview.test.tsx — add fixture for /v1/billing call
```

#### Deep dive

Current behavior: Overview UsageMeter renders `0 / 100 GB` for Fast
Data Transfer regardless of billing data. The `/v1/billing` endpoint
IS being called by Billing page and returns the same shape Overview
needs.

Fix: in `OverviewPage`, add:

```tsx
const { data: billing } = useCloudApi<BillingResponse>('/v1/billing');
const usage = billing?.usage ?? {};

// In the UsageMeter section:
<UsageMeter
  label="Fast Data Transfer"
  used={usage['Fast Data Transfer']?.used ?? 0}
  limit={usage['Fast Data Transfer']?.limit ?? 100}
  unit="GB"
/>
```

Repeat for "Fast Origin Transfer" + "Edge Requests" (the 3 metrics
the Overview shows).

#### Edge case

If `/v1/billing` returns null (network error or new account), fall
back to `0 / limit` per current behavior. Don't break Overview if
billing is unreachable.

#### TDD

```
RED:     test_overview_usage_reflects_billing_data_when_loaded
RED:     test_overview_usage_falls_back_to_zero_when_billing_null
RED:     test_overview_usage_renders_unlimited_when_limit_null            (EC-5)
RED:     test_overview_usage_renders_over_quota                           (EC-6)
GREEN:   Add useCloudApi + thread usage data into UsageMeter props
VERIFY:  npm test -- overview && Chrome screenshot diff
```

Assertions:
- `test_overview_usage_renders_unlimited_when_limit_null` (EC-5) — fixture
  with `usage['Edge Requests']: {used: 5_000_000, limit: null}` (Enterprise
  tier unlimited per CLAUDE.md pricing), assert UsageMeter renders
  `"5M / Unlimited"` or `"5M / ∞"` (convention defined in ADR-D1.bis below).
- `test_overview_usage_renders_over_quota` (EC-6) — fixture `{used: 1200,
  limit: 1000}`, assert UsageMeter shows actual `1.2K / 1K` numeric +
  bar visually clamped at 100% + variant `warning` or `destructive`.

#### ADR-D1.bis — "Unlimited" UsageMeter convention

**Decision:** When `limit === null`, UsageMeter renders
`{formatNumber(used, 'compact')} / Unlimited` with bar variant `success`
(green, no fill — unlimited has no fill semantic).

**Alternatives rejected:**
- `5M / ∞` — symbol confusing for non-technical readers
- Hide limit entirely (`5M used`) — loses parallelism with other rows
- Custom UnlimitedMeter primitive — over-engineering

#### Acceptance criteria

- [ ] Overview usage values reflect `/v1/billing` data when present
- [ ] Falls back to `0 / 100 GB` etc. when billing is null (no regression)
- [ ] `limit: null` renders "Unlimited" per ADR-D1.bis (EC-5)
- [ ] `used > limit` shows actual numeric + 100% clamped bar + warning variant (EC-6)
- [ ] Headless Chrome screenshot of `/` shows `412 / 1000 GB` (matching mock fixture)
- [ ] No new fetch added per page load (use the same SWR-style cache pattern as use-projects.ts if needed)

#### DoD

- [ ] Commit `fix(dashboard): T-A.2 wire Overview UsageMeter to /v1/billing (B-2)`
- [ ] Test fixture updated to provide billing mock
- [ ] All 94+ vitest tests pass

---

## Phase B — Mock layer extension (closes 5 auth pages gap)

**Objective:** Extend `dev-mocks.ts` so VISUAL VALIDATION covers
all 13 routes, not just 8/13.

### T-B.1 — Add VITE_MOCK_LOGGED_OUT flag

#### Files to edit

```
cloud/dashboard/src/lib/dev-mocks.ts — add useMocksLoggedOut() helper
cloud/dashboard/src/lib/auth.ts — wire getSession() to return null when LOGGED_OUT
```

#### Deep dive

Current:

```ts
export function useMocks(): boolean {
  return import.meta.env.VITE_USE_MOCKS === 'true';
}

export function getMockSession(): Session {
  return { active: true, identity: {...} };
}
```

New:

```ts
export function useMocks(): boolean { ... }  // unchanged

export function useMocksLoggedOut(): boolean {
  return useMocks() && import.meta.env.VITE_MOCK_LOGGED_OUT === 'true';
}

export function getMockSession(): Session | null {
  if (useMocksLoggedOut()) return null;
  return { active: true, identity: {...} };
}
```

`getSession()` already calls `getMockSession()` first — no change needed
there; the null propagates correctly through `useRequireAuth`.

#### Acceptance criteria

- [ ] `VITE_USE_MOCKS=true npm run dev` → logged-in flows (current behavior preserved)
- [ ] `VITE_USE_MOCKS=true VITE_MOCK_LOGGED_OUT=true npm run dev` → /login renders the form (not redirect to /)
- [ ] Production build unchanged (env var statically eliminated)

#### DoD

- [ ] Commit `feat(dashboard): T-B.1 VITE_MOCK_LOGGED_OUT flag for auth page visual validation`

---

### T-B.2 — Kratos flow JSON fixtures

#### Files to edit

```
cloud/dashboard/src/lib/dev-mocks.ts — add Kratos flow fixtures
cloud/dashboard/src/lib/kratos.ts — top-of-function mock guards (NÃO TOCAR caveat: guard only, zero logic change)
```

#### Deep dive

Kratos flow endpoints that the 4 auth pages hit via raw `fetch()`:
- `/self-service/login/browser` (login.tsx + register.tsx use this for OIDC csrf)
- `/self-service/login/flows?id=<id>` (login.tsx loads flow by id)
- `/self-service/registration/browser` + `?id=<id>` (register.tsx)
- `/self-service/recovery/browser` + `?id=<id>` (recovery.tsx via lib/kratos.ts)
- `/self-service/verification/browser` + `?id=<id>` (verification.tsx)

Two implementation options:

**Option A — Mock at fetch level** (cleaner but more invasive):
add a global `fetch` interceptor in `dev-mocks.ts` that returns flow
fixtures for `/self-service/*/browser` paths.

**Option B — Mock at lib/kratos.ts level** (matches existing pattern):
add `useMocks()` guards in `getFlow()`, `initFlow()`, `submitFlow()`
that return canned flow JSON.

Recommend **Option B** for consistency with existing api.ts + auth.ts
guards. Estimated ~50 LOC of fixtures (one flow JSON per intent).

Fixtures shape (from existing login.tsx Flow interface):

```ts
const MOCK_FLOWS = {
  login: { id: 'mock-login-flow', ui: { action: '#mock-submit', nodes: [
    { group: 'default', attributes: { name: 'csrf_token', value: 'mock-csrf' } },
    { group: 'password', attributes: { name: 'identifier', type: 'email' } },
    { group: 'password', attributes: { name: 'password', type: 'password' } },
  ], messages: [] } },
  // similar for registration, recovery, verification
};
```

#### Edge case (EC-1, MUST FIX)

**`submitFlow()` mock MUST NOT mutate `window.location.href`** — would
break the dev tour by redirecting the operator to fake URL (`#mock-submit`).
The real `submitFlow()` does `window.location.href = data.redirect_browser_to`
on success; the mock guard MUST return a response object WITHOUT touching
`window`. Concrete implementation:

```ts
// In lib/kratos.ts#submitFlow, BEFORE the real impl:
if (useMocks()) {
  // NEVER set window.location — would break dev tour
  return { state: 'mocked', ui: { messages: [], nodes: [] }, redirect_browser_to: undefined };
}
```

Per EC-7 SHOULD TEST: recovery flow has 2 phases (email → code). Mock
must support BOTH phases via flow state transition. Verification flow
needs `state: 'passed_challenge'` after submit for success-state render.

#### TDD

```
RED:     test_mock_submitflow_never_mutates_window_location              (EC-1)
RED:     test_recovery_mock_supports_phase_transition_email_to_code      (EC-7)
GREEN:   Add useMocks guards in getFlow/initFlow/submitFlow + MOCK_FLOWS map
VERIFY:  npm test -- kratos && manual dev tour 4 auth pages
```

Assertion for EC-1: spy on `Object.getOwnPropertyDescriptor(window.location, 'href').set`,
invoke `submitFlow()` in mock mode, assert spy was NOT called. Concrete
implementation pattern in `login.test.tsx` line ~120 (vanilla DOM
invariant test from T-G.1).

#### Acceptance criteria

- [ ] `VITE_USE_MOCKS=true VITE_MOCK_LOGGED_OUT=true npm run dev` + visit /login → renders email/password form + GitHub + Google buttons
- [ ] Same for /register, /recovery, /verification
- [ ] Submit buttons render visual state but don't actually call backend
- [ ] **Mock `submitFlow` NUNCA toca `window.location.href`**, returns response object only (EC-1)
- [ ] Recovery mock supports both phases (email submit → code phase, code submit → passed_challenge) (EC-7)
- [ ] Verification mock has `state: 'passed_challenge'` available for success-state render (EC-7)
- [ ] No changes to lib/kratos.ts behavior outside the top-of-function guard

#### DoD

- [ ] Commit `feat(dashboard): T-B.2 Kratos flow fixtures for visual validation (M-2, EC-1, EC-7)`

---

### T-B.3 — Re-tour screenshots, 13/13 coverage

#### Files to edit

None (validation only).

#### Workflow

**EC-14 (DOCUMENT):** before running the tour, kill any prior Vite
process holding port 5173 — Vite auto-falls-back to 5174/5175 if busy
and the screenshot script hardcodes 5173, silently producing empty
PNGs. Pre-step: `pgrep -f "vite" | xargs -r kill ; sleep 0.5`.

```bash
cd cloud/dashboard
# EC-14: ensure port 5173 is free before starting
pgrep -f "vite" | xargs -r kill ; sleep 0.5

# Logged-in tour (8 pages)
VITE_USE_MOCKS=true npm run dev &
DEV_PID=$!
sleep 2
# screenshot /, /projects, /environments, /domains, /billing, /team,
# /settings, /profile, /device-success
kill $DEV_PID

# Logged-out tour (5 auth pages)
VITE_USE_MOCKS=true VITE_MOCK_LOGGED_OUT=true npm run dev &
DEV_PID=$!
sleep 2
# screenshot /login, /register, /recovery, /verification
# (verification-banner appears on /, /projects etc. when unverified — verify
#  by setting VITE_MOCK_UNVERIFIED=true too, optional follow-up flag)
kill $DEV_PID
```

#### Acceptance criteria

- [ ] 13 PNG files in `/tmp/dashboard-screenshots/` covering all routes (or save to `docs/reviews/2026-05-25-dashboard-screenshots/`)
- [ ] Side-by-side comparison vs `cloud/dashboard/design/loopzero_overview_dashboard_mockup.html` documented in a review report
- [ ] Report identifies any NEW issues introduced by Phase A fixes (regression detection)

#### DoD

- [ ] Review report committed to `docs/reviews/2026-05-25-dashboard-visual-polish-validation.md`

---

## Phase C — Format consistency helpers

**Objective:** Eliminate the 3-date-formats / 2-number-formats drift.

### T-C.1 — Create lib/format.ts

#### Files to edit

```
cloud/dashboard/src/lib/format.ts (NEW) — formatDate + formatNumber helpers
cloud/dashboard/src/lib/format.test.ts (NEW) — unit tests
```

#### API design (proposed)

```ts
/**
 * Format ISO date string for display per intent.
 * Single source of truth — all pages call this, never inline new Date().toLocaleDateString().
 */
export function formatDate(
  iso: string | undefined,
  intent: 'date' | 'datetime' | 'relative' | 'long',
): string {
  if (!iso) return '—';
  const d = new Date(iso);
  switch (intent) {
    case 'date':     return d.toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' });   // "May 25, 2026"
    case 'datetime': return d.toLocaleString('en-US', { dateStyle: 'medium', timeStyle: 'short' });               // "May 25, 2026, 6:42 AM"
    case 'relative': return relativeTime(d);                                                                       // "2h ago"
    case 'long':     return d.toLocaleDateString('en-US', { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric' }); // "Sunday, May 25, 2026"
  }
}

/**
 * Format large numbers with compact (K/M/B) or grouped separators.
 */
export function formatNumber(
  value: number,
  intent: 'compact' | 'grouped' | 'bytes' | 'percent',
): string {
  switch (intent) {
    case 'compact': return new Intl.NumberFormat('en-US', { notation: 'compact', maximumFractionDigits: 2 }).format(value);  // "1.24M"
    case 'grouped': return new Intl.NumberFormat('en-US').format(value);                                                      // "1,240,000"
    case 'bytes':   return formatBytes(value);                                                                                // "1.5 GB"
    case 'percent': return new Intl.NumberFormat('en-US', { style: 'percent', maximumFractionDigits: 1 }).format(value);     // "42.3%"
  }
}
```

#### Decisions

- **Locale**: `en-US` (matches CLAUDE.md product language convention — dashboard is English-first regardless of user locale; defer i18n to v1.x)
- **Time zone**: implicit user local (don't force UTC display)
- **Null safety**: return `—` (em-dash) for null / undefined / empty string / NaN / Infinity inputs (single defensive guard at function top)
- **Rounding**: max 2 fraction digits for compact + percent (avoids "1.234M")
- **Bytes convention**: SI 1000-base (KB/MB/GB), NOT IEC 1024-base (KiB/MiB/GiB) — see ADR-D4 below

#### ADR-D4 — `formatNumber(value, 'bytes')` uses SI 1000-base

**Decision:** `bytes` intent treats 1 KB = 1000 bytes, 1 MB = 1_000_000
bytes, 1 GB = 1_000_000_000 bytes. Matches `Intl.NumberFormat` style
`'unit'` + `unit: 'megabyte'` natural behavior.

**Context:** Network engineering convention is SI (storage industry
since 1998 IEC ratification of KiB/MiB but SI persisted in marketing +
billing). DigitalOcean, AWS S3, GCP Cloud Storage all bill in SI bytes.

**Alternatives rejected:**
- IEC 1024-base — accurate per CS convention but inconsistent with vendor billing surfaces
- Dual display ("1.5 GB / 1.4 GiB") — clutter, no clear user benefit
- Per-context decision — adds complexity, breaks helper as single source of truth

**Consequences:** Edge Requests in Billing shows `1.24M` (1 million,
not 1,048,576). Fast Data Transfer shows `412 GB` per SI. Matches what
operators see in DO billing console.

#### TDD

```
RED:     test_format_date_handles_4_intents
RED:     test_format_date_returns_em_dash_for_null
RED:     test_format_date_returns_em_dash_for_empty_string                (EC-2, MUST FIX)
RED:     test_format_number_compact_formats_millions
RED:     test_format_number_grouped_uses_thousand_separator
RED:     test_format_number_bytes_handles_KB_MB_GB
RED:     test_format_number_returns_em_dash_for_NaN
RED:     test_format_number_returns_em_dash_for_Infinity                  (EC-8)
GREEN:   Implement helpers using Intl APIs with single Number.isFinite + string-truthy guard
VERIFY:  npm test -- format
```

Assertions:
- `test_format_date_returns_em_dash_for_empty_string` (EC-2) — `formatDate('', 'date')` returns `'—'`. Defensive: today `!''` is true so the guard catches; this test pins the invariant in case future refactor changes the guard.
- `test_format_number_returns_em_dash_for_Infinity` (EC-8) — `formatNumber(Infinity, 'compact')` returns `'—'`. Same family as NaN; single guard `if (!Number.isFinite(value)) return '—'`.

#### Acceptance criteria

- [ ] All 8 TDD tests pass green
- [ ] `formatDate(undefined | null | '' | 'invalid-iso')` always returns `'—'` (4 fallback inputs covered)
- [ ] `formatNumber(NaN | Infinity | -Infinity)` always returns `'—'`
- [ ] ADR-D4 SI bytes convention documented in jsdoc of `formatNumber`

#### DoD

- [ ] Commit `feat(dashboard): T-C.1 lib/format.ts helpers for dates + numbers (P-1, P-2, EC-2, EC-8)`
- [ ] Helpers fully tested (4 date intents + 4 number intents + 4 defensive guards)

---

### T-C.2 — Migrate page formatters to lib/format.ts

#### Files to edit

```
cloud/dashboard/src/pages/billing.tsx        — replace `new Date().toLocaleDateString()` with formatDate(date, 'date')
cloud/dashboard/src/pages/domains.tsx        — same
cloud/dashboard/src/pages/overview.tsx       — replace last_deploy ISO display with formatDate(iso, 'relative')
cloud/dashboard/src/pages/projects.tsx       — same
cloud/dashboard/src/pages/team.tsx           — replace `joined` ISO short with formatDate(joined, 'date')
cloud/dashboard/src/pages/billing.tsx        — Edge Requests: formatNumber(value, 'compact')
```

#### Deep dive

Use `grep -rn "toLocaleDateString\|new Date(" src/pages/` to find every
existing date formatter. Replace each with `formatDate(iso, intent)`.

For numbers, find `useNumberFormat\|toLocaleString\|>{value}<` patterns
in billing.tsx UsageMeter and apply `formatNumber(value, 'compact')` for
Edge Requests; keep grouped or unitized for the others.

#### Edge case (EC-3, MUST FIX)

T-C.2 changes user-visible date format (`DD/MM/YYYY` browser-locale →
`Mon DD, YYYY` en-US per ADR-D1). Existing test assertions that
assert exact date strings (e.g., `expect(screen.getByText('Verified
10/04/2026'))`) WILL BREAK after migration. Concrete grep to find
victims:

```bash
grep -rn "10/04/2026\|2026-01-15\|toLocaleDateString" src/pages/*.test.tsx
```

**Mandatory: update all date string assertions in the SAME commit as
the production code change.** Otherwise vitest 94/94 → ~88/94 red on
CI; bisect-noise for future contributors.

#### Acceptance criteria

- [ ] `grep -rn "toLocaleDateString\|toLocaleString" src/pages/` returns ZERO matches
- [ ] Every date display uses `formatDate()` from `lib/format.ts`
- [ ] Every large number display uses `formatNumber()` from `lib/format.ts`
- [ ] **All 94+ vitest tests pass green in the SAME commit** (test assertions updated synchronously per EC-3 MUST FIX)
- [ ] `grep -rn "10/04/2026\|DD/MM" src/pages/*.test.tsx` returns ZERO matches post-migration
- [ ] Headless Chrome screenshots show consistent date format across all pages: `May 25, 2026` (medium-month + day + year)

#### DoD

- [ ] Commit `refactor(dashboard): T-C.2 migrate all page formatters to lib/format.ts + update test assertions (P-1, P-2, EC-3)`

---

## Phase D — Small polish

### T-D.1 — Email mask reformat

#### Files to edit

```
cloud/dashboard/src/pages/profile.tsx — replace maskEmail()
```

#### Current vs proposed

```ts
// Current (too aggressive):
function maskEmail(email: string): string {
  const [local, domain] = email.split('@');
  const [domainName, tld] = domain.split('.');
  return `${local.slice(0, 2)}***@${domainName.slice(0, 2)}***.${tld}`;
}
// "demo@loopzero.example" → "de***@us***.dev"  ❌ ofusca domínio

// Proposed (preserve domain, redact local, defensive against multi-@):
function maskEmail(email: string): string {
  const parts = email.split('@');
  if (parts.length !== 2) return email;        // EC-10 defensive passthrough
  const [local, domain] = parts;
  if (!domain) return email;
  if (local.length <= 1) return `*@${domain}`;
  return `${local[0]}${'*'.repeat(Math.min(local.length - 1, 8))}@${domain}`;
}
// "demo@loopzero.example" → "d***@loopzero.example"  ✓ preserva sinal de conta
// "a@b@c.com"        → "a@b@c.com"          ✓ passthrough EC-10
```

#### Edge cases

- Empty local part: render `*@domain`
- Single-char local: render `*@domain`
- Very long local (>10 chars): cap mask at 8 stars (avoid `d**********@x.com`)
- **EC-10:** Multi-`@` email (`john@doe@malformed.com`) — `split('@')` returns 3 parts. Without guard, would render `j***@doe`, losing `.malformed.com`. Defensive passthrough returns original string.

#### TDD

```
RED:     test_mask_email_preserves_domain
RED:     test_mask_email_handles_single_char_local
RED:     test_mask_email_caps_mask_length
RED:     test_mask_email_handles_multiple_at_signs                        (EC-10)
GREEN:   Replace maskEmail() per proposed impl
VERIFY:  npm test -- profile
```

Assertion for EC-10: `maskEmail('a@b@c.com')` returns `'a@b@c.com'`
(unchanged). `maskEmail('x@')` returns `'x@'` (no domain). `maskEmail('@y.com')`
returns `'@y.com'` (empty local).

#### DoD

- [ ] Commit `fix(dashboard): T-D.1 email mask preserves domain + defensive multi-@ (P-3, EC-10)`

---

### T-D.2 — Environment status capitalization

#### Files to edit

```
cloud/dashboard/src/pages/environments.tsx — capitalize status display
```

#### Approach

Source data is lowercase (`'live'`, `'building'`, etc.) — fine for
the value. Add display helper that **title-cases multi-word
snake_case strings** per EC-9 (e.g., `'pending_verification'` →
`'Pending Verification'`, not the broken `'Pending_verification'`):

```ts
function statusLabel(s?: string): string {
  if (!s) return 'Idle';
  return s.split('_')                                 // EC-9: split snake_case
    .map(w => w.charAt(0).toUpperCase() + w.slice(1))
    .join(' ');
}
```

Apply in the Status column cell render.

#### TDD

```
RED:     test_status_label_capitalizes_single_word
RED:     test_status_label_returns_idle_for_undefined
RED:     test_status_label_title_cases_multi_word                         (EC-9)
GREEN:   Replace inline status display with statusLabel helper
VERIFY:  npm test -- environments
```

Assertion for EC-9: `statusLabel('pending_verification')` returns
`'Pending Verification'`. `statusLabel('live')` returns `'Live'`.
`statusLabel('')` returns `'Idle'`.

#### DoD

- [ ] Commit `fix(dashboard): T-D.2 capitalize + title-case environment status labels (P-4, EC-9)`

---

### T-D.3 — Avatar fallback shape verification

#### Files to edit

```
cloud/dashboard/src/pages/profile.tsx — verify Avatar usage (no edit unless bug confirmed)
```

#### Investigation

Open profile.png at 100% zoom. If avatar IS visually round (Chrome
rendering — different from screenshot artifact), close as no-op.

If genuinely square, check the Avatar primitive call site for missing
className or wrong size variant. Possibly @loopzero/ui ships Avatar
with `rounded-md` default that needs `rounded-full` override.

#### DoD

- [ ] Either: NO-OP commit `chore(dashboard): T-D.3 Avatar shape confirmed correct (P-5)`
- [ ] OR fix commit `fix(dashboard): T-D.3 Avatar use rounded-full variant (P-5)`

---

## Phase E — Closure

### T-E.1 — Update CHANGELOG + MEET status

#### Files to edit

```
CHANGELOG.md — Changed/Fixed entries per bug
.claude/knowledge-base/meetings/MEET-ASYNC-AMENDMENT-2026-05-24-001-dashboard-visual-redesign-dod.md
  — note T-H.1 visual smoke gap closed by this plan (T-B.3 13/13 coverage)
```

#### DoD

- [ ] CHANGELOG entries for B-1, B-2, P-1..P-5 (fixed bugs in Fixed; new tooling in Added)
- [ ] MEET-001 amendment v1.1 noting T-H.1 closure mechanism (still pending ratification — added as ratification evidence)

---

## ADRs (Architecture Decision Records)

### ADR-D1 — `formatDate` returns en-US literal strings, not i18n keys

**Decision:** Hard-code `en-US` locale in `lib/format.ts` helpers.

**Context:** LoopZero dashboard is English-only in pre-release. Adding i18n
infrastructure (react-intl, translation files, language switcher) is a
v1.x decision and would block this polish plan unnecessarily.

**Alternatives rejected:**
- (a) `useLocale()` hook reading from session — premature; no session.locale field
- (b) Browser navigator.language — varies by user, inconsistent across team/billing/etc.
- (c) i18n library (react-intl, i18next) — pulls 30-50 KB brotli into bundle; defer to v1.x

**Consequences:**
- All dates display in en-US format regardless of user browser locale (acceptable for technical SaaS audience)
- Switching to i18n later requires single-file refactor (`lib/format.ts`) + locale wiring
- Bundle delta: ZERO (uses native `Intl` APIs already shipped by Vite-targeted browsers)

### ADR-D2 — Mock layer expansion uses env var flags, not query strings

**Decision:** Add `VITE_MOCK_LOGGED_OUT=true` as a second env var,
NOT a `?logged_out=true` query string.

**Context:** Two ways to toggle the logged-out mock state.

**Alternatives rejected:**
- (a) Query string `?mock_state=logged_out` — runtime, requires URL editing on each refresh
- (b) localStorage flag — sticky, easy to forget you set it
- (c) Single env var with values (`VITE_USE_MOCKS=logged_out`) — couples mock-on with mock-mode

**Consequences:**
- Both env vars at boot: `VITE_USE_MOCKS=true VITE_MOCK_LOGGED_OUT=true npm run dev`
- Reusable: `VITE_MOCK_UNVERIFIED=true` for verification-banner state (future)
- Dead-code elimination still works (env vars statically resolvable)

### ADR-D3 — TopBar Select uses compound API, not native option children

**Decision:** Use `<Select.Trigger>` + `<Select.Content>` + `<Select.Item>` per @loopzero/ui pattern.

**Context:** B-1 root cause is missing compound structure.

**Alternatives rejected:**
- Native HTML `<select>` — less styleable, no design-system continuity
- HeadlessUI ListBox — wrong primitive (not in stack)
- Custom Combobox — reinvents the wheel

**Consequences:**
- Compound structure is the documented @loopzero/ui pattern (consistent with Card.Header / DangerZone.Action)
- Accessibility: built-in keyboard nav + aria roles from Radix transitively

---

## Edge cases (v1.1 — integrated from `/edge-case-plan` review)

`/edge-case-plan` analysis (commit `6b9f8a6e`) produced
`docs/plans/2026-05-25-dashboard-visual-polish-edge-cases.md` with 14
NEW edges. v1.1 of this plan integrates each into the relevant task
AC / TDD / Deep Dive sections above (see EC-N tags inline).

### Inline triage (original 7 edges, already in AC)

| Edge | Phase | Severity | Notes |
|------|-------|----------|-------|
| `/v1/billing` returns null in T-A.2 | A | MUST FIX | In T-A.2 AC |
| Empty projects list (TopBar Select) | A | SHOULD TEST | EC-4, in T-A.1 TDD |
| Mock submitFlow shouldn't navigate | B | SHOULD TEST | Upgraded to EC-1 MUST FIX in T-B.2 |
| `formatDate(invalidISO)` throws | C | MUST FIX | In T-C.1 AC (4 defensive guards) |
| `formatNumber(NaN)` returns "NaN" | C | MUST FIX | In T-C.1 TDD + EC-8 (Infinity) |
| Email mask on undefined `session.traits.email` | D | SHOULD TEST | Handled by profile.tsx early return |
| Avatar shape verification | D | DOCUMENT | T-D.3 NO-OP path |

### Review-discovered edges (v1.1 integration, EC-N tags)

| EC | Phase | Severity | Integration |
|----|-------|----------|-------------|
| EC-1 | T-B.2 | MUST FIX | Mock `submitFlow` never mutates `window.location.href`; in TDD + AC |
| EC-2 | T-C.1 | MUST FIX | `formatDate('')` defensive test; in TDD |
| EC-3 | T-C.2 | MUST FIX | Test assertions updated in SAME commit as migration; in AC + DoD |
| EC-4 | T-A.1 | SHOULD TEST | Empty projects → only sentinel; in TDD |
| EC-5 | T-A.2 | SHOULD TEST | `limit: null` Enterprise unlimited; in TDD + ADR-D1.bis |
| EC-6 | T-A.2 | SHOULD TEST | `used > limit` over-quota visual; in TDD + AC |
| EC-7 | T-B.2 | SHOULD TEST | Recovery 2-phase + verification `passed_challenge`; in TDD + AC |
| EC-8 | T-C.1 | SHOULD TEST | `formatNumber(Infinity)` em-dash; in TDD |
| EC-9 | T-D.2 | SHOULD TEST | Multi-word snake_case title case; in TDD + Approach |
| EC-10 | T-D.1 | SHOULD TEST | Multi-`@` email passthrough; in TDD + impl |
| EC-11 | T-A.1 | SHOULD TEST | Long project name ellipsis verification; in TDD |
| EC-12 | T-A.1 | DOCUMENT | @loopzero/ui handles overflow natively; Deep Dive note |
| EC-13 | T-C.1 | DOCUMENT | ADR-D4 SI bytes 1000-base decision; full ADR added |
| EC-14 | T-B.3 | DOCUMENT | Port 5173 conflict pre-step in workflow |

---

## Coverage matrix (v1.1 expanded with edge-case integration)

### Original findings (from validation tour)

| Finding | Severity | Resolution task | Validation |
|---------|----------|-----------------|------------|
| B-1 (TopBar) | 🔴 HIGH | T-A.1 | Headless Chrome diff |
| B-2 (Overview usage) | 🟠 MED | T-A.2 | Headless Chrome diff + vitest assertion |
| M-1 (logged-out mock) | n/a | T-B.1 | Manual flag toggle |
| M-2 (Kratos flow fixtures) | n/a | T-B.2 | Manual visit /login etc. |
| P-1 (date drift) | 🟡 MED | T-C.1 + T-C.2 | grep returns 0 toLocaleDateString |
| P-2 (number format) | 🟡 MED | T-C.1 + T-C.2 | Visual diff (Edge Requests "1.24M") |
| P-3 (email mask) | 🟢 LOW | T-D.1 | vitest assertion |
| P-4 (env status capitalization) | 🟢 LOW | T-D.2 | Visual diff |
| P-5 (avatar shape) | 🟢 LOW | T-D.3 | Visual confirmation |

### Edge-case findings (v1.1 integration)

| EC | Resolution task | Validation |
|----|-----------------|------------|
| EC-1 (MUST FIX) | T-B.2 | vitest spy on window.location setter |
| EC-2 (MUST FIX) | T-C.1 | vitest empty-string assertion |
| EC-3 (MUST FIX) | T-C.2 | vitest 94/94 green in same commit |
| EC-4 | T-A.1 | vitest empty projects assertion |
| EC-5 | T-A.2 | vitest unlimited render assertion + ADR-D1.bis |
| EC-6 | T-A.2 | vitest over-quota assertion |
| EC-7 | T-B.2 | vitest recovery 2-phase + verification state |
| EC-8 | T-C.1 | vitest Infinity guard assertion |
| EC-9 | T-D.2 | vitest title-case assertion |
| EC-10 | T-D.1 | vitest multi-@ passthrough assertion |
| EC-11 | T-A.1 | vitest CSS ellipsis class assertion |
| EC-12 | T-A.1 | Deep Dive note (no test) |
| EC-13 | T-C.1 | ADR-D4 doc + jsdoc on `formatNumber` |
| EC-14 | T-B.3 | Workflow pre-step documented |

**Coverage: 9/9 findings + 14/14 edges = 23/23 (100%) with designated tasks/validation.**

---

## Global DoD (plan completion gates)

- [ ] All 7 tasks (T-A.1, T-A.2, T-B.1, T-B.2, T-B.3, T-C.1, T-C.2) implemented
- [ ] All 3 polish tasks (T-D.1, T-D.2, T-D.3) implemented or NO-OP-closed
- [ ] T-E.1 CHANGELOG + MEET update committed
- [ ] **vitest 94 + ~13 new tests from EC integration ≈ 107/107 green** (never regress; T-C.2 MUST update assertions in same commit per EC-3)
- [ ] `npm run build` green
- [ ] `npm run size` 9/9 green (no per-chunk regression; lib/format.ts is ~50 LOC, dev-mocks delta is dev-only)
- [ ] `task quality:dashboard-bundle` green
- [ ] 13/13 routes visually validated via headless Chrome (T-B.3 evidence)
- [ ] All 14 EC tags resolved per Coverage matrix above
- [ ] All 3 MUST FIX edges (EC-1, EC-2, EC-3) have passing tests + AC checked
- [ ] No new internal terms in CLI output (Rule 3 of CLAUDE.md)
- [ ] No new `pnpm` artifacts introduced (the `.gitignore` from commit `38834efd` should prevent it)

---

## Estimated effort (v1.1 — +EC integration overhead)

| Phase | Tasks | LOC est. | EC tests added | Time est. |
|-------|-------|----------|---------------:|-----------|
| A | 2 | ~55 | +3 (EC-4, EC-5, EC-6, EC-11) | 35 min |
| B | 3 | ~110 | +2 (EC-1, EC-7) | 50 min |
| C | 2 | ~95 | +3 (EC-2, EC-8) + ADR-D4 | 40 min |
| D | 3 | ~35 | +2 (EC-9, EC-10) | 20 min |
| E | 1 | ~20 | 0 | 10 min |
| **Total v1.1** | **11** | **~315 LOC** (+35 vs v1.0) | **+13 RED tests** | **~2h35min** (+25min vs v1.0) |

Within a single session. KISS-compliant. The +35 LOC and +25 min are
the cost of integrating the 14 edges — small premium for eliminating
risk of EC-3 CI red + EC-1 dev tour break + EC-2 future-proofing.

---

## Next-step suggestions (NOT part of this plan)

1. **Storybook + Chromatic visual regression** — permanent T-H.1 closure with pixel-diff baselines. ~3-4h setup + ongoing maintenance. Best after this polish plan ships and visuals are stable.
2. **MEET-003 Lighthouse CI** — output-side perf gate (LCP/TBT/Performance score). Complements MEET-002 bundle gate which is input-side.
3. **Brief #5 to @loopzero/ui** — if T-A.1 reveals additional Select primitive gaps (e.g., search-in-dropdown, multiselect, async loading), batch them in a brief.
4. **i18n infrastructure** — when product gets non-English customer (Stripe/billing copy too).

---

## References

- Visual validation tour: commit `2af9133c` + 13 screenshots in `/tmp/dashboard-screenshots/` (transient — re-run via mock layer)
- Parent plan: `docs/plans/2026-05-24-dashboard-visual-redesign-mockup-aligned-plan.md` v1.2
- MEET amendments that this plan respects:
  - `MEET-ASYNC-AMENDMENT-2026-05-24-001-dashboard-visual-redesign-dod.md` (LOC + visual smoke deferral)
  - `MEET-ASYNC-AMENDMENT-2026-05-24-002-dashboard-bundle-gate-redefinition.md` v1.4 (bundle gates)
- Mock layer source: `cloud/dashboard/src/lib/dev-mocks.ts` (commit `2af9133c`)
- Design reference: `cloud/dashboard/design/loopzero_overview_dashboard_mockup.html`
- Design language doc: `docs/design/2026-05-24-dashboard-design-language.md`
