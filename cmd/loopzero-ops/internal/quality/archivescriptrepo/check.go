// Package archivescriptrepo enforces the T1.2 hermetic guard of plan
// m1-commit-canonical-30-fixes: no script in
// archive/canonical-tests-v1/scripts/local/ may resolve REPO_ROOT with
// only 3 dirs up ($(dirname "$0")/../../..) — canonical post-quarantine
// commit 289cf9d6 is 4 dirs up ($(dirname "$0")/../../../..).
//
// Regression class: after the archive/canonical-tests-v1 quarantine moved
// scripts one directory deeper, any script still resolving REPO_ROOT via
// 3 dirs up silently targets the WRONG directory (parent of the actual
// repo root), producing baffling "file not found" / permission failures.
//
// Migrated from infra/tests/check-archive-script-repo-root_test.sh
// (M-Migrate2 2026-07-10 M16 T2.2 Wave 1). Shape 3 (file-layout scan) per
// blueprint Q7 canonical mapping.
package archivescriptrepo

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const scanRoot = "archive/canonical-tests-v1/scripts/local"

// forbiddenRE matches $(dirname "$0")/../../.. with exactly 3 segments
// (NOT followed by another /..), which is the pre-quarantine 3-dirs-up
// form that must be replaced with the canonical 4-dirs-up variant.
//
// Bash regex was: \$\(dirname[[:space:]]+"\$0"\)/\.\./\.\./\.\.[^/]
// Go translation matches the same shape.
var forbiddenRE = regexp.MustCompile(`\$\(dirname\s+"\$0"\)/\.\./\.\./\.\.(?:[^/]|$)`)

// Config controls the check.
type Config struct {
	// RepoRoot resolves the target scan directory. When empty, os.Getwd() is used.
	RepoRoot string
}

// Outcome is the aggregated result (canonical M-Migrate2 shape — plan D2 of
// M16 mandates Failures []string per blueprint ADR D1).
type Outcome struct {
	Failures []string
}

// ErrViolations signals ≥ 1 assertion failed.
var ErrViolations = errors.New("archive-script-repo-root canonical 4-dirs-up violated")

// Check walks scanRoot under RepoRoot and returns each file:line hit of the
// forbidden 3-dirs-up pattern.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}
	target := filepath.Join(root, scanRoot)

	if _, err := os.Stat(target); errors.Is(err, fs.ErrNotExist) {
		// Directory absent — treat as no-content (empty Outcome). Matches bash
		// `grep -rEn ... 2>/dev/null || true` which produces empty violations
		// on missing dir.
		return Outcome{}, nil
	} else if err != nil {
		return Outcome{}, fmt.Errorf("stat %s: %w", target, err)
	}

	var failures []string
	walkErr := filepath.WalkDir(target, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// Only scan text/script-shaped files. Skip binaries/large blobs.
		if !isTextCandidate(path) {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read %s: %w", path, readErr)
		}
		for lineNum, line := range strings.Split(string(content), "\n") {
			if forbiddenRE.MatchString(line) {
				rel, _ := filepath.Rel(root, path)
				failures = append(failures,
					fmt.Sprintf("%s:%d: forbidden 3-dirs-up REPO_ROOT resolution — %s",
						rel, lineNum+1, strings.TrimSpace(line)),
				)
			}
		}
		return nil
	})
	if walkErr != nil {
		return Outcome{}, fmt.Errorf("walk %s: %w", target, walkErr)
	}

	if len(failures) > 0 {
		return Outcome{Failures: failures}, ErrViolations
	}
	return Outcome{}, nil
}

// isTextCandidate returns true if the extension suggests a text/script file
// the scanner should examine. Conservative: covers .sh, .bash, and no-ext
// executables that live in archive/canonical-tests-v1/scripts/.
func isTextCandidate(path string) bool {
	ext := filepath.Ext(path)
	switch ext {
	case ".sh", ".bash", "":
		return true
	}
	return false
}
