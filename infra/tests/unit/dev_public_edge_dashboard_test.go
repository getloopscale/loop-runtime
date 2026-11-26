// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package unit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The edge serves the dashboard from LOOP_CONSOLE_DASHBOARD_DIST, defaulting to a sibling
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
		"LOOP_CONSOLE_DASHBOARD_DIST="+dashboard,
		"LOOP_RUNTIME_EDGE_ROUTE_TOKEN=test",
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
	if !strings.Contains(out, "LOOP_CONSOLE_DASHBOARD_DIST") {
		t.Errorf("the refusal does not name LOOP_CONSOLE_DASHBOARD_DIST:\n%s", out)
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

// The dashboard used to be served from a directory copied by hand, with no record of the
// commit it was built from (#462). The edge now takes it from the pinned loop-console-dashboard
// image: when LOOP_CONSOLE_DASHBOARD_IMAGE is given and no LOOP_CONSOLE_DASHBOARD_DIST overrides it, the
// bundle is copied out of a short-lived pod on the core cluster — which already
// authenticates to the registry — into a directory named after the tag, and the edge
// serves that.
func TestTheEdgeServesTheDashboardFromItsPinnedImage(t *testing.T) {
	bin, home := t.TempDir(), t.TempDir()
	log := filepath.Join(t.TempDir(), "calls")
	stubs := map[string]string{
		"caddy": "echo \"caddy $*\" >> " + log,
		"curl":  "exit 0", // the admin API answers: reload, do not start
		// kubectl cp <pod>:/usr/share/nginx/html <dest> — lay down a bundle at <dest>.
		"kubectl": "echo \"kubectl $*\" >> " + log + "\n" +
			"case \"$*\" in *' cp '*) for a; do dest=\"$a\"; done; mkdir -p \"$dest\" && echo '<html>' > \"$dest/index.html\";; esac",
	}
	for name, body := range stubs {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	image := "registry.example/loop-console-dashboard:0123456789abcdef0123456789abcdef01234567"
	cmd := exec.Command("bash", filepath.Join(repoRoot(), "infra", "scripts", "local", "caddy-up-dev-public.sh"))
	env := []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "HOME=" + home, "LOOP_CONSOLE_DASHBOARD_IMAGE=" + image, "LOOP_RUNTIME_EDGE_ROUTE_TOKEN=test"}
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "LOOP_CONSOLE_DASHBOARD_DIST=") && !strings.HasPrefix(e, "PATH=") && !strings.HasPrefix(e, "HOME=") {
			env = append(env, e)
		}
	}
	cmd.Env = env

	out, err := cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("the edge did not come up from the pinned image: %v\n%s", err, out)
	}
	dist := filepath.Join(home, ".cache", "loop-runtime", "dashboard", "0123456789abcdef0123456789abcdef01234567")
	if _, err := os.Stat(filepath.Join(dist, "index.html")); err != nil {
		t.Errorf("no bundle at %s: %v", dist, err)
	}
	calls, _ := os.ReadFile(log)
	for _, want := range []string{"run", image, " cp ", "delete pod", "caddy reload"} {
		if !strings.Contains(string(calls), want) {
			t.Errorf("calls lack %q:\n%s", want, calls)
		}
	}
}
