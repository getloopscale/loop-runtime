// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package naming

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Sanitize
// ---------------------------------------------------------------------------

func TestSanitize_lowercases(t *testing.T) {
	if got := Sanitize("MyTeam"); got != "myteam" {
		t.Errorf("Sanitize(MyTeam) = %q, want %q", got, "myteam")
	}
}

func TestSanitize_replaces_non_alphanumeric_with_hyphens(t *testing.T) {
	if got := Sanitize("my_team.name"); got != "my-team-name" {
		t.Errorf("Sanitize(my_team.name) = %q, want %q", got, "my-team-name")
	}
}

func TestSanitize_trims_leading_and_trailing_hyphens(t *testing.T) {
	if got := Sanitize("--hello--"); got != "hello" {
		t.Errorf("Sanitize(--hello--) = %q, want %q", got, "hello")
	}
}

func TestSanitize_collapses_consecutive_hyphens(t *testing.T) {
	if got := Sanitize("a---b"); got != "a-b" {
		t.Errorf("Sanitize(a---b) = %q, want %q", got, "a-b")
	}
}

func TestSanitize_truncates_to_63_chars(t *testing.T) {
	long := strings.Repeat("a", 100)
	got := Sanitize(long)
	if len(got) > MaxKubernetesNameLength {
		t.Errorf("Sanitize(100 a's) length = %d, want <= %d", len(got), MaxKubernetesNameLength)
	}
}

func TestSanitize_truncation_does_not_end_with_hyphen(t *testing.T) {
	// 62 a's + hyphen at position 63 should be trimmed
	input := strings.Repeat("a", 62) + "-b"
	got := Sanitize(input)
	if len(got) > MaxKubernetesNameLength {
		t.Errorf("length = %d, want <= %d", len(got), MaxKubernetesNameLength)
	}
	if strings.HasSuffix(got, "-") {
		t.Errorf("Sanitize result %q ends with hyphen", got)
	}
}

func TestSanitize_empty_string(t *testing.T) {
	if got := Sanitize(""); got != "" {
		t.Errorf("Sanitize('') = %q, want empty", got)
	}
}

func TestSanitize_unicode(t *testing.T) {
	got := Sanitize("cafe\u0301")
	// Non-ASCII chars replaced with hyphens, then trimmed
	if !validNameRegexp.MatchString(got) && got != "" {
		t.Errorf("Sanitize(unicode) = %q is not RFC 1123 compliant", got)
	}
}

func TestSanitize_all_special_chars(t *testing.T) {
	got := Sanitize("@#$%")
	if got != "" {
		t.Errorf("Sanitize(@#$%%) = %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// ValidateKubernetesName
// ---------------------------------------------------------------------------

func TestValidateKubernetesName_valid(t *testing.T) {
	valid := []string{"abc", "a", "a-b", "abc-def-123", strings.Repeat("a", 63)}
	for _, name := range valid {
		if err := ValidateKubernetesName(name); err != nil {
			t.Errorf("ValidateKubernetesName(%q) unexpected error: %v", name, err)
		}
	}
}

func TestValidateKubernetesName_empty(t *testing.T) {
	if err := ValidateKubernetesName(""); err == nil {
		t.Error("ValidateKubernetesName('') should return error")
	}
}

func TestValidateKubernetesName_too_long(t *testing.T) {
	if err := ValidateKubernetesName(strings.Repeat("a", 64)); err == nil {
		t.Error("ValidateKubernetesName(64 chars) should return error")
	}
}

func TestValidateKubernetesName_uppercase(t *testing.T) {
	if err := ValidateKubernetesName("ABC"); err == nil {
		t.Error("ValidateKubernetesName('ABC') should return error")
	}
}

func TestValidateKubernetesName_starts_with_hyphen(t *testing.T) {
	if err := ValidateKubernetesName("-abc"); err == nil {
		t.Error("ValidateKubernetesName('-abc') should return error")
	}
}

func TestValidateKubernetesName_ends_with_hyphen(t *testing.T) {
	if err := ValidateKubernetesName("abc-"); err == nil {
		t.Error("ValidateKubernetesName('abc-') should return error")
	}
}

func TestValidateKubernetesName_special_chars(t *testing.T) {
	if err := ValidateKubernetesName("abc_def"); err == nil {
		t.Error("ValidateKubernetesName('abc_def') should return error")
	}
}

// ---------------------------------------------------------------------------
// TenantNamespace
// ---------------------------------------------------------------------------

func TestTenantNamespace_format(t *testing.T) {
	got, err := TenantNamespace("tn-loopzero")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "loopzero-tn-loopzero" {
		t.Errorf("TenantNamespace(tn-loopzero) = %q, want %q", got, "loopzero-tn-loopzero")
	}
}

func TestTenantNamespace_empty(t *testing.T) {
	_, err := TenantNamespace("")
	if err == nil {
		t.Error("TenantNamespace('') should return error")
	}
}

func TestTenantNamespace_too_long(t *testing.T) {
	// "loopzero-" = 5 chars, name can be at most 58
	_, err := TenantNamespace(strings.Repeat("a", 59))
	if err == nil {
		t.Error("TenantNamespace(59 chars) should return error")
	}
}

// ---------------------------------------------------------------------------
// EnvironmentNamespace
// ---------------------------------------------------------------------------

func TestBuildNamespace_format(t *testing.T) {
	got, err := BuildNamespace("acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "build-acme" {
		t.Errorf("got %q, want %q", got, "build-acme")
	}
}

func TestBuildNamespace_empty(t *testing.T) {
	_, err := BuildNamespace("")
	if err == nil {
		t.Error("should return error for empty tenant")
	}
}

// ---------------------------------------------------------------------------
// PreviewNamespace
// ---------------------------------------------------------------------------

func TestNamespace_canonical(t *testing.T) {
	cases := []struct {
		name      string
		tenant    string
		project   string
		env       string
		want      string
		wantError bool
	}{
		{name: "production", tenant: "tn-x", project: "teste", env: "production", want: "loopzero-tn-x-teste-production"},
		{name: "staging", tenant: "tn-x", project: "api", env: "staging", want: "loopzero-tn-x-api-staging"},
		{name: "preview", tenant: "tn-x", project: "web", env: "preview-pr-7", want: "loopzero-tn-x-web-preview-pr-7"},
		{name: "empty tenant", tenant: "", project: "p", env: "e", wantError: true},
		{name: "empty project", tenant: "t", project: "", env: "e", wantError: true},
		{name: "empty env", tenant: "t", project: "p", env: "", wantError: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Namespace(tc.tenant, tc.project, tc.env)
			if tc.wantError {
				if err == nil {
					t.Fatalf("expected error, got nil (result=%q)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRepoName_format(t *testing.T) {
	got, err := RepoName("acme", "backend")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "acme-backend" {
		t.Errorf("got %q, want %q", got, "acme-backend")
	}
}

func TestRepoName_empty_team(t *testing.T) {
	_, err := RepoName("", "backend")
	if err == nil {
		t.Error("should return error for empty team")
	}
}

func TestRepoName_empty_project(t *testing.T) {
	_, err := RepoName("acme", "")
	if err == nil {
		t.Error("should return error for empty project")
	}
}

// ---------------------------------------------------------------------------
// GitOpsRepoName
// ---------------------------------------------------------------------------

func TestGitOpsRepoName_format(t *testing.T) {
	got, err := GitOpsRepoName("acme", "backend", "production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "acme-backend-production" {
		t.Errorf("got %q, want %q", got, "acme-backend-production")
	}
}

func TestGitOpsRepoName_empty_team(t *testing.T) {
	_, err := GitOpsRepoName("", "backend", "production")
	if err == nil {
		t.Error("should return error for empty team")
	}
}

func TestGitOpsRepoName_empty_project(t *testing.T) {
	_, err := GitOpsRepoName("acme", "", "production")
	if err == nil {
		t.Error("should return error for empty project")
	}
}

func TestGitOpsRepoName_empty_env(t *testing.T) {
	_, err := GitOpsRepoName("acme", "backend", "")
	if err == nil {
		t.Error("should return error for empty env")
	}
}

// ---------------------------------------------------------------------------
// Hostname
// ---------------------------------------------------------------------------

func TestHostname_format(t *testing.T) {
	got, err := Hostname("backend", "production", "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "backend-production.example.com" {
		t.Errorf("got %q, want %q", got, "backend-production.example.com")
	}
}

func TestHostname_sanitizes_parts(t *testing.T) {
	got, err := Hostname("My_App", "PROD", "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "my-app-prod.example.com" {
		t.Errorf("got %q, want %q", got, "my-app-prod.example.com")
	}
}

func TestHostname_empty_subdomain_returns_error(t *testing.T) {
	_, err := Hostname("@#$", "!!!", "example.com")
	if err == nil {
		t.Error("Hostname with all-special-char inputs should return error")
	}
}

// ---------------------------------------------------------------------------
// ArgoAppName
// ---------------------------------------------------------------------------

func TestArgoAppName_format(t *testing.T) {
	got, err := ArgoAppName("acme", "backend", "production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "acme-backend-production" {
		t.Errorf("got %q, want %q", got, "acme-backend-production")
	}
}

func TestArgoAppName_empty_team(t *testing.T) {
	_, err := ArgoAppName("", "backend", "production")
	if err == nil {
		t.Error("should return error for empty team")
	}
}

// ---------------------------------------------------------------------------
// CertSecretName
// ---------------------------------------------------------------------------

func TestCertSecretName_format(t *testing.T) {
	got, err := CertSecretName("backend-production.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "backend-production-example-com-tls" {
		t.Errorf("got %q, want %q", got, "backend-production-example-com-tls")
	}
}

func TestCertSecretName_sanitizes(t *testing.T) {
	got, err := CertSecretName("My.App.io")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "my-app-io-tls" {
		t.Errorf("got %q, want %q", got, "my-app-io-tls")
	}
}

func TestCertSecretName_truncates_long_hostname(t *testing.T) {
	long := strings.Repeat("a", 60) + ".example.com"
	got, err := CertSecretName(long)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) > MaxKubernetesNameLength {
		t.Errorf("CertSecretName result length %d exceeds max %d", len(got), MaxKubernetesNameLength)
	}
	if !strings.HasSuffix(got, "-tls") {
		t.Errorf("CertSecretName result %q does not end with -tls", got)
	}
}

func TestCertSecretName_empty_hostname_returns_error(t *testing.T) {
	_, err := CertSecretName("@#$%")
	if err == nil {
		t.Error("CertSecretName with all-special-char hostname should return error")
	}
}

// ---------------------------------------------------------------------------
// Labels (deprecated free function) — REMOVED per audit gap M04 #23.
//
// The legacy `Labels(tenant, project, env)` free function is gone; the
// canonical API is `StandardLabels{...}.AsMap()`. Its dedicated tests
// (TestLabels_contains_required_keys + TestLabels_empty_values_are_allowed
// + TestStandardLabels_AsMap_is_compatible_with_legacy_Labels) were deleted
// with the function. The required-keys invariant is now exercised by
// TestStandardLabels_AsMap_returns_canonical_shape below.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// StandardLabels (T1.5 — typed labels per gate PKG-G02)
// ---------------------------------------------------------------------------

func TestLabel_constants_match_canonical_keys(t *testing.T) {
	cases := []struct {
		got, want string
		name      string
	}{
		{LabelTenant, "loopzero.io/tenant", "LabelTenant"},
		{LabelProject, "loopzero.io/project", "LabelProject"},
		{LabelEnvironment, "loopzero.io/environment", "LabelEnvironment"},
		{LabelManagedBy, "loopzero.io/managed-by", "LabelManagedBy"},
		{ManagedByValue, "loopzero", "ManagedByValue"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q (canonical regression guard)", c.name, c.got, c.want)
		}
	}
}

func TestStandardLabels_AsMap_returns_canonical_shape(t *testing.T) {
	sl := StandardLabels{Tenant: "acme", Project: "backend", Environment: "production"}
	got := sl.AsMap()

	expected := map[string]string{
		LabelTenant:      "acme",
		LabelProject:     "backend",
		LabelEnvironment: "production",
		LabelManagedBy:   "loopzero",
	}
	if len(got) != len(expected) {
		t.Errorf("AsMap() returned %d keys, want %d", len(got), len(expected))
	}
	for k, want := range expected {
		if got[k] != want {
			t.Errorf("AsMap()[%q] = %q, want %q", k, got[k], want)
		}
	}
}

func TestStandardLabels_AsMap_independent_from_struct(t *testing.T) {
	// Mutating the returned map MUST NOT affect future calls.
	sl := StandardLabels{Tenant: "acme", Project: "backend", Environment: "production"}
	m1 := sl.AsMap()
	m1[LabelTenant] = "EVIL"
	m2 := sl.AsMap()
	if m2[LabelTenant] != "acme" {
		t.Errorf("AsMap() leaked mutation: m2[tenant] = %q, want %q", m2[LabelTenant], "acme")
	}
}

// TestStandardLabels_AsMap_is_compatible_with_legacy_Labels was deleted
// with the deprecated Labels() function (audit gap M04 #23 closure).

// ---------------------------------------------------------------------------
// GitRepoBindingName
// ---------------------------------------------------------------------------

func TestGitRepoBindingName_format(t *testing.T) {
	got, err := GitRepoBindingName("tn-acme", "production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "tn-acme-production" {
		t.Errorf("got %q, want %q", got, "tn-acme-production")
	}
}

func TestGitRepoBindingName_empty_tenant(t *testing.T) {
	_, err := GitRepoBindingName("", "production")
	if err == nil {
		t.Error("should return error for empty tenant")
	}
}

func TestGitRepoBindingName_empty_env(t *testing.T) {
	_, err := GitRepoBindingName("tn-acme", "")
	if err == nil {
		t.Error("should return error for empty env")
	}
}

func TestGitRepoBindingName_too_long(t *testing.T) {
	// tenantName(30) + "-" + envName(33) = 64 chars, exceeds 63
	_, err := GitRepoBindingName(strings.Repeat("a", 30), strings.Repeat("b", 33))
	if err == nil {
		t.Error("should return error when combined name exceeds 63 chars")
	}
}

// ---------------------------------------------------------------------------
// ArgoRepoSecretName
// ---------------------------------------------------------------------------

func TestArgoRepoSecretName_format(t *testing.T) {
	got, err := ArgoRepoSecretName("tn-acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "repo-tn-acme-gitops" {
		t.Errorf("got %q, want %q", got, "repo-tn-acme-gitops")
	}
}

func TestArgoRepoSecretName_empty(t *testing.T) {
	_, err := ArgoRepoSecretName("")
	if err == nil {
		t.Error("should return error for empty tenant")
	}
}

func TestArgoRepoSecretName_too_long(t *testing.T) {
	// "repo-" (5) + tenantName(53) + "-gitops" (7) = 65, exceeds 63
	_, err := ArgoRepoSecretName(strings.Repeat("a", 53))
	if err == nil {
		t.Error("should return error when combined name exceeds 63 chars")
	}
}

// ---------------------------------------------------------------------------
// Determinism: same input -> same output
// ---------------------------------------------------------------------------

func TestDeterminism_all_functions(t *testing.T) {
	for i := 0; i < 10; i++ {
		s := Sanitize("My_Team.Name")
		if s != "my-team-name" {
			t.Fatalf("Sanitize not deterministic: got %q on iteration %d", s, i)
		}

		tn, _ := TenantNamespace("tn-acme")
		if tn != "loopzero-tn-acme" {
			t.Fatalf("TenantNamespace not deterministic on iteration %d", i)
		}

		bn, _ := BuildNamespace("acme")
		if bn != "build-acme" {
			t.Fatalf("BuildNamespace not deterministic on iteration %d", i)
		}

		// Preview is not special in the naming layer: it is the triple like any other environment.
		pn, _ := Namespace("acme", "api", "preview-pr-42")
		if pn != "loopzero-acme-api-preview-pr-42" {
			t.Fatalf("Namespace (preview) not deterministic on iteration %d", i)
		}

		rn, _ := RepoName("acme", "api")
		if rn != "acme-api" {
			t.Fatalf("RepoName not deterministic on iteration %d", i)
		}

		gn, _ := GitOpsRepoName("acme", "api", "prod")
		if gn != "acme-api-prod" {
			t.Fatalf("GitOpsRepoName not deterministic on iteration %d", i)
		}

		h, _ := Hostname("api", "prod", "example.com")
		if h != "api-prod.example.com" {
			t.Fatalf("Hostname not deterministic on iteration %d", i)
		}

		an, _ := ArgoAppName("acme", "api", "prod")
		if an != "acme-api-prod" {
			t.Fatalf("ArgoAppName not deterministic on iteration %d", i)
		}

		cn, _ := CertSecretName("api-prod.example.com")
		if cn != "api-prod-example-com-tls" {
			t.Fatalf("CertSecretName not deterministic on iteration %d", i)
		}

		grb, _ := GitRepoBindingName("tn-acme", "prod")
		if grb != "tn-acme-prod" {
			t.Fatalf("GitRepoBindingName not deterministic on iteration %d", i)
		}

		ars, _ := ArgoRepoSecretName("tn-acme")
		if ars != "repo-tn-acme-gitops" {
			t.Fatalf("ArgoRepoSecretName not deterministic on iteration %d", i)
		}
	}
}

// ---------------------------------------------------------------------------
// ImageTag
// ---------------------------------------------------------------------------

func TestImageTag_format(t *testing.T) {
	got := ImageTag("registry.do.com", "loopzero", "my-app", "api", "abc123")
	want := "registry.do.com/loopzero/my-app-api:abc123"
	if got != want {
		t.Errorf("ImageTag = %q, want %q", got, want)
	}
}

func TestImageTag_all_parts_present(t *testing.T) {
	got := ImageTag("r", "t", "p", "a", "sha")
	// Verify separators: / between registry and tenant, / between tenant and project-app, - between project and app, : before SHA
	if got != "r/t/p-a:sha" {
		t.Errorf("ImageTag = %q, want %q", got, "r/t/p-a:sha")
	}
}

func TestImageTag_deterministic(t *testing.T) {
	for i := 0; i < 10; i++ {
		got := ImageTag("reg", "tenant", "proj", "app", "sha256")
		if got != "reg/tenant/proj-app:sha256" {
			t.Fatalf("ImageTag not deterministic on iteration %d: %q", i, got)
		}
	}
}

// CHG-2026-04-29-DOGFOOD iter 8 — ImageDigestRef MUST produce a digest-pinned
// reference required by the supply-chain VAP. Live discovery: pods got
// Forbidden in standalone-multi when manifest used `image:tag`.
func TestImageDigestRef_addsSha256Prefix(t *testing.T) {
	const bareDigest = "8f0a1b2c3d4e5f6789abcdef0123456789abcdef0123456789abcdef01234567"
	got := ImageDigestRef("reg.io", "tn", "p", "api", bareDigest)
	want := "reg.io/tn/p-api@sha256:" + bareDigest
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestImageDigestRef_acceptsPrefixedDigest(t *testing.T) {
	const prefixed = "sha256:8f0a1b2c3d4e5f6789abcdef0123456789abcdef0123456789abcdef01234567"
	got := ImageDigestRef("reg.io", "tn", "p", "api", prefixed)
	want := "reg.io/tn/p-api@" + prefixed
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestImageDigestRef_doesNotDoubleSha256Prefix(t *testing.T) {
	const prefixed = "sha256:abc123"
	got := ImageDigestRef("r", "t", "p", "a", prefixed)
	// MUST NOT produce "sha256:sha256:abc123".
	if want := "r/t/p-a@sha256:abc123"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestImageDigestRef_emptyDigestFailsLoud(t *testing.T) {
	// Empty digest → reference is syntactically valid but admission
	// rejects (no signature). Caller error, not a silent fallback.
	got := ImageDigestRef("r", "t", "p", "a", "")
	if got == ImageTag("r", "t", "p", "a", "") {
		t.Errorf("empty digest must NOT collapse to ImageTag form (would lose admission protection): %q", got)
	}
	if !strings.Contains(got, "@sha256:") {
		t.Errorf("empty digest fallback must still use @sha256: anchor: %q", got)
	}
}

// ---------------------------------------------------------------------------
// ArgoAppName — missing edge case tests
// ---------------------------------------------------------------------------

func TestArgoAppName_empty_project(t *testing.T) {
	_, err := ArgoAppName("acme", "", "production")
	if err == nil {
		t.Error("should return error for empty project")
	}
}

func TestArgoAppName_empty_env(t *testing.T) {
	_, err := ArgoAppName("acme", "backend", "")
	if err == nil {
		t.Error("should return error for empty env")
	}
}

// Red Team loopzero-cloud#41 regression-protector (2026-06-27): IsPreviewEnv
// must cover the legacy literal "preview" env name AND the broader
// "preview-*" prefix the CLI emitted before the canonical "preview-pr-N"
// pattern was introduced. LIVE DB inspection 2026-06-27 confirmed a row
// with environment='preview' + kind='permanent' (legacy data) that the
// strict prefix predicate missed → SQL count of "production envs"
// included it incorrectly.
func TestIsPreviewEnv_RedTeam41(t *testing.T) {
	tests := []struct {
		envName string
		want    bool
	}{
		{"preview-pr-123", true},  // canonical pattern
		{"preview-pr-1", true},    // canonical pattern (single digit)
		{"preview", true},         // legacy CLI literal — Red Team #41 catch
		{"preview-feature", true}, // broader "preview-*" prefix
		{"production", false},     // explicit non-preview
		{"staging", false},        // explicit non-preview
		{"prod", false},           // short form
		{"", false},               // empty
		{"my-preview", false},     // contains "preview" but doesn't start with it
	}
	for _, tt := range tests {
		t.Run(tt.envName, func(t *testing.T) {
			got := IsPreviewEnv(tt.envName)
			if got != tt.want {
				t.Errorf("IsPreviewEnv(%q) = %v, want %v", tt.envName, got, tt.want)
			}
		})
	}
}

// EnvironmentCRName — #273: the Environment CR's identity needs the project, because its
// content (spec.project, spec.namespace.name) already depends on the project.

func TestEnvironmentCRName_includesTheProject(t *testing.T) {
	got, err := EnvironmentCRName("tn-acme", "backend", "production")
	if err != nil {
		t.Fatalf("EnvironmentCRName: %v", err)
	}
	if got != "tn-acme-backend-production" {
		t.Errorf("EnvironmentCRName = %q, want %q", got, "tn-acme-backend-production")
	}
}

func TestEnvironmentCRName_distinctProjectsDoNotCollide(t *testing.T) {
	a, _ := EnvironmentCRName("tn-acme", "alpha", "preview")
	b, _ := EnvironmentCRName("tn-acme", "beta", "preview")
	if a == b {
		t.Fatalf("two projects in the same environment collided on %q — this is the #273 defect", a)
	}
}

func TestEnvironmentCRName_emptyProjectFailsLoudly(t *testing.T) {
	if _, err := EnvironmentCRName("tn-acme", "", "production"); err == nil {
		t.Error("an empty project must return an error, not a degenerate name")
	}
}

// AppCertSecretName — #277: the renderer names the TLS Secret "tls-{project}-{app}"
// (api/internal/manifests/renderer_ingress.go), while the exposure controller looked it up
// via CertSecretName(hostname) => "{sanitized-hostname}-tls". Two formulas for the same
// object, which never matched: the ExposedRoute stayed Pending for 27 days with the
// certificate issued and valid.

func TestAppCertSecretName_matchesTheRenderer(t *testing.T) {
	got, err := AppCertSecretName("teste", "app")
	if err != nil {
		t.Fatalf("AppCertSecretName: %v", err)
	}
	// This value was observed in app-dev: the real Secret created by the renderer.
	if got != "tls-teste-app" {
		t.Errorf("AppCertSecretName = %q, want %q — the renderer produces tls-{project}-{app}", got, "tls-teste-app")
	}
}

func TestAppCertSecretName_distinctProjectsDoNotShareCert(t *testing.T) {
	a, _ := AppCertSecretName("alpha", "api")
	b, _ := AppCertSecretName("beta", "api")
	if a == b {
		t.Fatalf("two projects with the same app name would share the cert: %q", a)
	}
}

func TestAppCertSecretName_emptyArgumentFailsLoudly(t *testing.T) {
	if _, err := AppCertSecretName("", "api"); err == nil {
		t.Error("an empty project must fail loudly")
	}
	if _, err := AppCertSecretName("alpha", ""); err == nil {
		t.Error("an empty app must fail loudly")
	}
}
