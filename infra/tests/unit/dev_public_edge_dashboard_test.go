// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The edge serves the dashboard from LOOPZERO_DASHBOARD_DIST, defaulting to a sibling
// checkout's build that the dev droplet does not have. Reloading the live edge without it
// swapped the dashboard's root for a directory that does not exist: app-dev answered 404
// on every page, and --verify logged it as "upstream UP" because it accepted 404
// (measured 2026-09-29, three minutes of outage). The script refuses the reload instead.

// runCaddyUp runs caddy-up-dev-public.sh with a stub `caddy` that records every call.
func runCaddyUp(t *testing.T, dashboard string) (string, string, error) {
	t.Helper()
	bin := t.TempDir()
	calls := filepath.Join(t.TempDir(), "caddy-calls")
	stub := "#!/bin/sh\necho \"$@\" >> " + calls + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "caddy"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", filepath.Join(repoRoot(), "infra", "scripts", "local", "caddy-up-dev-public.sh"), "--dry-run")
	cmd.Env = append(os.Environ(),
		"PATH="+bin+":"+os.Getenv("PATH"),
		"LOOPZERO_DASHBOARD_DIST="+dashboard,
		"LOOPZERO_EDGE_ROUTE_TOKEN=test",
	)
	out, err := cmd.CombinedOutput()
	recorded, _ := os.ReadFile(calls)
	return string(out), string(recorded), err
}

func TestTheEdgeRefusesToServeADashboardThatIsNotBuilt(t *testing.T) {
	out, calls, err := runCaddyUp(t, filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatalf("the script accepted a dashboard directory with no build:\n%s", out)
	}
	if !strings.Contains(out, "LOOPZERO_DASHBOARD_DIST") {
		t.Errorf("the refusal does not name LOOPZERO_DASHBOARD_DIST:\n%s", out)
	}
	if strings.Contains(calls, "reload") || strings.Contains(calls, "start") {
		t.Errorf("caddy was reloaded or started despite the refusal: %q", calls)
	}
}

func TestTheEdgeAcceptsABuiltDashboard(t *testing.T) {
	dist := t.TempDir()
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, _, err := runCaddyUp(t, dist); err != nil {
		t.Fatalf("a built dashboard was refused: %v\n%s", err, out)
	}
}

// A 404 at the dashboard root is the outage above, not a live upstream.
func TestTheEdgeVerifyTreatsADashboard404AsAFailure(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(), "infra", "scripts", "local", "caddy-up-dev-public.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "200|404) log") {
		t.Error("--verify still reports HTTP 404 as an upstream that is up")
	}
}
