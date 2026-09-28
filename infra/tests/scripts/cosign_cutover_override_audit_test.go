// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scripts

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The emergency override bypasses the cosign cutover gate. Its audit_log row
// is the only trace of that bypass, so the override is granted only when the
// row was written.

// psqlStub installs a fake psql that records its arguments and stdin into
// dir/psql.args and dir/psql.stdin, and exits with exitCode.
func psqlStub(t *testing.T, dir string, exitCode int) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > " + shellQuote(filepath.Join(dir, "psql.args")) + "\n" +
		"cat > " + shellQuote(filepath.Join(dir, "psql.stdin")) + "\n" +
		"exit " + string(rune('0'+exitCode)) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "psql"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func runOverride(t *testing.T, dir, reason string) (int, string) {
	t.Helper()
	cmd := exec.Command("bash", securityScriptPath(t, "check-cosign-cutover-deadline.sh"),
		"--emergency-override="+reason)
	cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+"/usr/bin:/bin", "USER=oncall")
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0, string(out)
	case errors.As(err, &exitErr):
		return exitErr.ExitCode(), string(out)
	default:
		t.Fatalf("run: %v", err)
		return -1, ""
	}
}

func TestEmergencyOverride_IsGrantedOnceTheAuditRowIsWritten(t *testing.T) {
	dir := t.TempDir()
	psqlStub(t, dir, 0)

	code, out := runOverride(t, dir, "rekor outage")

	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "psql.args")); err != nil {
		t.Fatal("psql was never called: the override left no audit row")
	}
}

func TestEmergencyOverride_IsRefusedWhenTheAuditRowCannotBeWritten(t *testing.T) {
	dir := t.TempDir()
	psqlStub(t, dir, 3)

	code, out := runOverride(t, dir, "rekor outage")

	if code != 2 {
		t.Fatalf("exit = %d, want 2 (override refused)\n%s", code, out)
	}
}

func TestEmergencyOverride_IsRefusedWithoutPsql(t *testing.T) {
	dir := t.TempDir() // no psql on PATH

	code, out := runOverride(t, dir, "rekor outage")

	if code != 2 {
		t.Fatalf("exit = %d, want 2 (override refused)\n%s", code, out)
	}
}

// The reason is operator text. It reaches psql as a variable, never spliced
// into the statement, so a quote in it can neither break nor extend the SQL.
func TestEmergencyOverride_PassesTheReasonAsAPsqlVariable(t *testing.T) {
	dir := t.TempDir()
	psqlStub(t, dir, 0)
	reason := `it's down'); DROP TABLE audit_log; --`

	if code, out := runOverride(t, dir, reason); code != 0 {
		t.Fatalf("exit = %d\n%s", code, out)
	}

	args, err := os.ReadFile(filepath.Join(dir, "psql.args"))
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := os.ReadFile(filepath.Join(dir, "psql.stdin"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "reason="+reason) {
		t.Errorf("reason not passed as a psql variable; args:\n%s", args)
	}
	if strings.Contains(string(stdin), "DROP TABLE") || strings.Contains(string(stdin), "it's") {
		t.Errorf("reason was spliced into the SQL:\n%s", stdin)
	}
	if !strings.Contains(string(stdin), ":'reason'") {
		t.Errorf("SQL does not read the reason through psql quoting:\n%s", stdin)
	}
}
