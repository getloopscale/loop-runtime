// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// B-165: the `standalone-public` helmfile environment was left as the default of
// `task infra:install:core-dashboard-only` after B-025 removed the three bare
// hostnames (app|cloud|auth.loopzero.example) it serves from infra/local/Caddyfile.
// A task default that names an environment no edge serves breaks silently: the
// install succeeds and nothing answers on the hostname. This test pins the
// invariant so the pairing cannot drift apart again.

// taskEnvDefaultRe captures the environment name in `{{.ENV | default "x"}}`.
var taskEnvDefaultRe = regexp.MustCompile(`\{\{\s*\.ENV\s*\|\s*default\s+"([^"]+)"\s*\}\}`)

// caddySiteBlockRe captures a Caddyfile site address opening a block, e.g.
// `app-dev.loopzero.example {` or `*.preview-dev.loopzero.example {`. Comment lines are
// skipped by the caller, so a hostname that survives only in prose does not
// count as an edge.
var caddySiteBlockRe = regexp.MustCompile(`^([A-Za-z0-9*.:-]+)\s*\{`)

// publicHostRe finds loopzero.example hostnames inside a helmfile environment's
// public URL settings.
var publicHostRe = regexp.MustCompile(`https://([a-z0-9.-]+\.loopzero\.dev)`)

// publicURLKeys are the environment settings that name a hostname a browser or
// an external caller is expected to reach through the local Caddy edge.
var publicURLKeys = []string{"uiBaseUrl", "dashboardOrigin", "publicAuthUrl", "publicCloudUrl"}

// taskfilesWithEnvDefaults returns every Taskfile the repo ships.
func taskfilePaths(t *testing.T) []string {
	t.Helper()
	root := repoRoot()
	paths := []string{filepath.Join(root, "Taskfile.yml")}
	entries, err := os.ReadDir(filepath.Join(root, "taskfiles"))
	if err != nil {
		t.Fatalf("read taskfiles dir: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yml") {
			paths = append(paths, filepath.Join(root, "taskfiles", e.Name()))
		}
	}
	sort.Strings(paths)
	return paths
}

// caddyServedHosts returns the set of hostnames that infra/local/Caddyfile
// actually serves, reading only real site blocks and never comments.
func caddyServedHosts(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(), "infra", "local", "Caddyfile"))
	if err != nil {
		t.Fatalf("read Caddyfile: %v", err)
	}
	served := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if m := caddySiteBlockRe.FindStringSubmatch(line); m != nil {
			served[m[1]] = true
		}
	}
	return served
}

// hostIsServed reports whether host matches a site block, honouring the single
// leading `*.` wildcard label Caddy supports.
func hostIsServed(host string, served map[string]bool) bool {
	if served[host] {
		return true
	}
	for addr := range served {
		if !strings.HasPrefix(addr, "*.") {
			continue
		}
		suffix := addr[1:] // ".preview-dev.loopzero.example"
		if strings.HasSuffix(host, suffix) &&
			!strings.Contains(strings.TrimSuffix(host, suffix), ".") {
			return true
		}
	}
	return false
}

// envPublicHosts reads the public hostnames a helmfile environment points its
// browser-facing URLs at, ignoring commented lines.
func envPublicHosts(t *testing.T, env string) []string {
	t.Helper()
	path := filepath.Join(repoRoot(), "infra", "helmfile", "environments", env+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read helmfile environment %q: %v", env, err)
	}
	seen := map[string]bool{}
	var hosts []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		matchedKey := false
		for _, k := range publicURLKeys {
			if strings.HasPrefix(trimmed, k+":") {
				matchedKey = true
				break
			}
		}
		if !matchedKey {
			continue
		}
		for _, m := range publicHostRe.FindAllStringSubmatch(trimmed, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				hosts = append(hosts, m[1])
			}
		}
	}
	sort.Strings(hosts)
	return hosts
}

// TestNoTaskDefaultsToAnEnvironmentWithoutAnEdge is DoD bullet 3 of B-165 as an
// executable invariant: every environment a task names as its default must have
// its public hostnames served by a real Caddyfile site block.
func TestNoTaskDefaultsToAnEnvironmentWithoutAnEdge(t *testing.T) {
	served := caddyServedHosts(t)
	if len(served) == 0 {
		t.Fatal("infra/local/Caddyfile declares no site blocks; the edge measurement would be vacuous")
	}

	envsDir := filepath.Join(repoRoot(), "infra", "helmfile", "environments")
	checked := 0

	for _, tf := range taskfilePaths(t) {
		data, err := os.ReadFile(tf)
		if err != nil {
			t.Fatalf("read %s: %v", tf, err)
		}
		for _, m := range taskEnvDefaultRe.FindAllStringSubmatch(string(data), -1) {
			env := m[1]
			// Only helmfile environments are in scope; an ENV default naming
			// something else is not an environment claim.
			if _, err := os.Stat(filepath.Join(envsDir, env+".yaml")); err != nil {
				continue
			}
			checked++
			for _, host := range envPublicHosts(t, env) {
				if !hostIsServed(host, served) {
					t.Errorf(
						"%s defaults ENV to %q, whose public hostname %q has no site block in infra/local/Caddyfile; "+
							"a task default must name an environment the edge actually serves (B-165)",
						filepath.Base(tf), env, host,
					)
				}
			}
		}
	}

	if checked == 0 {
		t.Fatal("no taskfile ENV default resolved to a helmfile environment; the invariant would pass vacuously")
	}
}
