// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package scriptcount is the program-level monotonic ceiling on the number of
// canonical shell scripts (.sh) in the repository, for the Script Minimization
// Program (2026-05-24-script-minimization-program-plan, T0.1).
//
// It complements:
//   - no-new-bash (blocks ADDING a .sh not in the allowlist), and
//   - unhomed-logic (counts un-homed scripts by allowlist disposition),
//
// by enforcing the raw canonical .sh count never regresses above a baseline.
// This catches the "half-migration leftover" failure (a Go gate is added but
// the bash it replaced is never deleted) at the program level even when
// unhomed-logic would not flag it.
//
// Design constraints:
//   - Source of truth is the git index (`git ls-files`), NOT the on-disk tree.
//     A filepath.WalkDir count ignored .gitignore, so any untracked/generated
//     .sh on disk (discover reference clones, dist/build output, tool caches)
//     inflated the count non-deterministically — the same commit produced 427
//     on a machine with reference clones present vs 354 on a clean checkout
//     (issue #151, CHG-2026-06-24-002). Counting only tracked files makes the
//     verdict identical for a given commit on every machine and in CI. This
//     deliberately trades the former "pure Go, no shellout" property for
//     determinism; git is guaranteed present in the gate's runtime context
//     (it runs inside a git repo, in the pre-push hook and CI).
//   - excludedPathSegments remains a secondary filter. trash/ is load-bearing
//     (EC-1: a script git-mv'd to trash/ is still in the index, so git ls-files
//     would list it during the trash→delete window); the rest are defense in
//     depth (already gitignored, so absent from the index anyway).
package scriptcount

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// excludedPathSegments — any tracked path containing one of these directory
// segments is NOT counted. trash/ is load-bearing (EC-1: a script git-mv'd to
// trash/ is still in the index, so git ls-files lists it during the
// trash→delete window). The rest are defense in depth — they are gitignored, so
// already absent from the index — but kept so a future stray `git add -f` of a
// vendored/reference/worktree script cannot silently inflate the count.
var excludedPathSegments = []string{
	"/.claude/worktrees/",
	"/.claude/knowledge-base/references/",
	"/trash/",
	"/node_modules/",
	"/vendor/",
	"/.git/",
	"/_legacy/",
}

// Baseline is the monotonic ceiling persisted at
// infra/quality/script-count-baseline.json.
type Baseline struct {
	CanonicalSH int `json:"canonical_sh"`
}

// ParseBaseline decodes the baseline JSON.
func ParseBaseline(data []byte) (Baseline, error) {
	var b Baseline
	err := json.Unmarshal(data, &b)
	return b, err
}

// CountCanonical returns the number of canonical .sh files tracked by git under
// root — i.e. *.sh in the git index, excluding the directory segments in
// excludedPathSegments. The source is `git ls-files` (the index), NOT the
// on-disk tree, so untracked/gitignored .sh never count (issue #151). Returned
// paths are repo-relative with a leading "/" so the segment match is unambiguous
// (a top-level trash/ matches "/trash/").
func CountCanonical(root string) (int, []string, error) {
	cmd := exec.Command("git", "-C", root, "ls-files", "-z")
	out, err := cmd.Output()
	if err != nil {
		return 0, nil, fmt.Errorf("git ls-files in %q: %w", root, err)
	}
	var matched []string
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" || !strings.HasSuffix(rel, ".sh") {
			continue
		}
		anchored := "/" + filepath.ToSlash(rel)
		excluded := false
		for _, seg := range excludedPathSegments {
			if strings.Contains(anchored, seg) {
				excluded = true
				break
			}
		}
		if !excluded {
			matched = append(matched, anchored)
		}
	}
	return len(matched), matched, nil
}

// Tally is the count compared with the ceiling.
type Tally struct {
	Count    int
	Baseline int
	OK       bool // count <= baseline
	Improved bool // count < baseline (ratchet hint)
}

// Verdict is Pass while the count stays at or under the ceiling, Fail above it.
func (t Tally) Verdict() verdict.Verdict { return verdict.FromPass(t.OK) }

// Check counts canonical .sh under root and compares to the baseline ceiling.
func Check(root string, baseline Baseline) (Tally, error) {
	n, _, err := CountCanonical(root)
	if err != nil {
		return Tally{}, err
	}
	return Tally{
		Count:    n,
		Baseline: baseline.CanonicalSH,
		OK:       n <= baseline.CanonicalSH,
		Improved: n < baseline.CanonicalSH,
	}, nil
}

// Ratcheted reports a request to lower the ceiling to the current count.
type Ratcheted struct {
	Count    int
	Baseline int
	// Refused is set when the count is above the ceiling: a ratchet only moves
	// down, and the baseline was left untouched.
	Refused bool
}

// Verdict is Fail when the ratchet was refused and Pass when the ceiling was
// lowered to the count.
func (r Ratcheted) Verdict() verdict.Verdict { return verdict.FromPass(!r.Refused) }

// Ratchet writes the current count as the new ceiling at path. It refuses to
// raise the ceiling: a count above it is left for the gate to fail.
func Ratchet(path string, t Tally) (Ratcheted, error) {
	r := Ratcheted{Count: t.Count, Baseline: t.Baseline}
	if t.Count > t.Baseline {
		r.Refused = true
		return r, nil
	}
	body := fmt.Sprintf("{\n  \"canonical_sh\": %d\n}\n", t.Count)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil { // #nosec G306 -- repo-internal baseline
		return r, err
	}
	return r, nil
}
