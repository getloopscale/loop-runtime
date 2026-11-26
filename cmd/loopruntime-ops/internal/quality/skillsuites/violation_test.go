// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package skillsuites

import (
	"errors"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

// violationOf folds the check's channels into one error for the assertions in
// this package: the error when the check could not run, otherwise the
// violation, otherwise why nothing was verified. A violation or an unverified
// run still returned as an error breaks the verdict contract, so it is
// refused, not passed through.
func violationOf(out Outcome, err error) (Outcome, error) {
	if err != nil {
		if errors.Is(err, ErrSuitesFailed) || errors.Is(err, ErrNoSuitesFound) || errors.Is(err, ErrSkillsRootMissing) {
			return out, errors.New("a verdict was returned as an error instead of on the outcome: " + err.Error())
		}
		return out, err
	}
	if out.Violation != nil {
		return out, out.Violation
	}
	return out, out.Unverified
}

func TestOutcomeVerdictNamesEachState(t *testing.T) {
	cases := []struct {
		name string
		o    Outcome
		want verdict.Verdict
	}{
		{"red suite", Outcome{Violation: ErrSuitesFailed}, verdict.Fail},
		{"no suite found", Outcome{Violation: ErrNoSuitesFound}, verdict.Fail},
		{"kit not installed", Outcome{Unverified: ErrSkillsRootMissing}, verdict.Skip},
		{"pytest absent", Outcome{Advisory: true}, verdict.Skip},
		{"green", Outcome{Results: []Result{{Skill: "a", Passed: true}}}, verdict.Pass},
	}
	for _, c := range cases {
		if got := c.o.Verdict(); got != c.want {
			t.Errorf("%s: Verdict() = %v, want %v", c.name, got, c.want)
		}
	}
}
