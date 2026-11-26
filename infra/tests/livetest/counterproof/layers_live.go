// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// LIVE layers 4-7 of the counter-proof v2 per T3.4 of plan canonical-tests-rewrite-v2 v1.0.
// These wrap exec calls to $LOOPRUNTIME CLI + kubectl + HTTP probes that exercise a
// real deploy/delete cycle on dev-public. Per ADR D5: the old chain remains
// executable; this is the new path's equivalent.

package counterproof

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// LiveConfig captures everything needed to execute layers 4-7.
type LiveConfig struct {
	LoopRuntimeCLI string        // path to compiled $LOOPRUNTIME binary
	ProjectName    string        // counter-proof-{ts}
	FixtureDir     string        // /tmp/counter-proof-deploy-{ts}/
	EnvName        string        // production
	PollInterval   time.Duration // default 5s
	BuildTimeout   time.Duration // default 15m (Buildkit cold cache)
	HTTPProbeURL   string        // assigned after deploy success
	KubeContext    string        // k3d-loopruntime-cell-0 (for verification)
	TenantNs       string        // loopruntime-{tenant-slug}-{project}-{env} per canonical formula
}

// DefaultLiveConfig with conventional values.
func DefaultLiveConfig(timestamp string) LiveConfig {
	return LiveConfig{
		LoopRuntimeCLI: "loopruntime",
		ProjectName:    "counter-proof-" + timestamp,
		FixtureDir:     filepath.Join("/tmp", "counter-proof-deploy-"+timestamp),
		EnvName:        "production",
		PollInterval:   5 * time.Second,
		BuildTimeout:   15 * time.Minute,
		KubeContext:    "k3d-loopruntime-cell-0",
	}
}

// Layer5_ProofOfLife HTTP-probes the deployed app via port-forward and asserts /health=200.
func Layer5_ProofOfLife(ctx context.Context, cfg LiveConfig) Layer {
	c := Layer{N: 5, Name: "proof of life HTTP + SecurityContext"}
	if cfg.HTTPProbeURL == "" {
		c.Detail = "HTTPProbeURL not set — call Layer4 first to populate"
		c.Skipped = true
		return c
	}

	req, err := http.NewRequestWithContext(ctx, "GET", cfg.HTTPProbeURL+"/health", nil)
	if err != nil {
		c.Detail = fmt.Sprintf("build req: %v", err)
		return c
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		c.Detail = fmt.Sprintf("GET /health: %v", err)
		return c
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		c.Detail = fmt.Sprintf("GET /health: status %d, want 200", resp.StatusCode)
		return c
	}
	c.Detail = fmt.Sprintf("HTTP 200 at %s/health", cfg.HTTPProbeURL)
	c.OK = true
	return c
}

// createMinimalFixture writes a Node.js Hello-World fixture used by Layer 4.
func createMinimalFixture(dir, projectName string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	files := map[string]string{
		"loopzero.yaml": fmt.Sprintf(`project: %s
apps:
  api:
    framework: node
    build: ""
`, projectName),
		"package.json":      `{"name":"counter-proof","version":"0.0.1","main":"index.js","scripts":{"start":"node index.js"}}`,
		"package-lock.json": `{"name":"counter-proof","version":"0.0.1","lockfileVersion":3,"requires":true,"packages":{}}`,
		"index.js": fmt.Sprintf(`const http = require('http');
http.createServer((req, res) => {
  if (req.url === '/health') { res.end('ok'); return; }
  res.writeHead(200, {'Content-Type':'application/json'});
  res.end(JSON.stringify({project: '%s'}));
}).listen(3000);`, projectName),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			return err
		}
	}
	// git init + commit (loopruntime deploy requires a git repo per autodeploy.go pattern)
	return runIn(dir, "git", "init", "-q")
}

func runIn(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	return cmd.Run()
}
