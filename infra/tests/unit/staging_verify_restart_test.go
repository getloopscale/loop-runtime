// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package unit

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// `task staging:platform:verify` replaces the loopruntime-api pod that started before
// post-core ran. The argocd-token seed restarts the same Deployment a moment before, and
// `kubectl rollout restart` stamps kubectl.kubernetes.io/restartedAt to the second: a
// second restart within that second leaves the stamp unchanged, and kubectl refuses it
// ("if restart has already been triggered within the past second"). That failed the
// staging deploys of c2a7e09 and 6979865.

// fakeKubectl stands in for kubectl with its restart rule: `rollout restart` stamps the
// second FAKE_NOW names, `patch` stamps what the patch carries, and a stamp equal to the
// one already there is refused as kubectl refuses it.
const fakeKubectl = `#!/usr/bin/env bash
set -euo pipefail
state="$FAKE_STATE/restartedAt"
case " $* " in
  *" rollout restart "*) stamp="$FAKE_NOW" ;;
  *" patch "*) stamp=$(printf '%s' "$*" | sed -n 's/.*restartedAt\\*"*:\\*"*\([^"\\]*\).*/\1/p') ;;
  *) exit 0 ;;
esac
[ -n "$stamp" ] || { echo "fake kubectl: no restartedAt in: $*" >&2; exit 2; }
if [ -f "$state" ] && [ "$(cat "$state")" = "$stamp" ]; then
  echo "error: failed to create patch for loopruntime-api: if restart has already been triggered within the past second, please wait before attempting to trigger another" >&2
  exit 1
fi
printf '%s' "$stamp" >"$state"
`

func stagingVerifyRestart(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(), "taskfiles", "staging.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var taskfile struct {
		Tasks map[string]struct {
			Cmds []any `yaml:"cmds"`
		} `yaml:"tasks"`
	}
	if err := yaml.Unmarshal(data, &taskfile); err != nil {
		t.Fatalf("parse taskfiles/staging.yml: %v", err)
	}
	verify, ok := taskfile.Tasks["platform:verify"]
	if !ok {
		t.Fatal("no platform:verify task in taskfiles/staging.yml")
	}
	for _, cmd := range verify.Cmds {
		if text, ok := cmd.(string); ok && strings.Contains(text, "deployment/loopruntime-api") && !strings.Contains(text, "rollout status") {
			return text
		}
	}
	t.Fatal("platform:verify restarts no deployment/loopruntime-api")
	return ""
}

func TestTheVerifyRestartSucceedsRightAfterAnotherRestart(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(fakeKubectl), 0o755); err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	env := append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "FAKE_STATE="+state, "FAKE_NOW=2026-11-23T00:21:10Z")
	seed := exec.Command(filepath.Join(bin, "kubectl"), "-n", "loopruntime-system", "rollout", "restart", "deployment/loopruntime-api")
	seed.Env = env
	if out, err := seed.CombinedOutput(); err != nil {
		t.Fatalf("the seed's restart: %v\n%s", err, out)
	}

	verify := exec.Command("bash", "-euo", "pipefail", "-c", stagingVerifyRestart(t))
	verify.Env = env
	out, err := verify.CombinedOutput()

	if err != nil {
		t.Fatalf("the verify restart failed right after the seed's: %v\n%s", err, out)
	}
}

// The credentials third parties issue are kept on the host, outside the emulator a
// rebuild starts from scratch, and seeded into Secrets Manager on every platform sync.
func TestStagingSeedsTheThirdPartyCredentialsFromTheHost(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(), "taskfiles", "staging.yml"))
	if err != nil {
		t.Fatal(err)
	}
	loop := regexp.MustCompile(`for name in ([a-z -]+); do\s+kept=/etc/loopruntime-staging/\$name\.json`).FindStringSubmatch(string(data))
	if loop == nil {
		t.Fatal("taskfiles/staging.yml seeds no secret from /etc/loopruntime-staging")
	}
	for _, want := range []string{"ghcr-pull", "resend", "stripe"} {
		if !strings.Contains(" "+loop[1]+" ", " "+want+" ") {
			t.Errorf("the host seed loop covers %q, not %s", loop[1], want)
		}
	}
}

// loopruntime-api's Loki address is the build cluster's ALB, read from the Ingress status: a
// deploy reads the previous one before the core sync, and the core sync that runs after
// the build one (with Promtail and Prometheus) re-renders loopruntime-api with it, which is
// what a rebuild, whose first core sync has no ALB yet, relies on.
func TestStagingSyncsTheApiAgainOnceTheLokiALBIsKnown(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(), "taskfiles", "staging.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	firstCore := strings.Index(text, "helmfile --selector 'name!=promtail' -f infra/helmfile/core.helmfile.yaml.gotmpl")
	earlyLoki := strings.Index(text, `eval "$(bash infra/staging/monitoring-addresses.sh loki)"`)
	if firstCore < 0 || earlyLoki < 0 || earlyLoki > firstCore {
		t.Errorf("the previous deploy's Loki ALB is not read before the first core sync (loki read at %d, core sync at %d)", earlyLoki, firstCore)
	}
	if !regexp.MustCompile(`helmfile( --selector name=[a-z-]+)*? --selector name=loopruntime-api( --selector name=[a-z-]+)* -f infra/helmfile/core\.helmfile\.yaml\.gotmpl`).MatchString(text) {
		t.Error("no core sync after the build one re-renders loopruntime-api")
	}
}

// The cells' broker store calls core's secret-fetch ALB, whose DNSName exists once the
// core sync has made it: the platform task reads it after the core sync and before the
// cells render.
func TestStagingReadsTheCoreAPIALBBetweenTheCoreAndCellSyncs(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(), "taskfiles", "staging.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	firstCore := strings.Index(text, "helmfile --selector 'name!=promtail' -f infra/helmfile/core.helmfile.yaml.gotmpl")
	read := strings.Index(text, `eval "$(bash infra/staging/monitoring-addresses.sh core-api --wait `)
	cells := strings.Index(text, "helmfile -f infra/helmfile/cell.helmfile.yaml.gotmpl")
	if firstCore < 0 || read < 0 || cells < 0 || !(firstCore < read && read < cells) {
		t.Errorf("the core API's ALB is not read between the core sync and the cells (core %d, read %d, cells %d)", firstCore, read, cells)
	}
}

// The per-cell broker tokens are seeded once loopruntime-api is verified Ready (the seed
// restarts it and waits for it to settle), in the platform task itself, so a rebuild
// and every redeploy write them.
func TestStagingSeedsTheBrokerTokensAfterTheAPIIsVerified(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(), "taskfiles", "staging.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var taskfile struct {
		Tasks map[string]struct {
			Cmds []any `yaml:"cmds"`
		} `yaml:"tasks"`
	}
	if err := yaml.Unmarshal(data, &taskfile); err != nil {
		t.Fatalf("parse taskfiles/staging.yml: %v", err)
	}
	verify, seed := -1, -1
	for i, cmd := range taskfile.Tasks["platform"].Cmds {
		switch c := cmd.(type) {
		case map[string]any:
			if c["task"] == "platform:verify" {
				verify = i
			}
		case string:
			if strings.Contains(c, "bootstrap-aws-clusters.sh broker-tokens") {
				seed = i
			}
		}
	}
	if verify < 0 || seed < 0 || seed < verify {
		t.Errorf("staging:platform does not seed the broker tokens after platform:verify (verify step %d, seed step %d)", verify, seed)
	}
}
