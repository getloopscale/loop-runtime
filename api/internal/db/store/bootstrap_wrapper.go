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
// main builds it on the cross-tenant background pool (loopzero_worker): the
// tenant being created does not exist yet, so no tenant scope can be set
// on the session, and tenants/tenant_members/subscriptions are under
// fail-closed RLS (migration 000062).
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
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/pkg/domain"

	"github.com/getloopscale/loopzero/api/internal/db/dbtx"
	"github.com/getloopscale/loopzero/api/internal/db/generated"
	"github.com/getloopscale/loopzero/api/internal/ports"
)

// PostgresBootstrapStore implements ports.BootstrapStore using
// PostgreSQL via sqlc. Every write is idempotent (ON CONFLICT DO NOTHING);
// the wrapper accumulates the per-statement rows-affected so `created=true`
// reflects whether ANY record was inserted on this call.
type PostgresBootstrapStore struct {
	DB db.DBTX
}

// bootstrapIdentity is the user a bootstrap provisions: the explicit
// identity (a Kratos UUID) when the request names one, else the legacy
// GitHub identity github-<login>.
type bootstrapIdentity struct {
	userID, email, displayName string
	githubID                   *int64
}

func identityOf(req ports.BootstrapRequest) bootstrapIdentity {
	id := bootstrapIdentity{
		userID:      req.AdminUserID,
		email:       req.AdminEmail,
		displayName: req.AdminDisplayName,
	}
	if id.userID == "" {
		id.userID = fmt.Sprintf("github-%s", req.AdminLogin)
	}
	if id.email == "" {
		id.email = fmt.Sprintf("%s@users.noreply.github.com", req.AdminLogin)
	}
	if id.displayName == "" {
		id.displayName = req.AdminLogin
	}
	// users.github_id is BIGINT NULL: a non-numeric or empty id lands as NULL.
	if n, err := strconv.ParseInt(req.AdminGitHubID, 10, 64); err == nil {
		id.githubID = &n
	}
	return id
}

// Bootstrap creates the user, the Account it owns, the tenant (in the cell
// placement assigned), the owner membership and the Account subscription,
// and makes the tenant the user's active tenant when none is set. Returns
// (userID, tenantID, created, error). Repeated calls are safe.
//
// The tenant is resolved by slug: a slug the same identity already owns is
// reused (its id and cell are kept — placement is sticky), and a slug another
// identity owns is refused with ports.ErrTenantSlugTaken, never joined.
//
// Default values applied wrapper-side:
//   - cellID  → domain.DefaultCellID when BootstrapRequest.CellID empty
//   - plan    → "starter"      when BootstrapRequest.Plan empty
//   - display → tenant slug    when BootstrapRequest.TenantDisplayName empty
func (s *PostgresBootstrapStore) Bootstrap(ctx context.Context, req ports.BootstrapRequest) (string, string, bool, error) {
	// MEET-053 D-01: use the placement serializer's transaction when present,
	// so count-and-pick and every INSERT run in one transaction.
	conn := s.DB
	if tx := dbtx.TxFromContext(ctx); tx != nil {
		conn = tx
	}
	w := bootstrapWrites{ctx: ctx, gdb: db.Generated(conn), q: generated.New()}
	id := identityOf(req)
	plan := req.Plan
	if plan == "" {
		plan = "starter"
	}

	w.count("insert user", func() (int64, error) {
		return w.q.InsertBootstrapUser(ctx, w.gdb, generated.InsertBootstrapUserParams{
			ID: id.userID, Username: req.AdminLogin, Email: id.email, GithubID: id.githubID, DisplayName: id.displayName,
		})
	})
	w.count("insert account", func() (int64, error) {
		return w.q.InsertBootstrapAccount(ctx, w.gdb, generated.InsertBootstrapAccountParams{
			ID: "acct-" + id.userID, DisplayName: id.displayName, Plan: plan, OwnerID: id.userID,
		})
	})
	accountID := w.accountOf(id.userID)
	tenantID := w.tenant(req, id.userID, accountID)
	w.count("insert tenant member", func() (int64, error) {
		return w.q.InsertBootstrapTenantMember(ctx, w.gdb, generated.InsertBootstrapTenantMemberParams{
			TenantID: tenantID, UserID: id.userID,
		})
	})
	w.count("insert subscription", func() (int64, error) {
		return w.q.InsertBootstrapSubscription(ctx, w.gdb, generated.InsertBootstrapSubscriptionParams{
			ID: "sub_bootstrap_" + tenantID, TenantID: tenantID, AccountID: accountID, Plan: plan,
		})
	})
	if w.err == nil {
		if err := w.q.SetBootstrapActiveTenantIfUnset(ctx, w.gdb, generated.SetBootstrapActiveTenantIfUnsetParams{
			ID: id.userID, ActiveTenantID: &tenantID,
		}); err != nil {
			w.err = fmt.Errorf("bootstrap: set active tenant: %w", err)
		}
	}
	if w.err != nil {
		return "", "", false, w.err
	}
	return id.userID, tenantID, w.created, nil
}

// bootstrapWrites runs the bootstrap statements in order and stops at the
// first failure: every step after a failed one is skipped, and the failure is
// the error Bootstrap returns.
type bootstrapWrites struct {
	ctx     context.Context
	gdb     generated.DBTX
	q       *generated.Queries
	created bool
	err     error
}

func (w *bootstrapWrites) count(what string, insert func() (int64, error)) {
	if w.err != nil {
		return
	}
	n, err := insert()
	if err != nil {
		w.err = fmt.Errorf("bootstrap: %s: %w", what, err)
		return
	}
	if n > 0 {
		w.created = true
	}
}

func (w *bootstrapWrites) accountOf(ownerID string) string {
	if w.err != nil {
		return ""
	}
	accountID, err := w.q.GetBootstrapAccountIDByOwner(w.ctx, w.gdb, ownerID)
	if err != nil {
		w.err = fmt.Errorf("bootstrap: resolve account of %s: %w", ownerID, err)
	}
	return accountID
}

// tenant inserts the tenant tn-<slug> and resolves the slug to the tenant it
// names, refusing one the caller does not own.
func (w *bootstrapWrites) tenant(req ports.BootstrapRequest, ownerID, accountID string) string {
	if w.err != nil {
		return ""
	}
	displayName := req.TenantDisplayName
	if displayName == "" {
		displayName = req.TenantSlug
	}
	cellID := req.CellID
	if cellID == "" {
		cellID = domain.DefaultCellID
	}
	w.count("insert tenant", func() (int64, error) {
		return w.q.InsertBootstrapTenant(w.ctx, w.gdb, generated.InsertBootstrapTenantParams{
			ID: fmt.Sprintf("tn-%s", req.TenantSlug), DisplayName: displayName, Slug: req.TenantSlug,
			OwnerID: ownerID, CellID: cellID, AccountID: accountID,
		})
	})
	if w.err != nil {
		return ""
	}
	existing, err := w.q.GetBootstrapTenantBySlug(w.ctx, w.gdb, req.TenantSlug)
	if errors.Is(err, pgx.ErrNoRows) {
		// The insert conflicted on id with a tenant under another slug.
		w.err = fmt.Errorf("bootstrap: tenant slug %q: %w", req.TenantSlug, ports.ErrTenantSlugTaken)
		return ""
	}
	if err != nil {
		w.err = fmt.Errorf("bootstrap: resolve tenant slug %q: %w", req.TenantSlug, err)
		return ""
	}
	if existing.OwnerID != ownerID {
		w.err = fmt.Errorf("bootstrap: tenant slug %q: %w", req.TenantSlug, ports.ErrTenantSlugTaken)
		return ""
	}
	return existing.ID
}
