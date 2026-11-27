// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// Plan v1.2 T3.1 — shared test helper. Render now returns (string, error);
// tests that previously passed the result inline (e.g.
// `parseDeployment(t, Render(input))`) use mustRender to keep the same
// single-value call shape while still failing the test on a real error.

package manifests

import "testing"

func mustRender(t *testing.T, input RenderInput) string {
	t.Helper()
	out, err := Render(input)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return out
}
