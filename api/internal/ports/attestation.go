// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Attestation domain types — moved from routes/build_attestation.go per
// T1.3 of deep-review-remediation-plan (closes find_arch_001 partial).

package ports

// AttestationInfo describes the supply chain attestation for a build's releases.
type AttestationInfo struct {
	BuildID        string `json:"build_id"`
	ImageDigest    string `json:"image_digest,omitempty"`
	ImageSignature string `json:"image_signature,omitempty"`
	SignedAt       string `json:"signed_at,omitempty"`
	Signed         bool   `json:"signed"`
}
