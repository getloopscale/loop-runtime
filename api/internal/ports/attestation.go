// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Attestation domain types — moved from routes/build_attestation.go per
// T1.3 of deep-review-remediation-plan (closes find_arch_001 partial).

package ports

import "context"

// AttestationInfo describes the supply chain attestation for a build's releases.
type AttestationInfo struct {
	BuildID        string `json:"build_id"`
	ImageDigest    string `json:"image_digest,omitempty"`
	ImageSignature string `json:"image_signature,omitempty"`
	SignedAt       string `json:"signed_at,omitempty"`
	Signed         bool   `json:"signed"`
}

// AttestationStore is the minimal contract for reading attestation data.
//
// The only read is tenant-scoped on purpose: a build belongs to one tenant,
// and a build owned by another tenant must be indistinguishable from one that
// does not exist (ErrBuildNotFound, ADR-0014). There is deliberately no
// caller-trusted variant — the one that existed was what let
// GET /builds/{id}/attestation disclose any tenant's signing data.
type AttestationStore interface {
	GetBuildAttestationForTenant(ctx context.Context, tenantID, buildID string) (*AttestationInfo, error)
}
