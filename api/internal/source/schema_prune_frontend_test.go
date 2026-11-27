// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package source

import (
	"strings"
	"testing"
)

// T3.2 (plan simplify-deploy-loopzerokit-only v2.0, D5): the services.json `type`
// enum is narrowed to server|worker. `frontend` is removed — LoopZeroKit never
// emits it and the platform serves no static frontends (renderer cascade
// deleted in T3.3). A services.json declaring `type: frontend` must be rejected
// by the schema validator.

func TestSchema_RejectsFrontendType(t *testing.T) {
	content := `{
		"version": 2,
		"project": "myapp",
		"services": [
			{
				"name": "web",
				"runtime": "node",
				"type": "frontend",
				"port": 8080,
				"proxy": "/",
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
		t.Fatal("expected schema rejection for type=frontend, got nil")
	}
}

func TestSchema_AcceptsServerAndWorker(t *testing.T) {
	for _, typ := range []string{"server", "worker"} {
		content := `{
			"version": 2,
			"project": "myapp",
			"services": [
				{
					"name": "svc",
					"runtime": "node",
					"type": "` + typ + `",
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
		if _, err := ValidateServicesJSONNative(content, EnvBlocklistEnforce); err != nil {
			t.Errorf("type=%q must be accepted, got error: %v", typ, err)
		}
	}
}

// Parity: the embedded schema's type enum MUST be exactly server+worker (no
// frontend) — guards against the schema drifting back.
func TestSchema_TypeEnumIsServerWorkerOnly(t *testing.T) {
	s := string(servicesSchemaBytes)
	if strings.Contains(s, `"frontend"`) {
		t.Error("services.schema.json must NOT contain frontend in the type enum (D5)")
	}
	if !strings.Contains(s, `"server"`) || !strings.Contains(s, `"worker"`) {
		t.Error("services.schema.json type enum must contain server + worker")
	}
}
