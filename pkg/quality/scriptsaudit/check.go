// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package scriptsaudit produces an objective inventory of shell scripts
// (.sh) in a repository — path, LOC, last-mod time, callers, and proposed
// disposition (KEEP-OPERATOR / DELEGATE-* / MIGRATE-* / AUTO-DIE /
// DELETE-DEAD / REVIEW-NEEDED).
//
// Phase 0 T0.1 of plan docs/plans/2026-05-22-shell-script-strategic-plan.md.
// The CSV emitted by this package is reviewed by an operator and drives
// downstream phases (delete, delegate, migrate).
//
// Design constraints (per plan):
//   - Pure Go (no shellout): regexp + filepath.WalkDir + encoding/csv
//   - Symlink-safe (EC-07): dedup via filepath.EvalSymlinks
//   - Deterministic: CSV rows sorted by path
//   - Excludes vendor/, node_modules/, .git/, _legacy/ from scan
package scriptsaudit

import (
	"encoding/csv"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Script captures one shell script with audit metadata.
type Script struct {
	Path        string    // path relative to repo root (canonical, post-EvalSymlinks)
	LOC         int       // line count (wc -l semantics: number of \n in file)
	LastMod     time.Time // last modification time (UTC)
	Callers     []string  // file paths that reference this script
	Disposition string    // proposal: KEEP-OPERATOR / DELEGATE-* / MIGRATE-* / etc.
}

// excludedDirs — directory basenames that ScanRepo skips entirely.
// Rationale: vendored / third-party / git internals never contain scripts
// we govern. _legacy is by definition out-of-scope.
var excludedDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	".git":         true,
	"_legacy":      true,
	"dist":         true,
	"build":        true,
	".idea":        true,
	".vscode":      true,
}

// callerSearchExtensions — file extensions ScanRepo searches when detecting
// callers. Matches the plan's "Caller detection regex" deep dive.
var callerSearchExtensions = map[string]bool{
	".yml":    true,
	".yaml":   true,
	".sh":     true,
	".gotmpl": true,
}

// callerSearchFilenames — exact filenames (no extension) also searched.
var callerSearchFilenames = map[string]bool{
	"Taskfile.yml": true,
	"Tiltfile":     true,
}

// ScanRepo walks the repo root and returns a sorted []Script with
// {Path, LOC, LastMod, Callers, Disposition} populated for every .sh
// found outside excludedDirs.
//
// Symlinks are dereferenced via filepath.EvalSymlinks; if multiple paths
// point to the same canonical file, only the first encountered is kept
// (EC-07: no double-counting).
func ScanRepo(root string) ([]Script, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("abs root: %w", err)
	}

	// Pass 1: collect all .sh files (deduplicated by canonical path)
	canonicalPaths := map[string]string{} // canonical → display path
	displayOrder := []string{}            // preserve first-seen display path

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if excludedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".sh") {
			return nil
		}
		// Resolve symlinks for dedup (EC-07).
		canonical, evalErr := filepath.EvalSymlinks(path)
		if evalErr != nil {
			// If EvalSymlinks fails (broken symlink, permission), fall back
			// to the original path — better to count something than skip silently.
			canonical = path
		}
		// Skip if canonical was already seen
		displayPath, _ := filepath.Rel(root, path)
		if _, exists := canonicalPaths[canonical]; exists {
			return nil
		}
		canonicalPaths[canonical] = displayPath
		displayOrder = append(displayOrder, displayPath)
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk %s: %w", root, walkErr)
	}

	// Pass 2: build Script entries with LOC + LastMod
	scripts := make([]Script, 0, len(displayOrder))
	for _, displayPath := range displayOrder {
		fullPath := filepath.Join(root, displayPath)
		info, err := os.Stat(fullPath)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", fullPath, err)
		}
		loc, err := countLines(fullPath)
		if err != nil {
			return nil, fmt.Errorf("count lines %s: %w", fullPath, err)
		}
		scripts = append(scripts, Script{
			Path:    displayPath,
			LOC:     loc,
			LastMod: info.ModTime().UTC(),
		})
	}

	// Pass 3: detect callers (this is O(scripts × searchFiles))
	if err := populateCallers(root, scripts); err != nil {
		return nil, fmt.Errorf("populate callers: %w", err)
	}

	// Pass 4: propose disposition per script
	for i := range scripts {
		scripts[i].Disposition = ProposeDisposition(scripts[i])
	}

	// Deterministic order
	sort.Slice(scripts, func(i, j int) bool {
		return scripts[i].Path < scripts[j].Path
	})
	return scripts, nil
}

// countLines reads a file and counts newline-terminated lines. Empty file
// returns 0; file without trailing newline still counts its last line.
func countLines(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return 0, nil
	}
	count := 0
	for _, b := range data {
		if b == '\n' {
			count++
		}
	}
	// If file doesn't end with \n, the last line is still real
	if data[len(data)-1] != '\n' {
		count++
	}
	return count, nil
}

// populateCallers searches all candidate files under root for references
// to each script's basename. Populates Script.Callers in place.
//
// Pattern matched: `<basename-without-ext>(?:\.sh)?\b` — i.e., references
// can omit the .sh suffix (matches `bash mytool` as well as `bash mytool.sh`).
//
// NOTE: Dynamic references like `bash "$VAR"` are NOT detected here (regex
// limitation). EC-03 of plan: T1.1.a operator validation handles those.
func populateCallers(root string, scripts []Script) error {
	if len(scripts) == 0 {
		return nil
	}

	// Build lookup: basename → script index
	type scriptRef struct {
		basename string
		stem     string // basename without .sh
		idx      int
	}
	refs := make([]scriptRef, len(scripts))
	for i, s := range scripts {
		base := filepath.Base(s.Path)
		stem := strings.TrimSuffix(base, ".sh")
		refs[i] = scriptRef{basename: base, stem: stem, idx: i}
	}

	// Walk and search every candidate file
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if excludedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !isCallerSearchFile(d.Name()) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil // skip unreadable files (best-effort)
		}
		text := string(data)
		relPath, _ := filepath.Rel(root, path)
		// For each script, look for `<basename>` or `<stem>` references
		for _, r := range refs {
			// Skip self-references (script referencing itself)
			if relPath == scripts[r.idx].Path {
				continue
			}
			if containsReference(text, r.basename, r.stem) {
				scripts[r.idx].Callers = append(scripts[r.idx].Callers, relPath)
			}
		}
		return nil
	})
}

// isCallerSearchFile decides whether a file is searched for caller patterns.
func isCallerSearchFile(name string) bool {
	if callerSearchFilenames[name] {
		return true
	}
	for ext := range callerSearchExtensions {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	// Special: Tiltfile, Tiltfile.local
	if strings.HasPrefix(name, "Tiltfile") {
		return true
	}
	return false
}

// referenceWordChar matches a regex "word character" boundary (alphanumeric or _).
// Used to confirm that a basename appears as a STANDALONE token, not embedded
// in a longer identifier.
var referenceWordChar = regexp.MustCompile(`[A-Za-z0-9_]`)

// containsReference returns true if `text` mentions either the full
// `basename` (e.g., "foo.sh") OR the `stem` (e.g., "foo") as a standalone
// token. A standalone token is preceded/followed by a non-word char (or EOF).
func containsReference(text, basename, stem string) bool {
	if isolatedTokenPresent(text, basename) {
		return true
	}
	// Stem-only matching is risky (e.g., "common" matches many things);
	// require it to be preceded by /, space, quote, or backtick AND followed
	// by non-word char to qualify.
	return isolatedStemPresent(text, stem)
}

func isolatedTokenPresent(text, token string) bool {
	for i := 0; ; {
		idx := strings.Index(text[i:], token)
		if idx < 0 {
			return false
		}
		start := i + idx
		end := start + len(token)
		// Check that surrounding chars are NOT word chars (would make this a substring match).
		// Special case: end of file or word boundary is OK.
		prevOK := start == 0 || !referenceWordChar.MatchString(string(text[start-1]))
		nextOK := end == len(text) || !referenceWordChar.MatchString(string(text[end]))
		if prevOK && nextOK {
			return true
		}
		i = end
	}
}

// isolatedStemPresent is stricter than isolatedTokenPresent: it requires
// the stem to be preceded by `/`, space, quote, or backtick to reduce false positives.
func isolatedStemPresent(text, stem string) bool {
	if len(stem) < 4 {
		return false // skip very short stems (e.g., "a") to avoid noise
	}
	prefixes := []string{"/", " ", "\"", "'", "`", "\t"}
	for i := 0; ; {
		idx := strings.Index(text[i:], stem)
		if idx < 0 {
			return false
		}
		start := i + idx
		end := start + len(stem)
		// Require explicit prefix (not just non-word char) to qualify.
		hasPrefix := start == 0
		if !hasPrefix && start > 0 {
			prev := string(text[start-1])
			for _, p := range prefixes {
				if prev == p {
					hasPrefix = true
					break
				}
			}
		}
		nextOK := end == len(text) || !referenceWordChar.MatchString(string(text[end]))
		if hasPrefix && nextOK {
			return true
		}
		i = end
	}
}

// ProposeDisposition heuristically classifies a script based on its
// {Path, LOC, LastMod, Callers}. Returns one of the canonical disposition codes:
//
//	KEEP-OPERATOR | DELEGATE-HELMFILE | DELEGATE-TILT | DELEGATE-ARGOCD |
//	MIGRATE-GO-PUREFN | MIGRATE-GO-SDK | MIGRATE-GINKGO | MIGRATE-GENERATOR |
//	AUTO-DIE | DELETE-DEAD | REVIEW-NEEDED
//
// Order of evaluation matters: more specific patterns win.
func ProposeDisposition(s Script) string {
	base := filepath.Base(s.Path)
	stem := strings.TrimSuffix(base, ".sh")

	// 1. Hermetic test pattern: X_test.sh — AUTO-DIE (D6: morre com pai)
	if strings.HasSuffix(stem, "_test") {
		return "AUTO-DIE"
	}

	// 2. Dead code candidates: zero callers + last-mod > 90d (very conservative)
	if len(s.Callers) == 0 && time.Since(s.LastMod) > 90*24*time.Hour {
		return "DELETE-DEAD"
	}

	// 3. Bare helm wrapper: contains `helm upgrade` or `helm install`
	if scriptContains(s.Path, "helm upgrade", "helm install ") {
		return "DELEGATE-HELMFILE"
	}

	// 4. Bare kubectl apply chains for tenant-day-2 → DELEGATE-ARGOCD
	if scriptContains(s.Path, "kubectl apply -f infra/argocd", "argocd app sync") {
		return "DELEGATE-ARGOCD"
	}

	// 5. Ginkgo migration candidates: test orchestrators in parity/dogfood
	//    (must come BEFORE generic check-/validate-/audit- heuristic, because
	//    validate-matrix.sh em parity/ should go to Ginkgo, not pure-fn migration)
	if strings.Contains(s.Path, "infra/tests/parity/") || strings.Contains(s.Path, "infra/tests/dogfood/") {
		return "MIGRATE-GINKGO"
	}

	// 6. Pure-logic gate naming: check-*.sh, validate-*.sh, audit-*.sh
	if strings.HasPrefix(stem, "check-") || strings.HasPrefix(stem, "validate-") || strings.HasPrefix(stem, "audit-") {
		return "MIGRATE-GO-PUREFN"
	}

	// 7. SDK-native candidates: pulumi-*.sh (D5 says use Automation API)
	if strings.HasPrefix(stem, "pulumi-") {
		return "MIGRATE-GO-SDK"
	}

	// 8. Generator pattern: gen-*.sh or generate-*.sh
	if strings.HasPrefix(stem, "gen-") || strings.HasPrefix(stem, "generate-") {
		return "MIGRATE-GENERATOR"
	}

	// 9. Drill / break-glass / operator-time small scripts (< 100 LOC) — KEEP candidate
	if s.LOC < 100 && (strings.Contains(s.Path, "break-glass") || strings.Contains(s.Path, "drill-")) {
		return "KEEP-OPERATOR"
	}

	// Default: needs human review
	return "REVIEW-NEEDED"
}

// scriptContains reads the script file and returns true if it contains
// ANY of the given substrings.
func scriptContains(path string, needles ...string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	text := string(data)
	for _, n := range needles {
		if strings.Contains(text, n) {
			return true
		}
	}
	return false
}

// EmitCSV writes scripts to w in canonical CSV format:
//
//	path,loc,last_mod_utc,caller_count,disposition,callers
//
// Callers are joined by `;` in the last column. Output is deterministic
// (sorted by path; scripts assumed already sorted).
func EmitCSV(scripts []Script, w io.Writer) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()

	header := []string{"path", "loc", "last_mod_utc", "caller_count", "disposition", "callers"}
	if err := cw.Write(header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	// Sort by path (defensive — input may not be sorted)
	sorted := make([]Script, len(scripts))
	copy(sorted, scripts)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	for _, s := range sorted {
		callersSorted := make([]string, len(s.Callers))
		copy(callersSorted, s.Callers)
		sort.Strings(callersSorted)
		row := []string{
			s.Path,
			strconv.Itoa(s.LOC),
			s.LastMod.Format(time.RFC3339),
			strconv.Itoa(len(s.Callers)),
			s.Disposition,
			strings.Join(callersSorted, ";"),
		}
		if err := cw.Write(row); err != nil {
			return fmt.Errorf("write row %s: %w", s.Path, err)
		}
	}
	cw.Flush()
	return cw.Error()
}
