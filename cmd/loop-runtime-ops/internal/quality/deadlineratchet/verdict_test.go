// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package deadlineratchet

import (
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

func TestOutcomeVerdictFailsOnlyOnABlownDeadline(t *testing.T) {
	cases := []struct {
		name string
		o    Outcome
		want verdict.Verdict
	}{
		{"blocked", Outcome{Blocked: true, Days: 91}, verdict.Fail},
		{"dormant", Outcome{Dormant: true}, verdict.Skip},
		{"permitted exception", Outcome{ExceptionPermitted: true, Days: 91}, verdict.Pass},
		{"warning window", Outcome{Warn: true, Days: 80}, verdict.Pass},
		{"within budget", Outcome{Days: 3}, verdict.Pass},
	}
	for _, c := range cases {
		if got := c.o.Verdict(); got != c.want {
			t.Errorf("%s: Verdict() = %v, want %v", c.name, got, c.want)
		}
	}
}
