// Package shellcheckadvisory runs shellcheck across the operator/CI script
// set as an advisory-only quality gate.
//
// Conditional: runs shellcheck when the tool is installed on PATH; emits
// WARN + exits 0 when absent so first-landing doesn't break unrelated CI
// flows. Once shellcheck is universally installed across dev + CI, the
// PATH guard can be dropped and this can graduate to a hard gate.
//
// T4.1 of plan 2026-06-15-code-review-49-findings-remediation (R1-F5 HIGH).
//
// Migrated from infra/scripts/local/shellcheck-advisory.sh (M-Migrate2
// continuation 2026-07-07). Retains the `shellcheck` subprocess invocation
// (Anti-Pattern #8 tradeoff explicitly accepted — shellcheck is the only
// tool that reads shell scripts semantically; a Go re-impl would be a
// re-invention). What the Go port BUYS: a stable typed exit contract +
// the ability to inject fixture repo roots for hermetic tests.
package shellcheckadvisory

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// excludeCodes are the shellcheck codes intentionally suppressed today.
//
//	SC1091 — Not following sourced files (dynamic source paths in dogfood wrappers).
//	SC2155 — Declare and assign separately (style; many existing files use combined).
//	SC2086 — Double-quote to prevent globbing (intentional in some operator scripts).
//	SC2046 — Quote to prevent word splitting (intentional in some operator scripts).
var excludeCodes = []string{"SC1091", "SC2155", "SC2086", "SC2046"}

// scanGlobs mirrors the bash script's three globs — expanded to real file
// lists at Check() time so shellcheck receives a fixed argv (no shell
// glob semantics).
var scanGlobs = []struct {
	dir     string
	pattern string
}{
	{"infra/scripts/local", `*.sh`},
	{"infra/tests", `check-*-syntax_test.sh`},
	{"infra/tests/e2e", `dev-public-*.sh`},
}

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome is the summary.
type Outcome struct {
	// Warn is true when shellcheck is not installed (advisory-only fallthrough).
	Warn bool
	// ScannedFiles is the count of files handed to shellcheck.
	ScannedFiles int
	// Diagnostic is the shellcheck stdout+stderr when it emitted violations.
	Diagnostic string
}

// ErrViolations means shellcheck flagged at least one violation.
var ErrViolations = errors.New("shellcheck emitted violations across operator/CI script set")

// Check runs the shellcheck pass with the same tolerated codes as the bash
// script. When shellcheck is not installed, returns nil error with
// Outcome.Warn=true (advisory-only fallthrough).
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}
	if _, err := exec.LookPath("shellcheck"); err != nil {
		return Outcome{Warn: true}, nil
	}

	files, err := gatherFiles(root)
	if err != nil {
		return Outcome{}, fmt.Errorf("gather scan files: %w", err)
	}
	if len(files) == 0 {
		// No scripts to lint; treat as clean.
		return Outcome{Warn: false, ScannedFiles: 0}, nil
	}

	args := []string{"-e", strings.Join(excludeCodes, ",")}
	args = append(args, files...)
	cmd := exec.Command("shellcheck", args...)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	out := Outcome{ScannedFiles: len(files)}
	if runErr != nil {
		out.Diagnostic = strings.TrimRight(stdout.String()+stderr.String(), "\n")
		return out, ErrViolations
	}
	return out, nil
}

// gatherFiles walks the three canonical globs under root and returns a stable
// sorted set of files that exist. Missing directories are tolerated (a
// strangler wave may have deleted an entire tree).
func gatherFiles(root string) ([]string, error) {
	seen := map[string]struct{}{}
	for _, g := range scanGlobs {
		dir := filepath.Join(root, g.dir)
		if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		matches, err := filepath.Glob(filepath.Join(dir, g.pattern))
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			// Feed shellcheck repo-relative paths so its diagnostics are portable.
			rel, err := filepath.Rel(root, m)
			if err != nil {
				rel = m
			}
			seen[rel] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}
