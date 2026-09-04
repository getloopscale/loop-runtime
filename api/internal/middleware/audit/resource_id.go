// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package audit exposes the context surface handlers use to declare the
// resource identifier the audit trail should record.
//
// Why this exists: the old middleware `AuditLog` buffered up to 64 KiB of every
// mutating request body to guess a `resource_id` from a hardcoded list of body
// keys (`projectIDBodyKeys = []string{"project_id","project","projectId"}`).
// Every new domain that spelt its id a fourth way lost `resource_id` in the
// audit log silently. `SetResourceID` moves the declaration into the handler
// — the code that already knows the id — and `GetResourceID` lets `AuditLog`
// read it back without touching the request body. The handler is the source of
// truth; the middleware is the recorder.
package audit

import "context"

// resourceIDKey is a private context-key type. Using a package-private struct
// prevents any other package from colliding on the value's storage — the
// standard Go idiom documented in `context.WithValue`.
type resourceIDKey struct{}

// SetResourceID returns a new context carrying the given resource id. Handlers
// call this after the id is known and before writing their response, so
// `AuditLog` can read the id from context instead of guessing it from the
// request body.
//
// The returned context is a derivation of the input; the input is not mutated.
// Calling twice replaces the value; the empty string is a legitimate "no id"
// state that GetResourceID will return unchanged.
func SetResourceID(ctx context.Context, resourceID string) context.Context {
	return context.WithValue(ctx, resourceIDKey{}, resourceID)
}

// GetResourceID returns the id previously stored by SetResourceID. Returns ""
// when nothing was set — a legitimate state, not an error. Callers that need to
// distinguish "declared empty" from "never declared" should re-read the audit
// contract: the two are treated identically by design, since a handler that
// runs without knowing its own resource id has no id to record.
func GetResourceID(ctx context.Context) string {
	v, _ := ctx.Value(resourceIDKey{}).(string)
	return v
}
