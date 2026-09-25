// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// bootstrap_wrapper.go — sqlc-backed implementation of
// PostgresBootstrapStore (struct in this same file, no separate
// _types.go — bootstrap is small).
//
// T2.2 (Batch B #3) of plan 2026-05-29-sqlc-migration-plan
// v1.3.
//
// The 1 prod call site (api/cmd/setup_stores.go:90) uses struct-
// literal `&db.PostgresBootstrapStore{DB: dbAdapter}` — preserved.
//
// MEET-053 D-01 SERIALIZABLE-via-dbtx.TxFromContext pattern preserved:
// when placement runs AssignCell under the advisory lock serializer,
// it injects the transaction into the context. The wrapper uses
// TxFromContext to pick up that transaction so count-and-pick and
// the tenant INSERT execute in the same transaction (fixes TOCTOU
// race where two concurrent creates could both read the same cell
// counts and both pick the same cell).

import (
	"context"
	"fmt"
	"strconv"

	"github.com/getloopscale/loopzero/api/internal/db"

	"github.com/getloopscale/loopzero/api/internal/db/dbtx"
	"github.com/getloopscale/loopzero/api/internal/db/generated"
	"github.com/getloopscale/loopzero/api/internal/ports"
)

// PostgresBootstrapStore implements ports.BootstrapStore using
// PostgreSQL via sqlc. All INSERTs use ON CONFLICT DO NOTHING for
// idempotency; the wrapper accumulates the per-statement
// rows-affected so `created=true` reflects whether ANY of the 4
// records were inserted on this call.
type PostgresBootstrapStore struct {
	DB db.DBTX
}

// Bootstrap creates the admin user, tenant, tenant membership, and
// subscription records. Returns (userID, tenantID, created, error).
// Repeated calls are safe — each INSERT is ON CONFLICT DO NOTHING.
//
// Default values applied wrapper-side (legacy contract):
//   - cellID  → "cell-0" when BootstrapRequest.CellID empty
//   - plan    → "starter"      when BootstrapRequest.Plan empty
//   - display → tenant slug    when BootstrapRequest.TenantDisplayName empty
//   - email   → "{login}@users.noreply.github.com" (GitHub convention)
func (s *PostgresBootstrapStore) Bootstrap(ctx context.Context, req ports.BootstrapRequest) (string, string, bool, error) {
	userID := fmt.Sprintf("github-%s", req.AdminLogin)
	tenantID := fmt.Sprintf("tn-%s", req.TenantSlug)

	// MEET-053 D-01: Use the serializer transaction if available.
	// When AssignCell runs under the advisory lock serializer, it
	// injects the transaction into the context. Using it here ensures
	// count-and-pick + INSERT run in the same transaction.
	conn := s.DB
	if tx := dbtx.TxFromContext(ctx); tx != nil {
		conn = tx
	}

	gdb := db.Generated(conn)
	q := generated.New()

	anyCreated := false

	// 1. INSERT user (ON CONFLICT by id → DO NOTHING)
	// AdminGitHubID is a string in ports.BootstrapRequest; sqlc emits
	// users.github_id as *int64 (schema: BIGINT NULL). Parse to int64;
	// non-numeric / empty values land as NULL (caller-visible record
	// is identical to legacy which passed the string directly to
	// libpq for implicit coercion).
	var githubIDPtr *int64
	if n, err := strconv.ParseInt(req.AdminGitHubID, 10, 64); err == nil {
		githubIDPtr = &n
	}
	n, err := q.InsertBootstrapUser(ctx, gdb, generated.InsertBootstrapUserParams{
		ID:          userID,
		Username:    req.AdminLogin,
		Email:       fmt.Sprintf("%s@users.noreply.github.com", req.AdminLogin),
		GithubID:    githubIDPtr,
		DisplayName: req.AdminLogin,
	})
	if err != nil {
		return "", "", false, fmt.Errorf("bootstrap: insert user: %w", err)
	}
	if n > 0 {
		anyCreated = true
	}

	// 2. INSERT tenant (ON CONFLICT by id → DO NOTHING)
	displayName := req.TenantDisplayName
	if displayName == "" {
		displayName = req.TenantSlug
	}
	// ADR-0008: cell_id assigned by PlacementEngine via BootstrapRequest.
	// Phase 0: always "cell-0". Phase 1+: PlacementEngine provides
	// real cell_id.
	cellID := req.CellID
	if cellID == "" {
		cellID = "cell-0"
	}
	n, err = q.InsertBootstrapTenant(ctx, gdb, generated.InsertBootstrapTenantParams{
		ID:          tenantID,
		DisplayName: displayName,
		Slug:        req.TenantSlug,
		OwnerID:     userID,
		CellID:      cellID,
	})
	if err != nil {
		return "", "", false, fmt.Errorf("bootstrap: insert tenant: %w", err)
	}
	if n > 0 {
		anyCreated = true
	}

	// 3. INSERT tenant membership (ON CONFLICT by (tenant_id, user_id) → DO NOTHING)
	n, err = q.InsertBootstrapTenantMember(ctx, gdb, generated.InsertBootstrapTenantMemberParams{
		TenantID: tenantID,
		UserID:   userID,
	})
	if err != nil {
		return "", "", false, fmt.Errorf("bootstrap: insert tenant member: %w", err)
	}
	if n > 0 {
		anyCreated = true
	}

	// 4. INSERT subscription (§1.1: subscriptions is single source of
	// truth for plan).
	plan := req.Plan
	if plan == "" {
		plan = "starter"
	}
	subID := fmt.Sprintf("sub_bootstrap_%s", tenantID)
	n, err = q.InsertBootstrapSubscription(ctx, gdb, generated.InsertBootstrapSubscriptionParams{
		ID:       subID,
		TenantID: tenantID,
		Plan:     plan,
	})
	if err != nil {
		return "", "", false, fmt.Errorf("bootstrap: insert subscription: %w", err)
	}
	if n > 0 {
		anyCreated = true
	}

	return userID, tenantID, anyCreated, nil
}
