// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// deliveryGateHook is the PreToolUse(Bash) hook that intercepts `git push`. It
// is the CI substitute of this repository and has no bypass.
const deliveryGateHook = "../../.claude/hooks/delivery-gate.sh"

// runGateWithTaskShim executes the hook for real, with a recording stand-in for
// `task` first on PATH, and returns every argv the hook handed to it.
//
// Reading the hook as TEXT was the first attempt and it was bypassable three
// ways, each measured against the real file: `if task quality:all --dry` (the
// regex captured the target and ignored the flag, so the gate ran NOTHING); an
// `if [ -n "$LOOPZERO_FAST_PUSH" ]` branch (the slow arm alone satisfied an any-of
// assertion); and `command task` plus a decoy line inside a heredoc (the
// line-anchored pattern never saw the real invocation).
//
// The property being asserted is behavioural — "the command the hook actually
// executes reaches the module's tests" — so it has to be observed
// behaviourally. A shim is the cheapest honest observer: it cannot be talked
// out of recording what it was called with.
func runGateWithTaskShim(t *testing.T) [][]string {
	t.Helper()

	dir := t.TempDir()
	record := filepath.Join(dir, "invocations")
	shim := filepath.Join(dir, "task")
	// Records argv one line per invocation, then succeeds so the hook proceeds.
	body := "#!/usr/bin/env bash\nprintf '%s\\n' \"$*\" >>" + record + "\nexit 0\n"
	if err := os.WriteFile(shim, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", deliveryGateHook)
	cmd.Stdin = strings.NewReader(`{"tool_input":{"command":"git push"}}`)
	cmd.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"CLAUDE_PROJECT_DIR="+root,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the hook did not accept a plain `git push`: %v\n%s", err, out)
	}

	b, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("the hook ran without invoking `task` at all — nothing gates the push: %v", err)
	}
	var argvs [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		argvs = append(argvs, strings.Fields(line))
	}
	return argvs
}

// The delivery gate runs exactly one target, it runs it for real, and that
// target's expansion reaches this module's whole test suite.
//
// Three claims in one test on purpose: they are the same property observed at
// three depths, and splitting them let the review satisfy each half with a
// different command.
//
// Why it matters: until 2026-08-13 the hook ran `quality:gates`, whose deps are
// `[build]` and nothing else. The tests that prove a gate REJECTS what it must
// reject live in this module and were run by `quality:hermetic`, which the hook
// never called. A gate could be silently broken — an inverted condition, an
// exemption widened — and the push would sail through, because the only
// mandatory checkpoint in the flow never looked at the net protecting the gates.
//
// That was not an oversight: the suite cost 408 s, of which 390 s were four
// hermetic tests waiting on a real clock (B-101). With the cost gone, the reason
// for the omission went with it. See
// knowledge-base/decisions/MEET-ASYNC-AMENDMENT-2026-08-13-delivery-gate-hermetic.md
func TestDeliveryGate_RunsOneRealTargetThatCoversThisModule(t *testing.T) {
	if _, err := exec.LookPath("task"); err != nil {
		// NOT a skip. A skip here would delete the guard silently on exactly the
		// machine where somebody is most likely to be improvising, and the only
		// path that runs this test is `quality:hermetic` — reachable solely
		// through `task`, so on the path that matters `task` is present by
		// construction.
		t.Fatal("go-task is not on PATH, and it is a declared prerequisite of this repository — the guard cannot verify the delivery gate without it")
	}

	argvs := runGateWithTaskShim(t)

	// (1) Exactly one invocation. More than one means a BRANCH, and a branch in
	// the delivery gate is where a bypass lives — the review built one out of
	// `if [ -n "$LOOPZERO_FAST_PUSH" ]` and satisfied the earlier any-of assertion
	// with the slow arm while the fast arm skipped hermetic entirely. The hook's
	// own header says there is no bypass flag; this is what defends that claim.
	if len(argvs) != 1 {
		t.Fatalf("the delivery gate must invoke exactly one target — %d invocations means a branch, and a branch is where a bypass lives: %v", len(argvs), argvs)
	}
	argv := argvs[0]

	// (2) It runs it. `--dry`/`-n` renders without executing: one word, and the
	// gate checks nothing while still naming the right target.
	for _, a := range argv {
		if a == "--dry" || a == "-n" || a == "--dry-run" {
			t.Fatalf("the gate invokes `task %s` — it RENDERS the target without executing it, so nothing is actually checked", strings.Join(argv, " "))
		}
	}

	target := ""
	for _, a := range argv {
		if !strings.HasPrefix(a, "-") {
			target = a
			break
		}
	}
	if target == "" {
		t.Fatalf("no target in the gate's task invocation: %v", argv)
	}

	// (3) A expansao do alvo roda a suite INTEIRA deste modulo.
	//
	// Esta assercao le' a saida de `task --dry`, e `--dry` SUPER-REPORTA: um
	// `cmds:` em bloco (`- |`) e' impresso verbatim, sem avaliar o `if`/`exit` de
	// dentro. Medido em 2026-08-19 injetando exatamente essa forma — o `--dry`
	// imprimiu a chamada e a execucao real pulou (B-102). O complemento que fecha
	// o furo e' `TestPernaHermeticaRodaDeFatoASuiteDesteModulo`, que roda o alvo
	// com um substituto de `go` e observa o que foi EXECUTADO.
	//
	// Esta metade fica: ela cobre a LINHA exata, que a observacao nao distingue
	// (o substituto ve' `test ./...`, nao a forma do comando que o produziu).
	//
	// One assertion on the exact line, not two loose substrings. The review
	// showed that `cd cmd/loopzero-ops && go test ./internal/quality/...` plus any
	// unrelated `go test ./...` satisfied a split pair — while carving out
	// internal/seed, which is the regression the second half existed to prevent
	// and the package where the next forgotten wall-clock wait would land.
	out, err := exec.Command("task", "--dry", target).CombinedOutput()
	if err != nil {
		t.Fatalf("`task --dry %s` failed — the gate invokes a target that does not render: %v\n%s", target, err, out)
	}
	// B-088 names cmd/loopzero-ops AND tools/ — the latter sits outside go.work by
	// design, which is precisely why nobody reached it: those tests lived only
	// in `test:unit`, a target the delivery gate never calls.
	//
	// B-088 named TWO binaries under tools/. `loopzero-dockerfile-lint` was removed on
	// 2026-08-19 (B-157): it validated user-supplied Dockerfiles, and that path was
	// deleted in T2.2 — the build now generates a canonical Dockerfile in-process, so
	// nothing invoked it. Its expectation is gone with it; the surviving one stays,
	// because the reason B-088 exists (tools/ sits outside go.work and is easy to miss)
	// did not go away.
	for _, want := range []string{
		"cd cmd/loopzero-ops && go test ./...",
		"cd tools/gen-service-auth-ed25519 && GOWORK=off go test ./...",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the delivery gate runs `task %s`, whose expansion never reaches %q.\n"+
				"Those tests are therefore OUTSIDE the only mandatory checkpoint in the flow:\n"+
				"they can break and the push still passes.\nExpansion was:\n%s", target, want, out)
		}
	}
}

// The hook's source carries exactly one `task` invocation.
//
// The behavioural test above observes the branch TAKEN under this test's
// environment; it cannot see an arm guarded by an env var nobody set. Counting
// invocations in the source closes that, and it is deliberately broad about the
// forms — `command task`, `env task` and a bare `task` are all invocations, and
// the review used the first of those to hide one from a line-anchored pattern.
func TestDeliveryGate_SourceCarriesExactlyOneTaskInvocation(t *testing.T) {
	b, err := os.ReadFile(deliveryGateHook)
	if err != nil {
		t.Fatalf("the delivery gate is the CI substitute; it must exist: %v", err)
	}

	// Comments and heredoc prose are not invocations. Strip full-line comments;
	// anything else that mentions `task` in a command position counts.
	invocation := regexp.MustCompile(`(?:^|[;&|]|\bthen\b|\belse\b|\bdo\b|\bif\b|\bcommand\b|\benv\b)\s*task\s+[a-z0-9:_-]+`)
	count := 0
	for _, line := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		count += len(invocation.FindAllString(line, -1))
	}
	if count != 1 {
		t.Errorf("the delivery gate's source carries %d `task <target>` invocations; it must carry exactly one.\n"+
			"A second one is a branch, and a branch is where a bypass flag lives — the behavioural test\n"+
			"above only observes the arm this environment happens to take.", count)
	}
}

// ---------------------------------------------------------------------------
// B-187. The three tests below were added when the hook was restored on
// 2026-09-05, after it had been deleted three times and restored twice.
//
// The two tests above assert what the gate RUNS. They say nothing about what it
// does when it cannot run — and "a gate that passes when it cannot run its own
// subject" is the defect family this repository measured six times in the week
// before the restore. The last live version carried one instance of it in a
// single line, `cd "$REPO_ROOT" || exit 0`, and a second in a `try/except` that
// turned an unreadable tool call into an empty command that matched nothing.
// ---------------------------------------------------------------------------

// gateRun is one execution of the hook, observed from outside: what it returned,
// what it told `task` to do, and what it said on the way.
type gateRun struct {
	exitCode    int
	invocations [][]string
	stderr      string
}

// runGate executes the hook for real with a recording `task` shim first on PATH
// and does NOT fail the test on a non-zero exit — the exit code is the subject
// of these tests, not an accident of them.
func runGate(t *testing.T, payload, projectDir string) gateRun {
	t.Helper()

	dir := t.TempDir()
	record := filepath.Join(dir, "invocations")
	shim := filepath.Join(dir, "task")
	body := "#!/usr/bin/env bash\nprintf '%s\\n' \"$*\" >>" + record + "\nexit 0\n"
	if err := os.WriteFile(shim, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	var stderr bytes.Buffer
	cmd := exec.Command("bash", deliveryGateHook)
	cmd.Stdin = strings.NewReader(payload)
	cmd.Stderr = &stderr
	cmd.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"CLAUDE_PROJECT_DIR="+projectDir,
	)
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("could not run the delivery gate at all: %v", err)
	}

	run := gateRun{exitCode: code, stderr: stderr.String()}
	b, readErr := os.ReadFile(record)
	if readErr != nil {
		return run // no invocations recorded; the file is only created on first use
	}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		run.invocations = append(run.invocations, strings.Fields(line))
	}
	return run
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// Pointed at a directory that is not this repository, the gate must say so and
// block — never exit 0.
//
// `CLAUDE_PROJECT_DIR` is supplied by the harness and the fallback is
// `git rev-parse --show-toplevel || pwd`, so "somewhere else" is not a
// hypothetical: a session started outside the tree lands on `pwd`. The last
// live version had no check at all here — it would have run `task quality:all`
// in whatever directory it found, and the only thing standing between that and
// a green gate was go-task refusing to start.
func TestDeliveryGate_BlocksWhenItIsNotStandingInThisRepository(t *testing.T) {
	elsewhere := t.TempDir() // exists, is readable, has no Taskfile.yml

	run := runGate(t, `{"tool_input":{"command":"git push"}}`, elsewhere)

	if run.exitCode != 2 {
		t.Errorf("the gate exited %d from a directory with no Taskfile — it must block (2).\nstderr:\n%s", run.exitCode, run.stderr)
	}
	if len(run.invocations) != 0 {
		t.Errorf("the gate invoked task %v outside the repository — whatever that ran, it was not this tree's gates", run.invocations)
	}
	if !strings.Contains(run.stderr, "Taskfile.yml") {
		t.Errorf("the gate blocked without naming what it could not find; the operator has to guess.\nstderr:\n%s", run.stderr)
	}
}

// The same property one step earlier: a project dir that does not exist at all.
//
// Separate from the test above because it exercises a different line. The last
// live version's was `cd "$REPO_ROOT" || exit 0` — a failed `cd` reported as a
// green gate, which is the whole defect family in seven characters.
func TestDeliveryGate_BlocksWhenTheProjectDirectoryDoesNotExist(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "no-such-directory")

	run := runGate(t, `{"tool_input":{"command":"git push"}}`, gone)

	if run.exitCode != 2 {
		t.Errorf("the gate exited %d when it could not enter its project directory — it must block (2).\nstderr:\n%s", run.exitCode, run.stderr)
	}
	if len(run.invocations) != 0 {
		t.Errorf("the gate invoked task %v from a directory it could not enter", run.invocations)
	}
}

// A tool call the hook cannot parse must not be read as "not a push".
//
// The hook reads the command out of the JSON on stdin with python3. The last
// live version wrapped that in `try: ... except: print("")` and appended
// `|| true`, so a parse failure — or python3 simply being absent — produced an
// empty command, which matched no push pattern, which exited 0. On such a
// machine EVERY push passed the CI substitute without a single gate running,
// and nothing on stderr said so.
//
// The payload here is truncated JSON that still contains the push, so a hook
// that reads it correctly and a hook that cannot read it at all are
// distinguishable by exactly one thing: whether the gate ran.
func TestDeliveryGate_StillGatesAPushItCouldNotParse(t *testing.T) {
	if _, err := exec.LookPath("task"); err != nil {
		t.Fatal("go-task is not on PATH, and it is a declared prerequisite of this repository — the guard cannot verify the delivery gate without it")
	}

	run := runGate(t, `{"tool_input":{"command":"git push"`, repoRoot(t))

	if len(run.invocations) != 1 {
		t.Fatalf("the gate ran %d target(s) on a tool call it could not parse; it must still run exactly one.\n"+
			"An unreadable payload that reads as 'not a push' is how every push passes on a machine\n"+
			"where python3 is missing.\ninvocations: %v\nstderr:\n%s", len(run.invocations), run.invocations, run.stderr)
	}
	if run.exitCode != 0 {
		t.Errorf("the shim reported the gate green, so the hook must allow the push; it exited %d.\nstderr:\n%s", run.exitCode, run.stderr)
	}
	if !strings.Contains(run.stderr, "could not read the tool call") {
		t.Errorf("the gate degraded silently — it must say that it could not read the tool call.\nstderr:\n%s", run.stderr)
	}
}

// The hook file existing is not the hook running. It must be wired.
//
// This is the half that was missed the last time somebody restored this file.
// `63cc51c48` (2026-08-29) replaced the `hooks` key of `.claude/settings.json`
// wholesale and the delivery-gate entry went with it; `53c1dc1ea` restored the
// FILE the same day and never noticed the wiring was gone. For the four days
// between that and the third deletion, the CI substitute of this repository
// existed on disk, passed nothing, blocked nothing, and was cited in four
// documents as the reason a check was covered.
//
// The two assertions above would have been green throughout.
func TestDeliveryGate_IsWiredIntoThePreToolUseBashHook(t *testing.T) {
	b, err := os.ReadFile("../../.claude/settings.json")
	if err != nil {
		t.Fatalf("cannot read the harness settings that wire this repository's hooks: %v", err)
	}
	var settings struct {
		Hooks struct {
			PreToolUse []struct {
				Matcher string `json:"matcher"`
				Hooks   []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &settings); err != nil {
		t.Fatalf("settings.json does not parse: %v", err)
	}

	for _, block := range settings.Hooks.PreToolUse {
		if block.Matcher != "Bash" {
			continue
		}
		for _, h := range block.Hooks {
			if strings.Contains(h.Command, "delivery-gate.sh") {
				return
			}
		}
	}
	t.Errorf("no PreToolUse(Bash) hook in .claude/settings.json runs delivery-gate.sh.\n" +
		"The file can be present and the gate still never fire — that state lasted four days\n" +
		"in August and both assertions above stayed green through it.")
}
