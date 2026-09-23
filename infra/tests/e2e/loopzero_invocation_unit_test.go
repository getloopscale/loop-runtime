// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Hermetic unit tests for the e2e facade: the workload namespace a journey
// inspects, and the one place a `loopzero` process is spawned. No cluster needed —
// the CLI is replaced by a stub on PATH that reports what it received.

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/infra/scripts/seed"
)

// A journey that builds the two-segment "loopzero-<tenant>-<env>" name inspects a
// namespace no deploy ever creates, and an absence check there passes vacuously.
func TestE2ENamespaceIsTheTenantProjectEnvTriple(t *testing.T) {
	ns, err := e2eNamespace("shop", "production")

	if err != nil {
		t.Fatalf("e2eNamespace: %v", err)
	}
	if want := "loopzero-" + seed.TenantID + "-shop-production"; ns != want {
		t.Fatalf("e2eNamespace = %q, want %q", ns, want)
	}
}

func TestE2ENamespaceRejectsAnEmptyProject(t *testing.T) {
	if _, err := e2eNamespace("", "production"); err == nil {
		t.Fatal("e2eNamespace accepted an empty project")
	}
}

// installLoopZeroStub puts a `loopzero` on PATH that prints the env vars the facade
// controls and exits with the code in LOOPZERO_STUB_EXIT.
func installLoopZeroStub(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	stub := "#!/bin/sh\n" +
		"echo \"api=$LOOPZERO_API_URL token=[${LOOPZERO_TOKEN-unset}] auth=[${LOOPZERO_SERVICE_AUTH-unset}] timeout=[${LOOPZERO_API_TIMEOUT-unset}] pwd=$(pwd) args=$*\"\n" +
		"echo stderr-line >&2\n" +
		"exit ${LOOPZERO_STUB_EXIT:-0}\n"
	if err := os.WriteFile(filepath.Join(dir, "loopzero"), []byte(stub), 0o755); err != nil {
		t.Fatalf("write loopzero stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LOOPZERO_API_URL", "http://api.test")
}

func TestExecLoopZeroStripsTheInheritedTokenByDefault(t *testing.T) {
	installLoopZeroStub(t)
	t.Setenv("LOOPZERO_TOKEN", "parent-token")

	stdout, _, code := execLoopZero(loopzeroInvocation{}, "whoami")

	if code != 0 || !strings.Contains(stdout, "token=[]") || !strings.Contains(stdout, "api=http://api.test") {
		t.Fatalf("exit=%d stdout=%q, want exit 0, an empty LOOPZERO_TOKEN and the suite API URL", code, stdout)
	}
}

func TestExecLoopZeroLetsAnExplicitEnvEntryOverrideTheDefaults(t *testing.T) {
	installLoopZeroStub(t)

	stdout, _, _ := execLoopZero(loopzeroInvocation{env: []string{"LOOPZERO_TOKEN=garbage"}}, "whoami")

	if !strings.Contains(stdout, "token=[garbage]") {
		t.Fatalf("stdout=%q, want the explicit LOOPZERO_TOKEN to win", stdout)
	}
}

func TestExecLoopZeroRemovesAnUnsetKeyFromTheInheritedEnvironment(t *testing.T) {
	installLoopZeroStub(t)
	t.Setenv("LOOPZERO_SERVICE_AUTH", "inherited")

	stdout, _, _ := execLoopZero(loopzeroInvocation{unset: []string{"LOOPZERO_SERVICE_AUTH"}}, "admin")

	if !strings.Contains(stdout, "auth=[unset]") {
		t.Fatalf("stdout=%q, want LOOPZERO_SERVICE_AUTH absent from the child", stdout)
	}
}

func TestExecLoopZeroReportsTheCLIExitCodeAndBothStreams(t *testing.T) {
	installLoopZeroStub(t)
	t.Setenv("LOOPZERO_STUB_EXIT", "4")

	stdout, stderr, code := execLoopZero(loopzeroInvocation{}, "whoami", "--json")

	if code != 4 || !strings.Contains(stdout, "args=whoami --json") || !strings.Contains(stderr, "stderr-line") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestExecLoopZeroRunsInTheRequestedDirectory(t *testing.T) {
	installLoopZeroStub(t)
	dir := t.TempDir()

	stdout, _, _ := execLoopZero(loopzeroInvocation{dir: dir}, "deploy")

	if !strings.Contains(stdout, "pwd="+dir) {
		t.Fatalf("stdout=%q, want pwd=%s", stdout, dir)
	}
}

// A binary that never started is not a CLI that exited 0. The copies this
// facade replaced reported exit 0 in that case, so an "exit 0" assertion passed
// with no `loopzero` on PATH at all.
func TestExecLoopZeroReportsMinusOneWhenTheBinaryCannotStart(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, _, code := execLoopZero(loopzeroInvocation{}, "whoami")

	if code != -1 {
		t.Fatalf("exit=%d, want -1 for a CLI that never started", code)
	}
}
