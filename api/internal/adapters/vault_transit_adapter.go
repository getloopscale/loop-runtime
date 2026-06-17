// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// vault_transit_adapter.go — T2.2 of plan
// 2026-06-10-j22-openbao-transit-eso-sync v1.2. Bridges the narrow
// pkg/vault.Transit interface (Encrypt/Decrypt) to the db-layer
// VaultTransitClient interface (EncryptForTenant/DecryptForTenant).
//
// Signatures match byte-for-byte; only the method names differ between
// the two interfaces (pkg/vault uses Go-idiomatic short names, db/ uses
// the upstream openbao-adoption naming convention). Adapter is the
// one-screen ISP-correct bridge so neither side has to import the other.

package adapters

import (
	"context"

	"github.com/getloopscale/loopzero/pkg/vault"
)

// VaultTransitAdapter wraps a pkg/vault.Transit so it satisfies the
// db.VaultTransitClient interface required by PostgresSecretStore's
// decryptFromStore dispatch.
//
// Production wiring: cmd/setup_server.go constructs *vault.TransitClient
// (per T2.5 fail-loud boot wiring) and passes it through this adapter
// into NewPostgresSecretStoreWithVault.
type VaultTransitAdapter struct {
	Client vault.Transit
}

// NewVaultTransitAdapter constructs the adapter. Nil client returns nil —
// caller (cmd/setup_server.go) MUST check + refuse to wire when transit
// is required (D6 ADR fail-loud refusal).
func NewVaultTransitAdapter(client vault.Transit) *VaultTransitAdapter {
	if client == nil {
		return nil
	}
	return &VaultTransitAdapter{Client: client}
}

// EncryptForTenant delegates to the wrapped Transit.Encrypt. Plaintext is
// passed as []byte per EC-07 absorption (string immutability footgun
// prevention).
func (a *VaultTransitAdapter) EncryptForTenant(ctx context.Context, tenantID string, plaintext []byte) (string, error) {
	return a.Client.Encrypt(ctx, tenantID, plaintext)
}

// DecryptForTenant delegates to the wrapped Transit.Decrypt. Returned
// plaintext is []byte; caller MUST Zeroize when done (see
// pkg/vault.Zeroize godoc).
func (a *VaultTransitAdapter) DecryptForTenant(ctx context.Context, tenantID string, ciphertext string) ([]byte, error) {
	return a.Client.Decrypt(ctx, tenantID, ciphertext)
}
