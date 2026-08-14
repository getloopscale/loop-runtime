// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
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

	// (3) That target's expansion runs this module's WHOLE suite.
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
	for _, want := range []string{
		"cd cmd/loopzero-ops && go test ./...",
		"cd tools/loopzero-dockerfile-lint && GOWORK=off go test ./...",
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
