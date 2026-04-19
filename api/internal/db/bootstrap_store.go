// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"fmt"

	"github.com/loopzero/loopzero/api/internal/placement"
	"github.com/loopzero/loopzero/api/internal/routes"
)

// PostgresBootstrapStore implements routes.BootstrapStore using PostgreSQL.
// All INSERTs use ON CONFLICT DO NOTHING for idempotency.
type PostgresBootstrapStore struct {
	DB DBTX
}

// Bootstrap creates the admin user, tenant, and tenant membership records.
// Returns (userID, tenantID, created, error).
// Uses ON CONFLICT DO NOTHING so duplicate calls are safe.
func (s *PostgresBootstrapStore) Bootstrap(ctx context.Context, req routes.BootstrapRequest) (string, string, bool, error) {
	userID := fmt.Sprintf("github-%s", req.AdminLogin)
	tenantID := fmt.Sprintf("tn-%s", req.TenantSlug)

	// MEET-053 D-01: Use the serializer transaction if available.
	// When AssignCell runs under the advisory lock serializer, it injects
	// the transaction into the context. Using it here ensures that the
	// count-and-pick (AssignCell) and the tenant INSERT execute in the same
	// transaction, fixing the TOCTOU race where two concurrent creates
	// could both read the same cell counts and both pick the same cell.
	db := s.DB
	if tx := placement.TxFromContext(ctx); tx != nil {
		db = &PgxTxAdapter{Tx: tx}
	}

	anyCreated := false

	// 1. INSERT user (ON CONFLICT by id → DO NOTHING)
	res, err := db.Exec(ctx,
		`INSERT INTO users (id, email, github_id, display_name)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (id) DO NOTHING`,
		userID,
		fmt.Sprintf("%s@users.noreply.github.com", req.AdminLogin),
		req.AdminGitHubID,
		req.AdminLogin,
	)
	if err != nil {
		return "", "", false, fmt.Errorf("bootstrap: insert user: %w", err)
	}
	if res.RowsAffected > 0 {
		anyCreated = true
	}

	// 2. INSERT tenant (ON CONFLICT by id → DO NOTHING)
	displayName := req.TenantDisplayName
	if displayName == "" {
		displayName = req.TenantSlug
	}
	// ADR-0008: cell_id assigned by PlacementEngine via BootstrapRequest.
	// Phase 0: always "cell-default". Phase 1+: PlacementEngine provides real cell_id.
	cellID := req.CellID
	if cellID == "" {
		cellID = "cell-default"
	}
	res, err = db.Exec(ctx,
		`INSERT INTO tenants (id, display_name, slug, owner_id, cell_id)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (id) DO NOTHING`,
		tenantID,
		displayName,
		req.TenantSlug, // slug is NOT NULL since migration 000017
		userID,
		cellID,
	)
	if err != nil {
		return "", "", false, fmt.Errorf("bootstrap: insert tenant: %w", err)
	}
	if res.RowsAffected > 0 {
		anyCreated = true
	}

	// 3. INSERT tenant membership (ON CONFLICT by (tenant_id, user_id) → DO NOTHING)
	res, err = db.Exec(ctx,
		`INSERT INTO tenant_members (tenant_id, user_id, role)
		 VALUES ($1, $2, 'owner')
		 ON CONFLICT (tenant_id, user_id) DO NOTHING`,
		tenantID,
		userID,
	)
	if err != nil {
		return "", "", false, fmt.Errorf("bootstrap: insert tenant member: %w", err)
	}
	if res.RowsAffected > 0 {
		anyCreated = true
	}

	// 4. INSERT subscription (§1.1: subscriptions is single source of truth for plan).
	// Plan comes from BootstrapRequest.Plan (defaults to "starter").
	plan := req.Plan
	if plan == "" {
		plan = "starter"
	}
	subID := fmt.Sprintf("sub_bootstrap_%s", tenantID)
	res, err = db.Exec(ctx,
		`INSERT INTO subscriptions (id, tenant_id, plan, status, created_at, updated_at)
		 VALUES ($1, $2, $3, 'active', now(), now())
		 ON CONFLICT (tenant_id) DO NOTHING`,
		subID, tenantID, plan,
	)
	if err != nil {
		return "", "", false, fmt.Errorf("bootstrap: insert subscription: %w", err)
	}
	if res.RowsAffected > 0 {
		anyCreated = true
	}

	return userID, tenantID, anyCreated, nil
}
