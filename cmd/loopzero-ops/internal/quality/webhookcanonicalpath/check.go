// Package webhookcanonicalpath enforces that no test/seed script references the
// wrong webhook path `/engine/api/v1/webhooks/github` — Caddy 301-redirects it
// to app-dev where there is no handler (404). The canonical is
// `/api/v1/webhooks/github` (handler-direct per Caddyfile lines 142-156).
//
// Migrated from infra/tests/check-webhook-canonical-path_test.sh (M-Migrate2, 2026-07-07).
// Hermetic: NO network, NO kubectl, NO helmfile — pure text walk over the working tree.
package webhookcanonicalpath

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// forbiddenPattern is the wrong URL that Caddy 301-redirects to app-dev where
// the /engine handler does not exist → 404 trap.
const forbiddenPattern = "/engine/api/v1/webhooks/github"

// scanDirs are the directory roots the original bash script walked. Missing dirs
// are tolerated (e.g. archive/canonical-tests-v1 was DELETED in M10 wave).
var scanDirs = []string{
	"infra/tests/e2e",
	"infra/scripts/local",
}

// Config controls the check.
type Config struct {
	RepoRoot string
}

// Outcome is the summary the CLI prints.
type Outcome struct {
	Violations []string
}

// ErrViolations means the forbidden pattern was found on at least one file.
var ErrViolations = errors.New("forbidden /engine/api/v1/webhooks/github pattern detected")

// Check walks scanDirs under repoRoot and returns ErrViolations if any tracked
// file contains the forbiddenPattern. Comments-in-code are allowed via the
// `guard` / `legacy` marker on the same line (mirrors the sibling allowlist
// heuristic in T1.4).
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}

	var violations []string
	for _, d := range scanDirs {
		full := filepath.Join(root, d)
		if _, err := os.Stat(full); errors.Is(err, fs.ErrNotExist) {
			continue // tolerate missing dirs (deleted in strangler waves)
		}
		if err := filepath.WalkDir(full, func(path string, e fs.DirEntry, walkErr error) error {
			if walkErr != nil || e.IsDir() {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			for i, line := range strings.Split(string(body), "\n") {
				if !strings.Contains(line, forbiddenPattern) {
					continue
				}
				if isAllowlistedComment(line) {
					continue
				}
				rel, _ := filepath.Rel(root, path)
				violations = append(violations, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimSpace(line)))
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

// isAllowlistedComment allows lines that are documenting the forbidden pattern
// as a guard/legacy/forbidden reference (audit-trail comments in other checks
// pointing at this same pattern). Mirrors the check-oidc-cred-admin-loopzero
// pattern.
func isAllowlistedComment(line string) bool {
	lower := strings.ToLower(line)
	if !strings.Contains(strings.TrimSpace(lower), "#") && !strings.Contains(strings.TrimSpace(lower), "//") {
		return false
	}
	for _, marker := range []string{"guard", "legacy", "deprecated", "superseded", "forbidden"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
