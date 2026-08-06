# SEPA pre-RED — Iteration 1 (resume) / Task T2.3 (return-contract redesign + wrapper)

Date: 2026-07-01. MODE=TIGHT. Consulted before touching money code (driver step 1). This is the resume /implement (query layer committed ff76ec5f; wrapper + ~14 tests pending).

## Two CRITICAL findings that MUST shape the T2.3 implementation

1. **[CRITICAL] RLS-bypass negative twin (`WithoutRLSBypass_WritesZeroRows`) is theatre if the pool connects as OWNER.** The owner DSN bypasses RLS unconditionally (FORCE RLS disabled in mig 000010; see mig 000023 header lines 13-26). A worker write run as owner WITHOUT `SET LOCAL app.tenant_id=''` still succeeds → the negative twin passes for the wrong reason. **The `MeteringStore` pool MUST connect as `loopzero_worker`** (config exposes `WorkerDatabaseURL` / `LOOPZERO_WORKER_DATABASE_URL`, per config_test.go:468). Tests MUST `t.Skip`-or-fail-loud when the DSN resolves to the owner. Without this, every SC-3/RLS money claim in the DoD is UNTESTED. Grep-guard: no new worker-write test may reuse `secretL19DBURL()` / owner `DATABASE_URL`.

2. **[CRITICAL] SC-7 split-two-usage-records is the invariant most likely gamed by a shallow test.** The RED test must assert BOTH usage rows land in the RIGHT periods: `usage(p1_start)==metering_cost(req,tail)` AND `usage(p2_start)==metering_cost(req,head)` where `p2_start == metering_period_end(expected)` (NOT `period_of(now())` — the exact bug MEET-006 rejected). A test asserting only the summed total re-hides the multi-boundary misattribution. Separately assert `tail+head == LEAST(now()-expected, max_delta)` (SC-7 total-clamp).

## MAJOR
- Struct-return redesign is a money-query change (T2/T3), NOT mechanical. `p2_start` MUST be surfaced FROM SQL (`metering_period_end(@expected)`), never Go `time.Truncate`/month-add (EC-7 pure-UTC; DST/drift). Any Go-side period-boundary derivation = CRITICAL.
- Atomicity: the 1-or-2 usage upserts + cursor advance + counter upsert = ONE tx. `pool.Begin` → `SET LOCAL app.tenant_id=''` → MeterCompute*(ErrNoRows=HELD, skip usage) → 1|2 UpsertUsageRecord → Commit. No "read boundaries in tx A, write usage in tx B" split.

## MINOR
- `UpsertUsageRecord` conflict target already correct (`ON CONFLICT (tenant_id,metric,period_start)`). The redesign RED should still include a same-period double-tick asserting accumulate (5+7=12, first id kept) so it doesn't regress.

## Recommended first RED increment (SEPA)
`TestMeterCompute_ReturnsPeriodBoundaries` (same-period) — call generated `MeterCompute`, scan THREE columns `{delta, period_start, period_end}`, assert `period_start == metering_period_start(expected)` + `period_end == metering_period_end(expected)`. **Fails to COMPILE today** (current `MeterCompute :one` returns scalar `int64`) — a compile-RED that forces the SELECT struct-return + sqlc regen. Do same-period FIRST (1 row, 1 usage) before the split's two-usage struct — isolates the sqlc-struct mechanic from the split money logic. **Do NOT start with the wrapper** — the query return-contract must compile as a struct first. **Before any wrapper code:** wire the pool to `loopzero_worker` + skip-if-owner helper (finding #1).

## Main-session decision (honest, Rule 1 + skill § "when context is exhausting")
The SEPA's two CRITICAL findings confirm this is subtle, non-mechanical money-writer work (RLS-theatre + split-gaming are exactly the traps). This resume iteration was set up + consulted in a heavily-saturated session context (the entire prior work + governance + commit + merge lived here). Per `skills/implement/SKILL.md` — *"'Run out of session context' is NOT a valid halt reason... the correct response is to let ralph-loop's Stop hook trigger a fresh iteration, NOT to pause and ask the user"* — the honest action is to STOP this turn WITHOUT emitting the completion promise and let the Stop hook re-feed a FRESH iteration 2 (post-compaction) that starts with these CRITICAL findings + the harness map front-and-center. No false promise. Real progress captured: SEPA brief + the two money-CRITICAL guardrails the redesign must honor.
