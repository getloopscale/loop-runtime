// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Per plan dashboard-memory-bff-server-side-key-vault v1.2 § T3.1.
// Plus F-rev-5 fix-cycle absorption (2026-06-08): 4 promised but absent tests.

package config

import (
	"os"
	"testing"
)

// =========================================================================
// envOrInt — promised by plan v1.2 § T3.1 TDD block (4 of 7 tests)
// =========================================================================

func TestConfig_ParsesBFFKeyTTLHoursFromEnv(t *testing.T) {
	t.Setenv("LOOPZEROCLOUD_BFF_KEY_TTL_HOURS", "12")
	cfg := Load()
	if cfg.BFFKeyTTLHours != 12 {
		t.Fatalf("expected BFFKeyTTLHours=12; got %d", cfg.BFFKeyTTLHours)
	}
}

func TestConfig_DefaultsBFFKeyTTLHoursTo24WhenEnvUnset(t *testing.T) {
	// Ensure env var absent
	_ = os.Unsetenv("LOOPZEROCLOUD_BFF_KEY_TTL_HOURS")
	cfg := Load()
	if cfg.BFFKeyTTLHours != 24 {
		t.Fatalf("expected default BFFKeyTTLHours=24; got %d", cfg.BFFKeyTTLHours)
	}
}

func TestConfig_FallsBackToDefaultOnInvalidEnv(t *testing.T) {
	t.Setenv("LOOPZEROCLOUD_BFF_KEY_TTL_HOURS", "not-a-number")
	cfg := Load()
	// envOrInt returns fallback (24) on parse failure; range validation
	// happens in NewBFFKeyVault constructor (fail-loud at consumer).
	if cfg.BFFKeyTTLHours != 24 {
		t.Fatalf("expected fallback BFFKeyTTLHours=24 on parse error; got %d", cfg.BFFKeyTTLHours)
	}
}

func TestConfig_ParsesNumericEdgeValues(t *testing.T) {
	cases := []struct {
		envValue string
		expected int
	}{
		{"1", 1},     // floor
		{"168", 168}, // ceiling (1 week)
		{"24", 24},   // default
		{"0", 0},     // invalid range but parses; vault constructor rejects
		{"-5", -5},   // invalid; vault rejects
		{"1000", 1000}, // invalid; vault rejects
	}
	for _, tc := range cases {
		t.Run(tc.envValue, func(t *testing.T) {
			t.Setenv("LOOPZEROCLOUD_BFF_KEY_TTL_HOURS", tc.envValue)
			cfg := Load()
			if cfg.BFFKeyTTLHours != tc.expected {
				t.Fatalf("env=%q: expected %d; got %d", tc.envValue, tc.expected, cfg.BFFKeyTTLHours)
			}
		})
	}
}
