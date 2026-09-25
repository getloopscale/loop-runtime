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
