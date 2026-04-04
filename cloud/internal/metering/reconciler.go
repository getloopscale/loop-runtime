// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metering

import (
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"time"
)

// Reconciler compares usage_records with actual data to detect drift.
// Runs weekly as a background job. Logs drift but does not auto-correct.
type Reconciler struct {
	db     *sql.DB
	logger *slog.Logger

	// DriftCallback is called when drift > threshold.
	// In production, this emits a Prometheus metric.
	DriftCallback func(tenantID, metric string, recorded, actual int, driftPct float64)
}

// NewReconciler creates a usage reconciler.
func NewReconciler(db *sql.DB, logger *slog.Logger) *Reconciler {
	return &Reconciler{db: db, logger: logger}
}

// DriftThresholdPct is the percentage above which drift is flagged.
const DriftThresholdPct = 5.0

// ReconcileBuilds compares recorded build usage with actual build count.
func (r *Reconciler) ReconcileBuilds() error {
	if r.db == nil {
		return nil
	}

	now := time.Now().UTC()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	// Get actual build counts per tenant for current period
	rows, err := r.db.Query(
		`SELECT b.tenant_id, COUNT(*) as actual_count
		 FROM builds b
		 WHERE b.created_at >= $1
		 AND b.status IN ('SUCCEEDED', 'FAILED')
		 GROUP BY b.tenant_id`,
		periodStart,
	)
	if err != nil {
		return fmt.Errorf("query actual builds: %w", err)
	}
	defer rows.Close()

	var driftCount int
	for rows.Next() {
		var tenantID string
		var actualCount int
		if err := rows.Scan(&tenantID, &actualCount); err != nil {
			continue
		}

		// Get recorded count
		var recordedCount int
		err := r.db.QueryRow(
			"SELECT COALESCE(quantity, 0) FROM usage_records WHERE tenant_id = $1 AND metric = 'builds' AND period_start = $2",
			tenantID, periodStart,
		).Scan(&recordedCount)
		if err != nil {
			recordedCount = 0
		}

		if actualCount == 0 {
			continue
		}

		driftPct := math.Abs(float64(recordedCount-actualCount)) / float64(actualCount) * 100
		if driftPct > DriftThresholdPct {
			driftCount++
			r.logger.Warn("usage drift detected",
				"tenant", tenantID,
				"metric", "builds",
				"recorded", recordedCount,
				"actual", actualCount,
				"drift_pct", fmt.Sprintf("%.1f%%", driftPct),
			)
			if r.DriftCallback != nil {
				r.DriftCallback(tenantID, "builds", recordedCount, actualCount, driftPct)
			}
		}
	}

	r.logger.Info("reconciliation complete", "metric", "builds", "tenants_with_drift", driftCount)
	return nil
}
