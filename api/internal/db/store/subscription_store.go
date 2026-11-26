// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/generated"
)

// SubscriptionStore reads a tenant's billing subscription rows. The plan a
// tenant is on is NOT read here: that is pkgdomain.ResolvePlanTier over the
// enforcement limits store, the one reading every plan gate shares.
type SubscriptionStore struct {
	DB db.DBTX
}

// NewSubscriptionStore constructs a SubscriptionStore.
func NewSubscriptionStore(dbtx db.DBTX) *SubscriptionStore {
	return &SubscriptionStore{DB: dbtx}
}

// StripeCustomerID returns the billing customer id of the tenant, or ("", nil)
// when the tenant has no subscription carrying one.
func (s *SubscriptionStore) StripeCustomerID(ctx context.Context, tenantID string) (string, error) {
	bridge, ok := s.DB.(db.SqlcBridge)
	if !ok {
		return "", fmt.Errorf("subscription store: DBTX %T does not implement SqlcBridge", s.DB)
	}
	id, err := generated.New().GetStripeCustomerIDForTenant(ctx, bridge.GeneratedDB(), tenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("subscription store: stripe customer for tenant %q: %w", tenantID, err)
	}
	return id, nil
}
