// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

// app_attestation_types.go — public types for the per-app attestation
// domain that survive the T1.3b deletion of the legacy hand-rolled
// `app_attestation_store.go`. Per plan
// 2026-05-29-sqlc-migration-plan v1.3 § T1.3b, this file
// carries:
//
//   - SBOMFormat* constants (CHECK constraint mirror per migration
//     000005; the sqlc-backed UpdateAppSBOM still validates against
//     these in the wrapper)
//   - AppAttestation struct (row-shape consumed by callers)
//   - AppAttestationInfo struct (JSON-friendly per-app summary returned
//     by the /api/v1/builds/:id/attestation + /sbom endpoints)
//   - AppAttestationStore interface (service field types depend on it)
//   - validateDigest helper (sha256: prefix gate, called by the wrapper
//     pre-INSERT validation loop)
//
// The sqlc-backed implementation (AppAttestationStoreSqlc) lives in
// app_attestation_wrapper.go and satisfies AppAttestationStore 1:1.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// SBOM format constants — must match CHECK constraint in migration
// 000005. The wrapper UpdateAppSBOM validates against these BEFORE any
// DB call to surface bad input as application errors instead of PG
// 23514.
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

// AppAttestationInfo is the JSON-friendly attestation summary used by
// the /api/v1/builds/:id/attestation endpoint (per-app variant) and the
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

// AppAttestationStore is the contract for per-app attestation
// persistence (Sprint 6 / T0.1 — sidecar table for ADR-0014 multi-app
// fix).
//
// Implemented by *AppAttestationStoreSqlc
// (app_attestation_wrapper.go) as of T1.3b of plan
// 2026-05-29-sqlc-migration v1.3. Previously implemented by
// *PostgresAppAttestationStore in app_attestation_store.go — that file
// was deleted in T1.3b after the 2 prod call sites switched to
// NewAppAttestationStoreSqlc.
type AppAttestationStore interface {
	// SaveAppAttestations upserts attestation rows for every app in a
	// release. The unique key (release_id, app_name) makes this
	// idempotent.
	SaveAppAttestations(ctx context.Context, releaseID, tenantID string, atts []AppAttestation) error

	// SaveAppAttestationsTx — same as SaveAppAttestations but accepts
	// an explicit DBTX (transaction) so the release row + attestation
	// rows can be committed atomically. Plan T2.4 D7 — eliminates the
	// FK violation race where attestation insert raced ahead of
	// release commit (F-S2-01).
	SaveAppAttestationsTx(ctx context.Context, tx db.DBTX, releaseID, tenantID string, atts []AppAttestation) error

	// GetBuildAppAttestations returns all per-app attestations for the
	// most recent release of a build. Empty slice if no release exists
	// yet. Cross-tenant queries return empty slice (no existence
	// leak).
	GetBuildAppAttestations(ctx context.Context, tenantID, buildID string) ([]AppAttestationInfo, error)

	// UpdateAppSBOM persists SBOM metadata for one (release, app) row.
	UpdateAppSBOM(ctx context.Context, releaseID, appName, s3Key, format string, componentCount int) error
}

// validateDigest enforces the migration CHECK (image_digest LIKE
// 'sha256:%'). Reject early to surface bad input as application
// errors instead of PG 23514. Called by the wrapper's saveOn pre-loop
// validation (ALL-signed contract).
func validateDigest(digest string) error {
	if !strings.HasPrefix(digest, "sha256:") {
		return fmt.Errorf("image_digest must start with sha256:, got %q", digest)
	}
	return nil
}
