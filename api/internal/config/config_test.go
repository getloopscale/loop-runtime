// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package config

import (
	"os"
	"testing"
	"time"
)

// Roadmap 02 — API Loop Extraction feature flag.
//
// DisableLegacyLoops is the switch that tells the API process not to run
// its background reconciliation goroutines. The default (unset / "false")
// must preserve pre-extraction behavior. Any value except the exact
// literal "true" must also default to OFF to avoid accidental activation
// via a typo (e.g. "True", "yes", "1").

func TestLoad_DisableLegacyLoops_DefaultsFalse(t *testing.T) {
	_ = os.Unsetenv("LOOPZERO_DISABLE_LEGACY_LOOPS")
	cfg := Load()
	if cfg.DisableLegacyLoops {
		t.Error("DisableLegacyLoops must default to false when env is unset")
	}
}

func TestLoad_DisableLegacyLoops_TrueActivates(t *testing.T) {
	t.Setenv("LOOPZERO_DISABLE_LEGACY_LOOPS", "true")
	cfg := Load()
	if !cfg.DisableLegacyLoops {
		t.Error(`DisableLegacyLoops must be true when env = "true"`)
	}
}

func TestLoad_AdmissionControl_DefaultsFalse(t *testing.T) {
	_ = os.Unsetenv("LOOPZERO_ADMISSION_CONTROL")
	cfg := Load()
	if cfg.AdmissionControl {
		t.Error("AdmissionControl must default to false when env is unset")
	}
}

func TestLoad_AdmissionControl_TrueActivates(t *testing.T) {
	t.Setenv("LOOPZERO_ADMISSION_CONTROL", "true")
	cfg := Load()
	if !cfg.AdmissionControl {
		t.Error(`AdmissionControl must be true when env = "true"`)
	}
}

func TestLoad_AdmissionControl_TypoRejected(t *testing.T) {
	// Same typo-safety contract as DisableLegacyLoops: only the exact
	// literal "true" activates the flag. Anything else (including "TRUE",
	// "1", "yes") is treated as off.
	cases := []string{"TRUE", "True", "1", "yes", "on"}
	for _, v := range cases {
		t.Run(v, func(t *testing.T) {
			t.Setenv("LOOPZERO_ADMISSION_CONTROL", v)
			cfg := Load()
			if cfg.AdmissionControl {
				t.Errorf("value %q must not enable AdmissionControl", v)
			}
		})
	}
}

func TestLoad_DisableLegacyLoops_NonTrueRejected(t *testing.T) {
	// Anything other than the exact literal "true" is treated as false —
	// conservative default to avoid accidental loop disable via typo.
	cases := []string{"True", "TRUE", "1", "yes", "on", "false", "0", "off"}
	for _, v := range cases {
		t.Run(v, func(t *testing.T) {
			t.Setenv("LOOPZERO_DISABLE_LEGACY_LOOPS", v)
			cfg := Load()
			if cfg.DisableLegacyLoops {
				t.Errorf("value %q must not enable DisableLegacyLoops (only exact %q does)", v, "true")
			}
		})
	}
}

func TestValidate_RejectsMaxConnsLessThanMinConns(t *testing.T) {
	cfg := Config{
		DBMaxConns: 5,
		DBMinConns: 10,
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for DBMaxConns < DBMinConns, got nil")
	}
	if want := "DB_MAX_CONNS (5) must be >= DB_MIN_CONNS (10)"; err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestValidate_AcceptsValidPoolConfig(t *testing.T) {
	cfg := Config{
		DBMaxConns: 25,
		DBMinConns: 5,
	}
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("expected no error for valid pool config, got %v", err)
	}
}

func TestValidate_AcceptsEqualPoolConfig(t *testing.T) {
	cfg := Config{
		DBMaxConns: 10,
		DBMinConns: 10,
	}
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("expected no error for equal pool config, got %v", err)
	}
}

func TestValidate_AcceptsZeroPoolConfig(t *testing.T) {
	// Zero values should not trigger validation (unset config)
	cfg := Config{}
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("expected no error for zero config, got %v", err)
	}
}

func TestValidate_ProductionRejectsDefaultCredentials(t *testing.T) {
	cfg := Config{
		Env:            "production",
		MinIOAccessKey: "loopzero-admin",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for default MinIO credentials in production")
	}
}

func TestValidate_ProductionRejectsInsecureRegistry(t *testing.T) {
	cfg := Config{
		Env:              "production",
		RegistryInsecure: true,
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for insecure registry in production")
	}
}

func TestValidate_WarnsReleasePendingLessThanSyncTimeout(t *testing.T) {
	// This should warn but not error (cross-field validation)
	cfg := Config{
		ReleasePendingTimeout: 1 * time.Minute,
		ArgocdSyncTimeout:     5 * time.Minute,
	}
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("expected warning (not error) for timeout mismatch, got %v", err)
	}
}

// WS.1: envTime and ServiceAuthSecretCreatedAt tests.

func TestEnvTime_ParsesRFC3339(t *testing.T) {
	t.Setenv("LOOPZERO_SERVICE_AUTH_SECRET_CREATED_AT", "2026-01-15T10:30:00Z")
	got := envTime("LOOPZERO_SERVICE_AUTH_SECRET_CREATED_AT")
	expected := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	if !got.Equal(expected) {
		t.Errorf("envTime = %v, want %v", got, expected)
	}
}

func TestEnvTime_ReturnsZeroWhenUnset(t *testing.T) {
	_ = os.Unsetenv("LOOPZERO_SERVICE_AUTH_SECRET_CREATED_AT")
	got := envTime("LOOPZERO_SERVICE_AUTH_SECRET_CREATED_AT")
	if !got.IsZero() {
		t.Errorf("envTime should return zero time when unset, got %v", got)
	}
}

func TestEnvTime_ReturnsZeroOnInvalidFormat(t *testing.T) {
	t.Setenv("LOOPZERO_SERVICE_AUTH_SECRET_CREATED_AT", "not-a-timestamp")
	got := envTime("LOOPZERO_SERVICE_AUTH_SECRET_CREATED_AT")
	if !got.IsZero() {
		t.Errorf("envTime should return zero time on invalid format, got %v", got)
	}
}

func TestLoad_ServiceAuthSecretCreatedAt_ParsesFromEnv(t *testing.T) {
	t.Setenv("LOOPZERO_SERVICE_AUTH_SECRET_CREATED_AT", "2026-03-01T00:00:00Z")
	cfg := Load()
	expected := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if !cfg.ServiceAuthSecretCreatedAt.Equal(expected) {
		t.Errorf("ServiceAuthSecretCreatedAt = %v, want %v", cfg.ServiceAuthSecretCreatedAt, expected)
	}
}

func TestLoad_ServiceAuthSecretCreatedAt_DefaultsToZero(t *testing.T) {
	_ = os.Unsetenv("LOOPZERO_SERVICE_AUTH_SECRET_CREATED_AT")
	cfg := Load()
	if !cfg.ServiceAuthSecretCreatedAt.IsZero() {
		t.Errorf("ServiceAuthSecretCreatedAt should be zero when unset, got %v", cfg.ServiceAuthSecretCreatedAt)
	}
}
