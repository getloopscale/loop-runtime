// Package canonicalfixturerepo enforces that no LIVE test/seed script defaults
// to the legacy `loopzero-repotest` fixture instead of the canonical `loopzerokit-example`
// (2026-06-16 sponsor mandate; LIVE-validated 2026-06-23).
//
// Migrated from infra/tests/check-canonical-fixture-repo_test.sh (M-Migrate2 2026-07-07).
package canonicalfixturerepo

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const legacy = "loopzero-repotest"

var scanDirs = []string{"infra/tests/e2e", "infra/scripts/local"}

type Config struct{ RepoRoot string }
type Outcome struct{ Violations []string }

var ErrViolations = errors.New("legacy loopzero-repotest fixture referenced — canonical is getloopscale/loopzerokit-example")

func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil { return Outcome{}, err }
	}
	var violations []string
	for _, d := range scanDirs {
		full := filepath.Join(root, d)
		if _, err := os.Stat(full); errors.Is(err, fs.ErrNotExist) { continue }
		_ = filepath.WalkDir(full, func(path string, e fs.DirEntry, walkErr error) error {
			if walkErr != nil || e.IsDir() { return nil }
			body, err := os.ReadFile(path)
			if err != nil { return nil }
			for i, line := range strings.Split(string(body), "\n") {
				if !strings.Contains(line, legacy) { continue }
				low := strings.ToLower(line)
				// Comments explaining the deprecation/migration are allowed
				if isAllowlisted(low) { continue }
				rel, _ := filepath.Rel(root, path)
				violations = append(violations, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimSpace(line)))
			}
			return nil
		})
	}
	if len(violations) > 0 { return Outcome{Violations: violations}, ErrViolations }
	return Outcome{}, nil
}

func isAllowlisted(low string) bool {
	if !strings.Contains(low, "#") && !strings.Contains(low, "//") { return false }
	for _, m := range []string{"legacy", "deprecated", "superseded", "guard", "forbidden", "canonical"} {
		if strings.Contains(low, m) { return true }
	}
	return false
}
