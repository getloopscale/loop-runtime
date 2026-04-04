// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package auth

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

// TenantProvisioner automatically creates a tenant for new users on first signup.
// The tenant name is derived from the user's email (local part before @).
type TenantProvisioner struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewTenantProvisioner creates a provisioner for auto-creating tenants.
func NewTenantProvisioner(db *sql.DB, logger *slog.Logger) *TenantProvisioner {
	return &TenantProvisioner{db: db, logger: logger}
}

// EnsureTenant checks if the user has a tenant. If not, creates one.
// Returns the tenant ID (slug) and role.
func (p *TenantProvisioner) EnsureTenant(ctx context.Context, userID, email string) (tenantID string, role string, err error) {
	if p.db == nil {
		return "dev-tenant", "owner", nil // Dev mode without DB
	}

	// Check if user already has a tenant membership
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

	// No tenant — create one from email
	slug := DeriveTenantSlug(email)

	// Resolve conflicts (slug already taken by another user)
	finalSlug, err := p.resolveSlug(ctx, slug)
	if err != nil {
		return "", "", err
	}

	tenantID = fmt.Sprintf("tn-%s", finalSlug)

	// Create tenant + membership in a transaction
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		"INSERT INTO tenants (id, display_name, plan, owner_id, created_at, updated_at) VALUES ($1, $2, 'starter', $3, now(), now()) ON CONFLICT (id) DO NOTHING",
		tenantID, finalSlug, userID,
	)
	if err != nil {
		return "", "", fmt.Errorf("create tenant: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		"INSERT INTO tenant_members (tenant_id, user_id, role, created_at, updated_at) VALUES ($1, $2, 'owner', now(), now()) ON CONFLICT DO NOTHING",
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
		"user_id", userID,
		"email", email,
		"plan", "starter",
	)
	return tenantID, "owner", nil
}

// DeriveTenantSlug creates a URL-safe slug from an email address.
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

	// Max length 63 (K8s label limit)
	if len(slug) > 60 {
		slug = slug[:60]
	}

	return slug
}

func (p *TenantProvisioner) resolveSlug(ctx context.Context, slug string) (string, error) {
	// Check if slug is available
	var exists bool
	err := p.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM tenants WHERE id = $1)",
		fmt.Sprintf("tn-%s", slug),
	).Scan(&exists)
	if err != nil {
		return "", fmt.Errorf("check slug: %w", err)
	}
	if !exists {
		return slug, nil
	}

	// Slug taken — try with suffix
	for i := 1; i <= 100; i++ {
		candidate := fmt.Sprintf("%s-%d", slug, i)
		err := p.db.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM tenants WHERE id = $1)",
			fmt.Sprintf("tn-%s", candidate),
		).Scan(&exists)
		if err != nil {
			return "", fmt.Errorf("check slug candidate: %w", err)
		}
		if !exists {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not find available slug for %q", slug)
}
