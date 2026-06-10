// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T2.7 of maturity-3-62-to-4-0-roadmap plan — Stripe Customer Portal SDK
// integration. Replaces the placeholder URL returned by
// POST /api/v1/billing/portal with a real customer-scoped Portal session.
//
// Design (DIP, CLAUDE.md Rule 13.5):
//   - routes.BillingHandler depends on PortalSessionCreator interface,
//     defined in this package alongside the concrete StripePortalSessionCreator.
//   - The interface lives in services/ so a future provider swap (Paddle,
//     Lemon Squeezy) is one new file + one setup_server wiring change.
//   - When STRIPE_SECRET_KEY env is unset, BillingHandler skips the SDK
//     call and falls back to the legacy placeholder URL (back-compat for
//     local dev + test environments without a Stripe key).
//
// Stripe SDK contract (stripe-go/v82 BillingPortalSessionParams):
//   - Customer (required): tenant's stripe_customer_id (cus_xxxx)
//   - ReturnURL (required): where customer lands after closing portal
//   - Params.Context: assigned directly (v82 dropped SetContext method
//     and made the field public)
// Returns &stripe.BillingPortalSession{URL: "https://billing.stripe.com/p/session/..."}
// with TTL = 30 minutes (one-time-use, Stripe-managed).

package services

import (
	"context"
	"fmt"

	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/billingportal/session"
)

// PortalSessionCreator is the port that BillingHandler depends on.
// Production wiring uses StripePortalSessionCreator (real SDK).
// Test wiring uses a fake from routes/billing_test.go.
type PortalSessionCreator interface {
	// CreateSession returns a one-time-use portal URL that the CLI
	// redirects the customer to. customerID is the Stripe customer
	// reference (cus_xxxx); returnURL is where Stripe sends the
	// customer when they close the portal tab.
	CreateSession(ctx context.Context, customerID, returnURL string) (string, error)
}

// StripePortalSessionCreator implements PortalSessionCreator via the
// Stripe Go SDK v82. Each CreateSession call mints a fresh 30-minute
// portal session bound to the customer.
//
// Concurrency: stripe.Key is package-global in stripe-go. We set it
// once at construction; multiple instances with different keys is
// undefined behavior (use one per process — production uses exactly one).
type StripePortalSessionCreator struct {
	apiKey string
}

// NewStripePortalSessionCreator constructs a creator with the given
// secret key. Test keys (sk_test_...) are accepted — Stripe test mode
// returns real session URLs that point at the test dashboard.
func NewStripePortalSessionCreator(apiKey string) *StripePortalSessionCreator {
	return &StripePortalSessionCreator{apiKey: apiKey}
}

// CreateSession calls stripe.billingPortal.sessions.New(). Errors from
// the Stripe API (rate limit, customer not found, network) are returned
// verbatim wrapped with context so the caller can map to a typed
// LoopZeroError.
//
// Important: Stripe SDK uses a package-global stripe.Key. We assign on
// every call (cheap pointer set) to be idempotent against future
// multi-key scenarios (unlikely but defensive).
func (s *StripePortalSessionCreator) CreateSession(ctx context.Context, customerID, returnURL string) (string, error) {
	if customerID == "" {
		return "", fmt.Errorf("stripe portal: empty customer ID")
	}
	if returnURL == "" {
		return "", fmt.Errorf("stripe portal: empty return URL")
	}

	stripe.Key = s.apiKey

	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(customerID),
		ReturnURL: stripe.String(returnURL),
	}
	params.Context = ctx

	sess, err := session.New(params)
	if err != nil {
		return "", fmt.Errorf("stripe portal session create: %w", err)
	}
	return sess.URL, nil
}
