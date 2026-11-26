// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package production holds hermetic tests for the operator scripts in this
// directory. The scripts are executed against stubbed `kubectl` and `psql`
// binaries, so the tests need no cluster and no database.
package production

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// kubectlStub answers the three questions run-migrations.sh asks on the
// fallback path: the loopruntime-api pod (absent), the postgresql pod (present), and
// `exec`, which it forwards to whatever follows `--` so the psql stub below
// serves it.
const kubectlStub = `#!/usr/bin/env bash
for a in "$@"; do
  case "$a" in
    app=loopruntime-api) exit 0 ;;
    app.kubernetes.io/name=postgresql) printf 'postgresql-0'; exit 0 ;;
  esac
done
for a in "$@"; do
  if [[ "$a" == exec ]]; then
    while [[ $# -gt 0 && "$1" != "--" ]]; do shift; done
    shift
    exec "$@"
  fi
done
echo "kubectl stub: unexpected call: $*" >&2
exit 1
`

// psqlStub simulates the single-row schema_migrations table golang-migrate
// keeps, persisted in $STATE_DIR/version and $STATE_DIR/dirty. A migration
// read from stdin whose body contains FAIL_THIS_MIGRATION fails the way psql
// does under ON_ERROR_STOP=1 (exit 3); without ON_ERROR_STOP psql exits 0 on
// a failed statement, which is exactly the behaviour that hid failures before.
const psqlStub = `#!/usr/bin/env bash
sql=""
on_error_stop=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    -c) sql="$2"; shift ;;
    ON_ERROR_STOP=1) on_error_stop=1 ;;
  esac
  shift
done
if [[ -z "$sql" ]]; then
  body="$(cat)"
  if [[ "$body" == *FAIL_THIS_MIGRATION* ]]; then
    echo 'ERROR:  syntax error at or near "FAIL_THIS_MIGRATION"' >&2
    [[ "$on_error_stop" == 1 ]] && exit 3
    exit 0
  fi
  echo "$body" | head -1 >> "$STATE_DIR/applied"
  exit 0
fi
if [[ "$sql" =~ VALUES\ \(([0-9]+),\ (true|false)\) ]]; then
  echo "${BASH_REMATCH[1]}" > "$STATE_DIR/version"
  echo "${BASH_REMATCH[2]}" > "$STATE_DIR/dirty"
fi
if [[ "$sql" == *SELECT* ]]; then
  if [[ -f "$STATE_DIR/version" ]]; then
    d=f; [[ "$(cat "$STATE_DIR/dirty")" == true ]] && d=t
    printf '%s|%s\n' "$(cat "$STATE_DIR/version")" "$d"
  else
    printf '|\n'
  fi
fi
exit 0
`

type migrationRun struct {
	exitCode int
	output   string
	stateDir string
}

func (r migrationRun) state(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.stateDir, name))
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatalf("read state %s: %v", name, err)
	}
	return strings.TrimSpace(string(b))
}

// writeMigrations materialises a migrations directory. Each body's first line
// is recorded by the psql stub when the file is applied, so it identifies it.
func writeMigrations(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

// runFallback executes run-migrations.sh down its psql fallback (loopruntime-api pod
// absent) against the given migrations dir and initial schema_migrations row.
func runFallback(t *testing.T, migrationsDir, initialVersion, initialDirty string) migrationRun {
	t.Helper()
	binDir := t.TempDir()
	stateDir := t.TempDir()
	for name, body := range map[string]string{"kubectl": kubectlStub, "psql": psqlStub} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(body), 0o700); err != nil {
			t.Fatalf("write %s stub: %v", name, err)
		}
	}
	if initialVersion != "" {
		for name, v := range map[string]string{"version": initialVersion, "dirty": initialDirty} {
			if err := os.WriteFile(filepath.Join(stateDir, name), []byte(v+"\n"), 0o600); err != nil {
				t.Fatalf("seed state: %v", err)
			}
		}
	}
	script, err := filepath.Abs("run-migrations.sh")
	if err != nil {
		t.Fatalf("resolve script: %v", err)
	}

	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+"/usr/bin:/bin",
		"PG_PASS=stub-password",
		"MIGRATIONS_DIR="+migrationsDir,
		"STATE_DIR="+stateDir,
		"LOOPRUNTIME_MIGRATIONS_REQUIRE_API=true",
	)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run %s: %v (output: %s)", script, err, out)
		}
		code = exitErr.ExitCode()
	}
	return migrationRun{exitCode: code, output: string(out), stateDir: stateDir}
}

func TestFallbackStopsAtFailedMigrationAndLeavesItDirty(t *testing.T) {
	dir := writeMigrations(t, map[string]string{
		"000001_first.up.sql":  "-- m1\nCREATE TABLE a (id int);\n",
		"000002_broken.up.sql": "-- m2\nFAIL_THIS_MIGRATION;\n",
		"000003_third.up.sql":  "-- m3\nCREATE TABLE c (id int);\n",
	})

	run := runFallback(t, dir, "", "")

	if run.exitCode == 0 {
		t.Fatalf("a failed migration must fail the run, got exit 0\noutput:\n%s", run.output)
	}
	if got := run.state(t, "version"); got != "2" {
		t.Errorf("schema_migrations.version = %q, want 2 (the failed file)\noutput:\n%s", got, run.output)
	}
	if got := run.state(t, "dirty"); got != "true" {
		t.Errorf("schema_migrations.dirty = %q, want true — a failed schema must never be stamped clean\noutput:\n%s", got, run.output)
	}
	if applied := run.state(t, "applied"); strings.Contains(applied, "m3") {
		t.Errorf("migration 3 was applied after migration 2 failed; applied:\n%s", applied)
	}
	if !strings.Contains(run.output, "000002_broken.up.sql") {
		t.Errorf("output must name the failed migration, got:\n%s", run.output)
	}
}

func TestFallbackStampsHighestVersionCleanWhenEveryMigrationSucceeds(t *testing.T) {
	dir := writeMigrations(t, map[string]string{
		"000001_first.up.sql":  "-- m1\n",
		"000002_second.up.sql": "-- m2\n",
	})

	run := runFallback(t, dir, "", "")

	if run.exitCode != 0 {
		t.Fatalf("expected exit 0, got %d\noutput:\n%s", run.exitCode, run.output)
	}
	if got, dirty := run.state(t, "version"), run.state(t, "dirty"); got != "2" || dirty != "false" {
		t.Errorf("schema_migrations = (%q, %q), want (2, false)", got, dirty)
	}
}

func TestFallbackAppliesOnlyMigrationsNewerThanTheRecordedVersion(t *testing.T) {
	dir := writeMigrations(t, map[string]string{
		"000001_first.up.sql":  "-- m1\n",
		"000002_second.up.sql": "-- m2\n",
		"000003_third.up.sql":  "-- m3\n",
	})

	run := runFallback(t, dir, "2", "false")

	if run.exitCode != 0 {
		t.Fatalf("expected exit 0, got %d\noutput:\n%s", run.exitCode, run.output)
	}
	if applied := run.state(t, "applied"); applied != "-- m3" {
		t.Errorf("applied = %q, want only migration 3", applied)
	}
}

func TestFallbackRefusesADirtyDatabase(t *testing.T) {
	dir := writeMigrations(t, map[string]string{"000001_first.up.sql": "-- m1\n"})

	run := runFallback(t, dir, "1", "true")

	if run.exitCode == 0 {
		t.Fatalf("a dirty schema_migrations row must refuse the run, got exit 0\noutput:\n%s", run.output)
	}
	if applied := run.state(t, "applied"); applied != "" {
		t.Errorf("nothing may be applied on a dirty database, applied:\n%s", applied)
	}
	if got := run.state(t, "dirty"); got != "true" {
		t.Errorf("the dirty flag must be left for an operator to resolve, got %q", got)
	}
}
