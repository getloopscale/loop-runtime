// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Command gen-service-auth-ed25519 generates an Ed25519 keypair for the
// service-auth dual-control feature (ADR 2026-05-13-service-auth-
// ed25519-adoption). Output is two base64-encoded raw key strings,
// ready to paste into `pulumi config set --secret`:
//
//   - 64-byte Ed25519 private key  (PrivateKeySize per crypto/ed25519)
//   - 32-byte Ed25519 public key   (PublicKeySize)
//
// Why this helper exists: openssl genpkey produces a PEM-encoded ASN.1
// PKCS#8 envelope, but the Pulumi component + cloud signer + API
// loader all consume RAW key bytes (the pkg/serviceauth contract).
// Converting PEM → raw via openssl + xxd + tail is brittle and easy
// to mis-paste. This helper produces the exact strings the operator
// runbook tells them to paste, with zero post-processing.
//
// Usage:
//
//	go run ./tools/gen-service-auth-ed25519
//	# OR
//	task service-auth-ed25519:generate-keypair
//
// Output (stable shape — operator runbook depends on these field names):
//
//	{
//	  "private_key_b64": "...",
//	  "public_key_b64":  "...",
//	  "private_key_b64_len": 88,
//	  "public_key_b64_len": 44,
//	  "key_generation_marker": "12-char-sha256-prefix"
//	}
//
// The generation marker is a deterministic 12-char SHA256 prefix of
// the private key — same shape as cosign-key.ts.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

type output struct {
	PrivateKeyB64       string `json:"private_key_b64"`
	PublicKeyB64        string `json:"public_key_b64"`
	PrivateKeyB64Length int    `json:"private_key_b64_len"`
	PublicKeyB64Length  int    `json:"public_key_b64_len"`
	KeyGenerationMarker string `json:"key_generation_marker"`
}

func main() {
	pretty := flag.Bool("pretty", true, "human-friendly multi-line output (default true; set false for compact JSON)")
	humanHints := flag.Bool("hints", true, "emit pulumi config set hints to stderr (default true; off for piping)")
	flag.Parse()

	out, err := generate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "service-auth-ed25519 keygen FAILED: %v\n", err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	if *pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "json encode failed: %v\n", err)
		os.Exit(1)
	}

	if *humanHints {
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Next steps (operator):")
		fmt.Fprintln(os.Stderr, "  cd infra/pulumi && \\")
		fmt.Fprintln(os.Stderr, "    pulumi config set --secret serviceAuthEd25519Private "+out.PrivateKeyB64)
		fmt.Fprintln(os.Stderr, "  cd infra/pulumi && \\")
		fmt.Fprintln(os.Stderr, "    pulumi config set --secret serviceAuthEd25519PublicCurrent "+out.PublicKeyB64)
		fmt.Fprintln(os.Stderr, "  # for rotation only (move previous current → previous BEFORE setting new current):")
		fmt.Fprintln(os.Stderr, "  cd infra/pulumi && pulumi config set --secret serviceAuthEd25519PublicPrevious <previous current>")
	}
}

// generate produces the keypair output. Split from main() so the test
// suite can exercise the keygen + encoding logic without parsing the
// stdout JSON.
func generate() (*output, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("ed25519 keygen: %w", err)
	}
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("private key wrong length %d (want %d)", len(priv), ed25519.PrivateKeySize)
	}
	if len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key wrong length %d (want %d)", len(pub), ed25519.PublicKeySize)
	}

	privB64 := base64.StdEncoding.EncodeToString(priv)
	pubB64 := base64.StdEncoding.EncodeToString(pub)

	// Generation marker: deterministic 12-char SHA256 prefix.
	// Same shape as components/cosign-key.ts so the audit_log can
	// correlate across rotation events.
	h := sha256.Sum256(priv)
	marker := hex.EncodeToString(h[:])[:12]

	return &output{
		PrivateKeyB64:       privB64,
		PublicKeyB64:        pubB64,
		PrivateKeyB64Length: len(privB64),
		PublicKeyB64Length:  len(pubB64),
		KeyGenerationMarker: marker,
	}, nil
}
