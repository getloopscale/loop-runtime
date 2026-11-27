// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package secrets

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// rotate-secret.sh (T8.1 EC-22 of plan 2026-06-02-loop-runtime-api-vault-integration)
// writes a new value to OpenBao, refreshes every ExternalSecret that reads the path,
// and restarts every Deployment that consumes one. A path two binaries read must
// restart both, a typo must stop before anything is written, and a failed write
// must stop before anything is restarted.

const rotateScript = openbaoScripts + "/rotate-secret.sh"

// typoRefusal is the refusal for a path no inventory ExternalSecret reads.
var typoRefusal = regexp.MustCompile(`no ExternalSecret in .* reads 'api/secrets/typo/typoed'`)

// rotateProbe is the value the leak test hands the script and then looks for.
const rotateProbe = "fixture-rotation-value-must-not-appear"

func init() {
	registerFakes("rotate-secret", map[string]fakeTool{
		"bao": func(args []string) int {
			recordCall("FAKE_BAO_LOG", "BAO_CALL", args)
			return envExit("FAKE_BAO_EXIT")
		},
		"kubectl": rotateKubectl,
	})
}

func rotateKubectl(args []string) int {
	recordCall("FAKE_KUBECTL_LOG", "KUBECTL_CALL", args)
	switch {
	case argsMatch(args, "get externalsecret", "jsonpath"):
		status := os.Getenv("FAKE_ES_STATUS")
		if status == "" {
			status = "True"
		}
		fmt.Println(status)
		return 0
	case argsMatch(args, "rollout restart deployment"):
		return envExit("FAKE_RESTART_EXIT")
	case argsMatch(args, "rollout status deployment"):
		return envExit("FAKE_STATUS_EXIT")
	default:
		return envExit("FAKE_KUBECTL_EXIT")
	}
}

// baoSession is the environment of a non-dry-run rotation. The values only need
// to be non-empty: the fake bao never dials them.
var baoSession = []string{"BAO_ADDR=fixture", "BAO_TOKEN=fixture"}

func runRotate(t *testing.T, env []string, args ...string) scriptRun {
	t.Helper()
	requireTools(t, "yq", "jq")
	return runScript(t, rotateScript, "rotate-secret", env, args...)
}

func TestRotateSecretRequiresAnEnv(t *testing.T) {
	r := runRotate(t, nil, "--path", "api/secrets/gitea/token", "--value", "v")
	expectExit(t, r, 1, "env required")
}

func TestRotateSecretRequiresAValueOutsideDryRun(t *testing.T) {
	r := runRotate(t, nil, "--env", "vault-local", "--path", "api/secrets/gitea/token")
	expectExit(t, r, 1, "value required")
}

func TestRotateSecretRequiresBaoAddrOutsideDryRun(t *testing.T) {
	r := runRotate(t, nil, "--env", "vault-local", "--path", "api/secrets/gitea/token", "--value", "v")
	expectExit(t, r, 1, "BAO_ADDR env var required")
}

// EC-22: a path no ExternalSecret reads is almost always a typo, and writing it
// would put a value in OpenBao that nothing will ever deliver.
func TestRotateSecretRefusesAPathNoExternalSecretReads(t *testing.T) {
	r := runRotate(t, baoSession, "--env", "vault-local", "--path", "api/secrets/typo/typoed", "--value", "v")
	expectExit(t, r, 1)
	if !typoRefusal.MatchString(r.stderr) {
		t.Errorf("stderr does not refuse the unread path by name\n%s", r)
	}
}

func TestRotateSecretDryRunNamesFourStepsAndCallsNothing(t *testing.T) {
	dir := t.TempDir()
	baoLog, kubectlLog := filepath.Join(dir, "bao.log"), filepath.Join(dir, "kubectl.log")
	r := runRotate(t, []string{"FAKE_BAO_LOG=" + baoLog, "FAKE_KUBECTL_LOG=" + kubectlLog},
		"--env", "vault-local", "--path", "api/secrets/gitea/token", "--dry-run")

	expectExit(t, r, 0, "STEP 1/4", "STEP 2/4", "STEP 3/4", "STEP 4/4", "DRY-RUN COMPLETE")
	if c := calls(t, baoLog); len(c) != 0 {
		t.Errorf("a dry run called bao: %v", c)
	}
	if c := calls(t, kubectlLog); len(c) != 0 {
		t.Errorf("a dry run called kubectl: %v", c)
	}
}

// gitea/token is read by the api and by the tenant operator; rotating it and
// restarting one of the two leaves the other on a revoked token.
func TestRotateSecretRestartsBothBinariesThatReadTheGiteaToken(t *testing.T) {
	r := runRotate(t, nil, "--env", "vault-local", "--path", "api/secrets/gitea/token", "--dry-run")
	expectExit(t, r, 0, "binaries to restart: loop-runtime-api,loop-runtime-tenant-operator")
}

func TestRotateSecretRestartsOnlyTheOperatorForTheDigitalOceanToken(t *testing.T) {
	r := runRotate(t, nil, "--env", "vault-local", "--path", "operators/secrets/digitalocean/token", "--dry-run")
	expectExit(t, r, 0)
	if !lineEndsWith(r.stderr, "binaries to restart: loop-runtime-tenant-operator") {
		t.Errorf("want exactly loop-runtime-tenant-operator restarted\n%s", r)
	}
}

func TestRotateSecretRestartsNothingWhenTheWriteFails(t *testing.T) {
	kubectlLog := filepath.Join(t.TempDir(), "kubectl.log")
	r := runRotate(t, append([]string{"FAKE_BAO_EXIT=1", "FAKE_KUBECTL_LOG=" + kubectlLog}, baoSession...),
		"--env", "vault-local", "--path", "api/secrets/gitea/token", "--value", "v")

	expectExit(t, r, 2, "rotation aborted")
	if n := countCalls(t, kubectlLog, "rollout restart"); n != 0 {
		t.Errorf("a failed write still restarted %d Deployment(s): %v", n, calls(t, kubectlLog))
	}
}

func TestRotateSecretReportsAPartialRotationWhenARolloutFails(t *testing.T) {
	r := runRotate(t, append([]string{"FAKE_STATUS_EXIT=1"}, baoSession...),
		"--env", "vault-local", "--path", "api/secrets/gitea/token", "--value", "v", "--skip-verify")
	expectExit(t, r, 2, "PARTIAL")
}

func TestRotateSecretDryRunNeverPrintsTheValue(t *testing.T) {
	r := runRotate(t, nil,
		"--env", "vault-local", "--path", "api/secrets/gitea/token", "--value", rotateProbe, "--dry-run")
	expectExit(t, r, 0)
	refuseLeak(t, r, rotateProbe)
}

// lineEndsWith reports whether some line of text ends with suffix — the Go form of
// `grep -q "suffix$"`.
func lineEndsWith(text, suffix string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasSuffix(line, suffix) {
			return true
		}
	}
	return false
}
