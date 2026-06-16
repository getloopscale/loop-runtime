// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	planpkg "github.com/loopzero/loopzero/pkg/plan"
)

// T1.9 of plans/2026-05-12-launch-readiness-waves-plan.md (v1.4).
//
// CostAggregator runs every 5 minutes (per plan), reads Prometheus counters
// for the three cost metrics, and UPSERTs `cost_counters` rows. The
// enforcement middleware reads from `cost_counters` synchronously on every
// mutation; the aggregator's job is to keep the counter within ~5 min of
// reality.
//
// EC-NEW-11 (Prometheus restart handles counter reset):
//   Prometheus counter is monotonically increasing UNTIL Prom restarts, at
//   which point it resets to zero. Naive delta-computation would yield
//   `current - previous = NEGATIVE`, which if subtracted from cost_counters
//   would incorrectly relax the cap. The aggregator treats any negative
//   delta as zero (the new period's accumulation starts fresh after Prom
//   restart; we lose visibility into the gap, but we never UNDER-count).

// PromQuerier is the DIP interface for fetching gauge/counter values from
// Prometheus. Production impl wraps the Prometheus HTTP client; tests use
// in-memory.
type PromQuerier interface {
	// QueryCounterByTenant runs a PromQL query keyed by tenant_id label
	// and returns map[tenantID]value. Returns error on Prom outage; the
	// aggregator treats this as "skip tick" (no UPSERT this round).
	QueryCounterByTenant(ctx context.Context, query string) (map[string]float64, error)
}

// CostAggregator periodically pulls Prom counters, computes deltas, and
// upserts per-tenant cost_counters rows.
type CostAggregator struct {
	db     *sql.DB
	prom   PromQuerier
	logger *slog.Logger

	// previous tick values per (tenant, metric) — used to compute deltas.
	// EC-NEW-11: when current < previous, treat delta as 0 (Prom reset).
	previous map[string]float64

	// clock injection for deterministic period tests.
	now func() time.Time
}

// NewCostAggregator constructs an aggregator. The clock defaults to
// time.Now; tests inject a deterministic clock via WithClock.
func NewCostAggregator(db *sql.DB, prom PromQuerier, logger *slog.Logger) *CostAggregator {
	if logger == nil {
		logger = slog.Default()
	}
	return &CostAggregator{
		db:       db,
		prom:     prom,
		logger:   logger,
		previous: map[string]float64{},
		now:      time.Now,
	}
}

// WithClock injects a deterministic clock. Returns the receiver for chaining.
func (a *CostAggregator) WithClock(now func() time.Time) *CostAggregator {
	a.now = now
	return a
}

// promQueries maps each canonical cost metric to its PromQL query. The
// queries assume `loopzero_*` instrumentation already in place (see CLAUDE.md
// observability section).
var promQueries = map[planpkg.CostMetric]string{
	planpkg.MetricEgressBytes:  `sum by (tenant) (loopzero_egress_bytes_total)`,
	planpkg.MetricDOCRPulls:    `sum by (tenant) (loopzero_docr_pulls_total)`,
	planpkg.MetricBuildMinutes: `sum by (tenant) (rate(loopzero_build_duration_seconds_sum[5m]) * 60)`,
}

// Tick runs one aggregation pass: queries Prometheus, computes deltas,
// and upserts cost_counters. Returns the count of rows updated.
//
// Errors fail-open: if Prom is down or DB is degraded, the aggregator
// logs and returns. The enforcement middleware fails-open on counter
// read errors too — so a degraded aggregator never blocks production
// traffic.
func (a *CostAggregator) Tick(ctx context.Context) (int, error) {
	periodStart, periodEnd := planpkg.CurrentPeriod(a.now())
	updated := 0

	for metric, query := range promQueries {
		values, err := a.prom.QueryCounterByTenant(ctx, query)
		if err != nil {
			a.logger.Warn("cost aggregator: Prometheus query failed; skipping metric",
				"metric", string(metric),
				"error", err)
			continue
		}

		for tenantID, current := range values {
			delta := a.deltaSafe(tenantID, metric, current)
			if delta == 0 {
				continue
			}

			if err := a.upsertCounter(ctx, tenantID, metric, periodStart, periodEnd, int64(delta)); err != nil {
				a.logger.Error("cost aggregator: upsert failed",
					"tenant_id", tenantID,
					"metric", string(metric),
					"delta", delta,
					"error", err)
				continue
			}
			updated++
		}
	}

	return updated, nil
}

// deltaSafe computes the safe delta between current Prom value and the
// previously seen value for (tenant, metric).
//
// EC-NEW-11 protection: if `current < previous`, Prometheus restarted and
// the counter reset. The legitimate accumulation between previous and the
// restart is LOST (no way to recover); we treat the delta as 0 to avoid
// subtracting from cost_counters. The new baseline becomes `current` so
// subsequent ticks compute correctly.
//
// Returns 0 on first-seen (previous absent — counter starts at current).
func (a *CostAggregator) deltaSafe(tenantID string, metric planpkg.CostMetric, current float64) float64 {
	key := tenantID + "|" + string(metric)
	previous, seen := a.previous[key]
	a.previous[key] = current

	if !seen {
		// First observation — accept current as the new baseline. The
		// counter row in DB starts at 0 (or wherever the migration left
		// it); subsequent ticks accumulate from here.
		return 0
	}

	delta := current - previous
	if delta < 0 {
		// Prometheus reset (restart, recreate). Do NOT subtract from
		// cost_counters. Resetting baseline to current means next tick's
		// delta starts fresh and accurate.
		return 0
	}
	return delta
}

// upsertCounter inserts or increments the cost_counters row for
// (tenant, metric, period). Uses the composite PK as the conflict target.
//
// Reads plan_tier from subscriptions table inline to avoid a separate
// store dependency at this layer; the column is a snapshot of the plan
// at the time of write.
func (a *CostAggregator) upsertCounter(ctx context.Context, tenantID string, metric planpkg.CostMetric, periodStart, periodEnd int64, delta int64) error {
	if a.db == nil {
		return errors.New("cost_aggregator: db is nil")
	}
	// Read plan inline. If subscriptions has no row, default to "starter"
	// — matches PlanLimiter fallback.
	var plan string
	err := a.db.QueryRowContext(ctx,
		`SELECT plan FROM subscriptions WHERE tenant_id = $1 AND status = 'active'`,
		tenantID).Scan(&plan)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("read plan: %w", err)
		}
		plan = string(planpkg.PlanStarter)
	}

	_, err = a.db.ExecContext(ctx, `
		INSERT INTO cost_counters (tenant_id, plan_tier, metric, period_start, period_end, value)
		VALUES ($1, $2, $3, to_timestamp($4), to_timestamp($5), $6)
		ON CONFLICT (tenant_id, metric, period_start) DO UPDATE SET
			value = cost_counters.value + EXCLUDED.value,
			plan_tier = EXCLUDED.plan_tier,
			updated_at = now()
	`, tenantID, plan, string(metric), periodStart, periodEnd, delta)
	if err != nil {
		return fmt.Errorf("upsert cost_counter: %w", err)
	}
	return nil
}

// Run starts the aggregator loop. Cancels on ctx.Done. Intended to be
// invoked from a goroutine by the cloud control plane bootstrap.
func (a *CostAggregator) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			a.logger.Info("cost aggregator: stopping",
				"reason", ctx.Err())
			return
		case <-ticker.C:
			updated, err := a.Tick(ctx)
			if err != nil {
				a.logger.Error("cost aggregator: tick failed", "error", err)
				continue
			}
			a.logger.Debug("cost aggregator: tick complete", "rows_updated", updated)
		}
	}
}
