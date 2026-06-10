// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package memory_webhook_worker

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Defaults — overridable via Config when constructing Worker.
const (
	defaultPollInterval        = 5 * time.Second
	defaultMaxInFlight         = 10
	defaultClaimBatch          = 50
	defaultHTTPClientTimeout   = 35 * time.Second // > httpAttemptTimeout (30s) so the client doesn't kill mid-flight
)

// Config tunes the worker. Zero values fall back to defaults.
type Config struct {
	PollInterval    time.Duration
	MaxInFlight     int
	ClaimBatch      int
	HTTPClient      *http.Client
}

// Worker is the background dispatcher for memory_webhook_deliveries.
// Per D4: single goroutine + worker-pool semaphore with bounded
// concurrency. Started once at cloud service startup; shutdown via
// context cancellation.
type Worker struct {
	db       *sql.DB
	logger   *slog.Logger
	cfg      Config
	semaCh   chan struct{}
	inflight sync.WaitGroup
	httpc    *http.Client
}

// New constructs a Worker. Pass nil cfg for defaults.
func New(db *sql.DB, logger *slog.Logger, cfg *Config) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	c := Config{
		PollInterval:  defaultPollInterval,
		MaxInFlight:   defaultMaxInFlight,
		ClaimBatch:    defaultClaimBatch,
	}
	if cfg != nil {
		if cfg.PollInterval > 0 {
			c.PollInterval = cfg.PollInterval
		}
		if cfg.MaxInFlight > 0 {
			c.MaxInFlight = cfg.MaxInFlight
		}
		if cfg.ClaimBatch > 0 {
			c.ClaimBatch = cfg.ClaimBatch
		}
		if cfg.HTTPClient != nil {
			c.HTTPClient = cfg.HTTPClient
		}
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: defaultHTTPClientTimeout}
	}
	return &Worker{
		db:     db,
		logger: logger,
		cfg:    c,
		semaCh: make(chan struct{}, c.MaxInFlight),
		httpc:  c.HTTPClient,
	}
}

// Run enters the poll loop. Blocks until ctx is canceled, at which point
// it waits for in-flight dispatches to finish before returning.
func (w *Worker) Run(ctx context.Context) {
	w.logger.Info("memory webhook worker started",
		"poll_interval", w.cfg.PollInterval,
		"max_in_flight", w.cfg.MaxInFlight,
	)
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("memory webhook worker: draining")
			w.inflight.Wait()
			w.logger.Info("memory webhook worker stopped")
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

// tick claims a batch of due deliveries and dispatches each via a
// semaphore-bounded goroutine.
func (w *Worker) tick(ctx context.Context) {
	deliveries, err := w.claimDue(ctx, w.cfg.ClaimBatch)
	if err != nil {
		w.logger.Warn("memory webhook worker: claim failed", "error", err)
		return
	}
	for _, d := range deliveries {
		select {
		case <-ctx.Done():
			return
		case w.semaCh <- struct{}{}:
			w.inflight.Add(1)
			go w.dispatch(ctx, d)
		}
	}
}

// dispatch is the per-delivery goroutine. Releases the semaphore on exit.
func (w *Worker) dispatch(ctx context.Context, d *Delivery) {
	defer func() {
		<-w.semaCh
		w.inflight.Done()
	}()

	result := Dispatch(ctx, w.httpc, w.logger, d)
	w.recordResult(ctx, d, result)
}

// claimDue selects + marks `status='delivering'` on up to limit due rows
// in a single tx. Each row is loaded with its parent webhook's URL and
// secret. Atomicity ensures no double-dispatch even if multiple ticks
// race (single-replica deployment per D4 — multi-replica needs leader
// election in a future plan).
func (w *Worker) claimDue(ctx context.Context, limit int) ([]*Delivery, error) {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	const sel = `
		SELECT d.id, d.webhook_id, w.url, w.secret, d.event_type, d.payload, d.attempt_count
		FROM memory_webhook_deliveries d
		JOIN memory_webhooks w ON w.id = d.webhook_id
		WHERE d.status IN ('pending', 'failed')
		  AND (d.next_attempt_at IS NULL OR d.next_attempt_at <= NOW())
		  AND w.revoked_at IS NULL
		ORDER BY d.next_attempt_at NULLS FIRST
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`
	rows, err := tx.QueryContext(ctx, sel, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var deliveries []*Delivery
	for rows.Next() {
		d := &Delivery{}
		if err := rows.Scan(&d.ID, &d.WebhookID, &d.URL, &d.Secret, &d.EventType, &d.Payload, &d.AttemptCount); err != nil {
			return nil, err
		}
		deliveries = append(deliveries, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = rows.Close()

	// Mark each as delivering. Single UPDATE keeps the tx tight.
	for _, d := range deliveries {
		if _, err := tx.ExecContext(ctx,
			`UPDATE memory_webhook_deliveries SET status='delivering' WHERE id=$1`, d.ID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if len(deliveries) == 0 {
		return nil, nil
	}
	return deliveries, nil
}

// recordResult updates the delivery row based on the dispatch outcome.
func (w *Worker) recordResult(ctx context.Context, d *Delivery, r DispatchResult) {
	bg, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Ignore the inbound ctx for the audit write — if the worker is
	// shutting down, we still want the row updated.
	_ = ctx

	newAttemptCount := d.AttemptCount + 1
	if r.Delivered {
		_, err := w.db.ExecContext(bg,
			`UPDATE memory_webhook_deliveries
			 SET status='delivered',
			     attempt_count=$1,
			     last_response_status=$2,
			     last_response_body=$3,
			     last_error_message=NULL,
			     next_attempt_at=NULL,
			     completed_at=NOW()
			 WHERE id=$4`,
			newAttemptCount, r.StatusCode, r.Body, d.ID,
		)
		if err != nil {
			w.logger.Warn("memory webhook delivery: mark delivered failed", "error", err, "id", d.ID)
		}
		return
	}

	// Non-delivered branch.
	statusValue := sql.NullInt64{Int64: int64(r.StatusCode), Valid: r.StatusCode != 0}
	if IsTerminalAttempt(newAttemptCount) {
		// 9 attempts exhausted → dead_letter.
		_, err := w.db.ExecContext(bg,
			`UPDATE memory_webhook_deliveries
			 SET status='dead_letter',
			     attempt_count=$1,
			     last_response_status=$2,
			     last_response_body=$3,
			     last_error_message=$4,
			     next_attempt_at=NULL,
			     completed_at=NOW()
			 WHERE id=$5`,
			newAttemptCount, statusValue, r.Body, r.ErrorMessage, d.ID,
		)
		if err != nil {
			w.logger.Warn("memory webhook delivery: mark dead_letter failed", "error", err, "id", d.ID)
		}
		return
	}

	// Retry scheduled.
	delay := NextBackoff(newAttemptCount)
	nextAt := time.Now().Add(delay)
	_, err := w.db.ExecContext(bg,
		`UPDATE memory_webhook_deliveries
		 SET status='failed',
		     attempt_count=$1,
		     last_response_status=$2,
		     last_response_body=$3,
		     last_error_message=$4,
		     next_attempt_at=$5
		 WHERE id=$6`,
		newAttemptCount, statusValue, r.Body, r.ErrorMessage, nextAt, d.ID,
	)
	if err != nil {
		w.logger.Warn("memory webhook delivery: mark failed-retry failed", "error", err, "id", d.ID)
	}
}

// ErrAlreadyRunning is reserved for a future plan where Run is
// idempotent / re-entrant. v0.1 callers MUST start Run only once.
var ErrAlreadyRunning = errors.New("worker already running")
