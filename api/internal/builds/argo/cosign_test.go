// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argo

import (
	"strings"
	"testing"

	forge "github.com/getloopscale/argo-workflows-go"
)

// B7 (CHG-2026-04-22-001): when the registry is insecure (local dev k3d),
// cosign must use --allow-insecure-registry so it speaks HTTP instead of HTTPS.
// Production registries (DO) are HTTPS — the flag must NOT appear there.

func TestCosignSignTemplate_Secure_NoInsecureFlag(t *testing.T) {
	c := cosignSignTemplate("api", "registry.example.com/org/app:abc", false)
	script := c.Source
	if strings.Contains(script, "--allow-insecure-registry") {
		t.Errorf("secure registry must NOT emit --allow-insecure-registry:\n%s", script)
	}
}

func TestCosignSignTemplate_Insecure_EmitsFlag(t *testing.T) {
	c := cosignSignTemplate("api", "k3d-loopzero-registry:5000/tenant/app:abc", true)
	script := c.Source
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
	script := c.Source

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

// Plan T4.1 Track-2 — keyless signing mode tests.
// CODE supports both modes; runtime activation deferred to Sprint 9+.

func TestCosignSignTemplateKeyless_UsesFulcioAndRekor(t *testing.T) {
	c := cosignSignTemplateKeyless(CosignSignParams{
		AppName:          "api",
		ImageTag:         "registry.example.com/org/app:abc",
		InsecureRegistry: false,
		Keyless:          true,
	})
	script := c.Source

	// Must reference Fulcio + Rekor URLs.
	if !strings.Contains(script, "fulcio.sigstore.dev") {
		t.Errorf("keyless script must invoke Fulcio:\n%s", script)
	}
	if !strings.Contains(script, "rekor.sigstore.dev") {
		t.Errorf("keyless script must record in Rekor:\n%s", script)
	}
	if !strings.Contains(script, "--fulcio-url") {
		t.Errorf("keyless script must pass --fulcio-url to cosign:\n%s", script)
	}
	if !strings.Contains(script, "--rekor-url") {
		t.Errorf("keyless script must pass --rekor-url to cosign:\n%s", script)
	}

	// Must NOT use --key (key-based signing flag).
	if strings.Contains(script, "--key ") {
		t.Errorf("keyless script must NOT pass --key:\n%s", script)
	}

	// Must read OIDC token.
	if !strings.Contains(script, "SIGSTORE_ID_TOKEN") {
		t.Errorf("keyless script must export SIGSTORE_ID_TOKEN:\n%s", script)
	}
	if !strings.Contains(script, "/var/run/secrets/sigstore/token") {
		t.Errorf("keyless script must read OIDC token from projected SA volume:\n%s", script)
	}

	// Annotation marking the mode for audit trail.
	if !strings.Contains(script, "mode=keyless") {
		t.Errorf("keyless script must annotate mode=keyless:\n%s", script)
	}
}

func TestCosignSignTemplateKeyless_PrivateFulcioOverride(t *testing.T) {
	c := cosignSignTemplateKeyless(CosignSignParams{
		AppName:   "api",
		ImageTag:  "registry.example.com/org/app:abc",
		Keyless:   true,
		FulcioURL: "https://fulcio.loopzero.internal",
		RekorURL:  "https://rekor.loopzero.internal",
	})
	script := c.Source

	if !strings.Contains(script, "fulcio.loopzero.internal") {
		t.Errorf("private Fulcio URL must override default:\n%s", script)
	}
	if !strings.Contains(script, "rekor.loopzero.internal") {
		t.Errorf("private Rekor URL must override default:\n%s", script)
	}
	if strings.Contains(script, "fulcio.sigstore.dev") {
		t.Errorf("public Fulcio URL must NOT appear when private override is set:\n%s", script)
	}
}

func TestCosignSignTemplateKeyless_NoCosignKeySecret(t *testing.T) {
	c := cosignSignTemplateKeyless(CosignSignParams{
		AppName:  "api",
		ImageTag: "registry.example.com/org/app:abc",
		Keyless:  true,
	})

	// Keyless must NOT mount the cosign-signing-key Secret (no long-lived key).
	for _, vm := range c.VolumeMounts {
		if sv, ok := vm.(*forge.SecretVolume); ok && sv.SecretName == secretCosignKey {
			t.Errorf("keyless mode must NOT mount cosign-signing-key Secret (key-based residue)")
		}
	}

	// Must NOT have COSIGN_PASSWORD env (no key-based password needed).
	for _, e := range c.Env {
		if se, ok := e.(forge.SecretEnv); ok && se.Name == "COSIGN_PASSWORD" {
			t.Errorf("keyless mode must NOT inject COSIGN_PASSWORD")
		}
	}
}

func TestCosignSignTemplateWithParams_DispatchesByMode(t *testing.T) {
	keyBased := cosignSignTemplateWithParams(CosignSignParams{
		AppName:  "api",
		ImageTag: "registry.example.com/org/app:abc",
		Keyless:  false,
	})
	if !strings.Contains(keyBased.Source, "--key ") {
		t.Errorf("Keyless=false must dispatch to key-based template (which uses --key)")
	}

	keyless := cosignSignTemplateWithParams(CosignSignParams{
		AppName:  "api",
		ImageTag: "registry.example.com/org/app:abc",
		Keyless:  true,
	})
	if strings.Contains(keyless.Source, "--key ") {
		t.Errorf("Keyless=true must dispatch to keyless template (no --key flag)")
	}
}

// T1.2 of plan 2026-05-28-meet-001-engineering-closure v1.1.
// CR-1 fix: gate `--insecure-skip-verify` flag behind explicit InsecureSkipSCT.

func TestCosignSignScript_RendersInsecureFlag_WhenEnabled(t *testing.T) {
	c := cosignSignTemplateKeyless(CosignSignParams{
		AppName:         "api",
		ImageTag:        "registry.example.com/org/app:abc",
		Keyless:         true,
		InsecureSkipSCT: true,
	})
	if !strings.Contains(c.Source, "--insecure-skip-verify") {
		t.Error("expected --insecure-skip-verify in rendered script when InsecureSkipSCT=true && Keyless=true")
	}
}

func TestCosignSignScript_OmitsInsecureFlag_WhenDisabled(t *testing.T) {
	c := cosignSignTemplateKeyless(CosignSignParams{
		AppName:         "api",
		ImageTag:        "registry.example.com/org/app:abc",
		Keyless:         true,
		InsecureSkipSCT: false,
	})
	if strings.Contains(c.Source, "--insecure-skip-verify") {
		t.Error("expected NO --insecure-skip-verify when InsecureSkipSCT=false (default)")
	}
}

// EC-11 fix: even when InsecureSkipSCT=true, flag omitted in keyed mode
// (keyed signing does not use SCT verification at all).
func TestCosignSignScript_KeyedMode_OmitsInsecureFlag_RegardlessOfEnv(t *testing.T) {
	c := cosignSignTemplateWithParams(CosignSignParams{
		AppName:         "api",
		ImageTag:        "registry.example.com/org/app:abc",
		Keyless:         false, // KEYED mode
		InsecureSkipSCT: true,  // even if set, flag must be omitted
	})
	if strings.Contains(c.Source, "--insecure-skip-verify") {
		t.Error("EC-11: --insecure-skip-verify must be OMITTED in keyed mode regardless of InsecureSkipSCT env")
	}
}
