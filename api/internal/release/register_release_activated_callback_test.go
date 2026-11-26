// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package release

import (
	"context"
	"testing"
)

// M9 Step 6: exercises the release context's own callback-registration seam
// (onReleaseActivated private var); moved out of a build test file so it can
// assert the private var natively without an accessor.

func TestRegisterReleaseActivatedCallback(t *testing.T) {
	called := false
	RegisterReleaseActivatedCallback(func(ctx context.Context, _, _, _, _ string) {
		called = true
	})
	if onReleaseActivated == nil {
		t.Error("expected callback to be registered")
	}
	onReleaseActivated(context.Background(), "t", "p", "b", "e")
	if !called {
		t.Error("expected callback to be called")
	}
	// Clean up
	RegisterReleaseActivatedCallback(nil)
}
