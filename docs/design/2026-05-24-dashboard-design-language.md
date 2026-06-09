# LoopZeroCloud Dashboard — Design Language

> **Status**: canonical reference for `cloud/dashboard/` visual redesign per plan `2026-05-24-dashboard-visual-redesign-mockup-aligned-plan.md v1.2`.
> **Mockup reference**: `cloud/dashboard/design/loopzero_overview_dashboard_mockup.html` (Vercel-style overview page).
> **Library reference**: `cloud/dashboard/.llms.txt` (`@loopzerokit/ui@0.9.0-next.0`, sync 2026-05-24).
> **Authority**: any divergence from this doc requires RFC + sponsor approval.

## Principles

1. **Geist + Violet Forge** — typography Geist Sans + Geist Mono, palette Violet Forge dark theme. No DM Sans/JetBrains Mono/Cabinet Grotesk (retired).
2. **Token-only styling** — never raw hex/rgb in JSX. Tokens from `@loopzerokit/ui` (`bg-card`, `text-foreground`, `border-border/40`, `bg-primary/10`, etc.).
3. **Compose primitives** — never reinvent Button/Badge/Card; never inline SVG (use Lucide). Per `.llms.txt` line 118.
4. **Layouts via DashboardLayout + ActionBar + ContentGrid** — never roll layout per page.
5. **Sub-component pattern** — `<Card.Header>`, `<Card.Body>`, `<Sidebar.Item>`, `<Table.Row>` (not `CardHeader`/`SidebarItem`).

---

## Pattern 1 — Layout

`<DashboardLayout>` is the shell for all 8 protected dashboard pages. Auth pages do NOT use it.

```
┌────────────┬───────────────────────────────────┐
│ AppSidebar │ TopBar (h-14)                     │
│ (w-220px)  ├───────────────────────────────────┤
│            │ <main> p-6                        │
│ fixed-left │   {pageContent}                   │
│            │ </main>                           │
└────────────┴───────────────────────────────────┘
```

Composition:
```tsx
<DashboardLayout pageTitle="Overview">
  <ActionBar searchPlaceholder="..." primaryAction={{...}} />
  <ContentGrid columns="2-asymmetric">{cols}</ContentGrid>
</DashboardLayout>
```

`pageTitle` set via `useSetPageTitle('Overview')` hook in page mount.

---

## Pattern 2 — Sidebar

`<AppSidebar>` wraps `@loopzerokit/ui` `<Sidebar>` primitive.

```
┌─────────────────────┐
│ Sidebar.Header      │
│  AccountMenu        │  ← avatar + name + PlanBadge
│  (clickable)        │
├─────────────────────┤
│ Sidebar.Section     │
│  Input (search)     │  ← search projects local
├─────────────────────┤
│ Sidebar.Section     │
│  Overview      ◀    │  ← active state primary tint
│  Projects           │
│  Environments       │
│  Domains            │
│  Team               │
├─────────────────────┤
│ Sidebar.Section     │
│ title="Billing"     │
│  Billing            │
├─────────────────────┤
│ Sidebar.Footer      │
│  Settings           │
│  Profile            │
└─────────────────────┘
```

Width: `w-[220px]` (override default 260px to match mockup denser feel).

Items use `Sidebar.Item` with `active` derived from `useLocation().pathname`. SPA navigation MUST use `as={Link}` from react-router OR wrap manually — NEVER `as="a" href="..."` (full reload, loses JWT in memory). See D-1 of edge cases.

Nav vocabulary = real LoopZeroCloud routes only (D1 ADR). No AI Gateway/Firewall/Speed Insights from template.

---

## Pattern 3 — TopBar

`<TopBar>` is the horizontal strip above main content area. `h-14`, border-bottom.

```
┌───────────────────────────────────────────────────────┐
│ [All Projects ▾]      Overview      [⋯]              │
└───────────────────────────────────────────────────────┘
   left slot           center slot     right slot
```

- **Left**: `<Select>` project switcher (consumes `use-projects.ts` cached fetch). Hidden via `showProjectSelector={false}` on Settings/Profile/Billing.
- **Center**: page title from `usePageMeta()`.
- **Right**: optional `<DropdownMenu>` with action items (destructive variant tinted).

---

## Pattern 4 — ActionBar

Used in: Projects, Environments, Team, Domains.

```tsx
<ActionBar
  searchPlaceholder="Search projects..."
  searchValue={query} onSearchChange={setQuery}
  onFilterClick={openFilters}
  primaryAction={{ label: 'Add New', icon: Plus, onClick: openCreate }}
/>
```

Renders:

```
[🔍 Search projects.........] [⚙ filter] ··· [+ Add New ▾]
                                              ^primary
```

`mt-6 mb-8`, full-width. Filter button optional; primary action right-aligned.

---

## Pattern 5 — ContentGrid

Wraps page content into responsive grid. Mockup uses 2-col 1.2fr:1fr.

API:

```tsx
<ContentGrid columns="2-asymmetric" gap={6}>
  <div>{leftCol}</div>
  <div>{rightCol}</div>
</ContentGrid>
```

Column variants:
- `1` → `grid grid-cols-1`
- `2-equal` → `grid grid-cols-1 lg:grid-cols-2`
- `2-asymmetric` → `grid grid-cols-1 lg:grid-cols-[1.2fr_1fr]` (mockup default)
- `3-equal` → `grid grid-cols-1 md:grid-cols-3`

Mobile-first: always single col below `lg` breakpoint.

---

## Pattern 6 — Card

Use `<Card>` primitive from `@loopzerokit/ui` with sub-components:

```tsx
<Card>
  <Card.Header>
    <Card.Title>Section title</Card.Title>
    <Card.Description>optional subtitle</Card.Description>
  </Card.Header>
  <Card.Body>
    {content}
  </Card.Body>
</Card>
```

Sub-component naming: `Card.Body` (NOT `Card.Content`) per `@loopzerokit/ui` Card.tsx.

Token defaults: `rounded-xl border bg-card border-border/40 p-4`. Override via `className` only when justified.

Section labels above card: `text-label-caps text-muted-foreground uppercase mb-3` (per mockup style).

---

## Pattern 7 — StatusDot

For visual indicators (deploy state, env health, domain verification).

```tsx
<StatusDot status="live" label="Live" />     {/* green pulse */}
<StatusDot status="building" />              {/* amber auto-pulse */}
<StatusDot status="failed" />                {/* red */}
<StatusDot status="idle" />                  {/* muted */}
<StatusDot status="warning" />               {/* amber static */}
```

5 status kinds. NEVER hand-roll `<span className="w-2 h-2 rounded-full bg-success">`. Always StatusDot primitive (Brief #2).

---

## Pattern 8 — Typography

Geist Sans + Geist Mono via `@loopzerokit/ui/fonts-cdn.css`. Tokens:

| Use | Token |
|---|---|
| Body text | `text-body-sm font-sans` |
| Section heading | `text-label-caps font-sans uppercase tracking-wider` |
| Page title | `text-title-sm font-display font-semibold` |
| Big stat number | `text-display-md font-display font-bold tabular-nums` |
| Inline code/identifier | `font-mono text-label` |
| Timestamp/numeric value | `font-mono tabular-nums` (always for column alignment) |

Color hierarchy:
- `text-foreground` — primary text
- `text-muted-foreground` — secondary, labels, descriptions
- `text-primary` — accent CTA, active states
- `text-destructive` — errors, danger zones
- `text-success`, `text-warning` — semantic status

Never `text-white` (use `text-foreground`). Never `text-gray-400` (use `text-muted-foreground`). Migration enforced via T-H.2 grep.

---

## Spacing Scale

| Use | Token |
|---|---|
| Sidebar items vertical | `gap-2` |
| Cards horizontal in grid | `gap-3` to `gap-6` (via ContentGrid prop) |
| Sections vertical | `gap-6` to `space-y-6` |
| Inside card padding | `p-4` (compact) or `p-5` (default) |
| Page outer padding | `p-6` (main element) |
| Top bar height | `h-14` (56px) |
| Sidebar width | `w-[220px]` |

---

## Empty State

Use `<EmptyState>` primitive:

```tsx
<EmptyState
  icon={FolderKanban}
  title="No projects yet"
  description={<>Run <code className="bg-muted px-2 py-0.5 rounded font-mono">loopzero init</code> to create your first.</>}
  action={<Button>Get started</Button>}
/>
```

Centered, `py-8`, optional CTA. Lucide icon (`aria-hidden="true"`). NEVER inline SVG.

---

## Cross-Page Consistency Rules

1. All 8 dashboard pages MUST use `<DashboardLayout>` shell.
2. All pages MUST call `useSetPageTitle(...)` in mount.
3. Section labels (uppercase) precede each card group.
4. Tokens-only color/spacing. Tailwind utilities from `@loopzerokit/ui` preset.
5. Status uses `StatusDot` primitive. Timestamps use `Timestamp` primitive (relative format default).
6. Destructive actions ALWAYS confirm via `ConfirmDialog` with `confirmationPhrase` for irreversible cases.
7. Tables use `<Table>` with `numeric` prop on amount/date columns for alignment.

---

## Anti-Patterns (Forbidden)

Per `.llms.txt` lines 125-138 + this dashboard scope:

- ❌ Raw hex/rgb in JSX (`bg-[#0A0A0A]`, `text-[rgb(255,255,255)]`)
- ❌ Inline `<svg>` (use Lucide React)
- ❌ `text-white`, `text-gray-*`, `bg-surface-[0-4]` (use semantic tokens)
- ❌ Hand-rolled status dots (`<span className="w-2 h-2 rounded-full bg-green-500">`)
- ❌ Hand-rolled card layout (`<div className="rounded-xl border ...">`)
- ❌ Hand-rolled relative timestamps (`{getRelativeTime(date)}`)
- ❌ Hand-rolled clipboard buttons
- ❌ Manual Portal/Dialog from Radix without using `@loopzerokit/ui` Dialog
- ❌ Tailwind raw `text-xs|sm|base|lg|xl` (use semantic scale `text-body-sm|label|label-caps|title-sm|display-md`)
- ❌ Disabling lint rules without WAI-ARIA justification

---

## Evolution

Changes to this doc require:

1. RFC short doc in `docs/design/rfcs/YYYY-MM-DD-<change>.md` proposing pattern change
2. Sponsor approval
3. Update this doc + commit message references the RFC
4. Re-validate all 8 pages for compliance via T-H.2 grep

Per `.llms.txt` line 268: when consumer docs (this) disagree with `@loopzerokit/ui` source files, source files win — update doc, not code.

---

**Generated**: 2026-05-24 from `loopzero_overview_dashboard_mockup.html` + `.llms.txt` cross-walk per plan v1.2 T-B.1.
