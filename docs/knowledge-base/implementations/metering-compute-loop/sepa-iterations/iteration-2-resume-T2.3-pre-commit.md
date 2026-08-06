# SEPA pre-COMMIT (VERBOSE) — Iteration 2 (resume) / Task T2.3 return-contract redesign

Date: 2026-07-01. Commit: **b3b23573** `feat(metering): MeterCompute/Split return per-period cost + boundaries`.

## Verdict: PROCEED — no CRITICAL, no MAJOR.

Money invariants all hold:
- p2 keyed off `metering_period_end(@expected)` NOT `now()` (cost.sql:132 / generated:228). Correct per MEET-006.
- Split returns tail/head SEPARATELY (`TailCost`+`HeadCost` distinct) → wrapper can write TWO usage records.
- All boundaries from SQL fns; no Go date math introduced.
- SC-7 total-clamp `tail+head == LEAST(now-expected, max_delta)` verified; split test asserts `tail+head==10==metering_cost(500,max_delta)`.
- Diff scope: ONLY the two trailing SELECTs + comments changed; SC-1/2/4/6/7 CTE guards untouched (no regression surface).
- `a=defer` HONEST (no production caller; wrapper pending; query layer WIP since ff76ec5f). Commit format ✓, no `Co-Authored-By` ✓.

## IMPORTANT MINOR — carry to the wrapper sub-unit (do NOT mistake this green for full SC-7 coverage)

`TestMeterComputeSplit_ReturnsTwoPeriodCosts` seeds `expected = period_start(now())-10s`, `max_delta=20s`. In THIS seed `metering_period_end(expected) == metering_period_start(now()) == metering_period_of(now())` — they COINCIDE. So the assertion `P2Start == metering_period_end(expected)` is correct but does NOT DISTINGUISH from a buggy query returning `metering_period_of(now())`. The SQL is demonstrably correct, but the DISTINCTION (MEET-006 "head NOT keyed into now()'s period") is proven ONLY by the **pending** `TestMeterComputeSplit_MultiMonthGap_NoMiddlePeriodMisattribution` — which MUST seed `expected` **2+ months before now()** (so `metering_period_end(expected) != metering_period_of(now())`) and assert `head` lands in `period(expected)+1`, NOT `period_of(now())`, and no middle-period row opens. This test is in the plan T2.3 list + the implementation contract; it is the one that actually closes the multi-boundary-misattribution guard. This sub-unit proves the return-shape + two-period split, not that distinction.

## Increment outcome
Return-contract redesign (both writers → per-period cost + SQL boundaries) DONE + committed (b3b23573), PG-REAL. **Next sub-unit:** `cost_wrapper.go MeteringStore` (`*pgxpool.Pool` on `EffectiveWorkerDatabaseURL()`=loopzero_worker per CRITICAL #1; per method = `pool.Begin` → `SET LOCAL app.tenant_id=''` → CTE → 1 usage upsert (same-period) OR 2 (split: tail→P1, head→P2) → commit; ErrNoRows=held) + the ~12 remaining PG-REAL tests INCLUDING `MultiMonthGap` (the SC-7 distinction) + `WithoutRLSBypass_WritesZeroRows` (MUST use loopzero_worker DSN, not owner — CRITICAL #1). T2.3 stays in-progress; no completion promise.
