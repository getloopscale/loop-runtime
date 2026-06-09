# LoopZeroCloud Dashboard — Design System Contract

> **Locked design system for `cloud/dashboard/`.**
> This file is the **single source of truth** for UX/UI standards. All other artifacts (audits, plans, code review, LLM-assisted generation) defer to this.
> Per `loopzero-ui` skill convention: presence of `DESIGN.md` at project root makes it the locked system — pre-flight scan reads this first and respects every clause.

**Version:** 1.0 · **Locked:** 2026-05-25 · **Owner:** Paulo (sponsor) + Staff Dashboard Engineering
**Related:** [audit](.claude/knowledge-base/audits/2026-05-25/dashboard-faang-review.md) · [plan](.claude/knowledge-base/plans/2026-05-25-dashboard-faang-remediation-plan.md) · [architecture](.claude/knowledge-base/architecture/dashboard/)

---

## 0. Quality bar (the meta-rule)

> *"Um desenvolvedor deve conseguir operar uma aplicação em produção com confiança, sem precisar entender Kubernetes, Argo, Cloudflare, cells, rollout internals ou infraestrutura interna."*

The UI hides complexity **without lying**.

The dashboard must feel: **Rápido · Calmo · Técnico · Confiável · Preciso · Operacional · Premium · Simples sem ser superficial.**

The success criterion is the developer thinking: **"Eu confio em colocar produção aqui."**

---

## 1. The 5Q-3s rule (clarity before beauty)

Every screen MUST answer 5 questions in ≤ 3 seconds:

1. **Onde estou?** (page title visible, breadcrumb when nested)
2. **O que está acontecendo?** (current state observable at a glance)
3. **Qual é a ação principal?** (one CTA dominant)
4. **Existe algum risco?** (destructive zones isolated)
5. **O que eu faço agora?** (next-step CTA OR empty-state guidance)

**Bad:** generic title + 5 competing buttons + ambiguous status.
**Good:** specific title + 1 primary CTA + status pill + 2-3 secondary actions.

**Acceptance gate (universal):** No screen ships without manual 5Q-3s validation by reviewer.

---

## 2. Operational anxiety reduction

The dashboard touches production. Confidence is a feature.

### 2.1 Production vs non-production visual distinction

Every screen that operates on a specific environment MUST show the environment badge:

| Environment | Visual | Where |
|---|---|---|
| `production` | Red dot + uppercase "PROD" badge | TopBar context + every action button context |
| `staging` | Yellow dot + "STAGING" badge | Same |
| `preview` | Blue dot + "PREVIEW" badge | Same |
| `development` | Gray dot + "DEV" badge | Same |

Implementation: `<EnvironmentBadge env={env}>` composite (T4.6 from plan).

### 2.2 Destructive action choreography

Pattern, in order:

1. Action lives in `<DangerZone>` section (visually segregated, red border).
2. Click triggers `<ConfirmDialog>` with:
   - Title: `<Action> <Resource>` (e.g., "Delete project acme-api").
   - Impact paragraph: what changes, what stops working.
   - Reversibility statement: "This cannot be undone." OR "You have N days to restore."
   - Affected resources list: enumerated explicit.
   - Confirmation phrase: typed verbatim (e.g., "delete acme-api").
3. Submit: shows progress (`<Loader2>` icon + label change).
4. Success: `<Toaster>` confirmation + navigate to safe context.

Never destructive action with a `variant="primary"` Button — always `variant="destructive"`.

### 2.3 Production speed/safety asymmetry

> **If the user can break production, the UI slows the action down. If the user is browsing, the UI is fast.**

- Navigation: instant (<100ms perceived).
- Read: optimistic.
- Write: feedback in <200ms.
- Production write: confirmation dialog mandatory.
- Production destructive: confirmation phrase mandatory.

---

## 3. Design system as contract (not suggestion)

The dashboard uses `@loopzerokit/ui@0.12.0-next.0+` as canonical component library.

### 3.1 Hard rules

- **Zero inline hex.** Use tokens via Tailwind preset (`bg-primary`, `text-foreground`, `border-border`) or CSS vars (`hsl(var(--primary))`).
- **Zero loose spacing values.** Use spacing tokens (`p-4`, `gap-6`); no `padding: 18px`.
- **Zero loose font weights.** Geist with 400 / 500 / 600 only.
- **Zero glassmorphism, blur decorativo, backdrop-filter.** Violet Forge rejects.
- **Zero custom sombras sem token.** Use `shadow-sm`, `shadow-md`, `shadow-lg` tokens.
- **Zero variações visuais "para resolver problema local".** If pattern recurs ≥2 times, becomes composite.
- **Zero duplicação de composite existente.** Grep before building.
- **Zero emojis em UI** (labels, botões, errors, system messages).
- **Zero densidades mistas na mesma surface.** Pick `comfortable` / `compact` / `spacious` per page.

### 3.2 Required composites (per surface type)

| Surface type | Anchor composites |
|---|---|
| List/table page | `PageShell` + `ActionBar` + `DataTable` + `DropdownMenu` |
| Detail page | `PageShell` + `Tabs` + `Card` + `ProjectHealthCard` |
| Settings page | `PageShell` + `Card[stacked-forms]` + `DangerZone` + `ConfirmDialog` |
| Auth page | `Card` + `Input` + `Button` + `Alert` + `PinInput` (for OTP) |
| Logs/stream | `LogViewer` (composite, not raw `<pre>`) |
| Deploy progress | `DeployTimeline` + `BuildStepList` |
| Canary | `CanaryProgress` |

Any new recurring need becomes an upstream contribution (per ADR D3 of remediation plan).

### 3.3 Tokens budget

| Token category | Source |
|---|---|
| Color | `@loopzerokit/ui/preset.css` + Violet Forge theme |
| Typography | Geist Sans + Geist Mono (via `fonts-cdn.css`) |
| Spacing | Tailwind v4 default scale |
| Radius | `rounded-{sm,md,lg,xl}` |
| Motion | `--motion-{instant,fast,base,slow}` in `index.css:113-119` |
| Easing | `--easing-{standard,decelerate,accelerate}` |

Local overrides allowed ONLY in `cloud/dashboard/src/index.css @theme` block with justification comment.

---

## 4. Task-oriented UX

Each screen is designed around **one dominant task**:

| Screen | Dominant task |
|---|---|
| Overview | "Is everything OK?" (health summary) |
| Projects list | "Open a project I worked on" |
| Project detail | "Operate THIS project" |
| Deployments | "Find a specific deploy / monitor in-progress" |
| Deploy detail | "Understand why this deploy is in state X" |
| Logs | "Find why something broke" |
| Rollback dialog | "Revert with confidence" |
| Promote flow | "Move staging → prod safely" |
| Secrets | "Manage env vars without leaking" |
| Domains | "Attach a custom domain" |
| Team | "Manage who has access" |
| Billing | "Understand my cost + upgrade" |
| Settings | "Configure workspace" |
| Profile | "Manage my identity" |

Secondary actions never compete with the primary CTA. If they compete, redesign.

### 4.1 No dead pages

A screen that only displays information without action is a dead page. Every screen needs at least one actionable next step:
- Empty state → "Create your first X" CTA
- Populated state → "Manage X" action per row
- Error state → "Retry" / "Fix" / "Contact support" CTA

---

## 5. Empty states sell competence

### 5.1 Required 4-element structure

```
<EmptyState
  icon={<RelevantIcon />}
  title="<What doesn't exist yet (positive framing)>"
  description={
    <>
      <Why this matters in 1 sentence.>
      <How to create it: action OR CLI command via CodeBlock.>
      <What happens next.>
    </>
  }
  cta={<PrimaryAction />}
  ctaSecondary={<CliFallback />}
/>
```

### 5.2 Examples

**Bad:** `No deployments yet.`

**Good:**
> **Deploy your first app**
>
> LoopZero builds your code into signed container images and ships them to a live URL.
>
> Connect a GitHub repo or run `loopzero deploy` from your project directory.
>
> *[Connect GitHub]* *[Copy CLI]*

**Bad:** `No projects.`

**Good:**
> **Start your first project**
>
> A project groups your apps, environments, and domains.
>
> Pick a GitHub repo and LoopZero detects the framework automatically. ~4 minutes to a live URL.
>
> *[Pick a repo]* *[loopzero init]*

### 5.3 Acceptance

Every `<EmptyState>` instance MUST pass:
- 4 elements present (icon + title + description + CTA).
- Title is positive framing (e.g., "Deploy your first app" not "No deployments").
- Description has why + how + what-next.
- CTA primary is in-app (not docs link).

---

## 6. Errors as diagnostics

### 6.1 Required 6-element error structure

Every error message (form errors, deploy failures, API errors, system errors) MUST contain:

| # | Element | Example |
|---|---|---|
| 1 | **Title** | "Build failed during dependency install" |
| 2 | **Cause** | "pnpm could not resolve `react@19` peer dependency for `loopzero-app`" |
| 3 | **Impact** | "No new release was deployed. Production still serves release `r_abc123`." |
| 4 | **Action** | "Update `package.json` peer deps or pin pnpm version in `loopzero.yaml`." |
| 5 | **Primary CTA** | `[View logs]` |
| 6 | **Secondary** | `[Copy diagnostic bundle]` `[Retry]` |

### 6.2 Implementation

Use `<ErrorPanel>` (new composite per remediation plan T3.5) OR `<Alert intent="destructive">` with structured `title` + `description` + `action` props.

For form-level errors: associate with field via `aria-describedby`.

### 6.3 Anti-patterns (REJECT in code review)

- "Something went wrong."
- "An error occurred."
- "Internal server error."
- "Oops!"
- Generic toast `<Toast intent="destructive" title={err.message}>` without context.

### 6.4 Error code surface

If backend provides an error code (e.g., `BUILD_LOCK_TIMEOUT`), surface it in a copyable code block. Useful for support tickets + LLM debug assistant (T3.5).

---

## 7. Logs/timelines as product, not dumps

`<LogViewer>` (planned in remediation plan T1.4) MUST satisfy:

### 7.1 Functional

- [ ] Search box with regex toggle.
- [ ] Severity filter (error / warn / info / debug) — multi-select chips.
- [ ] Jump-to-error button (scrolls to first ERROR-severity line).
- [ ] Timestamp per line (relative + absolute on hover).
- [ ] Step grouping (logs grouped by Argo Workflow step).
- [ ] Highlighted current step in companion BuildTimeline.
- [ ] Copy line (single click).
- [ ] Copy diagnostic bundle (entire log + last 5 builds + loopzero.yaml + redacted secrets).
- [ ] Follow-tail toggle (auto-scroll while live).
- [ ] Pause on user scroll-up (don't fight the user).
- [ ] Share-link with line anchor (`?cursor=foo&line=42`).

### 7.2 Layout

```
┌─────────────────────────────────────────────────┐
│ BuildTimeline ─ steps with duration + status     │  ← Summary
├─────────────────────────────────────────────────┤
│ [Search] [Severity ▾] [Jump to error] [⬇ Follow] │  ← Toolbar
├─────────────────────────────────────────────────┤
│ 14:32:01 ERROR  Step 3/7: install               │  ← Logs
│         pnpm ERR_PNPM_PEER_DEP_ISSUES           │
│ 14:32:01 ERROR  └─ react@19 conflict with...    │
│ ...                                              │
├─────────────────────────────────────────────────┤
│ [Copy diagnostic bundle] [Share]                 │  ← Footer
└─────────────────────────────────────────────────┘
```

### 7.3 Performance

- Virtualized rendering (`@tanstack/react-virtual`) for >1000 lines.
- Lines >10KB truncated with "Expand" button (`<details>`).
- ANSI escape codes stripped via `strip-ansi` (NEVER render as HTML).

---

## 8. First experience obsessively simple

Onboarding flow (remediation plan T2.10) MUST follow this sequence:

```
1. /onboarding/welcome      — "Let's deploy your first app. ~4 minutes."
2. /onboarding/github       — Detect existing OIDC connection; skip if connected.
3. /onboarding/repo         — Repo picker (last 20 commits, search, framework auto-detect badge).
4. /onboarding/configure    — Pre-filled config; "Advanced" toggle reveals env vars/build cmd.
5. /onboarding/deploy       — Preview of loopzero.yaml + [Deploy] button.
6. /onboarding/streaming    — Live BuildTimeline + LogViewer in companion mode.
7. /onboarding/success      — Live URL + 3 next-steps cards: [Add domain] [Create preview env] [Invite team]
```

### 8.1 Success criteria

- Time-to-first-deploy ≤ 5 minutes (measured via web-vitals custom event).
- Drop-off per step measured (analytics).
- "Did this work?" survey after step 7.

### 8.2 No documentation dependency

A good dev should complete first deploy **without reading external docs**. If a step needs external docs, the step is incomplete.

---

## 9. Developer language

The dashboard talks to developers. Be precise, not aspirational.

### 9.1 Banned terms in dashboard UI

| Term | Reason |
|---|---|
| "Something went wrong" | Useless. Be specific. |
| "Oops" / "Ouch" | Infantil. |
| "Magic" | Marketing fluff. |
| "Seamless" | Marketing fluff. |
| "Blazing fast" | Marketing fluff + unprovable. |
| "Powerful" | Marketing fluff. |
| "Robust" | Marketing fluff. |
| "Next-generation" | Marketing fluff. |
| "AI-powered" (without context) | Buzzword. |
| "In 5 minutes" / "In under N seconds" | Marketing claim violates voice rules. |
| Emoji in label/button/error | Anti-pattern (Violet Forge rule). |

### 9.2 Preferred phrasing

| Avoid | Prefer |
|---|---|
| "Saving..." | "Saving project settings..." |
| "Deploy succeeded" | "Production now serves release r_abc123 (commit 7f2a8b9)" |
| "Error" | "Build failed during install" |
| "Loading..." | "Fetching deployments..." |
| "Welcome!" | "LoopZero dashboard" |
| "Get started" (vague) | "Connect GitHub" (specific) |
| "Click here" | "Open project settings" (verb + object) |

### 9.3 Internal terms forbidden in user-facing UI (Rule 3 CLAUDE.md)

`ArgoCD`, `argocd`, `GitOps`, `gitops-push`, `CRD`, `tenant_id`, `advisory lock`, `pg_advisory`, `Gitea`, `MinIO`, `kubebuilder`, `envtest`, `Kaniko`, `BuildKit`, `cosign`, `Helmfile`, `Pulumi`, `Tilt`, `Cell`, `DSS § X.Y` (internal ref).

Use product language: `deploy`, `team`, `account`, `service`, `revert`, `build`, `production`, `release`, `environment`.

---

## 10. Tables for decision

`<DataTable>` for operational lists. Columns are decisions, not data dumps.

### 10.1 Canonical columns per surface

**Deployments:**

| # | Column | Sort | Mobile |
|---|---|---|---|
| 1 | Status (StatusDot + label) | ✓ | Visible |
| 2 | Release (short SHA + tag) | — | Visible |
| 3 | Environment (badge) | ✓ | Visible |
| 4 | Commit (msg + author) | — | Hidden md- |
| 5 | Author | ✓ | Hidden md- |
| 6 | Started | ✓ default desc | Hidden sm- |
| 7 | Duration | ✓ | Hidden md- |
| 8 | Traffic/canary (e.g., "20% canary") | — | Visible |
| 9 | Actions (View / Rollback / Promote / Rerun) | — | Visible |

**Projects:**

| # | Column |
|---|---|
| 1 | Project (name) |
| 2 | Framework (badge) |
| 3 | Apps (count) |
| 4 | Environment (latest active) |
| 5 | Status (live/building/failed — real, not hardcoded) |
| 6 | Last deploy (relative) |
| 7 | Actions (Open + DropdownMenu) |

**Secrets:**

| # | Column |
|---|---|
| 1 | Key |
| 2 | Value (masked, click to reveal) |
| 3 | Environment scope (multi) |
| 4 | ESO sync status (StatusDot) |
| 5 | Last updated |
| 6 | Actions (Edit / Rotate / Delete) |

**Audit log:**

| # | Column |
|---|---|
| 1 | Timestamp |
| 2 | Actor (email + IP) |
| 3 | Action (verb + resource) |
| 4 | Resource (link) |
| 5 | Result (success/failure StatusDot) |

### 10.2 Table requirements (universal)

- [ ] Empty state with CTA.
- [ ] Loading state with skeleton rows (NOT spinner).
- [ ] Error state with retry.
- [ ] Search box (when N>10 typical).
- [ ] Sortable columns where useful.
- [ ] Filter chips (status, env, etc.) when N>20.
- [ ] Bulk select if bulk action exists.
- [ ] Pagination at N>50.
- [ ] Mobile: low-priority columns hidden via `className="hidden md:table-cell"`.
- [ ] Don't add column just because backend returns the field.

---

## 11. Microinteractions confirm intention

Movement explains state, hierarchy, or continuity. Never decoration.

### 11.1 Required microinteractions per component

| Component | States required |
|---|---|
| `Button` | default · hover · `:active` · `:focus-visible` · disabled · loading (Loader2 inline) |
| `Input` | default · hover · focus · error · disabled · loading (subtle right-side spinner) |
| `Select` | default · hover · open · disabled · selected |
| `Dialog` | enter (200ms scale-up + fade) · exit (150ms fade) |
| `Toast` | enter (slide-in from top-right) · auto-dismiss (4s default) · manual dismiss |
| `Tabs` | active · hover · disabled · keyboard focus indicator |
| `CopyButton` | default · hover · success (✓ icon + "Copied" label for 1.5s) |
| `Save action` | idle · saving · saved (✓ for 2s) · error (red Alert) |
| `Deploy button` | idle · deploying (Loader2 + "Deploying...") · success (✓ + URL link) · failed (Alert) |
| `Loader2` | spinning at 1 turn/sec via `animate-spin` |
| `Skeleton` | pulse animation (1.5s cycle) representing real layout |

### 11.2 Reduced motion respected

`prefers-reduced-motion: reduce` MUST disable all non-essential animations. Already partially done in `cloud/dashboard/src/index.css:122-136`. Audit per Phase 4 T4.1.

### 11.3 Speed budget

| Interaction | Budget |
|---|---|
| Tab switch | <100ms perceived |
| Modal open | <200ms |
| Hover feedback | <50ms |
| Click feedback | <50ms |
| Page navigation (cached) | <100ms |
| Page navigation (uncached) | <500ms |
| Deploy submit feedback | <200ms (acknowledge then stream) |

---

## 12. Accessibility is production requirement (not polish)

### 12.1 Hard floor (WCAG AA)

- [ ] Contrast text-foreground ≥ 4.5:1 / text-muted-foreground ≥ 3:1.
- [ ] `:focus-visible` ring on every interactive element.
- [ ] Touch targets ≥ 24×24 CSS px.
- [ ] Tab order coherent.
- [ ] Escape closes modals/dropdowns.
- [ ] Skip-to-content link in `<DashboardLayout>` (T4.1).
- [ ] `<h1>` semantic per page (NOT `<span>` like current `top-bar.tsx:67`).
- [ ] Form fields have `<label>` OR `aria-label`.
- [ ] Errors associated via `aria-describedby`.
- [ ] Live regions for SSE/streaming updates (`aria-live="polite"`).
- [ ] State not communicated by color alone (use icon + label too).
- [ ] Lighthouse a11y ≥95.
- [ ] axe-core zero critical/serious violations.

### 12.2 Keyboard-only acceptance

> **If it doesn't work well on keyboard alone, it's not done.**

Manual test every flow with keyboard:
- Tab through every interactive element in logical order.
- Activate every button with Enter/Space.
- Open every dialog with Enter.
- Close every dialog with Escape.
- Navigate every menu with arrow keys.

---

## 13. Responsiveness is reorganization, not compression

### 13.1 Breakpoints

| Token | Width | Strategy |
|---|---:|---|
| `sm` | 640px | Hamburger menu activates below this |
| `md` | 768px | DataTable hides low-priority columns |
| `lg` | 1024px | 2-col grids unfold |
| `xl` | 1280px | 3-col grids unfold |
| `2xl` | 1536px | `<main>` max-width activates |

### 13.2 Mandatory tests

All pages must render correctly at: **320 / 375 / 414 / 768 / 1024 / 1280 / 1920 / 3440** px.

Plus zoom: WCAG 1.4.10 requires reflow at 320px wide OR 200% zoom.

### 13.3 Hard rules

- No horizontal scroll at any breakpoint.
- No two-line clickable text on buttons / CTAs (truncate or wrap container).
- DataTable low-priority columns hidden below `md`.
- Dialog full-screen below `sm`.
- Sidebar collapses to hamburger below `md`.
- `<main>` has `max-w-screen-2xl mx-auto` to prevent infinite stretch.
- Nothing depends on `hover` (touch devices have no hover).

---

## 14. Perceived performance

Even slow backends shouldn't feel laggy.

### 14.1 Strategy by operation type

| Operation | Strategy |
|---|---|
| Read (list, detail) | Skeleton mirroring real layout while loading |
| Write (form submit) | Optimistic update + revert on failure |
| Long write (deploy, build) | Stream progress via SSE; show step-by-step state |
| Background sync (revalidate on focus) | Silent; only show indicator if data changed |
| Search/filter | Debounced (250ms) client-side filter if data already in memory |

### 14.2 Skeleton requirements

Skeletons MUST mirror real layout dimensions. NOT generic gray boxes.

Pattern:
- `<Card><Card.Body><Skeleton className="h-4 w-32" /></Card.Body></Card>` (matches text dimension)
- `<Skeleton className="h-12 w-12 rounded-full" />` (avatar)

### 14.3 Progressive feedback

For multi-step operations:

**Bad:** `Loading...`

**Good:**
```
Creating build environment... (1 of 5)
Installing dependencies... (2 of 5)
Building api... (3 of 5)
Pushing image... (4 of 5)
Deploying to production... (5 of 5)
```

---

## 15. The dashboard has opinions

A FAANG-level product **guides** the user. Doesn't dump options.

### 15.1 Defaults are choices

Every form has sensible defaults. The user only changes what they want to change.

**Scaling:** start with preset per tier:
- Starter: 2 replicas, HPA disabled.
- Pro: 5 replicas, HPA 2-5.
- Team: 10 replicas, HPA 2-10.

Advanced (custom HPA bounds, anti-affinity, etc.) lives behind "Advanced" toggle.

### 15.2 Progressive disclosure

Settings are 80% defaults + 20% custom. Defaults visible; custom hidden behind `<Disclosure>` / "Show advanced" toggle.

### 15.3 Recommendations explicit

When trade-offs exist, show recommendation + brief explanation:

```
Build cache: ☑ Enabled (recommended — 3-5x faster rebuilds)
            ☐ Disabled (use only if cache corruption suspected)
```

### 15.4 Default to safe

Destructive defaults forbidden. Examples:
- "Auto-delete preview environments" defaults OFF.
- "Auto-promote staging to prod on green" defaults OFF.
- "Allow unsigned images" defaults OFF.

### 15.5 ADR record

Every opinionated default that diverges from "lowest common denominator" gets a brief ADR comment in code:

```tsx
// ADR: HPA defaults to enabled for Pro tier
// because >80% of Pro customers run >1 replica (data 2026-Q1).
// Override available via "Advanced" toggle.
const hpaEnabled = tier === 'pro' || tier === 'team';
```

---

## 16. Danger zone UX

### 16.1 Visual separation

- `<DangerZone>` composite (red-border `Card`) at bottom of page.
- "Danger zone" heading visible.
- Each destructive action in its own row inside.

### 16.2 Action choreography (revisitando §2.2)

Required sequence:

```
[DangerZone]
  ├─ Action title: "Delete <Resource>"
  ├─ Description: <Impact + reversibility statement>
  └─ Button (variant="destructive", size="sm")
        ↓ click
  ┌─ ConfirmDialog open
  │   ├─ Title: "Delete <Resource>?"
  │   ├─ Description: <Affected resources + impact>
  │   ├─ Confirmation phrase: "delete <slug>" (typed verbatim)
  │   ├─ Cancel (variant="ghost")
  │   └─ Submit (variant="destructive", disabled until phrase typed)
  └─ Backend call (NEVER setTimeout fake — see plan T0.2)
        ↓ success
  Navigate to safe context + show Toaster success
```

### 16.3 Anti-patterns (REJECT in code review)

- `<Button variant="primary">Delete</Button>` — destructive must be `variant="destructive"`.
- Destructive in `<DropdownMenu>` without `<ConfirmDialog>`.
- `setTimeout(1500)` fake (the exact smoking gun in `settings.tsx:13-21` + `profile.tsx:30-40`).
- Confirmation just "Are you sure?" (require typed phrase for irreversible).

### 16.4 Reversibility statements

| Action | Required statement |
|---|---|
| Delete project | "Live URLs stop. Data archived 30 days then deleted permanently." |
| Delete team | "All projects, environments, domains, members archived 30 days then permanent." |
| Rollback | "Production will serve release `r_abc123`. Reversible." |
| Promote | "Staging release will replace production. Reversible via rollback." |
| Revoke session | "Other devices will be signed out. Reversible — they can sign in again." |
| Cancel subscription | "Plan downgrades to Free at end of period. No data loss. Reversible until period end." |

---

## 17. Required UX metrics

> **If we don't measure, we're opining.**

Instrumentation lives in `cloud/dashboard/src/lib/rum.ts` (Web Vitals already wired) + new `lib/ux-metrics.ts` (planned T4.7).

### 17.1 Time-based metrics

| Metric | Target | Captured via |
|---|---|---|
| Time-to-first-deploy | ≤ 5 min p50 | Web Vitals custom event `loopzero.onboarding.deploy.complete` |
| Time-to-understand-failure | ≤ 60s p50 | Time from deploy.failed event to logs viewer interaction |
| Clicks-to-rollback | ≤ 3 clicks p50 | Click sequence tracking |
| Clicks-to-add-secret | ≤ 3 clicks p50 | Same |
| Clicks-to-connect-domain | ≤ 4 clicks p50 | Same |

### 17.2 Engagement metrics

| Metric | Threshold |
|---|---|
| Onboarding abandonment | <20% per step |
| Form error rate | <5% per submit |
| Time on logs viewer during failure | >30s = good engagement |
| Tickets per flow (e.g., billing) | <1% of users |
| NPS or satisfaction post-deploy | ≥40 |

### 17.3 Anti-engagement (bug signals)

| Signal | Detection | Action |
|---|---|---|
| Rage clicks (≥3 same element <1s) | `pointer` events analyzer | File bug; usually broken button |
| Dead clicks (click on non-interactive) | `pointer` events on non-button/link | File bug; usually missing affordance |
| Excessive scroll (>2 viewports on action page) | Scroll tracking | Layout review |
| Tab switch >5x on form | Visibility API | Confusing field — needs helper text |

### 17.4 Performance metrics (Web Vitals — already wired in `rum.ts`)

| Metric | p75 target |
|---|---|
| LCP | ≤ 2.5s |
| INP | ≤ 200ms |
| CLS | ≤ 0.1 |

### 17.5 Reporting

Weekly automated report to operator. Pre-launch: per-task verification in remediation plan acceptance criteria.

---

## 18. Systematic visual review

Every screen task in the remediation plan passes a 12-item visual checklist before merge:

```
[ ] 1. Alignment consistent (icons aligned to baseline, columns aligned at gridline).
[ ] 2. Spacing consistent (no random 5px / 7px / 13px gaps; only tokens).
[ ] 3. Hierarchy clear (one dominant element; secondary supports).
[ ] 4. Font sizes correct (matches scale: text-label / body-sm / body / title-sm / display-md).
[ ] 5. Buttons with correct weight (primary scarce; ghost/secondary majority).
[ ] 6. Cards without excessive border/shadow (1 border + subtle shadow max).
[ ] 7. Primary color used with restraint (one primary action per surface zone).
[ ] 8. Semantic states consistent (success=green, warning=yellow, destructive=red — never mixed).
[ ] 9. Icons necessary (every icon has reason; decorative removed).
[ ] 10. No "foreign component" (UI doesn't look like it came from another product).
[ ] 11. Not prototype-feeling (every state polished; not "TODO: design later").
[ ] 12. Density uniform (no random comfortable + spacious on same page).
```

> **If it looks like it came from another product, it's wrong.**

---

## 19. Definition of Done per screen

A screen is not done until ALL 18 states/checks pass:

```
1.  Normal state (data loaded, no errors).
2.  Empty state (no data; 4-element template from §5).
3.  Loading state (skeleton, NOT generic spinner).
4.  Error state (6-element template from §6).
5.  Permission denied state (clear message + alternate action).
6.  Disabled state (when applicable; visually distinct + tooltip).
7.  Mobile state (320px renders correctly).
8.  Extreme data state (1000 items in table; long strings; large numbers).
9.  Long names state (string truncation with tooltip on hover).
10. Many items state (pagination / virtualization activated).
11. High latency state (skeleton during slow backend).
12. Visual test passes (Storybook snapshot or Chromatic).
13. Accessibility test passes (axe-core zero critical/serious).
14. Copy reviewed (no marketing fluff, no internal refs, follows §9).
15. Components use tokens (zero hex/spacing/font literal).
16. Zero hardcoded values (status, plan, prices, IDs from backend).
17. Storybook entry with main variations.
18. Acceptance criteria from plan task validated.
```

These are non-negotiable. PR review checks all 18.

---

## 20. Closing — the bar

The product feels:

| Adjective | What it means |
|---|---|
| **Rápido** | <100ms interactions; <2.5s LCP. |
| **Calmo** | No flashy animations; no anxiety triggers; muted decoration. |
| **Técnico** | Linguagem de dev; CLI hint everywhere; copy CLI buttons. |
| **Confiável** | Status truthful; actions explicit; destructive guarded. |
| **Preciso** | No vague language; no hedging; no marketing speak. |
| **Operacional** | Every page does work; no dead pages. |
| **Premium** | Polish in every state; nothing feels prototype. |
| **Simples sem ser superficial** | Hides Kubernetes/Argo/Cloudflare but doesn't lie about what's happening. |

**Final test:** show the dashboard to a developer who has never used it. They should be able to:

1. Deploy their first app in <5 minutes without docs.
2. Understand why a build failed in <60s.
3. Roll back production in <3 clicks with confidence.
4. Understand their bill without going to Stripe portal.
5. Add a custom domain without filing a ticket.

If they can't, the dashboard isn't done.

---

## Cross-references

- **Audit AS-IS:** `.claude/knowledge-base/audits/2026-05-25/dashboard-faang-review.md` — current state vs this contract.
- **Remediation plan:** `.claude/knowledge-base/plans/2026-05-25-dashboard-faang-remediation-plan.md` — how we get from current to contract.
- **Edge case review:** `.claude/knowledge-base/plans/2026-05-25-dashboard-faang-remediation-edge-cases.md` — what can go wrong in implementation.
- **Architecture:** `.claude/knowledge-base/architecture/dashboard/` — C4 docs (system-context / container / component / deep-dive).
- **Composite library:** `@loopzerokit/ui@0.12.0-next.0` — required canonical components.
- **Voice rules:** root `CLAUDE.md` § Voice and Tone (technical-direct for dashboard; aspirational reserved for PITCH.md).

---

**Locked. Changes require sponsor authorization + MEET record.**
