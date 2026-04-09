// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitops

import (
	"fmt"
	"strings"
)

// blockedResourceKinds are resource types that must NOT enter the ArgoCD-managed gitops tree.
// Certificate CRs from cert-manager caused a production incident: a stuck "Progressing"
// Certificate blocked ArgoCD sync for 50+ minutes, preventing ALL deploys for the tenant.
// MEET-2026-04-09-042 P1: Manifest validation gate.
var blockedResourceKinds = []string{
	"kind: Certificate",   // cert-manager — CF handles TLS for custom domains
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
