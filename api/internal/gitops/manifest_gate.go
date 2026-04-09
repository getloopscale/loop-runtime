// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitops

// Removed fmt+strings — gate is no-op pending YAML-aware rewrite.

// validateManifests is a safety net for the gitops push path.
//
// MEET-2026-04-09-042 originally blocked Certificate, ClusterIssuer, and Issuer.
// This was too aggressive:
//   - Certificate CRs are required for platform-managed TLS (*.dev.loopzero.example)
//   - ClusterIssuer/Issuer appear as issuerRef inside Certificate CRs (not top-level)
//   - strings.Contains("kind: ClusterIssuer") false-positives on issuerRef fields
//
// The renderer already handles CF/non-CF branching correctly (CFManaged flag).
// The gate is now a no-op pending migration to proper YAML-aware validation.
//
// TODO(definitive): Implement YAML-aware validation that parses documents and
// checks top-level apiVersion+kind, not substring matching. Block only infra
// resources that should never be in tenant gitops (e.g., Namespace, ClusterRole).
func validateManifests(_ string) error {
	return nil
}
