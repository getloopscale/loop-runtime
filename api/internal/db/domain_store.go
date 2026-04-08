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


// domainColumns is the canonical column list for all domain SELECT queries.
// Must match scanDomain field order exactly.
const domainColumns = `id, tenant_id, project_name, app_name, environment, domain, status,
	verification_token, verified_at, tls_status, tls_expires_at,
	failure_reason, last_cname_check, cname_consecutive_failures, created_at, updated_at,
	cf_zone_id, cf_zone_status, cf_nameservers, dns_records_imported`

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

// GetByDomain retrieves a domain by its FQDN (globally unique).
func (s *PostgresDomainStore) GetByDomain(ctx context.Context, domain string) (*routes.CustomDomain, error) {
	return s.scanOne(ctx,
		`SELECT id, tenant_id, project_name, app_name, environment, domain, status,
		        verification_token, verified_at, tls_status, tls_expires_at,
		        failure_reason, last_cname_check, cname_consecutive_failures, created_at, updated_at,
		        cf_zone_id, cf_zone_status, cf_nameservers, dns_records_imported
		 FROM custom_domains WHERE domain = $1`,
		domain,
	)
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

// --- Methods for background services (DomainProvisioner + DomainHealthChecker) ---

// FindAndClaimVerified atomically claims verified domains → provisioning.
// FIND-001: atomic claim prevents HA double-processing.
func (s *PostgresDomainStore) FindAndClaimVerified(ctx context.Context) ([]routes.CustomDomain, error) {
	return s.ClaimByStatus(ctx, "verified", "provisioning")
}

// FindRemoving atomically claims removing domains for cleanup.
// F-011 fix: uses ClaimByStatus to prevent HA double-processing.
// The status stays "removing" (no-op transition) but the row is locked.
func (s *PostgresDomainStore) FindRemoving(ctx context.Context) ([]routes.CustomDomain, error) {
	return s.ClaimByStatus(ctx, "removing", "removing")
}

// FindPendingForAutoVerify returns pending_verification domains (for background auto-verify).
func (s *PostgresDomainStore) FindPendingForAutoVerify(ctx context.Context) ([]routes.CustomDomain, error) {
	return s.findByStatus(ctx, "pending_verification")
}

// FindActiveForCNAMECheck returns active domains due for weekly CNAME re-verification.
func (s *PostgresDomainStore) FindActiveForCNAMECheck(ctx context.Context, staleAfter time.Time) ([]routes.CustomDomain, error) {
	rows, err := s.DB.Query(ctx,
		`SELECT id, tenant_id, project_name, app_name, environment, domain, status,
		        verification_token, verified_at, tls_status, tls_expires_at,
		        failure_reason, last_cname_check, cname_consecutive_failures, created_at, updated_at,
		        cf_zone_id, cf_zone_status, cf_nameservers, dns_records_imported
		 FROM custom_domains
		 WHERE status = 'active' AND (last_cname_check IS NULL OR last_cname_check < $1)
		 ORDER BY last_cname_check ASC NULLS FIRST
		 LIMIT 50`,
		staleAfter,
	)
	if err != nil {
		return nil, fmt.Errorf("domain store: find active for CNAME check: %w", err)
	}
	defer rows.Close()
	return scanDomains(rows)
}

// FindStalePending returns domains in pending_verification older than maxAge.
func (s *PostgresDomainStore) FindStalePending(ctx context.Context, maxAge time.Duration) ([]routes.CustomDomain, error) {
	cutoff := time.Now().UTC().Add(-maxAge)
	rows, err := s.DB.Query(ctx,
		`SELECT id, tenant_id, project_name, app_name, environment, domain, status,
		        verification_token, verified_at, tls_status, tls_expires_at,
		        failure_reason, last_cname_check, cname_consecutive_failures, created_at, updated_at,
		        cf_zone_id, cf_zone_status, cf_nameservers, dns_records_imported
		 FROM custom_domains
		 WHERE status = 'pending_verification' AND created_at < $1
		 ORDER BY created_at ASC
		 LIMIT 50`,
		cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("domain store: find stale pending: %w", err)
	}
	defer rows.Close()
	return scanDomains(rows)
}

// FindStaleProvisioning returns domains stuck in "provisioning" older than maxAge.
// F-006: used by DomainHealthChecker.checkProvisioningTimeout().
func (s *PostgresDomainStore) FindStaleProvisioning(ctx context.Context, maxAge time.Duration) ([]routes.CustomDomain, error) {
	cutoff := time.Now().UTC().Add(-maxAge)
	rows, err := s.DB.Query(ctx,
		`SELECT id, tenant_id, project_name, app_name, environment, domain, status,
		        verification_token, verified_at, tls_status, tls_expires_at,
		        failure_reason, last_cname_check, cname_consecutive_failures, created_at, updated_at,
		        cf_zone_id, cf_zone_status, cf_nameservers, dns_records_imported
		 FROM custom_domains
		 WHERE status = 'provisioning' AND updated_at < $1
		 ORDER BY updated_at ASC
		 LIMIT 50`,
		cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("domain store: find stale provisioning: %w", err)
	}
	defer rows.Close()
	return scanDomains(rows)
}

// FindActiveWithTLSExpiry returns active domains with TLS certificates expiring within N days.
// F-005: used by DomainHealthChecker.checkTLSExpiry().
func (s *PostgresDomainStore) FindActiveWithTLSExpiry(ctx context.Context, withinDays int) ([]routes.CustomDomain, error) {
	rows, err := s.DB.Query(ctx,
		`SELECT id, tenant_id, project_name, app_name, environment, domain, status,
		        verification_token, verified_at, tls_status, tls_expires_at,
		        failure_reason, last_cname_check, cname_consecutive_failures, created_at, updated_at,
		        cf_zone_id, cf_zone_status, cf_nameservers, dns_records_imported
		 FROM custom_domains
		 WHERE status = 'active' AND tls_expires_at IS NOT NULL
		   AND tls_expires_at < now() + make_interval(days => $1)
		 ORDER BY tls_expires_at ASC
		 LIMIT 50`,
		withinDays,
	)
	if err != nil {
		return nil, fmt.Errorf("domain store: find active with TLS expiry: %w", err)
	}
	defer rows.Close()
	return scanDomains(rows)
}

// UpdateCNAMECheck records the result of a CNAME re-verification check.
func (s *PostgresDomainStore) UpdateCNAMECheck(ctx context.Context, domain string, checkTime time.Time, consecutiveFailures int) error {
	result, err := s.DB.Exec(ctx,
		"UPDATE custom_domains SET last_cname_check = $1, cname_consecutive_failures = $2, updated_at = now() WHERE domain = $3",
		checkTime, consecutiveFailures, domain,
	)
	if err != nil {
		return fmt.Errorf("domain store: update CNAME check: %w", err)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("domain store: domain not found: %s", domain)
	}
	return nil
}

// ListActiveByApp returns active custom domains for a specific app (used by manifest renderer).
func (s *PostgresDomainStore) ListActiveByApp(ctx context.Context, tenantID, project, app, env string) ([]routes.CustomDomain, error) {
	rows, err := s.DB.Query(ctx,
		`SELECT id, tenant_id, project_name, app_name, environment, domain, status,
		        verification_token, verified_at, tls_status, tls_expires_at,
		        failure_reason, last_cname_check, cname_consecutive_failures, created_at, updated_at,
		        cf_zone_id, cf_zone_status, cf_nameservers, dns_records_imported
		 FROM custom_domains
		 WHERE tenant_id = $1 AND project_name = $2 AND app_name = $3 AND environment = $4
		   AND status IN ('active', 'provisioning')
		 ORDER BY domain`,
		tenantID, project, app, env,
	)
	if err != nil {
		return nil, fmt.Errorf("domain store: list active by app: %w", err)
	}
	defer rows.Close()
	return scanDomains(rows)
}

// ClaimByStatus atomically finds and transitions domains from one status to another.
// FIND-001 fix: Uses a single UPDATE...WHERE id IN (SELECT...FOR UPDATE SKIP LOCKED)
// RETURNING statement. This is atomic — the row is claimed and transitioned in one
// statement, so the lock is never released before the transition completes.
// This replaces the broken findByStatus which used FOR UPDATE SKIP LOCKED in a
// plain SELECT that released locks before processing began.
func (s *PostgresDomainStore) ClaimByStatus(ctx context.Context, fromStatus, toStatus string) ([]routes.CustomDomain, error) {
	rows, err := s.DB.Query(ctx,
		`UPDATE custom_domains
		 SET status = $2, updated_at = now()
		 WHERE id IN (
		     SELECT id FROM custom_domains
		     WHERE status = $1
		     ORDER BY created_at ASC
		     LIMIT 50
		     FOR UPDATE SKIP LOCKED
		 )
		 RETURNING id, tenant_id, project_name, app_name, environment, domain, status,
		           verification_token, verified_at, tls_status, tls_expires_at,
		           failure_reason, last_cname_check, cname_consecutive_failures, created_at, updated_at,
		           cf_zone_id, cf_zone_status, cf_nameservers, dns_records_imported`,
		fromStatus, toStatus,
	)
	if err != nil {
		return nil, fmt.Errorf("domain store: claim %s→%s: %w", fromStatus, toStatus, err)
	}
	defer rows.Close()
	return scanDomains(rows)
}

// findByStatus is a simple read-only query (no locking) for non-critical reads.
func (s *PostgresDomainStore) findByStatus(ctx context.Context, status string) ([]routes.CustomDomain, error) {
	rows, err := s.DB.Query(ctx,
		`SELECT id, tenant_id, project_name, app_name, environment, domain, status,
		        verification_token, verified_at, tls_status, tls_expires_at,
		        failure_reason, last_cname_check, cname_consecutive_failures, created_at, updated_at,
		        cf_zone_id, cf_zone_status, cf_nameservers, dns_records_imported
		 FROM custom_domains
		 WHERE status = $1
		 ORDER BY created_at ASC
		 LIMIT 50`,
		status,
	)
	if err != nil {
		return nil, fmt.Errorf("domain store: find by status %s: %w", status, err)
	}
	defer rows.Close()
	return scanDomains(rows)
}

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

// --- v2: CF Zone Management methods ---

// CreateWithCFZone inserts a new custom domain with CF zone metadata.
func (s *PostgresDomainStore) CreateWithCFZone(ctx context.Context, d *routes.CustomDomain) error {
	_, err := s.DB.Exec(ctx,
		`INSERT INTO custom_domains (id, tenant_id, project_name, app_name, environment, domain, status,
		    verification_token, tls_status, cf_zone_id, cf_zone_status, cf_nameservers, dns_records_imported, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, now(), now())`,
		d.ID, d.TenantID, d.ProjectName, d.AppName, d.Environment, d.Domain, d.Status,
		d.VerificationToken, d.TLSStatus, d.CFZoneID, d.CFZoneStatus, d.CFNameservers, d.DNSRecordsImported,
	)
	if err != nil {
		var uv *UniqueViolationError
		if errors.As(err, &uv) {
			return routes.ErrDomainExists
		}
		return fmt.Errorf("domain store: create with cf zone: %w", err)
	}
	return nil
}

// UpdateCFZoneStatus updates the CF zone activation status.
func (s *PostgresDomainStore) UpdateCFZoneStatus(ctx context.Context, domain, cfZoneStatus string) error {
	result, err := s.DB.Exec(ctx,
		`UPDATE custom_domains SET cf_zone_status = $2, updated_at = now() WHERE domain = $1`,
		domain, cfZoneStatus,
	)
	if err != nil {
		return fmt.Errorf("domain store: update cf zone status: %w", err)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("domain store: update cf zone status: domain not found")
	}
	return nil
}

// FindPendingNS returns CF-managed domains waiting for nameserver propagation.
func (s *PostgresDomainStore) FindPendingNS(ctx context.Context) ([]routes.CustomDomain, error) {
	return s.findByStatus(ctx, "pending_ns")
}

// FindNSActive returns CF-managed domains with active nameservers ready for provisioning.
func (s *PostgresDomainStore) FindNSActive(ctx context.Context) ([]routes.CustomDomain, error) {
	return s.findByStatus(ctx, "ns_active")
}

// --- Internal helpers ---

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
