// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package scanresult is the result a scanning quality gate returns, with the
// "nothing was scanned" rule written once, in code.
//
// A gate that walks files and reports what it found cannot tell a clean tree
// from a tree it never reached: both produce zero findings. Each gate used to
// decide that case for itself — some passed, some skipped, some failed — and
// the ones that passed were green over a directory that did not exist. Result
// carries how much was scanned, so the verdict can refuse silence, and Partial,
// so a gate that knowingly covers less than everything says so instead of
// looking complete.
//
// Gates migrate to it one at a time; a gate that has not yet declares its own
// outcome type.
package scanresult

import (
	"errors"
	"fmt"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

// ErrNothingScanned means the gate reached no input at all. Finding nothing is
// not the same as finding nothing wrong.
var ErrNothingScanned = errors.New("nothing scanned — finding nothing is not finding nothing wrong")

// Result is what one scanning gate observed.
type Result struct {
	// Scanned counts the inputs (files, documents) the gate actually read.
	Scanned int
	// Findings are the violations, one human-readable line each.
	Findings []string
	// Partial, when set, says why the scan knowingly covered less than the
	// whole subject. It is the only way zero scanned inputs can be accepted.
	Partial string
}

// Err reports ErrNothingScanned when nothing was scanned and the gate did not
// declare the scan partial.
func (r Result) Err() error {
	if r.Scanned == 0 && r.Partial == "" {
		return ErrNothingScanned
	}
	return nil
}

// Pass reports whether the gate may print PASS: something was scanned (or the
// scan was declared partial) and nothing was found.
func (r Result) Pass() bool {
	return r.Err() == nil && len(r.Findings) == 0
}

// Verdict is what the scan amounts to: NotVerified when nothing was scanned
// and the scan was not declared partial, Fail when it found something, Pass
// otherwise.
func (r Result) Verdict() verdict.Verdict {
	switch {
	case r.Err() != nil:
		return verdict.NotVerified
	case len(r.Findings) > 0:
		return verdict.Fail
	}
	return verdict.Pass
}

// Summary renders the counts a PASS or FAIL line should carry, so a green run
// can never be read as "0 of 0".
func (r Result) Summary() string {
	s := fmt.Sprintf("%d scanned, %d finding(s)", r.Scanned, len(r.Findings))
	if r.Partial != "" {
		s += " (PARTIAL: " + r.Partial + ")"
	}
	return s
}
