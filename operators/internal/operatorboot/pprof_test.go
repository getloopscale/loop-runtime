// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package operatorboot

import (
	"testing"
)

// TestPprofEnabled exercises operatorboot.PprofEnabled (the canonical pprof gate
// used by all 4 split binaries via cmd/{tenant,app,preview,platform}-operator).
//
// Migrated from cmd/pprof_test.go which exercised the monolith's
// pprofEnabled() helper (deleted in T1.4).
func TestPprofEnabled(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		setEnv   bool
		want     bool
	}{
		{"unset defaults to false", "", false, false},
		{"empty string defaults to false", "", true, false},
		{"true enables", "true", true, true},
		{"1 enables", "1", true, true},
		{"false disables", "false", true, false},
		{"0 disables", "0", true, false},
		{"TRUE does not enable (case-sensitive)", "TRUE", true, false},
		{"yes does not enable", "yes", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv("LOOPRUNTIME_PPROF_ENABLED", tt.envValue)
			}
			if got := PprofEnabled(); got != tt.want {
				t.Errorf("PprofEnabled() = %v, want %v (env=%q, set=%v)", got, tt.want, tt.envValue, tt.setEnv)
			}
		})
	}
}
