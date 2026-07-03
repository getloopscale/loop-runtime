// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package scriptsaudit produces an objective inventory of shell scripts
// (.sh) in a repository — path, LOC, last-mod time, callers, and proposed
// disposition (KEEP-OPERATOR / DELEGATE-* / MIGRATE-* / AUTO-DIE /
// DELETE-DEAD / REVIEW-NEEDED).
//
// Phase 0 T0.1 of plan docs/plans/completed/2026-05-22-shell-script-strategic-plan.md.
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
	// Claude Code harness artifact: ephemeral agent worktree checkouts (a full
	// repo clone). Auditing it double-counts every script. Untracked, transient.
	"worktrees": true,
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
	data, err := os.ReadFile(path) // #nosec G304 -- audit tool intentionally reads scanned script paths
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
		data, err := os.ReadFile(path) // #nosec G304 G122 -- read-only audit tool; scanned paths are repo files, no TOCTOU mutation
		if err != nil {
			return nil // skip unreadable files (best-effort)
		}
		text := string(data)
		relPath, _ := filepath.Rel(root, path)
		// Indirect references (CI glob-runners, dynamic `source "$DIR/${v}.sh"`)
		// resolve to basename globs; collect them once per file.
		globs := indirectReferenceGlobs(text)
		// For each script, look for `<basename>` or `<stem>` references
		for _, r := range refs {
			// Skip self-references (script referencing itself)
			if relPath == scripts[r.idx].Path {
				continue
			}
			if containsReference(text, r.basename, r.stem) || matchesAnyGlob(r.basename, globs) {
				scripts[r.idx].Callers = append(scripts[r.idx].Callers, relPath)
			}
		}
		return nil
	})
}

// globRefPattern matches a path-glob token ending in .sh that contains a `*`
// wildcard — e.g. `infra/tests/scripts/drill-*_test.sh` in a CI glob-runner.
var globRefPattern = regexp.MustCompile(`[A-Za-z0-9_./*-]*\*[A-Za-z0-9_./*-]*\.sh`)

// dynSourceRefPattern matches a dynamically-interpolated path ending in .sh —
// e.g. `source "$LIB_DIR/cell2-gate-${g}.sh"`. Captures the literal prefix
// (group 1) and suffix (group 2) bracketing the `${var}`/`$var` interpolation.
var dynSourceRefPattern = regexp.MustCompile(`([A-Za-z0-9_.-]+)\$\{?[A-Za-z_][A-Za-z0-9_]*\}?([A-Za-z0-9_.-]*\.sh)`)

// indirectReferenceGlobs scans `text` for glob-runner and dynamic-source
// patterns and returns the set of basename globs they imply (e.g.
// `drill-*_test.sh`, `cell2-gate-*.sh`). A glob with no literal stem char
// (e.g. bare `*.sh`) is dropped — too broad to credit reliably.
func indirectReferenceGlobs(text string) []string {
	seen := map[string]bool{}
	out := []string{}
	add := func(g string) {
		if g == "" || seen[g] {
			return
		}
		// Require ≥2 literal (non-glob, non-suffix) chars to avoid bare `*.sh`.
		stem := strings.TrimSuffix(g, ".sh")
		stem = strings.ReplaceAll(stem, "*", "")
		if len(stem) < 2 {
			return
		}
		seen[g] = true
		out = append(out, g)
	}
	for _, m := range globRefPattern.FindAllString(text, -1) {
		add(filepath.Base(m))
	}
	for _, m := range dynSourceRefPattern.FindAllStringSubmatch(text, -1) {
		// m[1] = literal prefix, m[2] = literal suffix; interpolation → `*`.
		add(filepath.Base(m[1] + "*" + m[2]))
	}
	return out
}

// matchesAnyGlob returns true if basename matches any of the basename globs
// (glob `*` → `[^/]*`, anchored full match).
func matchesAnyGlob(basename string, globs []string) bool {
	for _, g := range globs {
		if globMatchesBasename(g, basename) {
			return true
		}
	}
	return false
}

// globMatchesBasename does an anchored basename match where `*` matches any run
// of non-`/` chars. Reuses path.Match semantics via filepath.Match.
func globMatchesBasename(glob, basename string) bool {
	ok, err := filepath.Match(glob, basename)
	return err == nil && ok
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
	// Dockerfile / Dockerfile.* / *.Dockerfile — container ENTRYPOINT/CMD/COPY
	// referencing an entrypoint .sh is a valid (non-literal-bash) caller.
	if strings.HasPrefix(name, "Dockerfile") || strings.HasSuffix(name, ".Dockerfile") {
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

	// 0. Harness hooks/scripts: registered with the Claude Code harness (settings
	//    + hook events), not invoked by literal basename. KEEP regardless of
	//    caller_count to prevent false DELETE-DEAD (EC false-positive class #4).
	if strings.Contains(s.Path, ".claude/hooks/") || strings.Contains(s.Path, ".claude/scripts/") {
		return "KEEP-OPERATOR"
	}

	// 1. Hermetic test pattern: X_test.sh — AUTO-DIE (D6: morre com pai)
	if strings.HasSuffix(stem, "_test") {
		return "AUTO-DIE"
	}

	// 2. Dead code candidates: zero callers + last-mod > 90d (very conservative)
	if len(s.Callers) == 0 && time.Since(s.LastMod) > 90*24*time.Hour {
		return "DELETE-DEAD"
	}

	// 3. Bare helm wrapper: a REAL `helm upgrade/install` command (line-start,
	//    not inside a comment / echo string / die() message — a frequent
	//    false-positive class for chaos/heal orchestrators that merely print
	//    install instructions).
	if scriptHasRealHelmInvocation(s.Path) {
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

	// 6. Pure-logic gate naming: check-*.sh, validate-*.sh, audit-*.sh — BUT
	//    only if genuinely pure-logic. A gate that shells out to kubectl/helm/
	//    cosign/etc. is NOT pure-logic; porting it to Go would be a forbidden
	//    shell-wrap (parent D5). Those stay bash → KEEP-OPERATOR.
	if strings.HasPrefix(stem, "check-") || strings.HasPrefix(stem, "validate-") || strings.HasPrefix(stem, "audit-") {
		if scriptInvokesExternalCLI(s.Path) {
			return "KEEP-OPERATOR"
		}
		return "MIGRATE-GO-PUREFN"
	}

	// 7. SDK-native candidates: pulumi-*.sh (D5 says use Automation API)
	if strings.HasPrefix(stem, "pulumi-") {
		return "MIGRATE-GO-SDK"
	}

	// 8. Generator pattern: gen-*.sh or generate-*.sh — but only if it's a PURE
	//    file generator. A generate-* that shells out (e.g. injects secrets via
	//    kubectl) is a bootstrap injector → helmfile-delegation slice, not a
	//    hermetic go:generate migration.
	if strings.HasPrefix(stem, "gen-") || strings.HasPrefix(stem, "generate-") {
		if scriptInvokesExternalCLI(s.Path) {
			return "DELEGATE-HELMFILE"
		}
		return "MIGRATE-GENERATOR"
	}

	// 9. Drill / break-glass / operator-time small scripts (< 100 LOC) — KEEP candidate
	if s.LOC < 100 && (strings.Contains(s.Path, "break-glass") || strings.Contains(s.Path, "drill-")) {
		return "KEEP-OPERATOR"
	}

	// Default: needs human review
	return "REVIEW-NEEDED"
}

// externalCLITokens — tools whose invocation makes a script LIVE/effectful,
// disqualifying a pure-logic Go migration (parent D5 shell-wrap prohibition).
var externalCLITokens = regexp.MustCompile(`\b(kubectl|helm|helmfile|cosign|argocd|doctl|pulumi|terraform)\b`)

// scriptInvokesExternalCLI returns true if the script body invokes any of the
// effectful external CLIs. Best-effort: an unreadable file returns false (the
// caller's prefix heuristic still applies).
func scriptInvokesExternalCLI(path string) bool {
	data, err := os.ReadFile(path) // #nosec G304 -- audit tool intentionally reads scanned script paths
	if err != nil {
		return false
	}
	return externalCLITokens.Match(data)
}

// helmInvocationLine matches a REAL helm upgrade/install command at the start
// of a line (ignoring leading whitespace). It deliberately does NOT match
// `helm install` embedded mid-line (echo strings, die() messages) — those are
// instructions, not invocations.
var helmInvocationLine = regexp.MustCompile(`^\s*helm\s+(upgrade|install)\b`)

// scriptHasRealHelmInvocation returns true if the script body contains a real
// `helm upgrade/install` command — a non-comment line whose first token is
// `helm`. Comment lines (`#…`) and string-embedded mentions are skipped.
func scriptHasRealHelmInvocation(path string) bool {
	data, err := os.ReadFile(path) // #nosec G304 -- audit tool intentionally reads scanned script paths
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if helmInvocationLine.MatchString(line) {
			return true
		}
	}
	return false
}

// scriptContains reads the script file and returns true if it contains
// ANY of the given substrings.
func scriptContains(path string, needles ...string) bool {
	data, err := os.ReadFile(path) // #nosec G304 -- audit tool intentionally reads scanned script paths
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
