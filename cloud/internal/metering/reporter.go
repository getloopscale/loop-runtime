// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package metering

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/loopzero/loopzero/cloud/internal/billing"
)

// Reporter sends unreported usage records to Stripe.
// Runs as a background cron job (hourly).
type Reporter struct {
	db     *sql.DB
	stripe *billing.StripeClient
	logger *slog.Logger
}

// NewReporter creates a Stripe usage reporter.
func NewReporter(db *sql.DB, stripe *billing.StripeClient, logger *slog.Logger) *Reporter {
	return &Reporter{db: db, stripe: stripe, logger: logger}
}

// ReportPending finds unreported usage records and sends them to Stripe.
// Idempotent: only processes records with reported_to_stripe = false.
// On success, marks them as reported. On failure, leaves them for retry.
func (r *Reporter) ReportPending() error {
	if r.db == nil || r.stripe == nil {
		return nil
	}

	rows, err := r.db.Query(
		`SELECT ur.id, ur.tenant_id, ur.metric, ur.quantity,
		        s.stripe_subscription_id
		 FROM usage_records ur
		 JOIN subscriptions s ON s.tenant_id = ur.tenant_id
		 WHERE ur.reported_to_stripe = false
		 AND ur.quantity > 0
		 ORDER BY ur.created_at ASC
		 LIMIT 100`,
	)
	if err != nil {
		return fmt.Errorf("query pending records: %w", err)
	}
	defer rows.Close()

	var reported, failed int
	for rows.Next() {
		var id, tenantID, metric, subID string
		var quantity int
		if err := rows.Scan(&id, &tenantID, &metric, &quantity, &subID); err != nil {
			continue
		}

		// Report to Stripe
		if err := r.stripe.ReportUsage(subID, int64(quantity), time.Now().Unix()); err != nil {
			r.logger.Warn("failed to report usage to Stripe",
				"id", id, "tenant", tenantID, "metric", metric, "error", err)
			failed++
			continue
		}

		// Mark as reported
		_, err := r.db.Exec(
			"UPDATE usage_records SET reported_to_stripe = true WHERE id = $1",
			id,
		)
		if err != nil {
			r.logger.Warn("failed to mark usage as reported", "id", id, "error", err)
		}
		reported++
	}

	if reported > 0 || failed > 0 {
		r.logger.Info("usage reporting complete", "reported", reported, "failed", failed)
	}
	return nil
}
