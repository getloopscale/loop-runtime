// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package aggregator

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

type fakeAppender struct {
	mu     sync.Mutex
	calls  int
	lastID int64
	last   struct {
		buildID string
		etype   string
		data    json.RawMessage
	}
}

func (f *fakeAppender) Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastID++
	f.last.buildID = buildID
	f.last.etype = eventType
	f.last.data = data
	return f.lastID, nil
}

// TestSSEEventSink_ForwardsDeployIDAsBuildID confirms the adapter maps
// deploy_id → underlying store's build_id column. This is the canonical
// pattern from J-17 — the column name predates the deploy abstraction
// but stores the event scope (one row per (scope, event)).
func TestSSEEventSink_ForwardsDeployIDAsBuildID(t *testing.T) {
	app := &fakeAppender{}
	sink := NewSSEEventSink(app)
	id, err := sink.AppendDeployTimelineEvent(context.Background(),
		"dep-007", "deploy_timeline.build.in_progress", json.RawMessage(`{"x":1}`))
	if err != nil {
		t.Fatalf("Append err: %v", err)
	}
	if id != 1 {
		t.Errorf("id = %d; want 1", id)
	}
	if app.last.buildID != "dep-007" {
		t.Errorf("buildID = %q; want dep-007", app.last.buildID)
	}
	if app.last.etype != "deploy_timeline.build.in_progress" {
		t.Errorf("etype = %q", app.last.etype)
	}
}

// onceAppender is a store that can also write an event only when its stream
// does not hold one of that type yet.
type onceAppender struct {
	fakeAppender
	onceTypes []string
}

func (f *onceAppender) AppendOnce(_ context.Context, _ string, eventType string, _ json.RawMessage) (int64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onceTypes = append(f.onceTypes, eventType)
	return 1, true, nil
}

// Two aggregators can walk the same deploy for a while: the one the submit
// started and one that resumed the timeline after a restart. A phase event
// must exist once however many of them reach it.
func TestSSEEventSink_WritesAPhaseEventOnlyOnce(t *testing.T) {
	app := &onceAppender{}
	sink := NewSSEEventSink(app)

	if _, err := sink.AppendDeployTimelineEvent(context.Background(),
		"dep-007", "deploy_timeline.live.completed", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("append: %v", err)
	}

	if len(app.onceTypes) != 1 || app.onceTypes[0] != "deploy_timeline.live.completed" {
		t.Errorf("events written once = %v; want [deploy_timeline.live.completed]", app.onceTypes)
	}
	if app.calls != 0 {
		t.Errorf("plain appends = %d; want 0", app.calls)
	}
}

// A degraded event reports trouble each time it happens, so it may repeat.
func TestSSEEventSink_LetsADegradedEventRepeat(t *testing.T) {
	app := &onceAppender{}
	sink := NewSSEEventSink(app)

	for range 2 {
		if _, err := sink.AppendDeployTimelineEvent(context.Background(),
			"dep-007", AggregatorDegradedEventType, json.RawMessage(`{}`)); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	if app.calls != 2 {
		t.Errorf("plain appends = %d; want 2", app.calls)
	}
	if len(app.onceTypes) != 0 {
		t.Errorf("events written once = %v; want none", app.onceTypes)
	}
}

func TestTimelineEventType_NamesTheEventOfAState(t *testing.T) {
	cases := map[string]string{
		"BUILD_IN_PROGRESS": "deploy_timeline.build.in_progress",
		"PENDING":           "deploy_timeline.release.in_progress",
		"ACTIVE":            "deploy_timeline.release.completed",
		"LIVE_COMPLETED":    "deploy_timeline.live.completed",
		"DEPLOY_FAILED":     "deploy_timeline.deploy.failed",
	}
	for state, want := range cases {
		if got := TimelineEventType(state); got != want {
			t.Errorf("TimelineEventType(%q) = %q; want %q", state, got, want)
		}
	}
}
