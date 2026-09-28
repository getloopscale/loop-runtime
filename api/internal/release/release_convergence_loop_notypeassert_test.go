// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestReleaseConvergenceLoop_NoTypeAssertions scans the source of
// release_convergence_loop.go for `r.sync_.(` — a duck-typed assertion
// against the SyncClient receiver. The post-T1.1 implementation calls
// every method directly through the formal interface; any reintroduction
// of the pattern is a regression and fails this test.
func TestReleaseConvergenceLoop_NoTypeAssertions(t *testing.T) {
	// Resolve the file path relative to this test file (works regardless
	// of where `go test` is invoked from).
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller path")
	}
	target := filepath.Join(filepath.Dir(thisFile), "release_convergence_loop.go")
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read %s: %v", target, err)
	}

	// The literal assertion pattern. We're permissive about whitespace
	// inside the parens (the type name) but require the prefix.
	const forbidden = "r.sync_.("
	if idx := strings.Index(string(body), forbidden); idx >= 0 {
		// Surface a useful excerpt for diagnosis.
		excerptStart := idx
		if excerptStart > 80 {
			excerptStart = idx - 80
		}
		excerptEnd := idx + 120
		if excerptEnd > len(body) {
			excerptEnd = len(body)
		}
		t.Fatalf("regression: %q found in release_convergence_loop.go at byte %d.\n"+
			"Sprint B T1.1 (ADR D1) removed all duck-typed assertions against r.sync_.\n"+
			"Every method on SyncClient is now part of the formal interface — call it directly.\n"+
			"Context:\n%s",
			forbidden, idx, string(body[excerptStart:excerptEnd]))
	}
}
