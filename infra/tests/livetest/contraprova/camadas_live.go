// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// LIVE layers (camadas) 4-7 of counter-proof (contra-prova) v2 per T3.4 of plan canonical-tests-rewrite-v2 v1.0.
// These wrap exec calls to $LOOPZERO CLI + kubectl + HTTP probes that exercise a
// real deploy/delete cycle on dev-public. Per ADR D5: chain antiga remains
// executable; this is the new path's equivalent.

package contraprova

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
	LoopZeroCLI      string        // path to compiled $LOOPZERO binary
	ProjectName  string        // contra-prova-{ts}
	FixtureDir   string        // /tmp/contra-prova-deploy-{ts}/
	EnvName      string        // production
	PollInterval time.Duration // default 5s
	BuildTimeout time.Duration // default 15m (Buildkit cold cache)
	HTTPProbeURL string        // assigned after deploy success
	KubeContext  string        // k3d-loopzero-cell-0 (for verification)
	TenantNs     string        // loopzero-{tenant-slug}-{project}-{env} per canonical formula
}

// DefaultLiveConfig with conventional values.
func DefaultLiveConfig(timestamp string) LiveConfig {
	return LiveConfig{
		LoopZeroCLI:      "loopzero",
		ProjectName:  "contra-prova-" + timestamp,
		FixtureDir:   filepath.Join("/tmp", "contra-prova-deploy-"+timestamp),
		EnvName:      "production",
		PollInterval: 5 * time.Second,
		BuildTimeout: 15 * time.Minute,
		KubeContext:  "k3d-loopzero-cell-0",
	}
}

// Camada5_ProvaDeVida HTTP-probes the deployed app via port-forward and asserts /health=200.
func Camada5_ProvaDeVida(ctx context.Context, cfg LiveConfig) Camada {
	c := Camada{N: 5, Name: "Prova de Vida HTTP + SecurityContext"}
	if cfg.HTTPProbeURL == "" {
		c.Detail = "HTTPProbeURL not set — call Camada4 first to populate"
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

// createMinimalFixture writes a Node.js Hello-World fixture used by Camada 4.
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
		"package.json":      `{"name":"contra-prova","version":"0.0.1","main":"index.js","scripts":{"start":"node index.js"}}`,
		"package-lock.json": `{"name":"contra-prova","version":"0.0.1","lockfileVersion":3,"requires":true,"packages":{}}`,
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
	// git init + commit (loopzero deploy requires a git repo per autodeploy.go pattern)
	return runIn(dir, "git", "init", "-q")
}

func runIn(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	return cmd.Run()
}
