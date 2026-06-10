// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package vault is a THIN LoopZero-specific helper over
// github.com/openbao/openbao/api/v2 (v2.5.1, matches chart v0.28.3).
//
// Per ADR D10 (vault-secrets-sot): do NOT reinvent SDK functionality.
// SDK provides: NewClient, Logical, KVv2, NewLifetimeWatcher (auto-renew),
// retries internos. This package adds ONLY LoopZero-specific glue:
//   - Bootstrap: read SA JWT file + call SDK kubernetes auth Login
//   - Client.SDK() accessor: consumers call SDK methods directly
//     (no redundant LoopZero-wrapped KvGet/Encrypt/etc)
//   - Preflight check (D9 EC-19): KvGet on `<prefix>/__health__`
//     fails fast with typed ErrPolicyMisconfigured if role lacks policy
//   - Typed errors mapping: SDK errors → ErrKvNotFound, ErrPermissionDenied,
//     ErrVaultUnreachable, ErrAuthFailed (used by pkg/secrets.VaultProvider)
//   - Graceful shutdown via context.Context
//
// Concurrency: Client is safe for concurrent use after NewClient returns.
// The underlying *api.Client (from openbao SDK) is thread-safe per upstream
// contract.
//
// Usage:
//
//	cfg := &vault.Config{
//	    Address:    "https://vault.loopzero:8200",
//	    K8sRole:    "loopzero-api",
//	    PathPrefix: "loopzero/dev/api/secrets",
//	}
//	c, err := vault.NewClient(cfg)
//	if err != nil { return err }
//	if err := c.Login(ctx); err != nil { return err }
//	if err := c.PreflightCheck(ctx); err != nil { return err }
//	// Consumers use SDK directly:
//	secret, err := c.SDK().KVv2("secret").Get(ctx, "loopzero/dev/api/secrets/gitea/token")
//
// SDK choice rationale (EC-18 + D10): openbao/api/v2 is the OpenBao-org
// official fork of hashicorp/vault/api. Wire-compatible as of Q1 2026.
// Alignment with our PRODLIKE chart adoption (openbao/openbao v0.28.3,
// appVersion v2.5.4).
package vault
