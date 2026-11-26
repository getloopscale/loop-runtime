// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package ports

// IdempotencyRecord is a cached response for an Idempotency-Key, as the store
// persists it. It is declared here, not in the HTTP middleware that reads it,
// so the persistence layer implements a contract without importing the
// interface layer (rules/architecture.md: inner layers never import outer ones).
type IdempotencyRecord struct {
	Key            string
	Endpoint       string
	TenantID       string
	RequestHash    string
	ResponseStatus int
	ResponseBody   []byte
}

// TenantMembership is a user's membership in a tenant, as the store reads it
// for the auth middleware. Declared here for the same reason as
// IdempotencyRecord.
type TenantMembership struct {
	TenantID string
	Role     string
}
