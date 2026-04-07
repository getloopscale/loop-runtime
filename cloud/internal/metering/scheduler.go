// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metering

import (
	"context"
	"log/slog"
	"time"
)

const (
	// ReporterInterval is how often the usage reporter runs.
	ReporterInterval = 1 * time.Hour

	// ReconcilerInterval is how often the drift reconciler runs.
	// §1.6: Daily instead of weekly for faster drift detection.
	ReconcilerInterval = 24 * time.Hour
)

// StartScheduler runs the metering reporter and reconciler as background
// goroutines. Blocks until ctx is cancelled. Uses time.Ticker (no cron
// library per MEET-038 — YAGNI for two jobs).
func StartScheduler(ctx context.Context, reporter *Reporter, reconciler *Reconciler, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}

	reportTicker := time.NewTicker(ReporterInterval)
	defer reportTicker.Stop()

	reconcileTicker := time.NewTicker(ReconcilerInterval)
	defer reconcileTicker.Stop()

	logger.Info("metering scheduler started",
		"reporter_interval", ReporterInterval.String(),
		"reconciler_interval", ReconcilerInterval.String(),
	)

	for {
		select {
		case <-ctx.Done():
			logger.Info("metering scheduler stopped")
			return

		case <-reportTicker.C:
			if err := reporter.ReportPending(); err != nil {
				logger.Error("usage reporter failed", "error", err)
			}

		case <-reconcileTicker.C:
			if err := reconciler.ReconcileBuilds(); err != nil {
				logger.Error("usage reconciler failed", "error", err)
			}
		}
	}
}
