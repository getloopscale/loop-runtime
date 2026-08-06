---
type: sepa-iteration
title: "Iteration 3 (resume) — T2.3 MeteringStore wrapper (same-period)"
---

# Iteration 3 (resume) — T2.3 MeteringStore wrapper (same-period)

Date: 2026-07-01. Commits this turn: **b3b23573** (return-contract), **83ecd959** (wrapper same-period).

## Delivered (verified PG-REAL)
- `metering_wrapper.go`: `MeterInput` (domain, plain Go — DIP, no generated/pgtype leak), `MeteringStore{pool *pgxpool.Pool}` (ADR-10 dedicated), `NewMeteringStore`, `MeterCompute(ctx, MeterInput) (advanced bool, delta int64, err error)` — one RLS-bypass tx: `Begin` → `SET LOCAL app.tenant_id=''` → ADR-4 CTE → (ErrNoRows=held, commit no-op) → (delta>0) `UpsertUsageRecord` → `Commit`. `ignoreTxClosed` for the post-commit rollback no-op. `uuid.NewString()` for usage id.
- `metering_store_test.go`: pool-based harness (`meteringPool`, `seedMeteringFixtureCommitted`, `cleanupMeteringTenant` — t.Cleanup FK-ordered, isolated by unique cellID) + `TestMeteringStore_MeterCompute_SamePeriod_WritesCounterAndUsage` (counter=5, ONE usage row=5, cursor advanced). PASS.
- SEPA pre-COMMIT: PROCEED — tx atomicity, DIP, SET LOCAL, zero-delta honesty, test correctness, commit hygiene all clean.

## CARRY-FORWARD — non-negotiable before T2.3/T3.1 declared done (SEPA MAJOR + MINOR)

1. **[MAJOR] Wrapper-level HELD + zero-delta tests MISSING.** The only wrapper test is happy-path. The `ErrNoRows`→held branch (SC-1/SC-2/SC-6 all funnel here — the single most money-critical path: "held → NO bill") and the `delta==0` usage-skip branch are UNTESTED at the wrapper level. The query-layer HELD was PG-REAL validated, but the *wrapper's* HELD handling was not. **Next increment MUST add:** `TestMeteringStore_MeterCompute_Held_NoBill` (stale `expected` → advanced=false, delta=0, NO usage row, counter unchanged, cursor unchanged) + a zero-delta test (req or elapsed → cost 0 → advanced=true, delta=0, NO usage row, counter += 0).

2. **[MINOR] T2.3 DoD stays OPEN.** `MeterComputeSplit` wrapper method (2 usage records: tail→P1 {P1Start,P1End}, head→P2 {P2Start,P2End}; SEPA split-pairing MAJOR — assert each row's period_start; zero-cost skip per tail/head) + the ~12 SC/edge tests pending: ExactlyOnce (`-race`), FencedWriterRejected (SC-6), NegativeDeltaClockSkew_NoDecrement (SC-1, cursor UNCHANGED), NullCursorBootstrapsZero, ClampsLongGap (SC-7), **MultiMonthGap_NoMiddlePeriodMisattribution (SC-7 distinction — seed expected 2+ months before now so period_end(expected) != period_of(now()))**, **WithoutRLSBypass_WritesZeroRows (MUST use genuine loopzero_worker DSN `LOOPZERO_E2E_WORKER_DATABASE_URL`, fail-loud if unset — NOT owner, NOT SET ROLE — CRITICAL #1)**, UsageRecordSameTx (crash→rollback), UsageRecords_OneRowPerPeriod_SplitWritesTwo, DispatchClockDisagreement_HoldsNotMisbills (SC-2), NewPeriodRowCreatedByUpsert, UnknownPlanDefaultsStarter, ContinuesNotResets (SC-4). Generated `MeterComputeSplit`/`MeterComputeSplitRow` is a temporary orphan until the split wrapper lands.

Then: `SeedMeteringCursor`/`GetPlanTierForTenant`/`GetCellForTenant` wrappers + T1.3/T3.1 (worker, the pillar-(a) caller) + T3.2 (controller) + T6.1 (LIVE ≥2-cell — OPERATOR HARD STOP). SC-7 guardian re-sign + `/plan-confidence` v1.10 before final.

## Iter-4 first RED (SEPA-recommended)
`TestMeteringStore_MeterCompute_Held_NoBill` — stale `expected` (cursor already advanced) → the wrapper returns `advanced=false, delta=0`, writes NO usage row, leaves counter + cursor unchanged. This closes the MAJOR (money-critical held branch untested). Then the split wrapper.

No completion promise — T2.3 far from done.
