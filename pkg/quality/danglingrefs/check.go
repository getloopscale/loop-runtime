// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package danglingrefs implements the T0.1 backstop gate from
// docs/plans/2026-05-23-incremental-bash-to-go-migration-plan.md.
//
// It fails when a .sh script DELETED (or renamed-away) in the diff is still
// referenced — in a live, non-comment context — by any caller surface:
// Taskfile.yml, taskfiles/, .github/workflows/, infra/, .claude/skills/.
//
// This is the automated backstop against the failure that abandoned the
// develop-bkp big-bang: scripts were deleted faster than their callers were
// rewired, and unit-test-green masked the broken drill/cleanroom/chaos SKILLs.
//
// Design notes:
//   - Basename-collision guard: a deleted `dr/preflight.sh` must NOT match a
//     surviving `tests/parity/preflight.sh`. We match the full path AND the
//     `<parentdir>/<basename>` suffix form the SKILLs use — both carry the
//     parent dir, so unrelated same-basename files never collide.
//   - Comment/provenance exclusion: lines that are pure comments or carry
//     "Migrated from"/"replaced by"/"per plan" provenance are not "live".
//   - EC-04 parity with nonewbash: base-ref unreachable fails LOUD.
package danglingrefs

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Config tells Check where to operate.
type Config struct {
	RepoRoot string // absolute path to repo root
	BaseRef  string // base git ref (e.g., "origin/develop")
	HeadRef  string // head git ref (e.g., "HEAD")
}

// Outcome captures the result of a Check invocation.
type Outcome struct {
	Pass       bool
	Violations []string
}

// searchSurfaces are the caller surfaces scanned for dangling references,
// relative to RepoRoot. Files outside these are ignored.
var searchSurfaces = []string{
	"Taskfile.yml",
	"taskfiles",
	".github/workflows",
	"infra",
	".claude/skills",
}

// excludedPathFragments are files that legitimately NAME a deleted script
// without being a live caller (the allowlist, the audit CSV).
var excludedPathFragments = []string{
	"shell-scripts-allowlist.md",
	"scripts-audit",
}

// provenanceMarkers identify comment lines that document a migration rather
// than invoke the script.
var provenanceMarkers = []string{
	"migrated from", "migrated to", "replaced by", "per plan",
	"superseded", "see d", "lesson",
}

// Check is the entry point for `loopzero-ops quality dangling-script-refs`.
// Error is reserved for INFRASTRUCTURE failures (git missing, base-ref
// unreachable); dangling references come back via Outcome.Violations.
func Check(cfg Config) (*Outcome, error) {
	if err := verifyRefReachable(cfg.RepoRoot, cfg.BaseRef); err != nil {
		return nil, err
	}
	deleted, err := gitDiffDeleted(cfg.RepoRoot, cfg.BaseRef, cfg.HeadRef)
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}

	outcome := &Outcome{Pass: true}
	for _, delPath := range deleted {
		refs, err := liveReferences(cfg.RepoRoot, delPath)
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			outcome.Pass = false
			outcome.Violations = append(outcome.Violations,
				fmt.Sprintf("deleted script %q still referenced (live) in %s", delPath, ref))
		}
	}
	return outcome, nil
}

// verifyRefReachable mirrors the nonewbash EC-04 guard.
func verifyRefReachable(repoRoot, ref string) error {
	// #nosec G204 -- ref is a CI-controlled git ref, not user input.
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	cmd.Dir = repoRoot
	if err := cmd.Run(); err != nil {
		return fmt.Errorf(
			"BASE_REF_UNREACHABLE: base ref %q not found in local git history. "+
				"Likely a shallow clone (default GitHub Actions depth=1). "+
				"Fix: set `fetch-depth: 0` on actions/checkout, OR `git fetch origin %s`. Underlying: %v",
			ref, ref, err)
	}
	return nil
}

// gitDiffDeleted returns .sh paths that were deleted (D) or renamed-away (the
// OLD path of an R). Rename detection is enabled (-M) so a moved script's old
// path is reported and its lingering references caught.
func gitDiffDeleted(repoRoot, base, head string) ([]string, error) {
	// #nosec G204 -- base/head are CI-controlled git refs, not user input.
	cmd := exec.Command("git", "diff", "-M", "--name-status", base+"..."+head, "--", "*.sh")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git diff failed: %w\n%s", err, out)
	}
	var paths []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		status := fields[0]
		switch {
		case status == "D":
			paths = append(paths, fields[1])
		case strings.HasPrefix(status, "R"):
			// R<score> <old> <new> — the OLD path (fields[1]) is gone.
			paths = append(paths, fields[1])
		}
	}
	return paths, nil
}

// liveReferences greps the search surfaces for live (non-comment) references to
// the deleted path. Returns "file:line: text" hits.
func liveReferences(repoRoot, delPath string) ([]string, error) {
	dirBase := dirBaseForm(delPath)
	var hits []string
	for _, surface := range searchSurfaces {
		full := filepath.Join(repoRoot, surface)
		info, err := os.Stat(full)
		if err != nil {
			continue // surface absent in this repo — fine
		}
		var files []string
		if info.IsDir() {
			files, err = walkFiles(full)
			if err != nil {
				return nil, err
			}
		} else {
			files = []string{full}
		}
		for _, f := range files {
			if isExcludedFile(f) {
				continue
			}
			fileHits, err := scanFile(repoRoot, f, delPath, dirBase)
			if err != nil {
				return nil, err
			}
			hits = append(hits, fileHits...)
		}
	}
	return hits, nil
}

// dirBaseForm returns the `<parentdir>/<basename>` suffix used by SKILL docs
// (e.g. "dr/preflight.sh" for "infra/scripts/dr/preflight.sh"). This carries
// the parent dir, so it never collides with a same-basename file elsewhere.
func dirBaseForm(p string) string {
	dir := filepath.Base(filepath.Dir(p))
	base := filepath.Base(p)
	if dir == "" || dir == "." {
		return base
	}
	return dir + "/" + base
}

func walkFiles(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil // skip dirs + symlinks (Walk does not follow symlinks)
		}
		files = append(files, path)
		return nil
	})
	return files, err
}

func isExcludedFile(path string) bool {
	lower := strings.ToLower(path)
	for _, frag := range excludedPathFragments {
		if strings.Contains(lower, frag) {
			return true
		}
	}
	// CSV/TXT are data/baseline/documentation files, not live callers.
	if strings.HasSuffix(lower, ".csv") || strings.HasSuffix(lower, ".txt") {
		return true
	}
	// Markdown prose (knowledge-base, plans, audits) is not a live caller.
	// EXCEPTION: SKILL.md files are operator instructions — a SKILL pointing
	// at a deleted script IS a live break, so those are scanned.
	if strings.HasSuffix(lower, ".md") && !strings.Contains(lower, "/skills/") {
		return true
	}
	return false
}

// liveCallContext matches lines that INVOKE/REFERENCE a script in an
// executable or operator-instruction context. Markdown ref lines in SKILLs
// (backticked paths, list items) also count — a SKILL must never point at a
// deleted file.
var liveCallContext = regexp.MustCompile(`(?i)(\bbash\b|\bsource\b|^\s*\.\s|run:|\bexec\b|script=|/tmp/loopzero-ops|` + "`" + `|^\s*-\s|\.sh)`)

// scanFile returns live-reference hits in one file for the given deleted path.
func scanFile(repoRoot, absFile, fullPath, dirBase string) ([]string, error) {
	// #nosec G304 -- absFile comes from our own filepath.Walk of fixed surfaces.
	data, err := os.ReadFile(absFile)
	if err != nil {
		return nil, err
	}
	rel, _ := filepath.Rel(repoRoot, absFile)
	var hits []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if !containsPathForm(line, fullPath, dirBase) {
			continue
		}
		if isProvenanceOrComment(line) {
			continue
		}
		if !liveCallContext.MatchString(line) {
			continue
		}
		hits = append(hits, fmt.Sprintf("%s:%d: %s", rel, lineNo, strings.TrimSpace(line)))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return hits, nil
}

// containsPathForm reports whether the line references the deleted script in
// either full-path or `<dir>/<base>` form. Both forms carry the parent dir,
// guarding against bare-basename collisions.
func containsPathForm(line, fullPath, dirBase string) bool {
	return strings.Contains(line, fullPath) || strings.Contains(line, dirBase)
}

// isProvenanceOrComment excludes pure comments and migration-provenance lines.
func isProvenanceOrComment(line string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
		return true
	}
	lower := strings.ToLower(trimmed)
	for _, marker := range provenanceMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
