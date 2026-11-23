// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

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

// `task staging:platform:verify` replaces the loopzero-api pod that started before
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
  echo "error: failed to create patch for loopzero-api: if restart has already been triggered within the past second, please wait before attempting to trigger another" >&2
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
		if text, ok := cmd.(string); ok && strings.Contains(text, "deployment/loopzero-api") && !strings.Contains(text, "rollout status") {
			return text
		}
	}
	t.Fatal("platform:verify restarts no deployment/loopzero-api")
	return ""
}

func TestTheVerifyRestartSucceedsRightAfterAnotherRestart(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "kubectl"), []byte(fakeKubectl), 0o755); err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	env := append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "FAKE_STATE="+state, "FAKE_NOW=2026-11-23T00:21:10Z")
	seed := exec.Command(filepath.Join(bin, "kubectl"), "-n", "loopzero-system", "rollout", "restart", "deployment/loopzero-api")
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
	loop := regexp.MustCompile(`for name in ([a-z -]+); do\s+kept=/etc/loopzero-staging/\$name\.json`).FindStringSubmatch(string(data))
	if loop == nil {
		t.Fatal("taskfiles/staging.yml seeds no secret from /etc/loopzero-staging")
	}
	for _, want := range []string{"ghcr-pull", "resend", "stripe"} {
		if !strings.Contains(" "+loop[1]+" ", " "+want+" ") {
			t.Errorf("the host seed loop covers %q, not %s", loop[1], want)
		}
	}
}
