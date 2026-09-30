// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package charts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
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
