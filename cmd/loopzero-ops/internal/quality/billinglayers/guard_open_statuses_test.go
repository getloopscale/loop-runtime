package billinglayers

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// repoRootForTest walks up for a marker that exists in BOTH a normal checkout and
// a linked worktree.
//
// `go.work` is checked by EXISTENCE and so is `.git`, never with IsDir: a linked
// worktree's `.git` is a regular FILE holding a `gitdir:` line, and requiring a
// directory is what made three sigstore tests fail in every lane until B-178.
// Failing loudly beats guessing a depth — a hardcoded "five levels up" is the
// same defect this ecosystem removed from four other guards this week.
func repoRootForTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		for _, marker := range []string{"go.work", ".git"} {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.work or .git above the test's working directory — this guard " +
				"cannot locate the repository and must say so rather than assert on nothing")
		}
		dir = parent
	}
}

// TestOpenStatusesAgreeWithTheInstalledContract keeps this map honest against the
// rule the kit installs, rather than against a copy of it written here.
//
// `openStatuses` is the SIXTH reader of `status:` in this ecosystem and the only
// one in Go. The kit pins the other five against each other in
// `tests/test_status_readers_agree.py` — all Python, all inside the kit — so
// nothing there can see this one. The gap is not loopzeroretical: `approved` reached
// the contract on 2026-09-04 and this map on 2026-09-05, in between failing the
// whole gate chain when an item moved into it.
//
// The contract is read from `.claude/rules/cycle-backlog.md`, the installed copy,
// so a status added upstream fails HERE — in a unit test, in seconds — instead of
// failing somebody's push after eight gates have run.
func TestOpenStatusesAgreeWithTheInstalledContract(t *testing.T) {
	root := repoRootForTest(t)
	rule := filepath.Join(root, ".claude", "rules", "cycle-backlog.md")
	body, err := os.ReadFile(rule) // #nosec G304 -- path derived from the repo root
	if err != nil {
		t.Fatalf("could not read the installed contract at %s: %v — without it this "+
			"test asserts nothing, and passing would be worse than failing", rule, err)
	}

	// The schema row enumerates every legal status.
	row := regexp.MustCompile(`(?m)^\|\s*` + "`status`" + `\s*\|.*$`).Find(body)
	if row == nil {
		t.Fatal("the installed cycle-backlog.md has no `status` schema row; this test " +
			"cannot tell which statuses are legal and must not guess")
	}
	declared := regexp.MustCompile("`(raw|triaged|approved|planned|shipped|killed)`").
		FindAllStringSubmatch(string(row), -1)
	if len(declared) == 0 {
		t.Fatal("the schema row names no statuses — the contract moved and this test " +
			"is reading the wrong line")
	}

	// Terminal by definition: an item there is not debt with an owner.
	terminal := map[string]bool{"shipped": true, "killed": true}

	var missing []string
	for _, m := range declared {
		name := m[1]
		if terminal[name] {
			if openStatuses[name] {
				t.Errorf("%q is terminal in the contract and open here — a closed item "+
					"would keep a live layer covered", name)
			}
			continue
		}
		if !openStatuses[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("the contract declares %s as non-terminal and openStatuses does not hold "+
			"them. An item in one of those states is debt with an owner, and this gate "+
			"would report its layer uncovered — failing every push until someone edits "+
			"the registry to work around a gate that is simply behind.",
			strings.Join(missing, ", "))
	}
}
