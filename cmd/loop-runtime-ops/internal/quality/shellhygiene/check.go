// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package shellhygiene is the Go home for the shell-hygiene gate (migrated from
// infra/tests/scripts/check-shell-hygiene_test.sh, plan 2026-05-23).
//
// Every non-lib .sh under infra/scripts, infra/helmfile/scripts, infra/tests
// MUST set a `set -euo pipefail` variant within its top 60 lines. Sourced libs
// (**/lib/*.sh) are exempt — `set -e` propagates to the caller, which is
// undesirable for a sourced library.
package shellhygiene

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// Config tells Check which repo to scan.
type Config struct {
	RepoRoot string
}

// Outcome is the gate result.
type Outcome struct {
	Pass       bool
	Total      int
	Violations []string
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// scanDirs are the trees scanned (relative to RepoRoot).
var scanDirs = []string{"infra/scripts", "infra/helmfile/scripts", "infra/tests"}

// hygieneRe matches the accepted `set -[eu]+o? pipefail` variants (canonical
// set -euo, plus set -uo / set -eo / set -eu), faithful to the bash gate.
var hygieneRe = regexp.MustCompile(`set -[eu][eu]?o?\s+pipefail`)

// headWindow is how many leading lines may carry the directive.
const headWindow = 60

// Check fails any non-lib .sh missing a set -euo pipefail variant in its top 60 lines.
func Check(cfg Config) (*Outcome, error) {
	out := &Outcome{Pass: true}
	seen := map[string]bool{}
	for _, d := range scanDirs {
		full := filepath.Join(cfg.RepoRoot, d)
		if _, err := os.Stat(full); err != nil {
			continue
		}
		err := filepath.Walk(full, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() || !strings.HasSuffix(path, ".sh") {
				return nil
			}
			if strings.Contains(path, "/lib/") {
				return nil // sourced libs exempt
			}
			if seen[path] {
				return nil
			}
			seen[path] = true
			ok, err := hasDirective(path)
			if err != nil {
				return err
			}
			out.Total++
			if !ok {
				rel, _ := filepath.Rel(cfg.RepoRoot, path)
				out.Violations = append(out.Violations,
					fmt.Sprintf("%s missing 'set -euo pipefail' in top %d lines", rel, headWindow))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	out.Pass = len(out.Violations) == 0
	return out, nil
}

func hasDirective(absPath string) (bool, error) {
	// #nosec G304 -- absPath comes from filepath.Walk of fixed scan dirs.
	f, err := os.Open(absPath)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for i := 0; i < headWindow && scanner.Scan(); i++ {
		if hygieneRe.MatchString(scanner.Text()) {
			return true, nil
		}
	}
	return false, scanner.Err()
}
