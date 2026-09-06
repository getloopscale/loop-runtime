// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// .github/scripts/assert-delivery-gate-armed.py is what goes red on GitHub when
// the local delivery gate is not armed. These tests are what make its verdicts
// worth reading.
//
// The order matters and so does the first test. A checker that passes on
// everything and a checker that works are indistinguishable from a green run, so
// the anchor is asserted first — the script says PASS on this repository as it
// stands — and every test after it reconstructs one MEASURED historical state
// and requires the script to name it. A mutant that fails for some unrelated
// reason does not satisfy them: each asserts the premise tag it must fail on.
//
// Why the tests are here and the checker is not: the checker has to run where
// the hook does not, on a runner with no toolchain, so it is python3 and lives
// under .github/. Its tests have no such constraint and belong where the rest of
// this repository's gate assertions already live.

func armedCheckPath(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	p := filepath.Join(root, ".github", "scripts", "assert-delivery-gate-armed.py")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("the armed-check is the only thing that reports an unarmed gate from outside this machine; it must exist: %v", err)
	}
	return p
}

// runArmedCheck runs the checker against a root and returns its exit code and
// combined output. A non-zero exit is the subject of most of these tests, so it
// is returned rather than failing them.
func runArmedCheck(t *testing.T, root string) (int, string) {
	t.Helper()
	cmd := exec.Command("python3", armedCheckPath(t), "--root", root)
	out, err := cmd.CombinedOutput()
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("could not run the armed-check at all: %v\n%s", err, out)
	}
	return code, string(out)
}

// armedTree builds a synthetic repository root that IS armed: the real hook,
// copied byte for byte, plus the minimal settings.json that wires it. Every test
// below starts from this and breaks exactly one thing, so a failure names the
// break rather than the scaffolding.
//
// The hook is copied rather than stubbed because premise 3 runs it: a stub would
// let this suite pass while the real file had a bypass in it, which is the
// failure being defended against wearing a test's clothes.
func armedTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	real, err := os.ReadFile(deliveryGatePath(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "hooks", "delivery-gate.sh"), real, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSettings(t, root, `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[`+
		`{"type":"command","command":"python3 \"$CLAUDE_PROJECT_DIR/.claude/hooks/validate-command.py\""},`+
		`{"type":"command","command":"bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/delivery-gate.sh\""}]}]}}`)
	return root
}

func writeSettings(t *testing.T, root, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, ".claude", "settings.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// THE ANCHOR. Without this every test below is satisfied by a script that always
// fails, and the suite would be green while the check was worthless.
func TestArmedCheck_PassesOnThisRepositoryAsItStands(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	code, out := runArmedCheck(t, root)
	if code != 0 {
		t.Fatalf("the armed-check fails on this repository — either the gate is genuinely not armed, or the check is wrong.\nexit %d\n%s", code, out)
	}
	for _, want := range []string{"PREMISE 1", "PREMISE 2", "PREMISE 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("the check passed without reporting %s. A premise it did not mention is a premise it did not check.\n%s", want, out)
		}
	}
}

// And the synthetic tree it is built from must pass too, or nothing below is
// measuring what it claims to measure.
func TestArmedCheck_PassesOnTheSyntheticArmedTree(t *testing.T) {
	code, out := runArmedCheck(t, armedTree(t))
	if code != 0 {
		t.Fatalf("the armed fixture does not pass, so every mutant below would fail for the wrong reason.\nexit %d\n%s", code, out)
	}
}

// 9a6e6e903, 2026-09-02: the third deletion. It touched nothing outside
// `.claude/**`, so ci.yml's paths-ignore would not have started ci.yml for it.
func TestArmedCheck_FailsWhenTheHookFileIsGone(t *testing.T) {
	root := armedTree(t)
	if err := os.Remove(filepath.Join(root, ".claude", "hooks", "delivery-gate.sh")); err != nil {
		t.Fatal(err)
	}

	code, out := runArmedCheck(t, root)
	if code == 0 {
		t.Fatalf("the hook is deleted and the check reported the gate armed:\n%s", out)
	}
	if !strings.Contains(out, "PREMISE 1") {
		t.Errorf("the refusal must name the premise that broke; got:\n%s", out)
	}
	// The hook is gone, so nothing observed a bypass. Reporting premise 3 as
	// satisfied here would be the whole defect family in one line.
	if !strings.Contains(out, "NOT CHECKED") || !strings.Contains(out, "PREMISE 3") {
		t.Errorf("with no hook to run, premise 3 must be reported as NOT CHECKED, never as passing; got:\n%s", out)
	}
}

// 63cc51c48 -> 53c1dc1ea, 2026-08-29: the state that lasted four days. The FILE
// is present and correct; only the wiring is gone. Every assertion that existed
// at the time was green through this, which is why it went unnoticed.
func TestArmedCheck_FailsWhenTheFileIsPresentButNotWired(t *testing.T) {
	root := armedTree(t)
	writeSettings(t, root, `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[`+
		`{"type":"command","command":"python3 \"$CLAUDE_PROJECT_DIR/.claude/hooks/validate-command.py\""}]}]}}`)

	code, out := runArmedCheck(t, root)
	if code == 0 {
		t.Fatalf("the hook exists on disk and fires for nothing, and the check called that armed:\n%s", out)
	}
	if !strings.Contains(out, "PREMISE 2") {
		t.Errorf("the refusal must name the wiring; got:\n%s", out)
	}
	if !strings.Contains(out, "PREMISE 1") {
		t.Errorf("premise 1 genuinely holds here and the report must still say so, or the operator cannot tell which half broke; got:\n%s", out)
	}
}

// The same absence one step wider: a settings file wiring the hook to Edit|Write
// covers no push at all. A block that does not match Bash must not answer for one
// that does.
func TestArmedCheck_FailsWhenTheWiringDoesNotCoverBash(t *testing.T) {
	root := armedTree(t)
	writeSettings(t, root, `{"hooks":{"PreToolUse":[{"matcher":"Edit|Write","hooks":[`+
		`{"type":"command","command":"bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/delivery-gate.sh\""}]}]}}`)

	code, out := runArmedCheck(t, root)
	if code == 0 {
		t.Fatalf("the gate is wired to a matcher that no `git push` ever hits, and the check called that armed:\n%s", out)
	}
	if !strings.Contains(out, "PREMISE 2") {
		t.Errorf("the refusal must name the wiring; got:\n%s", out)
	}
}

// Settings the harness cannot parse wire nothing. "I could not read it" must not
// come out as a pass, and must not come out as a shrug either.
func TestArmedCheck_FailsClosedOnSettingsItCannotRead(t *testing.T) {
	for name, body := range map[string]string{
		"truncated JSON":        `{"hooks":{"PreToolUse":[`,
		"PreToolUse not a list": `{"hooks":{"PreToolUse":"Bash"}}`,
		"block not an object":   `{"hooks":{"PreToolUse":["Bash"]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			root := armedTree(t)
			writeSettings(t, root, body)

			code, out := runArmedCheck(t, root)
			if code == 0 {
				t.Fatalf("the check could not read the wiring and reported the gate armed anyway:\n%s", out)
			}
			if !strings.Contains(out, "PREMISE 2") {
				t.Errorf("the refusal must name the premise it could not establish; got:\n%s", out)
			}
		})
	}
}

func TestArmedCheck_FailsWhenThereAreNoSettingsAtAll(t *testing.T) {
	root := armedTree(t)
	if err := os.Remove(filepath.Join(root, ".claude", "settings.json")); err != nil {
		t.Fatal(err)
	}

	code, out := runArmedCheck(t, root)
	if code == 0 {
		t.Fatalf("nothing in the tree wires any hook and the check reported the gate armed:\n%s", out)
	}
	if !strings.Contains(out, "PREMISE 2") {
		t.Errorf("the refusal must name the wiring; got:\n%s", out)
	}
}

// The third premise, and the only one whose falsification is behavioural. This
// hook is present, wired, and blocks the plainest push — it looks armed by every
// structural measure — and it is the pre-2026-09-05 matching logic, which let
// four real pushes reach the remote ungated.
//
// It is a reconstruction rather than the old file: the old file is what the
// restore was made from, and pinning the two causes is more honest than pinning
// a revision that also carried unrelated differences.
func TestArmedCheck_FailsWhenTheHookCarriesTheMeasuredBypass(t *testing.T) {
	root := armedTree(t)
	oldMatching := "#!/usr/bin/env bash\n" +
		"set -uo pipefail\n" +
		"INPUT=\"$(cat)\"\n" +
		"COMMAND=\"$(printf '%s' \"$INPUT\" | python3 -c 'import json,sys\n" +
		"print(json.load(sys.stdin).get(\"tool_input\", {}).get(\"command\", \"\"))' 2>/dev/null)\"\n" +
		// Cause 1: only value-less flags between `git` and `push`, so
		// `git -C <path> push` reads as not-a-push.
		"printf '%s' \"$COMMAND\" | grep -qE '(^|[;&|]|\\s)git(\\s+-[a-zA-Z])*\\s+push(\\s|$)' || exit 0\n" +
		// Cause 2: --dry-run searched across the WHOLE command, so the word
		// anywhere disarms the gate for the push beside it.
		"printf '%s' \"$COMMAND\" | grep -qE '(^|\\s)--dry-run(\\s|$)' && exit 0\n" +
		"echo \"BLOCKED: reconstruction of the pre-fix gate\" >&2\n" +
		"exit 2\n"
	if err := os.WriteFile(filepath.Join(root, ".claude", "hooks", "delivery-gate.sh"), []byte(oldMatching), 0o755); err != nil {
		t.Fatal(err)
	}

	code, out := runArmedCheck(t, root)
	if code == 0 {
		t.Fatalf("four payloads reached the remote with this hook deciding nothing, and the check reported no bypass:\n%s", out)
	}
	if !strings.Contains(out, "PREMISE 3") {
		t.Errorf("the refusal must name the bypass premise; got:\n%s", out)
	}
	// Naming the count is what tells the reader a spelling escaped rather than
	// the hook being broken outright — the two need different fixes.
	if !strings.Contains(out, "git -C") {
		t.Errorf("the refusal must name the payload that escaped, or the reader has to rediscover it; got:\n%s", out)
	}
}

// The other direction. A hook rewritten as a bare `exit 2` satisfies every
// must-be-gated payload while checking nothing, and a gate that fires on `git
// status` is removed within a day — which is how a premise stops holding without
// anybody deciding to stop holding it.
func TestArmedCheck_FailsWhenTheHookBlocksIndiscriminately(t *testing.T) {
	root := armedTree(t)
	body := "#!/usr/bin/env bash\ncat >/dev/null\necho 'BLOCKED: everything' >&2\nexit 2\n"
	if err := os.WriteFile(filepath.Join(root, ".claude", "hooks", "delivery-gate.sh"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	code, out := runArmedCheck(t, root)
	if code == 0 {
		t.Fatalf("a hook that blocks every Bash call decides nothing, and the check reported it armed:\n%s", out)
	}
	if !strings.Contains(out, "PREMISE 3") {
		t.Errorf("the refusal must name the premise; got:\n%s", out)
	}
}

// An empty file is present, is a file, and gates nothing. Presence checks that
// stop at `exists` are the family this repository has fixed twice.
func TestArmedCheck_FailsOnAnEmptyHookFile(t *testing.T) {
	root := armedTree(t)
	if err := os.WriteFile(filepath.Join(root, ".claude", "hooks", "delivery-gate.sh"), nil, 0o755); err != nil {
		t.Fatal(err)
	}

	code, out := runArmedCheck(t, root)
	if code == 0 {
		t.Fatalf("an empty hook file was reported as an armed gate:\n%s", out)
	}
	if !strings.Contains(out, "PREMISE 1") {
		t.Errorf("the refusal must name the premise; got:\n%s", out)
	}
}
