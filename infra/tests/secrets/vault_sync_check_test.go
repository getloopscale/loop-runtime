// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package secrets

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// vault-sync-check.sh (Phase 9 T9.1 of plan 2026-06-02-loopzero-api-vault-integration)
// reads the ExternalSecret inventory with yq and asks the cluster whether each
// target is Ready. It replaced the loopzero_config_secret_source metric, so a verdict it
// gets wrong is a sync failure nobody sees.

const vaultSyncScript = openbaoScripts + "/vault-sync-check.sh"

// syncInventory is a three-target inventory: two for one binary, one for another.
const syncInventory = `envName: ""
externalSecrets:
  - targetSecret: target-a
    vaultBinary: api
    vaultCategory: secrets
    data:
      - secretKey: keya
        vaultKey: a/keya
  - targetSecret: target-b
    vaultBinary: api
    vaultCategory: secrets
    data:
      - secretKey: keyb
        vaultKey: b/keyb
  - targetSecret: target-c
    vaultBinary: cloud
    vaultCategory: secrets
    data:
      - secretKey: keyc
        vaultKey: c/keyc
`

func init() {
	registerFakes("vault-sync", map[string]fakeTool{"kubectl": syncKubectl})
}

// syncKubectl answers `get externalsecret <name>` from FAKE_STATUS_<NAME>: an unset
// variable is a CR that does not exist, and FAKE_REASON_<NAME> is its Ready reason.
func syncKubectl(args []string) int {
	if argsMatch(args, "version --request-timeout") {
		return 0
	}
	name := argAfter(args, "externalsecret")
	if name == "" {
		return 0
	}
	suffix := strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	status := os.Getenv("FAKE_STATUS_" + suffix)
	if status == "" {
		fmt.Fprintf(os.Stderr, "Error from server (NotFound): externalsecrets.external-secrets.io %q not found\n", name)
		return 1
	}
	if !argsMatch(args, "-o jsonpath") {
		return 0 // the existence probe
	}
	if argsMatch(args, ".reason") {
		reason := os.Getenv("FAKE_REASON_" + suffix)
		if reason == "" {
			reason = "SecretSynced"
		}
		fmt.Println(reason)
		return 0
	}
	fmt.Println(status)
	return 0
}

// argAfter returns the argument following the first one equal to word.
func argAfter(args []string, word string) string {
	for i, a := range args {
		if a == word && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func runSyncCheck(t *testing.T, inventory string, env []string, args ...string) scriptRun {
	t.Helper()
	requireTools(t, "yq")
	inv := writeFixture(t, t.TempDir(), "inventory.yaml", inventory)
	return runScript(t, vaultSyncScript, "vault-sync", env,
		append([]string{"--inventory", inv, "--context", "k3d-mock"}, args...)...)
}

func readyStatuses(a, b, c string) []string {
	var env []string
	for name, s := range map[string]string{"A": a, "B": b, "C": c} {
		if s != "" {
			env = append(env, "FAKE_STATUS_TARGET_"+name+"="+s)
		}
	}
	return env
}

func TestVaultSyncCheckRejectsANonIntegerThreshold(t *testing.T) {
	r := runSyncCheck(t, syncInventory, nil, "--threshold", "abc")
	expectExit(t, r, 1, "threshold must be")
}

func TestVaultSyncCheckRejectsAMissingInventory(t *testing.T) {
	requireTools(t, "yq")
	r := runScript(t, vaultSyncScript, "vault-sync", nil,
		"--inventory", t.TempDir()+"/nonexistent.yaml", "--context", "k3d-mock")
	expectExit(t, r, 1, "inventory not found")
}

func TestVaultSyncCheckRejectsAnInventoryWithNoTargets(t *testing.T) {
	r := runSyncCheck(t, "envName: \"\"\nexternalSecrets: []\n", nil, "--threshold", "1")
	expectExit(t, r, 1, "0 ExternalSecret targets")
}

func TestVaultSyncCheckPassesWhenEveryTargetIsReady(t *testing.T) {
	r := runSyncCheck(t, syncInventory, readyStatuses("True", "True", "True"),
		"--namespace", "loopzero-system", "--threshold", "3")
	expectExit(t, r, 0, "Verdict: PASS")
}

func TestVaultSyncCheckPassesWhenTheThresholdIsMetDespiteANotReadyTarget(t *testing.T) {
	r := runSyncCheck(t, syncInventory, readyStatuses("True", "True", "False"),
		"--namespace", "loopzero-system", "--threshold", "2")
	expectExit(t, r, 0, "Verdict: PASS")
}

func TestVaultSyncCheckFailsWhenFewerTargetsThanTheThresholdAreReady(t *testing.T) {
	r := runSyncCheck(t, syncInventory, readyStatuses("True", "True", "False"),
		"--namespace", "loopzero-system", "--threshold", "3")
	expectExit(t, r, 2, "Verdict: FAIL")
}

// A CR that does not exist is not a CR that is not Ready: the report counts it
// under its own name, so an operator looks for a missing ExternalSecret and not
// for a sync error.
func TestVaultSyncCheckCountsAMissingCRAsMissingNotAsUnsynced(t *testing.T) {
	r := runSyncCheck(t, syncInventory, readyStatuses("True", "True", ""), "--threshold", "3")
	expectExit(t, r, 2)
	for _, want := range []string{"MISSING: target-c", "Missing: 1"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("stdout does not contain %q\n%s", want, r)
		}
	}
}

func TestVaultSyncCheckJSONCarriesTheVerdictFields(t *testing.T) {
	r := runSyncCheck(t, syncInventory, readyStatuses("True", "True", "True"), "--threshold", "3", "--json")
	expectExit(t, r, 0)

	var report map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &report); err != nil {
		t.Fatalf("--json output is not JSON: %v\n%s", err, r)
	}
	for _, key := range []string{"context", "expected", "synced", "threshold", "pass"} {
		if _, ok := report[key]; !ok {
			t.Errorf("--json output has no %q field: %v", key, report)
		}
	}
	if report["synced"] != float64(3) {
		t.Errorf("synced = %v, want 3", report["synced"])
	}
	if report["pass"] != true {
		t.Errorf("pass = %v, want true", report["pass"])
	}
}
