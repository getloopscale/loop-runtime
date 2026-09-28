// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Plan v1.2 T3.3 (M6) — ValidateServicesJSONNative tests.

package source

import (
	"testing"
)

func TestValidateServicesJSONNative_Valid_ReturnsManifest(t *testing.T) {
	const content = `{
		"version": 2,
		"project": "checkout",
		"services": [
			{
				"name": "api",
				"runtime": "python",
				"port": 8001,
				"proxy": "/api",
				"dev": "x",
				"start": "x",
				"healthcheck": "/health",
				"cors": false,
				"passSetCookie": false
			}
		]
	}`
	m, err := ValidateServicesJSONNative(content, EnvBlocklistEnforce)
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if m.Version != 2 {
		t.Errorf("expected version 2, got %d", m.Version)
	}
	if m.Project != "checkout" {
		t.Errorf("expected project=checkout, got %q", m.Project)
	}
	if len(m.Services) != 1 || m.Services[0].Name != "api" {
		t.Errorf("expected single service named 'api', got %+v", m.Services)
	}
}

func TestValidateServicesJSONNative_PreservesDependsOn(t *testing.T) {
	const content = `{
		"version": 2,
		"project": "checkout",
		"services": [
			{
				"name": "api",
				"runtime": "python",
				"port": 8001,
				"proxy": "/api",
				"dev": "x",
				"start": "x",
				"healthcheck": "/health",
				"cors": false,
				"passSetCookie": false
			},
			{
				"name": "worker",
				"runtime": "node",
				"port": 4001,
				"proxy": "/internal",
				"dev": "x",
				"start": "x",
				"healthcheck": "/health",
				"cors": false,
				"passSetCookie": false,
				"dependsOn": ["api"]
			}
		]
	}`
	m, err := ValidateServicesJSONNative(content, EnvBlocklistEnforce)
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if len(m.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(m.Services))
	}
	if len(m.Services[1].DependsOn) != 1 || m.Services[1].DependsOn[0] != "api" {
		t.Errorf("expected DependsOn=[api], got %v", m.Services[1].DependsOn)
	}
}

func TestValidateServicesJSONNative_PreservesOpenAPI(t *testing.T) {
	const content = `{
		"version": 2,
		"project": "checkout",
		"services": [
			{
				"name": "api",
				"runtime": "python",
				"port": 8001,
				"proxy": "/api",
				"dev": "x",
				"start": "x",
				"openapi": "http://localhost:8001/openapi.json",
				"healthcheck": "/health",
				"cors": false,
				"passSetCookie": false
			}
		]
	}`
	m, err := ValidateServicesJSONNative(content, EnvBlocklistEnforce)
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if m.Services[0].OpenAPI != "http://localhost:8001/openapi.json" {
		t.Errorf("expected OpenAPI preserved, got %q", m.Services[0].OpenAPI)
	}
}

func TestValidateServicesJSONNative_EmptyContent_ReturnsE008(t *testing.T) {
	_, err := ValidateServicesJSONNative("", EnvBlocklistEnforce)
	if err == nil {
		t.Fatal("expected E-008 on empty content, got nil")
	}
}
