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
		Env:           "development", // Sprint A T2.1: allowlist requires canonical value
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
		Env:           "development", // Sprint A T2.1: allowlist requires canonical value
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
		Env:           "development", // Sprint A T2.1: allowlist requires canonical value
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
	// + Env (Sprint A T2.1 allowlist).
	cfg := Config{Env: "development", ClusterIssuer: "test-issuer"}
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
		Env:                   "development", // Sprint A T2.1: allowlist requires canonical value
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

// T1.2 of plan 2026-05-28-meet-001-engineering-closure v1.1.
// CR-1 fix from MEET-2026-05-28-001: gate `--insecure-skip-verify` flag.

func TestValidate_RejectsInsecureSkipSCT_InProd_WithoutTUF(t *testing.T) {
	t.Setenv("LOOPZERO_CLUSTER_ISSUER", "letsencrypt-prod")
	t.Setenv("LOOPZERO_ENV", "production")
	t.Setenv("LOOPZERO_COSIGN_INSECURE_SKIP_SCT_VERIFY", "true")
	t.Setenv("LOOPZERO_COSIGN_TUF_ROOT_PINNED", "false")
	t.Setenv("LOOPZERO_SECRET_ENCRYPTION_KEY", "test-key")
	t.Setenv("REGISTRY_URL", "registry.example.com/loopzero-tenants")
	cfg := Load()
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected Validate error when INSECURE_SKIP_SCT=true && prod && !TUF_ROOT_PINNED")
	}
	if !contains(err.Error(), "INSECURE_SKIP_SCT_VERIFY") {
		t.Errorf("error must mention INSECURE_SKIP_SCT_VERIFY, got: %v", err)
	}
}

func TestValidate_AcceptsInsecureSkipSCT_InDevelopment(t *testing.T) {
	// Sprint A T2.1 (2026-06-01): renamed from _InLocal — LOOPZERO_ENV
	// allowlist now strict {development, staging, production}. "local"
	// was never canonical; "development" is the canonical non-prod env.
	t.Setenv("LOOPZERO_CLUSTER_ISSUER", "loopzero-ca-issuer")
	t.Setenv("LOOPZERO_ENV", "development")
	t.Setenv("LOOPZERO_COSIGN_INSECURE_SKIP_SCT_VERIFY", "true")
	t.Setenv("LOOPZERO_COSIGN_TUF_ROOT_PINNED", "false")
	cfg := Load()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate should accept INSECURE_SKIP_SCT in DEVELOPMENT: %v", err)
	}
}

func TestValidate_AcceptsInsecureSkipSCT_InProd_WithTUF(t *testing.T) {
	// Asserts: when TUF is pinned, Validate() does NOT raise the
	// INSECURE_SKIP_SCT error. Other unrelated prod gates may still error
	// (CORS, MINIO, etc.), but the SCT-specific gate must pass.
	cfg := Config{
		Env:                   "production",
		CosignInsecureSkipSCT: true,
		CosignTUFRootPinned:   true,
	}
	err := cfg.Validate()
	if err != nil && contains(err.Error(), "INSECURE_SKIP_SCT_VERIFY") {
		t.Fatalf("Validate must NOT raise INSECURE_SKIP_SCT_VERIFY error when TUF pinned: %v", err)
	}
}

func TestLoad_CosignInsecureSkipSCT_DefaultsFalse(t *testing.T) {
	// Default = false (safe by default)
	cfg := Load()
	if cfg.CosignInsecureSkipSCT {
		t.Error("CosignInsecureSkipSCT default must be false (safe by default)")
	}
	if cfg.CosignTUFRootPinned {
		t.Error("CosignTUFRootPinned default must be false (no ceremony performed)")
	}
}

// =============================================================================
// Sprint A T2.1 — LOOPZERO_ENV allowlist (closes sec-008 / CR-1 partial)
// =============================================================================

func TestValidate_AcceptsCanonicalDevelopment(t *testing.T) {
	cfg := Config{Env: "development", ClusterIssuer: "test-issuer"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected nil for canonical development, got %v", err)
	}
}

func TestValidate_AcceptsCanonicalStaging(t *testing.T) {
	cfg := Config{Env: "staging", ClusterIssuer: "test-issuer", RegistryURL: "registry.example.com/loopzero-tenants"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected nil for canonical staging, got %v", err)
	}
}

func TestValidate_AcceptsCanonicalProduction(t *testing.T) {
	// production requires many additional gates to pass — we ONLY assert
	// the LOOPZERO_ENV allowlist gate does NOT raise. Any error returned MUST
	// NOT mention "LOOPZERO_ENV" (other prod gates may still fire — that's fine).
	cfg := Config{
		Env:           "production",
		ClusterIssuer: "letsencrypt-prod",
	}
	err := cfg.Validate()
	if err != nil && contains(err.Error(), "LOOPZERO_ENV=") {
		t.Errorf("Validate raised LOOPZERO_ENV allowlist error for canonical production: %v", err)
	}
}

func TestValidate_RejectsCaseSensitiveProduction(t *testing.T) {
	cfg := Config{Env: "Production", ClusterIssuer: "test-issuer"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for LOOPZERO_ENV=Production (case bypass), got nil")
	}
	if !contains(err.Error(), `LOOPZERO_ENV="Production"`) {
		t.Errorf("error should mention the rejected value, got: %v", err)
	}
	if !contains(err.Error(), "development, staging, production") {
		t.Errorf("error should mention allowed set, got: %v", err)
	}
}

func TestValidate_RejectsAbbreviatedProd(t *testing.T) {
	cfg := Config{Env: "prod", ClusterIssuer: "test-issuer"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for LOOPZERO_ENV=prod (abbreviated), got nil")
	}
	if !contains(err.Error(), `LOOPZERO_ENV="prod"`) {
		t.Errorf("error should mention the rejected value, got: %v", err)
	}
}

func TestValidate_RejectsLeadingWhitespace(t *testing.T) {
	cfg := Config{Env: " production", ClusterIssuer: "test-issuer"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for LOOPZERO_ENV with leading whitespace, got nil")
	}
}

// =============================================================================
// T3.1 of plan 2026-06-01-p3-infra-observability-test-debt v1.1 ADR D3 —
// WorkerDatabaseURL / EffectiveWorkerDatabaseURL / WorkerRoleEnabled
// =============================================================================

// TestLoad_WorkerDatabaseURL_DefaultsEmpty: when LOOPZERO_WORKER_DATABASE_URL
// is unset, Load() returns Config.WorkerDatabaseURL=="" — caller falls
// back to DatabaseURL (legacy behavior, RLS bypass via owner).
func TestLoad_WorkerDatabaseURL_DefaultsEmpty(t *testing.T) {
	_ = os.Unsetenv("LOOPZERO_WORKER_DATABASE_URL")
	cfg := Load()
	if cfg.WorkerDatabaseURL != "" {
		t.Errorf("expected empty, got %q", cfg.WorkerDatabaseURL)
	}
}

// TestLoad_WorkerDatabaseURL_PopulatesFromEnv: env set → field set.
func TestLoad_WorkerDatabaseURL_PopulatesFromEnv(t *testing.T) {
	t.Setenv("LOOPZERO_WORKER_DATABASE_URL", "postgres://loopzero_worker:secret@db:5432/loopzero?sslmode=require")
	cfg := Load()
	if cfg.WorkerDatabaseURL != "postgres://loopzero_worker:secret@db:5432/loopzero?sslmode=require" {
		t.Errorf("WorkerDatabaseURL drift: %q", cfg.WorkerDatabaseURL)
	}
}

// TestEffectiveWorkerDatabaseURL_FallsBackToDatabaseURL: helper returns
// DatabaseURL when WorkerDatabaseURL is empty — operator-controlled
// rollout via env var; works in either state.
func TestEffectiveWorkerDatabaseURL_FallsBackToDatabaseURL(t *testing.T) {
	cfg := Config{
		DatabaseURL:       "postgres://loopzero:pass@db:5432/loopzero",
		WorkerDatabaseURL: "",
	}
	if got := cfg.EffectiveWorkerDatabaseURL(); got != cfg.DatabaseURL {
		t.Errorf("expected fallback to DatabaseURL, got %q", got)
	}
}

// TestEffectiveWorkerDatabaseURL_PrefersWorkerURL: when set, the helper
// returns the worker URL.
func TestEffectiveWorkerDatabaseURL_PrefersWorkerURL(t *testing.T) {
	cfg := Config{
		DatabaseURL:       "postgres://loopzero:pass@db:5432/loopzero",
		WorkerDatabaseURL: "postgres://loopzero_worker:other@db:5432/loopzero",
	}
	if got := cfg.EffectiveWorkerDatabaseURL(); got != cfg.WorkerDatabaseURL {
		t.Errorf("expected worker URL, got %q", got)
	}
}

// TestWorkerRoleEnabled_FalseWhenEmpty: legacy mode reports false.
func TestWorkerRoleEnabled_FalseWhenEmpty(t *testing.T) {
	cfg := Config{
		DatabaseURL:       "postgres://loopzero:pass@db:5432/loopzero",
		WorkerDatabaseURL: "",
	}
	if cfg.WorkerRoleEnabled() {
		t.Error("WorkerRoleEnabled MUST be false when WorkerDatabaseURL is empty")
	}
}

// TestWorkerRoleEnabled_TrueWhenDistinct: worker URL distinct from main
// → reports true (the operator has activated the role split).
func TestWorkerRoleEnabled_TrueWhenDistinct(t *testing.T) {
	cfg := Config{
		DatabaseURL:       "postgres://loopzero:pass@db:5432/loopzero",
		WorkerDatabaseURL: "postgres://loopzero_worker:other@db:5432/loopzero",
	}
	if !cfg.WorkerRoleEnabled() {
		t.Error("WorkerRoleEnabled MUST be true when WorkerDatabaseURL distinct from DatabaseURL")
	}
}

// TestWorkerRoleEnabled_FalseWhenSame: if operator accidentally points
// both to the same URL, the role split is NOT active — defense-in-depth
// signal that the worker role is not being exercised.
func TestWorkerRoleEnabled_FalseWhenSame(t *testing.T) {
	url := "postgres://loopzero:pass@db:5432/loopzero"
	cfg := Config{
		DatabaseURL:       url,
		WorkerDatabaseURL: url,
	}
	if cfg.WorkerRoleEnabled() {
		t.Error("WorkerRoleEnabled MUST be false when both URLs point to the same connection (no split)")
	}
}

// TestValidate_WorkerDatabaseURL_RejectsSSLModeDisableInProd: the SSL-
// mode-disable check is symmetric to the DatabaseURL one (same H1
// reasoning: plaintext credentials in transit). Verified by constructing
// Config directly with the minimum production fields required to REACH
// the new check (skips the full env-loading cascade that needs 15+
// env vars). The error message naming the env var is the consumer
// contract — operators reading from CrashLoopBackOff logs see exactly
// which URL is wrong.
func TestValidate_WorkerDatabaseURL_RejectsSSLModeDisableInProd(t *testing.T) {
	cfg := minProdConfig(t)
	cfg.WorkerDatabaseURL = "postgres://loopzero_worker:pass@db:5432/loopzero?sslmode=disable"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected Validate() to reject WorkerDatabaseURL with sslmode=disable in production")
	}
	if !contains(err.Error(), "LOOPZERO_WORKER_DATABASE_URL") {
		t.Errorf("error must name the env var; got: %v", err)
	}
	if !contains(err.Error(), "sslmode=disable") {
		t.Errorf("error must explain the problem; got: %v", err)
	}
}

// TestValidate_WorkerDatabaseURL_AcceptsSSLModeRequireInProd: the
// canonical production form passes (or fails downstream — we only
// require the WorkerDatabaseURL check NOT to be the failing branch).
func TestValidate_WorkerDatabaseURL_AcceptsSSLModeRequireInProd(t *testing.T) {
	cfg := minProdConfig(t)
	cfg.WorkerDatabaseURL = "postgres://loopzero_worker:pass@db:5432/loopzero?sslmode=require"
	if err := cfg.Validate(); err != nil && contains(err.Error(), "LOOPZERO_WORKER_DATABASE_URL") {
		t.Errorf("Validate MUST NOT reject sslmode=require for WorkerDatabaseURL; got: %v", err)
	}
}

// minProdConfig returns a Config{} populated with the MINIMUM set of
// fields required for Validate() to reach the WorkerDatabaseURL check
// in production mode. Other prod-validations (CORS, secret encryption,
// argo callback, etc.) are pre-satisfied with safe non-default values.
// Tests that need a different production-failure path should override
// the relevant field BEFORE calling Validate.
func minProdConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		Env:                 "production",
		ClusterIssuer:       "letsencrypt-prod",
		ArgoCallbackSecret:  "non-default-secret-value",
		SecretEncryptionKey: "REDACTED_SECRET_010",
		CORSAllowedOrigin:   "https://app.loopzero.dev",
		DatabaseURL:         "postgres://loopzero:pass@db:5432/loopzero?sslmode=require",
		MinIOAccessKey:      "non-default-minio-access",
		MinIOSecretKey:      "non-default-minio-secret",
		RegistryURL:         "123456789012.dkr.ecr.us-east-1.amazonaws.com/loopzero-tenants",
	}
}

// ============================================================================
// T2.5 + EC-05 of plan 2026-06-10-j22-openbao-transit-eso-sync v1.2 —
// fail-loud boot refusal when OpenBao transit is enabled but credentials
// are missing, AND the crypto-mode precedence (transit > aes-local >
// plaintext) is observable via the CryptoMode() method.
// ============================================================================

// TestCryptoMode_Transit — when OPENBAO_TRANSIT_ENABLED=true, CryptoMode
// returns "transit" regardless of whether LOOPZERO_SECRET_ENCRYPTION_KEY is
// set. Transit precedence is the EC-05 absorption invariant.
func TestCryptoMode_Transit(t *testing.T) {
	c := Config{
		OpenBaoTransitEnabled: true,
		SecretEncryptionKey:   "REDACTED_SECRET_010",
	}
	if got := c.CryptoMode(); got != "transit" {
		t.Errorf("CryptoMode: got %q want transit (transit > aes-local precedence)", got)
	}
}

// TestCryptoMode_AESLocal — only SecretEncryptionKey set → aes-local.
func TestCryptoMode_AESLocal(t *testing.T) {
	c := Config{
		OpenBaoTransitEnabled: false,
		SecretEncryptionKey:   "REDACTED_SECRET_010",
	}
	if got := c.CryptoMode(); got != "aes-local" {
		t.Errorf("CryptoMode: got %q want aes-local", got)
	}
}

// TestCryptoMode_Plaintext — neither configured → plaintext (dev only).
func TestCryptoMode_Plaintext(t *testing.T) {
	c := Config{
		OpenBaoTransitEnabled: false,
		SecretEncryptionKey:   "",
	}
	if got := c.CryptoMode(); got != "plaintext" {
		t.Errorf("CryptoMode: got %q want plaintext", got)
	}
}

// TestValidate_TransitEnabledRequiresAddr — fail-loud (D6 ADR): when
// transit is enabled but OPENBAO_VAULT_ADDR is empty, Validate MUST
// return an error mentioning the missing var so the operator can fix it.
func TestValidate_TransitEnabledRequiresAddr(t *testing.T) {
	c := minProdConfig(t)
	c.OpenBaoTransitEnabled = true
	c.OpenBaoVaultAddr = "" // missing
	c.OpenBaoVaultRoleID = "rid"
	c.OpenBaoVaultSecretID = "sid"

	err := c.Validate()
	if err == nil {
		t.Fatal("Validate should fail when transit enabled + addr empty")
	}
	if !contains(err.Error(), "OPENBAO_VAULT_ADDR") {
		t.Errorf("error should mention OPENBAO_VAULT_ADDR: %v", err)
	}
}

// TestValidate_TransitEnabledRequiresRoleID — fail-loud on missing
// OPENBAO_VAULT_ROLE_ID.
func TestValidate_TransitEnabledRequiresRoleID(t *testing.T) {
	c := minProdConfig(t)
	c.OpenBaoTransitEnabled = true
	c.OpenBaoVaultAddr = "https://openbao.secrets-system.svc:8200"
	c.OpenBaoVaultRoleID = "" // missing
	c.OpenBaoVaultSecretID = "sid"

	err := c.Validate()
	if err == nil {
		t.Fatal("Validate should fail when transit enabled + roleID empty")
	}
	if !contains(err.Error(), "OPENBAO_VAULT_ROLE_ID") {
		t.Errorf("error should mention OPENBAO_VAULT_ROLE_ID: %v", err)
	}
}

// TestValidate_TransitEnabledRequiresSecretID — fail-loud on missing
// OPENBAO_VAULT_SECRET_ID.
func TestValidate_TransitEnabledRequiresSecretID(t *testing.T) {
	c := minProdConfig(t)
	c.OpenBaoTransitEnabled = true
	c.OpenBaoVaultAddr = "https://openbao.secrets-system.svc:8200"
	c.OpenBaoVaultRoleID = "rid"
	c.OpenBaoVaultSecretID = "" // missing

	err := c.Validate()
	if err == nil {
		t.Fatal("Validate should fail when transit enabled + secretID empty")
	}
	if !contains(err.Error(), "OPENBAO_VAULT_SECRET_ID") {
		t.Errorf("error should mention OPENBAO_VAULT_SECRET_ID: %v", err)
	}
}

// TestValidate_TransitConfigCompletePasses — happy path: all 3 transit
// vars set + valid prod config → Validate passes. Confirms the validation
// is NOT over-restrictive (a properly configured transit deploy boots).
func TestValidate_TransitConfigCompletePasses(t *testing.T) {
	c := minProdConfig(t)
	c.OpenBaoTransitEnabled = true
	c.OpenBaoVaultAddr = "https://openbao.secrets-system.svc:8200"
	c.OpenBaoVaultRoleID = "rid"
	c.OpenBaoVaultSecretID = "sid"

	if err := c.Validate(); err != nil {
		t.Fatalf("Validate should pass with complete transit config; got %v", err)
	}
}

// TestValidate_TransitDisabledIgnoresVaultVars — legacy path: when
// OPENBAO_TRANSIT_ENABLED=false (default), missing vault vars MUST NOT
// fail Validate. Backwards compat for environments not yet wired.
func TestValidate_TransitDisabledIgnoresVaultVars(t *testing.T) {
	c := minProdConfig(t)
	c.OpenBaoTransitEnabled = false
	// All three vault vars empty — must NOT fail validation.

	if err := c.Validate(); err != nil {
		t.Fatalf("Validate should pass when transit disabled + vault vars empty; got %v", err)
	}
}

// TestLoad_OpenBaoTransitFromEnv — Load reads the 5 OpenBao env vars
// correctly. Defaults: enabled=false, all strings empty.
func TestLoad_OpenBaoTransitFromEnv(t *testing.T) {
	t.Setenv("OPENBAO_TRANSIT_ENABLED", "true")
	t.Setenv("OPENBAO_VAULT_ADDR", "https://openbao.secrets-system.svc:8200")
	t.Setenv("OPENBAO_VAULT_ROLE_ID", "rid-abc")
	t.Setenv("OPENBAO_VAULT_SECRET_ID", "sid-xyz")
	t.Setenv("OPENBAO_SPKI_PIN_SHA256", "deadbeef")

	cfg := Load()

	if !cfg.OpenBaoTransitEnabled {
		t.Error("OpenBaoTransitEnabled: want true (parsed from env)")
	}
	if cfg.OpenBaoVaultAddr != "https://openbao.secrets-system.svc:8200" {
		t.Errorf("OpenBaoVaultAddr: got %q", cfg.OpenBaoVaultAddr)
	}
	if cfg.OpenBaoVaultRoleID != "rid-abc" {
		t.Errorf("OpenBaoVaultRoleID: got %q", cfg.OpenBaoVaultRoleID)
	}
	if cfg.OpenBaoVaultSecretID != "sid-xyz" {
		t.Errorf("OpenBaoVaultSecretID: got %q", cfg.OpenBaoVaultSecretID)
	}
	if cfg.OpenBaoSPKIPinSHA256 != "deadbeef" {
		t.Errorf("OpenBaoSPKIPinSHA256: got %q", cfg.OpenBaoSPKIPinSHA256)
	}
}

// TestLoad_OpenBaoTransitDefaultsDisabled — when env vars are unset,
// transit is disabled and all credential fields are empty (zero-value).
func TestLoad_OpenBaoTransitDefaultsDisabled(t *testing.T) {
	t.Setenv("OPENBAO_TRANSIT_ENABLED", "")
	t.Setenv("OPENBAO_VAULT_ADDR", "")
	t.Setenv("OPENBAO_VAULT_ROLE_ID", "")
	t.Setenv("OPENBAO_VAULT_SECRET_ID", "")

	cfg := Load()

	if cfg.OpenBaoTransitEnabled {
		t.Error("OpenBaoTransitEnabled: want false (default)")
	}
	if cfg.OpenBaoVaultAddr != "" || cfg.OpenBaoVaultRoleID != "" || cfg.OpenBaoVaultSecretID != "" {
		t.Errorf("vault vars: want all empty; got addr=%q roleID=%q secretID=%q",
			cfg.OpenBaoVaultAddr, cfg.OpenBaoVaultRoleID, cfg.OpenBaoVaultSecretID)
	}
}

// REGISTRY_URL is written into buildkitd.toml; a quote, backslash or line
// break would break or inject TOML. It is rejected at startup, not at the
// first build.
func TestValidate_RejectsARegistryURLThatBreaksBuildkitConfig(t *testing.T) {
	t.Setenv("LOOPZERO_CLUSTER_ISSUER", "letsencrypt-prod")
	for _, bad := range []string{`registry"x`, "registry\nx", `registry\x`} {
		t.Setenv("REGISTRY_URL", bad)
		err := Load().Validate()
		if err == nil || !contains(err.Error(), "REGISTRY_URL") {
			t.Errorf("REGISTRY_URL=%q: Validate() = %v, want an error naming REGISTRY_URL", bad, err)
		}
	}
}

// The local registry is a development default only. Outside development an unset
// REGISTRY_URL stops startup: falling back to k3d-loopzero-registry:5000 made every build
// push to a host that does not exist there.
func TestLoad_DefaultsTheRegistryOnlyInDevelopment(t *testing.T) {
	t.Setenv("REGISTRY_URL", "")
	t.Setenv("LOOPZERO_ENV", "development")
	if got := Load().RegistryURL; got != "k3d-loopzero-registry:5000" {
		t.Errorf("development: RegistryURL = %q, want the local registry", got)
	}
	t.Setenv("LOOPZERO_ENV", "staging")
	if got := Load().RegistryURL; got != "" {
		t.Errorf("staging: RegistryURL = %q, want empty", got)
	}
}

func TestValidate_RequiresARegistryOutsideDevelopment(t *testing.T) {
	for _, env := range []string{"staging", "production"} {
		err := Config{Env: env}.validateRegistryURLs()
		if err == nil || !contains(err.Error(), "REGISTRY_URL") {
			t.Errorf("%s with no REGISTRY_URL: validateRegistryURLs() = %v, want an error naming REGISTRY_URL", env, err)
		}
	}
	if err := (Config{Env: "staging", RegistryURL: "123456789012.dkr.ecr.us-east-1.amazonaws.com/loopzero-tenants"}).validateRegistryURLs(); err != nil {
		t.Errorf("a registry with a repository prefix: validateRegistryURLs() = %v, want nil", err)
	}
}
