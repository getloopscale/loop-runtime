// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package charts

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// infra/helm/mcp-gateway puts every MCP server behind one agentgateway Gateway on the core
// cluster (#470). Each server is an OAuth 2.1 resource server (MCP authorization spec
// 2026-07-28), and the gateway enforces the same contract in front of it: a Hydra RS256
// token whose audience is the server's resource URI. The shapes asserted here are the ones
// the 2026-09-30 spike measured working against agentgateway v1.5.0 — each assertion names
// what failed when the shape was different.

const mcpGatewayValues = `
issuer: https://auth.example.test/
publicOrigin: https://app.example.test
jwks:
  service: ory-hydra-public
  namespace: loopruntime-auth
  port: 4444
  path: /.well-known/jwks.json
gateway:
  nodePort: 30190
servers:
  - name: memory
    path: /mcp/memory
    host: loopruntime-memory.loopruntime-data-plane.svc.cluster.local
    port: 8080
    upstreamPath: /mcp
`

func mcpGatewayChart(t *testing.T) string {
	t.Helper()
	return uniqueChart(t, "mcp-gateway")
}

func mcpGatewayValuesFile(t *testing.T, values string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "values.yaml")
	if err := os.WriteFile(f, []byte(values), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func renderMCPGateway(t *testing.T, values string) string {
	t.Helper()
	return renderChart(t, mcpGatewayChart(t), "mcp-gateway", nil, mcpGatewayValuesFile(t, values))
}

func TestMCPGatewayServiceIsPinnedToTheEdgeNodePort(t *testing.T) {
	rendered := renderMCPGateway(t, mcpGatewayValues)

	gw := renderedObject(t, rendered, "Gateway", "mcp")
	if gw == nil {
		t.Fatal("no Gateway named mcp rendered")
	}
	if got := dig(gw, "spec", "gatewayClassName"); got != "agentgateway" {
		t.Errorf("gatewayClassName = %v, want agentgateway", got)
	}
	ref, _ := dig(gw, "spec", "infrastructure", "parametersRef").(map[string]any)
	if ref["kind"] != "AgentgatewayParameters" || ref["name"] != "mcp" {
		t.Fatalf("Gateway parametersRef = %v, want AgentgatewayParameters/mcp — without it the Service is a LoadBalancer on a random port the edge cannot reach", ref)
	}

	params := renderedObject(t, rendered, "AgentgatewayParameters", "mcp")
	if params == nil {
		t.Fatal("no AgentgatewayParameters named mcp rendered")
	}
	svc, _ := dig(params, "spec", "service", "spec").(map[string]any)
	if svc["type"] != "NodePort" {
		t.Errorf("gateway Service type = %v, want NodePort", svc["type"])
	}
	ports, _ := svc["ports"].([]any)
	if len(ports) != 1 {
		t.Fatalf("gateway Service ports = %v, want exactly the listener port", ports)
	}
	port, _ := ports[0].(map[string]any)
	if port["port"] != 8080 || port["nodePort"] != 30190 {
		t.Errorf("gateway Service port = %v, want port 8080 on nodePort 30190", port)
	}
}

func TestMCPGatewayRoutesTheServerAndItsDiscoveryDocuments(t *testing.T) {
	rendered := renderMCPGateway(t, mcpGatewayValues)

	backend := renderedObject(t, rendered, "AgentgatewayBackend", "mcp-memory")
	if backend == nil {
		t.Fatal("no AgentgatewayBackend mcp-memory rendered")
	}
	targets, _ := dig(backend, "spec", "mcp", "targets").([]any)
	if len(targets) != 1 {
		t.Fatalf("backend targets = %v, want one", targets)
	}
	static, _ := dig(targets[0].(map[string]any), "static").(map[string]any)
	want := map[string]any{"host": "loopruntime-memory.loopruntime-data-plane.svc.cluster.local", "port": 8080, "path": "/mcp", "protocol": "StreamableHTTP"}
	for k, v := range want {
		if static[k] != v {
			t.Errorf("backend static.%s = %v, want %v", k, static[k], v)
		}
	}

	route := renderedObject(t, rendered, "HTTPRoute", "mcp-memory")
	if route == nil {
		t.Fatal("no HTTPRoute mcp-memory rendered")
	}
	var paths []string
	rules, _ := dig(route, "spec", "rules").([]any)
	for _, r := range rules {
		matches, _ := r.(map[string]any)["matches"].([]any)
		for _, m := range matches {
			if v, ok := dig(m.(map[string]any), "path", "value").(string); ok {
				paths = append(paths, v)
			}
		}
	}
	// Without the two well-known matches the gateway answered 401 with a resource_metadata
	// URL that itself 404'd: a client cannot discover the authorization server.
	for _, p := range []string{
		"/mcp/memory",
		"/.well-known/oauth-protected-resource/mcp/memory",
		"/.well-known/oauth-authorization-server/mcp/memory",
	} {
		if !slices.Contains(paths, p) {
			t.Errorf("HTTPRoute mcp-memory matches %v, missing %s", paths, p)
		}
	}
}

func TestMCPGatewayRequiresAHydraTokenBoundToTheServer(t *testing.T) {
	rendered := renderMCPGateway(t, mcpGatewayValues)

	policy := renderedObject(t, rendered, "AgentgatewayPolicy", "mcp-memory")
	if policy == nil {
		t.Fatal("no AgentgatewayPolicy mcp-memory rendered")
	}
	jwt, _ := dig(policy, "spec", "traffic", "jwtAuthentication").(map[string]any)
	if jwt["mode"] != "Strict" {
		t.Errorf("jwtAuthentication.mode = %v, want Strict", jwt["mode"])
	}
	providers, _ := jwt["providers"].([]any)
	if len(providers) != 1 {
		t.Fatalf("providers = %v, want exactly Hydra", providers)
	}
	p := providers[0].(map[string]any)
	if p["issuer"] != "https://auth.example.test/" {
		t.Errorf("issuer = %v, want the Hydra issuer verbatim", p["issuer"])
	}
	const resource = "https://app.example.test/mcp/memory"
	if aud, _ := p["audiences"].([]any); len(aud) != 1 || aud[0] != resource {
		t.Errorf("audiences = %v, want only %s (RFC 8707: a token for another resource must not pass)", aud, resource)
	}
	if got := dig(p, "jwks", "remote", "backendRef", "name"); got != "ory-hydra-public" {
		t.Errorf("jwks backendRef = %v, want ory-hydra-public", got)
	}
	// Written as {value: ...} the gateway published "resource": {"value": ...}, which no
	// RFC 9728 client accepts.
	if got := dig(jwt, "mcp", "resourceMetadata", "resource"); got != resource {
		t.Errorf("resourceMetadata.resource = %#v, want the string %q", got, resource)
	}
	// By default the gateway strips the token; the server validates it too, so it must
	// arrive — and only at the resource it was issued for.
	if _, ok := dig(policy, "spec", "backend", "auth", "passthrough").(map[string]any); !ok {
		t.Errorf("backend.auth.passthrough absent — the resource server would receive no token")
	}
}

func TestMCPGatewayWithoutServersRendersOnlyTheGateway(t *testing.T) {
	rendered := renderMCPGateway(t, "gateway:\n  nodePort: 30190\n")

	if renderedObject(t, rendered, "Gateway", "mcp") == nil {
		t.Error("the Gateway must exist with no server declared, so the edge has a stable target")
	}
	for _, kind := range []string{"AgentgatewayBackend", "HTTPRoute", "AgentgatewayPolicy"} {
		if docs := renderedObjectsOfKind(t, rendered, kind); len(docs) != 0 {
			t.Errorf("%d %s rendered with no server declared", len(docs), kind)
		}
	}
}

func TestMCPGatewayRefusesAServerItCannotProtect(t *testing.T) {
	cases := map[string]string{
		"no issuer":          strings.Replace(mcpGatewayValues, "issuer: https://auth.example.test/", `issuer: ""`, 1),
		"no public origin":   strings.Replace(mcpGatewayValues, "publicOrigin: https://app.example.test", `publicOrigin: ""`, 1),
		"origin with slash":  strings.Replace(mcpGatewayValues, "publicOrigin: https://app.example.test", "publicOrigin: https://app.example.test/", 1),
		"path outside /mcp/": strings.Replace(mcpGatewayValues, "path: /mcp/memory", "path: /memory", 1),
		"uppercase name":     strings.Replace(mcpGatewayValues, "name: memory", "name: Memory", 1),
	}
	for name, values := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := renderChartE(t, mcpGatewayChart(t), "mcp-gateway", nil, mcpGatewayValuesFile(t, values)); err == nil {
				t.Errorf("rendered with %s; a server the gateway cannot bind to a resource must be refused", name)
			}
		})
	}
}
