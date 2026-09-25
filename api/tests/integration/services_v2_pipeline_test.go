// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Plan v1.2 T2.4 (cutover-deep-review-hardening): cross-product integration
// gate. v2 services.json flows through ValidateServicesJSON with its project
// and each service's type preserved.

package integration

import (
	"testing"

	"github.com/getloopscale/loopzero/api/internal/manifests"
	"github.com/getloopscale/loopzero/api/internal/source"
)

const v2ServicesJSON = `{
	"version": 2,
	"project": "checkout",
	"services": [
		{
			"name": "api",
			"runtime": "python",
			"type": "server",
			"port": 8001,
			"proxy": "/api",
			"dev": "uvicorn main:app",
			"start": "uvicorn main:app --workers 4",
			"healthcheck": "/health",
			"cors": false,
			"passSetCookie": false
		},
		{
			"name": "queue",
			"runtime": "node",
			"type": "worker",
			"port": 4001,
			"proxy": "/internal/queue",
			"dev": "tsx watch src/worker.ts",
			"start": "node dist/worker.js",
			"healthcheck": "/health",
			"cors": false,
			"passSetCookie": false
		}
	]
}`

func TestPhaseDE2E_V2Schema_PreservesProjectAndType(t *testing.T) {
	m, err := source.ValidateServicesJSONNative(v2ServicesJSON, source.EnvBlocklistEnforce)
	if err != nil {
		t.Fatalf("v2 services.json must validate, got: %v", err)
	}
	if got := source.ProjectNameFromManifest(m); got != "checkout" {
		t.Errorf("expected project=checkout from v2 manifest, got %q", got)
	}
	appNames := source.AppNamesFromManifest(m)
	if len(appNames) != 2 {
		t.Fatalf("expected 2 services, got %d", len(appNames))
	}

	// The validated manifest keeps each service's declared type: the renderer
	// reads it to choose a worker (no traffic) or a server rollout.
	if got := m.Services[0].Type; got != manifests.ServiceTypeServer {
		t.Errorf("expected first service type=server, got %q", got)
	}
	if got := m.Services[1].Type; got != manifests.ServiceTypeWorker {
		t.Errorf("expected second service type=worker, got %q", got)
	}
}

func TestPhaseDE2E_V1Schema_FallsBackToServicesBundle(t *testing.T) {
	const v1 = `{
		"version": 1,
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
	m, err := source.ValidateServicesJSONNative(v1, source.EnvBlocklistEnforce)
	if err != nil {
		t.Fatalf("v1 must validate (deprecated path), got: %v", err)
	}
	// EC-2 ADR D10: v1 fallback stays "services-bundle" to preserve Gitea
	// repo lineage. Any drift to "default" silently orphans live ArgoCD Apps.
	if got := source.ProjectNameFromManifest(m); got != "services-bundle" {
		t.Errorf("EC-2: v1 project fallback MUST be services-bundle (ADR D10), got %q", got)
	}
}
