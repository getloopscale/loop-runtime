// Package clusterrenameguard proves that a cluster rename touched only cluster
// identifiers, and never a line where the same word meant something else.
//
// The rename of `mgmt` → `core` and `vault` → `secrets` changes 1.388 live
// occurrences across 312 files (measured 2026-08-09, tree 2a94065ed, see
// .claude/knowledge-base/discoveries/opportunities/cluster-topology-naming-opportunity.md).
// At that volume a human reviewing the diff detects errors of FORM, not errors of
// CLASSIFICATION: `// dispatches through the vault (OpenBao transit)` rewritten to
// `secrets` reads perfectly and is wrong, because there `vault` named the product,
// not the cluster. `vault` carries 74 such generic occurrences; `mgmt` carries zero.
//
// So the gate is the only real reviewer, and it asserts one thing: every line the
// baseline classified GENERIC still exists, byte for byte. It deliberately does NOT
// re-run the classifier — porting it to Go would put the classification itself
// inside the thing being verified, and a gate that recomputes what it checks can
// agree with itself while both are wrong.
package clusterrenameguard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// Occurrence is one line the baseline classified as GENERIC — a false positive of
// the rename, which must survive it unchanged.
type Occurrence struct {
	Path    string `json:"path"`
	Token   string `json:"token"`
	Content string `json:"content"`
}

// Baseline is the classification captured BEFORE the first edit.
type Baseline struct {
	// TreeSHA is the commit the baseline was measured against. Recorded so the
	// gate can refuse a baseline captured after the edits began (EC-1): such a
	// baseline compares the new state with itself and passes forever.
	TreeSHA string       `json:"tree_sha"`
	Generic []Occurrence `json:"generic"`
	// Amendments record captured lines that are legitimately gone — a file deleted for a
	// reason that has nothing to do with the rename. Without them a baseline can only ever
	// age into a permanent failure, because the tree it describes keeps moving.
	//
	// The field was in the JSON from 2026-08-09 and in no struct until 2026-09-16, so
	// `json.Unmarshal` dropped it and the four paths somebody amended went on being
	// reported as missing. A convention with no reader is worse than none: it looks like
	// the escape is there, so the next person uses it and nothing says it did nothing.
	Amendments []Amendment `json:"amendments"`
}

// Amendment exempts every occurrence under one of its paths.
//
// Scoped by PATH PREFIX so one entry covers a file or a subtree, and REFUSED WITHOUT A
// REASON — an exemption nobody justified is the shape the allowlist sunset contract
// rejects next door, and it is what turns a baseline into a rubber stamp.
type Amendment struct {
	Kind   string   `json:"kind"`
	Date   string   `json:"date"`
	Paths  []string `json:"paths"`
	Reason string   `json:"reason"`
	Count  int      `json:"count"`
}

// exempts reports whether any amendment carrying a reason covers this path.
func exempts(amendments []Amendment, path string) bool {
	for _, a := range amendments {
		if strings.TrimSpace(a.Reason) == "" {
			continue
		}
		for _, p := range a.Paths {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if path == p || strings.HasPrefix(path, strings.TrimSuffix(p, "/")+"/") {
				return true
			}
		}
	}
	return false
}

// Config controls the check.
type Config struct {
	RepoRoot     string
	BaselinePath string
	// SkipAncestryCheck disables the git ancestry guard. Tests set it because
	// their fixture root is not a git repository; production never does.
	SkipAncestryCheck bool
}

// Outcome is the summary.
type Outcome struct {
	Checked  int
	Failures []string
}

// Verdict is Fail when any generic occurrence did not survive the rename.
func (o Outcome) Verdict() verdict.Verdict { return verdict.Of(o.Failures) }

// Check verifies that every GENERIC occurrence in the baseline is still present.
// A modified occurrence is a Failure on the outcome; the error means the check
// could not run (an unreadable baseline, a baseline captured after the rename).
func Check(cfg Config) (Outcome, error) {
	var out Outcome

	raw, err := os.ReadFile(cfg.BaselinePath)
	if err != nil {
		return out, fmt.Errorf("read baseline: %w", err)
	}
	var base Baseline
	if err := json.Unmarshal(raw, &base); err != nil {
		return out, fmt.Errorf("parse baseline: %w", err)
	}

	if !cfg.SkipAncestryCheck {
		if err := assertAncestor(cfg.RepoRoot, base.TreeSHA); err != nil {
			return out, err
		}
	}

	// Read each file once; a baseline can cite the same file many times.
	cache := map[string]string{}
	for _, occ := range base.Generic {
		out.Checked++
		body, seen := cache[occ.Path]
		if !seen {
			// An unreadable file is NOT a failure on its own: the plan moves files with
			// `git mv`, so the line may simply live elsewhere now. Fall through to the
			// tree-wide search rather than accusing.
			b, err := os.ReadFile(filepath.Join(cfg.RepoRoot, occ.Path))
			if err == nil {
				body = string(b)
			}
			cache[occ.Path] = body
		}
		if body != "" && strings.Contains(body, occ.Content) {
			continue
		}
		if exempts(base.Amendments, occ.Path) {
			continue
		}
		// The line is not where the baseline recorded it. Before accusing anyone of
		// renaming a false positive, look for it elsewhere: the plan moves files with
		// `git mv` (EC-6), and the claim this guard defends is "the generic line still
		// exists", not "it still lives at this exact path". Tracking by path alone turned
		// every legitimate move into a false accusation.
		if existsAnywhere(cfg.RepoRoot, cfg.BaselinePath, occ.Content) {
			continue
		}
		out.Failures = append(out.Failures, fmt.Sprintf(
			"%s: a line classified GENERIC for %q exists nowhere in the tree — a false positive was renamed:\n      %s",
			occ.Path, occ.Token, strings.TrimSpace(occ.Content)))
	}

	return out, nil
}

// existsAnywhere reports whether the exact line survives somewhere in the tree.
//
// Uses `git grep --fixed-strings` so the content is matched literally — these lines are
// code and YAML, full of regex metacharacters. Falls back to a filesystem walk when git
// is unavailable (the unit tests run against a plain temp dir, not a repository).
func existsAnywhere(root, baselinePath, content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	// `:!<path>` excludes the baseline itself — it stores every generic line verbatim
	// as JSON, so searching it would find each one and the guard could never fail.
	cmd := exec.Command("git", "grep", "--fixed-strings", "--quiet", trimmed, "--", ".", ":!"+baselinePath)
	cmd.Dir = root
	err := cmd.Run()
	if err == nil {
		return true
	}
	// Exit 1 is git ANSWERING "not found", not git failing to be asked. Treating the two
	// alike sent every absent occurrence through the fallback walk below — measured
	// 2026-09-16 at over six minutes for one invocation, because the walk read all 554 MB
	// of `.git` file by file, once per absent occurrence. A higher status, or no binary at
	// all, is the only case where the question genuinely could not be put.
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return false
	}
	return walkFinds(root, baselinePath, trimmed)
}

// walkFinds is the fallback for when git cannot answer: no binary, or a root that is not
// a repository (which is what the unit tests build).
//
// It skips the trees that are not the working tree. `.git` is an object store, and a
// token found there is a token in history rather than in the code — the wrong answer as
// well as the slow one. It stops at the first match instead of reading the rest.
func walkFinds(root, baselinePath, trimmed string) bool {
	skip := map[string]bool{".git": true, "node_modules": true, "vendor": true}
	found := false
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable entry is not evidence of a rename
		}
		if info.IsDir() {
			if skip[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if abs, _ := filepath.Abs(path); abs == mustAbs(root, baselinePath) {
			return nil
		}
		b, readErr := os.ReadFile(path) // #nosec G304 -- walking the repo root under audit
		if readErr == nil && strings.Contains(string(b), trimmed) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// mustAbs resolves a possibly-relative baseline path against the repo root.
func mustAbs(root, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	abs, err := filepath.Abs(filepath.Join(root, p))
	if err != nil {
		return p
	}
	return abs
}

// assertAncestor refuses a baseline the gate cannot place before the current work.
//
// EC-1 of the edge-case review: a baseline captured after the first edit already
// contains the renamed state, so the comparison is the new state against itself —
// green, always, proving nothing. Requiring the baseline SHA to be an ancestor of
// HEAD makes that shape detectable instead of invisible.
func assertAncestor(root, sha string) error {
	if strings.TrimSpace(sha) == "" {
		return errors.New("baseline carries no tree_sha: cannot prove it predates the rename")
	}
	cmd := exec.Command("git", "merge-base", "--is-ancestor", sha, "HEAD")
	cmd.Dir = root
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("baseline tree_sha %s is not an ancestor of HEAD — it was captured on another branch, or after the rename began; refusing to report a green this gate cannot support", sha)
	}
	return nil
}
