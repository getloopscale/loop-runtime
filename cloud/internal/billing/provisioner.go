// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billing

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"log/slog"
)

// Provisioner handles Stripe customer and subscription provisioning for new users.
// Free tier (starter) is DB-only — no Stripe subscription created (MEET-038).
type Provisioner struct {
	db     *sql.DB
	stripe *StripeClient
	logger *slog.Logger
}

// NewProvisioner creates a billing provisioner.
func NewProvisioner(db *sql.DB, stripe *StripeClient, logger *slog.Logger) *Provisioner {
	return &Provisioner{db: db, stripe: stripe, logger: logger}
}

// ProvisionStarter creates a starter (free) subscription row in the DB.
// No Stripe customer or subscription is created for the free tier.
// Stripe customer is created lazily on first paid action (portal/upgrade).
func (p *Provisioner) ProvisionStarter(ctx context.Context, userID, email, tenantID string) error {
	if p.db == nil {
		return nil // Dev mode without DB
	}

	// Insert subscription row for starter plan (DB-only, no Stripe)
	id := generateSubscriptionID()
	_, err := p.db.ExecContext(ctx,
		`INSERT INTO subscriptions (id, tenant_id, plan, status, created_at, updated_at)
		 VALUES ($1, $2, 'starter', 'active', now(), now())
		 ON CONFLICT (tenant_id) DO NOTHING`,
		id, tenantID,
	)
	if err != nil {
		p.logger.Error("failed to provision starter subscription",
			"tenant_id", tenantID,
			"error", err,
		)
		return err
	}

	p.logger.Info("provisioned starter plan",
		"tenant_id", tenantID,
		"email", email,
	)
	return nil
}

// EnsureStripeCustomer lazily creates a Stripe customer if one doesn't exist.
// Called on first billing action (portal visit, upgrade).
// Returns the Stripe customer ID.
func (p *Provisioner) EnsureStripeCustomer(ctx context.Context, tenantID, email, name string) (string, error) {
	if p.db == nil || p.stripe == nil {
		return "", nil
	}

	// Check if customer already exists
	var customerID string
	err := p.db.QueryRowContext(ctx,
		"SELECT stripe_customer_id FROM subscriptions WHERE tenant_id = $1",
		tenantID,
	).Scan(&customerID)
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	if customerID != "" {
		return customerID, nil // Already has a Stripe customer
	}

	// Create Stripe customer (idempotent via Idempotency-Key in StripeClient)
	customer, err := p.stripe.CreateCustomer(email, tenantID, name)
	if err != nil {
		p.logger.Error("failed to create Stripe customer",
			"tenant_id", tenantID,
			"error", err,
		)
		return "", err
	}

	// Store customer ID
	_, err = p.db.ExecContext(ctx,
		"UPDATE subscriptions SET stripe_customer_id = $1, updated_at = now() WHERE tenant_id = $2",
		customer.ID, tenantID,
	)
	if err != nil {
		p.logger.Error("failed to store Stripe customer ID",
			"tenant_id", tenantID,
			"customer_id", customer.ID,
			"error", err,
		)
		return customer.ID, err // Return customer ID even if DB write fails
	}

	p.logger.Info("created Stripe customer",
		"tenant_id", tenantID,
		"customer_id", customer.ID,
	)
	return customer.ID, nil
}

func generateSubscriptionID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return fmt.Sprintf("sub_%x", b)
}
