// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package advisorylock is the Go home for the advisory-lock-scope gate
// (migrated from infra/tests/scripts/check-advisory-lock-scope_test.sh, plan
// 2026-05-23, originally T6.6 + CLAUDE.md Rule 7).
//
// It forbids session-scoped pg_advisory_lock( in production code. With pgx
// pooling a session lock acquired by request A can leak to request B that picks
// up the same pooled connection — silent deadlock or cross-request mutex
// violation. Only pg_advisory_xact_lock( (transaction-scoped) is allowed.
package advisorylock

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/scanresult"
)

// Config tells Check which repo to scan.
type Config struct {
	RepoRoot string
}

// scanDirs are the production trees scanned (relative to RepoRoot). A tree may
// be absent from a given checkout; all of them absent is ErrNothingScanned.
var scanDirs = []string{"api", "infra/scripts", "infra/helmfile/scripts"}

// Check fails any session-scoped pg_advisory_lock( in production code. It
// returns scanresult.ErrNothingScanned when none of scanDirs holds a file.
func Check(cfg Config) (scanresult.Result, error) {
	var res scanresult.Result
	for _, d := range scanDirs {
		full := filepath.Join(cfg.RepoRoot, d)
		if _, err := os.Stat(full); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return res, fmt.Errorf("advisory-lock-scope: stat %s: %w", d, err)
		}
		err := filepath.Walk(full, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() || isExcludedFile(path) {
				return nil
			}
			hits, err := scanFile(cfg.RepoRoot, path)
			if err != nil {
				return err
			}
			res.Scanned++
			res.Findings = append(res.Findings, hits...)
			return nil
		})
		if err != nil {
			return res, err
		}
	}
	return res, res.Err()
}

// isExcludedFile skips test files and test helpers (faithful to the bash grep).
func isExcludedFile(path string) bool {
	return strings.Contains(path, "_test.") || strings.Contains(path, "/test_helpers/")
}

func scanFile(repoRoot, absPath string) ([]string, error) {
	// #nosec G304 -- absPath comes from filepath.Walk of fixed production dirs.
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(repoRoot, absPath)
	if err != nil {
		return nil, err
	}
	var hits []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if !strings.Contains(line, "pg_advisory_lock(") {
			continue
		}
		// pg_advisory_xact_lock( never contains the substring pg_advisory_lock(,
		// so a match here is genuinely the session-scoped variant.
		if isCommentLine(line) {
			continue
		}
		hits = append(hits, fmt.Sprintf("%s:%d: %s", rel, lineNo, strings.TrimSpace(line)))
	}
	return hits, scanner.Err()
}

// isCommentLine reports whether the line is a pure // or # comment.
func isCommentLine(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "//") || strings.HasPrefix(t, "#")
}
