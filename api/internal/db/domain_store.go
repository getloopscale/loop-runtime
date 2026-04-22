// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/loopzero/loopzero/api/internal/routes"
)

// PostgresDomainStore implements routes.DomainStore using PostgreSQL.
type PostgresDomainStore struct {
	DB DBTX
}

// Create inserts a new custom domain.
// Returns ErrDomainExists if the domain is already registered (CODE-001 fix: atomic conflict detection).
func (s *PostgresDomainStore) Create(ctx context.Context, d *routes.CustomDomain) error {
	_, err := s.DB.Exec(ctx,
		`INSERT INTO custom_domains (id, tenant_id, project_name, app_name, environment, domain, status, verification_token, tls_status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now(), now())`,
		d.ID, d.TenantID, d.ProjectName, d.AppName, d.Environment, d.Domain, d.Status, d.VerificationToken, d.TLSStatus,
	)
	if err != nil {
		var uv *UniqueViolationError
		if errors.As(err, &uv) {
			return routes.ErrDomainExists
		}
		return fmt.Errorf("domain store: create: %w", err)
	}
	return nil
}

// GetByDomainAndTenant retrieves a domain by FQDN filtered by tenant (tenant isolation).
func (s *PostgresDomainStore) GetByDomainAndTenant(ctx context.Context, domain, tenantID string) (*routes.CustomDomain, error) {
	return s.scanOne(ctx,
		`SELECT id, tenant_id, project_name, app_name, environment, domain, status,
		        verification_token, verified_at, tls_status, tls_expires_at,
		        failure_reason, last_cname_check, cname_consecutive_failures, created_at, updated_at,
		        cf_zone_id, cf_zone_status, cf_nameservers, dns_records_imported
		 FROM custom_domains WHERE domain = $1 AND tenant_id = $2`,
		domain, tenantID,
	)
}

// ListByTenant returns domains for a tenant with optional filters.
func (s *PostgresDomainStore) ListByTenant(ctx context.Context, tenantID string, filter routes.DomainListFilter) ([]routes.CustomDomain, int, error) {
	var conditions []string
	var args []any
	argIdx := 1

	conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argIdx))
	args = append(args, tenantID)
	argIdx++

	if filter.Project != "" {
		conditions = append(conditions, fmt.Sprintf("project_name = $%d", argIdx))
		args = append(args, filter.Project)
		argIdx++
	}
	if filter.App != "" {
		conditions = append(conditions, fmt.Sprintf("app_name = $%d", argIdx))
		args = append(args, filter.App)
		argIdx++
	}
	if filter.Environment != "" {
		conditions = append(conditions, fmt.Sprintf("environment = $%d", argIdx))
		args = append(args, filter.Environment)
		argIdx++
	}

	where := strings.Join(conditions, " AND ")

	// Count total
	var total int
	countRow := s.DB.QueryRow(ctx, "SELECT COUNT(*) FROM custom_domains WHERE "+where, args...)
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("domain store: count: %w", err)
	}

	// Fetch with limit/offset
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}

	query := fmt.Sprintf(
		`SELECT id, tenant_id, project_name, app_name, environment, domain, status,
		        verification_token, verified_at, tls_status, tls_expires_at,
		        failure_reason, last_cname_check, cname_consecutive_failures, created_at, updated_at,
		        cf_zone_id, cf_zone_status, cf_nameservers, dns_records_imported
		 FROM custom_domains WHERE %s
		 ORDER BY created_at DESC
		 LIMIT $%d OFFSET $%d`,
		where, argIdx, argIdx+1,
	)
	args = append(args, limit, filter.Offset)

	rows, err := s.DB.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("domain store: list: %w", err)
	}
	defer rows.Close()

	var results []routes.CustomDomain
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("domain store: list scan: %w", err)
		}
		results = append(results, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("domain store: list rows: %w", err)
	}

	if results == nil {
		results = []routes.CustomDomain{}
	}
	return results, total, nil
}

// UpdateStatus sets the status of a domain.
func (s *PostgresDomainStore) UpdateStatus(ctx context.Context, domain, status string) error {
	result, err := s.DB.Exec(ctx,
		"UPDATE custom_domains SET status = $1, updated_at = now() WHERE domain = $2",
		status, domain,
	)
	if err != nil {
		return fmt.Errorf("domain store: update status: %w", err)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("domain store: domain not found: %s", domain)
	}
	return nil
}

// TransitionStatus atomically transitions a domain from expectedStatus to newStatus.
// CRIT-01 fix: CAS (compare-and-swap) prevents TOCTOU race conditions.
// Returns ErrStaleTransition if the domain is no longer in expectedStatus.
func (s *PostgresDomainStore) TransitionStatus(ctx context.Context, domain, expectedStatus, newStatus string) error {
	result, err := s.DB.Exec(ctx,
		"UPDATE custom_domains SET status = $1, updated_at = now() WHERE domain = $2 AND status = $3",
		newStatus, domain, expectedStatus,
	)
	if err != nil {
		return fmt.Errorf("domain store: transition %s→%s: %w", expectedStatus, newStatus, err)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("domain store: transition failed — domain %s not in expected status %s", domain, expectedStatus)
	}
	return nil
}

// UpdateVerified transitions a domain to verified status.
func (s *PostgresDomainStore) UpdateVerified(ctx context.Context, domain string, verifiedAt time.Time) error {
	result, err := s.DB.Exec(ctx,
		"UPDATE custom_domains SET status = 'verified', verified_at = $1, updated_at = now() WHERE domain = $2",
		verifiedAt, domain,
	)
	if err != nil {
		return fmt.Errorf("domain store: update verified: %w", err)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("domain store: domain not found: %s", domain)
	}
	return nil
}

// UpdateTLS updates the TLS status and expiry for a domain.
func (s *PostgresDomainStore) UpdateTLS(ctx context.Context, domain, tlsStatus string, expiresAt *time.Time) error {
	result, err := s.DB.Exec(ctx,
		"UPDATE custom_domains SET tls_status = $1, tls_expires_at = $2, updated_at = now() WHERE domain = $3",
		tlsStatus, expiresAt, domain,
	)
	if err != nil {
		return fmt.Errorf("domain store: update tls: %w", err)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("domain store: domain not found: %s", domain)
	}
	return nil
}

// UpdateFailure transitions a domain to failed with a reason.
func (s *PostgresDomainStore) UpdateFailure(ctx context.Context, domain, status, reason string) error {
	result, err := s.DB.Exec(ctx,
		"UPDATE custom_domains SET status = $1, failure_reason = $2, updated_at = now() WHERE domain = $3",
		status, reason, domain,
	)
	if err != nil {
		return fmt.Errorf("domain store: update failure: %w", err)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("domain store: domain not found: %s", domain)
	}
	return nil
}

// Delete removes a domain row permanently.
func (s *PostgresDomainStore) Delete(ctx context.Context, domain string) error {
	result, err := s.DB.Exec(ctx,
		"DELETE FROM custom_domains WHERE domain = $1",
		domain,
	)
	if err != nil {
		return fmt.Errorf("domain store: delete: %w", err)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("domain store: domain not found: %s", domain)
	}
	return nil
}

// CountByTenant returns the total number of non-deleted custom domains for a tenant.
// SEC-007 fix: Used by Create handler to enforce per-tenant domain quota.
func (s *PostgresDomainStore) CountByTenant(ctx context.Context, tenantID string) (int, error) {
	row := s.DB.QueryRow(ctx,
		"SELECT COUNT(*) FROM custom_domains WHERE tenant_id = $1 AND status != 'removing'",
		tenantID,
	)
	var count int
	if err := row.Scan(&count); err != nil {
		return 0, fmt.Errorf("domain store: count by tenant: %w", err)
	}
	return count, nil
}

// --- Internal scan helpers (shared by domain_store_background.go and domain_store_cfzone.go) ---

func scanDomains(rows Rows) ([]routes.CustomDomain, error) {
	var results []routes.CustomDomain
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("domain store: rows error: %w", err)
	}
	if results == nil {
		results = []routes.CustomDomain{}
	}
	return results, nil
}

func (s *PostgresDomainStore) scanOne(ctx context.Context, query string, args ...any) (*routes.CustomDomain, error) {
	rows, err := s.DB.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("domain store: query: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("domain store: rows error: %w", err)
		}
		return nil, nil
	}

	return scanDomain(rows)
}

func scanDomain(rows Rows) (*routes.CustomDomain, error) {
	var d routes.CustomDomain
	if err := rows.Scan(
		&d.ID, &d.TenantID, &d.ProjectName, &d.AppName, &d.Environment, &d.Domain, &d.Status,
		&d.VerificationToken, &d.VerifiedAt, &d.TLSStatus, &d.TLSExpiresAt,
		&d.FailureReason, &d.LastCNAMECheck, &d.CNAMEConsecutiveFailures, &d.CreatedAt, &d.UpdatedAt,
		&d.CFZoneID, &d.CFZoneStatus, &d.CFNameservers, &d.DNSRecordsImported,
	); err != nil {
		return nil, fmt.Errorf("domain store: scan: %w", err)
	}
	return &d, nil
}
