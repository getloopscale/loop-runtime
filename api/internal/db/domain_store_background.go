// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"fmt"
	"time"

	"github.com/loopzero/loopzero/api/internal/domain"
)

// --- Methods for background services (DomainProvisioner + DomainHealthChecker) ---

// FindAndClaimVerified atomically claims verified domains → provisioning.
// FIND-001: atomic claim prevents HA double-processing.
func (s *PostgresDomainStore) FindAndClaimVerified(ctx context.Context) ([]domain.CustomDomain, error) {
	return s.ClaimByStatus(ctx, "verified", "provisioning")
}

// FindRemoving atomically claims removing domains for cleanup.
// F-011 fix: uses ClaimByStatus to prevent HA double-processing.
// The status stays "removing" (no-op transition) but the row is locked.
func (s *PostgresDomainStore) FindRemoving(ctx context.Context) ([]domain.CustomDomain, error) {
	return s.ClaimByStatus(ctx, "removing", "removing")
}

// FindPendingForAutoVerify returns pending_verification domains (for background
// auto-verify), scoped to the given cell (roadmap 03). The filter joins to the
// tenants table because custom_domains is a small, low-frequency read; a JOIN
// at this cardinality is cheaper than denormalizing another column. Pass
// "cell-default" or "" for single-cell / backward-compatible behavior.
func (s *PostgresDomainStore) FindPendingForAutoVerify(ctx context.Context, cellID string) ([]domain.CustomDomain, error) {
	if cellID == "" {
		cellID = CellDefault
	}
	rows, err := s.DB.Query(ctx,
		`SELECT cd.id, cd.tenant_id, cd.project_name, cd.app_name, cd.environment, cd.domain, cd.status,
		        cd.verification_token, cd.verified_at, cd.tls_status, cd.tls_expires_at,
		        cd.failure_reason, cd.last_cname_check, cd.cname_consecutive_failures, cd.created_at, cd.updated_at,
		        cd.cf_zone_id, cd.cf_zone_status, cd.cf_nameservers, cd.dns_records_imported
		 FROM custom_domains cd
		 JOIN tenants t ON cd.tenant_id = t.id
		 WHERE cd.status = 'pending_verification'
		   AND t.cell_id = $1
		 ORDER BY cd.created_at ASC
		 LIMIT 50`,
		cellID,
	)
	if err != nil {
		return nil, fmt.Errorf("domain store: find pending for auto-verify (cell=%s): %w", cellID, err)
	}
	defer rows.Close()
	return scanDomains(rows)
}

// FindActiveForCNAMECheck returns active domains due for weekly CNAME re-verification.
func (s *PostgresDomainStore) FindActiveForCNAMECheck(ctx context.Context, staleAfter time.Time) ([]domain.CustomDomain, error) {
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
func (s *PostgresDomainStore) FindStalePending(ctx context.Context, maxAge time.Duration) ([]domain.CustomDomain, error) {
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
func (s *PostgresDomainStore) FindStaleProvisioning(ctx context.Context, maxAge time.Duration) ([]domain.CustomDomain, error) {
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
func (s *PostgresDomainStore) FindActiveWithTLSExpiry(ctx context.Context, withinDays int) ([]domain.CustomDomain, error) {
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
func (s *PostgresDomainStore) ListActiveByApp(ctx context.Context, tenantID, project, app, env string) ([]domain.CustomDomain, error) {
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
func (s *PostgresDomainStore) ClaimByStatus(ctx context.Context, fromStatus, toStatus string) ([]domain.CustomDomain, error) {
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
func (s *PostgresDomainStore) findByStatus(ctx context.Context, status string) ([]domain.CustomDomain, error) {
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
