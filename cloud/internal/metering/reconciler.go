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

// Reconciler compares usage_records with actual data to detect and correct drift.
// §1.6: Runs daily. Auto-corrects drift < 5%. Alerts on drift >= 5%.
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

	var driftCount, correctedCount int
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
		if driftPct > 0 && driftPct <= DriftThresholdPct {
			// §1.6: Auto-correct small drift
			_, corrErr := r.db.Exec(
				`INSERT INTO usage_records (id, tenant_id, metric, quantity, period_start, period_end, reported_to_stripe, created_at)
				 VALUES ($1, $2, 'builds', $3, $4, $5, false, now())
				 ON CONFLICT (tenant_id, metric, period_start)
				 DO UPDATE SET quantity = $3, reported_to_stripe = false`,
				fmt.Sprintf("reconcile_%s_%d", tenantID, periodStart.Unix()),
				tenantID, actualCount, periodStart,
				periodStart.AddDate(0, 1, 0),
			)
			if corrErr != nil {
				r.logger.Error("failed to auto-correct drift", "tenant", tenantID, "error", corrErr)
			} else {
				correctedCount++
				r.logger.Info("auto-corrected usage drift",
					"tenant", tenantID, "metric", "builds",
					"recorded", recordedCount, "actual", actualCount,
					"drift_pct", fmt.Sprintf("%.1f%%", driftPct),
				)
			}
		} else if driftPct > DriftThresholdPct {
			driftCount++
			r.logger.Warn("usage drift exceeds threshold — requires manual review",
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

	r.logger.Info("reconciliation complete", "metric", "builds",
		"tenants_with_drift", driftCount, "auto_corrected", correctedCount)
	return nil
}
