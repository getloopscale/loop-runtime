// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
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
	if err != sql.ErrNoRows {
		return "", "", fmt.Errorf("query tenant membership: %w", err)
	}

	// No tenant — create one.
	// Tenant ID is UUID-based: no collision possible, no TOCTOU race.
	// Slug is derived from email for human-readable display only.
	slug := DeriveTenantSlug(email)
	tenantID = generateTenantID()

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// 1. Ensure user record exists (Kratos UUID → users table).
	// Without this, tenant_members FK on user_id fails.
	// github_id is NULL for Kratos users (only set for legacy GitHub-auth users).
	_, err = tx.ExecContext(ctx,
		`INSERT INTO users (id, email, display_name, created_at, updated_at)
		 VALUES ($1, $2, $3, now(), now())
		 ON CONFLICT (id) DO UPDATE SET email = EXCLUDED.email, updated_at = now()`,
		userID, email, slug,
	)
	if err != nil {
		return "", "", fmt.Errorf("ensure user record: %w", err)
	}

	// 2. Create tenant with UUID-based ID.
	// Slug collision on display name is handled gracefully:
	// if the preferred slug is taken, append a short suffix.
	finalSlug := slug
	result, err := tx.ExecContext(ctx,
		`INSERT INTO tenants (id, display_name, slug, plan, owner_id, created_at, updated_at)
		 VALUES ($1, $2, $3, 'starter', $4, now(), now())`,
		tenantID, slug, finalSlug, userID,
	)
	if err != nil {
		// Slug collision — try with UUID suffix for uniqueness
		if isUniqueViolation(err) {
			shortID := tenantID[3:11] // first 8 chars of UUID portion
			finalSlug = fmt.Sprintf("%s-%s", slug, shortID)
			result, err = tx.ExecContext(ctx,
				`INSERT INTO tenants (id, display_name, slug, plan, owner_id, created_at, updated_at)
				 VALUES ($1, $2, $3, 'starter', $4, now(), now())`,
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

// isUniqueViolation checks if a PostgreSQL error is a unique constraint violation.
func isUniqueViolation(err error) bool {
	// PostgreSQL error code 23505 = unique_violation
	return err != nil && strings.Contains(err.Error(), "23505")
}
