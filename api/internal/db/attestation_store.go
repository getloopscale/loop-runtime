// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/loopzero/loopzero/api/internal/domain"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// PostgresAttestationStore implements domain.AttestationStore by reading
// the latest release for a build from the `releases` table and mapping
// the supply-chain columns (image_digest, image_signature, image_signed_at)
// to AttestationInfo.
//
// CHG-2026-04-25-ATTESTATION-WIRE Phase 6:
//   - The endpoint must NOT leak existence across tenants (cross-tenant
//     reads return ErrBuildNotFound, mapped to 404 by the handler).
//   - Empty signature fields → signed: false (legacy releases pre-Phase 5).
//   - Multi-app builds: the release row reflects Apps[0] (primary), per
//     ADR-0014. The endpoint's response describes the primary app.
type PostgresAttestationStore struct {
	db DBTX
}

// NewPostgresAttestationStore wires a concrete attestation store on the
// shared pgx pool / DBTX abstraction.
func NewPostgresAttestationStore(db DBTX) *PostgresAttestationStore {
	return &PostgresAttestationStore{db: db}
}

// GetBuildAttestation looks up the latest release for the given build
// (any tenant) and returns the supply-chain attestation summary.
//
// Tenant isolation: if the caller wants tenant-scoped lookup, use
// GetBuildAttestationForTenant instead. This method is the
// caller-trusted variant used by internal/admin paths.
//
// Returns loopzeroerrors.ErrBuildNotFound when no release exists for the
// build (e.g., build still RUNNING, or workflow Failed before Phase A).
func (s *PostgresAttestationStore) GetBuildAttestation(
	ctx context.Context, buildID string,
) (*domain.AttestationInfo, error) {
	if buildID == "" {
		return nil, loopzeroerrors.ErrBuildNotFound
	}

	var (
		digest    *string
		signature *string
		signedAt  *time.Time
	)
	err := s.db.QueryRow(ctx,
		`SELECT image_digest, image_signature, image_signed_at
		 FROM releases
		 WHERE build_id = $1
		 ORDER BY created_at DESC
		 LIMIT 1`,
		buildID,
	).Scan(&digest, &signature, &signedAt)
	if err != nil {
		if errors.Is(err, ErrNoRows) {
			return nil, loopzeroerrors.ErrBuildNotFound
		}
		return nil, fmt.Errorf("attestation store: query failed: %w", err)
	}

	info := &domain.AttestationInfo{
		BuildID: buildID,
		Signed:  signedAt != nil,
	}
	if digest != nil {
		info.ImageDigest = *digest
	}
	if signature != nil {
		info.ImageSignature = *signature
	}
	if signedAt != nil {
		info.SignedAt = signedAt.UTC().Format(time.RFC3339Nano)
	}
	return info, nil
}

// GetBuildAttestationForTenant is the tenant-isolated variant: only
// returns attestation if the build belongs to the named tenant.
// Cross-tenant reads return ErrBuildNotFound (NOT ErrForbidden) to
// avoid existence leaks per ADR-0014 trust model.
func (s *PostgresAttestationStore) GetBuildAttestationForTenant(
	ctx context.Context, tenantID, buildID string,
) (*domain.AttestationInfo, error) {
	if buildID == "" || tenantID == "" {
		return nil, loopzeroerrors.ErrBuildNotFound
	}

	var (
		digest    *string
		signature *string
		signedAt  *time.Time
	)
	err := s.db.QueryRow(ctx,
		`SELECT image_digest, image_signature, image_signed_at
		 FROM releases
		 WHERE build_id = $1 AND tenant_id = $2
		 ORDER BY created_at DESC
		 LIMIT 1`,
		buildID, tenantID,
	).Scan(&digest, &signature, &signedAt)
	if err != nil {
		if errors.Is(err, ErrNoRows) {
			return nil, loopzeroerrors.ErrBuildNotFound
		}
		return nil, fmt.Errorf("attestation store: query failed: %w", err)
	}

	info := &domain.AttestationInfo{
		BuildID: buildID,
		Signed:  signedAt != nil,
	}
	if digest != nil {
		info.ImageDigest = *digest
	}
	if signature != nil {
		info.ImageSignature = *signature
	}
	if signedAt != nil {
		info.SignedAt = signedAt.UTC().Format(time.RFC3339Nano)
	}
	return info, nil
}
