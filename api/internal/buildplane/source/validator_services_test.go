// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package source

import (
	"strings"
	"testing"
)

func TestValidateServicesJSON_ValidPayload(t *testing.T) {
	content := `{
		"version": 1,
		"services": [
			{
				"name": "agent",
				"runtime": "python",
				"port": 8001,
				"proxy": "/api/agent",
				"dev": "uvicorn main:app",
				"start": "uvicorn main:app --workers 4",
				"healthcheck": "/health",
				"cors": false,
				"passSetCookie": false
			}
		]
	}`
	m, err := ValidateServicesJSONNative(content)
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if m.Version != 1 {
		t.Errorf("expected version 1, got %d", m.Version)
	}
	appNames := AppNamesFromManifest(m)
	if len(appNames) != 1 || appNames[0] != "agent" {
		t.Errorf("expected appNames=[agent], got %v", appNames)
	}
	agent := findService(m, "agent")
	if agent == nil {
		t.Fatal("expected agent service in manifest")
	}
	if agent.Runtime != "python" {
		t.Errorf("expected runtime=python, got %q", agent.Runtime)
	}
	if agent.Port != 8001 {
		t.Errorf("expected port 8001, got %d", agent.Port)
	}
}

// findService returns the service with the given name from a manifest, or nil.
func findService(m *ServicesManifest, name string) *ServiceEntry {
	for i := range m.Services {
		if m.Services[i].Name == name {
			return &m.Services[i]
		}
	}
	return nil
}

// EC-B3-7 (v3.1) — truncated/empty payload returns E-008.
func TestValidateServicesJSON_EmptyPayload_ReturnsE008(t *testing.T) {
	_, err := ValidateServicesJSONNative("")
	if err == nil {
		t.Fatal("expected validation error on empty payload")
	}
	if !strings.Contains(err.Error(), "E-008") {
		t.Errorf("expected E-008 error code, got %q", err.Error())
	}
}

func TestValidateServicesJSON_TruncatedPayload_ReturnsE008(t *testing.T) {
	// Mid-object truncation
	_, err := ValidateServicesJSONNative(`{"version":1,"services":[`)
	if err == nil {
		t.Fatal("expected validation error on truncated payload")
	}
	if !strings.Contains(err.Error(), "E-008") {
		t.Errorf("expected E-008 error code, got %q", err.Error())
	}
}

// EC-B3-8 (v3.1) — schema validation error includes field path.
func TestValidateServicesJSON_InvalidField_ReturnsErrorWithFieldPath(t *testing.T) {
	// port as string instead of integer
	content := `{
		"version": 1,
		"services": [
			{
				"name": "agent",
				"runtime": "python",
				"port": "8001",
				"proxy": "/api/agent",
				"dev": "uvicorn main:app",
				"start": "uvicorn main:app",
				"healthcheck": "/health",
				"cors": false,
				"passSetCookie": false
			}
		]
	}`
	_, err := ValidateServicesJSONNative(content)
	if err == nil {
		t.Fatal("expected validation error on invalid port type")
	}
	// Error message should reference the field path (e.g., `/services/0/port`)
	if !strings.Contains(err.Error(), "port") {
		t.Errorf("expected error to reference `port` field path, got %q", err.Error())
	}
}

func TestValidateServicesJSON_InvalidRuntime_ReturnsErrorWithEnum(t *testing.T) {
	// runtime "go" — not in enum
	content := `{
		"version": 1,
		"services": [
			{
				"name": "agent",
				"runtime": "go",
				"port": 8001,
				"proxy": "/api/agent",
				"dev": "go run .",
				"start": "./agent",
				"healthcheck": "/health",
				"cors": false,
				"passSetCookie": false
			}
		]
	}`
	_, err := ValidateServicesJSONNative(content)
	if err == nil {
		t.Fatal("expected validation error on invalid runtime enum")
	}
	if !strings.Contains(err.Error(), "runtime") {
		t.Errorf("expected error to reference `runtime` field, got %q", err.Error())
	}
}

func TestValidateServicesJSON_MultipleServices_PreservesNames(t *testing.T) {
	content := `{
		"version": 1,
		"services": [
			{
				"name": "agent",
				"runtime": "python",
				"port": 8001,
				"proxy": "/api/agent",
				"dev": "uvicorn main:app",
				"start": "uvicorn main:app",
				"healthcheck": "/health",
				"cors": false,
				"passSetCookie": false
			},
			{
				"name": "worker",
				"runtime": "node",
				"port": 4001,
				"proxy": "/api/worker",
				"dev": "node worker.js",
				"start": "node worker.js",
				"healthcheck": "/health",
				"cors": false,
				"passSetCookie": false
			}
		]
	}`
	m, err := ValidateServicesJSONNative(content)
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	appNames := AppNamesFromManifest(m)
	if len(appNames) != 2 {
		t.Fatalf("expected 2 services, got %d", len(appNames))
	}
	// Deterministic order
	if appNames[0] != "agent" || appNames[1] != "worker" {
		t.Errorf("expected [agent worker], got %v", appNames)
	}
	worker := findService(m, "worker")
	if worker == nil || worker.Runtime != "node" {
		t.Errorf("expected worker.runtime=node, got %+v", worker)
	}
}

// Sprint 4 plan qa-sprint4-cleanup-2026-06-25 T2.1 — multi-field error aggregation.
// Closes loopzero-cloud#29: ValidateServicesJSONNative previously walked only Causes[0]
// (the FIRST jsonschema leaf cause), so a payload with 3 simultaneous violations
// would surface only the first one — forcing a cascading retry loop (fix violation 1,
// re-upload, hit violation 2, repeat). EC-2 absorption: the new Errors slice is
// populated by NewValidationError for every legacy site so len(ve.Errors) >= 1 holds
// for downstream consumers that switched from ve.Field/ve.Message to ve.Errors[0].
func TestValidateServicesJSONNative_AggregatesAllFieldErrors_closes29(t *testing.T) {
	// Intentionally violates 3 distinct schema constraints at once:
	//   1. project: 'Invalid_UPPER_Project' — fails ^[a-z][a-z0-9-]*$ pattern
	//   2. services[0].runtime: 'BAD_RUNTIME' — fails enum [python, node]
	//   3. services[0].port: 99 — fails minimum (assuming schema sets a >=1024 or similar floor)
	payload := `{
		"version": 2,
		"project": "Invalid_UPPER_Project",
		"services": [{
			"name": "api",
			"runtime": "BAD_RUNTIME",
			"type": "server",
			"dev": "node start",
			"start": "node start",
			"port": 0
		}]
	}`
	_, err := ValidateServicesJSONNative(payload)
	if err == nil {
		t.Fatal("expected validation error for multi-violation payload, got nil")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	// EC-2: legacy Field/Message must still be populated for backward-compat.
	if ve.Field == "" || ve.Message == "" {
		t.Errorf("legacy Field/Message must populate; got Field=%q Message=%q", ve.Field, ve.Message)
	}
	// Sprint 4 invariant: Errors slice contains ALL leaf violations, not just the first.
	if len(ve.Errors) < 2 {
		t.Errorf("expected >= 2 field errors aggregated from jsonschema Causes tree, got %d: %+v",
			len(ve.Errors), ve.Errors)
	}
	// Sanity: each FieldError carries a non-empty Message (Field may be empty
	// for root-level violations like additionalProperties on the document root,
	// where jsonschema/v5 emits InstanceLocation="").
	for i, fe := range ve.Errors {
		if fe.Message == "" {
			t.Errorf("Errors[%d] has empty Message: %+v", i, fe)
		}
	}
}

// EC-2: every legacy ValidationError construction site must populate Errors[0].
// Empty payload triggers the validator.go:98 site (E-008 missing payload) — this asserts
// the constructor populated the slice even though only one violation exists.
func TestValidateServicesJSONNative_EmptyPayload_PopulatesErrorsSlicePerEC2(t *testing.T) {
	_, err := ValidateServicesJSONNative("")
	if err == nil {
		t.Fatal("expected E-008 error for empty payload, got nil")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	// Legacy scalars still work.
	if ve.Field != "services_json" {
		t.Errorf("expected Field='services_json', got %q", ve.Field)
	}
	// EC-2: Errors[0] mirrors the legacy scalars.
	if len(ve.Errors) != 1 {
		t.Fatalf("EC-2: expected len(Errors)==1 for single-violation legacy site, got %d", len(ve.Errors))
	}
	if ve.Errors[0].Field != ve.Field || ve.Errors[0].Message != ve.Message {
		t.Errorf("EC-2: Errors[0] must mirror legacy Field+Message; got Errors[0]=%+v vs Field=%q Message=%q",
			ve.Errors[0], ve.Field, ve.Message)
	}
}
