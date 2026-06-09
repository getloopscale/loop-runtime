# Brief #2: Add 8 cross-cutting primitives to @loopzerokit/ui

> **Audience:** the `@loopzerokit/ui` maintainer team (or a Claude/Codex agent running in the `loopzero-ui` repo).
> **Output:** 8 new primitives + tests + stories + barrel exports, following existing `@loopzerokit/ui` conventions.
> **Issued by:** LoopZeroCloud dashboard team (consumer), 2026-05-23.
> **Companion to:** `loopzero/docs/handoff/2026-05-23-loopzero-ui-cloud-dashboard-gaps-brief.md` (Brief #1 — PaaS-shape gaps: UsageMeter, Progress, PlanBadge, AccountMenu).
> **Consumer plan:** `loopzero/docs/plans/completed/2026-05-23-dashboard-loopzero-ui-migration-plan.md` v1.1.

---

## Why this brief exists

Brief #1 covered 4 PaaS-shape gaps revealed by the Overview mockup. This Brief #2 covers **cross-cutting primitives** revealed by systematic review of the other 11 dashboard pages (Projects, Environments, Team, Billing, Domains, Settings, Profile, Login, Register, Verification, Recovery, DeviceSuccess) + recurring PaaS UI patterns.

Selection criteria: each primitive below is used in **≥3 distinct places** in the consumer dashboard, **not trivially composable** (>5 LOC if hand-rolled per site), and carries **its own semantic value** (not redundant with existing primitives). Components considered and rejected are listed in "Out of scope" at the end.

The library is **agent-first by design** but consumers explicitly want PaaS-capable. The 4 from Brief #1 + 8 here close the gap without redefining the library's positioning — they sit as sibling primitives alongside `CostMeter`, `ProjectSwitcher`, `MemoryEditor`, etc.

---

## Quality bar (same as Brief #1)

Every new primitive MUST:

- Follow the `forwardRef<...>` + `displayName` pattern (see `primitives/sidebar/sidebar.tsx`)
- Use `cn()` from `lib/cn.js` for class composition
- Use `lucide-react` icons (peer-dep) — never bundle SVG inline
- Use design tokens (`bg-card`, `text-foreground`, `text-muted-foreground`, `border-border/40`, `bg-primary/10`, etc.) — never raw hex
- Use typography scale (`text-body-sm`, `text-label-caps`, `font-mono`, `font-display`)
- Ship `<component>.tsx` + `<component>.test.tsx` + `<component>.stories.tsx` + `index.ts` barrel
- Pass `vitest run` (axe-validated via vitest-axe assertions)
- Pass `npm run typecheck` (strict TS)
- Add to `src/index.ts` top-level barrel
- Add to `package.json` `"exports"` map under `./<kebab-name>`

---

## Component 1 — `Table`

### Why

PaaS dashboards routinely display tabular data: invoice histories, team members, audit logs, environment variables grid views. Currently `@loopzerokit/ui` has only row-shaped primitives (`DeploymentRow`, `audit-log-entry`, `session-list-item`) — great for card-list density, insufficient for **sortable comparative data** with column headers and aligned numerics.

Consumer use cases in `cloud/dashboard`:
- **BillingPage** — invoice history (Date / Description / Amount / Status / PDF download)
- **TeamPage** — member list (Avatar+Name / Email / Role / Joined date / Actions)
- **AuditLog** (planned) — timeline of mutations (Timestamp / Actor / Action / Resource)
- **EnvVarEditor** (composite, internal usage today) — potentially benefits from table primitive underneath

### File location

```
src/components/primitives/table/
  ├── index.ts
  ├── table.tsx
  ├── table.test.tsx
  └── table.stories.tsx
```

### API (sub-components pattern, mirrors Sidebar)

```tsx
import { forwardRef } from "react";
import type { HTMLAttributes, TdHTMLAttributes, ThHTMLAttributes } from "react";
import { cn } from "../../../lib/cn.js";

export interface TableProps extends HTMLAttributes<HTMLTableElement> {
  /** Compact row height (24px vs default 40px). */
  density?: "default" | "compact";
}

// Root <table> with sub-components: Table.Header, Table.Body, Table.Row, Table.Cell, Table.HeaderCell
// Example:
// <Table>
//   <Table.Header>
//     <Table.Row>
//       <Table.HeaderCell>Date</Table.HeaderCell>
//       <Table.HeaderCell align="right">Amount</Table.HeaderCell>
//     </Table.Row>
//   </Table.Header>
//   <Table.Body>
//     {invoices.map(inv => (
//       <Table.Row key={inv.id}>
//         <Table.Cell>{inv.date}</Table.Cell>
//         <Table.Cell align="right" numeric>{inv.amount}</Table.Cell>
//       </Table.Row>
//     ))}
//   </Table.Body>
// </Table>

export interface TableCellProps extends TdHTMLAttributes<HTMLTableCellElement> {
  /** Alignment. Default left. */
  align?: "left" | "center" | "right";
  /** When true, applies tabular-nums + font-mono for value alignment. */
  numeric?: boolean;
}

export interface TableHeaderCellProps extends ThHTMLAttributes<HTMLTableCellElement> {
  align?: "left" | "center" | "right";
  /** When provided, header becomes a sort trigger button (clickable). */
  onSort?: () => void;
  /** Current sort state for this column. */
  sortDirection?: "asc" | "desc" | "none";
}
```

### Visual spec

- Root `<table>`: `w-full border-collapse text-body-sm`
- Header row: `border-b border-border/40` with `text-label-caps text-muted-foreground uppercase tracking-wider`
- Header cells: padding `px-3 py-2.5`, alignment per `align` prop
- Body rows: `border-b border-border/20 last:border-0`, hover `hover:bg-muted/40 transition-colors`
- Body cells: padding `px-3 py-3` default, `py-1.5` if `density="compact"`
- `numeric` cells: `font-mono tabular-nums`
- Sort icon (when `onSort` provided): `ChevronUp`/`ChevronDown` from lucide, 12px, after label

### Edge cases (REQUIRED tests)

1. Empty body (`<Table.Body />` with no rows) → renders header only, no crash
2. `align` prop on cell vs header — both respected independently
3. `numeric` cell — class includes `tabular-nums`
4. `density="compact"` — vertical padding reduces
5. Sort header — `onSort` fires on click; `sortDirection` swaps icon
6. axe a11y → zero violations, table has implicit row/cell semantics

### TDD

```
RED: test_renders_header_and_body_rows
RED: test_empty_body_does_not_crash
RED: test_numeric_cell_has_tabular_nums
RED: test_align_right_applies_text_right
RED: test_density_compact_reduces_padding
RED: test_sort_header_fires_onSort
RED: test_sort_direction_swaps_icon
RED: test_a11y_axe
GREEN: implement
VERIFY: vitest run table
```

### Ladle stories (minimum 4)

- `default` — invoice history (Date / Description / Amount / Status)
- `compact` — same data, density="compact"
- `sortable` — header with sort interactions
- `numeric-alignment` — financial table with right-aligned values

### Deliberately OUT of scope for v1

- Pagination (consumer handles via slicing array)
- Row selection (separate primitive)
- Resizable columns (CSS pain, defer)
- Virtual scrolling (use react-virtuoso when needed)
- Built-in sorting state (consumer provides `sortDirection`)

---

## Component 2 — `StatusDot`

### Why

Status dots (small colored circle + optional label) appear in **at least 7 places** in the dashboard:

- Sidebar nav (active project status)
- Overview "Recent Deploys" rows (live/failed)
- Projects page (per-app health: live/down)
- Environments page (per-env status: deployed/building/failed)
- Domains page (verification status: verified/pending/failed)
- Team page (member online status — future)
- ProjectCard composite (status indicator)

Currently every consumer hand-rolls `<span className="w-2 h-2 rounded-full bg-success animate-pulse" />` with inconsistent sizing/colors/labels. The mockup line 74 uses an orange dot for "needs git repo" status. This deserves a semantic primitive with consistent vocabulary.

### File location

```
src/components/primitives/status-dot/
  ├── index.ts
  ├── status-dot.tsx
  ├── status-dot.test.tsx
  └── status-dot.stories.tsx
```

### API

```tsx
import { forwardRef } from "react";
import type { HTMLAttributes, ReactNode } from "react";
import { cn } from "../../../lib/cn.js";

export type StatusKind =
  | "live"        // healthy, deployed, verified — success/green
  | "building"    // in-progress, deploying, queued — warning/amber pulsing
  | "failed"      // error, down, rejected — destructive/red
  | "idle"        // unknown, pending action, offline — muted/grey
  | "warning";    // degraded but functional — warning/amber static

export interface StatusDotProps extends Omit<HTMLAttributes<HTMLSpanElement>, "children"> {
  /** Status semantic. */
  status: StatusKind;
  /** When provided, renders label after dot (with gap-1.5). */
  label?: ReactNode;
  /** Size variant. Default sm (8px). */
  size?: "xs" | "sm" | "md";
  /** When true, dot pulses (for "in-progress" affordances). Auto-true for "building". */
  pulse?: boolean;
}
```

### Visual spec

| Status | Dot color | Pulse default | Text color (when label) |
|---|---|---|---|
| `live` | `bg-success` | false | `text-success` |
| `building` | `bg-warning` | true (auto) | `text-warning` |
| `failed` | `bg-destructive` | false | `text-destructive` |
| `idle` | `bg-muted-foreground/40` | false | `text-muted-foreground` |
| `warning` | `bg-warning` | false | `text-warning` |

Sizes:
- `xs` → `w-1.5 h-1.5` (6px)
- `sm` → `w-2 h-2` (8px, default)
- `md` → `w-2.5 h-2.5` (10px)

Label uses `text-label font-mono` for consistency with status badges. Container is `inline-flex items-center gap-1.5`.

### Edge cases (REQUIRED tests)

1. Each `status` kind renders distinct color class
2. `pulse=true` adds `animate-pulse` class
3. `status="building"` auto-pulses (no explicit `pulse` needed)
4. `pulse=false` on building overrides default (no animation)
5. No label → renders only dot, no extra spans
6. `aria-label` MUST be set when no visible label (a11y for screen readers)

### TDD

```
RED: test_each_status_renders_distinct_color
RED: test_pulse_true_adds_animation
RED: test_building_auto_pulses
RED: test_explicit_pulse_false_overrides_auto
RED: test_no_label_renders_dot_only
RED: test_aria_label_required_when_no_visible_label
RED: test_a11y_axe
GREEN: implement
VERIFY: vitest run status-dot
```

### Ladle stories (minimum 3)

- `kinds` — all 5 status kinds side-by-side, sm size
- `sizes` — xs/sm/md for `live` status
- `with-labels` — all 5 with labels for context comparison

---

## Component 3 — `CopyButton`

### Why

"Click to copy" is a ubiquitous PaaS interaction:
- Env vars (copy value)
- Domains (copy TXT verification record)
- API tokens (copy secret)
- Project IDs (copy for support tickets)
- Share URLs (copy preview link)
- Code snippets in empty states (copy `loopzero deploy`)

Currently every consumer writes `navigator.clipboard.writeText` + transient "Copied!" tooltip + icon swap state manually. ~25 LOC per site, easy to miss a11y (no `aria-live` for screen reader announcement).

### File location

```
src/components/primitives/copy-button/
  ├── index.ts
  ├── copy-button.tsx
  ├── copy-button.test.tsx
  └── copy-button.stories.tsx
```

### API

```tsx
import { forwardRef } from "react";
import type { ButtonHTMLAttributes, ReactNode } from "react";
import { cn } from "../../../lib/cn.js";

export interface CopyButtonProps
  extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, "type" | "onClick" | "children"> {
  /** String to copy when clicked. */
  value: string;
  /** Optional button label. Default: just the icon. */
  label?: ReactNode;
  /** Visual style. */
  variant?: "ghost" | "outline";
  /** Size. */
  size?: "sm" | "md";
  /** Callback after successful copy (e.g. analytics). */
  onCopied?: (value: string) => void;
  /** Duration of "Copied!" state in ms. Default 1500. */
  feedbackDuration?: number;
}
```

### Behavior

- On click: `navigator.clipboard.writeText(value)`
- On success:
  - Swap icon from `Copy` (lucide) to `Check`
  - If `label` provided, swap text to "Copied!"
  - Announce via `aria-live="polite"` region (visually hidden but SR-readable)
  - Fire `onCopied(value)` callback
  - Revert after `feedbackDuration` ms
- On failure (clipboard API blocked, http context): swap to `X` icon + "Failed" briefly; `aria-live` announces failure

### Visual spec

- Outer: `<button>` with `inline-flex items-center gap-1.5`
- `ghost` variant: `hover:bg-muted` only
- `outline` variant: `border border-border/60 rounded-md px-2 py-1`
- Icon `size-3.5` (14px)
- Transition opacity on icon swap (`transition-opacity duration-200`)

### Edge cases (REQUIRED tests)

1. Click → clipboard.writeText called with `value`
2. Icon swaps to Check after click
3. Reverts after `feedbackDuration`
4. `onCopied` fires after success
5. Clipboard API rejection → renders failure state + reverts
6. SSR — no `navigator` available → button still renders, click is no-op until hydrate
7. `aria-live` region exists and updates on copy

### TDD

```
RED: test_click_writes_to_clipboard (mock clipboard API)
RED: test_icon_swaps_to_check_after_copy
RED: test_reverts_after_feedback_duration
RED: test_onCopied_callback_fires
RED: test_failure_state_when_clipboard_rejects
RED: test_aria_live_announces_copy
RED: test_ssr_safe
RED: test_a11y_axe
GREEN: implement (use setTimeout + useState; debounce double-clicks)
VERIFY: vitest run copy-button
```

### Ladle stories (minimum 4)

- `icon-only-ghost` — just Copy icon, ghost variant
- `with-label-outline` — "Copy" text + icon, outline variant
- `long-value` — copies multi-line string (test wrapping)
- `failure` — mock rejection, see error state

---

## Component 4 — `Timestamp`

### Why

PaaS dashboards display dates everywhere: "Last deploy 24h ago", "Created 3 months ago", "Expires Dec 5 2026". Consumers currently hand-roll relative-time strings with custom formatting, often missing:
- Semantic `<time datetime="...">` element (a11y + SEO)
- Tooltip with absolute time on hover (UX expectation)
- Auto-updating relative time (1m ago → 2m ago without re-render trigger)
- Locale-aware formatting

Use cases:
- Overview "Recent Deploys" timestamps
- Projects "last_deploy"
- Environments "last activity"
- Billing invoice dates
- Audit log entries
- Domain verification "verified at"

### File location

```
src/components/primitives/timestamp/
  ├── index.ts
  ├── timestamp.tsx
  ├── timestamp.test.tsx
  └── timestamp.stories.tsx
```

### API

```tsx
import { forwardRef } from "react";
import type { HTMLAttributes } from "react";
import { cn } from "../../../lib/cn.js";

export interface TimestampProps extends Omit<HTMLAttributes<HTMLTimeElement>, "children"> {
  /** Source date — ISO string, Date object, or Unix ms. */
  value: string | Date | number;
  /** Format mode. */
  format?: "relative" | "absolute" | "both";
  /** Auto-refresh interval when format=relative. Default 60000 (1 min). 0 disables. */
  refreshInterval?: number;
  /** Locale for absolute formatting. Default browser locale. */
  locale?: string;
  /** When true, no tooltip with absolute time. Default false (tooltip shown). */
  noTooltip?: boolean;
}
```

### Behavior

- Renders `<time datetime="ISO_8601">RELATIVE_OR_ABSOLUTE_TEXT</time>`
- `relative` (default): "just now", "5m ago", "2h ago", "3d ago", "Dec 5" (after 7d), "Dec 5, 2024" (different year)
- `absolute`: full localized date+time
- `both`: "Dec 5, 2026 (2h ago)"
- Tooltip on hover (unless `noTooltip`): always shows absolute (e.g., "Dec 5, 2026, 14:32:01 GMT-3")
- Auto-refresh via `setInterval` (only when visible & relative mode); cleanup on unmount
- SSR: emits initial relative based on server time; client may re-compute on hydrate (handle warning)

### Visual spec

- Inline element, inherits surrounding text style
- Optional `font-mono` if used inside Table cells (consumer applies via className)
- Tooltip uses existing `Tooltip` primitive

### Edge cases (REQUIRED tests)

1. Date in past 60s → "just now"
2. 1-59 min → "Xm ago"
3. 1-23 hours → "Xh ago"
4. 1-6 days → "Xd ago"
5. Same year, >7 days → "Mon DD" (e.g., "Dec 5")
6. Different year → "Mon DD, YYYY"
7. Future date → "in Xm" / "in Xh"
8. Invalid date → renders empty + warns once in dev (not in prod)
9. `refreshInterval=0` → no setInterval scheduled
10. SSR-safe (no useEffect-dependent rendering)
11. `aria-label` always carries absolute time

### TDD

```
RED: test_just_now_for_recent
RED: test_minutes_ago
RED: test_hours_ago
RED: test_days_ago
RED: test_same_year_short_format
RED: test_different_year_includes_year
RED: test_future_dates_prefix_in
RED: test_invalid_date_renders_empty
RED: test_refresh_interval_zero_no_setInterval
RED: test_aria_label_carries_absolute
RED: test_a11y_axe
GREEN: implement (consider date-fns or Intl.RelativeTimeFormat — choose lightest)
VERIFY: vitest run timestamp
```

### Ladle stories (minimum 4)

- `relative` — various deltas (1m, 1h, 1d, 1mo, 1y ago)
- `absolute` — full date formatting
- `both` — combined
- `with-tooltip` — hover demo

### Implementation note

Prefer `Intl.RelativeTimeFormat` (native, zero deps) over date-fns to avoid bundle bloat. ~50 LOC including unit thresholds.

---

## Component 5 — `StatTile`

### Why

The Overview page (mockup-aligned) and most dashboards show a row of 2-4 "stat tiles": single big number + label + optional delta + optional icon + clickable navigation. Currently `cloud/dashboard/pages/overview.tsx:31-66` hand-rolls 21 LOC per tile (3 tiles = 63 LOC) of Card composition + icon container + delta + hover state. This pattern repeats in:

- Overview (Projects count, Deploys count, Avg Build duration)
- Billing (current period usage summary)
- Team (member count, seat utilization)
- (Future) AuditLog (events today, last hour, failures)

`CostMeter` is USD-shape; `Card` is too generic. A dedicated `StatTile` carries the "big-number + meta" semantic.

### File location

```
src/components/primitives/stat-tile/
  ├── index.ts
  ├── stat-tile.tsx
  ├── stat-tile.test.tsx
  └── stat-tile.stories.tsx
```

### API

```tsx
import { forwardRef } from "react";
import type { ButtonHTMLAttributes, ElementType, ReactNode } from "react";
import { cn } from "../../../lib/cn.js";

export interface StatTileProps
  extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, "type"> {
  /** Big number/value displayed prominently. */
  value: ReactNode;
  /** Short label under value (e.g. "Projects", "Deploys"). */
  label: ReactNode;
  /** Optional lucide icon shown in top-left tile. */
  icon?: ElementType;
  /** Optional delta with direction (e.g. "+12% vs last week"). */
  delta?: { value: ReactNode; trend: "up" | "down" | "flat" };
  /** When provided, tile is clickable button (with hover state + chevron). */
  onClick?: () => void;
}
```

### Visual spec

- Outer: `rounded-xl border bg-card p-5`
- If `onClick`: render as `<button>`, add `hover:border-primary/30` + `transition-colors` + `cursor-pointer`; trailing `ArrowUpRight` icon top-right (`size-3.5 text-muted-foreground group-hover:text-foreground`)
- If no `onClick`: render as `<div>`, no chevron
- Top row: icon tile (`flex h-8 w-8 items-center justify-center rounded-lg bg-muted/40 border border-border/40`) on left, optional ArrowUpRight on right
- Value: `font-display text-display-md font-bold tracking-tight text-foreground tabular-nums leading-none` (margin-top from icon row)
- Label: `text-body-sm text-muted-foreground` (margin-top 1)
- Delta: `text-label font-mono` — color based on trend (`up → text-success`, `down → text-destructive`, `flat → text-muted-foreground`); leading lucide icon (`TrendingUp`/`TrendingDown`/`Minus`)

### Edge cases (REQUIRED tests)

1. No icon → top row has just optional chevron (or empty)
2. No onClick → no chevron, no button semantics
3. With onClick → button + chevron + hover state
4. Delta trend up/down/flat → distinct color + icon
5. Long value (e.g. "1,234,567") → no wrap, tabular-nums alignment
6. axe a11y → button has accessible name (from label)

### TDD

```
RED: test_renders_value_and_label
RED: test_icon_renders_when_provided
RED: test_no_chevron_when_not_clickable
RED: test_chevron_and_button_when_onClick
RED: test_delta_trend_colors
RED: test_long_value_no_wrap
RED: test_a11y_axe (button has accessible name from label)
GREEN: implement
VERIFY: vitest run stat-tile
```

### Ladle stories (minimum 4)

- `simple` — value + label only
- `with-icon-and-delta` — full featured static
- `clickable` — with onClick + hover state demo
- `grid-of-three` — typical Overview row

---

## Component 6 — `ConfirmDialog`

### Why

Destructive actions (delete project, remove team member, cancel subscription, revoke API token, transfer ownership) require **explicit confirmation** with consistent UX: typed confirmation for irreversible operations, clear danger language, focused destructive button. Consumers currently compose `Dialog` manually each time, easy to skip best practices (auto-focus on confirm = dangerous, missing "type to confirm" for destructive cases).

Use cases in dashboard:
- Settings → Delete project
- Team → Remove member
- Billing → Cancel subscription
- Profile → Delete account
- Domains → Remove custom domain
- Environments → Delete environment

6+ confirmed use cases.

### File location

```
src/components/primitives/confirm-dialog/
  ├── index.ts
  ├── confirm-dialog.tsx
  ├── confirm-dialog.test.tsx
  └── confirm-dialog.stories.tsx
```

### API

```tsx
import { forwardRef } from "react";
import type { ReactNode } from "react";

export interface ConfirmDialogProps {
  /** Controlled open state. */
  open: boolean;
  /** Called when user closes (cancel, escape, click outside). */
  onOpenChange: (open: boolean) => void;
  /** Dialog title (e.g. "Delete project"). */
  title: ReactNode;
  /** Body description / warning. */
  description: ReactNode;
  /** Label on confirm button. Default "Confirm". */
  confirmLabel?: ReactNode;
  /** Label on cancel button. Default "Cancel". */
  cancelLabel?: ReactNode;
  /** Intent — controls confirm button styling. */
  intent?: "default" | "destructive";
  /** When provided, user must type this exact string to enable confirm button. */
  confirmationPhrase?: string;
  /** Called when confirm pressed (and confirmationPhrase matched if set). */
  onConfirm: () => void | Promise<void>;
  /** When async onConfirm in flight, shows spinner + disables. */
  loading?: boolean;
}
```

### Behavior

- Built on existing `Dialog` primitive
- Default focus: Cancel button (NOT Confirm — anti-pattern for destructive)
- ESC / click-outside → calls `onOpenChange(false)`
- `intent="destructive"` → confirm button uses `bg-destructive text-destructive-foreground`
- `confirmationPhrase` → renders `<input>` below description; confirm button disabled until input value === phrase (exact match, case-sensitive)
- `onConfirm` async → button shows `Loader2` spinner, both buttons disabled
- After successful confirm → `onOpenChange(false)` (consumer can override by not closing)

### Visual spec

- Title: `text-title-sm font-semibold`
- Description: `text-body-sm text-muted-foreground`
- For `confirmationPhrase`: render "Type `<phrase>` to confirm" with phrase in `<code>` styling
- Buttons: justify-end at bottom, Cancel left of Confirm
- Spacing per Dialog primitive standards

### Edge cases (REQUIRED tests)

1. Cancel button is auto-focused on open (not confirm)
2. ESC dispatches `onOpenChange(false)`
3. `intent="destructive"` confirm button has destructive class
4. `confirmationPhrase` set + input empty → confirm button disabled
5. `confirmationPhrase` set + input partial → still disabled
6. `confirmationPhrase` set + input exact match → confirm enabled
7. `confirmationPhrase` set + input case mismatch → still disabled
8. `loading=true` → both buttons disabled + spinner shown
9. Async `onConfirm` resolves → dialog closes
10. Async `onConfirm` rejects → dialog stays open (consumer handles error)
11. axe a11y → dialog has correct role + accessible name

### TDD

```
RED: test_cancel_button_auto_focused
RED: test_escape_calls_on_open_change_false
RED: test_destructive_intent_styles_confirm_button
RED: test_confirmation_phrase_disables_until_match
RED: test_phrase_case_sensitive
RED: test_loading_disables_both_buttons
RED: test_async_confirm_resolve_closes
RED: test_async_confirm_reject_stays_open
RED: test_a11y_axe
GREEN: implement
VERIFY: vitest run confirm-dialog
```

### Ladle stories (minimum 4)

- `default` — standard confirm/cancel
- `destructive` — red confirm button
- `with-phrase` — "type project name to confirm" pattern
- `async-loading` — simulated network call

---

## Component 7 — `CodeBlock`

### Why

Terminal commands appear in many places: empty states (`loopzero deploy`, `loopzero init`), inline docs ("Run this command"), DNS record values (long TXT strings), example responses. Currently consumers use raw `<code>` or `<pre>` with hand-rolled styling. CopyButton (Component 3) is the trigger; CodeBlock is the surface — they pair.

Use cases:
- Overview EmptyState "No deploys yet" → `loopzero deploy`
- Projects EmptyState "No projects yet" → `loopzero init`
- Domains DNS record display
- API token display
- LoginPage CLI hint section (already in current code, line 235)
- (Future) Build log code snippets

6+ confirmed use cases.

### File location

```
src/components/primitives/code-block/
  ├── index.ts
  ├── code-block.tsx
  ├── code-block.test.tsx
  └── code-block.stories.tsx
```

### API

```tsx
import { forwardRef } from "react";
import type { HTMLAttributes, ReactNode } from "react";
import { cn } from "../../../lib/cn.js";

export interface CodeBlockProps extends Omit<HTMLAttributes<HTMLDivElement>, "children"> {
  /** Code content. Can be multiline string. */
  code: string;
  /** Language hint for future syntax highlighting (v1 ignored, included in API for forward-compat). */
  language?: string;
  /** When true, prefix each line with "$ " for terminal commands. */
  terminal?: boolean;
  /** Show inline CopyButton in top-right. */
  copyable?: boolean;
  /** Optional caption above block (e.g. ".env.local"). */
  caption?: ReactNode;
}
```

### Visual spec

- Outer: `rounded-lg border bg-muted/40 font-mono text-body-sm`
- Caption (if): `border-b border-border/40 px-3 py-1.5 text-label text-muted-foreground`
- Code area: `<pre>` with `p-3 overflow-x-auto`
- Terminal lines: prefix each line with `<span class="text-muted-foreground select-none">$ </span>`
- CopyButton (if `copyable`): absolute positioned top-2 right-2, `ghost` variant
- Code text: `text-foreground` for code, `text-primary` for terminal prefix

### Edge cases (REQUIRED tests)

1. Single line → no scrollbar
2. Multi-line → preserves newlines via `<pre>`
3. `terminal=true` → "$ " prefix per line, not selectable
4. `copyable=true` → CopyButton present with `value` = unprefixed code
5. CopyButton value strips terminal "$ " prefixes (user copies just the command)
6. Long line → horizontal scroll, no wrap
7. axe a11y → has role=`region` or proper labelling

### TDD

```
RED: test_renders_code_in_pre
RED: test_terminal_prefix_added_per_line
RED: test_copy_button_renders_when_copyable
RED: test_copy_strips_terminal_prefix
RED: test_long_line_horizontal_scroll
RED: test_caption_renders_above
RED: test_a11y_axe
GREEN: implement
VERIFY: vitest run code-block
```

### Ladle stories (minimum 4)

- `single-command` — terminal=true, copyable=true, `loopzero deploy`
- `multi-line` — script with 5 lines
- `with-caption` — `.env.local` example
- `long-line-scroll` — DNS TXT record (200 chars)

---

## Component 8 — `DangerZone`

### Why

Settings pages universally have a "Danger Zone" section: destructive actions visually separated from normal settings with red border, warning header, and per-action confirm flow. Consumers currently compose `Card` + custom border-destructive + heading manually.

Use cases:
- Settings → Delete project + Transfer ownership
- Team → Delete team
- Profile → Delete account + Reset all data
- Billing → Cancel subscription

4+ confirmed use cases.

### File location

```
src/components/primitives/danger-zone/
  ├── index.ts
  ├── danger-zone.tsx
  ├── danger-zone.test.tsx
  └── danger-zone.stories.tsx
```

### API (sub-components pattern)

```tsx
import { forwardRef } from "react";
import type { HTMLAttributes, ReactNode } from "react";
import { cn } from "../../../lib/cn.js";

export interface DangerZoneProps extends HTMLAttributes<HTMLDivElement> {
  /** Section title. Default "Danger Zone". */
  title?: ReactNode;
}

// Sub-components: DangerZone.Action
//
// <DangerZone>
//   <DangerZone.Action
//     title="Delete project"
//     description="Permanently delete this project and all its data. This cannot be undone."
//     action={<Button intent="destructive" onClick={openConfirm}>Delete</Button>}
//   />
// </DangerZone>

export interface DangerZoneActionProps extends HTMLAttributes<HTMLDivElement> {
  /** Action name (e.g. "Delete project"). */
  title: ReactNode;
  /** Action description / warning. */
  description: ReactNode;
  /** Slot for the destructive button (consumer provides). */
  action: ReactNode;
}
```

### Visual spec

- Outer: `rounded-xl border border-destructive/30 bg-destructive/[0.02]`
- Title row: `border-b border-destructive/20 px-5 py-3 text-label-caps text-destructive uppercase tracking-wider`
- Action row: `flex items-center justify-between gap-4 px-5 py-4`, `border-b border-destructive/10 last:border-0`
- Action title: `text-body-sm font-medium text-foreground`
- Action description: `text-label text-muted-foreground` (mt-0.5)
- Action button: right-aligned, consumer provides (typically destructive variant)

### Edge cases (REQUIRED tests)

1. Default title is "Danger Zone"
2. Multiple actions render with dividers
3. Last action has no bottom border
4. Empty children (no actions) → renders title only
5. axe a11y → semantic region role + heading hierarchy correct

### TDD

```
RED: test_default_title_is_danger_zone
RED: test_custom_title_overrides
RED: test_multiple_actions_render_with_dividers
RED: test_last_action_has_no_bottom_border
RED: test_empty_renders_title_only
RED: test_a11y_axe
GREEN: implement
VERIFY: vitest run danger-zone
```

### Ladle stories (minimum 3)

- `single-action` — Delete project example
- `multiple-actions` — Delete + Transfer + Reset
- `with-confirm-flow` — paired with ConfirmDialog (Component 6) integration demo

---

## Barrel exports

Update `src/index.ts`:

```ts
export { Table, type TableProps, type TableCellProps, type TableHeaderCellProps } from "./components/primitives/table/index.js";
export { StatusDot, type StatusDotProps, type StatusKind } from "./components/primitives/status-dot/index.js";
export { CopyButton, type CopyButtonProps } from "./components/primitives/copy-button/index.js";
export { Timestamp, type TimestampProps } from "./components/primitives/timestamp/index.js";
export { StatTile, type StatTileProps } from "./components/primitives/stat-tile/index.js";
export { ConfirmDialog, type ConfirmDialogProps } from "./components/primitives/confirm-dialog/index.js";
export { CodeBlock, type CodeBlockProps } from "./components/primitives/code-block/index.js";
export { DangerZone, type DangerZoneProps, type DangerZoneActionProps } from "./components/primitives/danger-zone/index.js";
```

Update `package.json` `"exports"` map with 8 new subpaths (mirroring existing pattern).

---

## Acceptance criteria for this brief

- [ ] 8 new primitives created in `src/components/primitives/{table,status-dot,copy-button,timestamp,stat-tile,confirm-dialog,code-block,danger-zone}/`
- [ ] Each ships `.tsx` + `.test.tsx` + `.stories.tsx` + `index.ts`
- [ ] All tests pass (`pnpm test` in `loopzero-ui` repo)
- [ ] All stories axe-validated (zero violations)
- [ ] `pnpm typecheck` passes
- [ ] Barrel `src/index.ts` exports all 8 + relevant types
- [ ] `package.json` `"exports"` map includes 8 new subpaths
- [ ] CHANGELOG `[Unreleased]` → `### Added`: "Table, StatusDot, CopyButton, Timestamp, StatTile, ConfirmDialog, CodeBlock, DangerZone primitives (cross-cutting PaaS dashboard surfaces; consumer: LoopZeroCloud dashboard, Brief #2)"
- [ ] Bundle size delta within library's ±5% budget (8 primitives may push +3-4%; if exceeds, document in CHANGELOG)
- [ ] Zero breaking change to existing components

## Validation against consumer

After implementation, `cloud/dashboard` will:

1. Install new version (`npm install @loopzerokit/ui@<next-version>`)
2. Refactor `BillingPage` invoice history using `<Table>` (T4.4 of consumer plan)
3. Refactor `TeamPage` member list using `<Table>` (T5.1)
4. Replace all `<span className="w-2 h-2 rounded-full bg-*">` with `<StatusDot>` (~7 sites)
5. Add `<CopyButton>` to DNS records (DomainsPage), env vars, API tokens
6. Replace inline date strings with `<Timestamp>` across all pages
7. Replace `OverviewPage` hand-rolled stat tiles with `<StatTile>` (T4.1)
8. Replace manual `<Dialog>` destructive confirms with `<ConfirmDialog>` (Settings, Team, Domains)
9. Use `<CodeBlock>` in EmptyState CLI hints (Overview, Projects, LoginPage)
10. Use `<DangerZone>` in Settings + Profile

If primitives need API adjustments after integration, file follow-ups + bump minor version.

## Out of scope (considered + rejected)

The following were considered but **NOT included** — open separate briefs if usage proves recurrent:

- **`AvatarUploader`** — only 1 page (Profile), composable with Avatar + Input file in ~30 LOC
- **`PaymentMethodCard`** — only 1 page (Billing), too specific to Stripe shape
- **`Pagination`** — Table v1 ships without; consumer slices array. Re-evaluate at >100 rows
- **`Tabs`** — already in `@loopzerokit/ui` inventory ✓
- **`Pagination`** primitive — defer until Table consumer demands
- **`KeyValueList`** — composable trivially: `<dl>...</dl>` in 3 lines
- **`StepIndicator`** / wizard — no wizard flow in current 12 pages
- **`SettingsSection`** — composable from Card + heading in 5 lines
- **`DnsRecord`** — sub-component of existing `DomainConfig` composite, internal
- **`InvoiceRow`** — Table.Row is enough for v1; if invoice display becomes complex, revisit
- **`SparklineChart`** — Overview mockup doesn't have it; cleaner to ship MetricsPanel composite when needed (`metrics-panel` already in inventory)
- **`MemberRow`** — Table.Row is enough
- **`RoleSelector`** — Select primitive + options handles it
- **`FilterBar`** — composable from Input + Select + Button in 10 lines
- **`SortControls`** — Table.HeaderCell `onSort` handles in-place sorting trigger

## Voice / naming guardrails

Same as Brief #1. Names are PaaS-shape semantic (Table, StatTile, CodeBlock), avoid agent-specific vocabulary, use plain English for default labels.

## Cross-reference

- Brief #1: `loopzero/docs/handoff/2026-05-23-loopzero-ui-cloud-dashboard-gaps-brief.md` (4 PaaS-shape primitives — read first)
- Consumer plan: `loopzero/docs/plans/completed/2026-05-23-dashboard-loopzero-ui-migration-plan.md` v1.1
- Consumer mockup: `loopzero/cloud/dashboard/design/loopzero_overview_dashboard_mockup.html`
- Library README: `loopzero-ui/README.md` § Quality Gates
- Library voice: `loopzero-ui/CLAUDE.md` § Voice and Tone
- Existing siblings for API patterns:
  - `loopzero-ui/src/components/primitives/sidebar/sidebar.tsx` — sub-component composition (for Table, DangerZone)
  - `loopzero-ui/src/components/primitives/cost-meter/cost-meter.tsx` — display value + meta primitive (for StatTile)
  - `loopzero-ui/src/components/primitives/badge/badge.tsx` — variant API (for StatusDot)
  - `loopzero-ui/src/components/primitives/dialog/dialog.tsx` — modal foundation (for ConfirmDialog)
  - `loopzero-ui/src/components/primitives/tooltip/tooltip.tsx` — overlay primitive (for Timestamp tooltip)

## Suggested sequencing for implementer

If running this brief in a single session, the dependency graph is:

```
Component 3 (CopyButton) ──▶ Component 7 (CodeBlock uses CopyButton)
                          ──▶ Brief #1 may inline CopyButton in UsageMeter actions

Component 6 (ConfirmDialog) ──▶ relies on existing Dialog primitive only

All others are independent — can be implemented in any order.
```

Recommended order: 3 (CopyButton) → 7 (CodeBlock) → 1 (Table) → 2 (StatusDot) → 4 (Timestamp) → 5 (StatTile) → 6 (ConfirmDialog) → 8 (DangerZone).
