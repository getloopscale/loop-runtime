// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package unit

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestOryKratosHasOIDCProviders ensures the Kratos values.yaml has
// OIDC provider configuration for GitHub and Google.
func TestOryKratosHasOIDCProviders(t *testing.T) {
	data, _, err := helmValues("ory-kratos")
	if err != nil {
		t.Fatalf("ory-kratos/values.yaml{,.gotmpl} missing: %v", err)
	}

	content := string(data)

	// Must have both providers configured
	if !strings.Contains(content, "provider: github") {
		t.Error("ory-kratos values.yaml missing GitHub OIDC provider")
	}
	if !strings.Contains(content, "provider: google") {
		t.Error("ory-kratos values.yaml missing Google OIDC provider")
	}

	// Must have mapper_url for both (base64 encoded Jsonnet)
	if !strings.Contains(content, "mapper_url:") {
		t.Error("ory-kratos values.yaml missing mapper_url for OIDC providers")
	}

	// Providers must use REPLACE_VIA_PULUMI as placeholder (not real secrets)
	if strings.Contains(content, "ghp_") || strings.Contains(content, "gho_") {
		t.Error("ory-kratos values.yaml contains real GitHub tokens — use REPLACE_VIA_PULUMI")
	}
}

// TestOryKratosMapperFilesExist checks that Jsonnet mapper files exist.
func TestOryKratosMapperFilesExist(t *testing.T) {
	dir := helmDir()
	mappers := []string{
		"ory-kratos/mappers/github.jsonnet",
		"ory-kratos/mappers/google.jsonnet",
	}
	for _, mapper := range mappers {
		path := filepath.Join(dir, mapper)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("mapper file missing: %s", path)
		}
	}
}

// TestOryKratosMapperContent validates the mapper Jsonnet content.
func TestOryKratosMapperContent(t *testing.T) {
	dir := helmDir()
	for _, mapper := range []string{"github", "google"} {
		data, err := os.ReadFile(filepath.Join(dir, "ory-kratos", "mappers", mapper+".jsonnet"))
		if err != nil {
			t.Errorf("%s.jsonnet missing: %v", mapper, err)
			continue
		}
		content := string(data)

		// Must map email trait
		if !strings.Contains(content, "email") {
			t.Errorf("%s.jsonnet must map email trait", mapper)
		}
		// Must reference claims
		if !strings.Contains(content, "claims") {
			t.Errorf("%s.jsonnet must reference claims variable", mapper)
		}
		// Must produce identity.traits structure
		if !strings.Contains(content, "identity") {
			t.Errorf("%s.jsonnet must produce identity object", mapper)
		}
	}
}

// TestOryKratosMapperBase64InValues ensures the base64-encoded mappers in values.yaml
// decode to valid Jsonnet that maps email.
func TestOryKratosMapperBase64InValues(t *testing.T) {
	data, _, err := helmValues("ory-kratos")
	if err != nil {
		t.Fatalf("ory-kratos/values.yaml{,.gotmpl} missing: %v", err)
	}

	// F27 (M23 audit 2026-07-10): .gotmpl form breaks plain yaml.Unmarshal but
	// base64:// extraction is string-based — parse-fallback OK.
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		// Downgrade to string-scan mode; the invariant (mapper base64 decodes)
		// is what we check next anyway.
		_ = raw
	}

	// Extract all base64:// values from the file
	content := string(data)
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "mapper_url:") {
			continue
		}
		// Extract base64 payload
		parts := strings.SplitN(trimmed, "base64://", 2)
		if len(parts) != 2 {
			continue
		}
		b64 := strings.Trim(parts[1], `"' `)
		decoded, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			t.Errorf("mapper_url base64 decode failed: %v", err)
			continue
		}
		jsonnet := string(decoded)
		if !strings.Contains(jsonnet, "email") {
			t.Error("decoded mapper must reference email")
		}
		if !strings.Contains(jsonnet, "claims") {
			t.Error("decoded mapper must reference claims")
		}
	}
}

// TestOryKratosAccountLinking ensures account linking is configured
// (Kratos merges identities with the same email from different providers).
func TestOryKratosAccountLinking(t *testing.T) {
	data, _, err := helmValues("ory-kratos")
	if err != nil {
		t.Fatalf("ory-kratos/values.yaml{,.gotmpl} missing: %v", err)
	}

	// The identity schema uses email as the unique identifier.
	// Kratos automatically links accounts when the same email
	// is used with different providers, because email is the
	// `credentials.password.identifier` in the identity schema.
	content := string(data)
	if !strings.Contains(content, "identifier: true") && !strings.Contains(content, `"identifier": true`) {
		t.Error("identity schema must have email as identifier for account linking")
	}
}

// TestUserIdentityMapMigrationExists ensures the lazy-identity-migration
// schema for `user_identity_map` is present in the canonical migrations
// directory.
//
// History note: this used to be migration `000014_user_identity_map.up.sql`
// (added in 2026-04-01 commit `ddac9ec`). On 2026-04-24, commit `632ce1e5`
// consolidated 37 development migrations into 2 (`000001_initial_schema.up.sql`
// for DDL + `000002_seed_data.up.sql` for plan_limits) per pre-release
// policy. The old per-feature migrations were kept in
// `api/internal/db/migrations-archive/` until that folder was deleted; they
// remain in git history (`git show 7bd4d578c:api/internal/db/migrations-archive/`).
// This test now
// asserts the consolidated schema still defines the table, columns, and
// FK that the lazy migration depends on. Forward-only is no longer
// per-file enforceable since the consolidated schema is allowed to drop
// no-longer-needed objects between consolidations.
func TestUserIdentityMapMigrationExists(t *testing.T) {
	migrationPath := filepath.Join(helmDir(), "..", "..", "api", "internal", "db", "migrations", "000001_initial_schema.up.sql")
	data, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("consolidated initial schema missing: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "user_identity_map") {
		t.Error("schema must create user_identity_map table")
	}
	if !strings.Contains(content, "kratos_identity_id") {
		t.Error("schema must have kratos_identity_id column")
	}
	if !strings.Contains(content, "REFERENCES users(id)") {
		t.Error("schema must reference users table")
	}
}
