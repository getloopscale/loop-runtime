// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package gitops

import "testing"

// Gate is currently a no-op (returns nil) pending YAML-aware rewrite.
// These tests verify the no-op behavior and document what the future
// YAML-aware gate should check.

func TestValidateManifests_AllowsEverything(t *testing.T) {
	// Gate is no-op — all manifests pass. The renderer handles
	// resource-type decisions via CFManaged flag.
	cases := []string{
		`kind: Certificate`,
		`kind: ClusterIssuer`,
		`kind: Deployment`,
		``,
	}
	for _, c := range cases {
		if err := validateManifests(c); err != nil {
			t.Errorf("gate should be no-op, got error for %q: %v", c, err)
		}
	}
}
