// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package cliexitcodes is the Go home for the cli-exit-codes gate (migrated from
// infra/tests/scripts/check-cli-exit-codes_test.sh, plan 2026-05-23).
//
// CLAUDE.md Rule 4: cli/src/ process.exit() codes must be 0-5 only (the
// permanent CLI contract). Exceptions: 130 (SIGINT) / 143 (SIGTERM) are allowed
// ONLY inside a process.on('SIG...') handler (within the 3 lines above). Test
// files (*.test.ts, *.spec.ts, __tests__/) are exempt.
package cliexitcodes

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Config tells Check which repo to scan.
type Config struct {
	RepoRoot string
}

// Outcome is the gate result.
type Outcome struct {
	Pass       bool
	Skipped    bool // cli/src absent
	Violations []string
}

var exitRe = regexp.MustCompile(`process\.exit\((\d+)\)`)

// Check fails any cli/src process.exit() outside the 0-5 contract (signal
// handlers using 130/143 in a process.on('SIG...) context are exempt).
func Check(cfg Config) (*Outcome, error) {
	srcDir := filepath.Join(cfg.RepoRoot, "cli", "src")
	if fi, err := os.Stat(srcDir); err != nil || !fi.IsDir() {
		return &Outcome{Pass: true, Skipped: true}, nil
	}

	out := &Outcome{Pass: true}
	err := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
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
		out.Violations = append(out.Violations, hits...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	out.Pass = len(out.Violations) == 0
	return out, nil
}

func isExcludedFile(path string) bool {
	return strings.Contains(path, ".test.") || strings.Contains(path, ".spec.") ||
		strings.Contains(path, "/__tests__/") || strings.Contains(path, "/node_modules/")
}

func scanFile(repoRoot, absPath string) ([]string, error) {
	// #nosec G304 -- absPath comes from filepath.Walk of cli/src.
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	rel, _ := filepath.Rel(repoRoot, absPath)
	var hits []string
	for i, line := range lines {
		m := exitRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		code, _ := strconv.Atoi(m[1])
		if code >= 0 && code <= 5 {
			continue
		}
		if (code == 130 || code == 143) && sigHandlerPrecedes(lines, i) {
			continue
		}
		hits = append(hits, fmt.Sprintf("%s:%d: process.exit(%d) outside 0-5 contract", rel, i+1, code))
	}
	return hits, nil
}

// sigHandlerPrecedes reports whether a process.on('SIG...) appears within the 3
// lines above (inclusive of the current line), mirroring the bash gate.
func sigHandlerPrecedes(lines []string, idx int) bool {
	start := idx - 3
	if start < 0 {
		start = 0
	}
	for j := start; j <= idx; j++ {
		if strings.Contains(lines[j], "process.on('SIG") {
			return true
		}
	}
	return false
}
