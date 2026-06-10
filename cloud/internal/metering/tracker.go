// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package metering tracks usage (builds, minutes, storage) and reports to Stripe.
package metering

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// Tracker records usage events into the usage_records table.
type Tracker struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewTracker creates a usage tracker.
func NewTracker(db *sql.DB, logger *slog.Logger) *Tracker {
	return &Tracker{db: db, logger: logger}
}

// RecordBuildCompletion increments build count and build minutes for a tenant.
// Called atomically within the build status update transaction.
func (t *Tracker) RecordBuildCompletion(tenantID string, durationSeconds int) error {
	if t.db == nil {
		return nil
	}

	now := time.Now().UTC()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0)

	tx, err := t.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit — standard Go tx pattern

	// Increment builds count
	if err := t.upsertUsage(tx, tenantID, "builds", 1, periodStart, periodEnd); err != nil {
		return err
	}

	// Record build minutes (round up to nearest minute)
	minutes := (durationSeconds + 59) / 60
	if minutes < 1 {
		minutes = 1
	}
	if err := t.upsertUsage(tx, tenantID, "build_minutes", minutes, periodStart, periodEnd); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	t.logger.Debug("usage recorded",
		"tenant", tenantID,
		"builds", 1,
		"minutes", minutes,
	)
	return nil
}

// RecordStorageSnapshot records the current storage usage for a tenant.
// Called by the storage metering cron job.
func (t *Tracker) RecordStorageSnapshot(tenantID string, storageBytes int64) error {
	if t.db == nil {
		return nil
	}

	now := time.Now().UTC()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0)

	// Storage is a snapshot (not incremental) — use the latest value
	storageMB := int(storageBytes / (1024 * 1024))

	_, err := t.db.Exec(
		`INSERT INTO usage_records (id, tenant_id, metric, quantity, period_start, period_end, reported_to_stripe)
		 VALUES ($1, $2, 'storage_mb', $3, $4, $5, false)
		 ON CONFLICT (tenant_id, metric, period_start)
		 DO UPDATE SET quantity = $3, reported_to_stripe = false`,
		fmt.Sprintf("usage-%s-storage-%s", tenantID, periodStart.Format("2006-01")),
		tenantID, storageMB, periodStart, periodEnd,
	)
	return err
}

// GetUsageSummary returns current usage for a tenant in the current billing period.
func (t *Tracker) GetUsageSummary(tenantID string) (map[string]int, error) {
	if t.db == nil {
		return map[string]int{}, nil
	}

	now := time.Now().UTC()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	rows, err := t.db.Query(
		"SELECT metric, quantity FROM usage_records WHERE tenant_id = $1 AND period_start = $2",
		tenantID, periodStart,
	)
	if err != nil {
		return nil, fmt.Errorf("query usage: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]int)
	for rows.Next() {
		var metric string
		var qty int
		if err := rows.Scan(&metric, &qty); err != nil {
			continue
		}
		result[metric] = qty
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate usage: %w", err)
	}
	return result, nil
}

func (t *Tracker) upsertUsage(tx *sql.Tx, tenantID, metric string, increment int, periodStart, periodEnd time.Time) error {
	id := fmt.Sprintf("usage-%s-%s-%s", tenantID, metric, periodStart.Format("2006-01"))
	_, err := tx.Exec(
		`INSERT INTO usage_records (id, tenant_id, metric, quantity, period_start, period_end, reported_to_stripe)
		 VALUES ($1, $2, $3, $4, $5, $6, false)
		 ON CONFLICT (tenant_id, metric, period_start)
		 DO UPDATE SET quantity = usage_records.quantity + $4, reported_to_stripe = false`,
		id, tenantID, metric, increment, periodStart, periodEnd,
	)
	if err != nil {
		return fmt.Errorf("upsert usage %s: %w", metric, err)
	}
	return nil
}
