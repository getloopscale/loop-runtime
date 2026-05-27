// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package config

import (
	"os"
	"testing"
	"time"
)

// TestMain seeds LOOPZERO_CLUSTER_ISSUER for ALL tests in this package.
// B14 (MEET-2026-04-23-058): ClusterIssuer is now required in Validate().
// Tests exercising the missing-var behavior must call t.Setenv("LOOPZERO_CLUSTER_ISSUER", "")
// explicitly to override.
func TestMain(m *testing.M) {
	_ = os.Setenv("LOOPZERO_CLUSTER_ISSUER", "test-cluster-issuer")
	os.Exit(m.Run())
}

// B14: happy path — non-empty LOOPZERO_CLUSTER_ISSUER passes Validate().
func TestValidate_RequiresClusterIssuer_HappyPath(t *testing.T) {
	t.Setenv("LOOPZERO_CLUSTER_ISSUER", "letsencrypt-prod")
	cfg := Load()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// B14: empty LOOPZERO_CLUSTER_ISSUER → Validate() returns error.
func TestValidate_RejectsEmptyClusterIssuer(t *testing.T) {
	t.Setenv("LOOPZERO_CLUSTER_ISSUER", "")
	cfg := Load()
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error when LOOPZERO_CLUSTER_ISSUER is empty")
	}
	if !contains(err.Error(), "LOOPZERO_CLUSTER_ISSUER") {
		t.Errorf("error should mention LOOPZERO_CLUSTER_ISSUER, got: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	n, m := len(s), len(substr)
	if m == 0 {
		return 0
	}
	for i := 0; i+m <= n; i++ {
		if s[i:i+m] == substr {
			return i
		}
	}
	return -1
}

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
		DBMaxConns:    5,
		DBMinConns:    10,
		ClusterIssuer: "test-issuer", // B14
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
		DBMaxConns:    25,
		DBMinConns:    5,
		ClusterIssuer: "test-issuer", // B14
	}
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("expected no error for valid pool config, got %v", err)
	}
}

func TestValidate_AcceptsEqualPoolConfig(t *testing.T) {
	cfg := Config{
		DBMaxConns:    10,
		DBMinConns:    10,
		ClusterIssuer: "test-issuer", // B14
	}
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("expected no error for equal pool config, got %v", err)
	}
}

func TestValidate_AcceptsZeroPoolConfig(t *testing.T) {
	// Zero values should not trigger validation (unset config) — EXCEPT ClusterIssuer (B14)
	cfg := Config{ClusterIssuer: "test-issuer"}
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
		ClusterIssuer:         "test-issuer", // B14
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

// --- Cosign Keyless config (M2 T8.0, v1.1 EC-3 of plan 2026-05-27-M07-to-3-85) ---

func TestLoad_CosignKeyless_DefaultsFalse(t *testing.T) {
	// Per v1.1 EC-3: default MUST be false (safe regression — keyed mode preserved).
	for _, k := range []string{"LOOPZERO_COSIGN_KEYLESS", "LOOPZERO_COSIGN_ATTEST_ENABLED", "LOOPZERO_FULCIO_URL", "LOOPZERO_REKOR_URL"} {
		_ = os.Unsetenv(k)
	}
	cfg := Load()
	if cfg.CosignKeyless {
		t.Error("CosignKeyless must default to false when env unset")
	}
	if cfg.CosignAttestEnabled {
		t.Error("CosignAttestEnabled must default to false when env unset")
	}
	if cfg.CosignFulcioURL != "" {
		t.Errorf("CosignFulcioURL must default to empty, got %q", cfg.CosignFulcioURL)
	}
	if cfg.CosignRekorURL != "" {
		t.Errorf("CosignRekorURL must default to empty, got %q", cfg.CosignRekorURL)
	}
}

func TestLoad_CosignKeyless_TrueActivates(t *testing.T) {
	t.Setenv("LOOPZERO_COSIGN_KEYLESS", "true")
	cfg := Load()
	if !cfg.CosignKeyless {
		t.Error(`CosignKeyless must be true when env = "true"`)
	}
}

func TestLoad_CosignAttestEnabled_TrueActivates(t *testing.T) {
	t.Setenv("LOOPZERO_COSIGN_ATTEST_ENABLED", "true")
	cfg := Load()
	if !cfg.CosignAttestEnabled {
		t.Error(`CosignAttestEnabled must be true when env = "true"`)
	}
}

func TestLoad_CosignFulcioURL_PropagatesFromEnv(t *testing.T) {
	t.Setenv("LOOPZERO_FULCIO_URL", "http://fulcio.sigstore-system.svc.cluster.local:80")
	cfg := Load()
	if cfg.CosignFulcioURL != "http://fulcio.sigstore-system.svc.cluster.local:80" {
		t.Errorf("CosignFulcioURL not propagated, got %q", cfg.CosignFulcioURL)
	}
}

func TestLoad_CosignRekorURL_PropagatesFromEnv(t *testing.T) {
	t.Setenv("LOOPZERO_REKOR_URL", "http://rekor-server.sigstore-system.svc.cluster.local:80")
	cfg := Load()
	if cfg.CosignRekorURL != "http://rekor-server.sigstore-system.svc.cluster.local:80" {
		t.Errorf("CosignRekorURL not propagated, got %q", cfg.CosignRekorURL)
	}
}

func TestLoad_CosignKeyless_InvalidBoolDefaultsFalse(t *testing.T) {
	// envBool helper falls back to default on parse error (logs warning).
	t.Setenv("LOOPZERO_COSIGN_KEYLESS", "garbage")
	cfg := Load()
	if cfg.CosignKeyless {
		t.Error("CosignKeyless must remain false when env is unparseable bool")
	}
}
