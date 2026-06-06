// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package memory_webhook_worker

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Delivery is one row's worth of dispatch context. Loaded from
// memory_webhook_deliveries JOIN memory_webhooks.
type Delivery struct {
	ID            string
	WebhookID     string
	URL           string
	Secret        sql.NullString // NULL = pre-delivery-plan webhook; skip signing per D1 consequence
	EventType     string
	Payload       []byte
	AttemptCount  int
}

// DispatchResult is the outcome of one HTTP attempt.
type DispatchResult struct {
	StatusCode    int    // 0 on transport error
	Body          string // truncated to 4 KiB
	ErrorMessage  string // populated on transport error
	Delivered     bool   // true iff 2xx
}

// maxResponseBodyForAudit caps how much of the consumer's response body we
// persist in the row. 4 KiB is enough to capture an error message; more
// would balloon the DB on misbehaving consumers.
const maxResponseBodyForAudit = 4 * 1024

// httpAttemptTimeout is the per-attempt timeout. Consumers that hang
// don't get to lock up worker slots.
const httpAttemptTimeout = 30 * time.Second

// Dispatch executes ONE HTTP attempt against the consumer endpoint.
// Returns the result for the caller to persist + schedule retry or
// dead_letter via DB updates.
//
// Per D1: signs with HMAC-SHA256 when the webhook has a secret. NULL
// secret = pre-delivery-plan webhook; signature header is omitted +
// a warning is logged.
// Per D5: emits stable `Idempotency-Key: delivery-{id}` so the consumer
// can dedupe retries of the same logical delivery.
func Dispatch(ctx context.Context, httpClient *http.Client, logger *slog.Logger, d *Delivery) DispatchResult {
	reqCtx, cancel := context.WithTimeout(ctx, httpAttemptTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, d.URL, bytes.NewReader(d.Payload))
	if err != nil {
		return DispatchResult{ErrorMessage: fmt.Sprintf("build request: %v", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "LoopZero-Webhook/1.0")
	// D5 — Idempotency-Key is stable across retries of the same logical delivery.
	req.Header.Set("Idempotency-Key", fmt.Sprintf("delivery-%s", d.ID))
	// D1 — Sign with HMAC-SHA256 when the webhook has a secret.
	if d.Secret.Valid && d.Secret.String != "" {
		ts := time.Now().Unix()
		sig := Sign(d.Secret.String, ts, d.Payload)
		req.Header.Set("X-LoopZero-Signature", HeaderValue(ts, sig))
	} else {
		logger.Warn("memory webhook delivery: NULL secret; skipping signing",
			"webhook_id", d.WebhookID,
			"delivery_id", d.ID,
		)
	}
	// Identify the event type so the consumer can route quickly.
	req.Header.Set("X-LoopZero-Event", d.EventType)

	resp, err := httpClient.Do(req)
	if err != nil {
		return DispatchResult{ErrorMessage: err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyForAudit))
	result := DispatchResult{
		StatusCode: resp.StatusCode,
		Body:       string(body),
		Delivered:  resp.StatusCode >= 200 && resp.StatusCode < 300,
	}
	if !result.Delivered {
		result.ErrorMessage = fmt.Sprintf("non-2xx status: %d", resp.StatusCode)
	}
	return result
}

// ErrNoRows is returned by ClaimDueDelivery when no row is currently due.
var ErrNoRows = errors.New("no due deliveries")
