// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Regression-lock for the `connect-canonical-fixture.sh` Step 11 fix landed on
// 2026-06-20 during the dev-public Cleanroom-fresh close.
//
// Symptom (caught LIVE during seed-post-cleanroom-all.sh --verify):
//
//	[connect-fixture] ERROR mint_app_jwt: cannot resolve GITHUB_APP_ID
//	                         from loopzero-api pod running env
//
// Root cause: the `kubectl exec deployment/loopzero-api -c <container>` call named a
// container the Deployment did not run. A wrong `-c` selects nothing, kubectl
// errors to stderr, the script suppresses stderr via `2>/dev/null` and swallows
// the exit via `|| true`, so `$app_id` ends up empty and the diagnostic blames a
// missing variable when the cause is the selector.
//
// # What changed, and why this test no longer holds a literal
//
// The 2026-06-20 lock asserted the literal `-c loopzero-api`, the container name of the
// bootstrap chart infra/helm/loopzero-api. That chart was deleted when the pair became
// one chart (docs/decisions.md, "One chart per bootstrap/published pair"); Helmfile
// now installs charts/loopzero-api, whose container is `api` — the same name the
// Tilt-owned Deployment always used. A literal in this test would have gone stale
// the same way the script's did, so the expected name is READ: from the rendered
// chart, and from the Tilt manifest, which must agree with each other and with the
// script.
//
// The audit is a pure function so the negative control can mutate the in-memory
// body and PROVE the guard catches a wrong selector. A guard that passes the
// negative control is theatre.
package charts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	appsV1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/yaml"

	"github.com/getloopscale/loopzero/infra/tests/helm/chartrender"
)

var (
	reExecLoopZeroAPISelector = regexp.MustCompile(`exec deployment/loopzero-api -c (\S+) --`)
	reMintAppJwtFunc          = regexp.MustCompile(`(?m)^mint_app_jwt\(\) \{`)
)

func loadConnectCanonicalFixture(t *testing.T) string {
	t.Helper()
	p := filepath.Join(repoRoot(t), "infra/scripts/local/connect-canonical-fixture.sh")
	b, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

// chartContainerName renders charts/loopzero-api the way Helmfile installs it and
// returns the name of the API container.
func chartContainerName(t *testing.T) string {
	t.Helper()
	chart := publishedChart(t, "loopzero-api")
	out := renderChart(t, chart, "loopzero-api", nil, filepath.Join(chart, "values-bootstrap.yaml"))
	var d appsV1.Deployment
	chartrender.MustUnmarshalK8SYamlByKind(t, out, "Deployment", &d)
	if len(d.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("charts/loopzero-api renders %d containers; this guard assumes one", len(d.Spec.Template.Spec.Containers))
	}
	return d.Spec.Template.Spec.Containers[0].Name
}

// tiltContainerName reads the container name of the Tilt-owned loopzero-api Deployment.
func tiltContainerName(t *testing.T) string {
	t.Helper()
	p := filepath.Join(repoRoot(t), "infra/tilt/api-deployment.yaml")
	b, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	for _, doc := range strings.Split(string(b), "\n---") {
		var d appsV1.Deployment
		if err := yaml.Unmarshal([]byte(doc), &d); err != nil || d.Kind != "Deployment" || d.Name != "loopzero-api" {
			continue
		}
		return d.Spec.Template.Spec.Containers[0].Name
	}
	t.Fatalf("%s declares no Deployment loopzero-api", p)
	return ""
}

// auditConnectFixtureContainerName returns failure messages (empty = guard passes)
// for a script body checked against the container name every install runs.
func auditConnectFixtureContainerName(body, want string) []string {
	var errs []string
	if !reMintAppJwtFunc.MatchString(body) {
		errs = append(errs, "(a) script does NOT define `mint_app_jwt()` — the function this guard locks the container selector for is missing; either it was renamed (update the guard) or removed (re-evaluate the Step 11 chain).")
	}
	matches := reExecLoopZeroAPISelector.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		errs = append(errs, "(b) script contains no `exec deployment/loopzero-api -c <container> --` — the Step 11 read of GITHUB_APP_ID from the pod's running env is gone, or changed shape.")
	}
	for _, m := range matches {
		if m[1] != want {
			errs = append(errs, "(c) script selects container `"+m[1]+"` on deployment/loopzero-api, but the Deployment runs `"+want+"`. A wrong `-c` selects nothing, the error is swallowed, and $app_id ends up empty — the 2026-06-20 failure.")
		}
	}
	return errs
}

// The chart and the Tilt manifest must run the same container name; otherwise
// the script is right for one install path and silently wrong for the other.
func TestLoopZeroAPIContainerNameIsTheSameOnEveryInstallPath(t *testing.T) {
	chart, tilt := chartContainerName(t), tiltContainerName(t)
	if chart != tilt {
		t.Fatalf("charts/loopzero-api runs container %q and the Tilt Deployment runs %q — one `kubectl exec -c` cannot target both", chart, tilt)
	}
}

func TestConnectFixtureContainerName(t *testing.T) {
	body := loadConnectCanonicalFixture(t)
	for _, e := range auditConnectFixtureContainerName(body, chartContainerName(t)) {
		t.Error(e)
	}
}

// The negative control rewrites the selector to a container the Deployment does
// not run and requires the audit to fire.
func TestConnectFixtureContainerName_NegativeControl(t *testing.T) {
	body := loadConnectCanonicalFixture(t)
	want := chartContainerName(t)
	mutated := reExecLoopZeroAPISelector.ReplaceAllString(body, "exec deployment/loopzero-api -c not-"+want+" --")
	if mutated == body {
		t.Fatal("negative-control setup failed: no `exec deployment/loopzero-api -c <container> --` in the script body — the GREEN test is vacuous")
	}
	if errs := auditConnectFixtureContainerName(mutated, want); len(errs) == 0 {
		t.Error("negative control: the audit PASSED a script whose selector names a container the Deployment does not run — the guard would not catch the 2026-06-20 regression")
	}
}
