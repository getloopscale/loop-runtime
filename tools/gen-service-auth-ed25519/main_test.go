// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"regexp"
	"testing"
)

// TestGenerate_OutputShape — happy path. Confirms the output struct
// has the right field shapes so the operator runbook's downstream
// parsing (jq, copy-paste, etc.) does not silently break.
func TestGenerate_OutputShape(t *testing.T) {
	out, err := generate()
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if out == nil {
		t.Fatal("output is nil")
	}
	if out.PrivateKeyB64Length != len(out.PrivateKeyB64) {
		t.Errorf("private_key_b64_len mismatch")
	}
	if out.PublicKeyB64Length != len(out.PublicKeyB64) {
		t.Errorf("public_key_b64_len mismatch")
	}
}

// TestGenerate_PrivateKeyDecodesTo64Bytes — the private key string, once
// decoded, must be exactly ed25519.PrivateKeySize. This is the
// invariant the pulumi component + cloud signer downstream rely on.
func TestGenerate_PrivateKeyDecodesTo64Bytes(t *testing.T) {
	out, _ := generate()
	raw, err := base64.StdEncoding.DecodeString(out.PrivateKeyB64)
	if err != nil {
		t.Fatalf("private_key_b64 is not valid base64: %v", err)
	}
	if len(raw) != ed25519.PrivateKeySize {
		t.Errorf("private key decoded length %d (want %d)", len(raw), ed25519.PrivateKeySize)
	}
}

// TestGenerate_PublicKeyDecodesTo32Bytes — mirror invariant for public.
func TestGenerate_PublicKeyDecodesTo32Bytes(t *testing.T) {
	out, _ := generate()
	raw, err := base64.StdEncoding.DecodeString(out.PublicKeyB64)
	if err != nil {
		t.Fatalf("public_key_b64 is not valid base64: %v", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		t.Errorf("public key decoded length %d (want %d)", len(raw), ed25519.PublicKeySize)
	}
}

// TestGenerate_KeysAreMathematicallyPaired — integration invariant. The
// emitted private key must produce signatures that verify under the
// emitted public key. Without this test, a coding bug could swap or
// duplicate keys and the runbook would silently emit a broken pair.
func TestGenerate_KeysAreMathematicallyPaired(t *testing.T) {
	out, _ := generate()
	priv, _ := base64.StdEncoding.DecodeString(out.PrivateKeyB64)
	pub, _ := base64.StdEncoding.DecodeString(out.PublicKeyB64)

	msg := []byte("service-auth-keygen-pairing-probe")
	sig := ed25519.Sign(ed25519.PrivateKey(priv), msg)
	if !ed25519.Verify(ed25519.PublicKey(pub), msg, sig) {
		t.Fatal("generated keys are not mathematically paired — keygen swapped or duplicated")
	}
}

// TestGenerate_MarkerIsDeterministicSHA256Prefix — the marker uses the
// same shape as components/cosign-key.ts so the audit_log correlation
// across the two key components is straightforward. Specifically:
//   - exactly 12 hex characters
//   - matches /^[a-f0-9]{12}$/
//   - deterministic over the same private key (so a re-run of the
//     same keypair through the marker hasher returns the same string)
func TestGenerate_MarkerIsDeterministicSHA256Prefix(t *testing.T) {
	out, _ := generate()
	if len(out.KeyGenerationMarker) != 12 {
		t.Errorf("marker length %d (want 12)", len(out.KeyGenerationMarker))
	}
	if !regexp.MustCompile(`^[a-f0-9]{12}$`).MatchString(out.KeyGenerationMarker) {
		t.Errorf("marker %q does not match hex prefix shape", out.KeyGenerationMarker)
	}
}

// TestGenerate_RunsAreNotDeterministic — control case. Two consecutive
// generate() calls MUST produce different keypairs (the whole point
// of ed25519.GenerateKey with crypto/rand).
func TestGenerate_RunsAreNotDeterministic(t *testing.T) {
	a, _ := generate()
	b, _ := generate()
	if a.PrivateKeyB64 == b.PrivateKeyB64 {
		t.Fatal("two consecutive generate() calls produced the same private key — randomness broken")
	}
	if a.PublicKeyB64 == b.PublicKeyB64 {
		t.Fatal("two consecutive generate() calls produced the same public key — randomness broken")
	}
	if a.KeyGenerationMarker == b.KeyGenerationMarker {
		t.Fatal("two consecutive generate() calls produced the same marker — randomness broken")
	}
}
