// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestOryKratosIdentitySchemaValid ensures the identity schema is valid JSON
// with required fields for LoopZero's auth flow.
func TestOryKratosIdentitySchemaValid(t *testing.T) {
	dir := helmDir()
	schemaPath := filepath.Join(dir, "ory-kratos", "identity.schema.json")

	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("identity.schema.json missing: %v", err)
	}

	var schema map[string]interface{}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("identity.schema.json is not valid JSON: %v", err)
	}

	// Must have $schema and properties.traits.properties.email
	if _, ok := schema["$schema"]; !ok {
		t.Error("identity schema missing $schema field")
	}

	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("identity schema missing properties")
	}
	traits, ok := props["traits"].(map[string]interface{})
	if !ok {
		t.Fatal("identity schema missing properties.traits")
	}
	traitProps, ok := traits["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("identity schema missing properties.traits.properties")
	}
	if _, ok := traitProps["email"]; !ok {
		t.Error("identity schema missing email trait")
	}
}

// TestOryKratosValuesHasIdentitySchema ensures the Kratos values.yaml references
// the identity schema file correctly.
func TestOryKratosValuesHasIdentitySchema(t *testing.T) {
	dir := helmDir()
	data, err := os.ReadFile(filepath.Join(dir, "ory-kratos", "values.yaml"))
	if err != nil {
		t.Fatalf("ory-kratos/values.yaml missing: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "identity.default.schema.json") {
		t.Error("ory-kratos values.yaml does not reference identity.default.schema.json")
	}
	if !strings.Contains(content, "identitySchemas") {
		t.Error("ory-kratos values.yaml missing identitySchemas section (inline schema mount)")
	}
}

// TestOryKratosValuesNoPlaintextSecrets ensures dev secrets are clearly marked
// and production fields are not hardcoded.
func TestOryKratosValuesNoPlaintextSecrets(t *testing.T) {
	dir := helmDir()
	data, err := os.ReadFile(filepath.Join(dir, "ory-kratos", "values.yaml"))
	if err != nil {
		t.Fatalf("ory-kratos/values.yaml missing: %v", err)
	}

	content := string(data)
	// Dev secrets must be clearly marked as dev-only
	if strings.Contains(content, "loopzero-dev-cookie-secret") && !strings.Contains(content, "LOCAL DEV ONLY") {
		t.Error("ory-kratos values.yaml has dev secrets without LOCAL DEV ONLY marker")
	}
}

// TestOryHydraValuesHasOAuth2Config ensures Hydra has OAuth2 configuration.
// Device Code Flow (RFC 8628) is built-in in Hydra v26+ and enabled per-client
// via the admin API, not via global config.
func TestOryHydraValuesHasOAuth2Config(t *testing.T) {
	dir := helmDir()
	data, err := os.ReadFile(filepath.Join(dir, "ory-hydra", "values.yaml"))
	if err != nil {
		t.Fatalf("ory-hydra/values.yaml missing: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "oauth2") {
		t.Error("ory-hydra values.yaml missing oauth2 config section")
	}
	if !strings.Contains(content, "issuer") {
		t.Error("ory-hydra values.yaml missing issuer URL")
	}
}

// TestOryHydraValuesJWTStrategy ensures Hydra uses JWT access tokens
// (not opaque), required for stateless validation by the engine API.
func TestOryHydraValuesJWTStrategy(t *testing.T) {
	dir := helmDir()
	data, err := os.ReadFile(filepath.Join(dir, "ory-hydra", "values.yaml"))
	if err != nil {
		t.Fatalf("ory-hydra/values.yaml missing: %v", err)
	}

	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("ory-hydra values.yaml invalid YAML: %v", err)
	}

	// Navigate hydra.config.strategies.access_token
	hydra, _ := raw["hydra"].(map[string]interface{})
	if hydra == nil {
		t.Fatal("ory-hydra values.yaml missing hydra top-level key")
	}
	config, _ := hydra["config"].(map[string]interface{})
	if config == nil {
		t.Fatal("ory-hydra values.yaml missing hydra.config")
	}
	strategies, _ := config["strategies"].(map[string]interface{})
	if strategies == nil {
		t.Fatal("ory-hydra values.yaml missing hydra.config.strategies")
	}
	accessToken, _ := strategies["access_token"].(string)
	if accessToken != "jwt" {
		t.Errorf("ory-hydra access_token strategy is %q, want \"jwt\"", accessToken)
	}
}

// TestOryServicesAreClusterIP ensures Ory services are not exposed externally.
func TestOryServicesAreClusterIP(t *testing.T) {
	dir := helmDir()
	for _, chart := range []string{"ory-kratos", "ory-hydra"} {
		data, err := os.ReadFile(filepath.Join(dir, chart, "values.yaml"))
		if err != nil {
			t.Errorf("%s/values.yaml missing: %v", chart, err)
			continue
		}

		var raw map[string]interface{}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			t.Errorf("%s values.yaml invalid YAML: %v", chart, err)
			continue
		}

		svc, _ := raw["service"].(map[string]interface{})
		if svc == nil {
			t.Errorf("%s values.yaml missing service section", chart)
			continue
		}

		for _, endpoint := range []string{"public", "admin"} {
			ep, _ := svc[endpoint].(map[string]interface{})
			if ep == nil {
				continue
			}
			svcType, _ := ep["type"].(string)
			if svcType != "ClusterIP" {
				t.Errorf("%s service.%s.type is %q, want \"ClusterIP\" (must not be exposed externally)",
					chart, endpoint, svcType)
			}
		}
	}
}
