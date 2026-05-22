// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package cosigncutover implements the §6.B.4 gate: track the cosign keyless
// v1.1 cutover 90-day deadline from v1.0 GA.
//
// Plan T5.3 of docs/plans/2026-05-22-shell-script-strategic-plan.md v1.3:
// replaces infra/scripts/check-cosign-cutover-deadline.sh + its hermetic test
// (D6 AUTO-DIE). Pure-Go (time arithmetic + file reads) — replaces bash
// date(1). No external CLI.
//
// Behaviour:
//   - marker .github/v1.0-ga-date.txt absent → DORMANT (pre-GA), pass.
//   - day ≤ 75              → silent OK.
//   - 75 < day ≤ 90         → warn (still pass).
//   - day > 90              → BLOCK unless a sponsor-signed exception file
//     (.github/cosign-cutover-exception.txt with an `approved-by: ` line) exists.
package cosigncutover

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	dateLayout  = "2006-01-02"
	warnDay     = 75
	deadlineDay = 90
)

// Config drives the gate.
type Config struct {
	Root string    // repo root (contains .github/)
	AsOf time.Time // reference "today"; zero value means time.Now().UTC()
}

// Outcome captures the result.
type Outcome struct {
	Dormant            bool   // pre-GA, marker absent
	GADate             string // raw GA date string
	Days               int    // days since GA
	Warn               bool   // in the 75<day≤90 alert window
	Blocked            bool   // day>90 without a signed exception
	ExceptionPermitted bool   // day>90 but a signed exception permitted it
}

// Check evaluates the cutover deadline.
func Check(cfg Config) (*Outcome, error) {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	marker := filepath.Join(root, ".github", "v1.0-ga-date.txt")
	exceptionFile := filepath.Join(root, ".github", "cosign-cutover-exception.txt")

	raw, err := os.ReadFile(marker)
	if err != nil {
		if os.IsNotExist(err) {
			return &Outcome{Dormant: true}, nil
		}
		return nil, fmt.Errorf("read marker %s: %w", marker, err)
	}
	gaStr := strings.TrimSpace(string(raw))
	if gaStr == "" {
		return nil, fmt.Errorf("marker %s exists but is empty", marker)
	}
	// First line only (mirrors `head -1`).
	if idx := strings.IndexByte(gaStr, '\n'); idx >= 0 {
		gaStr = strings.TrimSpace(gaStr[:idx])
	}

	ga, err := time.Parse(dateLayout, gaStr)
	if err != nil {
		return nil, fmt.Errorf("cannot parse v1.0 GA date %q in %s", gaStr, marker)
	}

	asOf := cfg.AsOf
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	days := int(asOf.Sub(ga).Hours() / 24)

	out := &Outcome{GADate: gaStr, Days: days}
	if days <= warnDay {
		return out, nil
	}
	if days <= deadlineDay {
		out.Warn = true
		return out, nil
	}
	// day > 90 — deadline blown; check for a signed sponsor exception.
	if signedException(exceptionFile) {
		out.ExceptionPermitted = true
		return out, nil
	}
	out.Blocked = true
	return out, nil
}

// signedException reports whether the exception file has an `approved-by: ` line.
func signedException(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "approved-by: ") {
			return true
		}
	}
	return false
}
