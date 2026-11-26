// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unhomedlogic

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

// LoadAllowlist reads the allowlist markdown at path.
//
// AN ABSENT ALLOWLIST DECLARES NO EXEMPTIONS — the same contract
// `internal/quality/nonewbash` states, and for the same measured reason: this
// allowlist lived under `.claude/`, which `3d4075ca3` (2026-09-08) removed from
// tracking, so it existed in no clone and on no runner. Measured 2026-09-16,
// treating its absence as an error blocked a push.
//
// Nothing exempt is the strictest reading, so losing the file cannot weaken
// the gate: absent is reported and yields empty content. A file that is
// PRESENT and unreadable is an error.
func LoadAllowlist(path string) (md string, absent bool, err error) {
	data, err := os.ReadFile(path) // #nosec G304 -- repo-internal allowlist path
	if errors.Is(err, fs.ErrNotExist) {
		return "", true, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(data), false, nil
}

// BodiesUnder reads script bodies relative to root, for content verification:
// a script that cannot be read has no body to judge.
func BodiesUnder(root string) ReadBody {
	return func(p string) (string, bool) {
		b, err := os.ReadFile(filepath.Join(root, p)) // #nosec G304 -- repo-internal script path
		if err != nil {
			return "", false
		}
		return string(b), true
	}
}

// Outcome is what a run established about un-homed operational logic.
type Outcome struct {
	Current Counts
	// Checked is set when the run ratcheted Current against a baseline. A
	// report, a ledger or a baseline update judges nothing.
	Checked     bool
	Baseline    Counts
	Regressions []string
}

// Verdict is Fail when a ratcheted run finds a suspect KEEP or an increase
// over the baseline, and Pass otherwise.
func (o Outcome) Verdict() verdict.Verdict {
	if o.Checked && (len(o.Current.SuspectKeeps) > 0 || len(o.Regressions) > 0) {
		return verdict.Fail
	}
	return verdict.Pass
}

// Ratchet compares current against baseline.
func Ratchet(current, baseline Counts) Outcome {
	_, regressions := RatchetCheck(current, baseline)
	return Outcome{Current: current, Checked: true, Baseline: baseline, Regressions: regressions}
}

// BaselineReadError is a baseline file that could not be read, as opposed to
// one that could not be decoded. Its message is the read error's own.
type BaselineReadError struct {
	Path string
	Err  error
}

func (e *BaselineReadError) Error() string { return e.Err.Error() }

// Unwrap returns the read error.
func (e *BaselineReadError) Unwrap() error { return e.Err }

// LoadBaseline reads the frozen counts at path. A file that cannot be read is
// a *BaselineReadError; one that cannot be decoded is the decoder's error.
func LoadBaseline(path string) (Counts, error) {
	var c Counts
	data, err := os.ReadFile(path) // #nosec G304 -- repo-internal baseline path
	if err != nil {
		return c, &BaselineReadError{Path: path, Err: err}
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, err
	}
	return c, nil
}

// WriteBaseline freezes c at path.
func WriteBaseline(path string, c Counts) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode baseline: %w", err)
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}
