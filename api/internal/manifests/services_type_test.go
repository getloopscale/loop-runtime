// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// services.json `type` enum propagation tests. Default-to-server fallback.
// (Frontend type + EC-9 rendering rejection removed in
// simplify-deploy-loopzerokit-only v2.0 T3.3 — server|worker only.)

package manifests

import (
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
