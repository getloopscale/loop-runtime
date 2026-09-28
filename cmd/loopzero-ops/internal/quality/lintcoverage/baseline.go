// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package lintcoverage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ErrBaselineMissing means the frozen counts are absent.
//
// Absent is a FAILURE, never an implicit empty set: without the frozen counts there is no way
// to know what a regression is, and treating that as "no regression" is the usual green-over-nothing.
var ErrBaselineMissing = errors.New("baseline missing — without the frozen counts there is no ratchet")

// BaselineRelPath is where the frozen counts live.
const BaselineRelPath = "infra/quality/lint-baseline.json"

// Baseline holds the frozen per-module counts, one map per dimension.
//
// Per MODULE, not a total: a single number would let `cmd/loopzero-ops` improve by 10 and `api`
// worsen by 10 without anyone seeing it. That is what the 200 baseline of `lint:strict` did — and
// it was 200 off from reality when it was measured.
type Baseline struct {
	Note  string         `json:"note,omitempty"`
	Lint  map[string]int `json:"lint"`
	Gofmt map[string]int `json:"gofmt"`
}

// Regression is one module that got worse.
type Regression struct {
	Module    string
	Dimension string
	Was, Now  int
}

// Improvement is one module that got better — reported so the number can actually come down.
type Improvement struct {
	Module   string
	Was, Now int
}

// Comparison is the outcome of comparing one dimension.
type Comparison struct {
	Regressions  []Regression
	Improvements []Improvement
	// Undeclared lists modules with no frozen entry. A new module enters DECLARED, otherwise
	// it is born with invisible findings — the defect this slice fixes.
	Undeclared []string
}

const baselineNote = "Per-MODULE frozen counts of golangci-lint and gofmt, read by " +
	"`loopzero-ops quality lint-coverage`. The ratchet refuses REGRESSION: a module at 0 that gains a " +
	"finding fails, even with hundreds frozen beside it. Improving passes and shows up in the output, " +
	"so that someone refreezes with `--write`. A module with no entry here is reported — it enters " +
	"declared, never silently. See B-121, B-083 and B-148."

// LoadBaseline reads the frozen counts.
func LoadBaseline(path string) (Baseline, error) {
	var b Baseline
	body, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return b, fmt.Errorf("%w: %s (generate it with `loopzero-ops quality lint-coverage --write`)", ErrBaselineMissing, path)
	}
	if err != nil {
		return b, fmt.Errorf("reading %s: %w", path, err)
	}
	if err := json.Unmarshal(body, &b); err != nil {
		return b, fmt.Errorf("unreadable baseline at %s: %w", path, err)
	}
	if b.Lint == nil {
		b.Lint = map[string]int{}
	}
	if b.Gofmt == nil {
		b.Gofmt = map[string]int{}
	}
	return b, nil
}

// Compare measures the observed counts against one frozen dimension.
func (b Baseline) Compare(observed, frozen map[string]int, dimension string) Comparison {
	var v Comparison
	mods := make([]string, 0, len(observed))
	for m := range observed {
		mods = append(mods, m)
	}
	sort.Strings(mods)

	for _, m := range mods {
		now := observed[m]
		was, ok := frozen[m]
		if !ok {
			v.Undeclared = append(v.Undeclared, m)
			continue
		}
		switch {
		case now > was:
			v.Regressions = append(v.Regressions, Regression{Module: m, Dimension: dimension, Was: was, Now: now})
		case now < was:
			v.Improvements = append(v.Improvements, Improvement{Module: m, Was: was, Now: now})
		}
	}
	return v
}

// WriteBaseline freezes the observed counts.
func WriteBaseline(path string, lint, gofmtCounts map[string]int) error {
	b := Baseline{Note: baselineNote, Lint: lint, Gofmt: gofmtCounts}
	if b.Lint == nil {
		b.Lint = map[string]int{}
	}
	if b.Gofmt == nil {
		b.Gofmt = map[string]int{}
	}
	// MarshalIndent over a map sorts the keys, so the output is deterministic — and a diff
	// whose order varies is noise nobody reviews.
	body, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("serializing baseline: %w", err)
	}
	return os.WriteFile(path, append(body, '\n'), 0o600)
}
