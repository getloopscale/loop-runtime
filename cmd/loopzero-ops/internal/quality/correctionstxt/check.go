// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package correctionstxt is the Go home for the corrections-txt-closed gate
// (migrated from infra/tests/check-corrections-txt-closed_test.sh, plan
// 2026-05-23).
//
// It enforces close-out hygiene on the drill war-journal
// infra/tests/performance/load/CORRECTIONS.txt: every `### ` item must carry a
// `**STATUS YYYY-MM-DD:**` annotation before the next heading, the header must
// reference the audit finding, and no item containing a "Definitive fix" may
// lack a STATUS. (The original bash also had a 4th no-op placeholder test for a
// post-cutoff grace period that asserted nothing — not ported.)
package correctionstxt

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

// Outcome is the gate result.
type Outcome struct {
	Pass       bool
	Violations []string
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

var (
	headingRe = regexp.MustCompile(`^### `)
	statusRe  = regexp.MustCompile(`\*\*STATUS \d{4}-\d{2}-\d{2}:`)
)

const headerRef = "finding-2026-05-13-corrections-txt-audit"

// Check validates close-out hygiene on the given CORRECTIONS.txt path.
func Check(path string) (*Outcome, error) {
	// #nosec G304 -- path is a CI-supplied repo path, not user input.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read CORRECTIONS.txt %q: %w", path, err)
	}
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %q: %w", path, err)
	}

	out := &Outcome{Pass: true}

	// Header (first 10 lines) must reference the audit finding.
	headerEnd := 10
	if len(lines) < headerEnd {
		headerEnd = len(lines)
	}
	if !strings.Contains(strings.Join(lines[:headerEnd], "\n"), headerRef) {
		out.Violations = append(out.Violations,
			fmt.Sprintf("header (first 10 lines) does not reference %q", headerRef))
	}

	// Per-item check: every `### ` block must carry a STATUS annotation before
	// the next heading. (The original bash also had a separate "Definitive fix
	// without STATUS" check — subsumed here: any item lacking STATUS fails,
	// Definitive-fix or not.)
	var heading string
	var hasStatus bool
	finalize := func() {
		if heading != "" && !hasStatus {
			out.Violations = append(out.Violations,
				fmt.Sprintf("%s — missing **STATUS YYYY-MM-DD:** annotation", heading))
		}
	}
	for _, line := range lines {
		switch {
		case headingRe.MatchString(line):
			finalize()
			heading = line
			hasStatus = false
		case statusRe.MatchString(line):
			hasStatus = true
		}
	}
	finalize()

	out.Pass = len(out.Violations) == 0
	return out, nil
}
