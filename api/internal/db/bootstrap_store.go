package db

import (
	"context"
	"fmt"

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

	anyCreated := false

	// 1. INSERT user (ON CONFLICT by github_id → DO NOTHING)
	res, err := s.DB.Exec(ctx,
		`INSERT INTO users (id, email, github_id, display_name)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (github_id) DO NOTHING`,
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
	res, err = s.DB.Exec(ctx,
		`INSERT INTO tenants (id, display_name, plan, owner_id)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (id) DO NOTHING`,
		tenantID,
		displayName,
		req.Plan,
		userID,
	)
	if err != nil {
		return "", "", false, fmt.Errorf("bootstrap: insert tenant: %w", err)
	}
	if res.RowsAffected > 0 {
		anyCreated = true
	}

	// 3. INSERT tenant membership (ON CONFLICT by (tenant_id, user_id) → DO NOTHING)
	res, err = s.DB.Exec(ctx,
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

	return userID, tenantID, anyCreated, nil
}
