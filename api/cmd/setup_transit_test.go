// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// setup_transit_test.go — T2.5 unit tests of plan
// 2026-06-10-j22-openbao-transit-eso-sync v1.2. Validates the production
// boot-time wire-up of the OpenBao transit client.
//
// Acceptance criteria covered:
//   - TestSetupTransitClient_ReturnsNilWhenAESLocalMode (EC-05 precedence)
//   - TestSetupTransitClient_ReturnsNilWhenPlaintextMode (EC-05 precedence)
//   - TestSetupTransitClient_ConstructsClientWhenTransitMode (T2.5 happy path)
//   - TestSetupTransitClient_PropagatesConstructorError (D6 fail-loud)
//
// Misconfig fail-loud at the env-var-presence layer is covered by
// config.Validate() — see api/internal/config/config_validate_test.go.
// This file focuses on the constructor wire-up post-Validate.

package main

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/config"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestSetupTransitClient_ReturnsNilWhenAESLocalMode — EC-05 precedence:
// when only LOOP_RUNTIME_SECRET_ENCRYPTION_KEY is set and transit is OFF, the
// helper MUST return (nil, nil) so callers fall back to the legacy AES
// path. The store.encryptForStore dispatcher handles encryption there.
func TestSetupTransitClient_ReturnsNilWhenAESLocalMode(t *testing.T) {
	cfg := config.Config{
		OpenBaoTransitEnabled: false,
		SecretEncryptionKey:   "deadbeefdeadbeefdeadbeefdeadbeef",
	}
	if got := cfg.CryptoMode(); got != "aes-local" {
		t.Fatalf("CryptoMode() = %q, want aes-local", got)
	}
	client, err := setupTransitClient(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("setupTransitClient: %v", err)
	}
	if client != nil {
		t.Fatalf("setupTransitClient: want nil, got %T", client)
	}
}

// TestSetupTransitClient_ReturnsNilWhenPlaintextMode — EC-05 precedence:
// dev-only plaintext path also short-circuits to (nil, nil). Production
// never reaches this state because config.Validate() refuses plaintext
// unless LOOP_RUNTIME_DEV=true; the helper is mode-agnostic and trusts upstream.
func TestSetupTransitClient_ReturnsNilWhenPlaintextMode(t *testing.T) {
	cfg := config.Config{
		OpenBaoTransitEnabled: false,
		SecretEncryptionKey:   "",
	}
	if got := cfg.CryptoMode(); got != "plaintext" {
		t.Fatalf("CryptoMode() = %q, want plaintext", got)
	}
	client, err := setupTransitClient(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("setupTransitClient: %v", err)
	}
	if client != nil {
		t.Fatalf("setupTransitClient: want nil, got %T", client)
	}
}

// TestSetupTransitClient_ConstructsClientWhenTransitMode — T2.5 happy
// path: transit enabled + all 3 required creds present → constructor
// returns a non-nil vault.Transit. Real I/O is NOT exercised here (no
// OpenBao server in unit test); the wire-up contract under test is
// "constructor reached + interface returned + no error".
func TestSetupTransitClient_ConstructsClientWhenTransitMode(t *testing.T) {
	cfg := config.Config{
		OpenBaoTransitEnabled: true,
		OpenBaoVaultAddr:      "https://openbao.secrets-system.svc:8200",
		OpenBaoVaultRoleID:    "ee35da8a-e26c-4f80-93e1-2bd9b9d05c41",
		OpenBaoVaultSecretID:  "REDACTED_SECRET_029",
	}
	if got := cfg.CryptoMode(); got != "transit" {
		t.Fatalf("CryptoMode() = %q, want transit", got)
	}
	client, err := setupTransitClient(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("setupTransitClient: %v", err)
	}
	if client == nil {
		t.Fatalf("setupTransitClient: want non-nil client, got nil")
	}
}

// TestSetupTransitClient_PropagatesConstructorError — D6 ADR fail-loud:
// missing AppRole role_id passes config.Validate() (which only checks
// presence as string) but the vault.NewTransitClient constructor MUST
// refuse missing creds. Helper propagates the error up; caller (main.go)
// treats it as fatal and exits.
//
// EC-05: the Validate() layer enforces role_id presence as non-empty
// string at env-load time. This test exercises the defense-in-depth
// path where the constructor itself enforces the same invariant — both
// gates must hold so removing one does not silently degrade the other.
func TestSetupTransitClient_PropagatesConstructorError(t *testing.T) {
	cfg := config.Config{
		OpenBaoTransitEnabled: true,
		OpenBaoVaultAddr:      "https://openbao.secrets-system.svc:8200",
		OpenBaoVaultRoleID:    "",
		OpenBaoVaultSecretID:  "REDACTED_SECRET_029",
	}
	client, err := setupTransitClient(cfg, newTestLogger())
	if err == nil {
		t.Fatalf("setupTransitClient: want error for missing role_id, got nil")
	}
	if !strings.Contains(err.Error(), "openbao transit client") {
		t.Fatalf("setupTransitClient: error %q missing wrap prefix", err)
	}
	if client != nil {
		t.Fatalf("setupTransitClient: want nil client on error, got %T", client)
	}
}
