// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package aggregator

import (
	"testing"

	sm "github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// Every state the release state machine can put on a row has its own
// projection. A state that falls through to a default renders a failed
// release as "in progress" and keeps the reader polling forever.
func TestReleaseStateProjectionCoversEveryReleaseState(t *testing.T) {
	want := map[sm.State]struct {
		status   string
		terminal bool
	}{
		sm.ReleaseReady:           {"in_progress", false},
		sm.ReleasePending:         {"in_progress", false},
		sm.ReleaseActive:          {"completed", false},
		sm.ReleaseGitMirrorFailed: {"degraded", false},
		sm.ReleaseSyncFailed:      {"degraded", false},
		sm.ReleaseDriftAlert:      {"failed", true},
		sm.ReleaseSuperseded:      {"completed", false},
		sm.ReleaseRollingBack:     {"degraded", false},
		sm.ReleaseRolledBack:      {"failed", true},
		sm.ReleaseRollbackFailed:  {"failed", true},
		sm.ReleaseCancelled:       {"canceled", true},
		sm.ReleaseArchived:        {"completed", true},
	}
	for state, w := range want {
		t.Run(string(state), func(t *testing.T) {
			phase, status, terminal := releaseStateProjection(string(state))
			if phase != "release" || status != w.status || terminal != w.terminal {
				t.Errorf("got (%s,%s,%v), want (release,%s,%v)", phase, status, terminal, w.status, w.terminal)
			}
		})
	}
}

func TestReleaseStateProjectionMarksAnUnknownStateDegraded(t *testing.T) {
	phase, status, terminal := releaseStateProjection("SOMETHING_NEW")

	if phase != "release" || status != "degraded" || terminal {
		t.Errorf("unknown state projected to (%s,%s,%v), want (release,degraded,false)", phase, status, terminal)
	}
}
