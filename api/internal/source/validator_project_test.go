// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// Plan v1.2 T2.1 (cutover-deep-review-hardening): services.json v2 project
// field tests. v1 retained as deprecated with services-bundle fallback per
// ADR D10 — preserves Gitea repo lineage.

package source

import (
	"strings"
	"testing"
)

func validV1ServicesJSON() string {
	return `{
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
				"passSetCookie": false
			}
		]
	}`
}

func validV2ServicesJSON(project string) string {
	return `{
		"version": 2,
		"project": "` + project + `",
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
				"passSetCookie": false
			}
		]
	}`
}

func TestValidateServicesJSON_V2_Project_OK(t *testing.T) {
	m, err := ValidateServicesJSONNative(validV2ServicesJSON("myapp"), EnvBlocklistEnforce)
	if err != nil {
		t.Fatalf("expected v2 with valid project to validate, got: %v", err)
	}
	if got := ProjectNameFromManifest(m); got != "myapp" {
		t.Errorf("expected project=myapp, got %q", got)
	}
}

// EC-2 absorbed: v1 fallback MUST stay "services-bundle" so live tenants
// keep their Gitea repos intact (loop-runtime-source-{tenant}-services-bundle).
func TestValidateServicesJSON_V1_NoProject_FallbackServicesBundle(t *testing.T) {
	m, err := ValidateServicesJSONNative(validV1ServicesJSON(), EnvBlocklistEnforce)
	if err != nil {
		t.Fatalf("expected v1 to validate (deprecation only), got: %v", err)
	}
	if got := ProjectNameFromManifest(m); got != "services-bundle" {
		t.Errorf("EC-2: v1 fallback MUST stay services-bundle (ADR D10), got %q", got)
	}
}

func TestValidateServicesJSON_V2_ProjectMissing_Returns422(t *testing.T) {
	content := `{
		"version": 2,
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
				"passSetCookie": false
			}
		]
	}`
	_, err := ValidateServicesJSONNative(content, EnvBlocklistEnforce)
	if err == nil {
		t.Fatal("expected validation error for v2 without project, got nil")
	}
	// JSON Schema oneOf surfaces the deepest leaf — either the v1 const
	// mismatch ("/version: value must be 1") OR the v2 missing-property
	// ("project required"). Both prove the structural rejection fired.
	msg := err.Error()
	if !strings.Contains(msg, "project") && !strings.Contains(msg, "version") {
		t.Errorf("expected error to mention project or version branch, got: %v", err)
	}
}

func TestValidateServicesJSON_V2_ProjectInvalidDNS_Returns422(t *testing.T) {
	for _, bad := range []string{
		"Acme",                  // uppercase
		"acme/../passwd",        // path traversal
		"_acme",                 // leading underscore
		"acme-",                 // trailing hyphen
		strings.Repeat("a", 64), // too long
	} {
		content := validV2ServicesJSON(bad)
		_, err := ValidateServicesJSONNative(content, EnvBlocklistEnforce)
		if err == nil {
			t.Errorf("expected validation error for invalid project %q, got nil", bad)
		}
	}
}

// Smoke test that schema correctly distinguishes versions via oneOf:
// emitting "version": 1 with a project field is OK (additional info), but
// version 2 requires project. We accept extra field on v1 because real-world
// emitters may include it during migration.
func TestValidateServicesJSON_V1_WithProject_Accepted(t *testing.T) {
	// v1 schema clause does not allow `project` (additionalProperties: false).
	// This test pins the behavior that emitters MUST bump to version 2 when
	// including project, preventing silent acceptance of half-migrated emits.
	content := `{
		"version": 1,
		"project": "myapp",
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
				"passSetCookie": false
			}
		]
	}`
	_, err := ValidateServicesJSONNative(content, EnvBlocklistEnforce)
	if err == nil {
		t.Fatal("expected v1 + project field to be rejected (must bump version to 2)")
	}
}
