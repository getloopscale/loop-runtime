// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import "testing"

func TestSeedDispatch_NoArgsExit1(t *testing.T) {
	if code := runSeed(nil); code != 1 {
		t.Errorf("seed with no args: exit %d, want 1 (usage)", code)
	}
}

func TestSeedDispatch_UnknownSeedExit1(t *testing.T) {
	if code := runSeed([]string{"does-not-exist"}); code != 1 {
		t.Errorf("seed unknown: exit %d, want 1", code)
	}
}

func TestSeedDispatch_HelpExit0(t *testing.T) {
	if code := runSeed([]string{"--help"}); code != 0 {
		t.Errorf("seed --help: exit %d, want 0", code)
	}
}

func TestSeedDispatch_ListExit0(t *testing.T) {
	// `seed list` prints registered seeds and exits 0 even when the registry
	// is empty (Phase 0 — waves register the actual seeds later).
	if code := runSeed([]string{"list"}); code != 0 {
		t.Errorf("seed list: exit %d, want 0", code)
	}
}

func TestMainDispatch_SeedRouted(t *testing.T) {
	// The top-level `run` must route "seed" to runSeed (wiring, not dead code).
	if code := run([]string{"seed", "--help"}); code != 0 {
		t.Errorf("run seed --help: exit %d, want 0", code)
	}
}
