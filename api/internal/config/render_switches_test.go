// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package config

import "testing"

func TestLoadProgressiveDeliveryIsOnUnlessExplicitlyDisabled(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"", true},
		{"true", true},
		{"yes", true},
		{"false", false},
		{"FALSE", false},
		{" 0 ", false},
	} {
		t.Run("value="+tc.value, func(t *testing.T) {
			t.Setenv("LOOPZERO_PROGRESSIVE_DELIVERY", tc.value)

			cfg := Load()

			if cfg.ProgressiveDelivery != tc.want {
				t.Fatalf("LOOPZERO_PROGRESSIVE_DELIVERY=%q: ProgressiveDelivery = %v, want %v", tc.value, cfg.ProgressiveDelivery, tc.want)
			}
		})
	}
}

func TestLoadLeanResourcesIsOffUnlessExplicitlyEnabled(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"", false},
		{"false", false},
		{"no", false},
		{"off", false},
		{"true", true},
		{"TRUE", true},
		{" 1 ", true},
	} {
		t.Run("value="+tc.value, func(t *testing.T) {
			t.Setenv("LOOPZERO_LEAN_RESOURCES", tc.value)

			cfg := Load()

			if cfg.LeanResources != tc.want {
				t.Fatalf("LOOPZERO_LEAN_RESOURCES=%q: LeanResources = %v, want %v", tc.value, cfg.LeanResources, tc.want)
			}
		})
	}
}
