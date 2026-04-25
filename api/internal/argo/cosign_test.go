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

// CHG-2026-04-25-ATTESTATION-WIRE Phase 1 (AC-1.1): the cosign step MUST
// declare two output parameters so the workflow surfaces signature data
// to the API after the pod ends.
//
// `signature` carries the full pinned reference {imageTag}@{digest} —
// what `cosign verify` needs to validate.
// `digest` carries the sha256:... portion alone, used to populate
// releases.image_digest with the actual cryptographic digest (not the
// tag, which is what AppRelease.ImageDigest currently aliases).
//
// Trust model: both files are written by the cosign container into its
// OWN /tmp filesystem. The path MUST NOT be on the shared /workspace
// volume — that would let the BuildKit container forge signatures by
// writing to the same path before cosign exits.
func TestCosignSignTemplate_DeclaresOutputs(t *testing.T) {
	c := cosignSignTemplate("api", "registry.example.com/org/app:abc", false)
	if len(c.Outputs) < 2 {
		t.Fatalf("expected at least 2 output params (signature, digest), got %d", len(c.Outputs))
	}

	byName := map[string]bool{}
	pathByName := map[string]string{}
	for _, p := range c.Outputs {
		byName[p.Name] = true
		if p.ValueFrom != nil {
			pathByName[p.Name] = p.ValueFrom.Path
		}
	}

	for _, want := range []string{"signature", "digest"} {
		if !byName[want] {
			t.Errorf("missing required output parameter %q (declared outputs: %v)", want, c.Outputs)
		}
	}

	// Trust-model invariant: paths must be under /tmp, NOT /workspace.
	for name, path := range pathByName {
		if path == "" {
			t.Errorf("output %q has no ValueFrom.Path", name)
			continue
		}
		if strings.HasPrefix(path, "/workspace") || strings.HasPrefix(path, workspaceMountPath) {
			t.Errorf("output %q path %q is on shared /workspace — tamper risk; must be on container-local /tmp", name, path)
		}
		if !strings.HasPrefix(path, "/tmp/") {
			t.Errorf("output %q path %q is not under /tmp (containerization guarantee)", name, path)
		}
	}
}

// AC-1.1 cont: the script must actually write the signature + digest
// to the declared output paths so Argo can capture them. Without this,
// the output param declaration would yield empty strings.
func TestCosignSignTemplate_ScriptWritesOutputFiles(t *testing.T) {
	c := cosignSignTemplate("api", "registry.example.com/org/app:abc", false)
	script := c.Command[len(c.Command)-1]

	// Must reference both output paths (so the script writes them).
	pathSig := ""
	pathDig := ""
	for _, p := range c.Outputs {
		if p.ValueFrom == nil {
			continue
		}
		switch p.Name {
		case "signature":
			pathSig = p.ValueFrom.Path
		case "digest":
			pathDig = p.ValueFrom.Path
		}
	}
	if pathSig == "" || pathDig == "" {
		t.Fatal("signature/digest paths not declared")
	}

	if !strings.Contains(script, pathSig) {
		t.Errorf("script does not write signature to %s:\n%s", pathSig, script)
	}
	if !strings.Contains(script, pathDig) {
		t.Errorf("script does not write digest to %s:\n%s", pathDig, script)
	}
}
