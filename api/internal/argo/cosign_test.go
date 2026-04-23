// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"strings"
	"testing"
)

// B7 (CHG-2026-04-22-001): when the registry is insecure (local dev k3d),
// cosign must use --allow-insecure-registry so it speaks HTTP instead of HTTPS.
// Production registries (DO) are HTTPS — the flag must NOT appear there.

func TestCosignSignTemplate_Secure_NoInsecureFlag(t *testing.T) {
	c := cosignSignTemplate("api", "registry.example.com/org/app:abc", false)
	script := c.Command[len(c.Command)-1]
	if strings.Contains(script, "--allow-insecure-registry") {
		t.Errorf("secure registry must NOT emit --allow-insecure-registry:\n%s", script)
	}
}

func TestCosignSignTemplate_Insecure_EmitsFlag(t *testing.T) {
	c := cosignSignTemplate("api", "k3d-loopzero-registry:5000/tenant/app:abc", true)
	script := c.Command[len(c.Command)-1]
	if !strings.Contains(script, "--allow-http-registry") {
		t.Errorf("insecure registry MUST emit --allow-http-registry (for plain HTTP local):\n%s", script)
	}
	if !strings.Contains(script, "--allow-insecure-registry") {
		t.Errorf("insecure registry MUST also emit --allow-insecure-registry:\n%s", script)
	}
}
