// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/loopzero/loopzero/api/internal/domain"
)

// --- v2: CF Zone Management methods ---

// CreateWithCFZone inserts a new custom domain with CF zone metadata.
func (s *PostgresDomainStore) CreateWithCFZone(ctx context.Context, d *domain.CustomDomain) error {
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
			return domain.ErrDomainExists
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
func (s *PostgresDomainStore) FindPendingNS(ctx context.Context) ([]domain.CustomDomain, error) {
	return s.findByStatus(ctx, "pending_ns")
}

// FindNSActive returns CF-managed domains with active nameservers ready for provisioning.
func (s *PostgresDomainStore) FindNSActive(ctx context.Context) ([]domain.CustomDomain, error) {
	return s.findByStatus(ctx, "ns_active")
}
