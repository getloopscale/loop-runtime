// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"gopkg.in/yaml.v3"
)

// MCP reaches the platform through agentgateway on the core cluster (docs/decisions.md,
// "One control plane, and MCP behind agentgateway", #470). agentgateway is a Gateway API
// implementation, and the core cluster had no Gateway API at all, so both land together
// and in every environment the core helmfile serves: a component that exists in one
// environment only is the local-versus-production drift CLAUDE.md Rule 10 forbids.

// gatewayAPIVersion and its digest pin the Gateway API release vendored in
// infra/helm/gateway-api-crds. The digest is the sha256 of the upstream asset
// https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.6.0/standard-install.yaml
// as published, so an edit to the vendored file — or a bump that forgets the digest —
// fails here instead of shipping a Gateway API nobody released.
const (
	gatewayAPIVersion = "v1.6.0"
	gatewayAPIDigest  = "a557172e8348f758479e9ee4000bbbb4b4aa48302a6b73461823ea5349bad56d"
	agentgatewayPin   = "v1.5.0"
)

func TestGatewayAPICRDsAreTheOfficialRelease(t *testing.T) {
	dir := filepath.Join(repoRoot(t), "infra", "helm", "gateway-api-crds")

	manifest, err := os.ReadFile(filepath.Join(dir, "templates", "standard-install.yaml"))
	if err != nil {
		t.Fatalf("read the vendored Gateway API manifest: %v", err)
	}
	sum := sha256.Sum256(manifest)
	if got := hex.EncodeToString(sum[:]); got != gatewayAPIDigest {
		t.Errorf("vendored Gateway API manifest sha256 = %s, want %s (the %s release asset)", got, gatewayAPIDigest, gatewayAPIVersion)
	}

	var chart struct {
		AppVersion string `yaml:"appVersion"`
	}
	raw, err := os.ReadFile(filepath.Join(dir, "Chart.yaml"))
	if err != nil {
		t.Fatalf("read Chart.yaml: %v", err)
	}
	if err := yaml.Unmarshal(raw, &chart); err != nil {
		t.Fatalf("parse Chart.yaml: %v", err)
	}
	if chart.AppVersion != gatewayAPIVersion {
		t.Errorf("gateway-api-crds appVersion = %q, want %q", chart.AppVersion, gatewayAPIVersion)
	}
}

type listedVersionedRelease struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Chart     string `json:"chart"`
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
}

func TestEveryCoreEnvironmentInstallsTheMCPGateway(t *testing.T) {
	want := map[string]listedVersionedRelease{
		"gateway-api-crds":  {Namespace: "kube-system", Chart: "../helm/gateway-api-crds"},
		"agentgateway-crds": {Namespace: "agentgateway-system", Chart: "agentgateway/agentgateway-crds", Version: agentgatewayPin},
		"agentgateway":      {Namespace: "agentgateway-system", Chart: "agentgateway/agentgateway", Version: agentgatewayPin},
		"mcp-gateway":       {Namespace: "loopzero-mcp", Chart: "../helm/mcp-gateway"},
	}
	for _, env := range declaredPlatformEnvironments(t) {
		t.Run(env, func(t *testing.T) {
			t.Parallel()
			out := helmfileRun(t, []string{"LOOPZERO_DB_PASSWORD=unused"},
				"-f", "infra/helmfile/core.helmfile.yaml.gotmpl", "-e", env, "list", "--output=json")
			var releases []listedVersionedRelease
			if err := json.Unmarshal([]byte(out), &releases); err != nil {
				t.Fatalf("decode helmfile list output: %v\n%s", err, truncate(out, 500))
			}
			got := map[string]listedVersionedRelease{}
			for _, r := range releases {
				got[r.Name] = r
			}
			for name, w := range want {
				r, ok := got[name]
				if !ok || !r.Installed {
					t.Errorf("%s: release %s is not installed (declared=%t)", env, name, ok)
					continue
				}
				if r.Namespace != w.Namespace || r.Chart != w.Chart || r.Version != w.Version {
					t.Errorf("%s: release %s = %s/%s@%s, want %s/%s@%s", env, name, r.Namespace, r.Chart, r.Version, w.Namespace, w.Chart, w.Version)
				}
			}
		})
	}
}

// The gateway binds each server to the issuer Hydra actually stamps and to the public
// origin clients reach, so both come from the same environment values Hydra and the edge
// read — a second spelling of either is a token the gateway refuses for no visible reason.
func TestDevPublicMCPGatewayTakesItsIdentityFromTheEnvironment(t *testing.T) {
	values := helmfileValues(t, "core.helmfile.yaml.gotmpl", "dev-public", "mcp-gateway", helmfileCoreEnv...)

	if got := values["issuer"]; got != "https://auth-dev.getloopscale.com/" {
		t.Errorf("issuer = %v, want Hydra's dev-public issuer https://auth-dev.getloopscale.com/", got)
	}
	if got := values["publicOrigin"]; got != "https://app-dev.getloopscale.com" {
		t.Errorf("publicOrigin = %v, want https://app-dev.getloopscale.com", got)
	}
}

// A NodePort the load balancer does not publish is reachable from inside the cluster only;
// the edge on the host would get connection refused.
// The mapping is checked against the node port the chart actually defaults to, so moving
// one without the other fails here.
func TestCoreClusterPublishesTheMCPGateway(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "infra", "helm", "mcp-gateway", "values.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var values struct {
		Gateway struct {
			NodePort int `yaml:"nodePort"`
		} `yaml:"gateway"`
	}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		t.Fatal(err)
	}
	mapping := fmt.Sprintf("18091:%d", values.Gateway.NodePort)
	if !readCoreClusterPorts(t)[mapping] {
		t.Errorf("infra/k3d/core-cluster.yaml does not publish %s on the load balancer", mapping)
	}
}

// The gateway and the server each validate the token (defence in depth), each against its
// own copy of the resource URI. If the copies disagree every request fails one of the two
// checks, so they are compared here rather than trusted to be typed the same twice.
func TestDevPublicMemoryMCPIsBehindTheGatewayWithOneResourceURI(t *testing.T) {
	gw := helmfileValues(t, "core.helmfile.yaml.gotmpl", "dev-public", "mcp-gateway", helmfileCoreEnv...)
	var server map[string]any
	servers, _ := gw["servers"].([]any)
	for _, s := range servers {
		if m, _ := s.(map[string]any); m["name"] == "memory" {
			server = m
		}
	}
	if server == nil {
		t.Fatalf("dev-public mcp.servers = %v, want a memory server", servers)
	}
	if server["host"] != "loopzero-memory-mcp.loopzero-data-plane.svc.cluster.local" || server["port"] != 3100 {
		t.Errorf("memory server target = %v:%v, want the loopzero-memory-mcp Service on 3100", server["host"], server["port"])
	}
	gatewayResource := fmt.Sprint(gw["publicOrigin"], server["path"])

	mem := helmfileValues(t, "core.helmfile.yaml.gotmpl", "dev-public", "loopzero-memory", helmfileCoreEnv...)
	var worker map[string]any
	workers, _ := mem["workers"].([]any)
	for _, w := range workers {
		if m, _ := w.(map[string]any); m["name"] == "mcp" {
			worker = m
		}
	}
	if worker == nil {
		t.Fatalf("loopzero-memory workers = %v, want an mcp worker", workers)
	}
	if worker["port"] != 3100 {
		t.Errorf("mcp worker port = %v, want 3100", worker["port"])
	}
	env, _ := worker["env"].(map[string]any)
	if env["LOOPZERO_MCP_RESOURCE"] != gatewayResource {
		t.Errorf("server resource %v != gateway audience %v", env["LOOPZERO_MCP_RESOURCE"], gatewayResource)
	}
	if env["LOOPZERO_OAUTH_ISSUER"] != gw["issuer"] {
		t.Errorf("server issuer %v != gateway issuer %v", env["LOOPZERO_OAUTH_ISSUER"], gw["issuer"])
	}
	if env["LOOPZERO_MCP_TLS_TERMINATED_UPSTREAM"] != "true" {
		t.Errorf("LOOPZERO_MCP_TLS_TERMINATED_UPSTREAM = %v, want \"true\": the server binds 0.0.0.0 behind the gateway", env["LOOPZERO_MCP_TLS_TERMINATED_UPSTREAM"])
	}
}

// The public edge sends the MCP paths and both discovery prefixes to the gateway's host port.
// Missing either discovery prefix is invisible until a client follows the 401's
// resource_metadata link and lands on the dashboard instead of the metadata document.
func TestEdgeSendsMCPAndItsDiscoveryPathsToTheGateway(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "infra", "local", "Caddyfile"))
	if err != nil {
		t.Fatalf("canonical Caddyfile missing: %v", err)
	}
	cfg := string(b)
	for _, prefix := range []string{
		"/mcp/*",
		"/.well-known/oauth-protected-resource/mcp/*",
		"/.well-known/oauth-authorization-server/mcp/*",
	} {
		re := regexp.MustCompile(`(?s)handle ` + regexp.QuoteMeta(prefix) + ` \{\s*reverse_proxy localhost:18091\b`)
		if !re.MatchString(cfg) {
			t.Errorf("the Caddyfile has no `handle %s { reverse_proxy localhost:18091 … }`", prefix)
		}
	}
}
