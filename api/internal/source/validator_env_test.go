// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Plan v1.2 T1.3 (cutover-deep-review-hardening): reserved env key
// blocklist + schema patternProperties + maxProperties + maxLength tests.

package source

import (
	"fmt"
	"strings"
	"testing"
)

func validServicesJSONWithEnv(env map[string]string) string {
	envFragments := make([]string, 0, len(env))
	for k, v := range env {
		envFragments = append(envFragments, fmt.Sprintf("%q:%q", k, v))
	}
	envBlock := ""
	if len(envFragments) > 0 {
		envBlock = fmt.Sprintf(`, "env": {%s}`, strings.Join(envFragments, ","))
	}
	return fmt.Sprintf(`{
		"version": 1,
		"services": [
			{
				"name": "web",
				"runtime": "node",
				"port": 8080,
				"proxy": "/api",
				"dev": "npm run dev",
				"start": "npm start",
				"healthcheck": "/health",
				"cors": false,
				"passSetCookie": false%s
			}
		]
	}`, envBlock)
}

// T1.3 — RED test #1: reserved key PATH MUST be rejected with E-010.
func TestValidateServicesJSON_ReservedEnv_PATH_Rejected(t *testing.T) {
	t.Setenv("LOOPZERO_ENV_BLOCKLIST_MODE", "enforce")
	content := validServicesJSONWithEnv(map[string]string{"PATH": "/usr/bin"})
	_, _, err := ValidateServicesJSON(content)
	if err == nil {
		t.Fatal("expected validation error for reserved env key PATH, got nil")
	}
	if !strings.Contains(err.Error(), "E-010") {
		t.Errorf("expected E-010 marker in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "PATH") {
		t.Errorf("expected error to mention 'PATH', got: %v", err)
	}
}

// T1.3 — RED test #2: reserved prefix LD_LIBRARY_PATH rejected.
func TestValidateServicesJSON_ReservedEnv_LDLibraryPath_Rejected(t *testing.T) {
	t.Setenv("LOOPZERO_ENV_BLOCKLIST_MODE", "enforce")
	content := validServicesJSONWithEnv(map[string]string{"LD_LIBRARY_PATH": "/lib"})
	_, _, err := ValidateServicesJSON(content)
	if err == nil {
		t.Fatal("expected validation error for LD_LIBRARY_PATH, got nil")
	}
}

// T1.3 — RED test #3: prefix KUBERNETES_SERVICE_HOST rejected.
func TestValidateServicesJSON_ReservedEnv_KubernetesPrefix_Rejected(t *testing.T) {
	t.Setenv("LOOPZERO_ENV_BLOCKLIST_MODE", "enforce")
	content := validServicesJSONWithEnv(map[string]string{"KUBERNETES_SERVICE_HOST": "10.0.0.1"})
	_, _, err := ValidateServicesJSON(content)
	if err == nil {
		t.Fatal("expected validation error for KUBERNETES_SERVICE_HOST, got nil")
	}
}

// T1.3 — RED test #4: prefix LOOPZERO_AUDIT_KEY rejected (platform-reserved).
func TestValidateServicesJSON_ReservedEnv_LOOPZEROPrefix_Rejected(t *testing.T) {
	t.Setenv("LOOPZERO_ENV_BLOCKLIST_MODE", "enforce")
	content := validServicesJSONWithEnv(map[string]string{"LOOPZERO_AUDIT_KEY": "x"})
	_, _, err := ValidateServicesJSON(content)
	if err == nil {
		t.Fatal("expected validation error for LOOPZERO_AUDIT_KEY, got nil")
	}
}

// T1.3 — RED test #5: app-scoped key DATABASE_URL passes through.
func TestValidateServicesJSON_AppScopedEnv_DATABASE_URL_OK(t *testing.T) {
	t.Setenv("LOOPZERO_ENV_BLOCKLIST_MODE", "enforce")
	content := validServicesJSONWithEnv(map[string]string{
		"DATABASE_URL": "postgres://localhost/app",
		"API_KEY":      "secret",
		"NODE_ENV":     "production",
	})
	cfg, _, err := ValidateServicesJSON(content)
	if err != nil {
		t.Fatalf("expected no error for app-scoped env, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil cfg")
	}
}

// T1.3 — RED test #6: schema patternProperties rejects lowercase keys.
func TestValidateServicesJSON_EnvSchema_LowercaseKey_Rejected(t *testing.T) {
	t.Setenv("LOOPZERO_ENV_BLOCKLIST_MODE", "enforce")
	content := validServicesJSONWithEnv(map[string]string{"database_url": "x"})
	_, _, err := ValidateServicesJSON(content)
	if err == nil {
		t.Fatal("expected validation error for lowercase env key, got nil")
	}
}

// T1.3 — RED test #7: maxLength on value (4097 chars rejected).
func TestValidateServicesJSON_EnvSchema_ValueTooLong_Rejected(t *testing.T) {
	t.Setenv("LOOPZERO_ENV_BLOCKLIST_MODE", "enforce")
	huge := strings.Repeat("x", 4097)
	content := validServicesJSONWithEnv(map[string]string{"BIG": huge})
	_, _, err := ValidateServicesJSON(content)
	if err == nil {
		t.Fatal("expected validation error for >4096 char env value, got nil")
	}
}

// T1.3 — RED test #8: maxProperties 64 cap.
func TestValidateServicesJSON_EnvSchema_TooManyKeys_Rejected(t *testing.T) {
	t.Setenv("LOOPZERO_ENV_BLOCKLIST_MODE", "enforce")
	env := make(map[string]string, 65)
	for i := 0; i < 65; i++ {
		env[fmt.Sprintf("VAR_%d", i)] = "v"
	}
	content := validServicesJSONWithEnv(env)
	_, _, err := ValidateServicesJSON(content)
	if err == nil {
		t.Fatal("expected validation error for >64 env keys, got nil")
	}
}

// T1.3 — RED test #9 (EC-8): warn-only mode accepts reserved key.
func TestValidateServicesJSON_WarnMode_PathAccepted(t *testing.T) {
	t.Setenv("LOOPZERO_ENV_BLOCKLIST_MODE", "warn")
	content := validServicesJSONWithEnv(map[string]string{"PATH": "/usr/bin"})
	_, _, err := ValidateServicesJSON(content)
	if err != nil {
		t.Fatalf("warn mode must accept reserved keys (with log), got error: %v", err)
	}
}

// T1.3 — RED test #10: empty LOOPZERO_ENV_BLOCKLIST_MODE defaults to enforce
// (fail-closed per CLAUDE.md §8).
func TestValidateServicesJSON_EmptyMode_DefaultsToEnforce(t *testing.T) {
	t.Setenv("LOOPZERO_ENV_BLOCKLIST_MODE", "")
	content := validServicesJSONWithEnv(map[string]string{"PATH": "/usr/bin"})
	_, _, err := ValidateServicesJSON(content)
	if err == nil {
		t.Fatal("empty mode must default to enforce, expected error, got nil")
	}
}
