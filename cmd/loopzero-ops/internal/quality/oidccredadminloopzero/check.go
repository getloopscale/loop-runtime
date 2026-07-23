// Package oidccredadminloopzero enforces the canonical dev credential — admin@loopzero.example
// / loopzero-dev-password (LIVE-validated 2026-06-16, sponsor verbatim "SEMPRE vamos usar
// nos testes"). Rejects the fabricated paulo@loopzero.test / LoopZeroDev2026! which does
// NOT exist in dev-public (Kratos 401).
//
// Migrated from infra/tests/check-oidc-cred-admin-loopzero_test.sh (M-Migrate2, 2026-07-07).
// Hermetic: NO network, NO kubectl.
package oidccredadminloopzero

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
)

var forbiddenPattern = regexp.MustCompile(`paulo@loopzero\.test|LoopZeroDev2026!`)

var scanDirs = []string{
	"infra/tests/e2e",
	"infra/scripts/local",
}

type Config struct{ RepoRoot string }
type Outcome struct{ Violations []string }

var ErrViolations = errors.New("fabricated paulo@loopzero.test/LoopZeroDev2026! credentials detected — canonical is admin@loopzero.example/loopzero-dev-password")

func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, err
		}
	}
	var violations []string
	for _, d := range scanDirs {
		full := filepath.Join(root, d)
		if _, err := os.Stat(full); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		// FIX-2 (M24, ADR-M24-3): fail-CLOSED — capture + propagate the walk error
		// instead of discarding it (`_ =`) and swallowing per-entry/read errors.
		if err := filepath.WalkDir(full, func(path string, e fs.DirEntry, entryErr error) error {
			if entryErr != nil {
				return entryErr
			}
			if e.IsDir() {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			for i, line := range splitLines(body) {
				if forbiddenPattern.MatchString(line) {
					rel, _ := filepath.Rel(root, path)
					violations = append(violations, fmt.Sprintf("%s:%d: %s", rel, i+1, trimSpace(line)))
				}
			}
			return nil
		}); err != nil {
			return Outcome{}, fmt.Errorf("walk %s: %w", full, err)
		}
	}
	if len(violations) > 0 {
		return Outcome{Violations: violations}, ErrViolations
	}
	return Outcome{}, nil
}

func splitLines(b []byte) []string {
	var out []string
	start := 0
	for i, c := range b {
		if c == '\n' {
			out = append(out, string(b[start:i]))
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, string(b[start:]))
	}
	return out
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
