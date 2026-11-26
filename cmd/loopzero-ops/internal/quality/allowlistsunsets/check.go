// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package allowlistsunsets enforces the sunset contract on the code-review allowlist:
// every exemption carries a date, and on that date it stops exempting anything.
//
// `code-quality-golden-rule.md` § 4 states the rule — a sunset window of at most 90 days,
// and an expired entry ignored so the finding re-fires at full severity. This is what
// makes an allowlist a deferral rather than a deletion, and it only works if something
// reads the dates.
//
// WHAT THIS USED TO BE, and why it changed. It was a hermetic contract test for
// `.claude/scripts/check-allowlist-sunsets.py`: it seeded fixture allowlists and asserted
// that script's exit codes (0 future, 1 past, 2 parse error). Measured 2026-09-16, the
// script exists in no clone, in no installed kit, and nowhere reachable — `.claude/` is
// gitignored, and its `scripts/` was renamed to `mechanisms/` on 2026-09-01, so the path
// could not have survived either way.
//
// The gate therefore reported a missing program, forever, and the contract it guarded had
// no enforcer at all. The live allowlist was carrying two entries seven days past their
// own deadline when this was written; nobody had been told, because the thing that would
// have told them was the thing that was gone.
//
// Rung 1 of the parsimony ladder removed the fixture half — a contract test for a program
// that does not exist does not need to exist — and the live check is implemented here,
// where the rest of the gates live and where no interpreter has to be present.
package allowlistsunsets

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/calendarday"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

const (
	liveAllowlistPath = "rules/code-review-allowlist.txt"
	// sunsetColumn is the 0-indexed pipe-separated column holding the date, per the
	// format the allowlist declares in its own header:
	//   finding_id | category | sunset_date | rationale
	sunsetColumn = 2
	minColumns   = 4
)

// Config controls the check.
type Config struct {
	RepoRoot string
	// AllowlistPath overrides liveAllowlistPath.
	AllowlistPath string
	// Today allows a test to inject a fixed reference date; zero means today.
	Today time.Time
}

// Outcome separates what blocks from what is merely worth saying. An entry expiring TODAY
// is still valid for the whole of its last day, and refusing it would refuse work on the
// one day somebody could still act on it.
type Outcome struct {
	Failures []string
	Warnings []string
	Checked  int
	// Violation is what the check found wrong (wrapping one of its Err*
	// reasons), or nil. A violation is the check's verdict, never its error.
	Violation error
}

// Verdict is Fail when the check found a violation and Pass otherwise.
func (o Outcome) Verdict() verdict.Verdict {
	if o.Violation != nil {
		return verdict.Fail
	}
	return verdict.Pass
}

// ErrViolations means at least one entry has expired or cannot be read.
var ErrViolations = errors.New("allowlist sunset contract violated")

// Check reads the allowlist and grades every entry's sunset.
func Check(cfg Config) (Outcome, error) {
	var out Outcome
	rel := cfg.AllowlistPath
	if rel == "" {
		rel = liveAllowlistPath
	}
	full := filepath.Join(cfg.RepoRoot, rel)

	f, err := os.Open(full) // #nosec G304 -- path is the configured allowlist under the repo root
	if err != nil {
		if os.IsNotExist(err) {
			// No allowlist is no exemptions, which is the strongest possible state of this
			// contract. Reporting it as a violation would make having nothing to forgive
			// look like a failure.
			return out, nil
		}
		return out, fmt.Errorf("allowlist-sunsets: read %s: %w", rel, err)
	}
	defer func() { _ = f.Close() }()

	today := calendarday.Today(cfg.Today)
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		raw := strings.TrimSpace(sc.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		out.Checked++

		cols := strings.Split(raw, "|")
		if len(cols) < minColumns {
			out.Failures = append(out.Failures, fmt.Sprintf(
				"%s:%d: entry has %d column(s), the declared format has %d (finding_id | category | sunset_date | rationale)",
				rel, line, len(cols), minColumns))
			continue
		}

		field := strings.TrimSpace(cols[sunsetColumn])
		sunset, perr := calendarday.Parse(field)
		if perr != nil {
			// Not a date in the future — a date nobody can read. Treating it as future
			// would turn a typo into a permanent exemption, which is what the sunset is
			// for.
			out.Failures = append(out.Failures, fmt.Sprintf(
				"%s:%d: sunset %q is not YYYY-MM-DD", rel, line, field))
			continue
		}

		id := strings.TrimSpace(cols[0])
		switch {
		case sunset.Before(today):
			out.Failures = append(out.Failures, fmt.Sprintf(
				"%s:%d: %s expired on %s (%d day(s) ago) — the exemption has ended; remove the entry so the finding re-fires, or do the work",
				rel, line, id, field, int(today.Sub(sunset).Hours()/24)))
		case sunset.Equal(today):
			out.Warnings = append(out.Warnings, fmt.Sprintf(
				"%s:%d: %s expires TODAY (%s) — last day it exempts anything", rel, line, id, field))
		}
	}
	if serr := sc.Err(); serr != nil {
		return out, fmt.Errorf("allowlist-sunsets: scan %s: %w", rel, serr)
	}
	if len(out.Failures) > 0 {
		out.Violation = ErrViolations
		return out, nil
	}
	return out, nil
}
