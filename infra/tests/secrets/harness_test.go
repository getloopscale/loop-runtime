// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package secrets holds the hermetic tests for the OpenBao secret-lifecycle
// scripts: seeding, rotation, sync checks, the ESO bootstrap, snapshot backup and
// restore, and the cold-bootstrap fallbacks of the helmfile helpers.
//
// They were fourteen bash harnesses, each carrying its own copy of the same
// skeleton — colour helpers, PASS/FAIL counters that had already diverged in name,
// an "ALL PASS (n/n)" footer, and a mock-PATH runner re-typed four times. The
// skeleton now exists once, here: `go test` is the counter and the footer, and
// runScript is the runner.
//
// The scripts under test are bash, so running bash is the point of these tests.
// What is NOT bash is everything the tests supply around them: the CLIs a script
// calls (kubectl, bao, aws, helmfile) are this test binary, re-entered under the
// tool's name. A fake is a Go function, not a heredoc.
package secrets

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// openbaoScripts is where the secret-lifecycle scripts live, relative to this package.
const openbaoScripts = "../../scripts/openbao"

// fakeProfileEnv names the set of fakes a re-entered binary answers with. Each
// script under test gets its own profile, because `kubectl get secret` means one
// thing to the ESO bootstrap and nothing at all to the sync check.
const fakeProfileEnv = "LOOPRUNTIME_FAKE_PROFILE"

// fakeTool is a stand-in for one CLI. It receives the arguments the script passed
// and returns the exit code the script sees. It runs in its own process, so it
// talks to the script through stdin/stdout/stderr and the environment, exactly as
// the real tool would.
type fakeTool func(args []string) int

// fakeProfiles maps a profile to the tools it fakes. Each test file registers its
// own profile from an init function.
var fakeProfiles = map[string]map[string]fakeTool{}

func registerFakes(profile string, tools map[string]fakeTool) {
	if _, taken := fakeProfiles[profile]; taken {
		panic("fake profile registered twice: " + profile)
	}
	fakeProfiles[profile] = tools
}

// TestMain lets this binary be re-entered as a fake CLI. When the process was
// started under a faked tool's name, it serves that call and exits before any test
// runs; otherwise it is the ordinary test binary.
func TestMain(m *testing.M) {
	if tools, ok := fakeProfiles[os.Getenv(fakeProfileEnv)]; ok {
		if fake, ok := tools[filepath.Base(os.Args[0])]; ok {
			os.Exit(fake(os.Args[1:]))
		}
	}
	os.Exit(m.Run())
}

// scriptRun is what one script invocation produced.
type scriptRun struct {
	code   int
	stdout string
	stderr string
}

func (r scriptRun) output() string { return r.stdout + r.stderr }

func (r scriptRun) String() string {
	return fmt.Sprintf("exit=%d\n--- stdout ---\n%s--- stderr ---\n%s", r.code, r.stdout, r.stderr)
}

// scrubbedEnv lists the variables a script under test reads from the operator's
// shell. They are removed before every run so the verdict depends on what the test
// passes, not on what the developer happened to export.
var scrubbedEnv = []string{
	"BAO_ADDR", "BAO_TOKEN", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY",
	"AWS_PROFILE", "SKIP_PREFLIGHT", fakeProfileEnv,
}

// runScript runs one bash script with the profile's fakes first on PATH. env adds
// KEY=VALUE pairs on top of a scrubbed copy of the process environment.
func runScript(t *testing.T, script, profile string, env []string, args ...string) scriptRun {
	t.Helper()
	tools, ok := fakeProfiles[profile]
	if !ok {
		t.Fatalf("no fake profile %q is registered", profile)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate the test binary to stand in for the faked tools: %v", err)
	}
	bin := t.TempDir()
	for name := range tools {
		if err := os.Symlink(self, filepath.Join(bin, name)); err != nil {
			t.Fatalf("install the fake %s: %v", name, err)
		}
	}

	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Env = append(scrub(os.Environ()),
		fakeProfileEnv+"="+profile,
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd.Env = append(cmd.Env, env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err = cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return scriptRun{code: 0, stdout: stdout.String(), stderr: stderr.String()}
	case errors.As(err, &exitErr):
		return scriptRun{code: exitErr.ExitCode(), stdout: stdout.String(), stderr: stderr.String()}
	default:
		t.Fatalf("could not start bash %s: %v", script, err)
		return scriptRun{}
	}
}

func scrub(environ []string) []string {
	kept := make([]string, 0, len(environ))
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if !isScrubbed(name) {
			kept = append(kept, kv)
		}
	}
	return kept
}

func isScrubbed(name string) bool {
	if strings.HasPrefix(name, "FAKE_") {
		return true
	}
	for _, s := range scrubbedEnv {
		if name == s {
			return true
		}
	}
	return false
}

// requireTools fails the test when a real CLI the script depends on is absent.
// These scripts parse YAML with yq for real; faking the parser would test the fake.
func requireTools(t *testing.T, names ...string) {
	t.Helper()
	for _, n := range names {
		if _, err := exec.LookPath(n); err != nil {
			t.Fatalf("%s is required on PATH: the script under test runs it for real", n)
		}
	}
}

// expectExit asserts the exit code and that every fragment appears in the output.
func expectExit(t *testing.T, r scriptRun, code int, fragments ...string) {
	t.Helper()
	if r.code != code {
		t.Errorf("exit code = %d, want %d\n%s", r.code, code, r)
	}
	for _, f := range fragments {
		if !strings.Contains(r.output(), f) {
			t.Errorf("output does not contain %q\n%s", f, r)
		}
	}
}

// refuseLeak asserts that none of the fixture values reached the output.
func refuseLeak(t *testing.T, r scriptRun, values ...string) {
	t.Helper()
	for _, v := range values {
		if strings.Contains(r.output(), v) {
			t.Errorf("a raw fixture value (%q) reached the script's output — scripts "+
				"handling secrets must print lengths, never values\n%s", v, r)
		}
	}
}

// --- helpers a fake uses from inside its own process ---

// argsMatch reports whether the joined arguments contain each part, in order —
// the Go form of a bash `case "$*" in *"a"*"b"*)` arm.
func argsMatch(args []string, parts ...string) bool {
	rest := strings.Join(args, " ")
	for _, p := range parts {
		i := strings.Index(rest, p)
		if i < 0 {
			return false
		}
		rest = rest[i+len(p):]
	}
	return true
}

// envExit returns the exit code a test asked a fake to return, defaulting to 0.
func envExit(name string) int {
	v := os.Getenv(name)
	if v == "" {
		return 0
	}
	var code int
	if _, err := fmt.Sscanf(v, "%d", &code); err != nil {
		fmt.Fprintf(os.Stderr, "fake: %s=%q is not an exit code\n", name, v)
		return 125
	}
	return code
}

// recordCall appends one call to the log file named by logEnv, when the test set one.
func recordCall(logEnv, prefix string, args []string) {
	path := os.Getenv(logEnv)
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake: open call log %s: %v\n", path, err)
		os.Exit(125)
	}
	_, werr := fmt.Fprintf(f, "%s: %s\n", prefix, strings.Join(args, " "))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		fmt.Fprintf(os.Stderr, "fake: write call log %s: %v\n", path, werr)
		os.Exit(125)
	}
}

// drainStdin consumes a piped stdin, as the real tool would. A fake that exits
// without reading makes the upstream writer die of SIGPIPE, and under the
// script's `set -o pipefail` that fails the pipeline for a reason no real
// cluster produces.
func drainStdin() {
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		fmt.Fprintf(os.Stderr, "fake: drain stdin: %v\n", err)
		os.Exit(125)
	}
}

// calls returns the lines a fake recorded in a call log, or none when nothing was
// called (the fake creates the file on its first call).
func calls(t *testing.T, logPath string) []string {
	t.Helper()
	b, err := os.ReadFile(logPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read call log %s: %v", logPath, err)
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

// countCalls counts the recorded calls that contain fragment.
func countCalls(t *testing.T, logPath, fragment string) int {
	t.Helper()
	n := 0
	for _, c := range calls(t, logPath) {
		if strings.Contains(c, fragment) {
			n++
		}
	}
	return n
}

// writeFixture writes a file into dir and returns its path.
func writeFixture(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", p, err)
	}
	return p
}

// readSource returns a file of the repository, relative to this package.
func readSource(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}
