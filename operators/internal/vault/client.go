// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package vault provides the minimal Vault Transit key management
// interface used by tenant-controller (T3.3 of openbao-adoption v1.3).
//
// The full SDK wrapper lives in pkg/vault; this package defines the small
// subset operators consume. Splitting prevents the operators binary from
// transitively pulling api/* internals via pkg/vault tests.
package vault

import (
	"context"
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
// tenant-deletion lifecycle nobody has decided yet (B-075). Deleting the primitive
// because half of it died would remove the right-to-erasure surface to tidy up an
// interface; it is kept as that surface, with nothing else beside it.
//
// The key path helper and the typed quota/unreachable errors that used to live here
// were removed: nothing read them, the errors' doc claimed a tenant_controller branch
// that never existed, and the path helper formatted `transit/keys/tenant-<id>` while
// the api creates `loopruntime-tenants-<id>`, so it named a key nobody creates. Whoever
// implements the shredder takes the key name from pkg/vault.TenantKeyName, the one
// the api encrypts with, not from a second formula.
type TenantTransitClient interface {
	DeleteTenantKey(ctx context.Context, tenantID string) error
}
