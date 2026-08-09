// Package clusterrenameguard proves that a cluster rename touched only cluster
// identifiers, and never a line where the same word meant something else.
//
// The rename of `mgmt` → `core` and `vault` → `secrets` changes 1.388 live
// occurrences across 312 files (measured 2026-08-09, tree 26ba1340, see
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

// Check verifies that every GENERIC occurrence in the baseline is still present.
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
			b, err := os.ReadFile(filepath.Join(cfg.RepoRoot, occ.Path))
			if err != nil {
				cache[occ.Path] = ""
				out.Failures = append(out.Failures, fmt.Sprintf(
					"%s: file no longer readable, but the baseline classified %d occurrence(s) of %q in it as GENERIC",
					occ.Path, 1, occ.Token))
				continue
			}
			body = string(b)
			cache[occ.Path] = body
		}
		if body == "" {
			out.Failures = append(out.Failures, fmt.Sprintf(
				"%s: file no longer readable (generic %q)", occ.Path, occ.Token))
			continue
		}
		if !strings.Contains(body, occ.Content) {
			out.Failures = append(out.Failures, fmt.Sprintf(
				"%s: a line classified GENERIC for %q no longer exists — a false positive was renamed:\n      %s",
				occ.Path, occ.Token, strings.TrimSpace(occ.Content)))
		}
	}

	if len(out.Failures) > 0 {
		return out, errors.New("generic occurrences were modified")
	}
	return out, nil
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
