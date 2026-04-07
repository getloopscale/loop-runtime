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
	logger        *slog.Logger
}

// NewWebhookHandler creates a webhook event processor.
func NewWebhookHandler(db *sql.DB, webhookSecret string, logger *slog.Logger) *WebhookHandler {
	return &WebhookHandler{db: db, webhookSecret: webhookSecret, logger: logger}
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
func (h *WebhookHandler) ProcessEvent(event StripeEvent) error {
	if h.db == nil {
		h.logger.Info("webhook received (no DB, skipping)", "type", event.Type, "id", event.ID)
		return nil
	}

	// Idempotency: check if event already processed
	var processed bool
	_ = h.db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM webhook_events WHERE event_id = $1)",
		event.ID,
	).Scan(&processed)
	if processed {
		h.logger.Debug("webhook event already processed", "id", event.ID)
		return nil
	}

	var err error
	switch event.Type {
	case "checkout.session.completed":
		err = h.handleCheckoutCompleted(event)
	case "customer.subscription.updated":
		err = h.handleSubscriptionUpdated(event)
	case "customer.subscription.deleted":
		err = h.handleSubscriptionDeleted(event)
	case "invoice.paid":
		err = h.handleInvoicePaid(event)
	case "invoice.payment_failed":
		err = h.handlePaymentFailed(event)
	default:
		h.logger.Debug("unhandled webhook event type", "type", event.Type)
		return nil
	}

	if err != nil {
		return err
	}

	// Mark event as processed (idempotency)
	_, _ = h.db.Exec(
		"INSERT INTO webhook_events (event_id, event_type, processed_at) VALUES ($1, $2, now()) ON CONFLICT DO NOTHING",
		event.ID, event.Type,
	)

	h.logger.Info("webhook event processed", "type", event.Type, "id", event.ID)
	return nil
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

	// Link the Stripe subscription ID to our DB row and upgrade plan.
	// The plan name is derived from the price — for now, any paid subscription = "pro".
	// The subscription.updated webhook will set the precise plan later.
	_, err := h.db.Exec(
		`UPDATE subscriptions
		 SET stripe_subscription_id = $1, plan = 'pro', status = 'active', updated_at = now()
		 WHERE stripe_customer_id = $2`,
		session.Subscription, session.Customer,
	)
	if err != nil {
		return fmt.Errorf("link subscription: %w", err)
	}

	h.logger.Info("checkout completed — subscription linked",
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
	}
	if err := json.Unmarshal(data.Object, &sub); err != nil {
		return fmt.Errorf("unmarshal subscription object: %w", err)
	}

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
