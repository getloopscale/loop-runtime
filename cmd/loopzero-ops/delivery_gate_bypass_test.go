package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Four payloads that reach the remote and never reach the gate, found by an
// independent audit on 2026-09-05 against the restored hook. The hook's own header,
// rules/squad-map.md and the allowlist row all assert "There is NO bypass flag."
//
// Two causes, both inherited verbatim from the last live version and both on the
// MATCH path rather than the error paths the restore audited:
//
//   - PUSH_RE allowed only FLAGS between `git` and `push`, so `git -C <path> push`
//     breaks the run. `-C` is idiomatic in this repository and the /tmp/lane-*
//     worktree convention makes `git -C <lane> push` the natural next form.
//   - the `--dry-run` skip grepped the WHOLE command, so the word anywhere in a
//     compound command disarmed the gate for the push beside it.
//
// A gate that concludes "not a push" about a real push is the same family as one
// that passes when it cannot run: it reports a verdict about something it did not
// look at.

func deliveryGatePath(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	p := filepath.Join(root, ".claude", "hooks", "delivery-gate.sh")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("the delivery gate is the CI substitute; it must exist: %v", err)
	}
	return p
}

// gateRuns reports whether the hook decided the command was a push. It stops the
// hook before it can spend six minutes on quality:all by pointing CLAUDE_PROJECT_DIR
// at a directory with no Taskfile, where the gate refuses and says so — a decision
// it only reaches AFTER concluding the command was a push.
func gateRuns(t *testing.T, command string) bool {
	t.Helper()
	dir := t.TempDir()
	payload := `{"tool_name":"Bash","tool_input":{"command":` + quoteJSON(command) + `}}`

	cmd := exec.Command("bash", deliveryGatePath(t))
	cmd.Stdin = strings.NewReader(payload)
	cmd.Env = append(os.Environ(), "CLAUDE_PROJECT_DIR="+dir)
	out, _ := cmd.CombinedOutput()
	// "not a push" exits 0 in silence; a push it cannot gate says BLOCKED and why.
	return strings.Contains(string(out), "BLOCKED")
}

func quoteJSON(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func TestDeliveryGate_SeesAPushThroughAWorkingDirectoryFlag(t *testing.T) {
	// `git -C <path> push` is how every lane in this repository would push.
	if !gateRuns(t, "git -C /tmp/lane-b187 push origin main") {
		t.Fatal("a real push reached the remote without the gate deciding anything about it")
	}
}

func TestDeliveryGate_IsNotDisarmedByDryRunElsewhereInTheCommand(t *testing.T) {
	for _, command := range []string{
		"echo --dry-run ; git push origin main",
		"git push origin main --force # --dry-run",
		"git push origin main; ls --dry-run",
	} {
		if !gateRuns(t, command) {
			t.Errorf("--dry-run somewhere else in the command disarmed the gate for a real push: %q", command)
		}
	}
}

func TestDeliveryGate_StillSkipsAPushThatIsItselfADryRun(t *testing.T) {
	// The skip exists because a dry run changes nothing on the remote, and
	// quality:all costs minutes. Narrowing the match must not delete it.
	if gateRuns(t, "git push origin main --dry-run") {
		t.Fatal("a genuine --dry-run push now pays for the full suite")
	}
}

func TestDeliveryGate_StillIgnoresWhatIsNotAPush(t *testing.T) {
	// The other direction: over-matching costs six minutes per false trigger, so
	// the widened pattern must not turn every git invocation into a gate run.
	for _, command := range []string{
		"git log --grep push",
		"git commit -m 'ready to push'",
		"git status",
	} {
		if gateRuns(t, command) {
			t.Errorf("gate ran for something that is not a push: %q", command)
		}
	}
}

func TestDeliveryGate_TheControlsStillGate(t *testing.T) {
	for _, command := range []string{"git push", "git push origin main"} {
		if !gateRuns(t, command) {
			t.Errorf("the plainest push is not gated: %q", command)
		}
	}
}

// A SECOND ROUND OF BYPASSES, one of them introduced by the fix for the first.
//
// Measured 2026-09-06 by an independent audit, same method as the first round: a real
// push that reaches the remote while the gate decides nothing.
//
//   (git push origin main)                                  the anchor class had no `(`
//   $(git push origin main)                                 nor `$`
//   git push --dry-run origin main; git push origin main    PUSH_SEGMENT did `head -1`
//
// The third is mine. Scoping the --dry-run check to "the segment the push is in" was
// the fix for the first round; taking only the FIRST such segment meant a flag
// belonging to one command waved through the command beside it. The skip exists for a
// push that changes nothing on the remote, so it applies only when EVERY push in the
// line is a dry run.

func TestDeliveryGate_SeesAPushInsideASubshell(t *testing.T) {
	for _, command := range []string{
		"(git push origin main)",
		"$(git push origin main)",
	} {
		if !gateRuns(t, command) {
			t.Errorf("a push inside a subshell is a push: %q", command)
		}
	}
}

func TestDeliveryGate_CountsEveryPushSegmentNotTheFirst(t *testing.T) {
	for _, command := range []string{
		"git push --dry-run origin main; git push origin main",
		"git push origin main; git push --dry-run origin main",
	} {
		if !gateRuns(t, command) {
			t.Errorf("one dry run does not make the real push beside it a dry run: %q", command)
		}
	}
}

func TestDeliveryGate_StillSkipsWhenEveryPushIsADryRun(t *testing.T) {
	// The skip must survive the narrowing, or every dry run pays for the suite.
	for _, command := range []string{
		"git push origin main --dry-run",
		"git push --dry-run origin main; git push --dry-run origin dev",
	} {
		if gateRuns(t, command) {
			t.Errorf("every push here is a dry run and changes nothing on the remote: %q", command)
		}
	}
}
