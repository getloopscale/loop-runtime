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

// securityScriptPath resolves a path under infra/scripts/security/ relative to
// this test file (infra/tests/scripts/).
func securityScriptPath(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "scripts", "security", name))
	if err != nil {
		t.Fatalf("resolve %s: %v", name, err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("%s not found at %s: %v", name, p, err)
	}
	return p
}

// kubectlStub writes a fake `kubectl` into a fresh dir and returns that dir,
// so the script under test can be executed with zero cluster access.
//
// versionTarget is served as .data.target_version of the ConfigMap
// loopruntime-cosign-version-config; versionSetAt as .data.set_at (empty = key
// absent). loopruntime-cosign-config is always served as NotFound, which is its
// measured live state (B-146: the cutover owner deliberately does not
// provision it; issue #348).
func kubectlStub(t *testing.T, versionTarget, versionSetAt string) string {
	t.Helper()
	dir := t.TempDir()
	stub := "#!/usr/bin/env bash\n" +
		"name=\"\"\n" +
		"jp=\"\"\n" +
		"for a in \"$@\"; do\n" +
		"  case \"$a\" in\n" +
		"    loopruntime-cosign-version-config) name=version ;;\n" +
		"    loopruntime-cosign-config) name=cosign ;;\n" +
		"    jsonpath=*) jp=\"${a#jsonpath=}\" ;;\n" +
		"  esac\n" +
		"done\n" +
		"if [[ \"$name\" == cosign ]]; then\n" +
		"  echo 'Error from server (NotFound): configmaps \"loopruntime-cosign-config\" not found' >&2\n" +
		"  exit 1\n" +
		"fi\n" +
		"if [[ \"$name\" == version ]]; then\n" +
		"  case \"$jp\" in\n" +
		"    *target_version*) printf '%s' " + shellQuote(versionTarget) + " ;;\n" +
		"    *set_at*) printf '%s' " + shellQuote(versionSetAt) + " ;;\n" +
		"  esac\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 0\n"
	path := filepath.Join(dir, "kubectl")
	if err := os.WriteFile(path, []byte(stub), 0o700); err != nil {
		t.Fatalf("write kubectl stub: %v", err)
	}
	return dir
}

// shellQuote single-quotes a value for safe embedding in the stub.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// runCutoverGuard executes check-cosign-cutover-deadline.sh against a stubbed
// kubectl and returns its exit code plus combined output.
func runCutoverGuard(t *testing.T, versionTarget, versionSetAt string) (int, string) {
	t.Helper()
	stubDir := kubectlStub(t, versionTarget, versionSetAt)
	script := securityScriptPath(t, "check-cosign-cutover-deadline.sh")

	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(), "PATH="+stubDir+string(os.PathListSeparator)+"/usr/bin:/bin")
	out, err := cmd.CombinedOutput()

	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run %s: %v (output: %s)", script, err, out)
		}
		code = exitErr.ExitCode()
	}
	return code, string(out)
}

// TestCutoverGuard_BaselineVersionIsNotAPendingUpgrade locks the contract that
// the chart, the script header and the script body must agree on.
//
// charts/loopruntime-admission-policies/templates/cosign-version-config.yaml ships
// target_version=v2.4.3 with set_at omitted and documents that state as
// "no upgrade pending"; it declares set_at REQUIRED only when
// targetVersion != v2.4.3. The script header says the same ("set_at REQUIRED
// when target_version=v3").
//
// Measured live 2026-09-05 on k3d-loopruntime-core: the ConfigMap exists exactly in
// that shipped default state (target_version=v2.4.3, no set_at), and the guard
// exited 1 — refusing an "ambiguous deadline" for a version that is the
// baseline, not an upgrade target. The v3 pre-check runs before the keyed/
// keyless logic, so the guard never reached the condition it exists to
// enforce, and the exit-1 it produced named the wrong ConfigMap.
//
// Expected: the baseline state is not a pending upgrade, so the guard falls
// through to the mode read and fails loudly on the ConfigMap that is genuinely
// absent (exit 2), which is the state B-146 deliberately left in place.
func TestCutoverGuard_BaselineVersionIsNotAPendingUpgrade(t *testing.T) {
	code, out := runCutoverGuard(t, "v2.4.3", "")

	if strings.Contains(out, "missing required 'set_at'") {
		t.Fatalf("baseline target_version=v2.4.3 with no set_at was treated as a pending "+
			"upgrade; the chart ships this exact state and documents it as 'no upgrade "+
			"pending'.\nexit=%d\noutput:\n%s", code, out)
	}
	if code != 2 {
		t.Fatalf("expected exit 2 (mode unreadable — loopruntime-cosign-config is absent by the "+
			"B-146 decision), got exit %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "cannot read mode from ConfigMap") {
		t.Fatalf("expected the loud missing-input failure naming loopruntime-cosign-config, "+
			"got:\n%s", out)
	}
}

// TestCutoverGuard_PendingUpgradeWithoutSetAtStillBlocks pins EC-1: a real
// pending upgrade (target_version=v3) with no set_at must still refuse, so the
// fix above cannot be read as removing the clock-reset guard.
func TestCutoverGuard_PendingUpgradeWithoutSetAtStillBlocks(t *testing.T) {
	code, out := runCutoverGuard(t, "v3", "")

	if code != 1 {
		t.Fatalf("expected exit 1 for a pending v3 upgrade with no set_at, got exit %d\n"+
			"output:\n%s", code, out)
	}
	if !strings.Contains(out, "missing required 'set_at'") {
		t.Fatalf("expected the EC-1 ambiguous-deadline refusal, got:\n%s", out)
	}
}
