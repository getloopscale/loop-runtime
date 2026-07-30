// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package danglingdata is the mirror of cmd/loopzero-ops/internal/quality/danglingrefs. Where
// danglingrefs catches a DELETED SCRIPT still referenced by a live caller, this
// gate catches a DELETED DATA FILE (config/catalog .yaml/.yml/.json) still
// referenced by a live executable caller (.sh script or workflow/Taskfile YAML).
//
// Both close the same integration-boundary failure class:
//   - develop-bkp (2026-05-23): deleted scripts, callers not rewired.
//   - governance subsystem (2026-05-23): the 2026-05-11 consolidation deleted
//     the maturity-scoring DATA (docs/governance/, docs/evidence/, ...) but left
//     the scripts that read it dangling — verify-independence always exit 2,
//     health-check-governance always exit 1. See
//     finding-2026-05-23-governance-subsystem-orphaned.md.
//
// Scope is deliberately TIGHT to keep false positives near zero:
//   - Deleted side: only .yaml/.yml/.json, and NOT under code/chart/fixture
//     trees (infra/helm, charts, */fixtures, */testdata, */migrations, api,
//     operators, cli, pkg, cmd) where data deletion is routine + self-contained
//     (consumed via globs/frameworks, not hardcoded script paths).
//   - Caller side: only EXECUTABLE orchestration — .sh anywhere, or .yml/.yaml
//     under .github/workflows / taskfiles / Taskfile.yml. Prose (.md) and
//     ordinary manifests/values are NOT callers.
//   - Reference must be live (non-comment, non-provenance).
package danglingdata

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Config tells Check where to operate.
type Config struct {
	RepoRoot string
	BaseRef  string
	HeadRef  string
}

// Outcome captures the result of a Check invocation.
type Outcome struct {
	Pass       bool
	Violations []string
}

// dataExtensions are the deleted-file types worth checking (config/data inputs
// a script might hardcode). .md is excluded — prose deletion is routine and
// danglingrefs already treats .md as non-caller.
var dataExtensions = []string{".yaml", ".yml", ".json"}

// deletedExcludedPrefixes: deletions under these trees are routine + self-
// contained (Helm charts, code, generated). A script wouldn't hardcode them as
// a config input; they're consumed by frameworks via globs.
var deletedExcludedPrefixes = []string{
	"infra/helm/", "charts/", "api/", "operators/", "cli/", "pkg/", "cmd/",
	"vendor/", "node_modules/",
}

// deletedExcludedFragments: path fragments marking fixture/test/generated data.
var deletedExcludedFragments = []string{
	"/fixtures/", "/testdata/", "/snapshots/", "/__tests__/", "/test_helpers/",
	"/migrations/",
}

// callerSurfaces are scanned for live references to a deleted data file.
var callerSurfaces = []string{
	"Taskfile.yml", "taskfiles", ".github/workflows", "infra", "scripts", "tools",
}

// provenanceMarkers identify documentation/comment lines (not live references).
var provenanceMarkers = []string{
	"migrated from", "migrated to", "replaced by", "per plan",
	"superseded", "was:", "deleted", "removed",
}

// Check is the entry point for `loopzero-ops quality dangling-data-refs`.
// Error is reserved for INFRASTRUCTURE failures (git missing, base-ref
// unreachable); dangling references come back via Outcome.Violations.
func Check(cfg Config) (*Outcome, error) {
	if err := verifyRefReachable(cfg.RepoRoot, cfg.BaseRef); err != nil {
		return nil, err
	}
	deleted, err := gitDiffDeletedData(cfg.RepoRoot, cfg.BaseRef, cfg.HeadRef)
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}

	outcome := &Outcome{Pass: true}
	for _, delPath := range deleted {
		refs, err := liveCallerReferences(cfg.RepoRoot, delPath)
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			outcome.Pass = false
			outcome.Violations = append(outcome.Violations,
				fmt.Sprintf("deleted data file %q still referenced (live) in %s", delPath, ref))
		}
	}
	return outcome, nil
}

// verifyRefReachable mirrors the nonewbash/danglingrefs EC-04 guard.
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

// gitDiffDeletedData returns deleted (D) or renamed-away data files that are in
// scope (data extension, not under an excluded tree).
func gitDiffDeletedData(repoRoot, base, head string) ([]string, error) {
	// #nosec G204 -- base/head are CI-controlled git refs, not user input.
	cmd := exec.Command("git", "diff", "-M", "--name-status", base+"..."+head)
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
		var delPath string
		switch {
		case status == "D":
			delPath = fields[1]
		case strings.HasPrefix(status, "R"):
			delPath = fields[1] // OLD path of a rename is gone
		default:
			continue
		}
		if isDataInScope(delPath) {
			paths = append(paths, delPath)
		}
	}
	return paths, nil
}

// isDataInScope reports whether a deleted path is a config/data file outside the
// routine-churn trees.
func isDataInScope(p string) bool {
	hasDataExt := false
	for _, ext := range dataExtensions {
		if strings.HasSuffix(p, ext) {
			hasDataExt = true
			break
		}
	}
	if !hasDataExt {
		return false
	}
	for _, pre := range deletedExcludedPrefixes {
		if strings.HasPrefix(p, pre) {
			return false
		}
	}
	for _, frag := range deletedExcludedFragments {
		if strings.Contains(p, frag) {
			return false
		}
	}
	return true
}

// liveCallerReferences greps caller surfaces for live references to the deleted
// data path. Returns "file:line: text" hits.
func liveCallerReferences(repoRoot, delPath string) ([]string, error) {
	dirBase := dirBaseForm(delPath)
	var hits []string
	for _, surface := range callerSurfaces {
		full := filepath.Join(repoRoot, surface)
		info, err := os.Stat(full)
		if err != nil {
			continue // surface absent — fine
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
			if !isCallerFile(repoRoot, f) {
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

// isCallerFile reports whether a file is executable orchestration: any .sh, or a
// .yml/.yaml under .github/workflows / taskfiles, or a Taskfile.yml. Ordinary
// manifests/values and .md prose are NOT callers.
func isCallerFile(repoRoot, abs string) bool {
	rel, err := filepath.Rel(repoRoot, abs)
	if err != nil {
		rel = abs
	}
	if strings.HasSuffix(rel, ".sh") {
		return true
	}
	if strings.HasSuffix(rel, ".yml") || strings.HasSuffix(rel, ".yaml") {
		base := filepath.Base(rel)
		return strings.Contains(rel, ".github/workflows/") ||
			strings.HasPrefix(rel, "taskfiles/") ||
			strings.Contains(rel, "/taskfiles/") ||
			base == "Taskfile.yml"
	}
	return false
}

// dirBaseForm returns the `<parentdir>/<basename>` suffix (carries the parent
// dir, so it never collides with a same-basename file elsewhere).
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
			return nil // skip dirs + symlinks
		}
		files = append(files, path)
		return nil
	})
	return files, err
}

// scanFile returns live-reference hits in one caller file for the deleted path.
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
		hits = append(hits, fmt.Sprintf("%s:%d: %s", rel, lineNo, strings.TrimSpace(line)))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return hits, nil
}

// containsPathForm reports whether the line references the deleted file in
// full-path or `<dir>/<base>` form. dirBase carries the parent dir, guarding
// against bare-basename collisions.
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
