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

	// Claim a batch with FOR UPDATE ... SKIP LOCKED so two replicas (HPA / rolling
	// restart) never report the same usage rows concurrently (find_data_002 —
	// duplicate billing). The transaction is held across the Stripe calls; this is
	// acceptable for an hourly job bounded by LIMIT 100, and the deterministic
	// idempotency key (created_at) is the backstop against double-charge on retry.
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin reporter tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after a successful Commit

	type pending struct {
		id, tenantID, metric, itemID string
		quantity                     int
		createdAt                    time.Time
	}
	// Drain the cursor (scoped closure so rows.Close runs via defer) BEFORE
	// issuing further statements on the same tx — the metered API path below
	// reuses this connection.
	drain := func() ([]pending, error) {
		rows, err := tx.Query(
			`SELECT ur.id, ur.tenant_id, ur.metric, ur.quantity, ur.created_at,
			        COALESCE(s.stripe_subscription_item_id, '')
			 FROM usage_records ur
			 JOIN subscriptions s ON s.tenant_id = ur.tenant_id
			 WHERE ur.reported_to_stripe = false
			 AND ur.quantity > 0
			 ORDER BY ur.created_at ASC
			 LIMIT 100
			 FOR UPDATE OF ur SKIP LOCKED`,
		)
		if err != nil {
			return nil, fmt.Errorf("query pending records: %w", err)
		}
		defer func() { _ = rows.Close() }()

		var batch []pending
		for rows.Next() {
			var p pending
			if err := rows.Scan(&p.id, &p.tenantID, &p.metric, &p.quantity, &p.createdAt, &p.itemID); err != nil {
				continue
			}
			batch = append(batch, p)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate pending records: %w", err)
		}
		return batch, nil
	}
	batch, err := drain()
	if err != nil {
		return err
	}

	var reported, failed, skipped int
	for _, p := range batch {
		// The Stripe metered API needs the subscription ITEM id (si_xxx). If it is
		// not yet known (backfills on the next subscription.updated), skip with a
		// visible WARN rather than reporting against the wrong id (find_data_001).
		if p.itemID == "" {
			r.logger.Warn("skipping usage report — no stripe_subscription_item_id yet",
				"id", p.id, "tenant", p.tenantID, "metric", p.metric)
			skipped++
			continue
		}
		// Use the record's created_at (deterministic) as the usage timestamp so
		// the Stripe idempotency key is STABLE across replicas and retries.
		if err := r.stripe.ReportUsage(p.itemID, int64(p.quantity), p.createdAt.Unix()); err != nil {
			r.logger.Warn("failed to report usage to Stripe",
				"id", p.id, "tenant", p.tenantID, "metric", p.metric, "error", err)
			failed++
			continue
		}
		if _, err := tx.Exec(
			"UPDATE usage_records SET reported_to_stripe = true WHERE id = $1", p.id,
		); err != nil {
			r.logger.Warn("failed to mark usage as reported", "id", p.id, "error", err)
		}
		reported++
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit reporter tx: %w", err)
	}

	if reported > 0 || failed > 0 || skipped > 0 {
		r.logger.Info("usage reporting complete", "reported", reported, "failed", failed, "skipped", skipped)
	}
	return nil
}
