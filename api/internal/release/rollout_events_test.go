// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package release

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

// argo-rollouts-progressive-delivery-plan T4.1 — rollout event tests.

type fakeEventStore struct {
	events []recordedEvent
	err    error
}

type recordedEvent struct {
	BuildID   string
	EventType string
	Data      json.RawMessage
}

func (f *fakeEventStore) Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.events = append(f.events, recordedEvent{BuildID: buildID, EventType: eventType, Data: data})
	return int64(len(f.events)), nil
}

func TestEmitRolloutAborted_HappyPath(t *testing.T) {
	es := &fakeEventStore{}
	logger := slog.Default()
	err := EmitRolloutAborted(context.Background(), logger, es, "build-xyz", RolloutAbortedEvent{
		Reason:  "analysis-failed",
		Step:    3,
		Weight:  20,
		Message: "success-rate dropped below threshold",
	})
	if err != nil {
		t.Fatalf("EmitRolloutAborted error: %v", err)
	}
	if len(es.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(es.events))
	}
	if es.events[0].EventType != "rollout.aborted" {
		t.Errorf("event type = %q, want rollout.aborted", es.events[0].EventType)
	}
	var payload RolloutAbortedEvent
	if err := json.Unmarshal(es.events[0].Data, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Reason != "analysis-failed" || payload.Step != 3 || payload.Weight != 20 {
		t.Errorf("payload mismatch: %+v", payload)
	}
}
