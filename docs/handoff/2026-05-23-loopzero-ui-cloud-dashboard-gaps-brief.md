# Brief: Add 4 PaaS-shape primitives to @loopzerokit/ui

> **Audience:** the `@loopzerokit/ui` maintainer team (or a Claude/Codex agent running in the `loopzero-ui` repo).
> **Output:** 4 new primitives + tests + stories + barrel export, following existing `@loopzerokit/ui` conventions.
> **Issued by:** LoopZeroCloud dashboard team (consumer), 2026-05-23.
> **Tracking:** companion plan `loopzero/docs/plans/completed/2026-05-23-dashboard-loopzero-ui-migration-plan.md` (v1.1).

---

## Why this brief exists

The `cloud/dashboard` (LoopZeroCloud's React SPA) is migrating to `@loopzerokit/ui@0.6.3-next.0` as canonical design system (see companion plan). Gap analysis against the reference mockup `cloud/dashboard/design/loopzero_overview_dashboard_mockup.html` (Vercel-style overview) revealed that **~95% of the mockup maps directly to existing primitives/composites**, but **4 PaaS-shape gaps** force consumers to either misuse agent-first primitives or build local components that should belong upstream.

The current library is **agent-first** by design (intentional positioning per `loopzero-ui/PITCH.md`): `CostMeter` is single-metric in USD for token spend; `ProjectSwitcher` carries workspace+branch+agent-status context. PaaS dashboards (LoopZeroCloud, and future external consumers like Vercel/Fly/Railway look-alikes) need PaaS-shape equivalents.

**Add the 4 primitives below. Do NOT modify existing components** — that would risk breaking agent-shape consumers. These are new, sibling primitives.

---

## Quality bar (reminder — already enforced by your CI)

Every new primitive MUST:

- Follow the `forwardRef<...>` + `displayName` pattern (see `primitives/sidebar/sidebar.tsx`)
- Use `cn()` from `lib/cn.js` for class composition
- Use `lucide-react` icons (already a peer-dep) — never bundle SVG inline
- Use only design tokens (`bg-card`, `text-foreground`, `text-muted-foreground`, `border-border/40`, `bg-primary/10`, etc.) — never hex colors hard-coded
- Use typography scale (`text-body-sm`, `text-label-caps`, `font-mono`, `font-display`) — never `text-xs`/`text-sm` raw
- Ship `<component>.tsx` + `<component>.test.tsx` + `<component>.stories.tsx` + `index.ts` barrel
- Pass `vitest run` (axe-validated via existing test infra)
- Pass `npm run typecheck` (strict TS)
- Add to `src/index.ts` top-level barrel export
- Add to `package.json` `"exports"` map under `./<kebab-name>` if you ship subpath exports per existing convention

---

## Component 1 — `UsageMeter`

### Why

`CostMeter` is mono-metric in USD (`cost + budget` only). PaaS dashboards display **multi-metric stacked usage** with arbitrary units (bytes, requests, build-minutes, seats). The reference mockup `loopzero_overview_dashboard_mockup.html` lines 49-58 shows:

- Title row: "Last 30 days" + "Upgrade" badge (right-aligned)
- 3 stacked rows: each = label + value/max + 4px progress bar
- Examples: "Fast Data Transfer · 0 / 100 GB", "Edge Requests · 0 / 1M"

This pattern repeats in every PaaS dashboard (Vercel, Fly, Railway, Render, Heroku).

### File location

```
src/components/primitives/usage-meter/
  ├── index.ts
  ├── usage-meter.tsx
  ├── usage-meter.test.tsx
  └── usage-meter.stories.tsx
```

### API

```tsx
import { forwardRef } from "react";
import type { HTMLAttributes, ReactNode } from "react";
import { cn } from "../../../lib/cn.js";
import { Progress } from "../progress/index.js"; // see Component 2 below

export interface UsageMetric {
  /** Display label (e.g. "Fast Data Transfer"). */
  label: ReactNode;
  /** Current consumption. */
  value: number;
  /** Maximum allowed in the current period. */
  max: number;
  /** Unit string (e.g. "GB", "req", "min"). Rendered after value/max. */
  unit?: string;
  /** Optional custom formatter — overrides default `${value} / ${max} ${unit}`. */
  formatter?: (value: number, max: number, unit?: string) => string;
}

export interface UsageMeterProps extends Omit<HTMLAttributes<HTMLDivElement>, "title"> {
  /** Card title (e.g. "Last 30 days", "This billing period"). */
  title?: ReactNode;
  /** Optional right-aligned action slot (e.g. an Upgrade Badge or Button). */
  action?: ReactNode;
  /** Array of metrics to display. Order preserved. */
  metrics: UsageMetric[];
  /** When true, show only the bars without label/value text (sparkline mode). */
  compact?: boolean;
}
```

### Composition example

```tsx
<UsageMeter
  title="Last 30 days"
  action={<Badge variant="outline">Upgrade</Badge>}
  metrics={[
    { label: "Fast Data Transfer", value: 0, max: 100, unit: "GB" },
    { label: "Fast Origin Transfer", value: 0, max: 10, unit: "GB" },
    { label: "Edge Requests", value: 0, max: 1_000_000, unit: "req", formatter: (v, m) => `${v} / ${m >= 1_000_000 ? `${m / 1_000_000}M` : m}` },
  ]}
/>
```

### Visual spec

- Outer container: `Card`-like (`rounded-xl border bg-card p-4`)
- Header: title left, action right (`flex justify-between items-baseline`)
- Each metric row: label left, value/max right, then `Progress` bar below (4px tall, full width, `rounded-full`)
- Spacing between rows: `gap-3` or `gap-2`
- Typography: title `text-label-caps text-muted-foreground uppercase tracking-wider`; metric labels `text-body-sm text-muted-foreground`; values `font-mono tabular-nums`

### Edge cases (REQUIRED tests)

1. `value > max` → bar fills 100% AND adds `text-warning` to value text (over-quota)
2. `max === 0` → renders bar at 0% without divide-by-zero error
3. `metrics === []` → renders empty (with title if provided)
4. `formatter` undefined → default format `${value} / ${max} ${unit}` with space before unit
5. `compact === true` → only stack of bars, no labels/values (used in narrow nav-bar slots)
6. SSR/no-window: must render server-side without `useEffect`-only logic

### TDD

```
RED:  test_renders_metric_rows_in_order — 3 metrics in array → 3 rows in DOM order
RED:  test_progress_fills_proportionally — value=50, max=100 → progress aria-valuenow=50
RED:  test_over_quota_warning — value=120, max=100 → bar 100% + text has warning class
RED:  test_zero_max_no_crash — max=0 → no Infinity/NaN in DOM
RED:  test_custom_formatter — formatter returns "5 of 10" → DOM contains "5 of 10"
RED:  test_compact_hides_labels — compact=true → labels not in DOM, bars present
RED:  test_a11y_axe — axe-core asserts zero violations
GREEN: implement
VERIFY: vitest run usage-meter
```

### Ladle stories (minimum 4)

- `default` — 3 metrics, all under-quota, with Upgrade action
- `over-quota` — one metric above cap (visual warning)
- `compact` — bars-only variant
- `single-metric` — 1 metric only (no array overhead)

---

## Component 2 — `Progress`

### Why

`UsageMeter` (Component 1) requires a primitive `Progress` bar. The library currently exposes `ProgressChecklist` (multi-step checklist for agent tasks) but **no standalone progress bar**. Mockup uses 4px-tall rounded bars in 3 places; PaaS dashboards use them everywhere (build progress, deploy phase, quota fill, file upload).

### File location

```
src/components/primitives/progress/
  ├── index.ts
  ├── progress.tsx
  ├── progress.test.tsx
  └── progress.stories.tsx
```

### API

```tsx
import { forwardRef } from "react";
import type { HTMLAttributes } from "react";
import { cn } from "../../../lib/cn.js";

export interface ProgressProps extends Omit<HTMLAttributes<HTMLDivElement>, "role"> {
  /** Current value (0..max). */
  value: number;
  /** Maximum value. Defaults to 100. */
  max?: number;
  /** Visual intent — controls fill color. */
  intent?: "default" | "success" | "warning" | "destructive";
  /** Bar height in tailwind units. Defaults to "h-1" (4px). */
  height?: "h-1" | "h-1.5" | "h-2" | "h-3";
  /** When true, indeterminate animated bar (no value). */
  indeterminate?: boolean;
  /** Accessible label override. Required if not preceded by `aria-labelledby` element. */
  "aria-label"?: string;
}
```

### Implementation notes

- Outer: `role="progressbar"` + `aria-valuenow={value}` + `aria-valuemin={0}` + `aria-valuemax={max}`
- If `indeterminate`: omit `aria-valuenow`, add `aria-busy="true"`, animate fill (`animate-pulse` or sliding gradient)
- Fill width: `width: ${Math.min(100, Math.max(0, (value / max) * 100))}%`
- Intent color map: `default → bg-primary`, `success → bg-success`, `warning → bg-warning`, `destructive → bg-destructive`
- Background track: `bg-muted` with `rounded-full`
- Fill respects `prefers-reduced-motion` (no animation on transition when set)

### Edge cases (REQUIRED tests)

1. `value > max` → fill clamped at 100%, `aria-valuenow=max`
2. `value < 0` → fill 0%, `aria-valuenow=0`
3. `max === 0` → fill 0%, no NaN in style
4. `indeterminate` → no `aria-valuenow`, has `aria-busy`
5. `intent="warning"` → fill class includes `bg-warning`
6. axe a11y → zero violations, role correct

### TDD

```
RED: test_renders_with_aria_progressbar
RED: test_value_clamping (above max, below zero)
RED: test_indeterminate_omits_aria_valuenow
RED: test_intent_color_mapping
RED: test_a11y_axe
GREEN: implement
VERIFY: vitest run progress
```

### Ladle stories (minimum 5)

- `default` — value=50, max=100
- `success` / `warning` / `destructive` — one per intent
- `indeterminate` — animated, no value
- `heights` — h-1 / h-1.5 / h-2 / h-3 side-by-side

---

## Component 3 — `PlanBadge`

### Why

PaaS pricing tiers (Hobby/Free, Pro, Team, Enterprise) need consistent semantic styling. The mockup line 7 shows `Hobby` rendered as an orange-bordered pill (`#C96442` border + text), distinct from the generic `Badge`. Misusing `Badge variant="outline"` with custom colors per tenant leaks design tokens to consumers.

This is **semantic over visual**: `<PlanBadge plan="hobby" />` self-documents intent. Future re-styling (rebrand, dark/light mode tweaks) propagates without consumers re-mapping colors.

### File location

```
src/components/primitives/plan-badge/
  ├── index.ts
  ├── plan-badge.tsx
  ├── plan-badge.test.tsx
  └── plan-badge.stories.tsx
```

### API

```tsx
import { forwardRef } from "react";
import type { HTMLAttributes } from "react";
import { cn } from "../../../lib/cn.js";

export type PlanTier = "free" | "hobby" | "pro" | "team" | "enterprise";

export interface PlanBadgeProps extends HTMLAttributes<HTMLSpanElement> {
  /** Plan tier identifier. */
  plan: PlanTier;
  /** Override the display label (defaults to capitalized plan name). */
  label?: string;
  /** Size variant. */
  size?: "sm" | "md";
}
```

### Visual spec

| Plan | Background | Border | Text |
|---|---|---|---|
| `free` | `bg-muted/40` | `border-muted-foreground/20` | `text-muted-foreground` |
| `hobby` | `bg-warning/10` | `border-warning/30` | `text-warning` |
| `pro` | `bg-primary/10` | `border-primary/30` | `text-primary` |
| `team` | `bg-success/10` | `border-success/30` | `text-success` |
| `enterprise` | `bg-foreground/5` | `border-foreground/20` | `text-foreground` |

All use `rounded-md` (6px) and `font-mono` `text-label` (tabular numerals, small caps).

Default label is `plan.charAt(0).toUpperCase() + plan.slice(1)` (so `hobby → Hobby`, `enterprise → Enterprise`).

### Edge cases (REQUIRED tests)

1. Each tier renders distinct color classes (no shared variant collision)
2. `label` override is rendered verbatim (case-preserved)
3. Unknown plan tier → TypeScript prevents at compile time; runtime fallback to `free` styling

### TDD

```
RED: test_renders_each_tier_with_distinct_styling (5 tiers)
RED: test_default_label_capitalizes_plan
RED: test_custom_label_overrides
RED: test_size_sm_vs_md
RED: test_a11y_axe
GREEN: implement
VERIFY: vitest run plan-badge
```

### Ladle stories (minimum 2)

- `tiers` — all 5 tiers side-by-side, sm and md sizes
- `with-custom-labels` — `<PlanBadge plan="enterprise" label="Custom" />`

---

## Component 4 — `AccountMenu` (sidebar header for PaaS)

### Why

`ProjectSwitcher` carries workspace + branch + agent-status — designed for code-agent surfaces (Claude Code, Cursor). PaaS dashboards (LoopZeroCloud, Vercel-like) have a **different sidebar header semantics**: account/user identity + plan tier + (often) workspace switcher.

Mockup lines 4-8 show this: avatar + `paulohenriquevn` + `Hobby` badge in the sidebar header. Using `ProjectSwitcher` would force "branch" as a required slot with no PaaS meaning. Local composition (Avatar + text + Badge) works but leaks the pattern to every consumer.

### File location

```
src/components/primitives/account-menu/
  ├── index.ts
  ├── account-menu.tsx
  ├── account-menu.test.tsx
  └── account-menu.stories.tsx
```

### API

```tsx
import { forwardRef } from "react";
import type { ButtonHTMLAttributes, ReactNode } from "react";
import { cn } from "../../../lib/cn.js";
import type { PlanTier } from "../plan-badge/index.js";

export interface AccountMenuProps
  extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, "type" | "children"> {
  /** Display name (username, email, org name). */
  name: ReactNode;
  /** Avatar URL or single-letter fallback. If string and not URL, treated as initials. */
  avatar?: string;
  /** Plan tier — renders inline PlanBadge. Omit for none. */
  plan?: PlanTier;
  /** Optional secondary line below name (e.g. email). */
  secondary?: ReactNode;
  /** When provided, renders chevron indicator and makes the row interactive (button). */
  onClick?: () => void;
}
```

### Composition example

```tsx
<Sidebar.Header className="p-0">
  <AccountMenu
    name="paulohenriquevn"
    avatar="https://avatars.githubusercontent.com/u/12345"
    plan="hobby"
    onClick={openAccountSwitcher}
  />
</Sidebar.Header>
```

### Visual spec

- Outer: `<button>` (if `onClick`) or `<div>` — full width of sidebar header, `flex items-center gap-3 px-3 py-2`
- Avatar: 24px circle (`Avatar` primitive internal use, fallback to initials)
- Stacked text: `name` (`text-body-sm font-medium`) + optional `secondary` (`text-label text-muted-foreground`)
- `plan` rendered as `<PlanBadge plan={plan} size="sm" />` inline-aligned with name
- If `onClick`: trailing `ChevronsUpDown` icon (`size-3 text-muted-foreground`)
- Hover state: `hover:bg-muted/40 transition-colors`
- Focus visible: standard `focus-visible:ring-2 focus-visible:ring-ring`

### Edge cases (REQUIRED tests)

1. No `avatar` → renders initials from `name` (first char, uppercased)
2. `avatar` is URL → renders `<img>` with proper alt text
3. No `onClick` → renders as `<div>`, no chevron, not in tab order
4. With `onClick` → renders as `<button>`, has chevron, focusable, fires onClick on Enter/Space
5. No `plan` → no PlanBadge rendered
6. Long `name` → truncates with `text-ellipsis overflow-hidden`
7. axe a11y → zero violations

### TDD

```
RED: test_initials_fallback_when_no_avatar
RED: test_renders_img_when_avatar_is_url
RED: test_no_chevron_when_not_interactive
RED: test_chevron_and_button_when_clickable
RED: test_keyboard_activation_fires_onclick
RED: test_plan_badge_renders_when_plan_provided
RED: test_long_name_truncates
RED: test_a11y_axe
GREEN: implement
VERIFY: vitest run account-menu
```

### Ladle stories (minimum 4)

- `default` — name + avatar + hobby plan + onClick
- `static` — no onClick (read-only display)
- `no-avatar` — initials fallback
- `with-secondary` — name + email below

---

## Barrel exports

Update `src/index.ts`:

```ts
export { UsageMeter, type UsageMeterProps, type UsageMetric } from "./components/primitives/usage-meter/index.js";
export { Progress, type ProgressProps } from "./components/primitives/progress/index.js";
export { PlanBadge, type PlanBadgeProps, type PlanTier } from "./components/primitives/plan-badge/index.js";
export { AccountMenu, type AccountMenuProps } from "./components/primitives/account-menu/index.js";
```

Update `package.json` `"exports"` map:

```json
"./usage-meter": { "types": "./dist/index.d.ts", "import": "./dist/index.js" },
"./progress": { "types": "./dist/index.d.ts", "import": "./dist/index.js" },
"./plan-badge": { "types": "./dist/index.d.ts", "import": "./dist/index.js" },
"./account-menu": { "types": "./dist/index.d.ts", "import": "./dist/index.js" }
```

---

## Acceptance criteria for this brief

- [ ] 4 new primitives created in `src/components/primitives/{usage-meter,progress,plan-badge,account-menu}/`
- [ ] Each primitive ships `.tsx` + `.test.tsx` + `.stories.tsx` + `index.ts`
- [ ] All tests pass (`pnpm test` or equivalent in `loopzero-ui` repo)
- [ ] All stories pass axe-core (`pnpm test:a11y` if exists, or vitest-axe assertions)
- [ ] `pnpm typecheck` passes (strict mode)
- [ ] Barrel `src/index.ts` exports all 4 + relevant types
- [ ] `package.json` `"exports"` map includes 4 new subpaths
- [ ] CHANGELOG entry under `[Unreleased]` → `### Added`: "UsageMeter, Progress, PlanBadge, AccountMenu primitives (PaaS-shape sibling of CostMeter/ProgressChecklist/Badge/ProjectSwitcher; consumer: LoopZeroCloud dashboard)"
- [ ] Bundle size delta within library's ±5% budget
- [ ] No breaking change to existing components

## Validation against consumer mockup

After implementation, the consumer (`cloud/dashboard`) will validate by:

1. Installing the new version (`npm install @loopzerokit/ui@<next-version>`)
2. Replacing the `CostMeter` attempt in `pages/overview.tsx` with `<UsageMeter>`
3. Replacing the local `<div className="dark bg-surface-1 ...">` sidebar with `<Sidebar.Header><AccountMenu plan="hobby" .../></Sidebar.Header>`
4. Replacing custom `<Badge>` plan pills with `<PlanBadge plan="hobby" />`
5. Smoke-test against `cloud/dashboard/design/loopzero_overview_dashboard_mockup.html` — visual delta < 5%

If any of the 4 primitives need API adjustments after consumer integration, file follow-up issue + bump minor version.

## Voice / naming guardrails

Follow `loopzero-ui/CLAUDE.md` voice and Cross-Project Rule 9. New primitive names MUST:

- Be PaaS-shape (`UsageMeter`, not `MetricsCard`)
- Avoid agent-specific vocabulary (no `Token*`, `Cost*`, `Skill*`)
- Be composable (sub-components allowed: `AccountMenu.Avatar`, `AccountMenu.Plan` if helpful — TBD by implementer)
- Use plain English for default labels (no `enterprise → Enterprise Tier` — just `Enterprise`)

## Out of scope

The following were considered but **NOT included** in this brief — open separate briefs if needed:

- `Breadcrumb` primitive — `topnav` already covers the mockup's top bar
- `DropdownMenu` primitive — already accessible via Radix peer-dep
- `ResourceCard` / generic icon+title+meta+action — `ProjectCard` + `DeploymentRow` + composable `Card` cover known cases
- Multi-metric chart (timeseries) — beyond mockup scope; would be a v0.7 feature ticket
- `UpgradePromptCard` — `EmptyState` + `Button` composes this in 5 lines; no abstraction benefit yet

## Cross-reference

- Consumer plan: `loopzero/docs/plans/completed/2026-05-23-dashboard-loopzero-ui-migration-plan.md` v1.1
- Consumer mockup: `loopzero/cloud/dashboard/design/loopzero_overview_dashboard_mockup.html`
- Library README: `loopzero-ui/README.md` § Quality Gates (1.123 tests passing, 126 Ladle stories, axe-validated)
- Library voice: `loopzero-ui/CLAUDE.md` § Voice and Tone
- Existing siblings for API patterns:
  - `loopzero-ui/src/components/primitives/cost-meter/cost-meter.tsx` — closest semantic sibling to `UsageMeter`
  - `loopzero-ui/src/components/primitives/sidebar/sidebar.tsx` — composition pattern for sub-components
  - `loopzero-ui/src/components/primitives/project-switcher/project-switcher.tsx` — composition pattern for clickable-vs-static dual mode
  - `loopzero-ui/src/components/primitives/badge/badge.tsx` — variant API pattern for `PlanBadge` reference
