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

// TenantTransitClient is the minimal Vault Transit interface used by
// tenant_controller's TransitKey reconcile step.
//
// EnsureTenantKey is idempotent: re-creating an existing key returns nil
// (success). Quota errors return ErrVaultQuotaExceeded (mapped from Vault's
// "rate limit quota exceeded" 429 response). Other failures return
// ErrVaultUnreachable (network/timeout/5xx).
//
// DeleteTenantKey is the crypto-shredding primitive (T3.6 LIVE test +
// T10.1 Day-30 verify + T10.2 Day-60 AES delete). When the per-tenant key
// is deleted, all ciphertexts encrypted by that key become permanently
// unrecoverable. EC-10 MUST FIX P0: cleanup job must enforce 2-person
// rule + 90-day window before calling this.
type TenantTransitClient interface {
	EnsureTenantKey(ctx context.Context, tenantID string) error
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
