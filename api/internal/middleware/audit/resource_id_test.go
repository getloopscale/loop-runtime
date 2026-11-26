// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package audit_test

import (
	"context"
	"sync"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/middleware/audit"
)

// TestSetResourceIDIsReadableFromTheSameSlot — a handler that declares its
// resource id must be able to have it read back verbatim from the context the
// middleware installed. This is the whole reason the helper exists; anything
// else is a mismatch between declaration and retrieval.
func TestSetResourceIDIsReadableFromTheSameSlot(t *testing.T) {
	// Arrange.
	ctx := audit.WithResourceIDSlot(context.Background())
	const declared = "b42-project-slug"

	// Act.
	audit.SetResourceID(ctx, declared)

	// Assert.
	if got := audit.GetResourceID(ctx); got != declared {
		t.Fatalf("GetResourceID after SetResourceID(%q) = %q; want %q", declared, got, declared)
	}
}

// TestSetResourceIDReachesTheFrameThatInstalledTheSlot — the declaration
// travels UP, from the handler to the middleware above it. A handler derives
// its own context (`context.WithTimeout`, `r.WithContext`) before declaring;
// the frame that installed the slot must still see the value. A plain
// `context.WithValue` surface fails exactly here, which is why the slot exists.
func TestSetResourceIDReachesTheFrameThatInstalledTheSlot(t *testing.T) {
	// Arrange — installer's context, then a child the "handler" owns.
	installer := audit.WithResourceIDSlot(context.Background())
	handlerCtx, cancel := context.WithCancel(installer)
	defer cancel()

	// Act — the handler declares on its own derived context.
	audit.SetResourceID(handlerCtx, "declared-below")

	// Assert — the installer, which never saw handlerCtx, reads it.
	if got := audit.GetResourceID(installer); got != "declared-below" {
		t.Fatalf("installer read %q; want %q declared on a derived context", got, "declared-below")
	}
}

// TestGetResourceIDReturnsEmptyWhenNothingDeclared — a context with a slot but
// no declaration reports absence, not a sentinel a caller might misread.
func TestGetResourceIDReturnsEmptyWhenNothingDeclared(t *testing.T) {
	if got := audit.GetResourceID(audit.WithResourceIDSlot(context.Background())); got != "" {
		t.Fatalf("GetResourceID on a fresh slot = %q; want empty string", got)
	}
}

// TestSetResourceIDIsANoOpWithoutASlot — a handler tested in isolation, or
// serving a route outside the audited group, calls SetResourceID against a
// context that never passed through AuditLog. That must not panic: no handler
// should have to ask whether it is being audited.
func TestSetResourceIDIsANoOpWithoutASlot(t *testing.T) {
	bare := context.Background()

	audit.SetResourceID(bare, "nowhere-to-put-this")

	if got := audit.GetResourceID(bare); got != "" {
		t.Fatalf("GetResourceID on a slotless context = %q; want empty string", got)
	}
}

// TestSlotsAreIsolatedPerRequest — two requests must not see each other's
// declarations. The slot is allocated per WithResourceIDSlot call, so a second
// request installing its own slot shadows the first.
func TestSlotsAreIsolatedPerRequest(t *testing.T) {
	first := audit.WithResourceIDSlot(context.Background())
	second := audit.WithResourceIDSlot(context.Background())

	audit.SetResourceID(first, "request-one")

	if got := audit.GetResourceID(second); got != "" {
		t.Fatalf("second request read %q; want its own empty slot", got)
	}
}

// TestSetResourceIDIsSafeUnderConcurrentDeclaration — a handler that declares
// from a goroutine it spawned while the middleware reads after ServeHTTP is a
// real shape (fire-and-forget enrichment). Run under `-race` this catches an
// unguarded slot.
func TestSetResourceIDIsSafeUnderConcurrentDeclaration(t *testing.T) {
	ctx := audit.WithResourceIDSlot(context.Background())

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); audit.SetResourceID(ctx, "concurrent") }()
		wg.Add(1)
		go func() { defer wg.Done(); _ = audit.GetResourceID(ctx) }()
	}
	wg.Wait()

	if got := audit.GetResourceID(ctx); got != "concurrent" {
		t.Fatalf("after concurrent declaration GetResourceID = %q; want %q", got, "concurrent")
	}
}
