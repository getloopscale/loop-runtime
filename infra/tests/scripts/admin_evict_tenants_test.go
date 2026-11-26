// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scripts

import (
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// admin-evict-tenants.sh deletes tenants from the database. These tests run it
// against stub kubectl/pkill/sleep that record what they were asked to do, so
// no cluster is touched.

// evictPassword is a fixture marker, not a credential: the tests look for it
// in what the script hands to kubectl. The URL is assembled so no connection
// string literal sits in the source.
const evictPassword = "fixture-password-marker"

var evictDatabaseURL = "postgres" + "://loop_runtime:" + evictPassword + "@db.internal:5432/loop_runtime?sslmode=disable"

// evictStubs writes kubectl, pkill and sleep stubs into a fresh dir. kubectl
// serves an empty list for every get, the api Secret for "get secret", and
// records the arguments and stdin of "run" into dir/run.args and dir/run.stdin
// before exiting with runExit.
func evictStubs(t *testing.T, runExit int) string {
	t.Helper()
	dir := t.TempDir()
	secret := `{"data":{"DATABASE_URL":"` + base64.StdEncoding.EncodeToString([]byte(evictDatabaseURL)) + `"}}`
	kubectl := "#!/bin/sh\n" +
		"case \"$*\" in\n" +
		"  *' get secret '*) printf '%s' " + shellQuote(secret) + " ;;\n" +
		"  *' run '*) printf '%s\\n' \"$@\" > " + shellQuote(filepath.Join(dir, "run.args")) +
		"; cat > " + shellQuote(filepath.Join(dir, "run.stdin")) + "; exit " + string(rune('0'+runExit)) + " ;;\n" +
		"esac\n" +
		"exit 0\n"
	for name, body := range map[string]string{
		"kubectl": kubectl,
		"pkill":   "#!/bin/sh\nexit 1\n",
		"sleep":   "#!/bin/sh\nexit 0\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func runEvict(t *testing.T, stubDir string, args ...string) (int, string) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", "..", "tools", "admin-evict-tenants.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Env = append(os.Environ(), "PATH="+stubDir+string(os.PathListSeparator)+"/usr/bin:/bin")
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

func TestEvictTenants_DeletesThroughAParameterizedTransaction(t *testing.T) {
	dir := evictStubs(t, 0)

	code, out := runEvict(t, dir, "--pattern", "tn-lt-%")

	if code != 0 || !strings.Contains(out, "=== DONE ===") {
		t.Fatalf("exit = %d, want 0 and DONE\n%s", code, out)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "run.args"))
	stdin, _ := os.ReadFile(filepath.Join(dir, "run.stdin"))
	if !strings.Contains(string(stdin), "LIKE :'pattern'") {
		t.Errorf("the SQL must read the pattern as a psql variable:\n%s", stdin)
	}
	if strings.Contains(string(stdin), "tn-lt-") {
		t.Errorf("the pattern was spliced into the SQL:\n%s", stdin)
	}
	if !strings.Contains(string(args), "pattern=tn-lt-%") {
		t.Errorf("the pattern was not passed to psql as a variable:\n%s", args)
	}
}

// The password must not be written into the pod spec, where
// `kubectl get pod -o yaml` and the API server audit log both show it.
func TestEvictTenants_KeepsTheDatabasePasswordOutOfThePodSpec(t *testing.T) {
	dir := evictStubs(t, 0)

	if code, out := runEvict(t, dir, "--pattern", "tn-lt-%"); code != 0 {
		t.Fatalf("exit = %d\n%s", code, out)
	}
	args, err := os.ReadFile(filepath.Join(dir, "run.args"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(args), evictPassword) {
		t.Fatalf("the database password is in the pod spec:\n%s", args)
	}
	if !strings.Contains(string(args), "secretKeyRef") {
		t.Fatalf("the pod must read DATABASE_URL from its Secret:\n%s", args)
	}
}

// A failed database transaction used to be piped through `tail`, so the run
// still printed DONE and exited 0.
func TestEvictTenants_FailsWhenTheDatabaseStepFails(t *testing.T) {
	dir := evictStubs(t, 3)

	code, out := runEvict(t, dir, "--pattern", "tn-lt-%")

	if code == 0 || strings.Contains(out, "=== DONE ===") {
		t.Fatalf("exit = %d, want a failure without DONE\n%s", code, out)
	}
}

func TestEvictTenants_RefusesAPatternThatIsNotATenantPrefix(t *testing.T) {
	for _, pattern := range []string{"tn-lt-'; DROP TABLE tenants; --%", "tn-lt-", "prod-%", "tn_%"} {
		t.Run(pattern, func(t *testing.T) {
			dir := evictStubs(t, 0)

			code, _ := runEvict(t, dir, "--pattern", pattern)

			if code != 1 {
				t.Fatalf("exit = %d, want 1 (usage)", code)
			}
			if _, err := os.Stat(filepath.Join(dir, "run.args")); err == nil {
				t.Fatal("the database step ran for a refused pattern")
			}
		})
	}
}

func TestEvictTenants_AllTenantsNeedsExplicitConfirmation(t *testing.T) {
	dir := evictStubs(t, 0)

	if code, _ := runEvict(t, dir, "--pattern", "%"); code != 1 {
		t.Fatalf("exit = %d, want 1 without --yes", code)
	}
}
