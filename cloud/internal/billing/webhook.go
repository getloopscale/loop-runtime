// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// WebhookHandler processes Stripe webhook events.
type WebhookHandler struct {
	db            *sql.DB
	webhookSecret string
	planConfig    PlanConfig
	logger        *slog.Logger
}

// dbExecer is the subset of database operations the event handlers need. Both
// *sql.DB and *sql.Tx satisfy it, so handlers can run inside a transaction.
type dbExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// NewWebhookHandler creates a webhook event processor.
func NewWebhookHandler(db *sql.DB, webhookSecret string, planConfig PlanConfig, logger *slog.Logger) *WebhookHandler {
	return &WebhookHandler{db: db, webhookSecret: webhookSecret, planConfig: planConfig, logger: logger}
}

// StripeEvent represents the top-level Stripe webhook event.
type StripeEvent struct {
	ID   string          `json:"id"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// StripeEventData wraps the object inside data.
type StripeEventData struct {
	Object json.RawMessage `json:"object"`
}

// WebhookSecret returns the configured webhook signing secret.
func (h *WebhookHandler) WebhookSecret() string { return h.webhookSecret }

// ProcessEvent handles a verified webhook event idempotently.
// §1.2: All DB errors are handled explicitly. Events are recorded with status.
func (h *WebhookHandler) ProcessEvent(event StripeEvent) error {
	if h.db == nil {
		h.logger.Info("webhook received (no DB, skipping)", "type", event.Type, "id", event.ID)
		return nil
	}

	// Idempotency: check if event already SUCCESSFULLY processed.
	// Failed events (status='failed') must be retryable when Stripe sends them again.
	var processedSuccessfully bool
	if err := h.db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM webhook_events WHERE event_id = $1 AND status = 'processed')",
		event.ID,
	).Scan(&processedSuccessfully); err != nil {
		h.logger.Error("webhook idempotency check failed", "event_id", event.ID, "error", err)
		return fmt.Errorf("idempotency check: %w", err)
	}
	if processedSuccessfully {
		h.logger.Debug("webhook event already processed successfully", "id", event.ID)
		return nil
	}

	// Compute payload hash for auditability
	payloadHash := hashPayload(event.Data)

	// Unhandled event types are recorded for audit but need no transaction.
	if !isHandledEvent(event.Type) {
		h.logger.Debug("unhandled webhook event type", "type", event.Type)
		h.recordEvent(h.db, event, "skipped", nil, payloadHash)
		return nil
	}

	// Process the business mutation and the event record ATOMICALLY. Stripe
	// delivers at-least-once; without a transaction a crash between the mutation
	// and the event record would let a retry re-apply the mutation.
	tx, err := h.db.Begin()
	if err != nil {
		return fmt.Errorf("begin webhook tx: %w", err)
	}

	processErr := h.dispatch(tx, event)
	if processErr != nil {
		_ = tx.Rollback()
		// Record the failure OUTSIDE the rolled-back tx so the attempt is audited
		// and the event stays retryable (status='failed', never 'processed').
		errStr := processErr.Error()
		h.recordEvent(h.db, event, "failed", &errStr, payloadHash)
		return processErr
	}

	// Success: record 'processed' in the SAME tx, then commit atomically.
	h.recordEvent(tx, event, "processed", nil, payloadHash)
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit webhook tx: %w", err)
	}

	h.logger.Info("webhook event processed", "type", event.Type, "id", event.ID)
	return nil
}

// dispatch routes an event to its handler, executing DB writes on exec (a tx).
func (h *WebhookHandler) dispatch(exec dbExecer, event StripeEvent) error {
	switch event.Type {
	case "checkout.session.completed":
		return h.handleCheckoutCompleted(exec, event)
	// 2026-06-01 fix: NEW subscriptions emit customer.subscription.created,
	// not .updated. Without handling .created, plan stays "pending" forever
	// on first-time signup (dashboard polls /v1/billing showing
	// "Activating your subscription..." in a loop). Reuses the .updated
	// handler since both carry the same Subscription object shape with
	// items[0].price.id.
	case "customer.subscription.created", "customer.subscription.updated":
		return h.handleSubscriptionUpdated(exec, event)
	case "customer.subscription.deleted":
		return h.handleSubscriptionDeleted(exec, event)
	case "invoice.paid":
		return h.handleInvoicePaid(event)
	case "invoice.payment_failed":
		return h.handlePaymentFailed(event)
	default:
		return nil
	}
}

// isHandledEvent reports whether the event type has a business handler.
func isHandledEvent(t string) bool {
	switch t {
	case "checkout.session.completed",
		"customer.subscription.created",
		"customer.subscription.updated",
		"customer.subscription.deleted",
		"invoice.paid", "invoice.payment_failed":
		return true
	default:
		return false
	}
}

// recordEvent upserts an audit row for a received webhook event on the given
// executor (db or tx). Recording failures are logged but never panic.
func (h *WebhookHandler) recordEvent(exec dbExecer, event StripeEvent, status string, lastError *string, payloadHash string) {
	if _, err := exec.Exec(
		`INSERT INTO webhook_events (event_id, event_type, status, last_error, payload_hash, received_at, processed_at)
		 VALUES ($1, $2, $3, $4, $5, now(), now())
		 ON CONFLICT (event_id) DO UPDATE SET
		   attempt_count = webhook_events.attempt_count + 1,
		   last_error = EXCLUDED.last_error,
		   status = EXCLUDED.status,
		   processed_at = now()`,
		event.ID, event.Type, status, lastError, payloadHash,
	); err != nil {
		h.logger.Error("failed to record webhook event", "event_id", event.ID, "error", err)
	}
}

// hashPayload computes a SHA256 hash of the event data for audit trail.
func hashPayload(data json.RawMessage) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]) // full SHA-256 (64-char hex) for the audit trail
}

func (h *WebhookHandler) handleCheckoutCompleted(exec dbExecer, event StripeEvent) error {
	var data StripeEventData
	if err := json.Unmarshal(event.Data, &data); err != nil {
		return fmt.Errorf("unmarshal checkout data: %w", err)
	}

	var session struct {
		Customer     string `json:"customer"`
		Subscription string `json:"subscription"`
		Mode         string `json:"mode"`
	}
	if err := json.Unmarshal(data.Object, &session); err != nil {
		return fmt.Errorf("unmarshal checkout session: %w", err)
	}

	if session.Mode != "subscription" || session.Subscription == "" {
		return nil // Not a subscription checkout
	}

	// Do NOT set `plan` here (find_data_006 / audit gap M10 #24). The
	// checkout.session.completed event does not carry a price_id; defaulting to
	// "pro" "until subscription.updated arrives" is fail-OPEN — if that follow-up
	// event is delayed, lost, or attacker-induced-raced, the tenant ends up at Pro
	// without having paid. Plan tier MUST only be derived from a confirmed
	// price_id in subscription.updated. Here we only link the subscription id +
	// flip status=active; the plan column retains its prior value (typically
	// `starter`). exec is the in-flight transaction (T3.1 atomic ProcessEvent).
	res, err := exec.Exec(
		`UPDATE subscriptions
		 SET stripe_subscription_id = $1, status = 'active', updated_at = now()
		 WHERE stripe_customer_id = $2`,
		session.Subscription, session.Customer,
	)
	if err != nil {
		return fmt.Errorf("link subscription: %w", err)
	}
	// If no subscription row matched, the customer paid but has no row to link —
	// return an error so Stripe retries instead of silently orphaning the plan.
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("link subscription: no subscription row for customer %s", session.Customer)
	}

	h.logger.Info("checkout completed — subscription linked (plan pending subscription.updated)",
		"customer", session.Customer,
		"subscription", session.Subscription,
	)
	return nil
}

func (h *WebhookHandler) handleSubscriptionUpdated(exec dbExecer, event StripeEvent) error {
	var data StripeEventData
	if err := json.Unmarshal(event.Data, &data); err != nil {
		return fmt.Errorf("unmarshal subscription data: %w", err)
	}

	var sub struct {
		ID                 string `json:"id"`
		Customer           string `json:"customer"`
		Status             string `json:"status"`
		CurrentPeriodStart int64  `json:"current_period_start"`
		CurrentPeriodEnd   int64  `json:"current_period_end"`
		Items              struct {
			Data []struct {
				ID    string `json:"id"` // subscription ITEM id (si_xxx) — required for metered usage reporting
				Price struct {
					ID string `json:"id"`
				} `json:"price"`
			} `json:"data"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data.Object, &sub); err != nil {
		return fmt.Errorf("unmarshal subscription object: %w", err)
	}

	// §1.3: Derive plan from price ID (handles upgrade/downgrade via Customer Portal)
	plan := ""
	itemID := ""
	if len(sub.Items.Data) > 0 {
		plan = h.planConfig.PlanForPriceID(sub.Items.Data[0].Price.ID)
		itemID = sub.Items.Data[0].ID // si_xxx — used by the metering reporter (find_data_001)
	}

	// WHERE matches by stripe_subscription_id OR stripe_customer_id. Stripe does
	// NOT guarantee ordering of webhook delivery: customer.subscription.created
	// can arrive BEFORE checkout.session.completed has had a chance to link
	// stripe_subscription_id onto the row. Matching by customer_id (which the
	// row already has from EnsureStripeCustomer at checkout creation time) makes
	// this handler idempotent across both arrival orders. We also SET
	// stripe_subscription_id so a .created arriving first links the id; a later
	// .updated remains a no-op for that column.
	//
	// COALESCE(NULLIF($,''), col) only overwrites stripe_subscription_item_id
	// when the event carries an item id; otherwise the existing value is kept.
	if plan != "" {
		_, err := exec.Exec(
			`UPDATE subscriptions SET status = $1, plan = $2,
			        stripe_subscription_id = $3,
			        stripe_subscription_item_id = COALESCE(NULLIF($4, ''), stripe_subscription_item_id),
			        current_period_start = $5, current_period_end = $6, updated_at = now()
			 WHERE stripe_subscription_id = $3 OR stripe_customer_id = $7`,
			sub.Status, plan, sub.ID, itemID,
			time.Unix(sub.CurrentPeriodStart, 0),
			time.Unix(sub.CurrentPeriodEnd, 0),
			sub.Customer,
		)
		return err
	}

	// No price mapping — sync status (and item id) only
	_, err := exec.Exec(
		`UPDATE subscriptions SET status = $1,
		        stripe_subscription_id = $2,
		        stripe_subscription_item_id = COALESCE(NULLIF($3, ''), stripe_subscription_item_id),
		        current_period_start = $4, current_period_end = $5, updated_at = now()
		 WHERE stripe_subscription_id = $2 OR stripe_customer_id = $6`,
		sub.Status, sub.ID, itemID,
		time.Unix(sub.CurrentPeriodStart, 0),
		time.Unix(sub.CurrentPeriodEnd, 0),
		sub.Customer,
	)
	return err
}

func (h *WebhookHandler) handleSubscriptionDeleted(exec dbExecer, event StripeEvent) error {
	var data StripeEventData
	if err := json.Unmarshal(event.Data, &data); err != nil {
		return err
	}
	var sub struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data.Object, &sub); err != nil {
		return err
	}

	_, err := exec.Exec(
		"UPDATE subscriptions SET status = 'canceled', updated_at = now() WHERE stripe_subscription_id = $1",
		sub.ID,
	)
	return err
}

func (h *WebhookHandler) handleInvoicePaid(event StripeEvent) error {
	h.logger.Info("invoice paid", "event_id", event.ID)
	return nil // Invoice tracking is informational — subscription.updated handles state
}

func (h *WebhookHandler) handlePaymentFailed(event StripeEvent) error {
	var data StripeEventData
	if err := json.Unmarshal(event.Data, &data); err != nil {
		return err
	}
	var invoice struct {
		Customer     string `json:"customer"`
		Subscription string `json:"subscription"`
	}
	if err := json.Unmarshal(data.Object, &invoice); err != nil {
		return err
	}

	h.logger.Warn("payment failed",
		"customer", invoice.Customer,
		"subscription", invoice.Subscription,
		"event_id", event.ID,
	)
	// Stripe handles retries and dunning. We log for visibility.
	// Subscription status change will arrive via subscription.updated event.
	return nil
}

// VerifySignature validates the Stripe-Signature header.
// Returns nil if valid, error if not.
func VerifySignature(payload []byte, sigHeader, secret string) error {
	if sigHeader == "" || secret == "" {
		return fmt.Errorf("missing signature or secret")
	}

	parts := strings.Split(sigHeader, ",")
	var timestamp string
	var signatures []string

	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			timestamp = kv[1]
		case "v1":
			signatures = append(signatures, kv[1])
		}
	}

	if timestamp == "" || len(signatures) == 0 {
		return fmt.Errorf("invalid signature format")
	}

	// Check timestamp freshness — Stripe recommends ≤300 seconds (5 minutes).
	// Tight tolerances cause legitimate events to be rejected due to network
	// latency, clock skew, and Stripe retry delays (MEET-038 security mandate).
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}
	eventTime := time.Unix(ts, 0)
	age := time.Since(eventTime)
	if age > 300*time.Second {
		return fmt.Errorf("timestamp too old")
	}
	if age < -300*time.Second {
		return fmt.Errorf("timestamp in the future")
	}

	// Compute expected signature and compare on raw bytes (decoded from hex).
	// Comparing raw bytes (not hex strings) is the idiomatic, constant-time
	// pattern and avoids any length artifact from malformed hex input.
	signedPayload := fmt.Sprintf("%s.%s", timestamp, string(payload))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signedPayload))
	expected := mac.Sum(nil)

	for _, sig := range signatures {
		sigRaw, err := hex.DecodeString(sig)
		if err != nil {
			continue // malformed hex signature — skip, do not leak via length
		}
		if hmac.Equal(sigRaw, expected) {
			return nil
		}
	}
	return fmt.Errorf("signature mismatch")
}
