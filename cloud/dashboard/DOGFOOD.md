# Dashboard Dogfood QA Runbook

> Plan v1.0 Final Phase. Use this runbook to validate dashboard changes
> before declaring a release ready. Each section maps to one acceptance
> criterion from the Final Phase DoD.

## Quick reference

```bash
# Auto-runnable in CI (no infra setup beyond npm install):
npm test                          # 200+ vitest tests (handlers + pages + journeys + a11y)
npm run build                     # tsc + vite build (catches type + bundle errors)
npm run size                      # size-limit cap (180 KB brotli)

# Requires extra setup (see sections below):
npm run test:e2e                  # Playwright browser-runtime journeys
npm run test:lighthouse           # Lighthouse CI against dev server with mocks
npm run test:visual               # Visual regression (vendor TBD)
```

## 1. Vitest acceptance (auto in CI)

The `src/test/e2e-journeys.test.tsx` file covers the 6 critical journeys
against jsdom + the full mock infrastructure (`lib/mocks/`). These are the
**baseline** acceptance — every release must land them green:

| Journey | Test name | What it validates |
|---|---|---|
| 1. Land on Overview | `journey_1_overview_renders_without_crash` | Page chrome + nav rendered |
| 2. Browse projects | `journey_2_projects_list_renders_primary_action` | CreateProjectDialog reachable |
| 2b. Drill into detail | `journey_2b_project_detail_renders_with_route_param` | Route param consumed |
| 3. Deploy lifecycle | `journey_3_deployments_page_renders_empty_state` | Empty + filter chrome |
| 4. Billing | `journey_4_billing_renders_without_crash` | Plan + portal CTA |
| 5. Team | `journey_5_team_page_renders_invite_button` | InviteMemberDialog reachable |
| 6. Secrets | `journey_6_secrets_page_renders_with_add_button` | AddSecretDialog reachable |
| 7. Audit | `journey_7_audit_log_renders_shell` | List + filter |
| 7b. Status | `journey_7b_project_status_renders_health_card` | ProjectHealthCard |
| 8. Domains | `journey_8_domains_renders_add_button` | AddDomainDialog reachable |
| 8b. Environments | `journey_8b_environments_renders_new_button` | CreateEnvironmentDialog reachable |

```bash
npx vitest run src/test/e2e-journeys.test.tsx
```

## 2. Mock handler shape parity (auto in CI)

The `src/lib/mocks/handlers.test.ts` file asserts every endpoint returns
the shape from `src/lib/api/types.ts` (which mirrors Go API structs). When
backend updates a json:"tag", run this to catch the drift:

```bash
npx vitest run src/lib/mocks/
```

41 assertions across 47 endpoints + 3 SSE streams.

## 3. Accessibility semantic skeleton (auto in CI)

```bash
npx vitest run src/test/a11y.test.tsx
```

Asserts each page has accessible names on primary buttons + headings
present. Wire `vitest-axe` for full axe-core scan (commented in
`a11y.test.tsx`):

```bash
npm i -D vitest-axe
```

Then uncomment the axe assertions per page.

## 4. Browser-runtime E2E (Playwright)

Setup required (one-time):

```bash
npm i -D @playwright/test
npx playwright install chromium firefox webkit
```

Run:

```bash
VITE_USE_MOCKS=true npm run dev -- --port 5174 &  # background dev server
npx playwright test
```

Or use the `webServer` config (auto-starts dev server with env stub):

```bash
CI=true npx playwright test
```

Browser-runtime catches what jsdom misses:
- Real CSS layout (Tailwind v4 + components.css)
- Real DropdownMenu portal positioning
- Real focus management across pages
- Real screenshot diffing (when visual regression vendor chosen)

## 5. Lighthouse CI

Setup:

```bash
npm i -D @lhci/cli
```

Run against local dev with mocks:

```bash
VITE_USE_MOCKS=true npm run dev -- --port 5174 &
npx lhci autorun --config=./lighthouserc.json
```

Budgets (per `lighthouserc.json`):
- Performance ≥85
- Accessibility ≥95
- Best practices ≥95
- LCP ≤2500ms
- CLS ≤0.1
- TBT ≤200ms (warn)

## 6. Visual regression — vendor decision OPEN

Three options:

| Vendor | Cost | Pros | Cons |
|---|---|---|---|
| **Chromatic** | $149/mo Pro | Storybook integration, auto-baseline | $$ |
| **Percy** | $599/mo Team | Per-page testing, BrowserStack | $$$ |
| **BackstopJS** | free | self-host, no SaaS dep | manual baseline mgmt |

Recommended: start with BackstopJS for the smoke and migrate to Chromatic
when Storybook ships. Add `backstop.json` + 11 reference scenarios (one
per journey above).

## 7. Manual UX walkthrough

When CI green + Lighthouse green + axe-core clean, ask a non-author human
to walk through the 6 critical journeys end-to-end. Capture findings in:

```
.claude/knowledge-base/audits/{date}-dashboard-dogfood-{n}.md
```

Use the audit template from the original FAANG audit
(`.claude/knowledge-base/audits/2026-05-25/dashboard-faang-review.md`)
for structure.

## 8. Backend integration smoke (requires Tilt)

```bash
# Stop the mock dev server, start Tilt:
task setup
tilt up

# Then dev server WITHOUT mocks:
VITE_USE_MOCKS=false npm run dev -- --port 5174

# Manually walk through 3 journeys (deploy / logs / rollback) against the
# real engine API + cosign + ArgoCD. Confirm:
#   - JWT exchange works
#   - All 47 endpoints respond with shapes matching types.ts
#   - SSE log streams emit per second
```

## Acceptance criteria

Plan v1.0 Final Phase declared complete when:

- [ ] All vitest tests green (200+ tests including handlers + journeys + a11y)
- [ ] Production build green + bundle <180 KB brotli cap
- [ ] Lighthouse: perf ≥85, a11y ≥95, bp ≥95 on every page
- [ ] axe-core: zero critical/serious violations
- [ ] Playwright runtime journeys green in Chromium + Firefox + WebKit
- [ ] Visual regression baseline established + no unintended diffs
- [ ] Manual UX walkthrough by non-author: zero blocker findings
- [ ] Backend integration smoke (VITE_USE_MOCKS=false): 3 journeys green against Tilt

## Pre-existing known issues

(Logged in `.claude/knowledge-base/audits/{date}-dashboard-dogfood-{n}.md`
post-walkthrough.)
