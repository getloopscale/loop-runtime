// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package config

import (
	"testing"
)

func TestPprofEnabledAcceptsOnlyTrueOrOne(t *testing.T) {
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
				t.Setenv("LOOP_RUNTIME_PPROF_ENABLED", tt.envValue)
			}
			if got := Load().PprofEnabled; got != tt.want {
				t.Errorf("Load().PprofEnabled = %v, want %v (env=%q, set=%v)", got, tt.want, tt.envValue, tt.setEnv)
			}
		})
	}
}

func TestPprofListensOn6060UnlessAnAddressIsGiven(t *testing.T) {
	if got := Load().PprofAddr; got != ":6060" {
		t.Fatalf("default PprofAddr = %q, want :6060", got)
	}
	t.Setenv("LOOP_RUNTIME_PPROF_ADDR", "127.0.0.1:7070")
	if got := Load().PprofAddr; got != "127.0.0.1:7070" {
		t.Fatalf("PprofAddr = %q, want the LOOP_RUNTIME_PPROF_ADDR value", got)
	}
}

func TestEnvReadinessCheckerIsOffUnlessExactlyTrue(t *testing.T) {
	t.Setenv("LOOP_RUNTIME_ENV_READINESS_CHECKER", "1")
	if Load().EnvReadinessChecker {
		t.Fatal(`EnvReadinessChecker must stay off for anything but "true"`)
	}
	t.Setenv("LOOP_RUNTIME_ENV_READINESS_CHECKER", "true")
	if !Load().EnvReadinessChecker {
		t.Fatal(`EnvReadinessChecker must be on for "true"`)
	}
}
