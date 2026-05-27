# Brief #3: Add 2 deferred primitives to @loopzero/ui (Alert + Pagination)

> **Audience:** the `@loopzero/ui` maintainer team (or a Claude/Codex agent in the `loopzero-ui` repo).
> **Output:** 2 new primitives + tests + stories + barrel exports.
> **Issued by:** LoopZeroCloud dashboard team (consumer), 2026-05-23.
> **Priority:** LOWER than Briefs #1/#2 — **not blocking** Phase 0 of consumer plan. Ship when convenient OR defer until consumer demands empirically.
> **Companion to:** Brief #1 (`2026-05-23-loopzero-ui-cloud-dashboard-gaps-brief.md`) + Brief #2 (`2026-05-23-loopzero-ui-cloud-dashboard-gaps-brief-2.md`).

---

## Why this brief exists (and why it's lower priority)

After Briefs #1 + #2 shipped in `@loopzero/ui@0.8.0-next.0` (114 components total), a systematic walkthrough of the LoopZeroCloud dashboard's 12 pages revealed **100% coverage of in-scope needs**. The 2 components below are **future-facing gaps** for PaaS dashboards at scale, not blockers for the initial migration.

Honest framing:

- **`Alert`** — replaces a 27-LOC local composition (`VerificationBanner`) and 3 plausible future use cases (quota warnings, plan expired, maintenance). Composable in ~25 LOC per site, but semantic coherence + accessibility consistency justify a primitive.
- **`Pagination`** — has **zero** consumer sites today, but 3 plausible future ones (Billing invoices >12 months, audit log streams, Team >50 members). When `<Table>` (Brief #2) starts paginating real data, this becomes a hard requirement. Non-trivial to compose (~80 LOC with state + keyboard arrows + a11y).

If you only ship one, ship **`Alert`** — it has a concrete site today. `Pagination` can wait until consumer demand surfaces.

Components considered + rejected for this brief (kept simple to avoid scope creep):

- `Spinner` — `Loader2` from lucide-react works in 1 line; no abstraction benefit
- `Kbd` — `<kbd>⌘K</kbd>` is native HTML; trivial
- `Tag` / `Chip` input — not in current 12-page scope
- `Drawer` — `Sheet` primitive already covers slide-over need
- `FileUpload` — only 1 page (Profile avatar), defer
- `InlineEdit` — composable from `Input` + `useState`
- `Breadcrumb` — `Topnav` covers shallow nav; deep nav not in current scope

---

## Quality bar (same as Briefs #1/#2)

Every new primitive MUST:

- `forwardRef<...>` + `displayName`
- `cn()` from `lib/cn.js` for class composition
- lucide-react icons (peer-dep) — never inline SVG
- Design tokens only — never raw hex/rgb
- Typography scale (`text-body-sm`, `text-label-caps`, `font-mono`, `font-display`)
- Ship `<component>.tsx` + `<component>.test.tsx` + `<component>.stories.tsx` + `index.ts`
- Pass `pnpm test` (axe-validated via vitest-axe)
- Pass `pnpm typecheck`
- Add to `src/index.ts` barrel + `package.json` `"exports"` map + `registry/{slug}.json`
- CHANGELOG `[Unreleased]` entry per component
- Bundle delta within ±5% (very likely well under for these 2 small primitives)

---

## Component 1 — `Alert`

### Why

Persistent inline notices that need to **stay visible** until user acts (unlike `Toast` which auto-dismisses). PaaS dashboards use this for:

- **Email verification required** (consumer site today — `cloud/dashboard/src/components/verification-banner.tsx`, 27 LOC)
- **Quota threshold warning** ("You're at 90% of your monthly build minutes") — future Billing/Overview surface
- **Plan downgrade notice** ("Your Pro subscription ends in 5 days") — future Billing surface
- **Maintenance window** ("Scheduled downtime: Friday 2-4am UTC") — future system-wide

Distinct from `Toast`:
- Toast = transient, dismissible-by-swipe, corner positioning, multiple stackable, ephemeral feedback ("Project saved")
- Alert = persistent inline, full-width-of-section, single, status that needs user acknowledgment

Distinct from `EmptyState`:
- EmptyState = card-shaped, centered, large icon, "no data" affordance
- Alert = horizontal banner, slim, leading icon, optional inline CTA

### File location

```
src/components/primitives/alert/
  ├── index.ts
  ├── alert.tsx
  ├── alert.test.tsx
  └── alert.stories.tsx
```

### API

```tsx
import { forwardRef } from "react";
import type { HTMLAttributes, ReactNode } from "react";
import { cn } from "../../../lib/cn.js";

export type AlertIntent = "info" | "success" | "warning" | "destructive";

export interface AlertProps extends Omit<HTMLAttributes<HTMLDivElement>, "title"> {
  /** Visual intent — controls icon + colors. */
  intent?: AlertIntent;
  /** Bold title row above description. */
  title?: ReactNode;
  /** Description body. */
  description?: ReactNode;
  /** Optional CTA slot (right-aligned button or link). */
  action?: ReactNode;
  /** When provided, renders an X button that calls this handler. */
  onDismiss?: () => void;
}
```

### Visual spec

| Intent | Icon (lucide) | Border | Background | Text |
|---|---|---|---|---|
| `info` | `Info` | `border-primary/30` | `bg-primary/[0.04]` | `text-primary` (icon), `text-foreground` (body) |
| `success` | `CheckCircle2` | `border-success/30` | `bg-success/[0.04]` | `text-success` (icon) |
| `warning` | `TriangleAlert` | `border-warning/30` | `bg-warning/[0.04]` | `text-warning` (icon) |
| `destructive` | `AlertCircle` | `border-destructive/30` | `bg-destructive/[0.04]` | `text-destructive` (icon) |

Layout:
- Outer: `rounded-lg border` + intent classes, padding `p-3` (or `p-4` if both title + description present)
- Inner: `flex items-start gap-3`
- Icon: `size-4 mt-0.5 flex-shrink-0` + intent color
- Body column: `flex-1 min-w-0`
  - Title (if): `text-body-sm font-medium text-foreground`
  - Description: `text-body-sm text-muted-foreground` (mt-0.5 if title present)
- Action (if): `flex-shrink-0 ml-auto`
- Dismiss button (if `onDismiss`): `X` icon top-right, `size-4 text-muted-foreground hover:text-foreground`

### Edge cases (REQUIRED tests)

1. Each intent renders distinct icon + border + bg
2. Title only (no description) → icon + title only, no extra spacing
3. Description only (no title) → icon + description
4. Both title + description → stacked, correct spacing
5. With `action` → right-aligned CTA, doesn't interfere with text layout
6. With `onDismiss` → X button visible, click fires handler
7. With both `action` AND `onDismiss` → action left of X
8. `role="alert"` for destructive intent (screen reader announcement); other intents use `role="status"` (less aggressive)
9. axe a11y → zero violations

### TDD

```
RED: test_each_intent_renders_distinct_icon
RED: test_title_only_no_description
RED: test_description_only_no_title
RED: test_title_and_description_stacked
RED: test_action_renders_right_aligned
RED: test_dismiss_button_fires_handler
RED: test_destructive_uses_role_alert
RED: test_other_intents_use_role_status
RED: test_a11y_axe
GREEN: implement
VERIFY: vitest run alert
```

### Ladle stories (minimum 5)

- `intents` — 4 intents side-by-side (info/success/warning/destructive)
- `title-only` — minimal alert
- `with-description` — common case
- `with-action` — "Verify your email → Verify now" CTA pattern
- `dismissible` — X button visible

### Consumer use site (validation)

After implementation, `cloud/dashboard/src/components/verification-banner.tsx` (27 LOC custom) becomes:

```tsx
<Alert
  intent="warning"
  title="Verify your email"
  description="We sent a link to your email. Click it to unlock all features."
  action={<Button size="sm" onClick={resend}>Resend</Button>}
/>
```

3 lines vs 27. Same a11y, same UX, semantic clarity.

---

## Component 2 — `Pagination`

### Why

`<Table>` (Brief #2) ships v1 without pagination. Acceptable today (consumer can slice array client-side) but **becomes a hard requirement** when:

- Billing invoice history exceeds 12 months (~50+ rows)
- Audit log (planned) streams thousands of events
- Team grows past 50 members
- Project list grows past initial view

Composing pagination correctly is non-trivial:

- State management (current page, total pages, items per page)
- Keyboard navigation (`ArrowLeft`/`ArrowRight`/`Home`/`End`)
- Accessible markup (`<nav aria-label="Pagination">` + `aria-current="page"` on active)
- Visual ellipsis when total pages > N (e.g., `1 ... 5 6 [7] 8 9 ... 42`)
- First/Previous/Next/Last buttons with proper disabled states

Local composition runs ~80 LOC per site with multiple subtle bugs to get right. A primitive consolidates the pattern.

### File location

```
src/components/primitives/pagination/
  ├── index.ts
  ├── pagination.tsx
  ├── pagination.test.tsx
  └── pagination.stories.tsx
```

### API

```tsx
import { forwardRef } from "react";
import type { HTMLAttributes } from "react";
import { cn } from "../../../lib/cn.js";

export interface PaginationProps extends Omit<HTMLAttributes<HTMLElement>, "onChange"> {
  /** Current page (1-indexed). */
  currentPage: number;
  /** Total number of pages. */
  totalPages: number;
  /** Called when user navigates to a different page. */
  onPageChange: (page: number) => void;
  /** Number of page-number buttons shown around current. Default 1 (e.g., "5 6 [7] 8 9"). */
  siblingCount?: number;
  /** Show first/last "Jump to start/end" buttons. Default true. */
  showJumpButtons?: boolean;
  /** Size variant. Default md. */
  size?: "sm" | "md";
}
```

### Behavior

- Renders `<nav aria-label="Pagination">` containing button group
- Layout: `[<<] [<] 1 ... 5 6 [7] 8 9 ... 42 [>] [>>]`
  - `<<` = first page (only if `showJumpButtons` and `currentPage > 1`)
  - `<` = previous page (disabled if `currentPage === 1`)
  - Page numbers around current with `siblingCount` neighbors
  - `...` ellipsis when gap > 1 between visible numbers
  - `>` = next page (disabled if `currentPage === totalPages`)
  - `>>` = last page (only if `showJumpButtons` and `currentPage < totalPages`)
- Active page: `aria-current="page"`, distinct visual treatment (filled, not outline)
- Keyboard:
  - `ArrowLeft` on any page button → focus + activate previous page
  - `ArrowRight` → next page
  - `Home` → first page
  - `End` → last page
  - Page number buttons are `<button>` (not `<a>`) — consumer handles URL routing
- Disabled buttons: `aria-disabled="true"` + `disabled` attribute
- Visual: numbers use `font-mono tabular-nums` for alignment

### Visual spec

- Outer `<nav>`: `flex items-center gap-1`
- Each button: `size-8` (md) or `size-7` (sm), `rounded-md`, `font-mono text-body-sm tabular-nums`
- Active: `bg-primary text-primary-foreground`
- Inactive: `hover:bg-muted text-foreground`
- Disabled: `opacity-40 cursor-not-allowed`
- Ellipsis: `text-muted-foreground` (not interactive — `<span>`)
- Icons (first/prev/next/last): `ChevronsLeft`, `ChevronLeft`, `ChevronRight`, `ChevronsRight` from lucide, `size-3.5`

### Edge cases (REQUIRED tests)

1. `totalPages === 1` → renders single "1" button or nothing (TBD: spec says "render nothing if only 1 page")
2. `currentPage === 1` → previous + first buttons disabled
3. `currentPage === totalPages` → next + last buttons disabled
4. `totalPages <= 7` (small) → all page numbers visible, no ellipsis
5. `totalPages > 7`, current in middle → ellipsis on both sides
6. `totalPages > 7`, current near start → ellipsis only on right
7. `totalPages > 7`, current near end → ellipsis only on left
8. `siblingCount={0}` → only current page shown (no neighbors)
9. `showJumpButtons={false}` → no first/last buttons
10. Click on page number → `onPageChange` called with that page
11. `ArrowRight` keyboard → focus next page button
12. `Home` keyboard → focus first page button
13. axe a11y → zero violations (nav landmark + aria-current correct)

### TDD

```
RED: test_renders_nothing_when_one_page
RED: test_prev_disabled_on_first_page
RED: test_next_disabled_on_last_page
RED: test_all_visible_when_under_seven
RED: test_ellipsis_both_sides_when_middle
RED: test_ellipsis_only_right_when_near_start
RED: test_ellipsis_only_left_when_near_end
RED: test_sibling_count_zero
RED: test_jump_buttons_hidden_when_disabled
RED: test_click_fires_onPageChange
RED: test_arrow_right_focuses_next
RED: test_home_focuses_first
RED: test_active_has_aria_current
RED: test_a11y_axe
GREEN: implement
VERIFY: vitest run pagination
```

### Ladle stories (minimum 5)

- `default` — 7 pages, current=3
- `many-pages` — 42 pages, current=20, ellipsis both sides
- `near-start` — 42 pages, current=2
- `near-end` — 42 pages, current=41
- `compact` — size="sm", no jump buttons
- `single-page` — `totalPages=1` (renders nothing)

### Implementation note

Use a helper function `computePageRange(currentPage, totalPages, siblingCount): (number | "ellipsis")[]` — single pure function easy to unit-test in isolation. Most pagination bugs are in this range computation, not the rendering.

---

## Barrel exports

Update `src/index.ts`:

```ts
export { Alert, type AlertProps, type AlertIntent } from "./components/primitives/alert/index.js";
export { Pagination, type PaginationProps } from "./components/primitives/pagination/index.js";
```

Update `package.json` `"exports"` map:

```json
"./alert": { "types": "./dist/index.d.ts", "import": "./dist/index.js" },
"./pagination": { "types": "./dist/index.d.ts", "import": "./dist/index.js" }
```

---

## Acceptance criteria

- [ ] 2 new primitives created in `src/components/primitives/{alert,pagination}/`
- [ ] Each ships `.tsx` + `.test.tsx` + `.stories.tsx` + `index.ts`
- [ ] `pnpm test` passes
- [ ] `pnpm typecheck` passes
- [ ] Zero axe violations
- [ ] Barrel + exports map + registry descriptors updated
- [ ] CHANGELOG `[Unreleased]` → `### Added`: "Alert, Pagination primitives (deferred PaaS dashboard surfaces; consumer: LoopZeroCloud dashboard, Brief #3)"
- [ ] Bundle size delta well within ±5% (both small primitives)
- [ ] Zero breaking change

## Validation against consumer

After implementation, `cloud/dashboard`:

1. Installs new version
2. Refactors `components/verification-banner.tsx` → `<Alert intent="warning" .../>`
3. Adds `<Pagination>` to BillingPage invoice history (when consumer accumulates >12 months data)
4. Adds `<Pagination>` to TeamPage when membership grows

## Release strategy recommendation

This brief should ship as **`@loopzero/ui@0.9.0-next.0`** (minor bump from 0.8.0-next.0). Both primitives are additive, zero breaking change. Consider including in a "PaaS polish release" with any other deferred-from-Brief-#2 items if backlog has accumulated.

## Cross-reference

- Brief #1: `loopzero/docs/handoff/2026-05-23-loopzero-ui-cloud-dashboard-gaps-brief.md` (4 PaaS-shape primitives — shipped in 0.7.0)
- Brief #2: `loopzero/docs/handoff/2026-05-23-loopzero-ui-cloud-dashboard-gaps-brief-2.md` (8 cross-cutting primitives — shipped in 0.8.0)
- Consumer plan: `loopzero/docs/plans/completed/2026-05-23-dashboard-loopzero-ui-migration-plan.md` v1.1 (pending v1.2 bump for Tailwind v4 + Geist fonts + theme API findings from llms.txt)
- llms.txt source-of-truth: `https://docs.loopzero.example/llms.txt`
- Existing siblings for API patterns:
  - `loopzero-ui/src/components/primitives/toast/toast.tsx` — Alert is the persistent counterpart
  - `loopzero-ui/src/components/primitives/empty-state/empty-state.tsx` — similar icon+title+description pattern, different shape
  - `loopzero-ui/src/components/primitives/badge/badge.tsx` — variant API pattern for AlertIntent
  - `loopzero-ui/src/components/primitives/button/button.tsx` — for Pagination button styling consistency
