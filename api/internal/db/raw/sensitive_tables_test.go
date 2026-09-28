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
