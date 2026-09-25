// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package source

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
		return nil, NewValidationError(
			"services_json",
			"E-008: missing payload (services.json content is empty)",
		)
	}

	schema, err := compileSchema()
	if err != nil {
		return nil, NewValidationError(
			"_internal",
			fmt.Sprintf("schema compile failed: %s", err.Error()),
		)
	}

	var raw interface{}
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return nil, NewValidationError(
			"services_json",
			fmt.Sprintf("E-008: invalid JSON payload: %s", err.Error()),
		)
	}

	if err := schema.Validate(raw); err != nil {
		// Sprint 4 T2.2 (closes loopzero-cloud#29): walk the FULL Causes tree,
		// collecting every leaf violation into a flat FieldError slice. Replaces
		// the legacy `leaf := ve.Causes[0]` single-cause walk (which masked
		// simultaneous violations and produced a cascading-retry trap for the
		// uploading user). Legacy Field/Message scalars are set from the first
		// leaf for backward-compat with consumers that haven't migrated to the
		// Errors slice.
		// errors.As rather than a direct assertion: an error wrapped anywhere in
		// the chain would defeat the assertion, and the user would get a generic
		// message instead of the per-field error list this function exists to produce.
		var asValidErr *jsonschema.ValidationError
		if errors.As(err, &asValidErr) {
			fieldErrors := flattenJSONSchemaCauses(asValidErr)
			if len(fieldErrors) == 0 {
				// Defensive: top-level error with no Causes (rare — e.g.,
				// type-mismatch at root). Surface the top-level message.
				return nil, NewValidationError(
					"services_json",
					fmt.Sprintf("%s: %s", asValidErr.InstanceLocation, asValidErr.Message),
				)
			}
			first := fieldErrors[0]
			return nil, &ValidationError{
				Field:   first.Field,
				Message: first.Message,
				Errors:  fieldErrors,
			}
		}
		return nil, NewValidationError("services_json", err.Error())
	}

	var manifest ServicesManifest
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		return nil, NewValidationError(
			"services_json",
			fmt.Sprintf("decode manifest: %s", err.Error()),
		)
	}

	if envErr := validateReservedEnvKeys(&manifest, slog.Default()); envErr != nil {
		return nil, envErr
	}

	return &manifest, nil
}

// flattenJSONSchemaCauses recursively walks the jsonschema.ValidationError
// Causes tree and emits one FieldError per LEAF (a cause with no further
// sub-causes). The InstanceLocation becomes Field; the schema message becomes
// Message. Sprint 4 T2.2 closes loopzero-cloud#29 (multi-field error aggregation).
//
// Walk order is depth-first, left-to-right — matches the order jsonschema/v5
// emits the Causes slice, which makes the first leaf deterministic and
// stable for the legacy Field/Message scalars (back-compat).
func flattenJSONSchemaCauses(ve *jsonschema.ValidationError) []FieldError {
	if ve == nil {
		return nil
	}
	var out []FieldError
	var walk func(*jsonschema.ValidationError)
	walk = func(n *jsonschema.ValidationError) {
		if n == nil {
			return
		}
		if len(n.Causes) == 0 {
			out = append(out, FieldError{
				Field:   n.InstanceLocation,
				Message: n.Message,
			})
			return
		}
		for _, c := range n.Causes {
			walk(c)
		}
	}
	walk(ve)
	return out
}
