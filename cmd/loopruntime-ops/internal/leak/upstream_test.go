// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package leak_test

import (
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/leak"
)

// A 502 at the edge does not say WHO is down: the engine path crosses the
// dashboard, so both show the same code. Dashboard product routes do not cross
// the engine, so one of them answering while `/engine/*` does not means the
// dashboard is up and the engine is not.
func TestClassifyUpstream(t *testing.T) {
	cases := []struct {
		name      string
		engine    int
		dashboard int
		want      leak.UpstreamState
	}{
		{"both answer", 401, 401, leak.UpstreamBothUp},
		{"engine down, dashboard up", 502, 401, leak.UpstreamEngineDown},
		{"dashboard down takes the engine with it", 502, 502, leak.UpstreamDashboardDown},
		{"dashboard down with 503", 503, 503, leak.UpstreamDashboardDown},
		{"engine 504 with the dashboard serving", 504, 302, leak.UpstreamEngineDown},
	}
	for _, c := range cases {
		if got := leak.ClassifyUpstream(c.engine, c.dashboard); got != c.want {
			t.Errorf("%s: engine=%d dashboard=%d -> %v, want %v", c.name, c.engine, c.dashboard, got, c.want)
		}
	}
}

// The dashboard failing while the engine answers contradicts the measured
// topology; inventing a verdict would claim what the probe does not know.
func TestClassifyUpstream_ContradictoryIsNotGuessed(t *testing.T) {
	if got := leak.ClassifyUpstream(401, 502); got != leak.UpstreamInconclusive {
		t.Errorf("want inconclusive, got %v", got)
	}
}

// Every state tells the operator what to do; otherwise the distinction is useless.
func TestUpstreamStateExplainsTheAction(t *testing.T) {
	for _, st := range []leak.UpstreamState{
		leak.UpstreamBothUp, leak.UpstreamEngineDown,
		leak.UpstreamDashboardDown, leak.UpstreamInconclusive,
	} {
		if st.Explanation() == "" {
			t.Errorf("%v explains nothing to the operator", st)
		}
	}
}
