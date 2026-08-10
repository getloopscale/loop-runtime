// Package nocloud enforces the T1.1 hermetic guard of plan
// m1-commit-canonical-30-fixes: no test/seed script may declare a default
// env-var value pointing at `cloud-dev.loopzero.example` (the Caddy 301
// redirect trap introduced by commit ee77b8da 2026-06-22).
//
// EC-1 absorption: the `/api/v1/webhooks/github` handler is the
// single redirect exception (Caddyfile lines 142-156 — webhook handler is
// handler-direct, goes straight to loopzero-api). Stage 2 filter excludes
// those paths from the violation set.
//
// Scan targets 3 directories:
//
//	archive/canonical-tests-v1/scripts/local/
//	infra/tests/e2e/
//	infra/scripts/local/
//
// Migrated from infra/tests/check-no-cloud-dev-default_test.sh
// (M-Migrate2 2026-07-10 M16 T2.3 Wave 1). Shape 3 (file-layout scan) per
// blueprint Q7 canonical mapping.
package nocloud

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var scanRoots = []string{
	"archive/canonical-tests-v1/scripts/local",
	"infra/tests/e2e",
	"infra/scripts/local",
}

// forbiddenRE matches `${VAR:-https://cloud-dev.loopzero.example...` — the
// default-value form that silently routes through Caddy's redirect trap.
//
// Bash regex was: \$\{[A-Z_]+:-https://cloud-dev\.loopzero\.dev
var forbiddenRE = regexp.MustCompile(`\$\{[A-Z_]+:-https://cloud-dev\.loopzero\.dev`)

// webhookExceptionRE is the EC-1 stage-2 filter — paths under
// /api/v1/webhooks/github are handler-direct (Caddyfile 142-156 exception)
// and MUST NOT be reported as violations.
var webhookExceptionRE = regexp.MustCompile(`/api/v1/webhooks/github`)

// Config controls the check.
type Config struct {
	// RepoRoot resolves the target scan directories. When empty, os.Getwd() is used.
	RepoRoot string
}

// Outcome is the aggregated result (canonical M-Migrate2 shape — plan D2 of
// M16 mandates Failures []string per blueprint ADR D1).
type Outcome struct {
	Failures []string
}

// ErrViolations signals ≥ 1 cloud-dev default env-var was found (excluding
// the webhook handler exception).
var ErrViolations = errors.New("cloud-dev default env-var trap detected")

// Check walks the 3 scan roots under RepoRoot and returns each file:line hit
// of the forbidden pattern that is NOT covered by the webhook exception.
func Check(cfg Config) (Outcome, error) {
	root := cfg.RepoRoot
	if root == "" {
		var err error
		if root, err = os.Getwd(); err != nil {
			return Outcome{}, fmt.Errorf("resolve repo root: %w", err)
		}
	}

	var failures []string
	for _, sub := range scanRoots {
		target := filepath.Join(root, sub)
		if _, err := os.Stat(target); errors.Is(err, fs.ErrNotExist) {
			// Directory absent — parity with bash `2>/dev/null || true`.
			continue
		} else if err != nil {
			return Outcome{}, fmt.Errorf("stat %s: %w", target, err)
		}

		walkErr := filepath.WalkDir(target, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !isTextCandidate(path) {
				return nil
			}
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return fmt.Errorf("read %s: %w", path, readErr)
			}
			for lineNum, line := range strings.Split(string(content), "\n") {
				if !forbiddenRE.MatchString(line) {
					continue
				}
				// Stage 2 filter — webhook handler exception (EC-1 absorption).
				if webhookExceptionRE.MatchString(line) {
					continue
				}
				rel, _ := filepath.Rel(root, path)
				failures = append(failures,
					fmt.Sprintf("%s:%d: cloud-dev default env-var (host removido em 2026-08-10; antes era armadilha de 301, agora nao resolve) — %s",
						rel, lineNum+1, strings.TrimSpace(line)),
				)
			}
			return nil
		})
		if walkErr != nil {
			return Outcome{}, fmt.Errorf("walk %s: %w", target, walkErr)
		}
	}

	if len(failures) > 0 {
		return Outcome{Failures: failures}, ErrViolations
	}
	return Outcome{}, nil
}

// isTextCandidate returns true if the file extension suggests a text/script.
// Conservative: covers .sh, .bash, .go, .py, .yaml, .yml, .md, and no-ext
// executables (which live under infra/scripts/local/).
func isTextCandidate(path string) bool {
	ext := filepath.Ext(path)
	switch ext {
	case ".sh", ".bash", ".go", ".py", ".yaml", ".yml", ".md", "":
		return true
	}
	return false
}
