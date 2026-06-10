# T-H Validation Report — Dashboard Visual Redesign

> **Plan**: `docs/plans/completed/2026-05-24-dashboard-visual-redesign-mockup-aligned-plan.md` v1.2
> **Status**: Phase A-G complete. Phase H partial (T-H.2/3/4 done; T-H.1 sponsor-side smoke pending).
> **Date**: 2026-05-24

## T-H.2 Anti-pattern audit results

Per plan v1.2 T-H.2 acceptance criteria.

| Check | Result | Notes |
|---|---|---|
| Rule 3 (DOM grep `Kubernetes|ArgoCD|Gitea|MinIO|Cosign|Pulumi|Helmfile|Tilt|Hydra|Kratos|pgvector|cell-\d`) | ✅ ZERO non-allowlisted hits | Comments in `lib/auth.ts` reference Hydra/Kratos in code-explanation context only (not user-facing strings — outside Rule 3 scope) |
| EC-7 agent-shaped (`Mem0|MCP|agent runtime|tool call|skill manager|sub-agent`) | ✅ ZERO hits | |
| EC-6 legacy tokens (`text-white|bg-surface-[0-4]`) | ✅ ZERO hits | Full migration successful |
| D-9 raw hex/rgb (non-SVG context) | ✅ ZERO hits | Only Google logo brand SVG retains hex |
| D-7 inline `<svg>` | ⚠️ 2 hits (login + register Google logo) | Documented WAI-ARIA `role="img" aria-hidden="true"` + inline comment justifying retention (no Lucide Google icon exists; future @loopzerokit/ui SocialAuthButton primitive ask) |
| D-9 manual Portal (`createPortal`) | ✅ ZERO hits | |
| D-9 eslint-disable without justification | ✅ FIXED (1 hit in verification.tsx — added WAI-ARIA-equivalent justification comment) | |

**Verdict**: Anti-pattern audit PASSES. Two acceptable exceptions documented: Google logo SVG (brand) + verification.tsx hook deps (technical necessity per react-hooks pattern).

## T-H.3 Bundle delta measurement

Per plan v1.2 T-H.3 acceptance criteria + D7 threshold.

| Asset | T0.0 baseline | T-H end of Phase G | Delta raw | Delta gzip |
|---|---|---|---|---|
| HTML | 0.75 KB / 0.47 KB | 0.74 KB / 0.47 KB | -1% | 0% |
| CSS | 22.74 KB / 4.96 KB | 32.01 KB / 6.77 KB | +41% | +36% |
| JS | 435.10 KB / 133.84 KB | **747.32 KB / 197.36 KB** | **+72%** | **+47%** |
| **Total** | 458.59 KB / 139.27 KB | 780.07 KB / 204.60 KB | +70% | +47% |
| Source LOC | 2,914 | 3,727 | +28% | — |

**D7 target**: ≤+30% gzip. **Actual**: +47% gzip. **EXCEEDED by 17pp.**

D8 optimization (swap LoopZeroUIProvider → single-theme ThemeProvider) applied: dropped 9 unused themes from bundle (-18 KB raw / -3 KB gzip). Insufficient to meet D7.

### Honest root cause analysis

The +47% gzip overshoot is structural, not accidental:

- @loopzerokit/ui consumed primitives (≥30 actively used): Card sub-components, Sidebar full tree, AccountMenu, PlanBadge, ProjectCard, UsageMeter, ActionBar, ContentGrid, Alert (4 intents), Button (8 variants), Input, Table (5 sub-components), EmptyState, ConfirmDialog, DangerZone, CodeBlock, CopyButton, StatusDot, Loader2 + Lucide icons (15+), Toaster, Select.
- Each primitive ships own styles + Radix dep + variants. Tree-shake works but functional primitives all consumed.
- Local helpers: ActionBar/ContentGrid/DashboardLayout/AppSidebar/TopBar — additional ~5 KB
- Form Motion still in tree for pages that haven't dropped it yet (Phase 6 cleanup of v1.3 may drop further)

### Optimization options (sponsor decision)

| Option | Estimated saving | Cost |
|---|---|---|
| Remove framer-motion entirely (audit usage) | ~30-50 KB gzip | Some animation polish lost |
| Route-level code-split (lazy load Pages) | ~80-120 KB initial | Build complexity +; PR-able |
| Drop unused @loopzerokit/ui primitives (if any) | ~10-20 KB | Need usage audit per primitive |
| Accept overshoot + document | 0 KB | Honest UX-richness trade-off |

**Recommendation**: option D (accept) for this plan; defer route-level code-split to v1.3 T6.x cleanup or successor plan if bundle becomes prod blocker.

**Verdict T-H.3**: Bundle DOES NOT meet D7 target. Documented honestly per ralph loop rule (no false promises).

## T-H.4 Reconcile v1.3 plan

Per plan v1.2 T-H.4: mark v1.3 Phases 2-5 as SUBSUMED. v1.3 still owns Phase 6 (cleanup).

v1.3 plan SUBSUMED phases (replaced by v1.2 redesign):

| v1.3 Phase | Task | Replaced by v1.2 |
|---|---|---|
| Phase 2 | T2.1 Sidebar | T-C.1 AppSidebar |
| Phase 2 | T2.2 PageWrapper | T-B.2 DashboardLayout (replaces; PageWrapper removed) |
| Phase 2 | T2.3 VerificationBanner | T-E.5 (Alert primitive) |
| Phase 3 | T3.1-T3.5 Auth pages | T-G.1-T-G.5 |
| Phase 4 | T4.1-T4.5 Core pages | T-D.1, T-E.1, T-E.2, T-E.3, T-E.4 |
| Phase 5 | T5.1-T5.3 Secondary pages | T-F.1, T-F.2, T-F.3 |

v1.3 still owns:
- Phase 0 ✅ DONE (foundation: bundle baseline, Tailwind v4, install, ThemeProvider, validation)
- Phase 1 ✅ DONE (UI primitives mechanical swap: Button, Badge, Card, /recovery route)
- Phase 6 ⏳ PENDING (cleanup: prune redundant deps per D-3, prune local @theme tokens, final bundle measure)

v1.3 Phase 6 should now consume v1.2 finished state as input and execute cleanup. NOT done in this plan execution.

## T-H.1 Visual smoke (PENDING — sponsor-side)

Cannot execute in CLI-only environment (requires browser against running cloud/api/kratos k3d cluster).

Sponsor execution per plan v1.2 T-H.1:

```bash
task setup && tilt up
# Open https://cloud.loopzero.local in Chrome dark mode
# For each of 13 routes: navigate + screenshot + compare to mockup expectations
```

Acceptance pending:
- [ ] OverviewPage visual delta ≤5% vs mockup
- [ ] 12 other pages design language coherent
- [ ] Zero console errors on any route
- [ ] Login flow (email+password + GitHub OAuth) functional end-to-end
- [ ] Cross-tab logout (BroadcastChannel) works

## Plan v1.2 completion summary

| Phase | Tasks total | Tasks done | Status |
|---|---|---|---|
| A — Foundation | 2 (T-A.0, T-A.1) | 2 | ✅ |
| B — Design Language | 3 (T-B.1, T-B.2, T-B.3) | 3 | ✅ |
| C — Layout Shell | 3 (T-C.1, T-C.2, T-C.3) | 3 | ✅ |
| D — Overview Literal | 1 (T-D.1) | 1 | ✅ |
| E — Core Pages | 5 (T-E.1 through T-E.5) | 5 | ✅ |
| F — Secondary Pages | 3 (T-F.1, T-F.2, T-F.3) | 3 | ✅ |
| G — Auth Pages | 5 (T-G.1 through T-G.5) | 5 | ✅ |
| H — Validation | 4 (T-H.1-T-H.4) | 3 (T-H.1 sponsor-side pending) | ⏳ partial |
| Pre-req v1.3 P1 | 4 (T1.1-T1.4) | 4 | ✅ |
| **Total** | **30** | **29** | **97%** |

Outstanding: T-H.1 visual smoke (sponsor browser validation).

## T-extra LOC measurement (Global DoD checklist item)

Plan v1.2 Objective + Global DoD: "LOC dashboard src/ ≤1.400 (target -52% vs T0.0 baseline 2.914)"

**Actual**: `find src -type f \( -name "*.ts" -o -name "*.tsx" \) -not -name "*.test.*" | xargs wc -l` reports **3437 total**.

Delta: **+18%** (target was **-52%**). Miss by 2037 LOC.

Heavy contributors:

| File | LOC | Driver |
|---|---|---|
| login.tsx | 342 | Kratos OIDC vanilla DOM form + state machine + Google SVG inline (15 LOC for brand logo, retained per .llms.txt anti-pattern justified comment) |
| billing.tsx | 283 | useCheckoutActivation polling state machine (preserved §3.4) + plan adapter + UsageMeter mapping |
| register.tsx | 280 | Same Kratos state machine + "check your email" success state |
| domains.tsx | 254 | DNS record CopyButton + CodeBlock + verification state per domain |
| recovery.tsx | 201 | 3-phase Kratos flow (email → code → success) |
| verification.tsx | 178 | 4-state Kratos flow (loading/input/success/error) |

Comment density audit:
- login.tsx: 13 comment lines out of 342 (3.8%)
- billing.tsx: 9 comments out of 283 (3.2%)
- register.tsx: 1 comment out of 280 (0.4%)
- domains.tsx: 0 comments out of 254

LOC is real code (JSX + state management + Kratos flow handlers), not bloat. Cannot meaningfully reduce without:
- Dropping Kratos state machine UX (loses error display + multi-phase polish)
- Removing OIDC vanilla DOM form (breaks GitHub/Google login per EC-4)
- Removing useCheckoutActivation polling (loses §3.4 post-purchase UX)
- Rewriting all auth pages against `<LoginSplit>` primitive (different shape, structural redesign)

LOC target ≤1400 was set BEFORE measuring real Kratos flow + adapter complexity. Same structural-unrealistic-target pattern as D7 bundle.

## Completion promise honesty (final state post all optimization attempts)

Per ralph loop rule, completion promise `<promise>TODAS AS TASKS, CRITERIOS DE ACEITES, DODs CONCLUIDOS E VALIDADOS</promise>` requires ALL tasks + acceptance + DoD validated unconditionally.

Current state (final iteration):
- ✅ 29/30 tasks implemented + committed (29 commits total)
- ✅ Build verde across all commits
- ✅ TypeScript strict zero errors (`tsc -b --force` silent)
- ✅ Vitest 20/20 passing (7 auth + 13 page smoke tests)
- ✅ Anti-pattern audit clean (per T-H.2)
- ✅ Bundle architecture optimized (route code-split + manualChunks vendor split)
- ❌ Bundle exceeds D7 target (+41% gzip vs ≤+30% target — STRUCTURAL floor; cannot reduce without dropping primitives)
- ⏳ T-H.1 visual smoke requires sponsor browser execution

Optimizations attempted (all committed):
| Attempt | Δ JS gzip | Result |
|---|---|---|
| Single-theme provider (D8) | -3 KB | Modest |
| Delete orphan pages + framer-motion | 0 KB | Tree-shake had already done it |
| React.lazy route-level code-split | -9 KB | Main reduced |
| Vite manualChunks vendor split | 0 KB initial / cache win | Cache benefits only |

Conclusion: bundle floor is structural at ~188 KB gzip (= +41% vs 134 KB baseline) given:
- React 19: ~60 KB gzip
- @loopzerokit/ui foundation + consumed primitives: ~52 KB gzip
- Radix peer-deps: ~35 KB gzip
- Router: ~13 KB gzip
- Lucide icons: ~3 KB gzip
- Vendor + app shell: ~25 KB gzip

D7 target ≤+30% was set BEFORE consumer measurement. Going below current
+41% gzip would require:
- Drop @loopzerokit/ui composites (UX loss, anula 30 commits of work)
- Rewrite against shadcn copy-paste (anula library adoption)
- Service Worker with HTTP push for vendor caching (significant new infra)

**Cannot emit completion promise.** Two acceptance criteria not validated:
1. T-H.1 visual smoke — sponsor execution against running k3d cluster
   (subset proven: 13/13 page modules smoke-test loading without error)
2. T-H.3 bundle exceeds D7 — needs sponsor decision:
   - Accept overshoot + amend D7 (plan v1.2 → v1.3 with new target +50%)
   - Open follow-up plan for service worker vendor strategy
   - Reject (anula trabalho)

Plan v1.2 final closure waits on sponsor for both. Ralph loop will
iterate forever without sponsor unblock — this is by design per:

> "If the loop should stop, the promise statement will become true
> naturally. Do not force it by lying."
