// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

// metering_store_test.go — PG-REAL tests for the compute-metering money-writer
// (plan metering-compute-loop T2.3). Real Postgres via LOOPRUNTIME_E2E_DATABASE_URL,
// each test in a BEGIN…ROLLBACK tx (no pollution). mig 000048 must be applied.

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/getloopscale/loop-runtime/api/internal/db/generated"
	"github.com/getloopscale/loop-runtime/pkg/naming"
)

// meteringPool opens a pgxpool on LOOPRUNTIME_E2E_DATABASE_URL (owner — the happy-path
// wrapper test proves tx-orchestration + delta-threading; the RLS-load-bearing
// proof lives in the loopruntime_worker-DSN negative twin). Skips when the harness is
// not wired. The pool-owning MeteringStore commits its OWN tx, so the test can
// NOT use an outer BEGIN…ROLLBACK — teardown is FK-ordered DELETEs via t.Cleanup.
func meteringPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("LOOPRUNTIME_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("LOOPRUNTIME_E2E_DATABASE_URL not set — real-PG metering wrapper test skipped")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return ctx, pool
}

// seedMeteringFixtureCommitted seeds the FK chain + leader_generations +
// metering_cursor via the pool (COMMITTED, so the store's own tx sees it),
// idempotent (FK-ordered DELETE first so a crashed prior run doesn't collide),
// and registers a t.Cleanup that removes the rows. Returns the stored cursor.
func seedMeteringFixtureCommitted(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, cellID string, cursorSQL string) time.Time {
	t.Helper()
	cleanupMeteringTenant(ctx, pool, tenantID, cellID)
	t.Cleanup(func() { cleanupMeteringTenant(ctx, pool, tenantID, cellID) })
	exec := func(sql string, args ...any) {
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed %q: %v", sql, err)
		}
	}
	u, a := "u-"+tenantID, "a-"+tenantID
	exec(`INSERT INTO users(id,email,username) VALUES ($1,$2,$3)`, u, tenantID+"@t.io", tenantID)
	exec(`INSERT INTO accounts(id,display_name,owner_id) VALUES ($1,$2,$3)`, a, tenantID, u)
	exec(`INSERT INTO tenants(id,slug,owner_id,account_id,cell_id) VALUES ($1,$1,$2,$3,$4)`, tenantID, u, a, cellID)
	exec(`INSERT INTO leader_generations(name,cell_id,holder,generation_id,acquired_at,heartbeat_at)
	       VALUES ($1,$2,'pod-x',$3,now(),now())`, meteringLoopName, cellID, meteringGen)
	exec(`INSERT INTO metering_cursor(tenant_id,metric,cell_id,last_metered_at) VALUES ($1,'cpu_core_seconds',$2, `+cursorSQL+`)`, tenantID, cellID)

	var expected time.Time
	if err := pool.QueryRow(ctx,
		`SELECT last_metered_at FROM metering_cursor WHERE tenant_id=$1 AND metric='cpu_core_seconds' AND cell_id=$2`,
		tenantID, cellID,
	).Scan(&expected); err != nil {
		t.Fatalf("read seeded cursor: %v", err)
	}
	return expected
}

// cleanupMeteringTenant removes all rows a metering fixture created, FK-ordered.
// leader_generations is scoped by (name, cellID) — each test uses a unique cellID
// so concurrent fixtures never collide on the (name, cell_id) key.
func cleanupMeteringTenant(ctx context.Context, pool *pgxpool.Pool, tenantID, cellID string) {
	_, _ = pool.Exec(ctx, `DELETE FROM usage_records WHERE tenant_id=$1`, tenantID)
	_, _ = pool.Exec(ctx, `DELETE FROM cost_counters WHERE tenant_id=$1`, tenantID)
	_, _ = pool.Exec(ctx, `DELETE FROM metering_cursor WHERE tenant_id=$1`, tenantID)
	_, _ = pool.Exec(ctx, `DELETE FROM leader_generations WHERE name=$1 AND cell_id=$2`, meteringLoopName, cellID)
	_, _ = pool.Exec(ctx, `DELETE FROM tenants WHERE id=$1`, tenantID)
	_, _ = pool.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, "a-"+tenantID)
	_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, "u-"+tenantID)
}

const (
	meteringLoopName   = "compute-metering"
	meteringLeaderCell = "cell-0"
	meteringGen        = int64(7)
)

// seedMeteringFixture inserts the FK chain (users→accounts→tenants) + a
// leader_generations row + a metering_cursor seeded `ageSeconds` before now,
// all inside the caller's tx (rolled back by the test). Returns the exact
// stored cursor value to pass as @expected (a moving target breaks the CAS).
func seedMeteringFixture(t *testing.T, ctx context.Context, tx pgx.Tx, tenantID, cellID string, ageSeconds int) pgtype.Timestamptz {
	t.Helper()
	exec := func(sql string, args ...any) {
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed %q: %v", sql, err)
		}
	}
	u, a := "u-"+tenantID, "a-"+tenantID
	exec(`INSERT INTO users(id,email,username) VALUES ($1,$2,$3)`, u, tenantID+"@t.io", tenantID)
	exec(`INSERT INTO accounts(id,display_name,owner_id) VALUES ($1,$2,$3)`, a, tenantID, u)
	exec(`INSERT INTO tenants(id,slug,owner_id,account_id,cell_id) VALUES ($1,$1,$2,$3,$4)`, tenantID, u, a, cellID)
	exec(`INSERT INTO leader_generations(name,cell_id,holder,generation_id,acquired_at,heartbeat_at)
	       VALUES ($1,$2,'pod-x',$3,now(),now())`, meteringLoopName, cellID, meteringGen)
	exec(`INSERT INTO metering_cursor(tenant_id,metric,cell_id,last_metered_at)
	       VALUES ($1,'cpu_core_seconds',$2, now() - make_interval(secs => $3))`, tenantID, cellID, ageSeconds)

	var expected pgtype.Timestamptz
	if err := tx.QueryRow(ctx,
		`SELECT last_metered_at FROM metering_cursor WHERE tenant_id=$1 AND metric='cpu_core_seconds' AND cell_id=$2`,
		tenantID, cellID,
	).Scan(&expected); err != nil {
		t.Fatalf("read seeded cursor: %v", err)
	}
	return expected
}

func meterTx(t *testing.T) (context.Context, pgx.Tx, func()) {
	t.Helper()
	dsn := os.Getenv("LOOPRUNTIME_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("LOOPRUNTIME_E2E_DATABASE_URL not set — real-PG metering test skipped")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		conn.Close(ctx)
		t.Fatalf("begin: %v", err)
	}
	return ctx, tx, func() { _ = tx.Rollback(ctx); conn.Close(ctx) }
}

func maxDelta1h() pgtype.Interval {
	return pgtype.Interval{Microseconds: 3600 * 1_000_000, Valid: true}
}

// TestMeterCompute_ReturnsPeriodBoundaries — the same-period writer must return
// the period boundaries alongside the delta (SC-7 return-contract redesign) so
// the wrapper threads the per-period cost into usage_records without ANY Go
// date math (EC-7 pure-UTC — boundaries come from SQL). RED against the current
// scalar-int64 return (row.Delta / row.PeriodStart do not compile).
func TestMeterCompute_ReturnsPeriodBoundaries(t *testing.T) {
	ctx, tx, done := meterTx(t)
	defer done()

	const tenantID = "tn-mc-bounds"
	expected := seedMeteringFixture(t, ctx, tx, tenantID, meteringLeaderCell, 10) // 10s ago, same period

	q := generated.New()
	row, err := q.MeterCompute(ctx, tx, generated.MeterComputeParams{
		ReqMillicore: 500,
		TenantID:     tenantID,
		Metric:       "cpu_core_seconds",
		CellID:       meteringLeaderCell,
		Expected:     expected,
		Gen:          meteringGen,
		LoopName:     meteringLoopName,
		LeaderCell:   meteringLeaderCell,
		MaxDelta:     maxDelta1h(),
		PlanTier:     "starter",
	})
	if err != nil {
		t.Fatalf("MeterCompute: %v", err)
	}

	// delta = metering_cost(500, 10s) = 500*10/1000 = 5.
	if row.Delta != 5 {
		t.Errorf("delta = %d, want 5 (metering_cost(500, 10s))", row.Delta)
	}

	// Period boundaries MUST match the pure-UTC SQL functions of @expected —
	// the wrapper uses THESE for the usage_records row, never Go time math.
	var wantStart, wantEnd pgtype.Timestamptz
	if err := tx.QueryRow(ctx,
		`SELECT metering_period_start($1::timestamptz), metering_period_end($1::timestamptz)`,
		expected,
	).Scan(&wantStart, &wantEnd); err != nil {
		t.Fatalf("read expected period bounds: %v", err)
	}
	if row.PeriodStart.Time != wantStart.Time {
		t.Errorf("period_start = %v, want %v (metering_period_start(expected))", row.PeriodStart.Time, wantStart.Time)
	}
	if row.PeriodEnd.Time != wantEnd.Time {
		t.Errorf("period_end = %v, want %v (metering_period_end(expected))", row.PeriodEnd.Time, wantEnd.Time)
	}
}

// TestMeterComputeSplit_ReturnsTwoPeriodCosts — the period-rollover writer must
// return the tail + head costs SEPARATELY plus BOTH period windows (SC-7), so
// the wrapper writes TWO usage_records (tail→P1, head→P2) in the right periods.
// Returning only a summed total would re-hide the multi-boundary misattribution
// MEET-006 rejected. RED against the current scalar-int64 return.
//
// Seed: expected = 10s before this month's UTC start (straddle); max_delta = 20s
// so the clamp bites past the boundary → tail = 10s (→cost 5, in P1 = prior month),
// head = 10s (→cost 5, in P2 = current month = metering_period_end(expected)).
func TestMeterComputeSplit_ReturnsTwoPeriodCosts(t *testing.T) {
	ctx, tx, done := meterTx(t)
	defer done()

	const tenantID = "tn-split-2cost"
	// Seed the cursor 10s BEFORE this month's UTC boundary (prior month → straddle).
	u, a := "u-"+tenantID, "a-"+tenantID
	mustExec := func(sql string, args ...any) {
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed %q: %v", sql, err)
		}
	}
	mustExec(`INSERT INTO users(id,email,username) VALUES ($1,$2,$3)`, u, tenantID+"@t.io", tenantID)
	mustExec(`INSERT INTO accounts(id,display_name,owner_id) VALUES ($1,$2,$3)`, a, tenantID, u)
	mustExec(`INSERT INTO tenants(id,slug,owner_id,account_id,cell_id) VALUES ($1,$1,$2,$3,$4)`, tenantID, u, a, meteringLeaderCell)
	mustExec(`INSERT INTO leader_generations(name,cell_id,holder,generation_id,acquired_at,heartbeat_at)
	           VALUES ($1,$2,'pod-x',$3,now(),now())`, meteringLoopName, meteringLeaderCell, meteringGen)
	mustExec(`INSERT INTO metering_cursor(tenant_id,metric,cell_id,last_metered_at)
	           VALUES ($1,'cpu_core_seconds',$2, metering_period_start(now()) - interval '10 seconds')`, tenantID, meteringLeaderCell)

	var expected pgtype.Timestamptz
	if err := tx.QueryRow(ctx,
		`SELECT last_metered_at FROM metering_cursor WHERE tenant_id=$1`, tenantID,
	).Scan(&expected); err != nil {
		t.Fatalf("read seeded cursor: %v", err)
	}

	q := generated.New()
	row, err := q.MeterComputeSplit(ctx, tx, generated.MeterComputeSplitParams{
		ReqMillicore: 500,
		TenantID:     tenantID,
		Metric:       "cpu_core_seconds",
		CellID:       meteringLeaderCell,
		Expected:     expected,
		Gen:          meteringGen,
		LoopName:     meteringLoopName,
		LeaderCell:   meteringLeaderCell,
		MaxDelta:     pgtype.Interval{Microseconds: 20 * 1_000_000, Valid: true},
		PlanTier:     "starter",
	})
	if err != nil {
		t.Fatalf("MeterComputeSplit: %v", err)
	}

	// tail_cost = metering_cost(500, 10s) = 5; head_cost = metering_cost(500, 10s) = 5.
	if row.TailCost != 5 {
		t.Errorf("tail_cost = %d, want 5", row.TailCost)
	}
	if row.HeadCost != 5 {
		t.Errorf("head_cost = %d, want 5", row.HeadCost)
	}
	// SC-7 total-clamp: tail+head == metering_cost(500, max_delta 20s) = 10.
	if row.TailCost+row.HeadCost != 10 {
		t.Errorf("tail+head = %d, want 10 (metering_cost(500, max_delta))", row.TailCost+row.HeadCost)
	}

	// P1 = expected's period (prior month); P2 = the NEXT period, keyed off
	// metering_period_end(expected) — NEVER period_of(now()) (MEET-006).
	var p1Start, p2Start pgtype.Timestamptz
	if err := tx.QueryRow(ctx,
		`SELECT metering_period_start($1::timestamptz), metering_period_end($1::timestamptz)`, expected,
	).Scan(&p1Start, &p2Start); err != nil {
		t.Fatalf("read expected period bounds: %v", err)
	}
	if row.P1Start.Time != p1Start.Time {
		t.Errorf("p1_start = %v, want %v (period_start(expected))", row.P1Start.Time, p1Start.Time)
	}
	if row.P2Start.Time != p2Start.Time {
		t.Errorf("p2_start = %v, want %v (period_end(expected), NOT period_of(now()))", row.P2Start.Time, p2Start.Time)
	}
}

// TestMeteringStore_MeterCompute_SamePeriod_WritesCounterAndUsage — the wrapper's
// same-period path: ONE tx (SET LOCAL app.tenant_id=” → MeterCompute CTE →
// UpsertUsageRecord) writes cost_counters += delta AND one usage_records row for
// (tenant,metric,period), and reports advanced=true. RED against the not-yet-existing
// MeteringStore/MeterInput/NewMeteringStore/MeterCompute (compile failure).
func TestMeteringStore_MeterCompute_SamePeriod_WritesCounterAndUsage(t *testing.T) {
	ctx, pool := meteringPool(t)
	const tenantID = "tn-ws-mc-same"
	const cellID = "cell-ws-mc-same"
	expected := seedMeteringFixtureCommitted(t, ctx, pool, tenantID, cellID, "now() - interval '10 seconds'")

	store := NewMeteringStore(pool)
	advanced, delta, err := store.MeterCompute(ctx, MeterInput{
		TenantID:     tenantID,
		Metric:       "cpu_core_seconds",
		CellID:       cellID,
		Expected:     expected,
		ReqMillicore: 500,
		Gen:          meteringGen,
		LoopName:     meteringLoopName,
		LeaderCell:   cellID,
		MaxDelta:     time.Hour,
		PlanTier:     "starter",
	})
	if err != nil {
		t.Fatalf("MeterCompute: %v", err)
	}
	if !advanced {
		t.Error("advanced = false, want true (cursor advanced, delta metered)")
	}
	if delta != 5 { // metering_cost(500, 10s) = 5
		t.Errorf("delta = %d, want 5", delta)
	}

	// cost_counters += delta in expected's period.
	var counter int64
	if err := pool.QueryRow(ctx,
		`SELECT value FROM cost_counters WHERE tenant_id=$1 AND metric='cpu_core_seconds'
		   AND period_start = metering_period_start($2::timestamptz)`,
		tenantID, expected,
	).Scan(&counter); err != nil {
		t.Fatalf("read cost_counter: %v", err)
	}
	if counter != 5 {
		t.Errorf("cost_counter = %d, want 5", counter)
	}

	// exactly ONE usage_records row for the period, quantity == delta.
	var usageRows, usageQty int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*), COALESCE(sum(quantity),0) FROM usage_records
		   WHERE tenant_id=$1 AND metric='cpu_core_seconds'
		     AND period_start = metering_period_start($2::timestamptz)`,
		tenantID, expected,
	).Scan(&usageRows, &usageQty); err != nil {
		t.Fatalf("read usage_records: %v", err)
	}
	if usageRows != 1 {
		t.Errorf("usage_records rows = %d, want 1 (one row per period)", usageRows)
	}
	if usageQty != 5 {
		t.Errorf("usage_records quantity = %d, want 5 (== delta)", usageQty)
	}

	// cursor advanced past expected (SC-1 forward-only).
	var advancedCursor bool
	if err := pool.QueryRow(ctx,
		`SELECT last_metered_at > $2::timestamptz FROM metering_cursor WHERE tenant_id=$1`,
		tenantID, expected,
	).Scan(&advancedCursor); err != nil {
		t.Fatalf("read cursor: %v", err)
	}
	if !advancedCursor {
		t.Error("cursor last_metered_at did not advance past expected")
	}
}

// TestMeteringStore_MeterCompute_Held_NoBill — the money-critical HELD branch:
// a second tick with a now-stale `expected` (cursor already advanced) MUST fail
// the CAS (0 rows → pgx.ErrNoRows), report advanced=false/delta=0, and write NO
// bill — counter, usage rows, and cursor all UNCHANGED from the first advance.
// SC-1/SC-2/SC-6 all funnel to this "held → no bill" path.
func TestMeteringStore_MeterCompute_Held_NoBill(t *testing.T) {
	ctx, pool := meteringPool(t)
	const tenantID = "tn-ws-held"
	const cellID = "cell-ws-held"
	expected := seedMeteringFixtureCommitted(t, ctx, pool, tenantID, cellID, "now() - interval '10 seconds'")
	store := NewMeteringStore(pool)
	in := MeterInput{
		TenantID: tenantID, Metric: "cpu_core_seconds", CellID: cellID, Expected: expected,
		ReqMillicore: 500, Gen: meteringGen, LoopName: meteringLoopName, LeaderCell: cellID,
		MaxDelta: time.Hour, PlanTier: "starter",
	}

	// First tick advances the cursor + bills.
	adv1, d1, err := store.MeterCompute(ctx, in)
	if err != nil {
		t.Fatalf("first MeterCompute: %v", err)
	}
	if !adv1 || d1 != 5 {
		t.Fatalf("first tick: advanced=%v delta=%d, want true 5", adv1, d1)
	}
	snap := func() (counter, usageRows int64, cursor time.Time) {
		if err := pool.QueryRow(ctx, `SELECT COALESCE(sum(value),0) FROM cost_counters WHERE tenant_id=$1`, tenantID).Scan(&counter); err != nil {
			t.Fatalf("snap counter: %v", err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM usage_records WHERE tenant_id=$1`, tenantID).Scan(&usageRows); err != nil {
			t.Fatalf("snap usage: %v", err)
		}
		if err := pool.QueryRow(ctx, `SELECT last_metered_at FROM metering_cursor WHERE tenant_id=$1`, tenantID).Scan(&cursor); err != nil {
			t.Fatalf("snap cursor: %v", err)
		}
		return
	}
	c1, u1, cur1 := snap()

	// Second tick with the SAME (now stale) expected → HELD.
	adv2, d2, err := store.MeterCompute(ctx, in)
	if err != nil {
		t.Fatalf("held tick errored (want nil — HELD is a business outcome): %v", err)
	}
	if adv2 {
		t.Error("held tick advanced=true, want false (stale expected → CAS fails)")
	}
	if d2 != 0 {
		t.Errorf("held delta=%d, want 0", d2)
	}

	c2, u2, cur2 := snap()
	if c2 != c1 {
		t.Errorf("HELD changed counter: %d -> %d (must not bill)", c1, c2)
	}
	if u2 != u1 {
		t.Errorf("HELD added usage row(s): %d -> %d (must not bill)", u1, u2)
	}
	if !cur2.Equal(cur1) {
		t.Errorf("HELD moved cursor: %v -> %v (SC-1: no re-advance)", cur1, cur2)
	}
}

// TestMeteringStore_MeterCompute_ZeroDelta_NoUsageRow — a scale-to-zero tick
// (EC-1, req_millicore=0) advances the cursor and bills 0, but writes NO usage
// record (no spurious 0-quantity Stripe line). advanced=true, delta=0.
func TestMeteringStore_MeterCompute_ZeroDelta_NoUsageRow(t *testing.T) {
	ctx, pool := meteringPool(t)
	const tenantID = "tn-ws-zero"
	const cellID = "cell-ws-zero"
	expected := seedMeteringFixtureCommitted(t, ctx, pool, tenantID, cellID, "now() - interval '10 seconds'")
	store := NewMeteringStore(pool)

	adv, delta, err := store.MeterCompute(ctx, MeterInput{
		TenantID: tenantID, Metric: "cpu_core_seconds", CellID: cellID, Expected: expected,
		ReqMillicore: 0, Gen: meteringGen, LoopName: meteringLoopName, LeaderCell: cellID,
		MaxDelta: time.Hour, PlanTier: "starter",
	})
	if err != nil {
		t.Fatalf("MeterCompute: %v", err)
	}
	if !adv {
		t.Error("advanced=false, want true (scale-to-zero advances the cursor, bills 0)")
	}
	if delta != 0 {
		t.Errorf("delta=%d, want 0 (req_millicore=0)", delta)
	}

	var usageRows int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM usage_records WHERE tenant_id=$1`, tenantID).Scan(&usageRows); err != nil {
		t.Fatalf("read usage: %v", err)
	}
	if usageRows != 0 {
		t.Errorf("usage_records rows=%d, want 0 (no spurious 0-quantity row)", usageRows)
	}

	var cursorAdvanced bool
	if err := pool.QueryRow(ctx,
		`SELECT last_metered_at > $2::timestamptz FROM metering_cursor WHERE tenant_id=$1`, tenantID, expected,
	).Scan(&cursorAdvanced); err != nil {
		t.Fatalf("read cursor: %v", err)
	}
	if !cursorAdvanced {
		t.Error("cursor did not advance on the scale-to-zero tick")
	}
}

// TestMeteringStore_MeterComputeSplit_WritesTwoUsageRecords — the split wrapper
// writes TWO usage_records from ONE straddling advance: tail→P1 {P1Start,P1End},
// head→P2 {P2Start,P2End}. Asserts EACH row's period_start explicitly (SEPA MAJOR:
// swapping the tail/head period pairing silently mis-bills across two Stripe
// periods and compiles clean). Seed: expected 10s before this month's boundary,
// max_delta 20s → tail 10s (cost 5, P1=prior month), head 10s (cost 5, P2=this month).
func TestMeteringStore_MeterComputeSplit_WritesTwoUsageRecords(t *testing.T) {
	ctx, pool := meteringPool(t)
	const tenantID = "tn-ws-split"
	const cellID = "cell-ws-split"
	expected := seedMeteringFixtureCommitted(t, ctx, pool, tenantID, cellID, "metering_period_start(now()) - interval '10 seconds'")
	store := NewMeteringStore(pool)

	adv, delta, err := store.MeterComputeSplit(ctx, MeterInput{
		TenantID: tenantID, Metric: "cpu_core_seconds", CellID: cellID, Expected: expected,
		ReqMillicore: 500, Gen: meteringGen, LoopName: meteringLoopName, LeaderCell: cellID,
		MaxDelta: 20 * time.Second, PlanTier: "starter",
	})
	if err != nil {
		t.Fatalf("MeterComputeSplit: %v", err)
	}
	if !adv {
		t.Error("advanced=false, want true")
	}
	if delta != 10 {
		t.Errorf("delta=%d, want 10 (tail 5 + head 5 = metering_cost(500, max_delta))", delta)
	}

	// tail → P1: assert the FULL window (period_start AND period_end) — a wrong
	// period_end on the right start still mis-bills the Stripe billing window.
	var p1Qty int64
	var p1EndOK bool
	if err := pool.QueryRow(ctx,
		`SELECT quantity, period_end = metering_period_end($2::timestamptz)
		   FROM usage_records WHERE tenant_id=$1 AND period_start=metering_period_start($2::timestamptz)`,
		tenantID, expected,
	).Scan(&p1Qty, &p1EndOK); err != nil {
		t.Fatalf("read P1 usage: %v", err)
	}
	if p1Qty != 5 {
		t.Errorf("P1 usage quantity=%d, want 5 (tail)", p1Qty)
	}
	if !p1EndOK {
		t.Error("P1 usage period_end != metering_period_end(expected) (wrong Stripe window)")
	}
	// head → P2: period_start=period_end(expected) AND period_end=period_end(period_end(expected)).
	var p2Qty int64
	var p2EndOK bool
	if err := pool.QueryRow(ctx,
		`SELECT quantity, period_end = metering_period_end(metering_period_end($2::timestamptz))
		   FROM usage_records WHERE tenant_id=$1 AND period_start=metering_period_end($2::timestamptz)`,
		tenantID, expected,
	).Scan(&p2Qty, &p2EndOK); err != nil {
		t.Fatalf("read P2 usage: %v", err)
	}
	if p2Qty != 5 {
		t.Errorf("P2 usage quantity=%d, want 5 (head)", p2Qty)
	}
	if !p2EndOK {
		t.Error("P2 usage period_end != metering_period_end(period_end(expected)) (wrong Stripe window)")
	}
	// exactly TWO usage rows total.
	var totalRows int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM usage_records WHERE tenant_id=$1`, tenantID).Scan(&totalRows); err != nil {
		t.Fatalf("count usage: %v", err)
	}
	if totalRows != 2 {
		t.Errorf("usage rows=%d, want 2 (split writes tail->P1 + head->P2)", totalRows)
	}
}

// TestMeteringStore_MeterComputeSplit_MultiMonthGap_NoMisattribution — the SC-7
// distinction (MEET-006): with a multi-month straddle, head MUST land in the
// period right AFTER expected (metering_period_end(expected)), NEVER period_of(now).
// Seed: expected = 10s before the month-boundary two months ago, max_delta 20s →
// tail→P(expected) [3 months ago], head→P(expected)+1 [2 months ago]. now is this
// month; NEITHER usage row may fall in period_of(now), and no middle month opens.
func TestMeteringStore_MeterComputeSplit_MultiMonthGap_NoMisattribution(t *testing.T) {
	ctx, pool := meteringPool(t)
	const tenantID = "tn-ws-mmgap"
	const cellID = "cell-ws-mmgap"
	expected := seedMeteringFixtureCommitted(t, ctx, pool, tenantID, cellID,
		"metering_period_start(now() - interval '2 months') - interval '10 seconds'")
	store := NewMeteringStore(pool)

	adv, delta, err := store.MeterComputeSplit(ctx, MeterInput{
		TenantID: tenantID, Metric: "cpu_core_seconds", CellID: cellID, Expected: expected,
		ReqMillicore: 500, Gen: meteringGen, LoopName: meteringLoopName, LeaderCell: cellID,
		MaxDelta: 20 * time.Second, PlanTier: "starter",
	})
	if err != nil {
		t.Fatalf("MeterComputeSplit: %v", err)
	}
	if !adv || delta != 10 {
		t.Fatalf("advanced=%v delta=%d, want true 10 (clamp bounds the multi-month gap to max_delta)", adv, delta)
	}

	// head MUST be in metering_period_end(expected), NOT period_of(now()).
	var headInExpectedNext, rowInNow int64
	if err := pool.QueryRow(ctx,
		`SELECT quantity FROM usage_records WHERE tenant_id=$1 AND period_start=metering_period_end($2::timestamptz)`,
		tenantID, expected,
	).Scan(&headInExpectedNext); err != nil {
		t.Fatalf("read head usage (period_end(expected)): %v", err)
	}
	if headInExpectedNext != 5 {
		t.Errorf("head usage in period_end(expected)=%d, want 5", headInExpectedNext)
	}
	// ZERO rows in period_of(now) — the misattribution MEET-006 rejected.
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(sum(quantity),0) FROM usage_records WHERE tenant_id=$1 AND period_start=metering_period_start(now())`,
		tenantID,
	).Scan(&rowInNow); err != nil {
		t.Fatalf("read now-period usage: %v", err)
	}
	if rowInNow != 0 {
		t.Errorf("usage in period_of(now)=%d, want 0 (head must NOT key into now's period — MEET-006)", rowInNow)
	}
	// exactly TWO usage rows (no middle month opened).
	var totalRows int64
	pool.QueryRow(ctx, `SELECT count(*) FROM usage_records WHERE tenant_id=$1`, tenantID).Scan(&totalRows)
	if totalRows != 2 {
		t.Errorf("usage rows=%d, want 2 (no middle-period row from a multi-month gap)", totalRows)
	}
}

// TestMeteringStore_MeterCompute_ExactlyOnce_TwoConcurrentPasses — the core
// at-most-once money invariant: two concurrent ticks with the SAME `expected`
// (two leaders / a double-fire) race the CAS (last_metered_at = @expected); EXACTLY
// ONE advances, the other sees the moved cursor → 0 rows → held. The counter is
// billed ONCE (no double-bill), one usage row. Run with -race.
func TestMeteringStore_MeterCompute_ExactlyOnce_TwoConcurrentPasses(t *testing.T) {
	ctx, pool := meteringPool(t)
	const tenantID = "tn-ws-once"
	const cellID = "cell-ws-once"
	expected := seedMeteringFixtureCommitted(t, ctx, pool, tenantID, cellID, "now() - interval '10 seconds'")
	store := NewMeteringStore(pool)
	in := MeterInput{
		TenantID: tenantID, Metric: "cpu_core_seconds", CellID: cellID, Expected: expected,
		ReqMillicore: 500, Gen: meteringGen, LoopName: meteringLoopName, LeaderCell: cellID,
		MaxDelta: time.Hour, PlanTier: "starter",
	}

	const n = 2
	type res struct {
		advanced bool
		delta    int64
		err      error
	}
	out := make([]res, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a, d, e := store.MeterCompute(ctx, in)
			out[i] = res{a, d, e}
		}(i)
	}
	wg.Wait()

	advancedCount := 0
	for _, r := range out {
		if r.err != nil {
			t.Errorf("concurrent MeterCompute err: %v", r.err)
		}
		if r.advanced {
			advancedCount++
			if r.delta != 5 {
				t.Errorf("advanced delta=%d, want 5", r.delta)
			}
		} else if r.delta != 0 {
			t.Errorf("held delta=%d, want 0", r.delta)
		}
	}
	if advancedCount != 1 {
		t.Errorf("advanced count=%d, want EXACTLY 1 (CAS at-most-once — no double-bill)", advancedCount)
	}

	var counter, usageRows int64
	if err := pool.QueryRow(ctx, `SELECT value FROM cost_counters WHERE tenant_id=$1`, tenantID).Scan(&counter); err != nil {
		t.Fatalf("read counter: %v", err)
	}
	if counter != 5 {
		t.Errorf("counter=%d, want 5 (billed exactly once, not twice)", counter)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM usage_records WHERE tenant_id=$1`, tenantID).Scan(&usageRows); err != nil {
		t.Fatalf("read usage: %v", err)
	}
	if usageRows != 1 {
		t.Errorf("usage rows=%d, want 1", usageRows)
	}
}

// TestMeteringStore_MeterCompute_FencedWriterRejected — SC-6: a writer presenting
// a generation_id that does NOT match the loop's own leader_generations row (a
// zombie leader from a superseded generation) is fenced out — the CTE fence
// subquery yields 0 rows → held → no bill.
func TestMeteringStore_MeterCompute_FencedWriterRejected(t *testing.T) {
	ctx, pool := meteringPool(t)
	const tenantID = "tn-ws-fence"
	const cellID = "cell-ws-fence"
	expected := seedMeteringFixtureCommitted(t, ctx, pool, tenantID, cellID, "now() - interval '10 seconds'")
	store := NewMeteringStore(pool)

	adv, delta, err := store.MeterCompute(ctx, MeterInput{
		TenantID: tenantID, Metric: "cpu_core_seconds", CellID: cellID, Expected: expected,
		ReqMillicore: 500, Gen: meteringGen + 1, // stale — the leader row holds meteringGen
		LoopName: meteringLoopName, LeaderCell: cellID, MaxDelta: time.Hour, PlanTier: "starter",
	})
	if err != nil {
		t.Fatalf("MeterCompute: %v", err)
	}
	if adv {
		t.Error("advanced=true, want false (stale fence generation rejected — SC-6)")
	}
	if delta != 0 {
		t.Errorf("delta=%d, want 0 (fenced)", delta)
	}
	var counterRows int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM cost_counters WHERE tenant_id=$1`, tenantID).Scan(&counterRows); err != nil {
		t.Fatalf("read counter: %v", err)
	}
	if counterRows != 0 {
		t.Errorf("cost_counters rows=%d, want 0 (fenced writer must not bill)", counterRows)
	}
}

// TestMeteringStore_MeterCompute_NegativeDeltaClockSkew_NoDecrement — SC-1: a
// backward DB clock (now() < last_metered_at, cursor seeded in the future) fails
// the monotonicity guard → 0 rows → held → the cursor is NOT rewound and nothing
// is billed. Prevents a clock skew from decrementing a counter or re-billing.
func TestMeteringStore_MeterCompute_NegativeDeltaClockSkew_NoDecrement(t *testing.T) {
	ctx, pool := meteringPool(t)
	const tenantID = "tn-ws-skew"
	const cellID = "cell-ws-skew"
	expected := seedMeteringFixtureCommitted(t, ctx, pool, tenantID, cellID, "now() + interval '1 hour'")
	store := NewMeteringStore(pool)

	adv, delta, err := store.MeterCompute(ctx, MeterInput{
		TenantID: tenantID, Metric: "cpu_core_seconds", CellID: cellID, Expected: expected,
		ReqMillicore: 500, Gen: meteringGen, LoopName: meteringLoopName, LeaderCell: cellID,
		MaxDelta: time.Hour, PlanTier: "starter",
	})
	if err != nil {
		t.Fatalf("MeterCompute: %v", err)
	}
	if adv {
		t.Error("advanced=true, want false (now() < last_metered_at → SC-1 monotonicity holds)")
	}
	if delta != 0 {
		t.Errorf("delta=%d, want 0 (backward clock held)", delta)
	}
	var cursorUnchanged bool
	if err := pool.QueryRow(ctx,
		`SELECT last_metered_at = $2::timestamptz FROM metering_cursor WHERE tenant_id=$1`, tenantID, expected,
	).Scan(&cursorUnchanged); err != nil {
		t.Fatalf("read cursor: %v", err)
	}
	if !cursorUnchanged {
		t.Error("cursor moved on a backward clock (SC-1 rewind — money bug)")
	}
}

// TestMeteringStore_GetPlanTierForTenant — reads the active billing plan tier;
// defaults to "starter" when no active subscription exists (EC-4).
func TestMeteringStore_GetPlanTierForTenant(t *testing.T) {
	ctx, pool := meteringPool(t)
	const tenantID = "tn-ws-plan"
	const cellID = "cell-ws-plan"
	seedMeteringFixtureCommitted(t, ctx, pool, tenantID, cellID, "now()")
	if _, err := pool.Exec(ctx,
		`INSERT INTO subscriptions(id,tenant_id,account_id,plan,status) VALUES ($1,$2,$3,'pro','active')`,
		"sub-"+tenantID, tenantID, "a-"+tenantID,
	); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM subscriptions WHERE tenant_id=$1`, tenantID) })

	store := NewMeteringStore(pool)
	tier, err := store.GetPlanTierForTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("GetPlanTierForTenant: %v", err)
	}
	if tier != "pro" {
		t.Errorf("tier=%q, want pro", tier)
	}

	// No active subscription → starter (EC-4).
	tier2, err := store.GetPlanTierForTenant(ctx, "tn-nonexistent")
	if err != nil {
		t.Fatalf("GetPlanTierForTenant(missing): %v", err)
	}
	if tier2 != "starter" {
		t.Errorf("missing-plan tier=%q, want starter (EC-4 default)", tier2)
	}
}

// TestMeteringStore_GetCellForTenant — reads tenants.cell_id (canonical placement
// SoT, INV I2); found=false on NoRows (caller skips, not fatal).
func TestMeteringStore_GetCellForTenant(t *testing.T) {
	ctx, pool := meteringPool(t)
	const tenantID = "tn-ws-cell"
	const cellID = "cell-ws-cell-xyz"
	seedMeteringFixtureCommitted(t, ctx, pool, tenantID, cellID, "now()")
	store := NewMeteringStore(pool)

	cell, found, err := store.GetCellForTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("GetCellForTenant: %v", err)
	}
	if !found {
		t.Error("found=false, want true")
	}
	if cell != cellID {
		t.Errorf("cell=%q, want %q (canonical tenants.cell_id)", cell, cellID)
	}

	_, found2, err := store.GetCellForTenant(ctx, "tn-nonexistent")
	if err != nil {
		t.Fatalf("GetCellForTenant(missing): %v", err)
	}
	if found2 {
		t.Error("found=true for nonexistent tenant, want false (skip, not fatal)")
	}
}

// TestMeteringStore_GetMeteringCursor_ReturnsSeededValue — the worker reads the
// current cursor (the CAS target it threads into MeterInput.Expected) via
// GetMeteringCursor. A seeded cursor is returned found=true with its exact value.
func TestMeteringStore_GetMeteringCursor_ReturnsSeededValue(t *testing.T) {
	ctx, pool := meteringPool(t)
	const tenantID = "tn-ws-getcursor"
	const cellID = "cell-ws-getcursor"
	seeded := seedMeteringFixtureCommitted(t, ctx, pool, tenantID, cellID, "now() - interval '90 seconds'")

	store := NewMeteringStore(pool)
	got, found, err := store.GetMeteringCursor(ctx, tenantID, "cpu_core_seconds", cellID)
	if err != nil {
		t.Fatalf("GetMeteringCursor: %v", err)
	}
	if !found {
		t.Fatal("found=false, want true for a seeded cursor")
	}
	if !got.Equal(seeded) {
		t.Errorf("cursor=%v, want seeded %v", got, seeded)
	}
}

// TestMeteringStore_GetMeteringCursor_NotFoundBeforeSeed — an unseeded (tenant,
// metric, cell) returns found=false + zero time + nil error (pgx.ErrNoRows is
// absorbed, never leaked to the caller). The worker seeds first (SC-5), then reads.
func TestMeteringStore_GetMeteringCursor_NotFoundBeforeSeed(t *testing.T) {
	ctx, pool := meteringPool(t)
	store := NewMeteringStore(pool)
	got, found, err := store.GetMeteringCursor(ctx, "tn-never-seeded-xyz", "cpu_core_seconds", "cell-none")
	if err != nil {
		t.Fatalf("GetMeteringCursor(unseeded): %v", err)
	}
	if found {
		t.Error("found=true, want false for an unseeded cursor")
	}
	if !got.IsZero() {
		t.Errorf("cursor=%v, want zero time when not found", got)
	}
}

// restoreMeteringConfig resets the metering_config singleton to enabled=false
// (its seeded state) both now and via t.Cleanup — the kill-switch tests mutate
// the shared singleton row, so each restores it to avoid cross-test bleed.
func restoreMeteringConfig(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	reset := func() {
		_, _ = pool.Exec(ctx, `INSERT INTO metering_config (id, enabled) VALUES (true, false)
			ON CONFLICT (id) DO UPDATE SET enabled = false`)
	}
	reset()
	t.Cleanup(reset)
}

// TestMeteringStore_IsMeteringEnabled_DisabledWhenSeeded — observe-only ships with
// the kill-switch OFF (EC-5): the seeded singleton reads enabled=false, found=true.
func TestMeteringStore_IsMeteringEnabled_DisabledWhenSeeded(t *testing.T) {
	ctx, pool := meteringPool(t)
	restoreMeteringConfig(t, ctx, pool)
	store := NewMeteringStore(pool)

	enabled, found, err := store.IsMeteringEnabled(ctx)
	if err != nil {
		t.Fatalf("IsMeteringEnabled: %v", err)
	}
	if !found {
		t.Error("found=false, want true (the seeded singleton row exists)")
	}
	if enabled {
		t.Error("enabled=true, want false (observe-only ships DISABLED — EC-5)")
	}
}

// TestMeteringStore_IsMeteringEnabled_EnabledWhenFlagTrue — after an operator flips
// the switch, the reader reports enabled=true.
func TestMeteringStore_IsMeteringEnabled_EnabledWhenFlagTrue(t *testing.T) {
	ctx, pool := meteringPool(t)
	restoreMeteringConfig(t, ctx, pool)
	if _, err := pool.Exec(ctx, `UPDATE metering_config SET enabled = true WHERE id = true`); err != nil {
		t.Fatalf("flip switch: %v", err)
	}
	store := NewMeteringStore(pool)

	enabled, found, err := store.IsMeteringEnabled(ctx)
	if err != nil {
		t.Fatalf("IsMeteringEnabled: %v", err)
	}
	if !found || !enabled {
		t.Errorf("enabled=%v found=%v, want true true (operator flipped the kill-switch)", enabled, found)
	}
}

// TestMeteringStore_IsMeteringEnabled_AbsentRow_EC5_DefaultDisabled — EC-5: a
// deleted config row is NOT an error; it defaults to DISABLED (found=false), so a
// producer that lost its kill-switch row never bills. The caller (loop) WARNs.
func TestMeteringStore_IsMeteringEnabled_AbsentRow_EC5_DefaultDisabled(t *testing.T) {
	ctx, pool := meteringPool(t)
	restoreMeteringConfig(t, ctx, pool)
	if _, err := pool.Exec(ctx, `DELETE FROM metering_config WHERE id = true`); err != nil {
		t.Fatalf("delete config row: %v", err)
	}
	store := NewMeteringStore(pool)

	enabled, found, err := store.IsMeteringEnabled(ctx)
	if err != nil {
		t.Fatalf("an absent config row must not error (EC-5): %v", err)
	}
	if found {
		t.Error("found=true, want false (row deleted — EC-5 absent path)")
	}
	if enabled {
		t.Error("enabled=true on an absent row — EC-5 violated (must default DISABLED)")
	}
}

// TestMeteringStore_IsMeteringEnabled_QueryError_FailsSafeDisabled — the
// load-bearing fail-safe: on a real query error (here a cancelled context), the
// reader returns enabled=false + the error. A metering producer that cannot read
// its own kill-switch MUST NOT bill (never enabled=true on error).
func TestMeteringStore_IsMeteringEnabled_QueryError_FailsSafeDisabled(t *testing.T) {
	ctx, pool := meteringPool(t)
	restoreMeteringConfig(t, ctx, pool)
	store := NewMeteringStore(pool)

	cancelled, cancel := context.WithCancel(ctx)
	cancel() // force the query to fail

	enabled, _, err := store.IsMeteringEnabled(cancelled)
	if err == nil {
		t.Fatal("want a query error on a cancelled context")
	}
	if enabled {
		t.Error("enabled=true on a query error — fail-safe violated (unreadable kill-switch must never bill)")
	}
}

// seedMeteringProjectsEnvs seeds the given projectName→[]env spec: one dummy
// build+release (to satisfy every environment_targets.desired_release_id FK) +
// the projects + the environment_targets. Registers an FK-ordered cleanup; it is
// registered AFTER seedMeteringFixtureCommitted's cleanup, so LIFO runs it FIRST
// (before the fixture deletes the tenant/user those rows reference).
func seedMeteringProjectsEnvs(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, spec map[string][]string) {
	t.Helper()
	exec := func(sql string, args ...any) {
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed %q: %v", sql, err)
		}
	}
	u := "u-" + tenantID
	buildID, relID := "b-"+tenantID, "r-"+tenantID
	exec(`INSERT INTO builds (id, tenant_id, project_id, environment, status, correlation_id, created_by)
	       VALUES ($1,$2,'_','_','SUCCEEDED','c',$3)`, buildID, tenantID, u)
	exec(`INSERT INTO releases (id, build_id, tenant_id, project_id, environment, status, correlation_id, created_by)
	       VALUES ($1,$2,$3,'_','_','ACTIVE','c',$4)`, relID, buildID, tenantID, u)
	for pname := range spec {
		id := "p-" + tenantID + "-" + pname
		exec(`INSERT INTO projects (id, name, tenant_id, created_by) VALUES ($1,$2,$3,$4)`, id, pname, tenantID, u)
	}
	// environment_targets.project_id carries the project NAME in production:
	// SetDesired is called with build.ProjectID / release.ProjectID, which the
	// CLI and autodeploy fill with the name. Seeding the id here is what let the
	// namespace enumeration match nothing in production while this test passed.
	for pname, envs := range spec {
		for _, env := range envs {
			exec(`INSERT INTO environment_targets (tenant_id, project_id, environment, desired_release_id, updated_by, correlation_id)
			       VALUES ($1,$2,$3,$4,$5,'c')`, tenantID, pname, env, relID, u)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM environment_targets WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM releases WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM builds WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(ctx, `DELETE FROM projects WHERE tenant_id=$1`, tenantID)
	})
}

// TestMeteringStore_NamespacesForTenant_AllProjectEnvNamespaces — ADR-5: the
// tenant's metered namespaces are naming.Namespace(tenant, project, env) for EVERY
// (project, env) tuple, NOT a single loopruntime-<tenant> ns (v1.1's single-ns
// under-billing bug). Asserts the EXACT set — a missing ns is the regression.
func TestMeteringStore_NamespacesForTenant_AllProjectEnvNamespaces(t *testing.T) {
	ctx, pool := meteringPool(t)
	const tenantID = "tn-ws-ns"
	const cellID = "cell-ws-ns"
	seedMeteringFixtureCommitted(t, ctx, pool, tenantID, cellID, "now()")
	seedMeteringProjectsEnvs(t, ctx, pool, tenantID, map[string][]string{
		"web": {"staging", "production"},
		"api": {"staging"},
	})
	store := NewMeteringStore(pool)

	got, err := store.NamespacesForTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("NamespacesForTenant: %v", err)
	}

	want := map[string]bool{}
	for _, tuple := range [][2]string{{"web", "staging"}, {"web", "production"}, {"api", "staging"}} {
		ns, nerr := naming.Namespace(tenantID, tuple[0], tuple[1])
		if nerr != nil {
			t.Fatalf("naming.Namespace(%v): %v", tuple, nerr)
		}
		want[ns] = true
	}
	if len(got) != len(want) {
		t.Fatalf("NamespacesForTenant returned %d namespaces %v, want %d %v (ADR-5: every project×env, not a single ns)",
			len(got), got, len(want), want)
	}
	for _, ns := range got {
		if !want[ns] {
			t.Errorf("unexpected namespace %q (not in the project×env set)", ns)
		}
		delete(want, ns)
	}
	if len(want) != 0 {
		t.Errorf("missing namespaces %v (the v1.1 single-ns under-billing bug)", want)
	}
}

// TestMeteringStore_NamespacesForTenant_ZeroProjects_EmptyNotError — a tenant with
// no projects meters nothing: empty slice, no error (tenant-guardian note 1 — an
// Active CR whose PG rows were removed mid-race).
func TestMeteringStore_NamespacesForTenant_ZeroProjects_EmptyNotError(t *testing.T) {
	ctx, pool := meteringPool(t)
	const tenantID = "tn-ws-ns-empty"
	const cellID = "cell-ws-ns-empty"
	seedMeteringFixtureCommitted(t, ctx, pool, tenantID, cellID, "now()")
	store := NewMeteringStore(pool)

	got, err := store.NamespacesForTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("NamespacesForTenant(no projects): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty (a tenant with no projects meters nothing)", got)
	}
}

// TestTenantStoreSqlc_DeleteTenantsFKOrdered_GCsMeteringCursor_SC5 — SC-5 (ADR-13):
// evicting a tenant GCs its metering_cursor rows alongside the other per-tenant
// deletes, so a reused/reprovisioned tenant_id cannot bill a stale
// [old_last_metered_at, now()] window (resurrection double-bill).
func TestTenantStoreSqlc_DeleteTenantsFKOrdered_GCsMeteringCursor_SC5(t *testing.T) {
	ctx, pool := meteringPool(t)
	const tenantID = "tn-ws-gc"
	const cellID = "cell-ws-gc"
	seedMeteringFixtureCommitted(t, ctx, pool, tenantID, cellID, "now()")

	var before int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM metering_cursor WHERE tenant_id=$1`, tenantID).Scan(&before); err != nil {
		t.Fatalf("count cursor before: %v", err)
	}
	if before != 1 {
		t.Fatalf("precondition: metering_cursor rows=%d, want 1", before)
	}

	store := NewTenantStoreSqlc(&db.PgxAdapter{Pool: pool})
	counts, err := store.DeleteTenantsFKOrdered(ctx, []string{tenantID})
	if err != nil {
		t.Fatalf("DeleteTenantsFKOrdered: %v", err)
	}
	if counts.MeteringCursors != 1 {
		t.Errorf("counts.MeteringCursors=%d, want 1 (SC-5 GC)", counts.MeteringCursors)
	}

	var after int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM metering_cursor WHERE tenant_id=$1`, tenantID).Scan(&after); err != nil {
		t.Fatalf("count cursor after: %v", err)
	}
	if after != 0 {
		t.Errorf("metering_cursor rows=%d after eviction, want 0 (a reused tenant_id must not bill a stale window — SC-5)", after)
	}
}

// TestMeteringStore_SeedMeteringCursor_Idempotent — seeds the cursor to now() on
// first sight (SC-5/EC-10); a second seed is a no-op (existing cursor untouched).
func TestMeteringStore_SeedMeteringCursor_Idempotent(t *testing.T) {
	ctx, pool := meteringPool(t)
	const tenantID = "tn-ws-seed"
	const cellID = "cell-ws-seed"
	seedMeteringFixtureCommitted(t, ctx, pool, tenantID, cellID, "now()")
	// Remove the fixture's cursor so SeedMeteringCursor creates it.
	if _, err := pool.Exec(ctx, `DELETE FROM metering_cursor WHERE tenant_id=$1`, tenantID); err != nil {
		t.Fatalf("clear cursor: %v", err)
	}
	store := NewMeteringStore(pool)

	if err := store.SeedMeteringCursor(ctx, tenantID, "cpu_core_seconds", cellID); err != nil {
		t.Fatalf("SeedMeteringCursor: %v", err)
	}
	var cnt int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM metering_cursor WHERE tenant_id=$1`, tenantID).Scan(&cnt); err != nil {
		t.Fatalf("count cursor: %v", err)
	}
	if cnt != 1 {
		t.Errorf("cursor rows=%d, want 1 after seed", cnt)
	}
	var first time.Time
	pool.QueryRow(ctx, `SELECT last_metered_at FROM metering_cursor WHERE tenant_id=$1`, tenantID).Scan(&first)

	// Second seed is idempotent — must NOT reset the cursor.
	if err := store.SeedMeteringCursor(ctx, tenantID, "cpu_core_seconds", cellID); err != nil {
		t.Fatalf("SeedMeteringCursor (2nd): %v", err)
	}
	var second time.Time
	pool.QueryRow(ctx, `SELECT last_metered_at FROM metering_cursor WHERE tenant_id=$1`, tenantID).Scan(&second)
	if !second.Equal(first) {
		t.Errorf("SeedMeteringCursor not idempotent: %v -> %v (would zero a live cursor)", first, second)
	}
}

// TestMeteringStore_DispatchClockDisagreement_HoldsNotMisbills — SC-2: the
// same-period vs split dispatch is governed by the DB now() period predicate, not
// the Go clock. If the worker's Go-clock guess picks the WRONG writer at a period
// boundary, the DB predicate yields 0 rows (HELD) — never a misbill/negative head.
// Both directions: same-period writer on an actual straddle HOLDS; split writer on
// an actual same-period HOLDS. The correct writer bills on the next tick.
func TestMeteringStore_DispatchClockDisagreement_HoldsNotMisbills(t *testing.T) {
	ctx, pool := meteringPool(t)
	store := NewMeteringStore(pool)

	// Direction 1: actual straddle, but the SAME-PERIOD writer is invoked → the
	// `period_of(now()) = period_of(expected)` predicate fails → HELD.
	t.Run("same-period-writer-on-straddle-holds", func(t *testing.T) {
		const tenantID = "tn-ws-disp1"
		const cellID = "cell-ws-disp1"
		expected := seedMeteringFixtureCommitted(t, ctx, pool, tenantID, cellID, "metering_period_start(now()) - interval '10 seconds'")
		adv, delta, err := store.MeterCompute(ctx, MeterInput{
			TenantID: tenantID, Metric: "cpu_core_seconds", CellID: cellID, Expected: expected,
			ReqMillicore: 500, Gen: meteringGen, LoopName: meteringLoopName, LeaderCell: cellID,
			MaxDelta: time.Hour, PlanTier: "starter",
		})
		if err != nil {
			t.Fatalf("MeterCompute: %v", err)
		}
		if adv || delta != 0 {
			t.Errorf("same-period writer on a straddle: advanced=%v delta=%d, want false 0 (SC-2 HOLD)", adv, delta)
		}
	})

	// Direction 2: actual same-period, but the SPLIT writer is invoked → the
	// `period_of(now()) <> period_of(expected)` predicate fails → HELD.
	t.Run("split-writer-on-same-period-holds", func(t *testing.T) {
		const tenantID = "tn-ws-disp2"
		const cellID = "cell-ws-disp2"
		expected := seedMeteringFixtureCommitted(t, ctx, pool, tenantID, cellID, "now() - interval '10 seconds'")
		adv, delta, err := store.MeterComputeSplit(ctx, MeterInput{
			TenantID: tenantID, Metric: "cpu_core_seconds", CellID: cellID, Expected: expected,
			ReqMillicore: 500, Gen: meteringGen, LoopName: meteringLoopName, LeaderCell: cellID,
			MaxDelta: time.Hour, PlanTier: "starter",
		})
		if err != nil {
			t.Fatalf("MeterComputeSplit: %v", err)
		}
		if adv || delta != 0 {
			t.Errorf("split writer on same-period: advanced=%v delta=%d, want false 0 (SC-2 HOLD)", adv, delta)
		}
	})
}

// TestMeteringStore_MeterComputeSplit_ContinuesNotResets — SC-4: after a split the
// cursor advances to now() (continuation), NEVER to the boundary B. Resetting to B
// would re-meter [B, now()] on the next tick (double-count). Cursor is
// period-independent (PK has no period column).
func TestMeteringStore_MeterComputeSplit_ContinuesNotResets(t *testing.T) {
	ctx, pool := meteringPool(t)
	const tenantID = "tn-ws-cont"
	const cellID = "cell-ws-cont"
	expected := seedMeteringFixtureCommitted(t, ctx, pool, tenantID, cellID, "metering_period_start(now()) - interval '10 seconds'")
	store := NewMeteringStore(pool)

	adv, _, err := store.MeterComputeSplit(ctx, MeterInput{
		TenantID: tenantID, Metric: "cpu_core_seconds", CellID: cellID, Expected: expected,
		ReqMillicore: 500, Gen: meteringGen, LoopName: meteringLoopName, LeaderCell: cellID,
		MaxDelta: time.Hour, PlanTier: "starter",
	})
	if err != nil {
		t.Fatalf("MeterComputeSplit: %v", err)
	}
	if !adv {
		t.Fatal("expected the split to advance")
	}

	// cursor = now() (past the boundary B), NOT reset to B = metering_period_end(expected).
	var pastBoundary, atBoundary bool
	if err := pool.QueryRow(ctx,
		`SELECT last_metered_at > metering_period_end($2::timestamptz),
		        last_metered_at = metering_period_end($2::timestamptz)
		   FROM metering_cursor WHERE tenant_id=$1`, tenantID, expected,
	).Scan(&pastBoundary, &atBoundary); err != nil {
		t.Fatalf("read cursor: %v", err)
	}
	if atBoundary {
		t.Error("cursor reset to boundary B — SC-4 violated (would re-meter [B, now()] next tick)")
	}
	if !pastBoundary {
		t.Error("cursor not advanced past the boundary — expected continuation to now()")
	}
}

// meteringWorkerPool opens a pool as the GENUINE loopruntime_worker role (RLS applies,
// rolbypassrls=false) via LOOPRUNTIME_E2E_WORKER_DATABASE_URL. Skips when unset. Fails
// LOUD if the DSN resolves to a BYPASSRLS role — an owner connection would make
// the RLS twin theatre (it bypasses the policy unconditionally). SEPA CRITICAL #1.
func meteringWorkerPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("LOOPRUNTIME_E2E_WORKER_DATABASE_URL")
	if dsn == "" {
		t.Skip("LOOPRUNTIME_E2E_WORKER_DATABASE_URL not set — loopruntime_worker RLS test skipped")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("worker pool: %v", err)
	}
	t.Cleanup(pool.Close)
	var bypass bool
	if err := pool.QueryRow(ctx, `SELECT rolbypassrls FROM pg_roles WHERE rolname=current_user`).Scan(&bypass); err != nil {
		t.Fatalf("check bypassrls: %v", err)
	}
	if bypass {
		t.Fatal("LOOPRUNTIME_E2E_WORKER_DATABASE_URL connects as a BYPASSRLS role — the RLS twin would be theatre; use a genuine loopruntime_worker DSN")
	}
	return ctx, pool
}

// TestMeteringStore_MeterCompute_UnderRealWorkerRLS — the wrapper writes SUCCEED
// on a genuine loopruntime_worker connection (RLS enforced, no bypass): the SET LOCAL
// app.tenant_id=” inside the tx satisfies the cost_counters policy so the
// cross-tenant write is allowed. Proves the money-writer works under production
// RLS, not just under the owner-bypass used by the other tests.
func TestMeteringStore_MeterCompute_UnderRealWorkerRLS(t *testing.T) {
	wctx, workerPool := meteringWorkerPool(t)
	octx, ownerPool := meteringPool(t)
	const tenantID = "tn-ws-rls"
	const cellID = "cell-ws-rls"
	expected := seedMeteringFixtureCommitted(t, octx, ownerPool, tenantID, cellID, "now() - interval '10 seconds'")

	store := NewMeteringStore(workerPool)
	adv, delta, err := store.MeterCompute(wctx, MeterInput{
		TenantID: tenantID, Metric: "cpu_core_seconds", CellID: cellID, Expected: expected,
		ReqMillicore: 500, Gen: meteringGen, LoopName: meteringLoopName, LeaderCell: cellID,
		MaxDelta: time.Hour, PlanTier: "starter",
	})
	if err != nil {
		t.Fatalf("wrapper under loopruntime_worker RLS: %v (SET LOCAL='' should satisfy the policy)", err)
	}
	if !adv || delta != 5 {
		t.Errorf("advanced=%v delta=%d, want true 5 (write succeeds under real RLS)", adv, delta)
	}
	var counter int64
	if err := ownerPool.QueryRow(octx, `SELECT value FROM cost_counters WHERE tenant_id=$1`, tenantID).Scan(&counter); err != nil {
		t.Fatalf("read counter (owner view): %v", err)
	}
	if counter != 5 {
		t.Errorf("counter=%d, want 5 (loopruntime_worker wrote via SET LOCAL='')", counter)
	}
}

// TestMeteringStore_WithoutRLSBypass_WritesZeroRows — the negative twin: on a
// loopruntime_worker connection carrying a LEAKED foreign app.tenant_id and WITHOUT the
// wrapper's reset, the raw CTE's cost_counters write violates the RLS policy and
// is rejected — NO counter row for the target tenant. Proves the wrapper's
// SET LOCAL app.tenant_id=” is load-bearing against a leaked pooled setting.
func TestMeteringStore_WithoutRLSBypass_WritesZeroRows(t *testing.T) {
	wctx, workerPool := meteringWorkerPool(t)
	octx, ownerPool := meteringPool(t)
	const tenantID = "tn-ws-noby"
	const cellID = "cell-ws-noby"
	expected := seedMeteringFixtureCommitted(t, octx, ownerPool, tenantID, cellID, "now() - interval '10 seconds'")

	// Raw CTE on a loopruntime_worker tx with a LEAKED foreign app.tenant_id and NO reset.
	tx, err := workerPool.Begin(wctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(wctx)
	if _, err := tx.Exec(wctx, `SET LOCAL app.tenant_id = 'some-other-tenant'`); err != nil {
		t.Fatalf("set leaked tenant: %v", err)
	}
	_, err = generated.New().MeterCompute(wctx, tx, generated.MeterComputeParams{
		ReqMillicore: 500, TenantID: tenantID, Metric: "cpu_core_seconds", CellID: cellID,
		Expected: pgtype.Timestamptz{Time: expected, Valid: true},
		Gen:      meteringGen, LoopName: meteringLoopName, LeaderCell: cellID,
		MaxDelta: pgtype.Interval{Microseconds: 3600 * 1_000_000, Valid: true},
		PlanTier: "starter",
	})
	if err == nil {
		t.Error("raw write with a leaked foreign app.tenant_id succeeded — RLS not enforced (SET LOCAL='' IS load-bearing)")
	}
	_ = tx.Rollback(wctx)

	// No counter row for the target tenant (the blocked write left nothing).
	var rows int64
	if err := ownerPool.QueryRow(octx, `SELECT count(*) FROM cost_counters WHERE tenant_id=$1`, tenantID).Scan(&rows); err != nil {
		t.Fatalf("read counter rows (owner view): %v", err)
	}
	if rows != 0 {
		t.Errorf("cost_counters rows=%d, want 0 (leaked-setting write must be blocked by RLS)", rows)
	}
}

// TestMeteringStore_MeterCompute_ClampsLongGap — ADR-8: a stale cursor within the
// SAME month bills at most max_delta of catch-up, not the full raw gap (a long
// leader outage must not over-bill). Seed the cursor at this month's start (same
// month), max_delta 30s → the same-period writer clamps elapsed to 30s. Asserts
// delta == the DB-computed clamped cost AND (when the gap exceeds max_delta) that
// the clamp strictly reduced the charge below the raw gap.
func TestMeteringStore_MeterCompute_ClampsLongGap(t *testing.T) {
	ctx, pool := meteringPool(t)
	const tenantID = "tn-ws-clamp"
	const cellID = "cell-ws-clamp"
	expected := seedMeteringFixtureCommitted(t, ctx, pool, tenantID, cellID, "metering_period_start(now())")
	store := NewMeteringStore(pool)

	adv, delta, err := store.MeterCompute(ctx, MeterInput{
		TenantID: tenantID, Metric: "cpu_core_seconds", CellID: cellID, Expected: expected,
		ReqMillicore: 500, Gen: meteringGen, LoopName: meteringLoopName, LeaderCell: cellID,
		MaxDelta: 30 * time.Second, PlanTier: "starter",
	})
	if err != nil {
		t.Fatalf("MeterCompute: %v", err)
	}
	if !adv {
		t.Fatal("expected advance")
	}

	var clamped, raw int64
	var gapExceedsMaxDelta bool
	if err := pool.QueryRow(ctx,
		`SELECT metering_cost(500, LEAST(now() - $1::timestamptz, interval '30 seconds')),
		        metering_cost(500, now() - $1::timestamptz),
		        (now() - $1::timestamptz) > interval '30 seconds'`,
		expected,
	).Scan(&clamped, &raw, &gapExceedsMaxDelta); err != nil {
		t.Fatalf("compute expected clamp: %v", err)
	}
	if delta != clamped {
		t.Errorf("delta=%d, want %d (clamped to max_delta 30s)", delta, clamped)
	}
	if gapExceedsMaxDelta && clamped >= raw {
		t.Errorf("clamp did not reduce the charge: clamped=%d raw=%d (ADR-8 clamp inert)", clamped, raw)
	}
	if !gapExceedsMaxDelta {
		t.Logf("note: test ran within 30s of the month start (gap=%ds-ish); clamp bite not exercised this run", 0)
	}
}
