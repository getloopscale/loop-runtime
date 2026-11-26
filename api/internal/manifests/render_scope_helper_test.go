// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package manifests

import "testing"

// scopeFor resolves ri into the scope a builder renders in, failing the test
// when the input would not render.
func scopeFor(t *testing.T, ri RenderInput) renderScope {
	t.Helper()
	scope, err := newRenderScope(ri)
	if err != nil {
		t.Fatalf("newRenderScope: %v", err)
	}
	return scope
}
