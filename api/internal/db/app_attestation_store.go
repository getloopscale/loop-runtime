// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package db hosts the data-access layer for the API: pgx-based
// connection pool, repository types per aggregate (tenant, project,
// build, release, environment, etc.), and the migration runner
// integration. The pool is shared across services via dependency
// injection. Migrations are forward-only `.up.sql` per CLAUDE.md
// Rule + ADR D5.
package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
	"github.com/loopzero/loopzero/pkg/id"
)

// SBOM format constants — must match CHECK constraint in migration 000005.
const (
	SBOMFormatCycloneDXJSON = "cyclonedx-json"
	SBOMFormatSPDXJSON      = "spdx-json"
)

// AppAttestation is the row representation of release_app_attestations.
// Wire-friendly fields (RFC3339 strings) live on AppAttestationInfo.
type AppAttestation struct {
	ID                 string
	ReleaseID          string
	TenantID           string
	AppName            string
	ImageDigest        string
	ImageSignature     *string
	SignedAt           *time.Time
	SBOMS3Key          *string
	SBOMFormat         *string
	SBOMComponentCount *int
}

// AppAttestationInfo is the JSON-friendly attestation summary used by the
// /api/v1/builds/:id/attestation endpoint (per-app variant) and the
// /api/v1/builds/:id/sbom endpoint.
type AppAttestationInfo struct {
	AppName            string `json:"app_name"`
	ImageDigest        string `json:"image_digest,omitempty"`
	ImageSignature     string `json:"image_signature,omitempty"`
	SignedAt           string `json:"signed_at,omitempty"`
	Signed             bool   `json:"signed"`
	SBOMS3Key          string `json:"sbom_s3_key,omitempty"`
	SBOMFormat         string `json:"sbom_format,omitempty"`
	SBOMComponentCount int    `json:"sbom_component_count,omitempty"`
	SBOMGeneratedAt    string `json:"sbom_generated_at,omitempty"`
}

// AppAttestationStore is the contract for per-app attestation persistence
// (Sprint 6 / T0.1 — sidecar table for ADR-0014 multi-app fix).
type AppAttestationStore interface {
	// SaveAppAttestations upserts attestation rows for every app in a release.
	// The unique key (release_id, app_name) makes this idempotent.
	SaveAppAttestations(ctx context.Context, releaseID, tenantID string, atts []AppAttestation) error

	// SaveAppAttestationsTx — same as SaveAppAttestations but accepts an
	// explicit DBTX (transaction) so the release row + attestation rows can be
	// committed atomically. Plan T2.4 D7 — eliminates the FK violation race
	// where attestation insert raced ahead of release commit (F-S2-01).
	SaveAppAttestationsTx(ctx context.Context, tx DBTX, releaseID, tenantID string, atts []AppAttestation) error

	// GetBuildAppAttestations returns all per-app attestations for the
	// most recent release of a build. Empty slice if no release exists yet.
	// Cross-tenant queries return empty slice (no existence leak).
	GetBuildAppAttestations(ctx context.Context, tenantID, buildID string) ([]AppAttestationInfo, error)

	// UpdateAppSBOM persists SBOM metadata for one (release, app) row.
	UpdateAppSBOM(ctx context.Context, releaseID, appName, s3Key, format string, componentCount int) error
}


// PostgresAppAttestationStore is the Postgres implementation.
type PostgresAppAttestationStore struct {
	db DBTX
}

// NewPostgresAppAttestationStore constructs a per-app attestation store.
func NewPostgresAppAttestationStore(db DBTX) *PostgresAppAttestationStore {
	return &PostgresAppAttestationStore{db: db}
}

// validateDigest enforces the migration CHECK (image_digest LIKE 'sha256:%').
// Reject early to surface bad input as application errors instead of PG 23514.
func validateDigest(digest string) error {
	if !strings.HasPrefix(digest, "sha256:") {
		return fmt.Errorf("image_digest must start with sha256:, got %q", digest)
	}
	return nil
}

// SaveAppAttestations upserts attestation rows. Idempotent by
// (release_id, app_name) UNIQUE constraint — same release+app updates
// signature/sbom fields without duplicating rows.
//
// Error handling:
//   - empty release_id / tenant_id / atts → no-op nil
//   - any digest violating sha256: prefix → returns error before DB roundtrip
//   - DB errors are wrapped with context
func (s *PostgresAppAttestationStore) SaveAppAttestations(
	ctx context.Context, releaseID, tenantID string, atts []AppAttestation,
) error {
	return s.saveAppAttestationsOn(ctx, s.db, releaseID, tenantID, atts)
}

// SaveAppAttestationsTx — same logic as SaveAppAttestations but uses an
// explicit transaction so the caller can atomically commit release + atts.
// Plan T2.4 D7 — closes F-S2-01 FK violation race.
func (s *PostgresAppAttestationStore) SaveAppAttestationsTx(
	ctx context.Context, tx DBTX, releaseID, tenantID string, atts []AppAttestation,
) error {
	if tx == nil {
		return fmt.Errorf("app_attestation_store: SaveAppAttestationsTx requires non-nil tx")
	}
	return s.saveAppAttestationsOn(ctx, tx, releaseID, tenantID, atts)
}

// saveAppAttestationsOn — shared implementation. Same query/validation as
// the legacy SaveAppAttestations, but parameterized on the DBTX so callers
// can pass either s.db (auto-commit) or a transaction handle.
func (s *PostgresAppAttestationStore) saveAppAttestationsOn(
	ctx context.Context, conn DBTX, releaseID, tenantID string, atts []AppAttestation,
) error {
	if releaseID == "" || tenantID == "" {
		// Audit gap M02 #13 fix: typed LoopZeroError so the validation surfaces
		// with a stable code in audit logs and HTTP error mapping.
		return loopzeroerrors.ErrMissingField.Wrap("app_attestation_store: release_id and tenant_id are required")
	}
	if len(atts) == 0 {
		return nil
	}

	// Validate all rows BEFORE writing — partial writes would leak signatures
	// for some apps but not others, breaking the ALL-signed contract.
	for i := range atts {
		if atts[i].AppName == "" {
			return fmt.Errorf("app_attestation_store: app_name is required (index %d)", i)
		}
		if err := validateDigest(atts[i].ImageDigest); err != nil {
			return fmt.Errorf("app_attestation_store: invalid digest for app %q: %w", atts[i].AppName, err)
		}
	}

	for i := range atts {
		att := &atts[i]
		newID := att.ID
		if newID == "" {
			newID = id.GenerateAppAttestationID()
		}

		_, err := conn.Exec(ctx,
			`INSERT INTO release_app_attestations
				(id, release_id, tenant_id, app_name,
				 image_digest, image_signature, signed_at,
				 sbom_s3_key, sbom_format, sbom_component_count)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			 ON CONFLICT (release_id, app_name) DO UPDATE SET
				image_digest = EXCLUDED.image_digest,
				image_signature = COALESCE(EXCLUDED.image_signature, release_app_attestations.image_signature),
				signed_at = COALESCE(EXCLUDED.signed_at, release_app_attestations.signed_at),
				sbom_s3_key = COALESCE(EXCLUDED.sbom_s3_key, release_app_attestations.sbom_s3_key),
				sbom_format = COALESCE(EXCLUDED.sbom_format, release_app_attestations.sbom_format),
				sbom_component_count = COALESCE(EXCLUDED.sbom_component_count, release_app_attestations.sbom_component_count)`,
			newID, releaseID, tenantID, att.AppName,
			att.ImageDigest, att.ImageSignature, att.SignedAt,
			att.SBOMS3Key, att.SBOMFormat, att.SBOMComponentCount,
		)
		if err != nil {
			return fmt.Errorf("app_attestation_store: insert app %q: %w", att.AppName, err)
		}
	}
	return nil
}

// GetBuildAppAttestations returns all per-app attestations for the most
// recent release of the build. Cross-tenant queries return empty slice
// (no existence leak — handler maps empty to 404 per ADR-0014).
//
// Returns ErrBuildNotFound when no release exists for the build at all.
func (s *PostgresAppAttestationStore) GetBuildAppAttestations(
	ctx context.Context, tenantID, buildID string,
) ([]AppAttestationInfo, error) {
	if tenantID == "" || buildID == "" {
		return nil, loopzeroerrors.ErrBuildNotFound
	}

	rows, err := s.db.Query(ctx,
		`SELECT raa.app_name,
				raa.image_digest,
				raa.image_signature,
				raa.signed_at,
				raa.sbom_s3_key,
				raa.sbom_format,
				raa.sbom_component_count,
				raa.created_at
		 FROM release_app_attestations raa
		 INNER JOIN releases r ON r.id = raa.release_id
		 WHERE r.build_id = $1 AND r.tenant_id = $2
		   AND r.created_at = (
			SELECT MAX(created_at) FROM releases
			WHERE build_id = $1 AND tenant_id = $2
		   )
		 ORDER BY raa.app_name ASC`,
		buildID, tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("app_attestation_store: query failed: %w", err)
	}
	defer rows.Close()

	var out []AppAttestationInfo
	for rows.Next() {
		var (
			appName     string
			digest      string
			signature   *string
			signedAt    *time.Time
			sbomS3Key   *string
			sbomFormat  *string
			sbomCount   *int
			createdAt   time.Time
		)
		if err := rows.Scan(&appName, &digest, &signature, &signedAt,
			&sbomS3Key, &sbomFormat, &sbomCount, &createdAt); err != nil {
			return nil, fmt.Errorf("app_attestation_store: scan: %w", err)
		}

		info := AppAttestationInfo{
			AppName:     appName,
			ImageDigest: digest,
			Signed:      signedAt != nil,
		}
		if signature != nil {
			info.ImageSignature = *signature
		}
		if signedAt != nil {
			info.SignedAt = signedAt.UTC().Format(time.RFC3339Nano)
		}
		if sbomS3Key != nil {
			info.SBOMS3Key = *sbomS3Key
		}
		if sbomFormat != nil {
			info.SBOMFormat = *sbomFormat
		}
		if sbomCount != nil {
			info.SBOMComponentCount = *sbomCount
		}
		// SBOMGeneratedAt mirrors created_at when SBOM key is present.
		if sbomS3Key != nil {
			info.SBOMGeneratedAt = createdAt.UTC().Format(time.RFC3339Nano)
		}
		out = append(out, info)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("app_attestation_store: rows iteration: %w", err)
	}
	return out, nil
}

// UpdateAppSBOM persists SBOM metadata for a (release, app) row. Used by
// build_reconcile_complete after extracting workflow output for the syft step.
func (s *PostgresAppAttestationStore) UpdateAppSBOM(
	ctx context.Context, releaseID, appName, s3Key, format string, componentCount int,
) error {
	if releaseID == "" || appName == "" {
		// Audit gap M02 #13 fix: typed LoopZeroError for input validation.
		return loopzeroerrors.ErrMissingField.Wrap("app_attestation_store: release_id and app_name are required")
	}
	if format != SBOMFormatCycloneDXJSON && format != SBOMFormatSPDXJSON {
		return fmt.Errorf("app_attestation_store: invalid sbom_format %q", format)
	}

	res, err := s.db.Exec(ctx,
		`UPDATE release_app_attestations
		 SET sbom_s3_key = $1,
		     sbom_format = $2,
		     sbom_component_count = $3
		 WHERE release_id = $4 AND app_name = $5`,
		s3Key, format, componentCount, releaseID, appName,
	)
	if err != nil {
		return fmt.Errorf("app_attestation_store: update sbom: %w", err)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("app_attestation_store: no row matched (release=%s app=%s)", releaseID, appName)
	}
	return nil
}
