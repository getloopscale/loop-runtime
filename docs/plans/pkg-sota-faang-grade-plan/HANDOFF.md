# pkg-sota-faang-grade-plan — Handoff Document

**Date:** 2026-05-04
**Plan version:** v1.4 (post Ralph loop iter 9 closure)
**Total commits:** 30 across iter 1-9
**Status:** IMPLEMENTATION COMPLETE PENDING DOGFOOD GATE
**Next milestone:** First GREEN run of `pkg-soak.yml` weekly cron → plan transitions to v2.0

## Executive summary

This plan took the `pkg/` shared library layer from SOTA score 7.0/10
to ~9.0/10 across 30 commits. Every actionable item in Phases 0-2 +
Phase 3 T3.1 + Phase 3 T3.2 prep is canonically complete. The only
gate that remains is empirical dogfood validation — Friday 02:00 UTC
weekly CI cron `pkg-soak.yml` first GREEN run, which requires:
- `KUBECONFIG_DEV` GitHub Actions secret (operator action)
- `SOAK_AUTH_TOKEN` GitHub Actions secret (operator action)
- 4-hour wall-clock soak duration

Per CONDITION-5 (sre-engineer absolute veto on production readiness)
and the operators-sota-faang-grade-plan precedent, dogfood is
non-skippable but is a wall-clock-bound CI gate, not a Ralph loop
iter activity.

## Phases & Tasks status

| Phase | Status | Tasks |
|---|---|---|
| 0 — Governance | ✅ DONE | T0.1 — MEET-2026-05-04-PKG-SOTA-ASYNC-AMENDMENT + 8 ADRs (0028-0035) + amendment A1 |
| 1 — Quick Wins | ✅ 7/7 DONE | T1.1 (delete pkg/health) + T1.2 (JWT RFC 8725bis) + T1.3 (id validators) + T1.4 (JWKS via CB) + T1.5 (typed labels) + T1.6 (errors stack) + T1.7 (usage matrix gate G14) |
| 2 — Modernization | ✅ 7/7 DONE | T2.1 (CB generics+ctx) + T2.2 (pkg/conditions, **5/5 acceptance criteria**) + T2.3 (errors HTTP split, **all stages A+B+C BREAKING + emit migration**) + T2.4 (LimitsStore DIP) + T2.5 (ContextLogger) + T2.6 (property tests) + T2.7 (DOT viz) |
| 3 — Validation T3.1 | ✅ DONE | validate-matrix sweep + 2 new pkg gates (G14, T2.7) |
| 3 — Validation T3.2 | ⏳ infra-encoded | pkg-soak.yml weekly cron + pkg-soak-smoke.sh local-equivalent + smoke evidence committed |

## Adoption Follow-ups status

| # | Item | Status |
|---|---|---|
| 1 | pkg/conditions in 8 controllers | ✅ FULLY CLOSED — tenant + environment iter 5; application + exposure + preview + platform-bootstrap + git-repository-binding + cluster-credential iter 6 |
| 2 | CB Execute[T] in 4 operator consumers | ✅ FULLY CLOSED — argocd iter 2; gitea + harbor + minio iter 4 |
| 3 | ContextLogger migration | ✅ NO-OP CONFIRMED — RegisterTraceExtractor unused in production (only self-tests reference it) |
| 4 | WrapWithStack adoption | OPEN as opt-in opportunism (not tracked as debt) |

## ADRs ratified (async per ADR-0022)

All status: `Accepted (async, pending human ratification)`. Ratification
should happen alongside or before the first pkg-soak.yml GREEN cron run.

- **ADR-0028** Delete pkg/health (dead code)
- **ADR-0029** JWT RFC 8725bis hardening
- **ADR-0030** Decouple LoopZeroError from HTTP transport (RFC 9457)
- **ADR-0031** Circuitbreaker generics + context (Breaker[T])
  - Amendment A1 (MEET in-loop): adopted package-level `Execute[T any]`
    instead of `Breaker[T any]` type — preserves 100% backward compat
- **ADR-0032** Introduce pkg/conditions kstatus-compatible
- **ADR-0033** pkg/enforcement DIP via LimitsStore
- **ADR-0034** pkg/logging ContextLogger (no global state)
- **ADR-0035** Property-based tests via testing/quick

## Governance Conditions binding (5/5 status)

| # | Source | Status | Evidence |
|---|---|---|---|
| 1 | sre-engineer: deprecation runbooks for D4+D7 | ✅ closed | CHANGELOG migration notes; 1-release window in code |
| 2 | api-engineer: T2.3 staged A/B/C rollout | ✅ closed | Stage A (introduce) + B (helpers) + C (BREAKING field removal) + emit migration all shipped; iter 9 grep `ToJSON()` in production code returns ZERO |
| 3 | verification-architect: property tests in CI default | ✅ closed | T2.6 ran quick.Check 1000-10000 iter; included in `task test:unit` default |
| 4 | platform-security-engineer: T1.2 max defer iter 2 | ✅ closed | T1.2 shipped iter 1 |
| 5 | sre-engineer: T3.2 dogfood non-skippable | ⏳ deferred | infrastructure-encoded as pkg-soak.yml; first GREEN cron run pending |

## Real Usage Matrix (post-iter-9 grep)

(see `docs/technical/pkg/architecture.md` for the auto-regenerated table)

Notable changes from baseline:
- `pkg/health` REMOVED (was 0 imports, deleted iter 1 per ADR-0028)
- `pkg/conditions` NEW — 9 importers (8 controllers + adapter)
- `pkg/errors/http` NEW — 6 importers (api/internal/routes + sse + services + tests)
- `pkg/circuitbreaker` 5 → 7 importers (4 operator consumers + jwt + 2 api services)
- `pkg/errors` 70 → 70 (codes refactored; HTTPStatus field removed)

## Test sweep evidence (final)

```
cd pkg && go test -race ./...
ok  github.com/loopzero/loopzero/pkg/circuitbreaker    1.0s
ok  github.com/loopzero/loopzero/pkg/conditions        1.0s
ok  github.com/loopzero/loopzero/pkg/enforcement       1.2s
ok  github.com/loopzero/loopzero/pkg/errors            1.0s
ok  github.com/loopzero/loopzero/pkg/errors/http       1.0s
ok  github.com/loopzero/loopzero/pkg/id                1.1s
ok  github.com/loopzero/loopzero/pkg/jwt               4.5s
ok  github.com/loopzero/loopzero/pkg/logging           1.0s
ok  github.com/loopzero/loopzero/pkg/naming            2.9s
ok  github.com/loopzero/loopzero/pkg/statemachine      3.3s
```

10 packages, all GREEN with race detector, zero downstream regressions
in api+operators+cloud builds.

## Known pre-existing flakes (non-regression)

- `api/internal/k8s/TestTenantCRManager_*` — envtest race in tenant CR
  setup; passes when isolated, fails in 156-spec batch.
- `cli:unit-tests` `getPlatformStatus-charts.test.ts` — chart inventory
  pinning, no pkg/* import.
- `pulumi:vitest` — pre-existing.
- `infra:tests-go` — pre-existing.

None of these are pkg-sota regressions; verified by inspecting
`failing test → grep pkg/* import` returns empty.

## Pending work for production canary

When operator runs the weekly canary on production DOKS clusters:

1. **Provision GitHub Actions secrets:**
   ```bash
   gh secret set KUBECONFIG_DEV --body "$(cat ~/.kube/config-dev)"
   gh secret set SOAK_AUTH_TOKEN --body "<bootstrap token>"
   ```

2. **First Friday 02:00 UTC cron run:** `.github/workflows/pkg-soak.yml`
   triggers automatically. Job order: preflight → rfc9457_shape →
   cb_recovery → soak (4h) → report.

3. **On GREEN:** plan transitions to v2.0; HANDOFF.md updated with
   `Final Dogfood Gate evidence` section pointing to the cron run URL.

4. **On RED:** auto-issue created with investigation hints. Common
   classes:
   - RFC 9457 shape gap: registry coverage missing for new code →
     add to `pkg/errors/http/mapping.go statusByCode`.
   - CB recovery gap: JWKS fallback degraded → check `pkg/jwt`
     stale-cache path.
   - SLO breach: kstatus computation regressed → bisect on
     `operators/internal/conditions/pkgadapter.go`.

5. **Pre-cron local validation:** `task pkg:soak:smoke` reports the
   same 3 criteria as a fast feedback loop. Operators can run after
   `tilt up` rebuilds api to verify Stage C migration is live.

## Files modified summary

```
pkg/health/                        → DELETED (-733 LOC, 6 files)
pkg/conditions/                    → NEW (4 files .go + 4 _test.go, ~870 LOC)
pkg/errors/                        → field removed + sentinels cleaned
pkg/errors/http/                   → NEW (4 files, ~430 LOC)
pkg/errors/errors_test.go          → migrated 9 tests
pkg/id/id.go                       → 4 IsValid* validators added
pkg/jwt/validator.go               → RFC 8725bis hardening + JWKS via CB
pkg/circuitbreaker/circuitbreaker.go → Execute[T] generic added
pkg/logging/context.go             → ContextLogger added (legacy deprecated)
pkg/naming/naming.go               → StandardLabels typed struct added
pkg/statemachine/visualize.go      → DOT visualization added
pkg/enforcement/store.go           → NEW LimitsStore interface

operators/internal/conditions/pkgadapter.go    → NEW adapter
operators/internal/{tenant,app,platform,preview}/* → 8 controllers migrated to SetReadinessSummaryViaPkg
operators/internal/{argocd,gitea,harbor,minio}/circuitbreaker_*.go → 4 CB consumers migrated to Execute

api/internal/routes/projects.go    → writeLoopZeroError → errorshttp.WriteJSON
api/internal/sse/stream_handler.go → 4 sites migrated (iter 9)
api/internal/routes/{bootstrap,teams}.go → struct literals cleaned
api/internal/services/build_orchestrator_submit.go → ErrScalingAmbiguous read fixed

infra/scripts/pkg-usage-matrix.sh         → NEW (gate PKG-G14)
infra/scripts/gen-state-machine-dots.sh   → NEW (gate T2.7)
infra/scripts/pkg-soak-smoke.sh           → NEW (local-equivalent dogfood)
.github/workflows/pkg-soak.yml            → NEW (T3.2 CI gate)
infra/tests/parity/validate-matrix.sh     → 2 new gates (9.56 + 9.57)
taskfiles/test.yml                        → 4 new tasks
docs/technical/pkg/                       → upgraded SOTA documentation
docs/state-machines/*.dot                 → 11 NEW DOT files (per machine)
docs/meetings/MEET-2026-05-04-PKG-SOTA-ASYNC-AMENDMENT.md → NEW
docs/meetings/adr/0028-0035-*.md          → 8 NEW ADRs
docs/plans/pkg-sota-faang-grade-plan/T3.2-smoke-evidence.md → iter 6 evidence
docs/plans/pkg-sota-faang-grade-plan/HANDOFF.md           → THIS FILE
docs/plans/pkg-sota-faang-grade-plan.md   → v1.0 → v1.4 (4 amendments)
```

## References

- Plan: `docs/plans/pkg-sota-faang-grade-plan.md`
- Source research: `docs/technical/pkg/improvement-roadmap.md`
- MEET record: `docs/meetings/MEET-2026-05-04-PKG-SOTA-ASYNC-AMENDMENT.md`
- ADRs: `docs/meetings/adr/0028-0035-*.md`
- Smoke evidence: `T3.2-smoke-evidence.md` (sibling)
- Migration guide: `docs/technical/pkg/conditions.md`
- CI gate: `.github/workflows/pkg-soak.yml`
- Local equivalent: `infra/scripts/pkg-soak-smoke.sh`

## Final note

This plan is the second SOTA-research-driven plan to ship under the
Ralph loop pattern (operators-sota-faang-grade-plan being the first).
The pattern proved effective: every iter audited the previous,
catching real gaps (iter 8 caught Stage C emit gap; iter 9 caught
SSE residual ToJSON sites). 30 commits, zero rollbacks, 100%
incremental safety.

The only path to canonical closure is operator action on the CI gate.
Ralph loop iter cannot honestly emit `<promise>` until that gate is
GREEN. This is the correct steady state.
