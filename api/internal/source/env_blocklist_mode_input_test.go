// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package source

import "testing"

// TestBlocklistModeComesFromTheCallerNotTheProcessEnvironment pins that the
// reserved-key enforcement is decided by the mode the composition root passes
// (config.Config.EnvBlocklistMode, validated at startup), not by whatever the
// API process environment holds when a request arrives.
func TestBlocklistModeComesFromTheCallerNotTheProcessEnvironment(t *testing.T) {
	content := validServicesJSONWithEnv(map[string]string{"PATH": "/usr/bin"})

	t.Run("enforce refuses although the process env says warn", func(t *testing.T) {
		t.Setenv("LOOPRUNTIME_ENV_BLOCKLIST_MODE", "warn")

		_, err := ValidateServicesJSONNative(content, EnvBlocklistEnforce)

		if err == nil {
			t.Fatal("enforce mode accepted a reserved key")
		}
	})

	t.Run("warn accepts although the process env says enforce", func(t *testing.T) {
		t.Setenv("LOOPRUNTIME_ENV_BLOCKLIST_MODE", "enforce")

		_, err := ValidateServicesJSONNative(content, EnvBlocklistWarn)

		if err != nil {
			t.Fatalf("warn mode refused a reserved key: %v", err)
		}
	})
}
