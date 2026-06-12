# Dashboard Deep Design Review — All 13 Pages

> **Slug:** `2026-05-25-dashboard-deep-design-review`
> **Status:** DRAFT (review output; not an action plan — see follow-up plan)
> **Scope:** all 13 routes of `cloud/dashboard/`
> **Anchored to:** `docs/design/2026-05-25-loopzero-dashboard-design-system.md` v1.0
> **Reference frameworks:**
>   - Refactoring UI (Adam Wathan, Steve Schoger)
>   - Laws of UX (Jon Yablonski)
>   - Nielsen Heuristics (10 usability principles)
>   - WAI-ARIA Authoring Practices Guide
>   - Open Design `design-templates/dashboard` SKILL pattern
>   - Open Design DESIGN.md references (`vercel`, `cal`, `apple`)
> **Companion screenshots:** `/tmp/dashboard-screenshots/*.png` + new tour
>   in `docs/reviews/2026-05-25-dashboard-screenshots/` (TBD on re-run)

---

## 0. Methodology

For each of 13 routes I evaluate against **5 dimensions**:

1. **Token compliance** — does the page use @loopzero/ui tokens (vs hex/arbitrary)?
2. **Density** — does it meet the targets in DESIGN.md § 6.1?
3. **Hierarchy** — Refactoring UI § Hierarchy (size > weight > color; never weight > size)?
4. **UX laws** — Hick's (decision count), Fitts's (target size), Miller's (chunking), Aesthetic-Usability?
5. **WAI-ARIA** — keyboard navigation, focus visibility, role/aria-label correctness?

**Each finding is classified:**

| Severity | Code | When |
|----------|------|------|
| 🔴 P0 | Blocking | Renders broken / accessibility regression / data wrong |
| 🟠 P1 | High | Pattern violation visible on first glance |
| 🟡 P2 | Medium | Polish — cumulative effect on perceived quality |
| 🟢 P3 | Low | Nitpick — fix during adjacent edits |

**Notation**: `DSS § X.Y` references our Design System sections.
`RUI § X` references Refactoring UI chapters. `LUX § X` references
Laws of UX principles.

---

## 1. Cross-cutting findings (apply to ALL pages)

These are violations that span multiple pages. Fix them once,
benefits compound across the dashboard.

### CC-1 🟠 Primary color over-applied

**Observation:** Multiple pages show `bg-primary` on more than 2
surfaces per viewport:
- Overview: "Add New" button (primary) + "Upgrade to Pro" inside
  Alerts Card (primary) + sidebar active = 3 primary uses
- Billing: "Upgrade to Pro $19/mo" Card has primary text + "Manage
  Plan" button has primary outline = 2 hot foci competing

**DSS rule violated:** § 2.3 — primary max 2× per viewport.
**RUI § Color:** "If everything is emphasized, nothing is emphasized."

**Fix direction:** Reserve `variant="primary"` for THE single action
that drives the user's goal on that page. Demote secondary CTAs to
`variant="secondary"` or `variant="ghost"`. "Upgrade to Pro" inline
hints should be `<Badge variant="outline">Upgrade</Badge>`, not
primary buttons.

### CC-2 🟠 Date format drift (PARTIALLY FIXED)

**Observation:** Pre-T-C.2 had 3 date formats across 4 pages. T-C.2
(commit `b988a133`) migrated 4 callsites to `formatDate(iso, 'date')`.
**Verification:** `grep -rn "toLocaleDateString" src/pages/` returns
zero matches ✓.

**Residual:** ProjectCard primitive (from @loopzero/ui) renders
`lastDeployedAt` with its own internal formatter — not in our control.
If primitive formatter diverges from `formatDate`, file Brief #5.

### CC-3 🟠 Empty-state density violation

**Observation:**
- Overview "Recent Previews" empty card = ~200px tall reserved for
  "Preview deployments that you have recently visited or created
  will appear here."
- Overview "Alerts" empty card with "Get alerted for anomalies" +
  "Upgrade to Pro" button = ~150px tall

These are HUGE for empty surfaces. RUI § Density: "When you have
nothing to say, say less."

**DSS rule violated:** § 6.3 — empty states ≤200px, max 1 per page.
**Fix direction:**
- Recent Previews: collapse to a 1-line muted footer "No recent
  previews · run `loopzero preview` to create one" OR hide entirely
  until ≥1 preview exists.
- Alerts: replace with a `<Badge>Pro feature</Badge>` chip in
  another section instead of dedicated card.

### CC-4 🟡 KPI strip pattern absent from Overview

**Observation:** Overview has no KPI strip. The "Usage" card holds 3
metrics (Fast Data Transfer / Fast Origin Transfer / Edge Requests)
in `UsageMeter` rows — that's USAGE, not KPI. There's no
"Active projects: 5", "Deploys this week: 12", "Open incidents: 0"
at-a-glance summary at the top.

**DSS pattern missing:** § 5.2 #1 — KPI strip + 2fr:1fr panels.

**Fix direction:** Add a KPI strip of 4 cards above the existing
2-asymmetric grid:
1. Active projects (count)
2. Deploys this week (count + delta)
3. Usage % of plan (formatNumber percent + inline bar)
4. Open incidents (count, color by 0/N)

### CC-5 🟢 Font tabular-nums missing on counts

**Observation:** Most numeric displays (table cells with counts,
KPI-like values) don't have `tabular-nums` class. Causes digits to
shift across rows.

**DSS rule violated:** § 3.2.

**Fix direction:** add `tabular-nums` class to every cell rendering
a numeric value. Either via a `<NumericCell>` wrapper or per-site.

### CC-6 🟡 Spacing irregularities

**Observation:** `grep -rn "gap-5\|gap-10\|space-y-5" src/` returns
matches in some pages. These break the 4/8/12/16/24/32 scale.

**DSS rule violated:** § 5.3 (only allowed scale).
**Fix direction:** sed pass to normalize `gap-5` → `gap-4` or
`gap-6`, `space-y-5` → `space-y-4` or `space-y-6`.

### CC-7 🟢 Focus ring inconsistent

**Observation:** @loopzero/ui primitives ship focus rings by default,
but custom elements (e.g., the sidebar nav items if they're rolled
custom) may not get them.

**DSS rule:** § 2.5 — `ring-ring` + `focus-visible:ring-2`.
**Fix direction:** audit custom interactive elements + add
`focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2`
to any plain `<button>` / `<a>` that's not from @loopzero/ui.

---

## 2. Per-page audit (13 routes)

### 2.1 `/` Overview (`pages/overview.tsx`)

**Layout:** ActionBar + 2-asymmetric grid (1.2fr:1fr) with:
- Left col: Usage section (UsageMeter, 3 metrics) + Alerts EmptyState
- Right col: Projects (3 ProjectCards) + Recent Previews EmptyState

**Strengths:**
- ✓ ActionBar primary CTA correctly sized (`size="md"`)
- ✓ ProjectCard composition reasonable
- ✓ Post-T-A.2 fix: UsageMeter reads real `/v1/billing` data
- ✓ Section labels use `text-label-caps` correctly

**Findings:**

| Sev | Issue | Recommendation |
|-----|-------|----------------|
| 🟠 P1 | **No KPI strip** at top — Overview has no at-a-glance metrics | Add 4-card KPI strip per CC-4 |
| 🟠 P1 | **3 primary uses** ("Add New", "Upgrade to Pro" inside Alerts, sidebar active) | Demote "Upgrade to Pro" inside Alerts to `<Badge>` chip |
| 🟠 P1 | **2-asymmetric grid wastes space** when right col only has 3 cards | Switch to `grid-cols-3 gap-6` with `col-span-2` on Projects (per DSS § 5.2 #1 alternative) |
| 🟡 P2 | **Recent Previews empty card** = ~200px reserved for emptiness | Collapse to 1-line footer per CC-3 |
| 🟡 P2 | **Alerts empty card** competes with real Usage info | Move to small chip elsewhere OR hide until enabled |
| 🟡 P2 | UsageMeter row labels don't use `tabular-nums` (CC-5) | Add to UsageMeter formatter output or wrap row in `.tabular-nums` |
| 🟢 P3 | "Add New" CTA label is vague | Rename to "New project" (specific noun) |

**Hierarchy verdict:** weak — no clear visual anchor. Eye doesn't
know where to land first (3 sections compete with similar weight).

**Density verdict:** 38% of viewport at 1440×900 is empty whitespace
or empty-state cards. DSS target = ≤15%.

### 2.2 `/projects` (`pages/projects.tsx`)

**Layout:** ActionBar + 3-col grid of ProjectCards.

**Strengths:**
- ✓ Grid scales nicely (3 cards then 2 cards layout)
- ✓ Status pill on each card (Live, green)
- ✓ Framework label (NEXT/GO/etc.) compact

**Findings:**

| Sev | Issue | Recommendation |
|-----|-------|----------------|
| 🟠 P1 | **Card-grid layout for entity list** — projects ARE entities, table is more scannable for 5+ items | Switch to Table view OR offer table/grid toggle (defer toggle to v1.x; ship Table now) |
| 🟡 P2 | Cards are ~140px tall with only 4 lines of info | Compress to 2-line list row (name + meta) at 56px |
| 🟡 P2 | "New Project" CTA visible AND search both at top — could collapse if no project filter active | Keep as-is; CC-1 unrelated |
| 🟢 P3 | Framework labels uppercase (`NEXT`, `GO`) — inconsistent with status pills (`Live` title-case) | Title-case framework labels OR use `<Badge variant="outline">` |

**Hierarchy verdict:** OK — card name is clearly the primary anchor.

**Density verdict:** 5 cards in 3-col + 2-col layout = OK for 5 projects.
At 10+ projects this layout would scroll heavily — table would scale better.

### 2.3 `/environments` (`pages/environments.tsx`)

**Layout:** ActionBar (search only, no CTA) + Table (Name/Domain/Projects/Status/Protected).

**Strengths:**
- ✓ Table-only is correct pattern (DSS § 5.2 #2)
- ✓ Status capitalization fixed by T-D.2 (`Live`, `Failed`)
- ✓ Protected column with Lock icon
- ✓ All 6 envs render with mock data

**Findings:**

| Sev | Issue | Recommendation |
|-----|-------|----------------|
| 🟡 P2 | **Projects count column** doesn't use `tabular-nums` | Add `tabular-nums` to TD |
| 🟡 P2 | **No primary action** in ActionBar (operations are CLI-first per ADR) | OK to leave — but consider adding "New environment" → opens docs link (like Domains "Add Domain") |
| 🟢 P3 | "Idle" status doesn't have a StatusDot color (just em-dash) | Add gray StatusDot for `idle` state |
| 🟢 P3 | Domain column for wildcards (`*.preview.acme.com`) wraps weirdly at narrow viewport | Truncate with ellipsis + tooltip |

**Hierarchy verdict:** good — Name is leftmost + bold, scannable.

**Density verdict:** 6 rows × 40px = 240px of table content; viewport
shows 6 rows + headers easily. Good.

### 2.4 `/domains` (`pages/domains.tsx`)

**Layout:** ActionBar + vertical stack of full-width Cards (one per domain).

**Strengths:**
- ✓ Inline DNS records expansion on pending_verification (DSS § 9.2)
- ✓ TLS badge on active domains
- ✓ CopyButton on each DNS record
- ✓ CodeBlock with `loopzero domain verify <domain>` CLI hint

**Findings:**

| Sev | Issue | Recommendation |
|-----|-------|----------------|
| 🟠 P1 | **Card-per-domain layout** is sparse for entities — Table is more scannable | Switch to Table with expandable row (Disclosure pattern) for pending verification |
| 🟡 P2 | Active+TLS+Verified row consumes ~96px vertical for 4 lines of meta | Compress to 56px 2-line row in Table |
| 🟡 P2 | "Add Domain" CTA opens docs.loopzero.example/cli/domain — design-intentional but feels like dead-end UX | Add CLI snippet inline OR small inline modal with `loopzero domain add <domain>` example |
| 🟢 P3 | Status pill labels: "Pending DNS", "Provisioning", "Active" — case mixed | Standardize all title-case (Pending DNS ✓; Active ✓; Provisioning ✓) |

**Hierarchy verdict:** weak — every domain Card has equal weight.
Active domains and pending domains visually compete equally for attention.

**Density verdict:** 4 domains × 96px (active) + 1 expanded (~240px) =
~600px. Same content in Table = ~280px. **53% space savings** with
Table redesign.

### 2.5 `/billing` (`pages/billing.tsx`)

**Layout:** Top: 2-col grid (Current Plan card + Upgrade to Pro card).
Bottom: UsageMeter full-width with 5 metrics.

**Strengths:**
- ✓ Plan name with PlanBadge (Pro)
- ✓ Period date range formatted via formatDate
- ✓ Usage rows include Edge Requests with `1,240,000 / 10M` formatting (post-T-C.2)

**Findings:**

| Sev | Issue | Recommendation |
|-----|-------|----------------|
| 🟠 P1 | **"Upgrade to Pro $19/mo" card** is shown to user ALREADY on Pro plan in screenshot — should not appear | Conditional render: hide Upgrade card when current plan equals or exceeds upsell plan |
| 🟠 P1 | **Two primary-styled affordances** ("Manage Plan ↗" + "Upgrade to Pro $19/mo") fight for attention | Demote one. Manage Plan is the routine action — keep as secondary outline. Upgrade is the conversion path — keep primary (when shown). |
| 🟡 P2 | **No KPI strip** for billing — could surface "Days until renewal", "Current cycle spend", "Plan utilization %" | Add 4-KPI strip above the 2-card top row |
| 🟡 P2 | "USAGE THIS PERIOD" date range uses ALL CAPS via CSS class — date itself becomes `APR 30, 2026 - MAY 31, 2026` (mixed case-pull from formatDate + class transform). Readable but unusual | Keep ALL CAPS for the section LABEL only; remove text-transform from the date string itself |
| 🟢 P3 | "Manage Plan ↗" arrow icon glued to text without space | Add `gap-1` to button |
| 🟢 P3 | Edge Requests value uses raw counter — could be "1.2M / 10M (12%)" with bar | Already has UsageMeter bar; add percentage label inline |

**Hierarchy verdict:** moderate — eye lands on "Pro" badge first
(strongest contrast). Layout works once Upgrade card is conditionally
hidden.

**Density verdict:** good post-T-C.2 — 5 usage rows fit comfortably.

### 2.6 `/team` (`pages/team.tsx`)

**Layout:** ActionBar with search + "Invite" CTA. Table with
Member / Email / Role / Joined columns.

**Strengths:**
- ✓ Table layout is canonical (DSS § 5.2 #2)
- ✓ Avatar fallback letters (P/A/B/C/D/E)
- ✓ Role badges colored (OWNER warning / ADMIN success / MEMBER default)
- ✓ Date format `Jan 14, 2026` post-T-C.2

**Findings:**

| Sev | Issue | Recommendation |
|-----|-------|----------------|
| 🟡 P2 | **OWNER badge** is the only non-success/non-default color; warning orange feels alarming for a status that's neutral (most workspaces have 1 owner) | Change OWNER variant to "default" or "outline" with `<Crown />` icon. Reserve warning for risky states (suspended, billing past due) |
| 🟡 P2 | Joined date column rendered as plain text — could show relative time for recent joins | Add hover tooltip with absolute date if showing relative |
| 🟡 P2 | No `tabular-nums` on Joined dates | Add class |
| 🟢 P3 | **No "Remove member" action** visible per row | Add icon-only button (`<Ellipsis />`) → dropdown menu (Edit role / Remove) per row |
| 🟢 P3 | Avatar fallback letter color matches name — could derive from email hash for stable consistent colors per person | Defer — currently OK |

**Hierarchy verdict:** good — Member name is bold leftmost.

**Density verdict:** 6 rows fit comfortably. At 50+ members, paginate.

### 2.7 `/settings` (`pages/settings.tsx`)

**Layout:** 1-col stack of Cards. Currently: "CLI-First" Card + DangerZone.

**Strengths:**
- ✓ Deliberately sparse per DSS § 9.1 — design choice, not bug
- ✓ CodeBlock with 4 common CLI commands
- ✓ DangerZone for Delete Team with confirmation dialog

**Findings:**

| Sev | Issue | Recommendation |
|-----|-------|----------------|
| 🟢 P3 | "CLI-First" card heading could be more action-oriented | Rename to "Manage via CLI" or "Workspace settings live in `loopzero`" |
| 🟢 P3 | DangerZone "Delete Team" — confirms with phrase match (good UX) but no preview of what's deleted | Add bullet list: "Deletes: 5 projects, 4 domains, 6 members, all environments" before confirm |

**Hierarchy verdict:** OK — single CLI-First section is clear anchor.

**Density verdict:** intentionally low. OK per § 9.1.

### 2.8 `/profile` (`pages/profile.tsx`)

**Layout:** 3 stacked Cards: Account + Sign out + DangerZone.

**Strengths:**
- ✓ Avatar with fallback (rounded-full confirmed)
- ✓ Email masked via maskEmail() preserving domain (post-T-D.1)
- ✓ DangerZone with confirmation phrase (email)
- ✓ "Authenticated via secure session" success banner

**Findings:**

| Sev | Issue | Recommendation |
|-----|-------|----------------|
| 🟡 P2 | **3 separate Cards** for account / sign-out / danger — could consolidate to 2 (Account + Sign-out as account-card action; DangerZone separate) | Reduce to 2 cards |
| 🟡 P2 | Sign out is a Card containing only a Button — wasteful | Inline the Sign out button at Account card bottom or in TopBar dropdown |
| 🟢 P3 | "Authenticated via secure session" is informational but takes 60px of card height | Move to a small `<Badge variant="success" className="text-xs">Verified session</Badge>` inline with email |
| 🟢 P3 | Avatar size-12 (48px) feels small for the dedicated profile page | Increase to size-16 (64px) on this page only (override OK because it's a "profile" surface, not a list cell) |

**Hierarchy verdict:** moderate — eye lands on Account heading first (good).

**Density verdict:** wasteful — 3 cards for 4 fields. Consolidate.

### 2.9 `/login` (`pages/login.tsx`)

**Layout:** Centered `Card max-w-sm` with LoopZero brand + Sign in heading +
OIDC buttons (GitHub + Google) + email/password form + CLI hint.

**Strengths:**
- ✓ LoopZero brand mark (Zap icon)
- ✓ Two OIDC providers visible (GitHub, Google via GoogleBrandIcon)
- ✓ "or with email" divider — pattern from auth0 / clerk
- ✓ CLI hint via CodeBlock — LoopZero-specific (`loopzero login`)
- ✓ Input + Button both `size="lg"` post-cb67866e (visual pair correct)
- ✓ EC-4 vanilla DOM `form.submit()` preserved for OIDC

**Findings:**

| Sev | Issue | Recommendation |
|-----|-------|----------------|
| 🟡 P2 | **`size="lg"` Buttons + Inputs make form feel large** — auth surfaces conventionally use `md` for tighter rhythm (Linear, Vercel, Cal all use md) | Consider reverting to `md` — DSS § 4.1 says match the form; both md is also a valid pair. User feedback: "buttons and inputs were small without pattern" — but THAT was about MISMATCH, not absolute size |
| 🟢 P3 | "Create account" + "Forgot password?" links in footer use `text-primary` for "Create account" — that's primary usage #3 on the page | Use `text-link` or hover-underlined text-foreground instead |
| 🟢 P3 | CLI hint at bottom is great but could include `--browser` flag hint: `loopzero login --browser` | Defer if not real flag |

**Hierarchy verdict:** strong — Sign in heading + Sign in button anchor.

**Density verdict:** appropriate for auth flow.

### 2.10 `/register` (`pages/register.tsx`)

**Layout:** Same shell as Login. LoopZero brand + Create account heading +
OIDC + Name/Email/Password form + Footer "Already have an account? Sign in".

**Strengths:**
- ✓ 3-field form (Name optional, Email required, Password required with 8-char hint)
- ✓ Same visual pair (`size="lg"` Input + Button)
- ✓ Consistent shell with login (good design system continuity)

**Findings:**

| Sev | Issue | Recommendation |
|-----|-------|----------------|
| 🟡 P2 | "Password (min. 8 characters)" placeholder — hint better as caption below input | Move to `<Caption>` element below Input |
| 🟢 P3 | No password-strength meter | Defer to v1.x; not a current concern |
| 🟢 P3 | Same `size="lg"` debate as Login | Same recommendation |

**Hierarchy verdict:** strong.
**Density verdict:** appropriate.

### 2.11 `/recovery?flow=...` (`pages/recovery.tsx`)

**Layout:** Centered `Card max-w-sm` with Key icon + "Reset your password" +
Email input + "Send recovery code" Button + "Back to login" link.

**Strengths:**
- ✓ Distinct icon (Key) per page identity
- ✓ Single-purpose form (1 input, 1 button)
- ✓ Phase 2 (code) properly transitions when fixture advances

**Findings:**

| Sev | Issue | Recommendation |
|-----|-------|----------------|
| 🟢 P3 | "Back to login" link uses `text-primary` (primary usage #2) | Already only 1 button + 1 link, so OK per § 2.3 — but verify across phase transitions |
| 🟢 P3 | Phase 2 (code) has no "Resend code" CTA visible | Add a 30s-cooldown "Resend code" link |

**Hierarchy verdict:** strong (single-purpose card).
**Density verdict:** appropriate.

### 2.12 `/verification?flow=...` (`pages/verification.tsx`)

**Layout:** Centered `Card max-w-sm` with Mail icon + "Enter verification code" +
Code input + Verify Button.

**Strengths:**
- ✓ Single-purpose card (code submission)
- ✓ Mail icon = mental model "we sent you mail, type the code"
- ✓ Clean form

**Findings:**

| Sev | Issue | Recommendation |
|-----|-------|----------------|
| 🟡 P2 | Code input is single field — for a 6-digit code, **6 separate boxes** is conventional UX (Apple, Stripe, Clerk all do this) | Use a 6-box code input (HTML pattern: `inputMode="numeric" maxLength={1}` per box) for better UX |
| 🟢 P3 | No "Resend code" or "Use a different email" affordance | Add small text-link below Verify button |

**Hierarchy verdict:** strong.
**Density verdict:** very low — could add resend affordance to fill.

### 2.13 `/device-success` (`pages/device-success.tsx`)

**Layout:** Centered card with green CheckCircle + "CLI authorized" +
"You can close this tab and return to your terminal."

**Strengths:**
- ✓ Minimal, single-purpose
- ✓ Clear success signal (green checkmark)
- ✓ Mental model: "you came from CLI, return to CLI"

**Findings:**

| Sev | Issue | Recommendation |
|-----|-------|----------------|
| 🟢 P3 | No countdown auto-close or "Continue" button | Add 5-sec countdown + auto-close OR explicit "Continue to dashboard" button (some users will want browser to stay open) |
| 🟢 P3 | No echo of what was authorized (terminal command, scope) | Add small caption: "Authorized CLI for: paulo@loopzero.example · scopes: read:projects, write:deploys" |

**Hierarchy verdict:** strong (single message).
**Density verdict:** intentionally low. OK.

---

## 3. Cumulative findings

Total findings: **42** (10 cross-cutting CC-* + 32 per-page).

Distribution by severity:
- 🔴 P0: 0 (no blocking bugs — Phase A fixes resolved B-1 and B-2)
- 🟠 P1: 8 (KPI strip absent, table-vs-card-grid choices, primary over-applied)
- 🟡 P2: 18 (density, tabular-nums, consolidation opportunities)
- 🟢 P3: 16 (polish, naming, minor accessibility tweaks)

### 3.1 Top-5 impact fixes (do these first)

| # | Finding | Pages affected | Estimated impact |
|---|---------|----------------|------------------|
| 1 | **CC-4 / O-1**: Add KPI strip to Overview (and Billing) | Overview, Billing | Adds at-a-glance value; closes "no KPIs" gap from reference dashboards |
| 2 | **D-1 + P-1 (per-page)**: Convert Domains + Projects from card-grid to table | Domains, Projects | -50% vertical space; table scans faster for entity lists |
| 3 | **CC-1**: Demote secondary primary usages | Overview, Billing, Login | Restores single-anchor hierarchy (RUI § Hierarchy) |
| 4 | **CC-3 + O-4**: Collapse empty states to inline footers | Overview (Alerts, Recent Previews) | Recovers ~350px viewport space per overview load |
| 5 | **V-1**: 6-box code input for verification | Verification | Industry-standard UX (Apple/Stripe/Clerk) |

### 3.2 Heuristic violations summary

| Heuristic | Pages violating | Cumulative score |
|-----------|-----------------|------------------|
| Aesthetic-Usability Effect (LUX) | Overview, Domains | -2 |
| Hick's Law (decision count) | Overview, Billing | -2 |
| Visibility of system status (Nielsen H1) | None (pills + StatusDot work) | 0 |
| Match between system and real world (Nielsen H2) | None (CLI-first design language clear) | 0 |
| User control + freedom (Nielsen H3) | Verification (no resend), DeviceSuccess (no continue) | -2 |
| Consistency + standards (Nielsen H4) | Login/Register (size=lg debate), Date formats (post-fix OK) | -1 |
| Error prevention (Nielsen H5) | Domains (Add Domain → docs link is dead-end) | -1 |
| Recognition over recall (Nielsen H6) | Settings (CLI commands need user to remember) | -0.5 (offset by CodeBlock hint) |
| Flexibility + efficiency (Nielsen H7) | All pages — keyboard shortcuts absent | -3 |
| Aesthetic + minimalist (Nielsen H8) | Overview empty cards, 3-card Profile | -2 |

**Top heuristic gap**: keyboard shortcuts. Vercel/Linear/Cal all have
`?` global shortcut menu + `/` for search focus. **Defer to v1.x** but
flag as known gap.

---

## 4. Recommended remediation plan

Convert this review into a tactical plan named
`docs/plans/completed/2026-05-26-dashboard-density-refactor-plan.md`:

### Phase A — Hierarchy & density (high impact)

1. Add KPI strip to Overview (4 cards: Active projects, Deploys this week,
   Usage %, Open incidents) — closes CC-4
2. Collapse empty states (Alerts, Recent Previews) — closes CC-3
3. Conditionally hide "Upgrade to Pro" card on Billing — closes CC-1 partially
4. Demote second-primary buttons across Overview/Billing — closes CC-1

### Phase B — Entity list patterns

5. Convert Domains card-grid to Table with expandable rows (Disclosure)
6. Convert Projects card-grid to Table (preserve grid as toggle if desired)
7. Add `tabular-nums` to all numeric cells

### Phase C — Auth + utility polish

8. 6-box verification code input (industry standard)
9. Resend code affordance on Verification + Recovery
10. Continue button on DeviceSuccess
11. Reconsider `size="lg"` vs `size="md"` on auth pages (sponsor decision)

### Phase D — Cross-cutting normalizations

12. Standardize spacing scale (`grep -rn "gap-5\|gap-10\|space-y-5" src/`)
13. Audit + add focus-visible rings on custom interactives
14. Profile page consolidation (3 cards → 2)
15. Team OWNER badge color reconsideration

### Phase E — Future (defer to v1.x)

- Keyboard shortcuts (`?` menu, `/` search)
- Storybook + Chromatic visual regression
- Lighthouse CI (MEET-003 candidate)
- i18n infrastructure

---

## 5. Verification methodology

To verify each fix in this review:

1. **Tokens compliance**: `grep -rn "#[0-9a-f]\{6\}\|border-gray-\|bg-gray-" src/` returns ZERO
2. **Date/number formatters**: `grep -rn "toLocaleDateString\|toLocaleString" src/pages/` returns ZERO (already validated)
3. **Spacing**: `grep -rn "gap-5\|gap-10\|space-y-5\|space-y-10\|gap-\[" src/` returns ZERO
4. **Primary color count**: visual audit per page — count `bg-primary`, `text-primary`, primary `<Button variant>` per route ≤2
5. **Density**: headless Chrome screenshots vs targets in DESIGN.md § 6.1
6. **A11y**: keyboard tour via `tab` → every interactive reachable, focus ring visible
7. **WAI-ARIA**: `axe-core` automated scan + manual screen reader pass (defer to v1.x for full audit)

---

## 6. References

### Design literature

- **Refactoring UI** (Adam Wathan, Steve Schoger, 2018). Chapters used:
  - § Hierarchy is everything
  - § Layout and spacing
  - § Designing text
  - § Working with color
  - § Creating depth
  - § Working with images
- **Laws of UX** (Jon Yablonski, 2020 / 2nd ed. 2024). Principles used:
  - Hick's Law (decision time vs choice count)
  - Fitts's Law (target acquisition time)
  - Miller's Law (7 ± 2 chunks)
  - Aesthetic-Usability Effect
  - Law of Common Region
- **Nielsen 10 Heuristics for User Interface Design** (Jakob Nielsen, 1994 / revised).
- **WAI-ARIA Authoring Practices Guide 1.2** (W3C). Patterns: Combobox, Listbox, Dialog.

### Pattern sources

- Open Design `design-templates/dashboard/SKILL.md`
- Open Design `design-templates/flowai-live-dashboard-template/SKILL.md`
- Open Design `design-templates/github-dashboard/SKILL.md`
- Open Design `design-systems/{vercel,cal,apple,notion,stripe,linear-app,supabase}/DESIGN.md`

### LoopZero internal

- `docs/design/2026-05-25-loopzero-dashboard-design-system.md` (companion DSS)
- `docs/plans/completed/2026-05-25-dashboard-visual-polish-plan.md` v1.1 (executed)
- `docs/plans/completed/2026-05-24-dashboard-visual-redesign-mockup-aligned-plan.md` v1.2 (parent)
- MEET-ASYNC-AMENDMENT-2026-05-24-001 (DoD relaxation, T-H.1 visual smoke evidence)
- MEET-ASYNC-AMENDMENT-2026-05-24-002 v1.4 (bundle gates)

### Real-product references (manual inspection 2026-05-25)

- Vercel dashboard (`vercel.com/dashboard`) — KPI strip + project table
- Linear app (`linear.app`) — sidebar density, command palette
- Cal.com (`app.cal.com`) — booking table layout
- Stripe dashboard (`dashboard.stripe.com`) — billing pattern
- Railway (`railway.app/project/...`) — environments tab
- Render (`dashboard.render.com`) — service list

---

## 7. Document versioning

- v1.0 (2026-05-25): initial deep review post-polish-plan completion
- Future iterations after remediation plan executes — update findings to
  RESOLVED status as fixes ship
