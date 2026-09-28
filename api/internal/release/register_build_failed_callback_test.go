// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"context"
	"testing"
)

// M9 Step 6: exercises the release context's own build-failed callback seam
// (onBuildFailed private var); moved out of a build test file so it asserts the
// private var natively.

func TestRegisterBuildFailedCallback(t *testing.T) {
	called := false
	RegisterBuildFailedCallback(func(ctx context.Context, _, _, _, _ string) {
		called = true
	})
	if onBuildFailed == nil {
		t.Error("expected callback to be registered")
	}
	onBuildFailed(context.Background(), "t", "p", "sha", "b")
	if !called {
		t.Error("expected callback to be called")
	}
	// Clean up
	RegisterBuildFailedCallback(nil)
}
