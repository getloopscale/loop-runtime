// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Hermetic unit tests for the e2e facade: the workload namespace a journey
// inspects, and the one place a `loopruntime` process is spawned. No cluster needed —
// the CLI is replaced by a stub on PATH that reports what it received.

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/infra/scripts/seed"
)

// A journey that builds the two-segment "loopruntime-<tenant>-<env>" name inspects a
// namespace no deploy ever creates, and an absence check there passes vacuously.
func TestE2ENamespaceIsTheTenantProjectEnvTriple(t *testing.T) {
	ns, err := e2eNamespace("shop", "production")

	if err != nil {
		t.Fatalf("e2eNamespace: %v", err)
	}
	if want := "loopruntime-" + seed.TenantID + "-shop-production"; ns != want {
		t.Fatalf("e2eNamespace = %q, want %q", ns, want)
	}
}

func TestE2ENamespaceRejectsAnEmptyProject(t *testing.T) {
	if _, err := e2eNamespace("", "production"); err == nil {
		t.Fatal("e2eNamespace accepted an empty project")
	}
}

// installLoopRuntimeStub puts a `loopruntime` on PATH that prints the env vars the facade
// controls and exits with the code in LOOPRUNTIME_STUB_EXIT.
func installLoopRuntimeStub(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	stub := "#!/bin/sh\n" +
		"echo \"api=$LOOPRUNTIME_API_URL token=[${LOOPRUNTIME_TOKEN-unset}] auth=[${LOOPRUNTIME_SERVICE_AUTH-unset}] timeout=[${LOOPRUNTIME_API_TIMEOUT-unset}] pwd=$(pwd) args=$*\"\n" +
		"echo stderr-line >&2\n" +
		"exit ${LOOPRUNTIME_STUB_EXIT:-0}\n"
	if err := os.WriteFile(filepath.Join(dir, "loopruntime"), []byte(stub), 0o755); err != nil {
		t.Fatalf("write loopruntime stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LOOPRUNTIME_API_URL", "http://api.test")
}

func TestExecLoopRuntimeStripsTheInheritedTokenByDefault(t *testing.T) {
	installLoopRuntimeStub(t)
	t.Setenv("LOOPRUNTIME_TOKEN", "parent-token")

	stdout, _, code := execLoopRuntime(loopruntimeInvocation{}, "whoami")

	if code != 0 || !strings.Contains(stdout, "token=[]") || !strings.Contains(stdout, "api=http://api.test") {
		t.Fatalf("exit=%d stdout=%q, want exit 0, an empty LOOPRUNTIME_TOKEN and the suite API URL", code, stdout)
	}
}

func TestExecLoopRuntimeLetsAnExplicitEnvEntryOverrideTheDefaults(t *testing.T) {
	installLoopRuntimeStub(t)

	stdout, _, _ := execLoopRuntime(loopruntimeInvocation{env: []string{"LOOPRUNTIME_TOKEN=garbage"}}, "whoami")

	if !strings.Contains(stdout, "token=[garbage]") {
		t.Fatalf("stdout=%q, want the explicit LOOPRUNTIME_TOKEN to win", stdout)
	}
}

func TestExecLoopRuntimeRemovesAnUnsetKeyFromTheInheritedEnvironment(t *testing.T) {
	installLoopRuntimeStub(t)
	t.Setenv("LOOPRUNTIME_SERVICE_AUTH", "inherited")

	stdout, _, _ := execLoopRuntime(loopruntimeInvocation{unset: []string{"LOOPRUNTIME_SERVICE_AUTH"}}, "admin")

	if !strings.Contains(stdout, "auth=[unset]") {
		t.Fatalf("stdout=%q, want LOOPRUNTIME_SERVICE_AUTH absent from the child", stdout)
	}
}

func TestExecLoopRuntimeReportsTheCLIExitCodeAndBothStreams(t *testing.T) {
	installLoopRuntimeStub(t)
	t.Setenv("LOOPRUNTIME_STUB_EXIT", "4")

	stdout, stderr, code := execLoopRuntime(loopruntimeInvocation{}, "whoami", "--json")

	if code != 4 || !strings.Contains(stdout, "args=whoami --json") || !strings.Contains(stderr, "stderr-line") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestExecLoopRuntimeRunsInTheRequestedDirectory(t *testing.T) {
	installLoopRuntimeStub(t)
	dir := t.TempDir()

	stdout, _, _ := execLoopRuntime(loopruntimeInvocation{dir: dir}, "deploy")

	if !strings.Contains(stdout, "pwd="+dir) {
		t.Fatalf("stdout=%q, want pwd=%s", stdout, dir)
	}
}

// A binary that never started is not a CLI that exited 0. The copies this
// facade replaced reported exit 0 in that case, so an "exit 0" assertion passed
// with no `loopruntime` on PATH at all.
func TestExecLoopRuntimeReportsMinusOneWhenTheBinaryCannotStart(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, _, code := execLoopRuntime(loopruntimeInvocation{}, "whoami")

	if code != -1 {
		t.Fatalf("exit=%d, want -1 for a CLI that never started", code)
	}
}
