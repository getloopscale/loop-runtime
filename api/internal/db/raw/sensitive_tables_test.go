// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package raw

import "testing"

func TestSensitiveTables_PopulatedWithCanonicalEntries(t *testing.T) {
	// The list MUST contain the 9 canonical sensitive tables
	// documented in the README and ADR-1 §6 D5.
	want := []string{
		"secrets",
		"audit_log",
		"users",
		"tenants",
		"tenant_members",
		"subscriptions",
		"tenants_pending_deletion",
		"tenant_tombstones",
		"cells",
	}
	if len(SensitiveTables) != len(want) {
		t.Fatalf("SensitiveTables length: want %d got %d", len(want), len(SensitiveTables))
	}
	for i, w := range want {
		if SensitiveTables[i] != w {
			t.Errorf("SensitiveTables[%d]: want %q got %q", i, w, SensitiveTables[i])
		}
	}
}

func TestIsSensitive_RecognizesAllCanonical(t *testing.T) {
	cases := []struct {
		table     string
		sensitive bool
	}{
		// Canonical sensitive
		{"secrets", true},
		{"audit_log", true},
		{"users", true},
		{"tenants", true},
		{"tenant_members", true},
		{"subscriptions", true},
		{"tenants_pending_deletion", true},
		{"tenant_tombstones", true},
		{"cells", true},
		// Case-insensitive
		{"SECRETS", true},
		{"Audit_Log", true},
		// Non-sensitive (raw allowed)
		{"builds", false},
		{"releases", false},
		{"projects", false},
		{"environments", false},
		{"environment_targets", false},
		// Edge case: similar prefix but distinct table
		{"secret_metadata", false}, // not "secrets"
		{"user_settings", false},   // not "users"
	}
	for _, tc := range cases {
		if got := IsSensitive(tc.table); got != tc.sensitive {
			t.Errorf("IsSensitive(%q): want %v got %v", tc.table, tc.sensitive, got)
		}
	}
}
