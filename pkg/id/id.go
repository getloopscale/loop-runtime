// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package id provides deterministic ID generation and validation for LoopZero
// resources. IDs use crypto/rand for cryptographic randomness.
package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
)

// Prefixes (canonical) for each resource type.
const (
	BuildIDPrefix          = "build-"
	ReleaseIDPrefix        = "rel-"
	PreviewIDPrefix        = "prev-"
	AppAttestationIDPrefix = "raa-"
)

// Pre-compiled regexes for validation. Each accepts exactly the canonical
// generated form: prefix + 16 lowercase hex characters.
var (
	buildIDPattern          = regexp.MustCompile(`^build-[0-9a-f]{16}$`)
	releaseIDPattern        = regexp.MustCompile(`^rel-[0-9a-f]{16}$`)
	previewIDPattern        = regexp.MustCompile(`^prev-[0-9a-f]{16}$`)
	appAttestationIDPattern = regexp.MustCompile(`^raa-[0-9a-f]{16}$`)
)

// GenerateBuildID returns a unique build identifier in the format "build-{16hex}".
// Uses 8 random bytes (64-bit entropy) for collision resistance (A-07).
func GenerateBuildID() string {
	return fmt.Sprintf("%s%s", BuildIDPrefix, randomHex(8))
}

// GenerateReleaseID returns a unique release identifier in the format "rel-{16hex}".
// Uses 8 random bytes (64-bit entropy) for collision resistance (A-07).
func GenerateReleaseID() string {
	return fmt.Sprintf("%s%s", ReleaseIDPrefix, randomHex(8))
}

// GeneratePreviewID returns a unique preview environment identifier in the
// format "prev-{16hex}". The "prev-" prefix is enforced by the
// preview_environments table CHECK constraint (migration 000003).
func GeneratePreviewID() string {
	return fmt.Sprintf("%s%s", PreviewIDPrefix, randomHex(8))
}

// GenerateAppAttestationID returns a unique per-app attestation identifier in
// the format "raa-{16hex}". The "raa-" prefix is enforced by the
// release_app_attestations table CHECK constraint (migration 000005).
// Sprint 6 / T0.1 — sidecar table for multi-app attestation (ADR-0014).
func GenerateAppAttestationID() string {
	return fmt.Sprintf("%s%s", AppAttestationIDPrefix, randomHex(8))
}

// GenerateRollbackID creates a new rollback ID with the format rb-<16 hex chars>.
func GenerateRollbackID() string {
	return "rb-" + randomHex(8)
}

// IsValidBuildID reports whether s matches the canonical build ID format
// "build-{16 lowercase hex chars}". Use this instead of strings.HasPrefix
// when validating IDs from untrusted sources (HTTP requests, DB rows from
// older migrations, CLI input). Per ADR-0028 supporting facilities + gate
// PKG-G06 (Deterministic Naming And IDs).
func IsValidBuildID(s string) bool {
	return buildIDPattern.MatchString(s)
}

// IsValidReleaseID reports whether s matches the canonical release ID format
// "rel-{16 lowercase hex chars}".
func IsValidReleaseID(s string) bool {
	return releaseIDPattern.MatchString(s)
}

// IsValidPreviewID reports whether s matches the canonical preview ID format
// "prev-{16 lowercase hex chars}".
func IsValidPreviewID(s string) bool {
	return previewIDPattern.MatchString(s)
}

// IsValidAppAttestationID reports whether s matches the canonical app
// attestation ID format "raa-{16 lowercase hex chars}".
func IsValidAppAttestationID(s string) bool {
	return appAttestationIDPattern.MatchString(s)
}

// randomHex generates a random hex string of n*2 characters from n random bytes.
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}
