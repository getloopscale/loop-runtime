---
type: review
title: M12 Dashboard Validation (Playwright + axe-core, kill bundle-grep)
---

# Review — M12 Dashboard Validation (Playwright + axe-core, kill bundle-grep)

**Date:** 2026-07-07
**Milestone:** M12 — Dashboard Validation: Playwright + axe-core (kill bundle-grep)
**Plan:** `.claude/knowledge-base/plans/m12-dashboard-validation-plan.md` (status was DRAFT — stale; corrected this session)
**Cross-repo:** the Playwright/axe work lives in the sibling `loopzero-cloud` dashboard (extracted at M4/M5); the 24 bundle-grep `.sh` scripts to delete live in `loopzero/infra/tests/e2e/`.
**Verdict:** **READY_TO_MERGE** for the autonomous scope (DoD#2/#3/#4 + the hermetic half of DoD#1) — with caveats. DoD#5 DELETE + the dev-public LIVE tier = sponsor/LIVE halt boundary.

## Reconciliation (no competing artifact — Top-risk lesson)

The autonomous core was already delivered by a prior session and merged to `loopzero-cloud` develop (2026-06-28): `032836a` (a11y axe spec), `2fdefa2` (19 bundle-grep → real-DOM Playwright + visual baseline), `e7178ce` (chromium-linux baselines). The DRAFT plan (2026-07-05) is **stale-pessimistic** — it scoped only axe-core install + the 5-issue a11y spec; the delivery EXCEEDS it (19-test migration spec + visual baselines). No competing artifact opened; the plan status was corrected to reflect reality.

## DoD status

| # | DoD | Status | Evidence |
|---|---|---|---|
| 1 | Playwright headless in CI AND runnable vs dev-public (same suite) | **PARTIAL** | `dashboard/playwright.config.ts:42-50` hermetic webServer (CI auto-starts Vite `VITE_USE_MOCKS=true` :5174). Chromium GREEN; the dev-public LIVE tier is not wired (halt boundary). Cross-browser caveat below. |
| 2 | axe-core integrado; #71/#79/#80/#81/#83 by DECLARATIVE assertion | **DONE + GREEN** | `dashboard/e2e/a11y.spec.ts` — `@axe-core/playwright ^4.12.1`; `AxeBuilder().withRules(['heading-order'])` (#79) + DOM-attr assertions (#71 boundingBox, #81 progressbar aria-valuenow/max, #80 inputmode/one-time-code, #83 aria-invalid/describedby). **Re-verified this session: `CI=1 npx playwright test e2e/a11y.spec.ts --project=chromium` → 4 passed (32.8s).** |
| 3 | 100% bundle-grep scripts replaced by behavior/DOM assertion | **DONE (structural)** | 24/24 mapped: 19 in `dashboard-m12-fase-a-bundle-grep-migration.spec.ts` (explicit script→test mapping lines 21-41), 5 in `a11y.spec.ts`. |
| 4 | Visual regression baseline for critical screens | **DONE** | 3 committed PNGs (login/projects/billing) under `…-migration.spec.ts-snapshots/`, `toHaveScreenshot({maxDiffPixels:200})`. |
| 5 | DELETE bundle-grep + check-*-syntax + aggregators; allowlist shrinks | **SPONSOR/LIVE-gated (halt boundary)** | 24 `dev-public-dashboard-*.sh` + 24 companion `check-*-syntax_test.sh` still present in `loopzero/infra/tests/e2e/`. Deleting LIVE-regression protectors requires LIVE behavioral parity (Class B) per `cleanroom-up-live-test-mandate.md`. |

## Caveats (tracked; not merge-blocking for the autonomous scope)

- **CAVEAT-1 (cross-browser instability).** The suite is GREEN on **chromium** but FAILS on firefox/webkit/mobile-chrome (`net::ERR_CONNECTION_REFUSED` on /profile,/recovery; 30s timeout on /projects sheet). DoD#1 says "headless in CI" — today that means chromium-green, not the 4-project matrix the config declares. Either scope CI to chromium explicitly OR fix the cross-browser mock resolution before claiming the full matrix. Not an M12-logic defect (mock-harness/route issue), but the DoD#1 observable is only chromium-met.
- **CAVEAT-2 (best-effort assertions — vacuous smell).** Several migration tests use `.toBeGreaterThanOrEqual(0)` (`dashboard-m12-fase-a-bundle-grep-migration.spec.ts:117,151,173,197`), which passes trivially if the mock harness doesn't surface the scenario. Same class as M11's M3/M4. These are behavioral-shape documentation that CAN pass without exercising the real condition — down-scope or strengthen before the LIVE tier relies on them.
- **CAVEAT-3 (behavioral vs bundle-grep provenance).** The migration replaces bundle-grep (which tested the compiler/minifier) with real-DOM assertions against a **mocked** backend. This is a genuine improvement (tests behavior, not minification), but hermetic-mock coverage ≠ LIVE behavioral parity — the LIVE run (DoD#1 dev-public tier) is where true parity is proven.

## Positives (genuine)

- axe-core is a REAL declarative a11y gate (`AxeBuilder…analyze()` + `expect(violations).toEqual([])`), not a hand-rolled grep — exactly what the ROADMAP asked to replace bundle-grep for the a11y issues.
- The 24/24 script→test coverage mapping is explicit and auditable (migration spec lines 21-41 + a11y spec), so DoD#5 DELETE is unblocked ONCE LIVE parity is validated.
- Visual regression baseline is real (`toHaveScreenshot`), committed, chromium-linux.
- bundle-grep (the worst anti-pattern, D1) is genuinely killed for the migrated surface — the new tests assert DOM/behavior, immune to minification changes.

## Hard gates (BLOCKER-level) — all pass

- [x] Failing tests — a11y GREEN on chromium (re-verified). Cross-browser is CAVEAT-1, not a failing-on-target gate.
- [x] New secrets — NONE.
- [x] Direct commit to `main` — NO (all on `develop`, loopzero-cloud).
- [x] Co-Authored-By — NONE in M12 commits.

## Verdict

**READY_TO_MERGE for the autonomous M12 scope.** axe-core integration + 5 a11y issues by declarative assertion (DoD#2) is DONE + re-verified GREEN; the 19-test behavioral migration + 24/24 coverage mapping (DoD#3) and visual baseline (DoD#4) are merged; the hermetic Playwright half of DoD#1 is GREEN on chromium. bundle-grep is genuinely killed for the migrated surface.

**M12 checkbox does NOT flip.** Two items are sponsor/LIVE-gated per `cleanroom-up-live-test-mandate.md`: (a) the dev-public LIVE tier of DoD#1 (same suite against the live dashboard) + cross-browser stabilization; (b) DoD#5 DELETE of the 48 `.sh` scripts (needs LIVE behavioral parity — Class B). Closing M12 requires the operator+sponsor LIVE ceremony. Consistent with the ROADMAP reconciliation note (line 87 — M10–M13 remain `[ ]` intentionally).

**Recommended before Class B:** strengthen or down-scope the `.toBeGreaterThanOrEqual(0)` best-effort assertions (CAVEAT-2); decide the CI browser matrix (chromium-only vs fix cross-browser, CAVEAT-1).
