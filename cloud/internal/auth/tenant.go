// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// TenantProvisioner automatically creates a tenant for new users on first signup.
// Tenant IDs are UUID-based (tn-{uuid}) to eliminate collision and TOCTOU races.
// The human-readable slug (derived from email) is stored separately for display.
// MEET-2026-04-07-039: B2 fix.
type TenantProvisioner struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewTenantProvisioner creates a provisioner for auto-creating tenants.
func NewTenantProvisioner(db *sql.DB, logger *slog.Logger) *TenantProvisioner {
	return &TenantProvisioner{db: db, logger: logger}
}

// EnsureTenant checks if the user has a tenant. If not, creates one with a UUID-based ID.
// Returns the tenant ID and role. The operation is idempotent — safe to call on every login.
func (p *TenantProvisioner) EnsureTenant(ctx context.Context, userID, email string) (tenantID string, role string, err error) {
	if p.db == nil {
		return "dev-tenant", "owner", nil // Dev mode without DB
	}

	// Fast path: check if user already has a tenant membership.
	// This is the 99% case for returning users — one SELECT, no transaction.
	err = p.db.QueryRowContext(ctx,
		"SELECT t.id, tm.role FROM tenants t JOIN tenant_members tm ON t.id = tm.tenant_id WHERE tm.user_id = $1 LIMIT 1",
		userID,
	).Scan(&tenantID, &role)
	if err == nil {
		return tenantID, role, nil // Already has a tenant
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", "", fmt.Errorf("query tenant membership: %w", err)
	}

	// No tenant — create one.
	slug := DeriveTenantSlug(email)

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Serialize concurrent tenant creation for the same user using an advisory lock.
	// pg_advisory_xact_lock(hashtext(userID)) ensures only one transaction at a time
	// can proceed past this point for a given user. Released automatically on commit/rollback.
	_, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", userID)
	if err != nil {
		return "", "", fmt.Errorf("advisory lock: %w", err)
	}

	// Re-check membership inside the lock. If a concurrent request already created
	// a tenant while we were waiting for the lock, return it instead of creating a duplicate.
	err = tx.QueryRowContext(ctx,
		"SELECT t.id, tm.role FROM tenants t JOIN tenant_members tm ON t.id = tm.tenant_id WHERE tm.user_id = $1 LIMIT 1",
		userID,
	).Scan(&tenantID, &role)
	if err == nil {
		return tenantID, role, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", "", fmt.Errorf("re-check tenant membership: %w", err)
	}

	// Tenant ID is UUID-based: no collision possible, no TOCTOU race.
	// Slug is derived from email for human-readable display only.
	tenantID = generateTenantID()

	// 1. Ensure user record exists (Kratos UUID → users table).
	// Without this, tenant_members FK on user_id fails.
	// github_id is NULL for Kratos users (only set for legacy GitHub-auth users).
	// username uses the Kratos UUID for guaranteed uniqueness (slug collides on common locals).
	_, err = tx.ExecContext(ctx,
		`INSERT INTO users (id, email, display_name, username, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, now(), now())
		 ON CONFLICT (id) DO UPDATE SET email = EXCLUDED.email, updated_at = now()`,
		userID, email, slug, userID,
	)
	if err != nil {
		return "", "", fmt.Errorf("ensure user record: %w", err)
	}

	// 2. Create tenant with UUID-based ID.
	// Plan is NOT stored on tenants — subscriptions table is the single source of truth (§1.1).
	// Slug collision on display name is handled gracefully.
	finalSlug := slug
	result, err := tx.ExecContext(ctx,
		`INSERT INTO tenants (id, display_name, slug, owner_id, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, now(), now())`,
		tenantID, slug, finalSlug, userID,
	)
	if err != nil {
		// Slug collision — try with UUID suffix for uniqueness
		if isUniqueViolation(err) {
			shortID := tenantID[3:11] // first 8 chars of UUID portion
			finalSlug = fmt.Sprintf("%s-%s", slug, shortID)
			result, err = tx.ExecContext(ctx,
				`INSERT INTO tenants (id, display_name, slug, owner_id, created_at, updated_at)
				 VALUES ($1, $2, $3, $4, now(), now())`,
				tenantID, slug, finalSlug, userID,
			)
			if err != nil {
				return "", "", fmt.Errorf("create tenant (retry): %w", err)
			}
		} else {
			return "", "", fmt.Errorf("create tenant: %w", err)
		}
	}

	// Verify the tenant was actually created by this transaction.
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return "", "", fmt.Errorf("tenant creation failed: no rows inserted")
	}

	// 3. Create membership — this user is the owner.
	_, err = tx.ExecContext(ctx,
		`INSERT INTO tenant_members (tenant_id, user_id, role, created_at, updated_at)
		 VALUES ($1, $2, 'owner', now(), now())
		 ON CONFLICT (tenant_id, user_id) DO NOTHING`,
		tenantID, userID,
	)
	if err != nil {
		return "", "", fmt.Errorf("create membership: %w", err)
	}

	// 4. Create starter subscription (§1.1: subscriptions is single source of truth for plan).
	subID := generateSubscriptionID()
	_, err = tx.ExecContext(ctx,
		`INSERT INTO subscriptions (id, tenant_id, plan, status, created_at, updated_at)
		 VALUES ($1, $2, 'starter', 'active', now(), now())
		 ON CONFLICT (tenant_id) DO NOTHING`,
		subID, tenantID,
	)
	if err != nil {
		return "", "", fmt.Errorf("create subscription: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", "", fmt.Errorf("commit: %w", err)
	}

	p.logger.Info("auto-provisioned tenant for new user",
		"tenant_id", tenantID,
		"slug", finalSlug,
		"user_id", userID,
		"email", email,
		"plan", "starter",
	)
	return tenantID, "owner", nil
}

// GetRole resolves the authoritative role for a user from tenant_members.
// Returns "" (no role → access denied by RequireRole) when the user has no
// membership. In dev mode (no DB) it returns "owner" to mirror EnsureTenant.
// Per S2.3 this is the single source of truth for authorization — never the token.
func (p *TenantProvisioner) GetRole(ctx context.Context, userID string) (string, error) {
	if p.db == nil {
		return "owner", nil // Dev mode without DB
	}
	var role string
	err := p.db.QueryRowContext(ctx,
		"SELECT role FROM tenant_members WHERE user_id = $1 LIMIT 1",
		userID,
	).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil // No membership — no role, RequireRole will deny
	}
	if err != nil {
		return "", fmt.Errorf("resolve role: %w", err)
	}
	return role, nil
}

// DeriveTenantSlug creates a URL-safe slug from an email address.
// Used for human-readable display only — NOT for tenant identity.
// "paulo@example.com" → "paulo"
// "john.doe@company.com" → "john-doe"
func DeriveTenantSlug(email string) string {
	parts := strings.SplitN(email, "@", 2)
	local := parts[0]

	// Replace dots and underscores with hyphens
	slug := strings.NewReplacer(".", "-", "_", "-").Replace(local)

	// Remove non-alphanumeric characters (except hyphens)
	re := regexp.MustCompile(`[^a-z0-9-]`)
	slug = re.ReplaceAllString(strings.ToLower(slug), "")

	// Trim leading/trailing hyphens and collapse multiple hyphens
	slug = regexp.MustCompile(`-+`).ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")

	if slug == "" {
		slug = "user"
	}

	// Max length 60 (K8s label limit with room for suffix)
	if len(slug) > 60 {
		slug = slug[:60]
	}

	return slug
}

// generateTenantID creates a collision-free tenant ID using crypto/rand.
// Format: tn-{16 hex chars} (e.g., "tn-a1b2c3d4e5f67890")
func generateTenantID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback to less random but still unique (should never happen)
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return fmt.Sprintf("tn-%x", b)
}

// generateSubscriptionID creates a unique subscription ID.
func generateSubscriptionID() string {
	b := make([]byte, 12)
	// Mirror generateTenantID: a crypto/rand failure must NOT silently produce a
	// zeroed (collision-prone) ID (MEDIUM-001). It should never happen in practice.
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return fmt.Sprintf("sub_%x", b)
}

// isUniqueViolation checks if a PostgreSQL error is a unique constraint violation.
// Uses the pgx typed error (SQLSTATE 23505). String matching alone is unreliable,
// so the typed check is preferred with a message-content fallback.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505" // unique_violation
	}
	// Fallback for drivers that don't surface a typed error: check message content.
	return err != nil && strings.Contains(err.Error(), "unique constraint")
}
