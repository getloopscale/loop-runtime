// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Plan v1.2 T2.4 (cutover-deep-review-hardening): cross-product integration
// gate. v2 services.json flows through ValidateServicesJSON → RenderInput,
// preserving project + service type all the way to the K8s emitter input.

package integration

import (
	"testing"

	"github.com/loopzero/loopzero/api/internal/manifests"
	"github.com/loopzero/loopzero/api/internal/source"
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
	t.Setenv("LOOPZERO_ENV_BLOCKLIST_MODE", "enforce")
	m, err := source.ValidateServicesJSONNative(v2ServicesJSON)
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

	// Translate through the mapping boundary the same way the production
	// orchestrator does (RenderInput consumer).
	svc := manifests.ServicesManifest{
		Version: 2,
		Services: []manifests.ServiceEntry{
			{Name: "api", Runtime: "python", Type: manifests.ServiceTypeServer, Port: 8001, Proxy: "/api", Dev: "x", Start: "x", Healthcheck: "/health"},
			{Name: "queue", Runtime: "node", Type: manifests.ServiceTypeWorker, Port: 4001, Proxy: "/internal/queue", Dev: "x", Start: "x", Healthcheck: "/health"},
		},
	}
	input, err := manifests.RenderFromServicesManifestInput(svc, manifests.RenderOpts{
		TenantID:          "tenant-1",
		ProjectID:         source.ProjectNameFromManifest(m),
		BaseDomain:        "test.local",
		ClusterIssuerName: "letsencrypt-staging",
		ImageRefDefault:   "registry.loopzero/app:v1",
	})
	if err != nil {
		t.Fatalf("RenderInput build failed: %v", err)
	}
	if len(input.Apps) != 2 {
		t.Fatalf("expected 2 apps in RenderInput, got %d", len(input.Apps))
	}
	if input.Apps[0].Type != manifests.ServiceTypeServer {
		t.Errorf("expected first app type=server, got %q", input.Apps[0].Type)
	}
	if input.Apps[1].Type != manifests.ServiceTypeWorker {
		t.Errorf("expected second app type=worker, got %q", input.Apps[1].Type)
	}
}

func TestPhaseDE2E_V1Schema_FallsBackToServicesBundle(t *testing.T) {
	t.Setenv("LOOPZERO_ENV_BLOCKLIST_MODE", "enforce")
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
	m, err := source.ValidateServicesJSONNative(v1)
	if err != nil {
		t.Fatalf("v1 must validate (deprecated path), got: %v", err)
	}
	// EC-2 ADR D10: v1 fallback stays "services-bundle" to preserve Gitea
	// repo lineage. Any drift to "default" silently orphans live ArgoCD Apps.
	if got := source.ProjectNameFromManifest(m); got != "services-bundle" {
		t.Errorf("EC-2: v1 project fallback MUST be services-bundle (ADR D10), got %q", got)
	}
}
