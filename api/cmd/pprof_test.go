// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"testing"
)

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
				t.Setenv("LOOPZERO_PPROF_ENABLED", tt.envValue)
			}
			if got := pprofEnabled(); got != tt.want {
				t.Errorf("pprofEnabled() = %v, want %v (env=%q, set=%v)", got, tt.want, tt.envValue, tt.setEnv)
			}
		})
	}
}
