// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitops

import (
	"fmt"
	"strings"
)

// blockedResourceKinds are resource types that must NOT enter the ArgoCD-managed gitops tree.
// ClusterIssuer/Issuer are infra-level resources managed by Pulumi, not per-deploy.
// MEET-2026-04-09-042 P1: Manifest validation gate.
//
// NOTE: Certificate CRs are intentionally ALLOWED. They are required for TLS on
// platform-managed subdomains (*.dev.loopzero.example) via cert-manager + Let's Encrypt.
// The original incident (stuck Certificate blocking ArgoCD sync) was caused by
// CF-managed custom domains generating Certificate CRs — that path now skips
// Certificate generation via CFManaged=true in the renderer (MEET-044).
//
// TODO(definitive): Move this validation to the renderer layer. The gate should
// validate structural correctness (e.g., no ClusterIssuer in tenant manifests),
// not blanket-block resource kinds. The renderer already handles the CF/non-CF
// branching correctly — the gate is defense-in-depth for infra-level resources only.
var blockedResourceKinds = []string{
	"kind: ClusterIssuer", // cert-manager — managed by infra, not deploy
	"kind: Issuer",        // cert-manager — managed by infra
}

// validateManifests checks rendered manifests for known-problematic resource types.
// This is a safety net: the renderer should already exclude these, but defense-in-depth
// ensures they never reach ArgoCD even if the renderer has a bug.
func validateManifests(manifests string) error {
	for _, blocked := range blockedResourceKinds {
		if strings.Contains(manifests, blocked) {
			return fmt.Errorf("manifests contain blocked resource type: %s — this resource type can block ArgoCD sync and must not be in the gitops tree", blocked)
		}
	}
	return nil
}
