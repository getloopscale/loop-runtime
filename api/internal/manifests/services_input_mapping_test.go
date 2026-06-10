// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package manifests

import (
	"strings"
	"testing"
)

// EC-B3-1 (v3.1) — runtimeToStrategy mapping table.
func TestMapRuntimeToStrategy_Python_ReturnsFastAPI(t *testing.T) {
	strategy, err := mapRuntimeToStrategy("python")
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if strategy != "python-fastapi" {
		t.Errorf(`expected "python-fastapi", got %q`, strategy)
	}
}

func TestMapRuntimeToStrategy_Node_ReturnsHono(t *testing.T) {
	strategy, err := mapRuntimeToStrategy("node")
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if strategy != "node-hono" {
		t.Errorf(`expected "node-hono", got %q`, strategy)
	}
}

func TestMapRuntimeToStrategy_UnknownRuntime_ReturnsError(t *testing.T) {
	_, err := mapRuntimeToStrategy("go")
	if err == nil {
		t.Fatal("expected error for unknown runtime")
	}
	if !strings.Contains(err.Error(), "unknown runtime") {
		t.Errorf("expected actionable error message, got %q", err.Error())
	}
}

func TestMapRuntimeToStrategy_CaseInsensitive(t *testing.T) {
	strategy, err := mapRuntimeToStrategy("PYTHON")
	if err != nil {
		t.Fatalf("expected nil err for uppercase runtime, got %v", err)
	}
	if strategy != "python-fastapi" {
		t.Errorf("expected case-insensitive match, got %q", strategy)
	}
}

// EC-B3-9 (v3.1) — services[] array order preserved into RenderInput.
func TestMapServicesToRenderInput_PreservesOrder(t *testing.T) {
	svc := ServicesManifest{
		Version: 1,
		Services: []ServiceEntry{
			{Name: "agent", Runtime: "python", Port: 8001, Proxy: "/agent", Dev: "x", Start: "x", Healthcheck: "/h"},
			{Name: "worker", Runtime: "node", Port: 4001, Proxy: "/worker", Dev: "x", Start: "x", Healthcheck: "/h"},
			{Name: "scheduler", Runtime: "python", Port: 8002, Proxy: "/sched", Dev: "x", Start: "x", Healthcheck: "/h"},
		},
	}
	opts := RenderOpts{
		TenantID:          "t1",
		ProjectID:         "p1",
		ClusterIssuerName: "ci",
		ImageRefDefault:   "registry.loopzero/app:v1",
	}
	input, err := mapServicesToRenderInput(svc, opts)
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if len(input.Apps) != 3 {
		t.Fatalf("expected 3 apps, got %d", len(input.Apps))
	}
	if input.Apps[0].Name != "agent" || input.Apps[1].Name != "worker" || input.Apps[2].Name != "scheduler" {
		t.Errorf("order not preserved: got [%s %s %s]",
			input.Apps[0].Name, input.Apps[1].Name, input.Apps[2].Name)
	}
}

func TestMapServicesToRenderInput_PerServiceImageRef(t *testing.T) {
	svc := ServicesManifest{
		Version: 1,
		Services: []ServiceEntry{
			{Name: "agent", Runtime: "python", Port: 8001, Proxy: "/agent", Dev: "x", Start: "x", Healthcheck: "/h"},
			{Name: "worker", Runtime: "node", Port: 4001, Proxy: "/worker", Dev: "x", Start: "x", Healthcheck: "/h"},
		},
	}
	opts := RenderOpts{
		ClusterIssuerName: "ci",
		ImageRefByName: map[string]string{
			"agent":  "registry.loopzero/agent:v2",
			"worker": "registry.loopzero/worker:v3",
		},
		ImageRefDefault: "fallback:default",
	}
	input, _ := mapServicesToRenderInput(svc, opts)
	if input.Apps[0].ImageRef != "registry.loopzero/agent:v2" {
		t.Errorf("expected per-service image, got %q", input.Apps[0].ImageRef)
	}
	if input.Apps[1].ImageRef != "registry.loopzero/worker:v3" {
		t.Errorf("expected per-service image, got %q", input.Apps[1].ImageRef)
	}
}

func TestMapServicesToRenderInput_DefaultsFallback(t *testing.T) {
	svc := ServicesManifest{
		Version: 1,
		Services: []ServiceEntry{
			{Name: "agent", Runtime: "python", Port: 8001, Proxy: "/agent", Dev: "x", Start: "x", Healthcheck: "/h"},
		},
	}
	opts := RenderOpts{
		ClusterIssuerName: "ci",
		ImageRefDefault:   "registry.loopzero/agent:vDEFAULT",
		// ImageRefByName intentionally absent — fallback path
	}
	input, _ := mapServicesToRenderInput(svc, opts)
	if input.Apps[0].ImageRef != "registry.loopzero/agent:vDEFAULT" {
		t.Errorf("expected default fallback, got %q", input.Apps[0].ImageRef)
	}
}

func TestMapServicesToRenderInput_UnknownRuntime_PropagatesError(t *testing.T) {
	svc := ServicesManifest{
		Version: 1,
		Services: []ServiceEntry{
			{Name: "agent", Runtime: "rust", Port: 8001, Proxy: "/agent", Dev: "x", Start: "x", Healthcheck: "/h"},
		},
	}
	opts := RenderOpts{ClusterIssuerName: "ci"}
	_, err := mapServicesToRenderInput(svc, opts)
	if err == nil {
		t.Fatal("expected error for unknown runtime")
	}
	if !strings.Contains(err.Error(), `service "agent"`) {
		t.Errorf("expected error to name the failing service, got %q", err.Error())
	}
}

func TestRenderFromServicesManifest_EndToEnd(t *testing.T) {
	svc := ServicesManifest{
		Version: 1,
		Services: []ServiceEntry{
			{Name: "agent", Runtime: "python", Port: 8001, Proxy: "/agent", Dev: "x", Start: "x", Healthcheck: "/h"},
		},
	}
	opts := RenderOpts{
		TenantID:          "tenant-1",
		ProjectID:         "project-1",
		Environment:       "production",
		BuildID:           "build-1",
		ReleaseID:         "release-1",
		BaseDomain:        "loopzero.example",
		ClusterIssuerName: "letsencrypt-prod",
		ImageRefDefault:   "registry.loopzero/agent:v1",
		DefaultReplicas:   1,
	}
	yaml, err := RenderFromServicesManifest(svc, opts)
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if !strings.Contains(yaml, "agent") {
		t.Errorf("expected rendered YAML to include service name; got\n%s", yaml)
	}
	// Existing Render() emits Deployment + Service + IngressRoute. Sanity
	// check key resources are present.
	if !strings.Contains(yaml, "kind: ") {
		t.Errorf("expected K8s manifest output to contain `kind: ...`")
	}
}
