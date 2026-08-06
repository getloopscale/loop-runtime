---
type: review
title: "Edge Case Review — J-25 Live URL Discoverability"
---

# Edge Case Review — J-25 Live URL Discoverability

Date: 2026-06-12
Plan: [`knowledge-base/plans/2026-06-12-dashboard-journey-j25-live-url-discoverability-plan.md`](../plans/2026-06-12-dashboard-journey-j25-live-url-discoverability-plan.md) v1.0
Tasks analyzed: 6 (T0.1, T1.1, T2.1, T3.1, T4.1, T5.1)
Edge cases found: **10** (MUST FIX: 3, SHOULD TEST: 4, DOCUMENT: 3)

## Boundary map

| Task | Inputs | Outputs | State |
|---|---|---|---|
| T0.1 | curl GET `/status?env=production`; JWT seed token | console JSON probe | none (read-only) |
| T1.1 | `status.url` (from `useProjectStatus(name)`) | `<a href>` + Copy button | none (render-only) |
| T2.1 | `useProjectStatus(name).active_release.build_id`, current `build.id`, `status.url` | conditional `<Button>` | none (render-only) |
| T3.1 | same as T2.1 per-row | conditional `<ExternalLink>` icon | none (render-only) |
| T4.1 | curl GET `/status` + curl HEAD `<live-url>` | bash exit code + stdout JSON | none (read-only) |
| T5.1 | sponsor browser interaction | sponsor verbatim approval | none (human gate) |

**Cross-cutting boundary**: `status.url` is computed server-side as `https://<project>-<env>.<baseDomain>` (`api/internal/routes/status.go:252`). The plan v1.0 example shows `https://teste.loopzero.example` — that is a **plan fabrication**; the real URL pattern is `https://teste-production.loopzero.example` (or `https://teste-production.loopzero.local` in standalone-multi). Affects T0.1 regex + T5.1 sponsor walkthrough expectation.

---

## MUST FIX

### EC-1: Plan example URL is wrong (fabricated citation)
- **Affected task:** T0.1, T1.1, T5.1
- **Family:** Format
- **Scenario:** Plan v1.0 mentions `https://teste.loopzero.example` as the live URL example, and the success criterion is "sponsor opens https://teste.loopzero.example". The actual canonical pattern emitted by `status.go:252` is `https://<project>-<env>.<baseDomain>`, e.g., `https://teste-production.loopzero.example`. Sponsor walkthrough will look for the wrong hostname.
- **Impact:** Sponsor LIVE walkthrough confusion + T0.1 regex passing for wrong reasons (`.+\.loopzero\.dev$` matches both shapes, so the bug would go silent until sponsor).
- **Suggested fix:** Replace every occurrence of `teste.loopzero.example` in the plan with `teste-production.loopzero.example`; add ADR note citing `status.go:252` as canonical source of URL pattern.

### EC-2: Shell allowlist entries missing (CI gate will BLOCK merge)
- **Affected task:** T4.1, Global DoD
- **Family:** Integration
- **Scenario:** T4.1 introduces `infra/tests/e2e/dev-public-live-url-discoverability_test.sh` and `infra/scripts/local/dogfood-live-url.sh`. Per `.claude/rules/shell-scripts-allowlist.md` D3 (plan 2026-05-22), CI `task quality:gates` runs `loopzero-ops quality no-new-bash` and BLOCKS the diff because new `.sh` files require explicit allowlist entries with LoC + Justificativa.
- **Impact:** Final integration validation FAILS at quality gates; halt-loop emits BLOCKED right before sponsor walkthrough.
- **Suggested fix:** Add T4.2 sub-task "Append both new scripts to `.claude/rules/shell-scripts-allowlist.md` under § Post-baseline additions with one-line justificativa each (KEEP-CI-WIRED for the hermetic, KEEP-OPERATOR for the LIVE)".

### EC-3: T0.1 has no halt condition when probe returns null URL
- **Affected task:** T0.1
- **Family:** State (flow control)
- **Scenario:** ADR D1 assumes `status.url` is populated by backend. If probe reveals `status.url == null` despite active release existing (= backend wiring is broken or ExposedRoute CR is missing), Phases 1-3 ship 3 dashboard pages reading a dead field. The plan does NOT specify what to do.
- **Impact:** Engineering ships 3 pages that perma-render "URL pending" badge against a real production deploy — dashboard looks broken without dashboard being broken.
- **Suggested fix:** Add to T0.1 acceptance: "If probe returns `url == null` with active_release populated, HALT plan execution and escalate as J-25.A backend wiring fix before resuming Phases 1-3."

---

## SHOULD TEST

### EC-4: Multi-env confusion — Overview always shows production URL
- **Affected task:** T1.1, T2.1, T3.1
- **Suggested test:** `test_overview_url_matches_default_production_only` — assert `useProjectStatus(name)` is called WITHOUT env arg (defaults to production per `use-project-status.ts:25`); document that staging deploys' Detail page will show the PRODUCTION URL via this hook, not the staging URL. If a future J-25.2 adds env-aware Overview, this test will fail-loud signaling the assumption flip.

### EC-5: T4.1 SC-B uses HEAD which can return 405 from apps that only handle GET
- **Affected task:** T4.1
- **Suggested test:** Replace `curl -sI <url>` with `curl -so /dev/null -w '%{http_code}' <url>` and assert HTTP code ∈ 200..399 (accept redirects; some apps 302 root → /index). Add scenario SC-B' for explicit 405-on-HEAD trap.

### EC-6: Loading flash from undefined → URL
- **Affected task:** T1.1
- **Suggested test:** `test_overview_renders_skeleton_while_status_loading` — mock `useProjectStatus` returning `{loading: true, status: null}`; assert Skeleton (not "URL pending" badge); on next render with populated status, badge or link appears. Prevents annoying flash where badge briefly says "pending" before flipping to URL.

### EC-7: Copy button has no TDD spec but is in acceptance criteria
- **Affected task:** T1.1
- **Suggested test:** `test_overview_copy_button_writes_to_clipboard_and_shows_toast` — fire click on Copy button, mock `navigator.clipboard.writeText`, assert called with `status.url`, assert toast "Copied" rendered. EC-6 of plan mentions Safari fallback but the TDD section ignores Copy entirely.

---

## DOCUMENT

### EC-8: Rolling window between deploy.completed and `useProjectStatus` refetch
- **Affected task:** T1.1, T2.1, T3.1
- **Accepted risk:** `useProjectStatus` polls every 5s; after a fresh `deploy.completed` SSE event, dashboard may render the OLD URL for up to 5s. Same pattern already in J-17 lessons (#3 useEffect deps reflecting lifecycle transitions). Acceptable since rollback/promote already gets the same treatment; no action needed.

### EC-9: Paginated Deployments list — affordance gap
- **Affected task:** T3.1
- **Accepted risk:** If `active_release.build_id` is on page 1 but user scrolls to page 2, the ExternalLink icon is never visible. Plan deliberately scopes the icon per-row; pagination-level "current production" indicator is out of scope. User can sort/filter back to page 1. Document as known affordance gap, not a bug.

### EC-10: dev/standalone/prod hostname variation breaks T0.1 hardcoded regex
- **Affected task:** T0.1
- **Accepted risk:** T0.1 regex `^https://.+\.loopzero\.dev$` works for dev-public + prod but FAILS against standalone-multi (`https://teste-production.loopzero.local`). The plan deliberately scopes the LIVE probe to dev-public. Document explicitly in T0.1 procedure: "Probe ONLY runs against dev-public OR production; standalone-multi LIVE probe is out of scope (use hermetic vitest only)".

---

## Summary

| Task | Edges found | MUST FIX | SHOULD TEST | DOCUMENT |
|---|---|---|---|---|
| T0.1 | 3 | 2 (EC-1, EC-3) | 0 | 1 (EC-10) |
| T1.1 | 4 | 1 (EC-1) | 2 (EC-6, EC-7) | 1 (EC-8 shared) |
| T2.1 | 2 | 0 | 1 (EC-4 shared) | 1 (EC-8 shared) |
| T3.1 | 3 | 0 | 1 (EC-4 shared) | 2 (EC-8, EC-9) |
| T4.1 | 2 | 1 (EC-2) | 1 (EC-5) | 0 |
| T5.1 | 1 | 1 (EC-1 shared) | 0 | 0 |

(EC counts above are the *first-attribution* of each EC; shared ones counted once per task but the EC itself listed only once in the body.)

**Verdict:** PLAN NEEDS ADJUSTMENT

The 3 MUST FIX items are surgical (1 plan-text fix + 1 new sub-task + 1 acceptance criterion addition). After absorption, plan jumps to PLAN OK with minor SHOULD TEST + DOCUMENT items to fold into Phase 1+4 task descriptions.

## Absorption guidance (for the user / cycle-plan wrapper)

To advance to `/plan-confidence`:

1. **EC-1**: Replace 4 mentions of `teste.loopzero.example` in the plan with `teste-production.loopzero.example`; add a 2-line ADR note under D1 citing `api/internal/routes/status.go:252` as canonical URL emitter.
2. **EC-2**: Add T4.2 to Phase 4 ("Allowlist entries"); update Global DoD to include "allowlist 2 new shell scripts under `.claude/rules/shell-scripts-allowlist.md` § Post-baseline additions".
3. **EC-3**: Add to T0.1 acceptance criteria: "Halt + escalate as J-25.A if `status.url == null` despite `active_release` populated".
4. **EC-4 / EC-5 / EC-6 / EC-7**: Add the 4 new TDD specs to existing task TDD sections (no new tasks needed).
5. **EC-8 / EC-9 / EC-10**: Add explicit notes to T0.1 / T3.1 / Drawbacks & Risks sections.

Result: plan v1.0 → v1.1 with +1 sub-task (T4.2 allowlist), 4 new tests, 3 documentation notes, 3 plan-text fixes. Estimated absorption time: 20-30 minutes.
