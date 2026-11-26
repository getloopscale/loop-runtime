// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package correlation provides correlation ID context-key utilities shared
// across api/ and operators/.
//
// Audit gap M08 #10 / §6.C.1b (active-execution-plan): originally lived under
// `api/internal/correlation`, but the operator-side annotation consumption
// (ApplicationReconciler reading `runtime.getloopscale.com/correlation-id` and propagating
// into ctx for downstream audit_log writes) needed the same context-key.
// Moved to pkg/ so both binaries import a single source of truth — no
// duplicated helper, no risk of ctxKey types diverging silently.
package correlation

import (
	"context"

	"github.com/google/uuid"
)

// GenerateID returns a new UUID v4 correlation ID.
func GenerateID() string {
	return uuid.New().String()
}

// ctxKey is the package-private key for storing the correlation ID in
// context.Context. Unexported type prevents collision with other packages.
type ctxKey struct{}

// WithID returns a new context carrying the correlation id. Empty id is a
// no-op (returns the parent ctx unchanged) so callers don't accidentally
// store empty strings.
//
// T5.6 of deep-review-remediation-plan (closes find_obs_002 HIGH):
// previously correlation ID lived only in HTTP headers (5/32 routes
// propagated it). Context-based propagation lets services + DB stores
// extract the ID without re-reading the *http.Request.
func WithID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, id)
}

// IDFromContext returns the correlation ID stored in ctx, or empty string
// if absent. Safe for use as a log/slog field — callers can filter empty
// values via slog handler.
func IDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(ctxKey{}).(string)
	return v
}
