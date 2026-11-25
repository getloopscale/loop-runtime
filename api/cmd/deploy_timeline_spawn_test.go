// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"reflect"
	"testing"
)

// TestEventTypeToCanonicalTransitions_HappyPath locks the canonical
// orchestrator event → FSM state mapping so a refactor of the
// happy-path mapper does not silently drop a transition.
func TestEventTypeToCanonicalTransitions_HappyPath(t *testing.T) {
	cases := []struct {
		event string
		want  []string
	}{
		{"build.started", []string{"BUILD_IN_PROGRESS"}},
		{"build.succeeded", []string{"BUILD_COMPLETED", "SIGN_IN_PROGRESS", "SIGN_COMPLETED"}},
		{"deploy.delivering", []string{"PENDING"}},
		{"deploy.verifying", []string{"ACTIVE", "DEPLOY_IN_PROGRESS"}},
		{"deploy.live", []string{"DEPLOY_COMPLETED", "LIVE_IN_PROGRESS"}},
		{"deploy.completed", []string{"LIVE_COMPLETED"}},
	}
	for _, tc := range cases {
		got := eventTypeToCanonicalTransitions(tc.event)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v; want %v", tc.event, got, tc.want)
		}
	}
}

// TestEventTypeToCanonicalTransitions_FailureSurfaces locks the canonical
// failure-path mapping. Sponsor LIVE-caught 2026-06-18: build.failed +
// build.cancelled previously returned []string{} on the false premise that
// "terminal failure surfaces via existing build SSE". The deploy timeline
// hero ONLY consumes the aggregator, so an empty mapping froze the hero
// in BUILD_IN_PROGRESS forever — visually "Deploying / Awaiting events"
// while the DB had status=FAILED for minutes. Lock the canonical
// transitions so a future refactor cannot regress.
func TestEventTypeToCanonicalTransitions_FailureSurfaces(t *testing.T) {
	cases := []struct {
		event string
		want  []string
	}{
		{"build.failed", []string{"BUILD_FAILED"}},
		{"build.cancelled", []string{"BUILD_CANCELLED"}},
		// A sync that fails ends the deploy phase. The release may fail before
		// the deploy phase was announced, so the phase is opened first; a
		// state already emitted is not emitted again.
		{"deploy.sync_failed", []string{"ACTIVE", "DEPLOY_IN_PROGRESS", "DEPLOY_FAILED"}},
	}
	for _, tc := range cases {
		got := eventTypeToCanonicalTransitions(tc.event)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v; want %v", tc.event, got, tc.want)
		}
	}
}

// TestEventTypeToCanonicalTransitions_UnknownEvent guarantees the mapper
// is conservative: any unrecognized event yields nil (no spurious FSM
// transition). This is the failsafe that lets us add new event types
// upstream without forcing a coordinated dashboard release.
func TestEventTypeToCanonicalTransitions_UnknownEvent(t *testing.T) {
	if got := eventTypeToCanonicalTransitions("unknown.event"); got != nil {
		t.Errorf("got %v; want nil", got)
	}
	if got := eventTypeToCanonicalTransitions(""); got != nil {
		t.Errorf("empty: got %v; want nil", got)
	}
}

// The reason of a failed sync is what the page shows under the failed phase.
func TestTimelineFailureReason_ReadsTheReasonOfAFailedSync(t *testing.T) {
	data := []byte(`{"reason": "Sync timeout: service did not become healthy within the time limit", "status": "failed"}`)
	if got := timelineFailureReason("DEPLOY_FAILED", data); got != "Sync timeout: service did not become healthy within the time limit" {
		t.Errorf("reason = %q", got)
	}
	if got := timelineFailureReason("DEPLOY_IN_PROGRESS", data); got != "" {
		t.Errorf("a state that is not a failure carries reason %q", got)
	}
	if got := timelineFailureReason("DEPLOY_FAILED", []byte(`not json`)); got != "" {
		t.Errorf("unreadable data gave reason %q", got)
	}
}
