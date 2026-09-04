// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package audit_test

import (
	"context"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/middleware/audit"
)

// TestSetResourceID_StoresValueInContext — a handler that declares its resource id
// through this context surface must be able to read it back verbatim. This is the
// whole reason the helper exists; anything else is a mismatch between declaration
// and retrieval.
func TestSetResourceID_StoresValueInContext(t *testing.T) {
	// Arrange.
	base := context.Background()
	const declared = "b42-project-slug"

	// Act.
	enriched := audit.SetResourceID(base, declared)

	// Assert.
	got := audit.GetResourceID(enriched)
	if got != declared {
		t.Fatalf("GetResourceID(SetResourceID(ctx, %q)) = %q; want %q", declared, got, declared)
	}
}

// TestGetResourceID_ReturnsEmptyWhenUnset — a bare context (no Set call in its
// lineage) must return the empty string, not panic and not return a zero-valued
// sentinel that a caller might misread as "unknown-declared". Absence is the
// legitimate state and must be observable as such.
func TestGetResourceID_ReturnsEmptyWhenUnset(t *testing.T) {
	if got := audit.GetResourceID(context.Background()); got != "" {
		t.Fatalf("GetResourceID(Background()) = %q; want empty string", got)
	}
}

// TestSetResourceID_ReturnsNewContext — the helper must NOT mutate the input
// context. Go's `context.WithValue` returns a derived context; any test caller
// that hands the same base context to two subtests must be able to trust that
// setting an id in one does not leak into the other.
func TestSetResourceID_ReturnsNewContext(t *testing.T) {
	base := context.Background()
	enriched := audit.SetResourceID(base, "will-not-leak")

	if audit.GetResourceID(base) != "" {
		t.Fatalf("SetResourceID mutated the input context — expected the base to remain unset")
	}
	if audit.GetResourceID(enriched) == "" {
		t.Fatalf("SetResourceID did not enrich the returned context")
	}
}
