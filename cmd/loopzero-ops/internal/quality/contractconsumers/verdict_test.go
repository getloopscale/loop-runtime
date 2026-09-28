// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package contractconsumers

import (
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func TestOutcomeVerdictNamesEachState(t *testing.T) {
	cases := []struct {
		name string
		o    Outcome
		want verdict.Verdict
	}{
		{"tracked path missing", Outcome{Pass: true, MissingTracked: []string{"api/x.go"}}, verdict.Fail},
		{"stale acknowledgement", Outcome{Pass: true, StaleAcknowledgements: []string{"x"}}, verdict.Fail},
		{"consumer reads a dropped tag", Outcome{Findings: []Finding{{}}}, verdict.Fail},
		{"partial pass", Outcome{Pass: true, Partial: true, MissingRoots: []string{"../loopzero-cli"}}, verdict.Pass},
		{"pass", Outcome{Pass: true}, verdict.Pass},
	}
	for _, c := range cases {
		if got := c.o.Verdict(); got != c.want {
			t.Errorf("%s: Verdict() = %v, want %v", c.name, got, c.want)
		}
	}
}
