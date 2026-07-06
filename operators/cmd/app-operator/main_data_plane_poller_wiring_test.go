// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// T2.2 hermetic wiring guard — plan dashboard-badge-data-plane-poller.
// Given the app-operator main.go source, When grep-inspected, Then it
// must wire the DataPlanePoller behind a LOOPZERO_DATA_PLANE_POLLER_ENABLED
// env gate + register it on the controller-runtime manager via mgr.Add.
// Zero-cost regression protector: any future refactor that drops the
// wiring (or its env gate) is caught at CI time before the next release.

package main

import (
	"os"
	"strings"
	"testing"
)

// TestMain_WiresDataPlanePoller_WhenEnabled asserts the composition-root
// contract for T2.2 of plan dashboard-badge-data-plane-poller.
func TestMain_WiresDataPlanePoller_WhenEnabled(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	content := string(src)

	required := []struct {
		fragment, why string
	}{
		{"LOOPZERO_DATA_PLANE_POLLER_ENABLED", "env gate name required so operators can toggle without recompile"},
		{"NewDataPlanePoller", "poller constructor must be called from the composition root"},
		{"mgr.Add(", "poller must be registered on the controller-runtime manager"},
		{"LOOPZERO_DATA_PLANE_POLLER_INTERVAL", "poll interval must be env-overridable"},
	}
	for _, r := range required {
		if !strings.Contains(content, r.fragment) {
			t.Errorf("main.go missing wiring fragment %q — %s", r.fragment, r.why)
		}
	}
}
