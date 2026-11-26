// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

// attestation_wrapper.go — sqlc-backed implementation of
// PostgresAttestationStore (GetBuildAttestationForTenant). T2.1 Batch A #8b of plan
// 2026-05-29-sqlc-migration-plan v1.3.
//
// The 1 prod call site (api/cmd/setup_server.go:84) compiles
// UNCHANGED — constructor `db.NewPostgresAttestationStore(dbAdapter)`
// preserved (constructor + struct in attestation_types.go; methods
// here).
//
// Invariants preserved:
//   - Empty buildID / tenantID → ErrBuildNotFound (no DB call)
//   - NoRows → ErrBuildNotFound (no existence leak per ADR-0014
//     trust model; cross-tenant reads return the same sentinel,
//     NOT ErrForbidden)
//   - Empty signature fields → Signed=false (legacy releases pre-
//     Phase 5)
//   - SignedAt formatted as RFC3339Nano UTC

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/getloopscale/loop-runtime/api/internal/db/generated"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// GetBuildAttestationForTenant is the tenant-isolated variant: only
// returns attestation if the build belongs to the named tenant.
// Cross-tenant reads return ErrBuildNotFound to avoid existence
// leaks per ADR-0014 trust model.
func (s *PostgresAttestationStore) GetBuildAttestationForTenant(ctx context.Context, tenantID, buildID string) (*ports.AttestationInfo, error) {
	if buildID == "" || tenantID == "" {
		return nil, loopruntimeerrors.ErrBuildNotFound
	}
	gdb := db.Generated(s.db)
	q := generated.New()

	row, err := q.GetLatestReleaseAttestationByBuildForTenant(ctx, gdb, generated.GetLatestReleaseAttestationByBuildForTenantParams{
		BuildID:  buildID,
		TenantID: tenantID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, loopruntimeerrors.ErrBuildNotFound
		}
		return nil, fmt.Errorf("attestation store: query failed: %w", err)
	}
	return attestationRowToInfo(buildID, row.ImageDigest, row.ImageSignature, row.ImageSignedAt), nil
}

// attestationRowToInfo converts the sqlc-emitted column subset
// (digest *string, signature *string, signedAt pgtype.Timestamptz)
// into the public AttestationInfo struct.
func attestationRowToInfo(buildID string, digest *string, signature *string, signedAt pgtype.Timestamptz) *ports.AttestationInfo {
	info := &ports.AttestationInfo{
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
