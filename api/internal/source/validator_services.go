// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package source

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// servicesJSONV1LegacyProject is the project name used when a v1 services.json
// payload arrives without an explicit project field. Plan v1.2 T2.1 + ADR D10:
// MUST remain "services-bundle" (NOT "default") to keep existing Gitea repos
// loopzero-source-{tenant}-services-bundle and their ArgoCD App lineage intact.
const servicesJSONV1LegacyProject = "services-bundle"

// Plan B v3.1 T-B.3 (2026-06-05) — services.json validator.
//
// Replaces the legacy `ValidateLoopZeroYAML()` for the new wire-level contract.
// Embeds the JSON Schema 7 artifact (mirror of loopzerokit's
// `ServicesManifest` interface from `packages/loopzero/src/services/adapters-bridge/manifest.ts`)
// and uses `santhosh-tekuri/jsonschema/v5` to validate payload shape before
// translating into the existing `LoopZeroYAML` struct shape (so downstream code
// in `build_reconcile_complete.go` + cascade can consume unchanged during
// the transition window per ADR D7).

//go:embed services.schema.json
var servicesSchemaBytes []byte

var (
	servicesSchemaOnce sync.Once
	servicesSchema     *jsonschema.Schema
	servicesSchemaErr  error
)

// ServiceEntry mirrors loopzerokit's `ManifestServiceEntry` interface.
// Plan v1.2 T2.2: `Type` enum field added (server | worker | frontend) with
// Go-side fallback to "server" (JSON Schema 7 does not apply default
// automatically).
type ServiceEntry struct {
	Name          string            `json:"name"`
	Runtime       string            `json:"runtime"`
	Type          string            `json:"type,omitempty"`
	Port          int               `json:"port"`
	Proxy         string            `json:"proxy"`
	Dev           string            `json:"dev"`
	Build         string            `json:"build,omitempty"`
	Start         string            `json:"start"`
	OpenAPI       string            `json:"openapi,omitempty"`
	Healthcheck   string            `json:"healthcheck"`
	CORS          bool              `json:"cors"`
	Env           map[string]string `json:"env,omitempty"`
	DependsOn     []string          `json:"dependsOn,omitempty"`
	PassSetCookie bool              `json:"passSetCookie"`
}

// ServicesManifest mirrors loopzerokit's `ServicesManifest` interface.
// Plan v1.2 T2.1: `Project` added as top-level identifier. Required from
// v2; v1 emit omits the field and the validator falls back to
// "services-bundle" (NOT "default") per ADR D10 — preserves Gitea repo
// lineage for tenants who shipped under Plan B v3.1.
type ServicesManifest struct {
	Version  int            `json:"version"`
	Project  string         `json:"project,omitempty"`
	Services []ServiceEntry `json:"services"`
}

func compileSchema() (*jsonschema.Schema, error) {
	servicesSchemaOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource("services.schema.json", bytes.NewReader(servicesSchemaBytes)); err != nil {
			servicesSchemaErr = fmt.Errorf("register schema: %w", err)
			return
		}
		schema, err := compiler.Compile("services.schema.json")
		if err != nil {
			servicesSchemaErr = fmt.Errorf("compile schema: %w", err)
			return
		}
		servicesSchema = schema
	})
	return servicesSchema, servicesSchemaErr
}

// ValidateServicesJSONNative parses + validates a services.json payload and
// returns the native *ServicesManifest. Plan v1.2 T3.3 (M6): preferred
// callsites should consume the native shape (carries dependsOn, openapi,
// type, env fully typed). ValidateServicesJSON remains for legacy callers
// that still need the *LoopZeroYAML adapter; it now delegates here.
func ValidateServicesJSONNative(content string) (*ServicesManifest, error) {
	if strings.TrimSpace(content) == "" {
		return nil, &ValidationError{
			Field:   "services_json",
			Message: "E-008: missing payload (services.json content is empty)",
		}
	}

	schema, err := compileSchema()
	if err != nil {
		return nil, &ValidationError{
			Field:   "_internal",
			Message: fmt.Sprintf("schema compile failed: %s", err.Error()),
		}
	}

	var raw interface{}
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return nil, &ValidationError{
			Field:   "services_json",
			Message: fmt.Sprintf("E-008: invalid JSON payload: %s", err.Error()),
		}
	}

	if err := schema.Validate(raw); err != nil {
		var ve *jsonschema.ValidationError
		msg := err.Error()
		if asValidErr, ok := err.(*jsonschema.ValidationError); ok {
			ve = asValidErr
			leaf := ve
			for len(leaf.Causes) > 0 {
				leaf = leaf.Causes[0]
			}
			msg = fmt.Sprintf("%s: %s", leaf.InstanceLocation, leaf.Message)
		}
		return nil, &ValidationError{Field: "services_json", Message: msg}
	}

	var manifest ServicesManifest
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		return nil, &ValidationError{
			Field:   "services_json",
			Message: fmt.Sprintf("decode manifest: %s", err.Error()),
		}
	}

	if envErr := validateReservedEnvKeys(&manifest, nil); envErr != nil {
		return nil, envErr
	}

	return &manifest, nil
}

// ValidateServicesJSON parses + validates a services.json payload against the
// embedded JSON Schema 7 contract, then translates into the existing
// `LoopZeroYAML` struct shape so downstream code paths stay unchanged during
// the transition window (per ADR D7 framework field deprecation strategy).
//
// Plan v1.2 T3.3 (M6) — DEPRECATED legacy adapter. New callers should
// consume ValidateServicesJSONNative() instead and read native fields
// (dependsOn, openapi, type, env) directly. This wrapper stays GREEN for
// existing routes/source.go callers during the migration window.
//
// EC-B3-7 (v3.1): truncated/empty payload returns E-008.
// EC-B3-8 (v3.1): schema violations include field path em error message.
//
// Deprecated: use ValidateServicesJSONNative.
func ValidateServicesJSON(content string) (*LoopZeroYAML, []string, error) {
	manifest, err := ValidateServicesJSONNative(content)
	if err != nil {
		return nil, nil, err
	}

	// Plan v1.2 T2.1 — derive project name from manifest.
	//   v2: project field is REQUIRED + DNS-1123 validated by schema.
	//   v1: project field absent; fall back to "services-bundle" per ADR D10
	//        and emit a structured deprecation warning so operators can plan
	//        the cutover (sunset loopzerokit 0.6.0).
	projectName := manifest.Project
	if projectName == "" {
		projectName = servicesJSONV1LegacyProject
		if manifest.Version == 1 {
			slog.Warn("services.json v1 missing project — applying legacy fallback (deprecated)",
				"code", "SERVICES_JSON_V1_DEPRECATED",
				"fallback_project", servicesJSONV1LegacyProject,
				"sunset", "loopzerokit 0.6.0",
				"action", "Upgrade emit to services.json v2 and set `project` from loopzero.config.ts name")
		}
	}

	cfg := &LoopZeroYAML{
		Version: manifest.Version,
		Name:    projectName,
		Project: projectName,
		Apps:    make(map[string]AppConfig, len(manifest.Services)),
	}

	appNames := make([]string, 0, len(manifest.Services))
	for _, svc := range manifest.Services {
		cfg.Apps[svc.Name] = AppConfig{
			Type:      "server",
			Port:      svc.Port,
			Path:      ".",
			Framework: svc.Runtime,
			Build:     "buildpack",
			Env:       svc.Env,
		}
		appNames = append(appNames, svc.Name)
	}

	sort.Strings(appNames)
	return cfg, appNames, nil
}
