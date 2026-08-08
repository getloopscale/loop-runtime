---
type: handoff
title: "Brief #5: 3 dashboard primitives that unblock measured Deep Review findings"
---

# Brief #5: 3 dashboard primitives that unblock measured Deep Review findings

> **Audience:** the `@loopzero/ui` maintainer team (or a Claude/Codex agent in the `loopzero-ui` repo).
> **Output:** 3 new primitives + tests + stories + barrel exports + subpath exports + shadcn registry entries.
> **Issued by:** LoopZeroCloud dashboard team (consumer), 2026-05-25.
> **Priority:** MEDIUM — each primitive **closes a specific Deep Review finding** documented in [`docs/reviews/2026-05-25-dashboard-deep-design-review.md`](../reviews/2026-05-25-dashboard-deep-design-review.md) (42 findings, anchored to [`docs/design/2026-05-25-loopzero-dashboard-design-system.md`](../design/2026-05-25-loopzero-dashboard-design-system.md) v1.0). Ship when convenient — no blocker on consumer-side timeline.
> **Companion to:** Brief #1 ([`2026-05-23-loopzero-ui-cloud-dashboard-gaps-brief.md`](./2026-05-23-loopzero-ui-cloud-dashboard-gaps-brief.md)) + Brief #2 (`...gaps-brief-2.md`) + Brief #3 (`...gaps-brief-3.md`) + Brief #4 ([`2026-05-24-loopzero-ui-subpath-tree-shaking-brief-4.md`](./2026-05-24-loopzero-ui-subpath-tree-shaking-brief-4.md)).
> **Tracks:** Deep Review § 2.4 (Domains), § 2.12 (Verification), and CC-cross-cutting findings about boilerplate duplication across 13 dashboard pages.

---

## Why this brief exists

`@loopzero/ui@0.10.0-next.0` shipped Brief #4 (subpath tree-shaking) in <24h — the velocity proved the upstream workflow scales. With the bundle now properly tree-shaking, the next bottleneck moved to **missing primitives that force us to either keep card-grid layouts or roll bespoke components**.

I audited the 13-route LoopZeroCloud dashboard against our Design System v1.0 + Refactoring UI + Laws of UX + Nielsen Heuristics (42 findings logged in our Deep Review). The 3 primitives requested below **each map to a specific measured P1/P2 finding**. Without them, that finding stays open — no amount of dashboard-side composition fixes it.

Hard discipline applied:
- **NOT requested**: components I can compose from existing primitives (Card variants, layout helpers, inline edit at MVP scope, Combobox until >10 items in a list)
- **NOT requested**: speculation about future features
- **REQUESTED**: only primitives where workaround = "redesign the entire feature in a way that violates Design System"

---

## Quality bar (same as Briefs #1/#2/#3)

Every new primitive MUST:

- `forwardRef<...>` + `displayName`
- `cn()` from `lib/cn.js` for class composition
- lucide-react icons (peer-dep) — never inline SVG
- Design tokens only — never raw hex/rgb
- Typography scale (`text-body-sm`, `text-label-caps`, `font-mono`, `font-display`)
- Ship `<component>.tsx` + `<component>.test.tsx` + `<component>.stories.tsx` + `index.ts`
- Pass `pnpm test` (axe-validated via vitest-axe)
- Pass `pnpm typecheck`
- Add to `src/index.ts` barrel + `package.json` `"exports"` map + per-component dist (post-0.10) + `registry/{slug}.json`
- CHANGELOG `[Unreleased]` entry per component
- Bundle delta within reason — the post-0.10 splitting model means each new primitive ships as its own chunk; total brotli delta in LoopZeroCloud should be measurable per `size-limit`

---

## Component 1 — `PinInput`

### Why

The LoopZeroCloud Verification page (`/verification`) currently uses a single text input for a 6-digit email verification code. Industry standard (Apple ID, Stripe, Clerk, Auth0, GitHub two-factor) is **N separate boxes** that auto-advance focus on input. Without this primitive, the page reads as off-brand vs every other modern SaaS auth surface.

Distinct from `Input`:
- Multiple slots that auto-advance focus
- Paste handling that fills all slots from clipboard
- numeric / alphanumeric / OTP-friendly inputMode
- Visual treatment: each slot is its own box, not connected
- Built-in `onComplete(code)` callback when all slots filled

### File location

```
src/components/primitives/pin-input/
  ├── index.ts
  ├── pin-input.tsx
  ├── pin-input.test.tsx
  └── pin-input.stories.tsx
```

### API

```tsx
import { forwardRef, useEffect, useRef, useState } from "react";
import type { ClipboardEvent, KeyboardEvent } from "react";
import { cn } from "../../../lib/cn.js";

export interface PinInputProps {
  /** Number of slots. Common: 4, 6, 8. */
  length?: number;
  /** Current value (controlled). When length reached, triggers onComplete. */
  value?: string;
  /** Change handler. Receives full concatenated string. */
  onChange?: (value: string) => void;
  /** Fired when value.length === length. */
  onComplete?: (value: string) => void;
  /** Input mode for mobile keyboards. */
  inputMode?: "numeric" | "alphanumeric";
  /** Visual size matching @loopzero/ui Input. */
  size?: "sm" | "md" | "lg";
  /** Disabled state. */
  disabled?: boolean;
  /** Error state — applies destructive border. */
  error?: boolean;
  /** Accessible label. Required. */
  "aria-label": string;
  /** Auto-focus the first slot on mount. */
  autoFocus?: boolean;
  /** Mask the digits as bullets (for passwords). */
  mask?: boolean;
}

declare const PinInput: React.ForwardRefExoticComponent<
  PinInputProps & React.RefAttributes<HTMLDivElement>
>;
```

### Behavior

- **Auto-advance**: typing a digit moves focus to next slot
- **Backspace**: empties current slot AND moves focus back if already empty
- **Paste**: pasting "123456" with cursor in slot 0 fills all 6 slots and fires onComplete
- **Arrow keys**: left/right navigate between slots
- **Tab**: standard tab order — escapes the input group
- **inputMode="numeric"**: triggers numeric keyboard on mobile (`pattern="[0-9]*"`)
- **inputMode="alphanumeric"**: full keyboard, auto-uppercase
- **Error state**: applies `border-destructive` + shake animation (subtle)

### Tests

```ts
test("PinInput auto-advances focus on digit input");
test("PinInput backspace empties current slot then moves focus back");
test("PinInput paste fills all slots from clipboard");
test("PinInput fires onComplete when all slots filled");
test("PinInput respects inputMode=numeric on mobile");
test("PinInput respects mask prop (renders bullets)");
test("PinInput has no axe violations");
test("PinInput length=4 renders 4 slots");
test("PinInput error state applies destructive border");
```

### Acceptance criteria

- [ ] Renders N slots per `length` prop (default 6)
- [ ] Auto-advance + backspace + arrow + paste all work per spec
- [ ] `onComplete(value)` fires exactly once when value reaches length
- [ ] Tab order: enters first slot, exits last slot
- [ ] axe a11y clean (each slot has unique `aria-label="Digit N of M"`)
- [ ] Visual size matches `<Input size="lg">` when `size="lg"` (vertical rhythm pair)
- [ ] Stories cover: numeric/alphanumeric/error/disabled/mask/autoFocus

### LoopZeroCloud Deep Review finding this closes

§ 2.12 P2: "Code input is single field — for a 6-digit code, **6 separate boxes** is conventional UX (Apple, Stripe, Clerk all do this). Use a 6-box code input."

---

## Component 2 — `DataTable` (composition enhancement)

### Why

LoopZeroCloud Domains and Projects pages currently render **card-grids** (each entity = ~140px tall card). The Deep Review Top-5 fix #2 says "convert card-grid to Table" because tables give 50% vertical space savings + better scannability for entity lists. **BUT** our current `<Table>` primitive is a thin styled wrapper — no sortable columns, no sticky header, no expandable rows (needed for Domains DNS records inline), no built-in row actions menu.

We need a `DataTable` composite that solves the operator-grade entity-list pattern in one place.

Distinct from `<Table>`:
- `<Table>` (Brief #2) = primitive markup wrapper. Stays.
- `<DataTable>` (this brief) = composite built ON TOP of `<Table>` that adds: column definitions, sortable headers, sticky header, expandable rows, row actions, optional pagination.

Inspired by:
- TanStack Table (headless data-table library — for reference, NOT a dependency)
- Linear's table rendering in /issues
- Vercel dashboard project table
- Refactoring UI § "Tables"

### File location

```
src/components/composites/data-table/
  ├── index.ts
  ├── data-table.tsx
  ├── data-table.test.tsx
  └── data-table.stories.tsx
```

### API

```tsx
export interface DataTableColumn<T> {
  /** Stable key for the column (used in sort state). */
  key: string;
  /** Header label (string OR ReactNode for icons). */
  label: ReactNode;
  /** Column alignment. */
  align?: "left" | "center" | "right";
  /** Enable click-to-sort on the header. */
  sortable?: boolean;
  /** Width hint — '120px', '20%', or 'auto' (default). */
  width?: string;
  /** Cell renderer. Default: `(row) => String(row[key])`. */
  render?: (row: T) => ReactNode;
  /** Cell className override. Use for `font-mono`, `tabular-nums`, etc. */
  className?: string;
}

export interface DataTableProps<T> {
  /** Row data. */
  data: T[];
  /** Column definitions. */
  columns: DataTableColumn<T>[];
  /** Function to derive a stable React key per row. */
  rowKey: (row: T) => string;
  /** Sticky header on scroll. Default true. */
  stickyHeader?: boolean;
  /** Expandable row renderer. Return null = row not expandable. */
  expandable?: (row: T) => ReactNode | null;
  /** Row actions menu (rendered in last column if defined). */
  rowActions?: (row: T) => ReactNode;
  /** Pagination — set null to disable. */
  pagination?: { pageSize: number } | null;
  /** Loading state — renders skeleton rows. */
  loading?: boolean;
  /** Empty state when data.length === 0. */
  emptyState?: ReactNode;
  /** Initial sort. */
  defaultSort?: { key: string; direction: "asc" | "desc" };
}

declare function DataTable<T>(props: DataTableProps<T>): JSX.Element;
```

### Behavior

- **Sortable**: click header arrow toggles asc/desc/none cycle. Sort is client-side by default (controlled mode opt-in via `onSortChange`).
- **Sticky header**: `position: sticky; top: 0` with `bg-card` + `border-b border-border/40`. Stays in viewport when scrolling.
- **Expandable**: if `expandable(row) !== null`, row gets `<ChevronDown />` toggle in first column. Expanded content renders in a row below spanning all columns. Pattern: Radix Disclosure semantically.
- **Row actions**: appears as `<MoreHorizontal />` icon button in last column → opens `DropdownMenu` with the provided ReactNode.
- **Pagination**: bottom of table, `<Pagination>` primitive (Brief #3) reused.
- **Loading**: renders 5 skeleton rows with shimmer animation.
- **Empty**: renders `emptyState` ReactNode (or default `<EmptyState>` if not provided).

### Tests

```ts
test("DataTable renders columns and rows from data");
test("DataTable sticky header stays in viewport on scroll");
test("DataTable sortable column cycles asc → desc → none");
test("DataTable expandable row reveals expanded content");
test("DataTable expandable row only renders chevron when expandable() returns non-null");
test("DataTable rowActions renders menu trigger in last column");
test("DataTable pagination shows N items per page");
test("DataTable loading state renders 5 skeleton rows");
test("DataTable empty state renders when data.length === 0");
test("DataTable defaultSort respected on first render");
test("DataTable has no axe violations");
```

### Acceptance criteria

- [ ] Sortable + sticky + expandable + actions + pagination all working independently
- [ ] Compound API matches @loopzero/ui pattern (i.e., NOT `<DataTable.Header>` — single component with props)
- [ ] Type-safe via generics (`DataTable<Domain>`, `DataTable<Project>`)
- [ ] Built on existing `<Table>` primitive (don't duplicate markup)
- [ ] Reuses `<Pagination>`, `<EmptyState>`, `<Skeleton>`, `<DropdownMenu>` from prior briefs
- [ ] Stories cover: minimal / sortable / expandable (Domains DNS records pattern) / paginated / loading / empty

### LoopZeroCloud Deep Review finding this closes

- § 2.4 P1: "Card-per-domain layout is sparse for entities — Table is more scannable. **Switch to Table with expandable row (Disclosure pattern) for pending verification**."
- § 2.2 P1: "Card-grid layout for entity list — projects ARE entities, table is more scannable for 5+ items."
- Top-5 fix #2: "Convert Domains + Projects from card-grid to table" — unlocks 50% vertical space savings.

---

## Component 3 — `PageShell` (composite)

### Why

All 13 dashboard pages follow the same boilerplate pattern:

```tsx
export function SomePage() {
  useRequireAuth();
  useSetPageTitle("Some Page");
  const { data, loading } = useEngineApi<...>("/api/v1/...");
  const [query, setQuery] = useState("");

  return (
    <>
      <ActionBar
        searchPlaceholder="Search..."
        searchValue={query}
        onSearchChange={setQuery}
        primaryAction={{ label: "...", icon: Plus, onClick: () => ... }}
      />
      {loading ? <SpinnerCard /> : items.length === 0 ? <EmptyState ... /> : <ContentGrid>...</ContentGrid>}
    </>
  );
}
```

That ~20 LOC pattern lives in 13 files = ~260 LOC of duplication. Worse: each page reimplements loading + empty state slightly differently, causing the inconsistencies the Deep Review flagged in CC-3 (empty-state density violations).

A `PageShell` composite would dedupe this AND enforce DSS § 5.2 patterns.

Distinct from `<ActionBar>`:
- `<ActionBar>` is a UI strip (just header bar with search + CTA)
- `<PageShell>` is a page-level scaffold that **owns** the page layout: ActionBar at top, loading/empty/error state handling, content slot, and DSS § 5.2 pattern enforcement

### File location

```
src/components/composites/page-shell/
  ├── index.ts
  ├── page-shell.tsx
  ├── page-shell.test.tsx
  └── page-shell.stories.tsx
```

### API

```tsx
export interface PageShellProps {
  /** Page title. Sets browser tab title AND TopBar title. */
  title: string;
  /** Optional description rendered below title (slim). */
  description?: string;
  /** Primary action button (top-right). Optional. */
  primaryAction?: {
    label: string;
    icon?: LucideIcon;
    onClick: () => void;
    loading?: boolean;
  };
  /** Search input (top, full-width). Optional. */
  search?: {
    placeholder: string;
    value: string;
    onChange: (v: string) => void;
  };
  /** Filter button (next to search). Optional. */
  onFilterClick?: () => void;
  /** Loading state — renders centered spinner. */
  loading?: boolean;
  /** Error state — renders centered error card with retry. */
  error?: { message: string; onRetry?: () => void };
  /** Empty state — renders when data is empty. Replaces children. */
  empty?: {
    icon?: LucideIcon;
    title: string;
    description?: ReactNode;
    action?: { label: string; onClick: () => void };
  };
  /** Page content (rendered when not loading/error/empty). */
  children: ReactNode;
}

declare const PageShell: React.FC<PageShellProps>;
```

### Behavior

- **Title**: passes to `<PageMetaProvider>` via internal `useSetPageTitle()` hook AND updates `<title>` tag
- **ActionBar**: rendered only if `search` OR `primaryAction` provided
- **Loading**: renders centered spinner Card OR custom skeleton, with `aria-busy="true"` on the page region
- **Error**: renders error card with optional retry button
- **Empty**: renders `<EmptyState>` primitive with provided props
- **Children**: rendered only when not loading/error/empty
- **Header pattern**: Title + optional Description sits above the ActionBar (when description provided)

### Tests

```ts
test("PageShell sets page title via useSetPageTitle");
test("PageShell renders ActionBar when search or primaryAction provided");
test("PageShell does NOT render ActionBar when neither provided");
test("PageShell renders loading spinner when loading=true");
test("PageShell renders error card with retry when error provided");
test("PageShell renders empty state when empty prop provided");
test("PageShell renders children when not loading/error/empty");
test("PageShell has aria-busy when loading");
test("PageShell has no axe violations");
```

### Acceptance criteria

- [ ] Replaces ~20 LOC of boilerplate per page with 5-10 LOC
- [ ] Uses existing primitives: `<ActionBar>`, `<EmptyState>`, `<Card>`, `<Loader2>` from prior briefs
- [ ] State precedence: loading > error > empty > children (mutually exclusive)
- [ ] Works with any of the 3 DSS § 5.2 patterns:
  - KPI strip + 2fr:1fr panels (children = `<KpiStrip /> + <PanelsRow />`)
  - Table-only (children = `<DataTable />`)
  - Form-only (children = `<Card />` stack)
- [ ] Stories cover: minimal / search-only / search+CTA / loading / error / empty / full-featured

### LoopZeroCloud Deep Review finding this closes

- CC-3 indirect: empty-state density violations stem from each page rolling its own EmptyState with arbitrary heights. PageShell centralizes this → enforces DSS § 6.3 (empty states ≤200px, max 1 per page).
- General DRY/SRP win across 13 pages.

---

## Secondary asks (NOT blocking; ship if time permits OR batch into Brief #6)

These don't close P1/P2 Deep Review findings but address P3 polish or DX:

### `EmptyState.Inline` variant

**Why**: Deep Review CC-3 says empty states should be ≤200px. For surfaces like "Recent Previews" on Overview, the ideal is a **1-line muted footer**, not a 200px card. Add an `inline` variant prop:

```tsx
<EmptyState
  variant="inline"
  description="No recent previews · run `loopzero preview` to create one"
/>
```

Renders as a single line with em-dash separator instead of full card.

### `ResendWithCooldown`

**Why**: Recovery + Verification both need "Resend code (30s)" affordance. Pattern repeats across 2 pages.

```tsx
<ResendWithCooldown
  cooldownSeconds={30}
  onResend={() => sendCode()}
  resendLabel="Resend code"
/>
```

### `Combobox` (Select with search)

**Why**: TopBar project selector works for 5 projects. At 50+ projects, users need search. Defer until a consumer reports the pain — currently loopzeroretical.

### `InlineEdit`

**Why**: Profile name update was deferred in DSS because no inline-edit primitive. Pattern: click pencil icon → text becomes input → save/cancel. Useful but defer until Profile redesign is on roadmap.

### Per-component `.d.ts` files

**Why**: Brief #4 announcement noted tsup OOMs when emitting per-component `.d.ts`. Workaround mentioned: `tsc --emitDeclarationOnly` per-entry separately. **DX impact**: large consumer apps will see IDE perf hit pulling barrel type graph for every subpath import. Low-priority for LoopZeroCloud at 30 components used, becomes painful at 80+.

---

## Anti-patterns the team should avoid

Based on Briefs #1/#2/#3/#4 quality bar:

- ❌ `PinInput` rendering visible `<input type="number">` controls (those spinner arrows). Use `type="text"` + `inputMode="numeric"`.
- ❌ `PinInput` blocking paste of "123 456" (with space) — strip whitespace and try to fill anyway.
- ❌ `DataTable` requiring data shape registration upfront (TanStack-style). Use columns array with render callbacks — simpler API for our use case.
- ❌ `DataTable` calling `setState` on sort change for controlled consumers. Provide BOTH controlled (`onSortChange`) and uncontrolled (default behavior) modes.
- ❌ `PageShell` mounting/unmounting children when switching between loading/error/empty/data states (loses scroll position + form state). Either render-prop OR conditional className visibility.
- ❌ `PageShell` setting `document.title` via direct DOM mutation. Use `useSetPageTitle` hook + PageMetaProvider context.
- ❌ Per-component CSS bundles for these three (they don't need it — keep CSS at barrel).

---

## Suggested commit titles for the maintainer

If shipped in a single PR:

```
feat: add PinInput, DataTable, PageShell — close 3 LoopZeroCloud Deep Review findings

- PinInput: 6-box OTP input (closes § 2.12 P2)
- DataTable: sortable + sticky + expandable + actions + pagination (closes § 2.2 + § 2.4 P1 Top-5 #2)
- PageShell: page composite that dedupes 13 dashboard page bootstrap patterns
- Stories + tests + barrel + subpath exports + shadcn registry entries
- CHANGELOG entries per component
- Brief reference: @loopzero/loopzero/docs/handoff/2026-05-25-loopzero-ui-cloud-dashboard-brief-5.md
```

If split (recommended for review):

```
1. feat(primitives): PinInput — OTP/code input with auto-advance + paste
2. feat(composites): DataTable — sortable + sticky + expandable rows + row actions + pagination
3. feat(composites): PageShell — page composite with header + search + states
4. docs: CHANGELOG + ADR for PageShell composite pattern decision
```

---

## Open questions for the maintainer to answer in the PR description

- **PinInput**: should we expose `<PinInput.Slot />` as a compound for power users (custom slot rendering) OR keep it as `length` prop only (simpler API)?
- **DataTable**: client-side sort only at v1, OR include `onSortChange` controlled mode for server-side pagination consumers? My preference: BOTH from the start (small additive complexity, big DX win for consumers who want server-side).
- **DataTable expandable**: Disclosure (single row at a time) OR allow multiple expanded simultaneously? My use case (Domains DNS records) is fine with multiple, but Linear's table is single-only. Default to multiple, opt-in to single via `<DataTable expandable={...} expandMode="single" />`.
- **PageShell loading state**: spinner ONLY, OR skeleton matching the children's pattern? Skeleton is harder to spec for arbitrary children. Default to spinner; add skeleton later.
- **PageShell error state**: link to docs, OR retry callback only? Both as optional props.
- Any other consumers besides LoopZeroCloud + LoopZeroKit that need to be told about these primitives once shipped?

---

## Bundle delta estimate (per Brief #4 empirical pattern)

After @loopzero/ui@0.10 splitting, each primitive ships as its own chunk:

| Primitive | Estimated brotli (chunk only) | Note |
|-----------|-------------------------------:|------|
| PinInput | ~1.5 KB | Pure JS, no heavy deps |
| DataTable | ~6-8 KB | Reuses Table + Pagination + Skeleton + DropdownMenu — composite is thin |
| PageShell | ~2 KB | Composes ActionBar + EmptyState + Loader2 |

**Predicted LoopZeroCloud impact**: when we adopt all 3 (replacing card-grids with DataTable, replacing 13 page boilerplates with PageShell), we expect:
- LOC: −150 to −200 net (3 primitives' bundle overhead absorbed; +600 LOC saved in dashboard code via PageShell composability)
- Bundle: +5-8 KB brotli total when all 3 primitives shipped — within current 240 KB hard gate's 105 KB headroom

I'll commit to publishing a Brief #6 with the empirically measured delta after shipping (same shape as Brief #4 evidence section).

---

## Companion to prior briefs

| Brief | Subject | Status |
|-------|---------|--------|
| #1 (2026-05-23) | 8 missing primitives | ✅ shipped in `@loopzero/ui@0.7.0` |
| #2 (2026-05-23) | 4 more primitives (Table, Topnav, CodeBlock, …) | ✅ shipped in `@loopzero/ui@0.8.0-next.0` |
| #3 (2026-05-23) | 2 deferred primitives (Alert, Pagination) | ✅ shipped in `@loopzero/ui@0.9.0-next.0` |
| #4 (2026-05-24) | Subpath exports tree-shaking fix | ✅ shipped in `@loopzero/ui@0.10.0-next.0` — measured −26 KB brotli on LoopZeroCloud canary |
| #5 (this brief, 2026-05-25) | 3 dashboard primitives (PinInput, DataTable, PageShell) | ⏳ proposed |

---

## How LoopZeroCloud will use these (validation path post-ship)

1. **Bump @loopzero/ui** to whatever version ships these primitives.
2. **Verification page redesign**: replace single text input with `<PinInput length={6} inputMode="numeric" ... />`. Verify via `/dashboard-design-check verification` skill (gate I built for this).
3. **Domains redesign**: replace card-stack with `<DataTable columns={...} data={domains} expandable={(d) => d.status === "pending_verification" ? <DnsRecords /> : null} ... />`. Measure space savings via `/dashboard-density-audit domains`.
4. **Projects redesign**: replace card-grid with `<DataTable>`. Same density audit.
5. **All 13 pages**: migrate boilerplate to `<PageShell>` composite. Measure LOC reduction via `wc -l src/pages/`.
6. **Publish Brief #6** with measured deltas (commit to writing this within 2 weeks of @loopzero/ui ship).

The LoopZeroCloud dashboard is happy to be the canary consumer for all 3 primitives — `npm run build && npm run size` produces deterministic measurements your CI can grep.

---

Brief ends here. Same handoff pattern as Briefs #1-#4: TL;DR + per-component spec + quality bar + anti-patterns + suggested commit titles + open questions + empirical commitment to publish post-ship deltas.
