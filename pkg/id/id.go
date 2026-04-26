// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package id provides deterministic ID generation for LoopZero resources.
// IDs use crypto/rand for cryptographic randomness.
package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GenerateBuildID returns a unique build identifier in the format "build-{16hex}".
// Uses 8 random bytes (64-bit entropy) for collision resistance (A-07).
func GenerateBuildID() string {
	return fmt.Sprintf("build-%s", randomHex(8))
}

// GenerateReleaseID returns a unique release identifier in the format "rel-{16hex}".
// Uses 8 random bytes (64-bit entropy) for collision resistance (A-07).
func GenerateReleaseID() string {
	return fmt.Sprintf("rel-%s", randomHex(8))
}

// GeneratePreviewID returns a unique preview environment identifier in the
// format "prev-{16hex}". The "prev-" prefix is enforced by the
// preview_environments table CHECK constraint (migration 000003).
func GeneratePreviewID() string {
	return fmt.Sprintf("prev-%s", randomHex(8))
}

// GenerateAppAttestationID returns a unique per-app attestation identifier in
// the format "raa-{16hex}". The "raa-" prefix is enforced by the
// release_app_attestations table CHECK constraint (migration 000005).
// Sprint 6 / T0.1 — sidecar table for multi-app attestation (ADR-0014).
func GenerateAppAttestationID() string {
	return fmt.Sprintf("raa-%s", randomHex(8))
}

// randomHex generates a random hex string of n*2 characters from n random bytes.
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}
