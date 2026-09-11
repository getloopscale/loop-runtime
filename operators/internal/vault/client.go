// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package vault provides the minimal Vault Transit key management
// interface used by tenant-controller (T3.3 of openbao-adoption v1.3).
//
// The full SDK wrapper lives in pkg/vault; this package defines the small
// subset operators consume. Splitting prevents the operators binary from
// transitively pulling api/* internals via pkg/vault tests.
package vault

import (
	"context"
	"errors"
	"fmt"
)

// TenantTransitClient is what the operator side still owns of the per-tenant Vault
// Transit key: **only the shredding**.
//
// DeleteTenantKey is the crypto-shredding primitive (T3.6 LIVE test + T10.1 Day-30
// verify + T10.2 Day-60 AES delete). When the per-tenant key is deleted, every
// ciphertext encrypted by it becomes permanently unrecoverable. EC-10 MUST FIX P0:
// the cleanup job must enforce the 2-person rule + 90-day window before calling this.
//
// EnsureTenantKey was removed in 2026-08-16 per ADR
// `docs/decisions.md` (B-042):
// provisioning is api-side. The api creates the key lazily on the first encrypt 404
// (`pkg/vault/transit_client.go:391`), and this side never had a production
// implementation — the only EnsureTenantKey in the tree were two test mocks, and
// TenantReconciler.VaultClient was never assigned. Ten weeks of silent no-op.
//
// **This interface still has no production implementer**, and that is now a smaller
// claim than it was: what is missing is the shredder, whose ownership depends on the
// tenant-deletion lifecycle nobody has looked at yet (B-075). Deleting the primitive
// because half of it died would remove the right-to-erasure surface to tidy up an
// interface.
type TenantTransitClient interface {
	DeleteTenantKey(ctx context.Context, tenantID string) error
}

// Typed errors returned by TenantTransitClient implementations.
// tenant_controller uses errors.Is(err, ErrVaultQuotaExceeded) to set
// Condition.Reason = ReasonVaultQuotaExceeded (EC-15 alarm trigger).
var (
	ErrVaultQuotaExceeded = errors.New("vault: quota exceeded (transit key create rate limit)")
	ErrVaultUnreachable   = errors.New("vault: unreachable (network/timeout/5xx)")
)

// TenantKeyPath returns the canonical Vault path for a tenant's transit key.
// Format: transit/keys/tenant-<tenantID>.
// Per T3.2 + T3.3 plan + tenant-controller-transit policy in openbao-init values.yaml
// (capabilities=[create, read, update, delete] on path "transit/keys/tenant-+").
func TenantKeyPath(tenantID string) string {
	return fmt.Sprintf("transit/keys/tenant-%s", tenantID)
}
