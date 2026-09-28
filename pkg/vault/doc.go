// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package vault is the engine's narrow client for the OpenBao Transit secrets
// engine: per-tenant envelope encryption of secret values, authenticated with
// AppRole (see TransitClient). It speaks the Transit HTTP API over net/http and
// pins the server's SPKI hash when one is configured.
//
// The package deliberately holds only what the api binary calls. A generic SDK
// wrapper (Kubernetes-auth Client, a caching decorator, database and
// Kubernetes secrets-engine clients) lived here without a single production
// caller and was removed; reintroduce such a client together with its first
// caller, not ahead of it.
//
// Also here: Zeroize (clear plaintext buffers) and RedactionHandler (an slog
// handler that masks secret-shaped values).
package vault
