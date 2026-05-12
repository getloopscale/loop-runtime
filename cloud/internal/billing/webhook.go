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

	// Process the event
	var processErr error
	switch event.Type {
	case "checkout.session.completed":
		processErr = h.handleCheckoutCompleted(event)
	case "customer.subscription.updated":
		processErr = h.handleSubscriptionUpdated(event)
	case "customer.subscription.deleted":
		processErr = h.handleSubscriptionDeleted(event)
	case "invoice.paid":
		processErr = h.handleInvoicePaid(event)
	case "invoice.payment_failed":
		processErr = h.handlePaymentFailed(event)
	default:
		h.logger.Debug("unhandled webhook event type", "type", event.Type)
		return nil
	}

	// Record event with status (success or failure)
	status := "processed"
	var lastError *string
	if processErr != nil {
		status = "failed"
		errStr := processErr.Error()
		lastError = &errStr
	}

	if _, err := h.db.Exec(
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

	if processErr != nil {
		return processErr
	}

	h.logger.Info("webhook event processed", "type", event.Type, "id", event.ID)
	return nil
}

// hashPayload computes a SHA256 hash of the event data for audit trail.
func hashPayload(data json.RawMessage) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:16]) // 32-char hex
}

func (h *WebhookHandler) handleCheckoutCompleted(event StripeEvent) error {
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

	// Audit gap M10 #24 fix: do NOT set `plan` here. The checkout.session.completed
	// event payload does not carry the price_id; the previous implementation
	// defaulted to "pro" "until subscription.updated arrives", which is fail-OPEN:
	// if subscription.updated is delayed, lost, or never arrives (network
	// partition, attacker-induced race, Stripe outage), the tenant ends up at
	// Pro tier without having paid for it. Plan tier MUST only be derived from
	// a confirmed price_id in subscription.updated.
	//
	// We link the subscription ID + flip status=active (so /api/v1/billing
	// surfaces that the customer is mid-flow) but leave `plan` at its prior
	// value (typically `starter` from initial provisioning). subscription.updated
	// is the authoritative path that maps price_id → plan tier.
	_, err := h.db.Exec(
		`UPDATE subscriptions
		 SET stripe_subscription_id = $1, status = 'active', updated_at = now()
		 WHERE stripe_customer_id = $2`,
		session.Subscription, session.Customer,
	)
	if err != nil {
		return fmt.Errorf("link subscription: %w", err)
	}

	h.logger.Info("checkout completed — subscription linked (plan pending subscription.updated)",
		"customer", session.Customer,
		"subscription", session.Subscription,
	)
	return nil
}

func (h *WebhookHandler) handleSubscriptionUpdated(event StripeEvent) error {
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
	if len(sub.Items.Data) > 0 {
		priceID := sub.Items.Data[0].Price.ID
		plan = h.planConfig.PlanForPriceID(priceID)
	}

	if plan != "" {
		// Sync status AND plan
		_, err := h.db.Exec(
			`UPDATE subscriptions SET status = $1, plan = $2, current_period_start = $3, current_period_end = $4, updated_at = now()
			 WHERE stripe_subscription_id = $5`,
			sub.Status, plan,
			time.Unix(sub.CurrentPeriodStart, 0),
			time.Unix(sub.CurrentPeriodEnd, 0),
			sub.ID,
		)
		return err
	}

	// No price mapping — sync status only
	_, err := h.db.Exec(
		`UPDATE subscriptions SET status = $1, current_period_start = $2, current_period_end = $3, updated_at = now()
		 WHERE stripe_subscription_id = $4`,
		sub.Status,
		time.Unix(sub.CurrentPeriodStart, 0),
		time.Unix(sub.CurrentPeriodEnd, 0),
		sub.ID,
	)
	return err
}

func (h *WebhookHandler) handleSubscriptionDeleted(event StripeEvent) error {
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

	_, err := h.db.Exec(
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

	// Compute expected signature
	signedPayload := fmt.Sprintf("%s.%s", timestamp, string(payload))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signedPayload))
	expected := hex.EncodeToString(mac.Sum(nil))

	for _, sig := range signatures {
		if hmac.Equal([]byte(sig), []byte(expected)) {
			return nil
		}
	}
	return fmt.Errorf("signature mismatch")
}
