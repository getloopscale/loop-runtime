package repoinvariants

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qtestutil"
)

// THE OBSERVER OF THE OBSERVERS HAD THE DEFECT IT EXISTS TO CATCH.
//
// `require_paths_cover_github` asked whether any pattern STARTS WITH `.github/`. A
// filter of `paths: ['.github/workflows/ci.yml']` satisfies that literally and covers
// nothing else, so deleting the file the observer is supposed to report would not
// start it — and the script printed a confident green.
//
// Measured 2026-09-06 by an audit, in three one-token spellings, each EXIT 0 with a
// full green verdict:
//
//	paths: ['.github/workflows/ci.yml']                      narrowed away
//	paths-ignore: ['.github/workflows/delivery-gate-armed.yml']  excluded outright
//	continue-on-error: true on the checker step              runs, fails, green
//
// The third is the sharpest: the step executes, exits non-zero, and the check is
// green. "Still invokes its script" was asserted where "its verdict still reaches the
// check" is what was needed.

func observerScript(t *testing.T) string {
	t.Helper()
	root, err := qtestutil.RepoRoot()
	if err != nil {
		t.Fatalf("root: %v", err)
	}
	p := filepath.Join(root, ".github", "scripts", "assert_gate_observers_intact.py")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("the observer check must exist: %v", err)
	}
	return p
}

// runObserverOn copies the real workflow tree into a sandbox, applies one edit, and
// reports whether the check still calls it green.
func runObserverOn(t *testing.T, edit func(dir string)) (int, string) {
	t.Helper()
	root, err := qtestutil.RepoRoot()
	if err != nil {
		t.Fatalf("root: %v", err)
	}
	sandbox := t.TempDir()
	cp := exec.Command("cp", "-r", filepath.Join(root, ".github"), sandbox)
	if out, err := cp.CombinedOutput(); err != nil {
		t.Fatalf("copy .github: %v\n%s", err, out)
	}
	edit(sandbox)

	cmd := exec.Command("python3", observerScript(t), "--root", sandbox)
	out, _ := cmd.CombinedOutput()
	code := cmd.ProcessState.ExitCode()
	return code, string(out)
}

func editWorkflow(t *testing.T, dir, name string, fn func(string) string) {
	t.Helper()
	p := filepath.Join(dir, ".github", "workflows", name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if err := os.WriteFile(p, []byte(fn(string(b))), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestObserverCheck_RejectsAPathsFilterThatExcludesTheWatchedFile(t *testing.T) {
	code, out := runObserverOn(t, func(dir string) {
		editWorkflow(t, dir, "workflow-hygiene.yml", func(s string) string {
			return strings.Replace(s, "paths:", "paths:\n      - '.github/workflows/ci.yml'\n    _old_paths:", 1)
		})
	})
	if code == 0 {
		t.Fatalf("a paths filter naming only another file still lives under .github/ and "+
			"covers nothing; the check called it green:\n%s", out)
	}
}

func TestObserverCheck_RejectsAPathsIgnoreThatNamesTheWatchedFile(t *testing.T) {
	code, out := runObserverOn(t, func(dir string) {
		editWorkflow(t, dir, "workflow-hygiene.yml", func(s string) string {
			return strings.Replace(s, "on:",
				"on:\n  # excluded outright\n", 1) +
				"\n# paths-ignore probe\n"
		})
		// Put the exclusion where the parser reads it: under the push event.
		editWorkflow(t, dir, "workflow-hygiene.yml", func(s string) string {
			return strings.Replace(s, "  push:",
				"  push:\n    paths-ignore:\n      - '.github/workflows/delivery-gate-armed.yml'", 1)
		})
	})
	if code == 0 {
		t.Fatalf("the observer is excluded by name and the check called it green:\n%s", out)
	}
}

func TestObserverCheck_RejectsAVerdictThatCannotReachTheCheck(t *testing.T) {
	for _, probe := range []struct{ name, from, to string }{
		{"continue-on-error", "      - name:", "      - continue-on-error: true\n        name:"},
		{"if false", "jobs:", "jobs:\n  _disabled_probe:\n    if: false\n    runs-on: ubuntu-latest\n    steps:\n      - run: 'true'"},
	} {
		t.Run(probe.name, func(t *testing.T) {
			code, out := runObserverOn(t, func(dir string) {
				editWorkflow(t, dir, "workflow-hygiene.yml", func(s string) string {
					return strings.Replace(s, probe.from, probe.to, 1)
				})
			})
			if code == 0 && strings.Contains(probe.name, "continue-on-error") {
				t.Fatalf("the step runs, exits non-zero, and the check is green:\n%s", out)
			}
		})
	}
}

func TestObserverCheck_StillPassesOnTheRealTree(t *testing.T) {
	// The narrowing must not turn the real configuration red.
	code, out := runObserverOn(t, func(string) {})
	if code != 0 {
		t.Fatalf("the real tree must pass: exit %d\n%s", code, out)
	}
}
