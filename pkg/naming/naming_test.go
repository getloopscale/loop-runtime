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

func TestEnvironmentNamespace_format(t *testing.T) {
	got, err := EnvironmentNamespace("tn-loopzero", "production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "loopzero-tn-loopzero-production" {
		t.Errorf("got %q, want %q", got, "loopzero-tn-loopzero-production")
	}
}

func TestEnvironmentNamespace_empty_tenant(t *testing.T) {
	_, err := EnvironmentNamespace("", "production")
	if err == nil {
		t.Error("should return error for empty tenant")
	}
}

func TestEnvironmentNamespace_empty_env(t *testing.T) {
	_, err := EnvironmentNamespace("tn-acme", "")
	if err == nil {
		t.Error("should return error for empty env")
	}
}

func TestEnvironmentNamespace_too_long(t *testing.T) {
	// "loopzero-" = 5 chars + name + "-" + env must be <= 63
	_, err := EnvironmentNamespace(strings.Repeat("a", 30), strings.Repeat("b", 30))
	if err == nil {
		t.Error("should return error when combined name exceeds 63 chars")
	}
}

// ---------------------------------------------------------------------------
// BuildNamespace
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

func TestPreviewNamespace_format(t *testing.T) {
	got, err := PreviewNamespace("acme", "api", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "acme-api-pr-42" {
		t.Errorf("got %q, want %q", got, "acme-api-pr-42")
	}
}

func TestPreviewNamespace_empty_tenant(t *testing.T) {
	_, err := PreviewNamespace("", "api", 1)
	if err == nil {
		t.Error("should return error for empty tenant")
	}
}

func TestPreviewNamespace_empty_project(t *testing.T) {
	_, err := PreviewNamespace("acme", "", 1)
	if err == nil {
		t.Error("should return error for empty project")
	}
}

func TestPreviewNamespace_zero_pr(t *testing.T) {
	_, err := PreviewNamespace("acme", "api", 0)
	if err == nil {
		t.Error("should return error for PR number 0")
	}
}

func TestPreviewNamespace_negative_pr(t *testing.T) {
	_, err := PreviewNamespace("acme", "api", -1)
	if err == nil {
		t.Error("should return error for negative PR number")
	}
}

// ---------------------------------------------------------------------------
// RepoName
// ---------------------------------------------------------------------------

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
// Labels
// ---------------------------------------------------------------------------

func TestLabels_contains_required_keys(t *testing.T) {
	labels := Labels("acme", "backend", "production")
	expected := map[string]string{
		"loopzero.io/tenant":      "acme",
		"loopzero.io/project":     "backend",
		"loopzero.io/environment": "production",
		"loopzero.io/managed-by":  "loopzero",
	}
	for k, v := range expected {
		got, ok := labels[k]
		if !ok {
			t.Errorf("missing label %q", k)
			continue
		}
		if got != v {
			t.Errorf("label %q = %q, want %q", k, got, v)
		}
	}
}

func TestLabels_empty_values_are_allowed(t *testing.T) {
	labels := Labels("acme", "", "")
	if labels["loopzero.io/tenant"] != "acme" {
		t.Errorf("tenant label should be set")
	}
	if labels["loopzero.io/project"] != "" {
		t.Errorf("project label should be empty")
	}
}

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

		en, _ := EnvironmentNamespace("tn-acme", "prod")
		if en != "loopzero-tn-acme-prod" {
			t.Fatalf("EnvironmentNamespace not deterministic on iteration %d", i)
		}

		bn, _ := BuildNamespace("acme")
		if bn != "build-acme" {
			t.Fatalf("BuildNamespace not deterministic on iteration %d", i)
		}

		pn, _ := PreviewNamespace("acme", "api", 42)
		if pn != "acme-api-pr-42" {
			t.Fatalf("PreviewNamespace not deterministic on iteration %d", i)
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
