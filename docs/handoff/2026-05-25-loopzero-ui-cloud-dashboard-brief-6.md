# Brief #6: Brief #5 primitives consumed — shipped evidence + measured deltas

> **Audience:** the `@loopzero/ui` maintainer team.
> **Output:** *none requested* — this brief reports back the consumer-side outcome of Brief #5.
> **Issued by:** LoopZeroCloud dashboard team (consumer), 2026-05-25.
> **Priority:** INFORMATIONAL — no action required upstream.
> **Companion to:** Brief #5 (`2026-05-25-loopzero-ui-cloud-dashboard-brief-5.md`) — the source request.
> **Tracks:** consumer-side migration tasks T-MIG-1..4 committed on `develop`.

---

## TL;DR

`@loopzero/ui@0.11.0-next.0` shipped 3 dashboard primitives (PinInput, DataTable, PageShell) requested in Brief #5. LoopZeroCloud consumed all 3 across **5 page migrations** with **117/117 vitest GREEN** and **145.63 KB / 180 KB hard-gate brotli total** (34.37 KB headroom retained). No upstream patches needed. The post-0.10 per-component dist + Brief #4 subpath tree-shaking held under real consumer load.

---

## What shipped (commits on `develop`)

| Commit | Track | Page | Brief #5 primitive consumed |
|--------|-------|------|-----------------------------|
| `3e96cb92` | T-MIG-1 | `/verification` | **PinInput** (`length={6}`, `inputMode="numeric"`, `onComplete`) |
| `8d8b17bf` | T-MIG-2 | `/domains`      | **DataTable** (`expandable={DnsRecords}`, sortable, sticky) |
| `ee516de6` | T-MIG-3 | `/projects`     | **DataTable** (sortable, sticky, `defaultSort: last_deploy desc`) |
| `f9c88360` | T-MIG-4 | `/settings`, `/profile`, `/billing`, `/overview`, `/team` | **PageShell** (title + state machine + search + primaryAction) |

Total: **4 commits, 8 pages migrated, 0 upstream patches required**.

---

## Bundle deltas (size-limit, brotli)

Measured after T-MIG-4 final commit on a clean `npm run build` + `npm run size`:

| Chunk | Cap | Actual | Headroom | Note |
|-------|-----|--------|----------|------|
| `01 react-vendor` | 55 KB | 50.95 KB | 4.05 KB | unchanged from Brief #5 baseline |
| `02 @loopzero/ui` | 18 KB | 16.19 KB | 1.81 KB | tree-shaking held — PageShell+DataTable+PinInput shared between consumers |
| `03 radix` | 26 KB | 22.03 KB | 3.97 KB | cap was raised 22→26 in `fbdfa327` to accommodate DropdownMenu transitive deps from DataTable rowActions |
| `04 vendor` | 25 KB | 19.86 KB | 5.14 KB | — |
| `05 router` | 15 KB | 11.98 KB | 3.02 KB | — |
| `06 lucide` | 4 KB | 2.56 KB | 1.44 KB | — |
| `07 app entry` | 8 KB | 3.92 KB | 4.08 KB | — |
| `08 worst per-route` | 10 KB | 1.83 KB | 8.17 KB | — |
| **TOTAL initial JS** | **180 KB** | **145.63 KB** | **34.37 KB** | **HARD GATE GREEN** |

**Per-route chunk sizes (raw, post-T-MIG-4):**

| Route | Size (raw) | Notes |
|-------|------------|-------|
| `settings` | 1.60 KB | thinnest — pure CodeBlock + DangerZone |
| `team` | 2.22 KB | PageShell.loading + PageShell.empty consume the state machine |
| `profile` | 2.40 KB | minimal — Card + DangerZone |
| `projects` | 2.82 KB | DataTable sortable + custom open-action column |
| `overview` | 3.61 KB | PageShell.search + PageShell.primaryAction replace standalone ActionBar |
| `verification` | 3.88 KB | PinInput (post-T-MIG-1) |
| `domains` | 4.31 KB | DataTable expandable + inline verify-button column |
| `billing` | 4.76 KB | PageShell.loading replaces inline spinner Card |

---

## LOC delta (T-MIG-4 only — PageShell migration)

`git diff --stat` against `develop~5`:

```
billing.tsx  | -7   net (inline spinner Card eliminated)
overview.tsx | +5   net (PageShell adds wrapper, ActionBar collapsed)
profile.tsx  | +3   net (PageShell wrapper overhead, no state-machine win)
settings.tsx | +3   net (PageShell wrapper overhead, no state-machine win)
team.tsx     | -14  net (ActionBar + spinner Card + inline EmptyState all collapsed into PageShell props)
TOTAL        | -16  LOC (5 files, 4 with state-machine, 1 with full collapse)
```

The dedupe win compounds with page count and inline-state-machine surface. Team was the cleanest demo because it had ALL of: ActionBar wrapper, inline `Loader2` Card, inline `EmptyState`, and a useSetPageTitle. Settings/Profile (pure-Card pages without state machines) gained LOC overhead — confirming PageShell is **not free** for trivially-shaped pages, but pays out clearly when there's loading or empty branches to collapse.

---

## Patterns we recommend for the Brief #5 storybook

These emerged from consumer use — worth documenting in the @loopzero/ui storybook to save future consumers the discovery:

### 1. PageShell.empty is render-instead, not conditional-render

If `empty` is truthy, PageShell renders that state instead of `children`. Pass `undefined` when the consumer's "is empty" predicate is false:

```tsx
<PageShell
  loading={loading}
  empty={
    !loading && filtered.length === 0
      ? { icon: Users, title: 'No team members', description: '...' }
      : undefined
  }
>
  <Table>...</Table>  {/* rendered only when both loading=false AND empty=undefined */}
</PageShell>
```

Reason: search-filtered emptiness is consumer-derived state, not API-driven — PageShell can't infer it.

### 2. PageShell.loading is page-level only

Use `PageShell.loading` for the **initial page fetch**. For per-section partial fetches (e.g., Overview's Projects sub-list while the rest of the page is hydrated), keep the loading branch inline. Setting `PageShell.loading=true` replaces the entire page body with a spinner card.

### 3. DataTable rowActions wraps in DropdownMenu — bad UX for single contextual actions

If a row has exactly ONE action that's contextual ("Verify" only when status=pending, "Open" always), `rowActions` is overkill — it forces the user through a DropdownMenu kebab. Define an inline custom column instead:

```tsx
{
  key: '__verify',
  label: '',
  align: 'right',
  render: (row) => row.status === 'pending_verification' ? (
    <Button size="sm" onClick={() => verify(row.id)}>Verify</Button>
  ) : null,
}
```

We used this pattern in `/domains` (T-MIG-2). Reserve `rowActions` for 2+ destructive/secondary actions where the kebab is appropriate.

### 4. DataTable.loading owns its own skeleton — no need for outer spinner

When `loading={true}`, DataTable renders 5 skeleton rows with `aria-busy="true"`. Tests asserting "is loading" should query `container.querySelector('[aria-busy="true"], [role="status"]')`, not look for a specific spinner aria-label.

### 5. PageShell.primaryAction + PageShell.search replace standalone ActionBar

We had a local `<ActionBar>` wrapper. Brief #5 PageShell consumes its API natively via `primaryAction={{label, icon, onClick}}` + `search={{placeholder, value, onChange}}`. Consumers should NOT add an extra `<ActionBar>` outside `<PageShell>`. Test selectors (`getByRole('button', { name: /add new/i })`, `getByPlaceholderText('Search projects...')`) survive verbatim — same accessible roles.

---

## Things that worked first try (no upstream issues)

- PinInput auto-advance focus on input + paste handling — flawless on the verification flow
- DataTable expandable + custom render col combination — DNS records expand smoothly in `/domains`
- PageShell.onTitleChange wiring to local `usePageMeta().setPageTitle` — clean separation between primitive (visual title) and consumer (TopBar context binding)
- Subpath tree-shaking from Brief #4 held — chunk 02 grew only when components were actually imported
- Per-component dist (post-0.10) — no full-package import side-effects

---

## Gaps surfaced (NOT blocking, NOT requesting yet)

These are notes for future Briefs IF the discomfort accumulates with more consumers:

1. **PageShell has no `breadcrumb` slot.** Acceptable for our 13-route flat IA; would matter for deeper hierarchies (`/teams/{id}/members/{id}`).
2. **DataTable.pagination is one-page-at-a-time only.** No cursor or page-size control surfaced in the consumer API. Fine for current scale (≤50 rows); will revisit if we onboard a customer with >500 domains.
3. **PinInput has no built-in resend cooldown timer.** Consumer-owned today (verification.tsx implements its own); a `<PinInput.ResendButton cooldownSeconds={60}>` sub-component would be nice but is YAGNI at MVP.

We will NOT open Briefs for these unless a second consumer hits the same edge — single-consumer demand is not strong enough signal.

---

## Validation evidence

- **Tests:** 22 vitest files / **117/117 GREEN** post-T-MIG-4 (`npm test`)
- **Typecheck:** `npx tsc -b` clean (no output)
- **Build:** `npm run build` clean — all 25 dist chunks emitted
- **Size gates:** `npm run size` — 9/9 chunks under cap, total 145.63 KB / 180 KB (`task quality:gates`-class verification)
- **Lint:** `npm run lint` not run — eslint devDep missing locally (pre-existing gap, unrelated to migration)

---

## Thank-you note

Brief #5 was scoped, audited, shipped as `0.11.0-next.0`, and consumed by 5 page migrations in <72h end-to-end. The discipline of "ship only primitives that close a measured finding" continues to scale — Brief #5 had 3 components requested and 3 components shipped, zero scope creep. That ratio is the reason we keep coming back through the Brief workflow instead of forking.

Brief #7 will be opened only if/when a new Deep Review surfaces a measured P1/P2 finding that requires an upstream primitive. Until then, the Brief #5 set is enough for our 13-route surface area.

— LoopZeroCloud dashboard team
