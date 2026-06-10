// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Plan v1.2 T2.2 (cutover-deep-review-hardening): services.json `type` enum
// propagation tests. Default-to-server fallback + EC-9 frontend-without-
// rendering rejection.

package manifests

import (
	"strings"
	"testing"
)

func baseRenderOpts() RenderOpts {
	return RenderOpts{
		TenantID:          "t1",
		ProjectID:         "p1",
		ClusterIssuerName: "ci",
		ImageRefDefault:   "registry.loopzero/app:v1",
	}
}

func TestMapServicesToRenderInput_TypeWorker_PreservedInOutput(t *testing.T) {
	svc := ServicesManifest{
		Version: 2,
		Services: []ServiceEntry{
			{Name: "queue-worker", Runtime: "node", Type: ServiceTypeWorker, Port: 4001, Proxy: "/internal", Dev: "x", Start: "x", Healthcheck: "/h"},
		},
	}
	input, err := mapServicesToRenderInput(svc, baseRenderOpts())
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if input.Apps[0].Type != ServiceTypeWorker {
		t.Errorf("expected Type=worker, got %q", input.Apps[0].Type)
	}
}

func TestMapServicesToRenderInput_TypeAbsent_DefaultsServer(t *testing.T) {
	svc := ServicesManifest{
		Version: 1,
		Services: []ServiceEntry{
			{Name: "api", Runtime: "python", Port: 8001, Proxy: "/api", Dev: "x", Start: "x", Healthcheck: "/h"},
		},
	}
	input, err := mapServicesToRenderInput(svc, baseRenderOpts())
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if input.Apps[0].Type != ServiceTypeServer {
		t.Errorf("expected default Type=server, got %q", input.Apps[0].Type)
	}
}

func TestMapServicesToRenderInput_TypeFrontend_WithRendering_OK(t *testing.T) {
	svc := ServicesManifest{
		Version: 2,
		Services: []ServiceEntry{
			{
				Name:      "web",
				Runtime:   "node",
				Type:      ServiceTypeFrontend,
				Port:      3000,
				Proxy:     "/app",
				Dev:       "x",
				Start:     "x",
				Healthcheck: "/h",
				Rendering: &FrontendRendering{Mode: "static", AssetsDir: "dist"},
			},
		},
	}
	input, err := mapServicesToRenderInput(svc, baseRenderOpts())
	if err != nil {
		t.Fatalf("expected nil err for frontend with rendering, got %v", err)
	}
	if input.Apps[0].Type != ServiceTypeFrontend {
		t.Errorf("expected Type=frontend preserved, got %q", input.Apps[0].Type)
	}
}

// EC-9 (v1.1): frontend without rendering metadata MUST return an actionable
// error, not silently fall through to server emit.
func TestMapServicesToRenderInput_TypeFrontendWithoutRendering_ReturnsError(t *testing.T) {
	svc := ServicesManifest{
		Version: 2,
		Services: []ServiceEntry{
			{Name: "web", Runtime: "node", Type: ServiceTypeFrontend, Port: 3000, Proxy: "/app", Dev: "x", Start: "x", Healthcheck: "/h"},
		},
	}
	_, err := mapServicesToRenderInput(svc, baseRenderOpts())
	if err == nil {
		t.Fatal("expected error for frontend without rendering, got nil")
	}
	if !strings.Contains(err.Error(), "frontend") || !strings.Contains(err.Error(), "rendering") {
		t.Errorf("expected actionable error mentioning 'frontend' and 'rendering', got: %v", err)
	}
}

func TestResolveServiceType_EmptyDefaultsServer(t *testing.T) {
	if got := resolveServiceType(""); got != ServiceTypeServer {
		t.Errorf("expected default server, got %q", got)
	}
}

func TestResolveServiceType_NonEmptyPreserved(t *testing.T) {
	if got := resolveServiceType(ServiceTypeWorker); got != ServiceTypeWorker {
		t.Errorf("expected worker preserved, got %q", got)
	}
}
