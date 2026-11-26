// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scripts

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The drill teardown validator and the cost watchdog are the safety net that
// stops a forgotten drill environment from billing for days. A doctl call that
// fails (expired token, network) returned an empty list, and an empty list
// reads as "nothing left": the net reported CLEAN without having looked.

// doctlStub writes a doctl that answers a cluster list with clusterList (or
// fails with exit 1 when listFails), serves an expired drill tag for
// `cluster get`, answers empty database and VPC lists, and accepts deletes.
func doctlStub(t *testing.T, listFails bool, clusterList string) string {
	t.Helper()
	dir := t.TempDir()
	list := "printf '%s\\n' " + shellQuote(clusterList)
	if listFails {
		list = "echo 'Error: Unable to authenticate you' >&2; exit 1"
	}
	body := "#!/bin/sh\n" +
		"case \"$*\" in\n" +
		"  'kubernetes cluster list'*) " + list + " ;;\n" +
		"  'kubernetes cluster get'*) echo 'loopruntime-drill loopruntime-drill-expires-at-2020-01-01T00-00-00' ;;\n" +
		"  'kubernetes cluster delete'*) exit 0 ;;\n" +
		"  *list*) exit 0 ;;\n" +
		"esac\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "doctl"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runDrillScript(t *testing.T, stubDir, name string) (int, string) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "drill", name))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(),
		"PATH="+stubDir+string(os.PathListSeparator)+"/usr/bin:/bin",
		"DIGITALOCEAN_TOKEN=fixture-token",
		"DRILL_TEARDOWN_LOCK_FILE="+filepath.Join(stubDir, "teardown.lock"),
		"DRILL_AUDIT_DIR="+stubDir,
		"PULUMI_ACCESS_TOKEN=",
	)
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0, string(out)
	case errors.As(err, &exitErr):
		return exitErr.ExitCode(), string(out)
	default:
		t.Fatalf("run %s: %v", name, err)
		return -1, ""
	}
}

func TestTeardownValidator_FailsWhenDoctlCannotList(t *testing.T) {
	code, out := runDrillScript(t, doctlStub(t, true, ""), "teardown-validator.sh")

	if code != 4 {
		t.Fatalf("exit = %d, want 4 (could not list)\n%s", code, out)
	}
	if strings.Contains(out, "fully torn down") {
		t.Fatalf("reported a clean teardown it could not check:\n%s", out)
	}
}

func TestTeardownValidator_PassesWhenNothingIsLeft(t *testing.T) {
	if code, out := runDrillScript(t, doctlStub(t, false, ""), "teardown-validator.sh"); code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
}

func TestWatchdog_FailsWhenDoctlCannotList(t *testing.T) {
	code, out := runDrillScript(t, doctlStub(t, true, ""), "watchdog.sh")

	if code != 4 {
		t.Fatalf("exit = %d, want 4 (could not list)\n%s", code, out)
	}
	if strings.Contains(out, "No drill clusters found") {
		t.Fatalf("reported no clusters without having listed them:\n%s", out)
	}
}

// The watchdog called drill-teardown-validator.sh next to itself, a path that
// stopped existing when the drill scripts moved into drill/, so every expired
// stack ended in "validator not found" and nothing was cleaned.
func TestWatchdog_CleansAnExpiredStackThroughTheValidator(t *testing.T) {
	stub := doctlStub(t, false, "c1 loopruntime-drill-mgmt loopruntime-drill")

	code, out := runDrillScript(t, stub, "watchdog.sh")

	if code != 0 || !strings.Contains(out, "validated clean") {
		t.Fatalf("exit = %d, want 0 after the validator ran\n%s", code, out)
	}
}
