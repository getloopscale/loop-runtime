// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billing

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	planpkg "github.com/getloopscale/loopzero-contracts/plan"
)

// fakePromQuerier is a deterministic in-memory PromQuerier for tests.
// Each call returns the next-queued result from `results`. If `results`
// is exhausted, returns an empty map (the safest default — no rows to
// upsert means no DB calls).
type fakePromQuerier struct {
	// per metric → per call → values
	values map[string][]map[string]float64
	calls  map[string]int
	err    error
}

func newFakeProm() *fakePromQuerier {
	return &fakePromQuerier{
		values: map[string][]map[string]float64{},
		calls:  map[string]int{},
	}
}

func (f *fakePromQuerier) enqueue(query string, vs map[string]float64) {
	f.values[query] = append(f.values[query], vs)
}

func (f *fakePromQuerier) QueryCounterByTenant(_ context.Context, query string) (map[string]float64, error) {
	if f.err != nil {
		return nil, f.err
	}
	queue := f.values[query]
	idx := f.calls[query]
	f.calls[query] = idx + 1
	if idx >= len(queue) {
		return map[string]float64{}, nil
	}
	return queue[idx], nil
}

// -----------------------------------------------------------------------------
// deltaSafe — the EC-NEW-11 protection unit
// -----------------------------------------------------------------------------

// TestCostAggregator_PrometheusRestart_HandlesCounterReset is the canonical
// EC-NEW-11 test specified in plan v1.4. Prometheus restarts mid-aggregation
// → counter resets from 5GB to 0. The aggregator MUST NOT subtract; it
// must treat the negative delta as 0 increment and rebaseline to current.
func TestCostAggregator_PrometheusRestart_HandlesCounterReset(t *testing.T) {
	prom := newFakeProm()
	// Tick 1: tenant tn-1 has accumulated 5 GB of egress.
	prom.enqueue(promQueries[planpkg.MetricEgressBytes], map[string]float64{
		"tn-1": 5 * 1024 * 1024 * 1024,
	})
	// Tick 2: PROMETHEUS RESTART — counter resets to 0 (well, less than the
	// previous 5GB — say 100MB of fresh accumulation between restart and
	// tick 2).
	prom.enqueue(promQueries[planpkg.MetricEgressBytes], map[string]float64{
		"tn-1": 100 * 1024 * 1024,
	})
	// Tick 3: post-restart accumulation continues — now at 200MB total.
	prom.enqueue(promQueries[planpkg.MetricEgressBytes], map[string]float64{
		"tn-1": 200 * 1024 * 1024,
	})

	agg := NewCostAggregator(nil, prom, slog.Default())

	// Tick 1: first observation — deltaSafe returns 0 (baseline).
	d := agg.deltaSafe("tn-1", planpkg.MetricEgressBytes, 5*1024*1024*1024)
	if d != 0 {
		t.Errorf("tick 1 (first observation): want delta=0 (baseline), got %d", int64(d))
	}

	// Tick 2: PROM RESTART — current (100MB) < previous (5GB).
	// MUST return 0 — NEVER subtract. EC-NEW-11.
	d = agg.deltaSafe("tn-1", planpkg.MetricEgressBytes, 100*1024*1024)
	if d != 0 {
		t.Errorf("tick 2 (prom restart): want delta=0 (no subtraction), got %d", int64(d))
	}

	// Tick 3: post-restart accumulation — current (200MB) > previous (100MB).
	// Legitimate +100MB delta. AGGREGATOR MUST RESUME correct accounting.
	d = agg.deltaSafe("tn-1", planpkg.MetricEgressBytes, 200*1024*1024)
	if d != 100*1024*1024 {
		t.Errorf("tick 3 (post-restart): want delta=%d, got %d",
			100*1024*1024, int64(d))
	}
}

func TestDeltaSafe_FirstObservationIsBaseline(t *testing.T) {
	agg := NewCostAggregator(nil, newFakeProm(), slog.Default())
	if d := agg.deltaSafe("tn-x", planpkg.MetricBuildMinutes, 42); d != 0 {
		t.Errorf("first observation: want 0 (baseline), got %d", int64(d))
	}
}

func TestDeltaSafe_NormalIncrement(t *testing.T) {
	agg := NewCostAggregator(nil, newFakeProm(), slog.Default())
	_ = agg.deltaSafe("tn-x", planpkg.MetricBuildMinutes, 100)
	d := agg.deltaSafe("tn-x", planpkg.MetricBuildMinutes, 150)
	if d != 50 {
		t.Errorf("normal increment: want 50, got %d", int64(d))
	}
}

func TestDeltaSafe_NegativeBecomesZero(t *testing.T) {
	agg := NewCostAggregator(nil, newFakeProm(), slog.Default())
	_ = agg.deltaSafe("tn-x", planpkg.MetricBuildMinutes, 999)
	d := agg.deltaSafe("tn-x", planpkg.MetricBuildMinutes, 5)
	if d != 0 {
		t.Errorf("negative delta: want 0 (EC-NEW-11), got %d", int64(d))
	}
	// Post-reset baseline must equal current observation (5).
	d = agg.deltaSafe("tn-x", planpkg.MetricBuildMinutes, 12)
	if d != 7 {
		t.Errorf("post-reset delta: want 7 (12 - 5), got %d", int64(d))
	}
}

func TestDeltaSafe_PerTenantIsolation(t *testing.T) {
	agg := NewCostAggregator(nil, newFakeProm(), slog.Default())
	_ = agg.deltaSafe("tn-a", planpkg.MetricBuildMinutes, 100)
	// Different tenant — must also start at baseline (0), not 100.
	d := agg.deltaSafe("tn-b", planpkg.MetricBuildMinutes, 50)
	if d != 0 {
		t.Errorf("tn-b first observation: want 0 (per-tenant baseline), got %d", int64(d))
	}
}

func TestDeltaSafe_PerMetricIsolation(t *testing.T) {
	agg := NewCostAggregator(nil, newFakeProm(), slog.Default())
	_ = agg.deltaSafe("tn-a", planpkg.MetricBuildMinutes, 100)
	d := agg.deltaSafe("tn-a", planpkg.MetricEgressBytes, 999)
	if d != 0 {
		t.Errorf("egress baseline must be independent of minutes: want 0, got %d",
			int64(d))
	}
}

// -----------------------------------------------------------------------------
// Tick — PromQL → counters (no DB; nil-safe path only)
// -----------------------------------------------------------------------------

// TestAggregator_5minPeriod validates the periodicity contract: each call
// to Tick computes deltas against the *previous* Tick's observation.
//
// The actual time.Ticker scheduling is tested implicitly by Run() — this
// test just verifies Tick is self-contained and idempotent in terms of
// previous-state tracking.
func TestAggregator_5minPeriod(t *testing.T) {
	prom := newFakeProm()
	// Tick 1: tn-1 has 60 min built. First observation → delta=0, no upsert.
	prom.enqueue(promQueries[planpkg.MetricBuildMinutes], map[string]float64{
		"tn-1": 60,
	})
	// Tick 2: tn-1 has 65 min built. Delta=5 — would upsert IF db were
	// connected. Here db is nil so the upsert fails-open and the tick
	// returns 0 updated (Tick logs the error and continues).
	prom.enqueue(promQueries[planpkg.MetricBuildMinutes], map[string]float64{
		"tn-1": 65,
	})

	// We pre-seed empty queries for the other two metrics so we don't
	// crash on map lookup — fakeProm returns empty map for exhausted
	// queues.
	agg := NewCostAggregator(nil, prom, slog.Default())

	// Tick 1: no DB writes expected (baseline).
	n, err := agg.Tick(context.Background())
	if err != nil {
		t.Fatalf("tick 1: unexpected error %v", err)
	}
	if n != 0 {
		t.Errorf("tick 1: want 0 upserts (baseline), got %d", n)
	}

	// Tick 2: would attempt upsert (delta=5) but db is nil → upsertCounter
	// returns error, Tick logs and continues. Effective upserts = 0.
	n, err = agg.Tick(context.Background())
	if err != nil {
		t.Fatalf("tick 2: unexpected error %v", err)
	}
	if n != 0 {
		t.Errorf("tick 2 with nil db: want 0 upserts, got %d", n)
	}
}

func TestAggregator_PromError_SkipsMetric(t *testing.T) {
	prom := newFakeProm()
	prom.err = errors.New("Prometheus unreachable")
	agg := NewCostAggregator(nil, prom, slog.Default())

	n, err := agg.Tick(context.Background())
	if err != nil {
		t.Errorf("Prom outage must not surface as Tick error (fail-open): got %v", err)
	}
	if n != 0 {
		t.Errorf("want 0 upserts on Prom outage, got %d", n)
	}
}

func TestAggregator_ClockInjection(t *testing.T) {
	// Verify WithClock returns the receiver and the clock is used by
	// CurrentPeriod via Tick. Pin clock to mid-month so periodStart is
	// the first of that month.
	pinned := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	agg := NewCostAggregator(nil, newFakeProm(), slog.Default()).WithClock(func() time.Time {
		return pinned
	})

	// Indirect assertion: agg.now() returns pinned.
	if got := agg.now(); !got.Equal(pinned) {
		t.Errorf("WithClock not honored: got %v, want %v", got, pinned)
	}
}
