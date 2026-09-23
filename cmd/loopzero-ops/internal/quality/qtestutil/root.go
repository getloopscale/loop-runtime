// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qtestutil

import "testing"

// Root is RepoRoot for a test: it fails the test (never skips) when the
// repository root cannot be found, because an assertion about the real tree
// that cannot locate the tree has measured nothing.
func Root(t testing.TB) string {
	t.Helper()
	root, err := RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}
