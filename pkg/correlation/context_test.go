// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// T5.6 of deep-review-remediation-plan (closes find_obs_002 HIGH).

package correlation

import (
	"context"
	"testing"
)

func TestWithID_RoundTrip(t *testing.T) {
	const id = "12345678-1234-4abc-8def-123456789012"
	ctx := WithID(context.Background(), id)
	got := IDFromContext(ctx)
	if got != id {
		t.Errorf("want %q, got %q", id, got)
	}
}

func TestWithID_Empty_NoOp(t *testing.T) {
	ctx := WithID(context.Background(), "")
	if IDFromContext(ctx) != "" {
		t.Error("empty id MUST NOT be stored")
	}
}

func TestIDFromContext_Absent(t *testing.T) {
	if IDFromContext(context.Background()) != "" {
		t.Error("ctx without ID must return empty string")
	}
}

func TestIDFromContext_NilCtx(t *testing.T) {
	// Defensive: nil ctx must not panic.
	// We use a typed-nil variable instead of the literal `nil` to avoid
	// staticcheck SA1012 ("do not pass a nil Context"); the test
	// VERIFIES that the defensive path works, so the linter warning at
	// the call site is a false positive in this specific context.
	var nilCtx context.Context
	if IDFromContext(nilCtx) != "" {
		t.Error("nil ctx must return empty string without panic")
	}
}

func TestWithID_OverridesExisting(t *testing.T) {
	ctx := WithID(context.Background(), "first")
	ctx = WithID(ctx, "second")
	got := IDFromContext(ctx)
	if got != "second" {
		t.Errorf("want second, got %q", got)
	}
}

// Same key from a different package MUST NOT collide. The unexported
// ctxKey type makes this structurally impossible — this test pins the
// invariant for future contributors.
func TestCtxKey_Unexported(t *testing.T) {
	type otherKey struct{}
	ctx := context.WithValue(context.Background(), otherKey{}, "decoy")
	ctx = WithID(ctx, "real")
	if IDFromContext(ctx) != "real" {
		t.Error("foreign keys must not interfere with correlation ID retrieval")
	}
}
