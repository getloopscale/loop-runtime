// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// setup_transit.go — T2.5 of plan 2026-06-10-j22-openbao-transit-eso-sync
// v1.2. Constructs the production *vault.TransitClient at boot time when
// the operator opted in via OPENBAO_TRANSIT_ENABLED=true.
//
// Fail-loud contract (D6 ADR): when transit is enabled but the constructor
// fails (e.g., bad URL, missing AppRole creds, SPKI parse error), this
// function returns a non-nil error. The caller (main.go) MUST treat that
// error as fatal — refuse to bind the HTTP listener. Silent fallback to
// AES-local or plaintext is exactly what the "no leak" sponsor mandate
// forbids.
//
// Crypto-mode precedence (EC-05 absorption) is already enforced inside
// config.Validate(); this helper trusts cfg.CryptoMode() and only acts
// when the resolved mode is "transit".

package main

import (
	"fmt"
	"log/slog"

	"github.com/getloopscale/loop-runtime/api/internal/config"
	"github.com/getloopscale/loop-runtime/pkg/vault"
)

// setupTransitClient builds the OpenBao Transit client when the resolved
// crypto mode is "transit". Returns (nil, nil) when the mode is anything
// else — the caller MUST treat nil as "use the legacy AES/plaintext path
// already wired by setupSecretStore".
//
// EC-08 absorption: defaults RateLimiterMaxEntries to 10000 (~2 MiB
// memory ceiling at ~200 bytes/entry); operator can override via env in
// a future iteration. Default RateLimitPerSec is 10 token/s per tenant
// per pod — generous for normal CRUD, stings on brute-force.
//
// SPKI pin: passed through from cfg.OpenBaoSPKIPinSHA256 (hex-encoded
// SHA-256 of the leaf cert's SubjectPublicKeyInfo). Empty disables the
// pin — NOT recommended in production; defense-in-depth against rogue
// CA mis-issuance. Operator opt-in via OPENBAO_SPKI_PIN_SHA256 env var.
func setupTransitClient(cfg config.Config, logger *slog.Logger) (vault.Transit, error) {
	if cfg.CryptoMode() != "transit" {
		return nil, nil
	}
	client, err := vault.NewTransitClient(vault.TransitConfig{
		Addr:                  cfg.OpenBaoVaultAddr,
		RoleID:                cfg.OpenBaoVaultRoleID,
		SecretID:              cfg.OpenBaoVaultSecretID,
		SPKIPinSHA256:         cfg.OpenBaoSPKIPinSHA256,
		RateLimitPerSec:       10,
		RateLimiterMaxEntries: 10000,
	})
	if err != nil {
		return nil, fmt.Errorf("openbao transit client: %w", err)
	}
	logger.Info("openbao transit client constructed",
		"addr", cfg.OpenBaoVaultAddr,
		"spki_pinned", cfg.OpenBaoSPKIPinSHA256 != "",
		"rate_limit_per_sec", 10,
		"lru_max_entries", 10000)
	return client, nil
}
