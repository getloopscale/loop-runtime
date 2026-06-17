// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

// attestation_wrapper.go — sqlc-backed implementation of
// PostgresAttestationStore (GetBuildAttestation +
// GetBuildAttestationForTenant). T2.1 Batch A #8b of plan
// docs/plans/2026-05-29-sqlc-migration-plan.md v1.3.
//
// The 1 prod call site (api/cmd/setup_server.go:84) compiles
// UNCHANGED — constructor `db.NewPostgresAttestationStore(dbAdapter)`
// preserved (constructor + struct in attestation_types.go; methods
// here).
//
// Invariants preserved:
//   - Empty buildID / tenantID → ErrBuildNotFound (no DB call)
//   - NoRows → ErrBuildNotFound (no existence leak per ADR-0014
//     trust model; cross-tenant reads in the tenant variant return
//     the same sentinel, NOT ErrForbidden)
//   - Empty signature fields → Signed=false (legacy releases pre-
//     Phase 5)
//   - SignedAt formatted as RFC3339Nano UTC

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/getloopscale/loopzero/api/internal/db/generated"
	"github.com/getloopscale/loopzero/api/internal/domain"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// GetBuildAttestation looks up the latest release for a build (any
// tenant) and returns the supply-chain attestation summary.
// Caller-trusted variant.
func (s *PostgresAttestationStore) GetBuildAttestation(ctx context.Context, buildID string) (*domain.AttestationInfo, error) {
	if buildID == "" {
		return nil, loopzeroerrors.ErrBuildNotFound
	}
	bridge, ok := s.db.(SqlcBridge)
	if !ok {
		return nil, fmt.Errorf("attestation store: DBTX %T does not implement SqlcBridge", s.db)
	}
	q := generated.New()

	row, err := q.GetLatestReleaseAttestationByBuild(ctx, bridge.GeneratedDB(), buildID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, loopzeroerrors.ErrBuildNotFound
		}
		return nil, fmt.Errorf("attestation store: query failed: %w", err)
	}
	return attestationRowToInfo(buildID, row.ImageDigest, row.ImageSignature, row.ImageSignedAt), nil
}

// GetBuildAttestationForTenant is the tenant-isolated variant: only
// returns attestation if the build belongs to the named tenant.
// Cross-tenant reads return ErrBuildNotFound to avoid existence
// leaks per ADR-0014 trust model.
func (s *PostgresAttestationStore) GetBuildAttestationForTenant(ctx context.Context, tenantID, buildID string) (*domain.AttestationInfo, error) {
	if buildID == "" || tenantID == "" {
		return nil, loopzeroerrors.ErrBuildNotFound
	}
	bridge, ok := s.db.(SqlcBridge)
	if !ok {
		return nil, fmt.Errorf("attestation store: DBTX %T does not implement SqlcBridge", s.db)
	}
	q := generated.New()

	row, err := q.GetLatestReleaseAttestationByBuildForTenant(ctx, bridge.GeneratedDB(), generated.GetLatestReleaseAttestationByBuildForTenantParams{
		BuildID:  buildID,
		TenantID: tenantID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, loopzeroerrors.ErrBuildNotFound
		}
		return nil, fmt.Errorf("attestation store: query failed: %w", err)
	}
	return attestationRowToInfo(buildID, row.ImageDigest, row.ImageSignature, row.ImageSignedAt), nil
}

// attestationRowToInfo converts the sqlc-emitted column subset
// (digest *string, signature *string, signedAt pgtype.Timestamptz)
// into the public AttestationInfo struct. Both variants of the
// query SELECT the same 3 columns so the conversion is shared.
func attestationRowToInfo(buildID string, digest *string, signature *string, signedAt pgtype.Timestamptz) *domain.AttestationInfo {
	info := &domain.AttestationInfo{
		BuildID: buildID,
		Signed:  signedAt.Valid,
	}
	if digest != nil {
		info.ImageDigest = *digest
	}
	if signature != nil {
		info.ImageSignature = *signature
	}
	if signedAt.Valid {
		info.SignedAt = signedAt.Time.UTC().Format(time.RFC3339Nano)
	}
	return info
}
