// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package deadlineratchet implements the shared deadline-ratchet algorithm used
// by time-bound governance gates: a marker file records a start date; the gate
// is dormant until it exists, then warns inside a window and blocks past a hard
// deadline unless a sponsor-signed exception file permits the slip.
//
// Plan T5.3 of 2026-05-22-shell-script-strategic-plan v1.3:
// extracts the shared core behind check-cosign-cutover-deadline.sh (§6.B.4) and
// check-service-auth-rotation.sh (§6.B.5) — the original bash author noted both
// were "the same ratchet pattern; operator learns ONE pattern". DRY: one piece
// of business logic, parameterised by Spec, instead of two near-identical copies.
package deadlineratchet

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/calendarday"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

// DateLayout is the ISO-8601 date layout for marker files.
const DateLayout = "2006-01-02"

// Spec parameterises one ratchet instance.
type Spec struct {
	// MarkerRelPath is the start-date file relative to repo root.
	MarkerRelPath string
	// ExceptionRelPath is the sponsor-exception file relative to repo root.
	ExceptionRelPath string
	// WarnDay is the last in-budget day; days in (WarnDay, DeadlineDay] warn.
	WarnDay int
	// DeadlineDay is the last permitted day; days > DeadlineDay block.
	DeadlineDay int
}

// Config drives a single evaluation.
type Config struct {
	Root string    // repo root (contains the marker/exception dir)
	AsOf time.Time // reference "today"; zero value means the local calendar day
}

// Outcome captures the result.
type Outcome struct {
	Dormant            bool   // marker absent (pre-tracking)
	Date               string // raw start date from the marker
	Days               int    // days since the start date
	Warn               bool   // in the (WarnDay, DeadlineDay] alert window
	Blocked            bool   // days > DeadlineDay without a signed exception
	ExceptionPermitted bool   // past deadline but a signed exception permitted it
}

// Verdict is Fail when the deadline blew with no signed exception, Skip while
// the ratchet is dormant (no start marker yet), and Pass otherwise — a
// warning window or a permitted exception warns without failing.
func (o Outcome) Verdict() verdict.Verdict {
	switch {
	case o.Blocked:
		return verdict.Fail
	case o.Dormant:
		return verdict.Skip
	}
	return verdict.Pass
}

// Check evaluates the ratchet for spec against cfg.
func Check(spec Spec, cfg Config) (*Outcome, error) {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	marker := filepath.Join(root, spec.MarkerRelPath)
	exceptionFile := filepath.Join(root, spec.ExceptionRelPath)

	raw, err := os.ReadFile(marker)
	if err != nil {
		if os.IsNotExist(err) {
			return &Outcome{Dormant: true}, nil
		}
		return nil, fmt.Errorf("read marker %s: %w", marker, err)
	}
	dateStr := strings.TrimSpace(string(raw))
	if dateStr == "" {
		return nil, fmt.Errorf("marker %s exists but is empty", marker)
	}
	if idx := strings.IndexByte(dateStr, '\n'); idx >= 0 {
		dateStr = strings.TrimSpace(dateStr[:idx]) // first line only (head -1)
	}

	start, err := time.Parse(DateLayout, dateStr)
	if err != nil {
		return nil, fmt.Errorf("cannot parse date %q in %s", dateStr, marker)
	}

	// Calendar days in the local clock: the marker is a hand-written date.
	days := calendarday.DaysBetween(start, calendarday.Today(cfg.AsOf))

	out := &Outcome{Date: dateStr, Days: days}
	switch {
	case days <= spec.WarnDay:
		// in budget
	case days <= spec.DeadlineDay:
		out.Warn = true
	case signedException(exceptionFile):
		out.ExceptionPermitted = true
	default:
		out.Blocked = true
	}
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
