// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package trufflehogscan

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ErrBaselineMissing means the adjudicated baseline is absent.
//
// Absent is a FAILURE, never an implicit empty set (EC-2): reading the absence as "nothing
// adjudicated, nothing new" would turn an `rm` into a bypass of the gate.
var ErrBaselineMissing = errors.New("baseline missing — no green is possible without the adjudicated set")

// BaselineRelPath is where the adjudicated set lives, relative to the repo root.
const BaselineRelPath = "infra/quality/trufflehog-baseline.json"

// defaultBaselineRel is the package-internal alias used by Check and Generate.
const defaultBaselineRel = BaselineRelPath

// Entry is one adjudicated candidate. It carries NO secret material — see Finding.ID.
type Entry struct {
	ID       string `json:"id"`
	Detector string `json:"detector"`
	// File is CONTEXT, not identity. The identity is (detector, raw): the same text
	// in five files is ONE candidate, because the adjudication says "this is not a secret"
	// and that nature does not change from file to file. The field name says it is the
	// first place it was seen — calling it `file` would suggest the only one.
	File string `json:"first_seen_in"`
}

// baselineNote travels inside the artifact because whoever finds this file next will
// not have this commit's context.
const baselineNote = "Set of ADJUDICATED trufflehog candidates. " +
	"A candidate outside this set FAILS the gate until someone rewrites it with `loop-runtime-ops quality trufflehog-scan --write` — " +
	"and the diff of that rewrite is the adjudication. " +
	"`--write` REFUSES while any live finding exists: this list adjudicates \"this is not a secret\", never \"this secret may live here\". " +
	"`id` is a truncated sha256(detector+raw) — no secret material lives in this file. See B-143."

// Baseline is the adjudicated candidate set.
type Baseline struct {
	Note    string  `json:"note,omitempty"`
	Entries []Entry `json:"entries"`
}

// LoadBaseline reads the versioned baseline.
func LoadBaseline(path string) (Baseline, error) {
	var b Baseline
	body, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return b, fmt.Errorf("%w: %s (generate it with `loop-runtime-ops quality trufflehog-scan --write`)", ErrBaselineMissing, path)
	}
	if err != nil {
		return b, fmt.Errorf("reading %s: %w", path, err)
	}
	if err := json.Unmarshal(body, &b); err != nil {
		return b, fmt.Errorf("unreadable baseline at %s: %w", path, err)
	}
	return b, nil
}

// Unadjudicated returns the findings absent from the baseline.
func (b Baseline) Unadjudicated(found []Finding) []Finding {
	known := b.index()
	seen := map[string]bool{}
	var out []Finding
	for _, f := range found {
		if known[f.ID()] || seen[f.ID()] {
			continue
		}
		seen[f.ID()] = true
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// Stale returns baseline entries with no matching finding.
//
// Ratchet hygiene — REPORTS, never fails: a secret removed from the tree cannot
// become a reason for the gate to lock. A set that only grows is a set nobody
// prunes, and that is how a baseline becomes a rubber stamp.
func (b Baseline) Stale(found []Finding) []Entry {
	live := map[string]bool{}
	for _, f := range found {
		live[f.ID()] = true
	}
	var out []Entry
	for _, e := range b.Entries {
		if !live[e.ID] {
			out = append(out, e)
		}
	}
	return out
}

func (b Baseline) index() map[string]bool {
	m := make(map[string]bool, len(b.Entries))
	for _, e := range b.Entries {
		m[e.ID] = true
	}
	return m
}

// WriteBaseline records the adjudicated set.
//
// Refuses while any LIVE finding exists (EC-1). Without this, rewriting would be the way to
// unlock a push blocked by a credential that works — and the mechanism designed
// to prevent B-142 would become the way to repeat it, with the appearance of process.
func WriteBaseline(path string, found []Finding) error {
	for _, f := range found {
		if f.Live {
			return fmt.Errorf("%w: %s — the baseline adjudicates \"this is not a secret\", never \"this secret may live here\"",
				ErrLiveSecret, f)
		}
	}

	seen := map[string]bool{}
	b := Baseline{Note: baselineNote, Entries: []Entry{}}
	for _, f := range found {
		if seen[f.ID()] {
			continue
		}
		seen[f.ID()] = true
		b.Entries = append(b.Entries, Entry{ID: f.ID(), Detector: f.Detector, File: f.File})
	}
	sort.Slice(b.Entries, func(i, j int) bool {
		if b.Entries[i].File != b.Entries[j].File {
			return b.Entries[i].File < b.Entries[j].File
		}
		if b.Entries[i].Detector != b.Entries[j].Detector {
			return b.Entries[i].Detector < b.Entries[j].Detector
		}
		return b.Entries[i].ID < b.Entries[j].ID
	})

	body, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("serializing baseline: %w", err)
	}
	return os.WriteFile(path, append(body, '\n'), 0o600)
}
