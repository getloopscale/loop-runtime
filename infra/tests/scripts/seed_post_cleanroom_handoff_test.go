// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func scriptFunction(t *testing.T, name string) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)\n` + name + `\(\) \{.*?\n\}`).FindString(readScript(t, "seed-post-cleanroom-all.sh"))
	if m == "" {
		t.Fatalf("%s function not found in seed-post-cleanroom-all.sh", name)
	}
	return m
}

func runBash(t *testing.T, env []string, script string) string {
	t.Helper()
	cmd := exec.Command("bash", "-c", "set -uo pipefail\nlog() { :; }\n"+script)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash failed: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// The fixture seed needs the tenant the admin seed just provisioned. Each seed is its own
// process, so on the 2026-09-29 cleanroom the id never reached it and the step skipped
// (#453). The admin seed reports the tenant in its result; the chain hands it on.
func TestTheChainHandsTheAdminTenantToLaterSeeds(t *testing.T) {
	logf := filepath.Join(t.TempDir(), "seed-3.log")
	line := "seed admin-loop-runtime-dev: PASS — identity 7c9e (tenant tn-admin) + session cookie persisted (428 bytes) to /x\n"
	if err := os.WriteFile(logf, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	got := runBash(t, []string{"PATH=" + os.Getenv("PATH")},
		scriptFunction(t, "export_admin_tenant_id")+"\nexport_admin_tenant_id "+logf+"\nprintf '%s' \"${LOOP_RUNTIME_TENANT_ID:-}\"")
	if got != "tn-admin" {
		t.Errorf("LOOP_RUNTIME_TENANT_ID = %q, want tn-admin", got)
	}
}

func TestTheChainKeepsATenantTheOperatorSet(t *testing.T) {
	logf := filepath.Join(t.TempDir(), "seed-3.log")
	_ = os.WriteFile(logf, []byte("seed admin-loop-runtime-dev: PASS — identity x (tenant tn-admin)\n"), 0o600)
	got := runBash(t, []string{"PATH=" + os.Getenv("PATH"), "LOOP_RUNTIME_TENANT_ID=tn-operator"},
		scriptFunction(t, "export_admin_tenant_id")+"\nexport_admin_tenant_id "+logf+"\nprintf '%s' \"$LOOP_RUNTIME_TENANT_ID\"")
	if got != "tn-operator" {
		t.Errorf("LOOP_RUNTIME_TENANT_ID = %q, want the operator's tn-operator", got)
	}
}

// The fixture step creates the project through the CLI, and a cold droplet has none: the
// earlier rounds used one installed by hand (#453). The chain installs the published CLI —
// what a user gets — into a cache and points LOOP_RUNTIME_BIN at it.
func TestTheChainInstallsThePublishedCLIWhenNoneIsPresent(t *testing.T) {
	bin := t.TempDir()
	record := filepath.Join(t.TempDir(), "npm.args")
	fakeNpm := `#!/usr/bin/env bash
printf '%s\n' "$*" > "` + record + `"
prefix=""; while [ $# -gt 0 ]; do [ "$1" = "--prefix" ] && prefix="$2"; shift; done
mkdir -p "$prefix/node_modules/.bin" && printf '#!/bin/sh\n' > "$prefix/node_modules/.bin/loop-runtime" && chmod +x "$prefix/node_modules/.bin/loop-runtime"`
	if err := os.WriteFile(filepath.Join(bin, "npm"), []byte(fakeNpm), 0o755); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	got := runBash(t, []string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + home},
		scriptFunction(t, "ensure_loop_runtime_cli")+"\nensure_loop_runtime_cli\nprintf '%s' \"${LOOP_RUNTIME_BIN:-}\"")
	args, _ := os.ReadFile(record)
	if !strings.Contains(string(args), "@loop-runtime/cli@") {
		t.Errorf("npm was not asked for the published CLI: %q", args)
	}
	if !strings.HasPrefix(got, home) || !strings.HasSuffix(got, "/node_modules/.bin/loop-runtime") {
		t.Errorf("LOOP_RUNTIME_BIN = %q, want the installed CLI under HOME", got)
	}
}

func TestTheChainUsesTheCLIAlreadyOnPath(t *testing.T) {
	bin := t.TempDir()
	_ = os.WriteFile(filepath.Join(bin, "loop-runtime"), []byte("#!/bin/sh\n"), 0o755)
	_ = os.WriteFile(filepath.Join(bin, "npm"), []byte("#!/bin/sh\necho npm-called >&2; exit 1\n"), 0o755)
	got := runBash(t, []string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + t.TempDir()},
		scriptFunction(t, "ensure_loop_runtime_cli")+"\nensure_loop_runtime_cli\nprintf '%s' \"${LOOP_RUNTIME_BIN:-}\"")
	if got != filepath.Join(bin, "loop-runtime") {
		t.Errorf("LOOP_RUNTIME_BIN = %q, want the CLI already on PATH", got)
	}
}
