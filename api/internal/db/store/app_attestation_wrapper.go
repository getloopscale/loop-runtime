// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// app_attestation_wrapper.go — thin Go wrapper around sqlc-generated
// Queries for the `release_app_attestations` table.
//
// T1.3 of plan 2026-05-29-sqlc-migration-plan v1.3 (Phase
// 1 pilot — M07 SLSA L3 attestation flow + F-S2-01 FK violation race
// closure).
//
// Satisfies db.AppAttestationStore interface 1:1 so the prod wiring
// sites swap NewPostgresAppAttestationStore → NewAppAttestationStoreSqlc
// without service-side signature churn.
//
// Invariants preserved:
//   - ALL-signed contract: validation pre-loops BEFORE any INSERT
//     (partial writes leak signatures for some apps but not others,
//     violating SLSA L3 atomicity)
//   - F-S2-01 FK race closed: SaveAppAttestationsTx requires non-nil
//     tx (legacy contract maintained — caller commits release + atts
//     atomically per ADR-0014)
//   - Idempotent UPSERT on (release_id, app_name): re-running with the
//     same input is a no-op; partial updates (signature-only, sbom-
//     only) preserve via COALESCE
//   - Cross-tenant queries return EMPTY (no existence leak) — handler
//     maps empty to 404 per ADR-0014
//   - Tenant predicate enforced at SQL layer: both UpsertAppAttestation
//     and GetBuildAppAttestationsForTenant carry tenant_id; UpdateAppSBOM
//     is intentionally exempt (caller authenticates via release ownership
//     in build_reconcile_complete)

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/dbconv"

	"github.com/jackc/pgx/v5"

	"github.com/getloopscale/loop-runtime/api/internal/db/generated"
	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
	"github.com/getloopscale/loop-runtime/pkg/id"
)

// Compile-time assertion: AppAttestationStoreSqlc satisfies
// db.AppAttestationStore 1:1.
var _ AppAttestationStore = (*AppAttestationStoreSqlc)(nil)

// AppAttestationStoreSqlc is the sqlc-backed store wrapper for the
// per-app attestation sidecar table.
type AppAttestationStoreSqlc struct {
	q    *generated.Queries
	pool db.DBTX
}

// NewAppAttestationStoreSqlc constructs a sqlc-backed per-app
// attestation store. `pool` is used for non-tx paths
// (SaveAppAttestations + GetBuildAppAttestations + UpdateAppSBOM); the
// tx variant requires the caller to pass a non-nil pgx.Tx-adapted
// db.DBTX (per F-S2-01 closure).
func NewAppAttestationStoreSqlc(pool db.DBTX) *AppAttestationStoreSqlc {
	return &AppAttestationStoreSqlc{
		q:    generated.New(),
		pool: pool,
	}
}

// gen resolves the sqlc handle (db.Generated): tx wins over pool. A handle
// that is not a SqlcBridge makes the query fail with db.ErrNotSqlcBridge.
func (s *AppAttestationStoreSqlc) gen(tx db.DBTX) generated.DBTX {
	target := tx
	if target == nil {
		target = s.pool
	}
	return db.Generated(target)
}

// SaveAppAttestations upserts attestation rows via the connection pool
// (auto-commit). For atomic commits with the release row, callers MUST
// use SaveAppAttestationsTx (per F-S2-01 closure).
func (s *AppAttestationStoreSqlc) SaveAppAttestations(
	ctx context.Context, releaseID, tenantID string, atts []AppAttestation,
) error {
	return s.saveOn(ctx, nil, releaseID, tenantID, atts)
}

// SaveAppAttestationsTx upserts attestation rows inside the caller's
// transaction. Plan T2.4 D7 — closes F-S2-01 FK violation race by
// letting the caller commit the release row + all attestation rows
// atomically.
func (s *AppAttestationStoreSqlc) SaveAppAttestationsTx(
	ctx context.Context, tx db.DBTX, releaseID, tenantID string, atts []AppAttestation,
) error {
	if tx == nil {
		return fmt.Errorf("app_attestation_store: SaveAppAttestationsTx requires non-nil tx")
	}
	return s.saveOn(ctx, tx, releaseID, tenantID, atts)
}

// saveOn — shared upsert implementation. Validation pre-loops BEFORE
// any INSERT so we surface bad input as application errors before
// touching the DB (partial writes would leak the ALL-signed contract).
func (s *AppAttestationStoreSqlc) saveOn(
	ctx context.Context, tx db.DBTX, releaseID, tenantID string, atts []AppAttestation,
) error {
	if releaseID == "" || tenantID == "" {
		// Audit gap M02 #13 fix: typed LoopZeroError so the validation
		// surfaces with a stable code in audit logs + HTTP error
		// mapping.
		return loopzeroerrors.ErrMissingField.Wrap("app_attestation_store: release_id and tenant_id are required")
	}
	if len(atts) == 0 {
		return nil
	}

	// Pre-validate ALL rows BEFORE writing — partial writes would leak
	// signatures for some apps but not others, breaking the SLSA L3
	// ALL-signed contract.
	for i := range atts {
		if atts[i].AppName == "" {
			return fmt.Errorf("app_attestation_store: app_name is required (index %d)", i)
		}
		if err := validateDigest(atts[i].ImageDigest); err != nil {
			return fmt.Errorf("app_attestation_store: invalid digest for app %q: %w", atts[i].AppName, err)
		}
	}

	dbtx := s.gen(tx)
	for i := range atts {
		att := &atts[i]
		newID := att.ID
		if newID == "" {
			newID = id.GenerateAppAttestationID()
		}

		_, err := s.q.UpsertAppAttestation(ctx, dbtx, generated.UpsertAppAttestationParams{
			ID:                 newID,
			ReleaseID:          releaseID,
			TenantID:           tenantID,
			AppName:            att.AppName,
			ImageDigest:        att.ImageDigest,
			ImageSignature:     att.ImageSignature,
			SignedAt:           dbconv.TimePtrToTimestamptz(att.SignedAt),
			SbomS3Key:          att.SBOMS3Key,
			SbomFormat:         att.SBOMFormat,
			SbomComponentCount: intPtrToInt32Ptr(att.SBOMComponentCount),
		})
		if err != nil {
			return fmt.Errorf("app_attestation_store: insert app %q: %w", att.AppName, err)
		}
	}
	return nil
}

// GetBuildAppAttestations returns all per-app attestations for the
// most recent release of the given build, scoped to the tenant. Cross-
// tenant queries return empty slice (no existence leak per ADR-0014).
// Empty inputs return ErrBuildNotFound (legacy contract).
func (s *AppAttestationStoreSqlc) GetBuildAppAttestations(
	ctx context.Context, tenantID, buildID string,
) ([]AppAttestationInfo, error) {
	if tenantID == "" || buildID == "" {
		return nil, loopzeroerrors.ErrBuildNotFound
	}

	rows, err := s.q.GetBuildAppAttestationsForTenant(ctx, s.gen(nil), generated.GetBuildAppAttestationsForTenantParams{
		BuildID:  buildID,
		TenantID: tenantID,
	})
	if err != nil {
		// sqlc Query() never returns ErrNoRows for :many queries; an
		// empty result becomes an empty slice. Real errors are wrapped
		// for caller diagnostics.
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("app_attestation_store: query failed: %w", err)
	}

	out := make([]AppAttestationInfo, 0, len(rows))
	for _, r := range rows {
		info := AppAttestationInfo{
			AppName:     r.AppName,
			ImageDigest: r.ImageDigest,
			Signed:      r.SignedAt.Valid,
		}
		if r.ImageSignature != nil {
			info.ImageSignature = *r.ImageSignature
		}
		if r.SignedAt.Valid {
			info.SignedAt = r.SignedAt.Time.UTC().Format(time.RFC3339Nano)
		}
		if r.SbomS3Key != nil {
			info.SBOMS3Key = *r.SbomS3Key
		}
		if r.SbomFormat != nil {
			info.SBOMFormat = *r.SbomFormat
		}
		if r.SbomComponentCount != nil {
			info.SBOMComponentCount = int(*r.SbomComponentCount)
		}
		// SBOMGeneratedAt mirrors created_at when SBOM key is present.
		if r.SbomS3Key != nil && r.CreatedAt.Valid {
			info.SBOMGeneratedAt = r.CreatedAt.Time.UTC().Format(time.RFC3339Nano)
		}
		out = append(out, info)
	}
	return out, nil
}

// FindSignedImages returns, per app name, the newest signed image an earlier
// build of the tenant's project pushed for commitSHA. Apps that never had a
// signed image at that commit are absent. tx may be nil to use the pool; the
// build orchestrator passes its environment-lock transaction. It satisfies
// builds.SignedImageFinder.
func (s *AppAttestationStoreSqlc) FindSignedImages(
	ctx context.Context, tx db.DBTX, tenantID, projectID, commitSHA string,
) (map[string]db.ReusedImage, error) {
	if tenantID == "" || projectID == "" || commitSHA == "" {
		return nil, loopzeroerrors.ErrMissingField.Wrap("app_attestation_store: tenant_id, project_id and commit_sha are required")
	}
	rows, err := s.q.ListSignedAppImagesForCommit(ctx, s.gen(tx), generated.ListSignedAppImagesForCommitParams{
		TenantID:  tenantID,
		ProjectID: projectID,
		CommitSha: &commitSHA,
	})
	if err != nil {
		return nil, fmt.Errorf("app_attestation_store: list signed images of commit %s: %w", commitSHA, err)
	}
	images := make(map[string]db.ReusedImage, len(rows))
	for _, r := range rows {
		// The query admits only rows with both; the guard keeps a NULL from
		// ever turning into an unsigned reuse.
		if r.ImageSignature == nil || !r.SignedAt.Valid {
			continue
		}
		img := db.ReusedImage{
			Digest:    r.ImageDigest,
			Signature: *r.ImageSignature,
			SignedAt:  r.SignedAt.Time.UTC(),
		}
		if r.SbomS3Key != nil {
			img.SBOMS3Key = *r.SbomS3Key
		}
		if r.SbomFormat != nil {
			img.SBOMFormat = *r.SbomFormat
		}
		if r.SbomComponentCount != nil {
			img.SBOMComponentCount = int(*r.SbomComponentCount)
		}
		images[r.AppName] = img
	}
	return images, nil
}

// UpdateAppSBOM persists SBOM metadata for a (release, app) row. Used
// by build_reconcile_complete after extracting workflow output for the
// syft step. Caller authenticates via release ownership upstream —
// query is tenantpredicate:exempt by design.
func (s *AppAttestationStoreSqlc) UpdateAppSBOM(
	ctx context.Context, releaseID, appName, s3Key, format string, componentCount int,
) error {
	if releaseID == "" || appName == "" {
		// Audit gap M02 #13 fix: typed LoopZeroError for input validation.
		return loopzeroerrors.ErrMissingField.Wrap("app_attestation_store: release_id and app_name are required")
	}
	if format != SBOMFormatCycloneDXJSON && format != SBOMFormatSPDXJSON {
		return fmt.Errorf("app_attestation_store: invalid sbom_format %q", format)
	}

	rows, err := s.q.UpdateAppSBOM(ctx, s.gen(nil), generated.UpdateAppSBOMParams{
		SbomS3Key:          dbconv.StringEmptyToNilPtr(s3Key),
		SbomFormat:         dbconv.StringEmptyToNilPtr(format),
		SbomComponentCount: intToInt32Ptr(componentCount),
		ReleaseID:          releaseID,
		AppName:            appName,
	})
	if err != nil {
		return fmt.Errorf("app_attestation_store: update sbom: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("app_attestation_store: no row matched (release=%s app=%s)", releaseID, appName)
	}
	return nil
}

// -----------------------------------------------------------------------
// Conversion helpers between db.AppAttestation (legacy public type) and
// the sqlc-generated row type. Public types in app_attestation_types.go.
// -----------------------------------------------------------------------

// intPtrToInt32Ptr maps *int → *int32 for nullable component counts.
// nil → nil; non-nil → heap-allocated int32 conversion.
func intPtrToInt32Ptr(p *int) *int32 {
	if p == nil {
		return nil
	}
	v := int32(*p)
	return &v
}

// intToInt32Ptr returns a heap-allocated *int32 for a non-pointer int
// input. Used by UpdateAppSBOM where the caller passes a plain int.
func intToInt32Ptr(v int) *int32 {
	out := int32(v)
	return &out
}

// NOTE: validateDigest lives in app_attestation_types.go (extracted in
// T1.3b alongside the legacy file deletion) so it's a single source of
// truth shared between the (deleted) legacy code path and the wrapper.
