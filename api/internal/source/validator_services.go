// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package source

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

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
type ServiceEntry struct {
	Name          string            `json:"name"`
	Runtime       string            `json:"runtime"`
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
type ServicesManifest struct {
	Version  int            `json:"version"`
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

// ValidateServicesJSON parses + validates a services.json payload against the
// embedded JSON Schema 7 contract, then translates into the existing
// `LoopZeroYAML` struct shape so downstream code paths stay unchanged during
// the transition window (per ADR D7 framework field deprecation strategy).
// EC-B3-7 (v3.1): truncated/empty payload returns E-008.
// EC-B3-8 (v3.1): schema violations include field path em error message.
func ValidateServicesJSON(content string) (*LoopZeroYAML, []string, error) {
	if strings.TrimSpace(content) == "" {
		return nil, nil, &ValidationError{
			Field:   "services_json",
			Message: "E-008: missing payload (services.json content is empty)",
		}
	}

	schema, err := compileSchema()
	if err != nil {
		// Internal error — schema compile should never fail at runtime
		// (embedded artifact); surface as 500 via ValidationError boundary.
		return nil, nil, &ValidationError{
			Field:   "_internal",
			Message: fmt.Sprintf("schema compile failed: %s", err.Error()),
		}
	}

	// Unmarshal to interface{} for schema validation
	var raw interface{}
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return nil, nil, &ValidationError{
			Field:   "services_json",
			Message: fmt.Sprintf("E-008: invalid JSON payload: %s", err.Error()),
		}
	}

	if err := schema.Validate(raw); err != nil {
		// EC-B3-8: include field path em validation error
		var ve *jsonschema.ValidationError
		msg := err.Error()
		if asValidErr, ok := err.(*jsonschema.ValidationError); ok {
			ve = asValidErr
			// Surface the deepest field path in the error chain
			leaf := ve
			for len(leaf.Causes) > 0 {
				leaf = leaf.Causes[0]
			}
			msg = fmt.Sprintf("%s: %s", leaf.InstanceLocation, leaf.Message)
		}
		return nil, nil, &ValidationError{
			Field:   "services_json",
			Message: msg,
		}
	}

	// Now decode strongly-typed into ServicesManifest
	var manifest ServicesManifest
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		return nil, nil, &ValidationError{
			Field:   "services_json",
			Message: fmt.Sprintf("decode manifest: %s", err.Error()),
		}
	}

	// Translate to LoopZeroYAML struct shape for downstream compatibility.
	// services.json `services[]` → LoopZeroYAML.Apps map (keyed by service.name).
	// The `runtime` field surfaces via the `framework` slot (ADR D7 transition);
	// Plan B v3.1 T-B.5 ResolveStrategy() switch covers both runtime + legacy
	// framework values explicitly.
	cfg := &LoopZeroYAML{
		Version: manifest.Version,
		// services.json does not carry a top-level project name (loopzerokit emits
		// platform-neutral manifest); downstream callers source the name from
		// the build context (project ID / git remote) per ADR-0015.
		Name:    "services-bundle",
		Project: "services-bundle",
		Apps:    make(map[string]AppConfig, len(manifest.Services)),
	}

	appNames := make([]string, 0, len(manifest.Services))
	for _, svc := range manifest.Services {
		cfg.Apps[svc.Name] = AppConfig{
			Type:      "server",
			Port:      svc.Port,
			Path:      ".",
			Framework: svc.Runtime, // "python" | "node" — Plan B v3.1 T-B.5 dispatches via runtime + legacy fallback
			Build:     "buildpack",
			Env:       svc.Env,
		}
		appNames = append(appNames, svc.Name)
	}

	// Deterministic order — matches loopzerokit's topological sort output
	// + provides stable downstream behavior.
	sort.Strings(appNames)
	return cfg, appNames, nil
}
