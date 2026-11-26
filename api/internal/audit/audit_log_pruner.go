// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package audit

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/metrics"
)

// AuditLogPruner removes audit_log rows older than the retention window.
//
// T7.3 of deep-review-remediation-plan + ADR-0047 (audit_log retention).
// Default retention: 1 year for security/operational events. Financial
// events flow through Stripe and are NOT in audit_log — Stripe owns their
// retention SLA.
//
// The pruner decides WHEN and HOW MUCH to delete; each batch is deleted by
// an AuditLogBatchDeleter (store.PostgresAuditLogPruneStore in production),
// which owns the transaction and the immutability-trigger bypass.
//
// Usage:
//
//	pruner := platform.NewAuditLogPruner(&store.PostgresAuditLogPruneStore{Pool: pool}, platform.AuditLogPrunerOpts{})
//	pruner.Start(ctx)  // runs nightly at 02:00 local time, no-op until then
type AuditLogPruner struct {
	deleter AuditLogBatchDeleter
	opts    AuditLogPrunerOpts
	logger  *slog.Logger
}

// AuditLogBatchDeleter deletes up to limit audit_log rows older than cutoff,
// oldest first, in one transaction, and reports how many it deleted.
type AuditLogBatchDeleter interface {
	DeleteAuditLogBatch(ctx context.Context, cutoff time.Time, limit int) (int64, error)
}

// AuditLogPrunerOpts configures the pruner.
type AuditLogPrunerOpts struct {
	// RetentionDuration is how long rows are kept. Default: 365 * 24h (1 year).
	RetentionDuration time.Duration
	// BatchSize bounds the rows deleted per transaction to avoid long locks.
	// Default: 1000.
	BatchSize int
	// MaxBatches per run caps total work per nightly invocation. Default: 100
	// (= up to 100k rows per night).
	MaxBatches int
	// Now returns the current time. Injected for tests.
	Now func() time.Time
	// Logger for structured logging. Default: slog.Default().
	Logger *slog.Logger
}

func (o *AuditLogPrunerOpts) defaults() {
	if o.RetentionDuration == 0 {
		o.RetentionDuration = 365 * 24 * time.Hour
	}
	if o.BatchSize == 0 {
		o.BatchSize = 1000
	}
	if o.MaxBatches == 0 {
		o.MaxBatches = 100
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
}

// NewAuditLogPruner constructs a pruner from a batch deleter + options.
func NewAuditLogPruner(deleter AuditLogBatchDeleter, opts AuditLogPrunerOpts) *AuditLogPruner {
	opts.defaults()
	return &AuditLogPruner{
		deleter: deleter,
		opts:    opts,
		logger:  opts.Logger,
	}
}

// PruneOnce runs a single pruning pass — deletes rows in batches up to
// MaxBatches × BatchSize. Returns total rows deleted.
//
// Each batch commits on its own, so partial deletes across batches are
// intentional: a failed batch stops the pass and the next run picks up where
// this one stopped.
func (p *AuditLogPruner) PruneOnce(ctx context.Context) (int64, error) {
	cutoff := p.opts.Now().Add(-p.opts.RetentionDuration)

	var totalDeleted int64
	for batch := 0; batch < p.opts.MaxBatches; batch++ {
		deleted, err := p.deleter.DeleteAuditLogBatch(ctx, cutoff, p.opts.BatchSize)
		if err != nil {
			metrics.AuditLogPrunerFailTotal.Inc()
			return totalDeleted, fmt.Errorf("audit_log_pruner batch %d: %w", batch, err)
		}
		totalDeleted += deleted
		if deleted < int64(p.opts.BatchSize) {
			// Last batch underfilled means we drained the cutoff window.
			break
		}
	}

	metrics.AuditLogPrunerDeletedTotal.Add(float64(totalDeleted))
	p.logger.Info("audit_log_pruner completed",
		"cutoff", cutoff.Format(time.RFC3339),
		"deleted_total", totalDeleted,
		"retention_days", int(p.opts.RetentionDuration.Hours()/24),
	)
	return totalDeleted, nil
}

// Start runs the pruner nightly at 02:00 local time. Returns when ctx is
// canceled. Errors are logged but do not stop the loop — next night
// retries fresh.
func (p *AuditLogPruner) Start(ctx context.Context) {
	const runHour = 2 // 02:00 local time
	for {
		next := nextRunAt(p.opts.Now(), runHour)
		wait := next.Sub(p.opts.Now())
		p.logger.Info("audit_log_pruner scheduled",
			"next_run_at", next.Format(time.RFC3339),
			"wait", wait.Round(time.Second).String(),
		)

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}

		if _, err := p.PruneOnce(ctx); err != nil {
			p.logger.Error("audit_log_pruner run failed", "error", err)
			// Loop continues — next night retries fresh.
		}
	}
}

// nextRunAt returns the next time-of-day occurrence of `hour` (in `now`'s
// timezone). If `hour` is in the past today, returns tomorrow's `hour`.
func nextRunAt(now time.Time, hour int) time.Time {
	loc := now.Location()
	candidate := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, loc)
	if !candidate.After(now) {
		candidate = candidate.Add(24 * time.Hour)
	}
	return candidate
}
