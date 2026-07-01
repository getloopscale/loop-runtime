// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

// metering_store_test.go — PG-REAL tests for the compute-metering money-writer
// (plan metering-compute-loop T2.3). Real Postgres via LOOPZERO_E2E_DATABASE_URL,
// each test in a BEGIN…ROLLBACK tx (no pollution). mig 000048 must be applied.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/getloopscale/loopzero/api/internal/db/generated"
)

// meteringPool opens a pgxpool on LOOPZERO_E2E_DATABASE_URL (owner — the happy-path
// wrapper test proves tx-orchestration + delta-threading; the RLS-load-bearing
// proof lives in the loopzero_worker-DSN negative twin). Skips when the harness is
// not wired. The pool-owning MeteringStore commits its OWN tx, so the test can
// NOT use an outer BEGIN…ROLLBACK — teardown is FK-ordered DELETEs via t.Cleanup.
func meteringPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("LOOPZERO_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("LOOPZERO_E2E_DATABASE_URL not set — real-PG metering wrapper test skipped")
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
	meteringLeaderCell = "cell-1"
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
	dsn := os.Getenv("LOOPZERO_E2E_DATABASE_URL")
	if dsn == "" {
		t.Skip("LOOPZERO_E2E_DATABASE_URL not set — real-PG metering test skipped")
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
// same-period path: ONE tx (SET LOCAL app.tenant_id='' → MeterCompute CTE →
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
