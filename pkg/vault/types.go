// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package vault

import "errors"

// Typed errors the Transit client wraps its failures in, so callers can tell
// an authentication problem from an unreachable or refusing Vault.
var (
	// ErrAuthFailed — AppRole login was rejected or returned no token.
	ErrAuthFailed = errors.New("vault: auth failed")
	// ErrVaultUnreachable — network/timeout/5xx, or an unusable response.
	ErrVaultUnreachable = errors.New("vault: unreachable")
	// ErrPermissionDenied — 403 after a fresh login (role lacks policy on path).
	ErrPermissionDenied = errors.New("vault: permission denied")
)
