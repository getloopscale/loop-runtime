// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package deletedrefs is the engine behind the dangling-reference gates: find
// the files a diff deleted (or renamed away) and the live references to them
// that survive in caller surfaces.
//
// Both gates that use it close the same integration-boundary failure — a file
// is deleted faster than its callers are rewired — and differ only in data:
// which deleted files are in scope, which files count as callers, which lines
// are provenance rather than use. That data is a Spec; the mechanism lives
// here once.
//
// A reference is matched by its full path or by its `<parentdir>/<basename>`
// form, never by bare basename, so a deleted `dr/preflight.sh` does not collide
// with a surviving `tests/parity/preflight.sh`.
package deletedrefs

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/revrange"
)

// Spec is what a dangling-reference gate looks for.
type Spec struct {
	// Noun names a deleted file in messages ("script", "data file").
	Noun string
	// DiffPathspec restricts the diff (e.g. "*.sh"); empty means the whole tree.
	DiffPathspec []string
	// InScope filters deleted paths; nil keeps every one.
	InScope func(deletedPath string) bool
	// Surfaces are repo-relative files or directories searched for callers.
	Surfaces []string
	// IsCaller decides whether a file (repo-relative) under a surface is a caller.
	IsCaller func(relPath string) bool
	// ProvenanceMarkers mark lines that document a migration rather than use it.
	ProvenanceMarkers []string
	// LiveContext, when set, must match a line for it to count as a use.
	LiveContext *regexp.Regexp
}

// Violations returns one message per live reference to a file deleted between
// base and head. An unreachable base ref is an error, never an empty diff.
func Violations(spec Spec, repoRoot, base, head string) ([]string, error) {
	if err := revrange.VerifyReachable(repoRoot, base); err != nil {
		return nil, err
	}
	deleted, err := deletedPaths(spec, repoRoot, base, head)
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	var out []string
	for _, del := range deleted {
		refs, err := liveReferences(spec, repoRoot, del)
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			out = append(out, fmt.Sprintf("deleted %s %q still referenced (live) in %s", spec.Noun, del, ref))
		}
	}
	return out, nil
}

// deletedPaths returns the paths deleted (D) or renamed away (the OLD path of
// an R) in base...head, filtered by the spec's scope.
func deletedPaths(spec Spec, repoRoot, base, head string) ([]string, error) {
	args := []string{"diff", "-M", "--name-status", base + "..." + head}
	if len(spec.DiffPathspec) > 0 {
		args = append(append(args, "--"), spec.DiffPathspec...)
	}
	// #nosec G204 -- base/head are CI-controlled git refs, not user input.
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git diff failed: %w\n%s", err, out)
	}
	var paths []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if status := fields[0]; status != "D" && !strings.HasPrefix(status, "R") {
			continue
		}
		if spec.InScope == nil || spec.InScope(fields[1]) {
			paths = append(paths, fields[1])
		}
	}
	return paths, nil
}

// liveReferences returns "file:line: text" hits for del across the surfaces.
func liveReferences(spec Spec, repoRoot, del string) ([]string, error) {
	dirBase := dirBaseForm(del)
	var hits []string
	for _, surface := range spec.Surfaces {
		files, err := surfaceFiles(filepath.Join(repoRoot, surface))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			rel, err := filepath.Rel(repoRoot, f)
			if err != nil {
				return nil, err
			}
			if !spec.IsCaller(filepath.ToSlash(rel)) {
				continue
			}
			fileHits, err := scanFile(spec, f, rel, del, dirBase)
			if err != nil {
				return nil, err
			}
			hits = append(hits, fileHits...)
		}
	}
	return hits, nil
}

// surfaceFiles lists the regular files of a surface; an absent surface has none.
func surfaceFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	var files []string
	err = filepath.Walk(path, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.Mode().IsRegular() { // dirs and symlinks are skipped
			files = append(files, p)
		}
		return nil
	})
	return files, err
}

func scanFile(spec Spec, absFile, rel, fullPath, dirBase string) ([]string, error) {
	f, err := os.Open(absFile) // #nosec G304 -- walked from fixed surfaces
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var hits []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for lineNo := 1; sc.Scan(); lineNo++ {
		line := sc.Text()
		if !strings.Contains(line, fullPath) && !strings.Contains(line, dirBase) {
			continue
		}
		if isProvenanceOrComment(line, spec.ProvenanceMarkers) {
			continue
		}
		if spec.LiveContext != nil && !spec.LiveContext.MatchString(line) {
			continue
		}
		hits = append(hits, fmt.Sprintf("%s:%d: %s", rel, lineNo, strings.TrimSpace(line)))
	}
	return hits, sc.Err()
}

// dirBaseForm returns the `<parentdir>/<basename>` suffix of p.
func dirBaseForm(p string) string {
	dir := filepath.Base(filepath.Dir(p))
	base := filepath.Base(p)
	if dir == "" || dir == "." {
		return base
	}
	return dir + "/" + base
}

// isProvenanceOrComment excludes pure comments and migration-provenance lines.
func isProvenanceOrComment(line string, markers []string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
		return true
	}
	lower := strings.ToLower(trimmed)
	for _, m := range markers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}
