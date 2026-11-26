// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

import "github.com/getloopscale/loop-runtime/api/internal/db"

// attestation_types.go — public struct + constructor for the
// supply-chain attestation summary store that survives the T2.1
// (Batch A #8b) deletion of the legacy `attestation_store.go`. The
// 1 prod call site uses the constructor — preserved here.

// PostgresAttestationStore implements domain.AttestationStore by
// reading the latest release for a build from the `releases` table
// and mapping the supply-chain columns (image_digest,
// image_signature, image_signed_at) to AttestationInfo.
//
// CHG-2026-04-25-ATTESTATION-WIRE Phase 6:
//   - The endpoint must NOT leak existence across tenants (cross-
//     tenant reads return ErrBuildNotFound, mapped to 404 by the
//     handler).
//   - Empty signature fields → signed: false (legacy releases pre-
//     Phase 5).
//   - Multi-app builds: the release row reflects Apps[0] (primary),
//     per ADR-0014. The endpoint's response describes the primary
//     app.
//
// T2.1 Batch A #8b (plan 2026-05-29-sqlc-migration v1.3): db field
// type preserved (db.DBTX); methods (attestation_wrapper.go)
// dispatch through SqlcBridge to sqlc-generated queries that read
// the same 3 supply-chain columns from `releases`.
type PostgresAttestationStore struct {
	db db.DBTX
}

// NewPostgresAttestationStore wires a concrete attestation store on
// the shared pgx pool / DBTX abstraction. Preserved verbatim from
// legacy.
func NewPostgresAttestationStore(db db.DBTX) *PostgresAttestationStore {
	return &PostgresAttestationStore{db: db}
}
