# LoopZero Dashboard — Design System

> **Slug:** `2026-05-25-loopzero-dashboard-design-system`
> **Status:** DRAFT (awaiting sponsor ratification)
> **Anchored to:** `@loopzerokit/ui@0.10.0-next.0` Violet Forge theme
> **Pattern source:** Open Design `design-templates/dashboard` SKILL +
> Open Design `design-systems/{vercel,cal,linear-app,supabase}` DESIGN.md +
> Refactoring UI (Wathan & Schoger) + Laws of UX (Yablonski) + WAI-ARIA APG
> **Document version:** v1.0
> **Audit companion:** `docs/reviews/2026-05-25-dashboard-deep-design-review.md`

---

## 0. Purpose & Scope

This document is the **single source of truth** for visual + behavioral
patterns of the LoopZeroCloud dashboard (`cloud/dashboard/`). It does NOT
redefine tokens already shipped by `@loopzerokit/ui` Violet Forge — it
**applies** those tokens to dashboard-specific surfaces.

**Rule of thumb**: if a token (color, spacing, radius, typescale entry)
exists in `@loopzerokit/ui`, USE IT verbatim. Never hex-literal,
never px-literal in component classes. If a token doesn't exist,
file a Brief to upstream (see Brief #4 precedent for the publishing-
pipeline gap that became `@loopzerokit/ui@0.10.0`).

**Out of scope:**
- @loopzerokit/ui primitive APIs themselves — read `node_modules/@loopzerokit/ui/dist/index.d.ts`
- Backend contracts — read `api/internal/`
- Auth flows — locked in `lib/{auth,api,kratos}.ts` per CLAUDE.md "NÃO TOCAR"

---

## 1. Visual Theme & Atmosphere

LoopZeroCloud's dashboard is the **operator's cockpit** for a developer
PaaS — the kind of surface devs return to between deploys, scaling
operations, and incident response. The visual posture mirrors the
product positioning: **"Vercel-quality DX for backends"**.

That means three things:

1. **Restrained density** — closer to Linear / Cal / Supabase than to
   Airtable / Notion. Every pixel earns its place. No marketing
   gradients, no decorative illustrations, no empty hero cards.
2. **Single-accent discipline** — the Violet Forge primary
   (`hsl(var(--primary))`) is the only accent. It appears at most
   2× per visible viewport: sidebar active nav + 1 primary CTA.
   Status colors (success/warning/destructive) are pastel pills,
   never saturated CTAs.
3. **Dark-mode native** — Violet Forge ships dark as the canonical
   mode. The dashboard never offers a theme switcher (per existing
   D8 ADR). Content density is tuned for dark surfaces (slightly
   lower text contrast, hairline borders prominent, glow effects
   minimized).

**Atmospheric metaphor**: an engineer's terminal window — calm
backdrop, structural typography, instruments arranged in a grid
where each instrument is at-a-glance scannable. Closer to OpenTUI /
Lazygit than to Stripe Atlas or Notion gallery.

### Anti-patterns the dashboard explicitly avoids

| Bad pattern | Why it's wrong here |
|-------------|---------------------|
| Big colorful KPI cards with gradient backgrounds | Reads as marketing landing page, not operator instrument |
| 200px-tall cards with a single metric + lots of whitespace | Wastes the viewport; reduces information-per-scroll |
| Multi-color status pills (red, yellow, blue, green) all at once | Color-blind unfriendly; semantic meaning collapses |
| Modal-heavy interactions for routine operations | Hick's Law violation; routine ops should be inline |
| Sidebar items with custom illustrations / icons-only-no-text | Increases cognitive load; mixed-mode nav |
| Different button sizes on the same form | Vertical rhythm broken (the bug `cb67866e` fixed) |

---

## 2. Color Palette & Roles

All colors below resolve to `@loopzerokit/ui` runtime CSS variables. Do
not write hex literals in component code; use the Tailwind utility
that resolves to the token (e.g., `bg-primary`, `text-muted-foreground`).

### 2.1 Primary surface

| Role | Tailwind utility | Token | Purpose |
|------|------------------|-------|---------|
| Page background | `bg-background` | `--background` | Sidebar + main canvas |
| Card surface | `bg-card` | `--card` | Panels, KPI cards, list rows |
| Popover surface | `bg-popover` | `--popover` | Dropdowns, tooltips, Select.Content |
| Muted surface | `bg-muted` | `--muted` | Code blocks, KBD backgrounds, search inputs |

### 2.2 Text scale

| Role | Tailwind utility | Token | Purpose |
|------|------------------|-------|---------|
| Primary text | `text-foreground` | `--foreground` | Headings, primary body, table cells |
| Muted text | `text-muted-foreground` | `--muted-foreground` | Descriptions, captions, helper text |
| On-primary | `text-primary-foreground` | `--primary-foreground` | Text inside primary CTAs |

### 2.3 Single accent (Violet Forge primary)

| Role | Tailwind utility | Token | Where it appears |
|------|------------------|-------|------------------|
| Primary action | `bg-primary` / `text-primary` | `--primary` | 1 CTA per visible area, sidebar active |
| Primary deep | `bg-primary-deep` | `--primary-deep` | Hover/pressed state |
| Primary glow | `bg-primary-glow` | `--primary-glow` | Focus ring + KPI bar fill |

**Discipline**: the primary color is used **at most 2× per viewport**
following Refactoring UI § "Use color sparingly". Counting:
- Sidebar active nav item = 1
- One primary CTA in the top-right of ActionBar = 1
- Status pills, links, badges: NOT primary. Use `text-info` / `text-success` / `text-warning` / `text-destructive` tokens.

### 2.4 Semantic palette (status, NOT decorative)

| Role | Tailwind utility | When |
|------|------------------|------|
| Success | `text-success` / `bg-success/10` | "Live", "Active", "Healthy" states |
| Warning | `text-warning` / `bg-warning/10` | "Pending", "Building", "Idle near quota" |
| Destructive | `text-destructive` / `bg-destructive/10` | "Failed", "Over quota", DangerZone |
| Info | `text-info` / `bg-info/10` | Informational alerts, neutral tags |

**Pill convention**: status pills use `bg-{role}/10` (10% tint of the
semantic color) + `text-{role}` + hairline border `border border-{role}/20`.
Never solid saturated backgrounds for non-action elements.

### 2.5 Borders & dividers

| Role | Tailwind utility | When |
|------|------------------|------|
| Standard hairline | `border-border` | Card outlines, table row dividers |
| Subtle divider | `border-border/40` | Internal section dividers in cards |
| Input border | `border-input` | Form fields (separate token because Violet Forge tunes input borders distinctly) |
| Focus ring | `ring-ring` | Keyboard focus 2px ring |

**Forbidden**: arbitrary border colors like `border-gray-400` or hex.

---

## 3. Typography Rules

`@loopzerokit/ui` ships Geist (display + body) + Geist Mono via
`@import "@loopzerokit/ui/fonts-cdn.css"`. The Violet Forge typescale
exposes compound utilities (`text-display-2xl` ... `text-label`)
with companion `--*--line-height`, `--*--letter-spacing`,
`--*--font-weight` properties Tailwind v4 picks up automatically.

### 3.1 Application hierarchy for dashboard surfaces

| Surface | Tailwind utility | Computed | Use |
|---------|------------------|----------|-----|
| Page title (TopBar) | `text-title-md font-display` | 20px, 1.4, -0.03em, 600 | "Overview", "Projects", per page |
| Section heading inside card | `text-title-lg` | 24px, 1.33, -0.04em, 600 | Card.Title in main panels |
| Card description | `text-body-sm text-muted-foreground` | 13px, 1.46, 400 | Card.Description |
| KPI value (large) | `text-headline font-display tabular-nums` | 28px, 1.25, -0.035em, 600 | "1.24M", "412 GB" |
| KPI label | `text-label-caps text-muted-foreground uppercase tracking-wider` | 11-12px caps + 0.06em | "USAGE", "PROJECTS" |
| Table header | `text-label-caps text-muted-foreground uppercase tracking-wider` | Same as KPI label | TH cells |
| Table cell | `text-body-md` | 14px, 1.43, 400 | TD cells |
| Inline code / KBD | `font-mono text-body-sm` | Geist Mono 13px | `loopzero deploy` snippets |
| Caption / hint | `text-label text-muted-foreground` | 14px, 400 | Helper text below inputs |

### 3.2 Numeric formatting

**Always use** `tabular-nums` class (or `font-variant-numeric: tabular-nums`)
for any displayed metric — KPI values, table cell numbers, usage
counters. This keeps digit columns aligned across rows.

**Always use** `lib/format.ts#formatNumber()` for thousand-separators
+ compact-millions:
- `compact`: `1.24M`, `87K`
- `grouped`: `1,240,000`
- `bytes`: `412 GB` (SI 1000-base per ADR-D4)
- `percent`: `42.3%`

**Never** inline `value.toLocaleString()` — it's browser-locale
dependent and produces drift (P-1/P-2 of plan v1.1).

### 3.3 Date formatting

**Always use** `lib/format.ts#formatDate(iso, intent)`:
- `'date'`: `Apr 10, 2026`
- `'datetime'`: `Apr 10, 2026, 6:42 AM`
- `'relative'`: `2 days ago` via `Intl.RelativeTimeFormat`
- `'long'`: `Sunday, April 10, 2026`

en-US locale hard-coded per ADR-D1 — defer i18n to v1.x.

### 3.4 Three-weight system (Vercel-inspired discipline)

- **400** — body, table cells, descriptions (read)
- **500** — nav items, button labels, links (interact)
- **600** — headings, KPI values, card titles (announce)

**Never** `font-bold` (700) for general UI. Weight 700 is reserved
for micro-badges (≤10px) and code keywords inside CodeBlock.

---

## 4. Component Stylings

These rules govern how `@loopzerokit/ui` primitives are USED on dashboard
surfaces. The primitives themselves are documented in `node_modules/
@loopzerokit/ui/dist/index.d.ts`. Read that first when in doubt.

### 4.1 Buttons

**Default size**: `size="md"` (40px tall) UNLESS the button anchors a
form with `Input size="lg"` (then match: `size="lg"`).

Variants ranked by visual weight (use sparingly to most):
- `variant="primary"` — solid primary background. **MAX 1 per visible area.**
- `variant="secondary"` — hairline border on muted bg. Most actions.
- `variant="ghost"` — no border, hover bg only. Icon-only buttons, sidebar items.
- `variant="destructive"` — solid destructive bg. DangerZone confirmations only.
- `variant="link"` — text-only, underline on hover. Inline links inside text.

**Sizing pairs** (visual rhythm):
- Standalone CTA in an ActionBar: `size="md"` matches default Input md
- Form submit at bottom of a `<form>`: match form Input size (md ↔ md, lg ↔ lg)
- Icon-only inside a row: `size="icon"` (square 32px)

**Forbidden**: `size="sm"` on primary actions in main content area
(too small for Fitts's Law). Acceptable in dense table-row inline
actions.

### 4.2 Inputs

**Default size**: `size="md"` (40px) matches Button md. Use `size="lg"`
**only** when paired with `Button size="lg"` in the same form (auth pages).

**Search inputs** (ActionBar, Sidebar "Find a project"): `size="md"`
with `<Search />` icon prepended via `className="pl-9"` or `pl-8`.
Smaller search inputs are conventional and OK to break the form-pair rule.

**Forbidden**: native `<select>`. Use @loopzerokit/ui `<Select>` compound
(`Select.Trigger` + `Select.Value` + `Select.Content` + `Select.Item`).

### 4.3 Cards

```tsx
<Card>
  <Card.Header>
    <Card.Title>...</Card.Title>
    <Card.Description>...</Card.Description>
  </Card.Header>
  <Card.Body>...</Card.Body>
</Card>
```

**Padding**: Card.Header + Card.Body inherit @loopzerokit/ui defaults.
**Do not override** with `className="p-8"` or custom padding (the
default is tuned for dashboard density). Only override when there's a
true visual gap to fix.

**Sizing**: Cards stretch to grid cell width. **Avoid** `max-w-sm` /
`max-w-md` on dashboard cards — let the grid system decide width.

**Forbidden card patterns**:
- Single KPI inside a 200px-tall card (use KPI strip primitive instead — see § 5.3)
- Card with only a button + lots of empty space (use direct Button placement)
- Nested cards (card inside card inside card)

### 4.4 Tables

```tsx
<Table>
  <Table.Header>
    <Table.Row>
      <Table.HeaderCell>NAME</Table.HeaderCell>
      ...
    </Table.Row>
  </Table.Header>
  <Table.Body>
    <Table.Row>...</Table.Row>
  </Table.Body>
</Table>
```

**Conventions:**
- Header cells: uppercase + `text-label-caps` + `tracking-wider`
- Body cells: `text-body-md` for primary content, `font-mono text-body-sm` for timestamps/IDs
- First column = identifier (project name, domain, member email)
- Last column = actions (icon buttons, status pills)
- Sticky header for tables ≥10 rows
- Row hover: subtle bg shift via @loopzerokit/ui default — don't override

**Density**: dashboard tables target **40-44px row height**. If a row
shows secondary metadata (e.g., framework + last deploy under project
name), use 2-line cell at 56px max. Triple-line rows = redesign signal.

### 4.5 Status pills (semantic, NOT primary)

```tsx
<Badge variant="success">Live</Badge>
<Badge variant="warning">Pending DNS</Badge>
<Badge variant="destructive">Failed</Badge>
<Badge variant="default">Idle</Badge>
```

OR if @loopzerokit/ui ships StatusDot (it does):

```tsx
<StatusDot status="live" label="Live" />
```

**Discipline**: pill text is **title-cased** ("Live", not "live" or
"LIVE"). Multi-word status uses `statusLabel()` helper:
`pending_verification` → `Pending Verification`.

### 4.6 Sidebar nav

`@loopzerokit/ui` ships `Sidebar` + `Sidebar.Item`. Convention:

- 6-8 nav items maximum (Miller's Law: 7 ± 2 chunks)
- Each item: icon (16px) + label
- Group dividers via `Sidebar.GroupLabel` for `BILLING` section
- Active state: bg-primary/10 + text-primary (the one place primary
  is allowed besides the CTA)
- Width: 220-260px (Open Design dashboard SKILL spec)

### 4.7 KPI cards (new pattern this DESIGN.md introduces)

For dashboards with numeric instruments, use the KPI strip pattern:

```tsx
<div className="grid grid-cols-4 gap-4">
  <Card>
    <Card.Body className="p-4">
      <p className="text-label-caps text-muted-foreground uppercase tracking-wider">MRR</p>
      <p className="text-headline font-display tabular-nums mt-1">$48.2K</p>
      <p className="text-label text-success mt-1">+12.4% MoM</p>
    </Card.Body>
  </Card>
  {/* 3 more */}
</div>
```

**Conventions:**
- 4-5 cards per row on desktop, 2 on tablet, 1 stacked on mobile
- Each card max 120px tall
- Label uppercase + muted; value `text-headline` tabular-nums;
  delta colored by good/bad direction (success / destructive)
- Primary color **NOT** used here — KPI strip is informational, not action

---

## 5. Layout Principles

### 5.1 App shell

```
┌──────────────────────────────────────────────────────────┐
│  TopBar  (h-14, border-b, project Select + page title)   │
├────────┬─────────────────────────────────────────────────┤
│        │                                                  │
│  Side- │                  Main                            │
│  bar   │                                                  │
│ (224px)│         (scrolls independently)                  │
│        │                                                  │
│  fixed │                                                  │
│        │                                                  │
└────────┴─────────────────────────────────────────────────┘
```

**Constraints:**
- Sidebar: 220-260px (DSS spec). Use `w-56` (224px) default.
- TopBar: `h-14` (56px), `border-b border-border/40`, sticky.
- Main: `p-6 md:p-8`, `overflow-y-auto`.
- No global horizontal scroll. Cards collapse to 1-col below 640px.

### 5.2 Content grid patterns

Per Open Design dashboard SKILL, prefer these layouts in order:

1. **KPI strip + 2fr:1fr panels** (operations dashboards):

```tsx
<div className="space-y-6">
  <KpiStrip />                                  {/* row 1: 4 KPI cards */}
  <div className="grid grid-cols-3 gap-6">      {/* row 2: 2fr:1fr */}
    <Card className="col-span-2">{/* primary chart/table */}</Card>
    <Card>{/* secondary list */}</Card>
  </div>
</div>
```

2. **Table-only page** (entity list — projects, domains, members):

```tsx
<>
  <ActionBar searchPlaceholder="..." primaryAction={...} />
  <Table>...</Table>
</>
```

3. **Form-only page** (settings, profile, auth):

```tsx
<ContentGrid columns="1" gap={6}>
  <Card>{/* form section 1 */}</Card>
  <Card>{/* form section 2 */}</Card>
  <DangerZone>{/* destructive actions */}</DangerZone>
</ContentGrid>
```

**Forbidden**: 2-asymmetric grid (`columns="2-asymmetric"`) with cards
that have ≥60% empty space. If a card is mostly empty, the layout
choice is wrong.

### 5.3 Spacing system

Anchored to Tailwind v4 default scale (4px base). Use only:

| Class | px | When |
|-------|----|------|
| `gap-1` / `space-y-1` | 4 | Inline icon + text |
| `gap-2` / `space-y-2` | 8 | Within table cells, badges |
| `gap-3` / `space-y-3` | 12 | Form field stack, table rows |
| `gap-4` / `space-y-4` | 16 | Card.Body content blocks |
| `gap-6` / `space-y-6` | 24 | Between cards in main grid |
| `gap-8` / `space-y-8` | 32 | Between major page sections (rare) |

**Forbidden**: `gap-5` (20px) — not in the rhythm; `gap-10` (40px) —
too much for dashboard density; arbitrary `gap-[18px]`.

### 5.4 Border radius

| Class | When |
|-------|------|
| `rounded-md` | Inputs, buttons, table cells (default) |
| `rounded-lg` | Pills, status dots, icon containers |
| `rounded-xl` | Cards (default from @loopzerokit/ui) |
| `rounded-full` | Avatars, circular icon buttons, kpi-bar terminators |

---

## 6. Density Discipline

This is the section that differentiates the LoopZero dashboard from
marketing-style admin panels. Refactoring UI § "Density" + Material
Design `MaterialDensity.compact` informed these targets.

### 6.1 Per-page vertical density targets

| Surface | Target | Max acceptable |
|---------|--------|----------------|
| Sidebar item | 36-40px tall | 44px |
| TopBar | 56px tall | 56px (fixed) |
| ActionBar | 56-64px tall | 72px |
| Table row (single-line) | 40-44px | 48px |
| Table row (2-line meta) | 56px | 64px |
| KPI card | 100-120px tall | 140px |
| Panel card (chart/list) | 280-360px tall | 480px |
| Form input + label | 64-72px stacked | 80px |

If a measurement exceeds the **Max acceptable** column, the design is
wrong — redesign before override-ing padding.

### 6.2 Information-per-viewport budget

A 1440×900 dashboard view should show, **without scrolling**:
- 4 KPIs OR 8-10 table rows OR 3 panel cards (mutually exclusive)
- Full sidebar nav
- Full TopBar with active page indicator
- ActionBar with primary action

If you can't fit that on Overview, the grid is too sparse.

### 6.3 Empty-state discipline

Empty states are inevitable (new account, filtered-no-match, etc.).
Conventions:

- Use `<EmptyState>` primitive — never roll custom
- Maximum **1 EmptyState per page**. If 2 sections are both empty,
  collapse into a single empty page state.
- Empty state height: **≤200px**. Don't reserve viewport for "future
  data that might come".
- Empty state CTA: at most 1 button (`variant="secondary"`).
  Multiple CTAs = decision paralysis (Hick's Law violation).

---

## 7. Do's and Don'ts

### Do

- **Use @loopzerokit/ui defaults first**. Override only with measured
  visual evidence (screenshot).
- **Pair Button + Input sizes** within a single form (md↔md, lg↔lg).
- **Title-case status labels** via `statusLabel()` helper.
- **Use `tabular-nums`** for any displayed metric.
- **Use `formatDate`/`formatNumber`/`maskEmail`** from `lib/format.ts`.
- **Limit primary color to 2 occurrences** per viewport (sidebar
  active + 1 CTA).
- **Use compound `Select.Trigger`/`Select.Content`/`Select.Item`** —
  never native `<option>` children.
- **Set focus rings** via `ring-ring` + `focus-visible:ring-2`.
- **Match component naming**: KPI strip = `<KpiStrip>`, not `<MetricRow>`.

### Don't

- **Don't hex-literal colors** in components. Use Tailwind utility
  resolving to `--color-*` tokens.
- **Don't `size="lg"` Buttons + `size="md"` Inputs** (the bug `cb67866e` fixed).
- **Don't use `border-gray-400`** or arbitrary Tailwind palette
  colors. Use `border-border` / `border-input` / `border-{role}/20`.
- **Don't write inline date/number formatters** (`new Date().toLocaleDateString()`).
- **Don't override Card padding** with `p-8` without a screenshot
  proving the default is wrong.
- **Don't put gradient backgrounds** on dashboard cards (marketing
  pattern, not operator pattern).
- **Don't use modals** for routine read/edit operations. Inline edit
  or full-page view instead.
- **Don't have more than one `variant="primary"` button** visible
  at once.
- **Don't pile status pills** with 4+ different semantic colors in
  the same view (cognitive overload).

---

## 8. Responsive Behavior

`@loopzerokit/ui` ships Tailwind v4 breakpoints. Dashboard uses these
breakpoints intentionally:

| Breakpoint | Width | Dashboard behavior |
|------------|-------|--------------------|
| Mobile | <640px | Sidebar collapses to drawer (Sheet primitive); KPI strip 1-col; tables horizontal scroll |
| Tablet | 640-1024px | Sidebar drawer; KPI strip 2-col; main panels 1-col stacked |
| Desktop | 1024-1440px | Sidebar fixed 224px; KPI strip 4-col; 2fr:1fr panels |
| Wide | ≥1440px | Same as desktop; no further grow — content max-width caps to avoid stretched rows |

**Touch targets**: minimum 40×40px on tablet/mobile per WCAG 2.5.5.
Icon-only buttons must be `size="icon"` (32px square) + provide
`aria-label`.

**Mobile navigation strategy**: bottom-tab nav NOT planned. Sidebar
becomes a hamburger-triggered Sheet. Most operator usage is desktop.

---

## 9. Distinctive LoopZero Patterns

These are unique to the LoopZeroCloud dashboard (not generic UI):

### 9.1 CLI-first philosophy on Settings

Settings page is **deliberately sparse** — most workspace configuration
goes through `loopzero` CLI. The page shows:
- A CodeBlock of common commands (`loopzero whoami`, `loopzero team list`, etc.)
- A DangerZone for "Delete team"

Adding more toggles to Settings is an **anti-pattern**. Anything
configurable belongs in `loopzero` CLI first, dashboard second.

### 9.2 Inline DNS records pattern (Domains)

When a domain is in `pending_verification` state, the row expands
inline with CNAME + TXT records + per-record CopyButton + a
CodeBlock of the equivalent CLI (`loopzero domain verify <domain>`).

This is the dashboard equivalent of "show the user how to act,
don't just tell them they have a problem." Pattern source: Refactoring
UI § "Don't tell, show".

### 9.3 Plan-aware upgrade affordances

Free/Hobby users see `<Badge variant="outline">Upgrade</Badge>` in
relevant locations (Overview Usage card action slot). Clicking
opens Billing page with upgrade CTA highlighted. Not aggressive,
not blocking — informational nudge.

### 9.4 Verification banner conditional

`<VerificationBanner>` shows globally (top of every authenticated
page) **only when** `isEmailVerified(session) === false`. Slim, single
CTA "Verify now" linking to Kratos self-service verification.
Auto-dismisses when verification completes.

---

## 10. Agent Prompt Guide (for future LLM-driven edits)

### Quick reference

- **Primary color uses**: sidebar active + 1 CTA per viewport. MAX 2.
- **Card padding**: use @loopzerokit/ui defaults. Don't override `p-8` or `p-4`.
- **Spacing scale**: 4, 8, 12, 16, 24, 32. Skip 5, 10, 20.
- **Status pills**: `<Badge variant={success|warning|destructive|default}>` with title-cased label.
- **Numeric display**: `formatNumber(v, intent) + tabular-nums`.
- **Date display**: `formatDate(iso, intent)`. NEVER `toLocaleDateString`.
- **Form pairing**: same `size` on Button + Input within one form.

### Example: "Add a KPI to Overview"

```tsx
<Card>
  <Card.Body className="p-4">
    <p className="text-label-caps text-muted-foreground uppercase tracking-wider">
      Deploys this week
    </p>
    <p className="text-headline font-display tabular-nums mt-1">
      {formatNumber(count, 'grouped')}
    </p>
    <p className="text-label text-success mt-1">
      +{delta} vs last week
    </p>
  </Card.Body>
</Card>
```

### Example: "Add a table row for domains"

```tsx
<Table.Row>
  <Table.Cell className="font-medium">{domain.name}</Table.Cell>
  <Table.Cell className="font-mono text-body-sm text-muted-foreground">
    {domain.project}/{domain.app}
  </Table.Cell>
  <Table.Cell>
    <Badge variant={statusVariant(domain.status)}>
      {statusLabel(domain.status)}
    </Badge>
  </Table.Cell>
  <Table.Cell>{formatDate(domain.verified_at, 'date')}</Table.Cell>
</Table.Row>
```

### Example: "Add a search action bar"

```tsx
<ActionBar
  searchPlaceholder="Search projects..."
  searchValue={query}
  onSearchChange={setQuery}
  primaryAction={{
    label: 'New project',
    icon: Plus,
    onClick: () => navigate('/projects/new'),
  }}
/>
```

---

## 11. References

- @loopzerokit/ui Violet Forge token source: `node_modules/@loopzerokit/ui/dist/tokens-v4.css`
- @loopzerokit/ui primitive APIs: `node_modules/@loopzerokit/ui/dist/index.d.ts`
- Open Design dashboard SKILL: `cloud/dashboard/referencia/open-design/design-templates/dashboard/SKILL.md`
- Open Design DESIGN.md references: `vercel/`, `cal/`, `apple/`, `notion/`, `stripe/`
- Refactoring UI (Adam Wathan, Steve Schoger) — § Hierarchy, § Color, § Density, § Whitespace
- Laws of UX (Jon Yablonski) — Hick's, Fitts's, Miller's, Aesthetic-Usability Effect
- Nielsen Heuristics (10 usability principles) — esp. visibility of system status, recognition over recall
- WAI-ARIA Authoring Practices Guide — Combobox, Listbox, Dialog patterns
- Material Design density specs — `MaterialDensity.compact` vs `regular` vs `comfortable`
- Prior plans/MEETs: `2026-05-24-dashboard-visual-redesign-mockup-aligned-plan.md`,
  `2026-05-25-dashboard-visual-polish-plan.md`, MEET-ASYNC-001/002

---

## 10. Motion System (NEW v1.1)

Per Linear pattern + ADR D7 of plan FAANG v1.2. Tokens em `cloud/dashboard/src/index.css`.

### Tokens

| Token | Value | Use |
|-------|-------|-----|
| `--motion-instant` | 0ms | Immediate response (focus ring, active state) |
| `--motion-fast` | 90ms | Hover transitions, color shifts |
| `--motion-base` | 180ms | Default transition (Linear standard) |
| `--motion-slow` | 320ms | Modal enter/exit, large surface transitions |
| `--easing-standard` | `cubic-bezier(0.2, 0, 0, 1)` | Default easing |
| `--easing-decelerate` | `cubic-bezier(0, 0, 0, 1)` | Enter transitions |
| `--easing-accelerate` | `cubic-bezier(0.3, 0, 1, 1)` | Exit transitions |

### Accessibility — `prefers-reduced-motion` (WCAG 2.3.3)

When user OS preference is `reduce-motion`, all `--motion-*` tokens collapse to `0ms` + universal selector overrides `animation-duration` + `transition-duration` to `0ms !important`. This catches Tailwind `animate-*` utilities + any inline transitions.

### Usage examples

```tsx
<div className="transition-colors" style={{ transitionDuration: 'var(--motion-base)', transitionTimingFunction: 'var(--easing-standard)' }}>
```

OR via Tailwind utilities (when motion duration utility is added — Phase 5 follow-up):
```tsx
<div className="transition-colors duration-base ease-standard">
```

## 11. Token Deprecation Policy (NEW v1.2 — ADR D13 / G13 FAANG gap)

> Per FAANG best practice (Linear 4-tier deprecation + Material Design 3 versioning). Token changes are NOT silent — they follow explicit lifecycle stages so consumers (cloud/dashboard, future @loopzerokit/ui consumers) have migration runway.

### 4-tier lifecycle

| Tier | Status | Behavior | Duration |
|------|--------|----------|----------|
| **1. Active** | `Stable` | Token rendered as documented; consumers use freely | Indefinite |
| **2. Deprecated** | `Soft warn` | Token still rendered + functional; `@deprecated` annotation em DSS doc; CHANGELOG entry "Will be removed in v{N+1}.0" | Min 1 minor version |
| **3. Removed-Next-Minor** | `Hard warn` | Token rendered with console.warn em dev mode; CI gate `pkg/quality/deprecated-tokens` lança `warning` (não-blocker); CHANGELOG "Removed in v{this+1}.0" | 1 minor version |
| **4. Removed** | `Gone` | Token classe Tailwind / CSS var não existe mais; consumer build fail; CHANGELOG "Removed v{N}" | Permanent |

### Semver alignment

- **MINOR** (0.x.0): can move tokens from Tier 1 → Tier 2 (Deprecated). Add new tokens freely.
- **MINOR** following: Tier 2 → Tier 3 (Removed-Next-Minor with warn).
- **MAJOR** (X.0.0): Tier 3 → Tier 4 (Removed). Breaking change documented.

### Communication channels

- DSS doc § change log mark `[DEPRECATED]` next to token name + target version
- `@loopzerokit/ui` upstream CHANGELOG.md per release
- Consumer projects: `pkg/quality/deprecated-tokens` CI gate (future task; not yet implemented)

### Example (hypothetical)

If `--color-surface-3` were renamed `--color-elevation-3`:
- v0.13.0: `--color-surface-3` Tier 2 (Deprecated). DSS notes "renamed → use --color-elevation-3". Both work.
- v0.14.0: `--color-surface-3` Tier 3 (warn em dev console). `[loopzero-ui] --color-surface-3 deprecated, use --color-elevation-3`.
- v1.0.0: `--color-surface-3` Tier 4 (removed). Consumer build fails.

### Current deprecations

None. DSS v1.0/v1.1/v1.2 was first formal version stack — no deprecations yet. This policy applies forward.

### Enforcement

- **Author responsibility:** any DSS PR adding/renaming/removing token MUST update Change log + § 11 if Tier transition
- **Consumer responsibility:** read CHANGELOG.md + DSS § 11 before bumping @loopzerokit/ui version
- **CI (future):** `pkg/quality/deprecated-tokens` scans consumer code for Tier 3 tokens, emits warning

## 12. Versioning

This document is versioned. Changes go through:

1. Sponsor ratification (status pending → ratified)
2. Companion audit re-run if any token/component rule changes
3. Reference page edits if pattern is added/removed

**Change log:**

- v1.0 (2026-05-25): initial draft post-polish plan completion (commits 15ec00b8 → b67b8550)
- v1.1 (2026-05-25 — plan FAANG v1.2 Phase 2): added § 10 Motion System per ADR D7 + EC-3. CSS impl em cloud/dashboard/src/index.css (commit f1e08c0a).
- v1.2 (2026-05-25 — plan FAANG v1.2 Phase 5): added § 11 Token Deprecation Policy per ADR D13 + G13 FAANG gap. 4-tier semver-aligned lifecycle (commit pending).
