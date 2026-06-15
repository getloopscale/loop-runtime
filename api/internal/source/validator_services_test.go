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
