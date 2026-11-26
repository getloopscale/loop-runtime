// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package aggregator

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/scrub"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// memoryStore is a hermetic in-memory EventSink for testing aggregator
// persistence without a Postgres dependency.
type memoryStore struct {
	mu     sync.Mutex
	events []StoredEvent
}

type StoredEvent struct {
	DeployID  string
	EventType string
	Data      json.RawMessage
}

func (m *memoryStore) AppendDeployTimelineEvent(ctx context.Context, deployID, eventType string, data json.RawMessage) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, StoredEvent{DeployID: deployID, EventType: eventType, Data: data})
	return int64(len(m.events)), nil
}

func (m *memoryStore) snapshot() []StoredEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]StoredEvent, len(m.events))
	copy(out, m.events)
	return out
}

// scriptedReader emits a pre-canned sequence then returns nil.
type scriptedReader struct {
	id     SourceID
	events []PhaseEvent
}

func (s *scriptedReader) ID() SourceID { return s.id }
func (s *scriptedReader) Stream(ctx context.Context, deployID string, out chan<- PhaseEvent) error {
	for _, e := range s.events {
		select {
		case out <- e:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// panickyReader simulates an adapter bug — aggregator MUST recover.
type panickyReader struct{}

func (p *panickyReader) ID() SourceID { return SourceLokiMarkers }
func (p *panickyReader) Stream(ctx context.Context, deployID string, out chan<- PhaseEvent) error {
	panic("simulated adapter bug")
}

// now is a fixed timestamp helper for deterministic tests.
func now() time.Time { return time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC) }

// TestAggregator_HappyPath_BuildToLive drives all 5 phases through the
// aggregator from one synthesized source and confirms scrubbed events
// are appended in canonical order.
func TestAggregator_HappyPath_BuildToLive(t *testing.T) {
	store := &memoryStore{}
	reader := &scriptedReader{
		id: SourceReleaseDB,
		events: []PhaseEvent{
			{Phase: "build", Status: "in_progress", Timestamp: now()},
			{Phase: "build", Status: "completed", Timestamp: now().Add(2 * time.Minute)},
			{Phase: "sign", Status: "in_progress", Timestamp: now().Add(2 * time.Minute)},
			{Phase: "sign", Status: "completed", Timestamp: now().Add(2*time.Minute + 32*time.Second)},
			{Phase: "release", Status: "in_progress", Timestamp: now().Add(3 * time.Minute)},
			{Phase: "release", Status: "completed", Timestamp: now().Add(3*time.Minute + 4*time.Second)},
			{Phase: "deploy", Status: "in_progress", Timestamp: now().Add(4 * time.Minute)},
			{Phase: "deploy", Status: "completed", Timestamp: now().Add(5 * time.Minute)},
			{Phase: "live", Status: "in_progress", Timestamp: now().Add(5*time.Minute + 10*time.Second)},
			{Phase: "live", Status: "completed", Timestamp: now().Add(5*time.Minute + 30*time.Second)},
		},
	}

	agg := New("dep-001", store, scrub.ScrubEventWithCount, []SourceReader{reader})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	agg.Run(ctx)

	got := store.snapshot()
	if len(got) != len(reader.events) {
		t.Fatalf("appended events = %d; want %d", len(got), len(reader.events))
	}
	if got[len(got)-1].EventType != "deploy_timeline.live.completed" {
		t.Fatalf("last event = %q; want deploy_timeline.live.completed", got[len(got)-1].EventType)
	}
	if got[0].DeployID != "dep-001" {
		t.Fatalf("deploy_id mismatch = %q", got[0].DeployID)
	}
}

// TestAggregator_ScrubAppliedBeforeAppend confirms denylist terms in
// DegradedReason never reach the EventStore (Cross-Project Rule 9 +
// ADR D4a write-time scrub).
func TestAggregator_ScrubAppliedBeforeAppend(t *testing.T) {
	store := &memoryStore{}
	// Walk the FSM canonically (build → sign → release) before deploy,
	// then exercise scrub on a deploy/degraded event with denylist text.
	reader := &scriptedReader{
		id: SourceArgoCDApp,
		events: []PhaseEvent{
			{Phase: "build", Status: "in_progress", Timestamp: now()},
			{Phase: "build", Status: "completed", Timestamp: now().Add(time.Second)},
			{Phase: "sign", Status: "in_progress", Timestamp: now().Add(2 * time.Second)},
			{Phase: "sign", Status: "completed", Timestamp: now().Add(3 * time.Second)},
			{Phase: "release", Status: "in_progress", Timestamp: now().Add(4 * time.Second)},
			{Phase: "release", Status: "completed", Timestamp: now().Add(5 * time.Second)},
			{Phase: "deploy", Status: "in_progress", Timestamp: now().Add(6 * time.Second)},
			{Phase: "deploy", Status: "degraded",
				DegradedReason: "ArgoCD application sync timeout in cell-0",
				Timestamp:      now().Add(7 * time.Second)},
			{Phase: "deploy", Status: "in_progress", Timestamp: now().Add(8 * time.Second)},
			{Phase: "deploy", Status: "completed", Timestamp: now().Add(9 * time.Second)},
			{Phase: "live", Status: "in_progress", Timestamp: now().Add(10 * time.Second)},
			{Phase: "live", Status: "completed", Timestamp: now().Add(11 * time.Second)},
		},
	}
	agg := New("dep-002", store, scrub.ScrubEventWithCount, []SourceReader{reader})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	agg.Run(ctx)

	for i, e := range store.snapshot() {
		var payload map[string]any
		if err := json.Unmarshal(e.Data, &payload); err != nil {
			t.Fatalf("event[%d] payload unmarshal: %v", i, err)
		}
		if reason, ok := payload["degraded_reason"].(string); ok {
			if reason != "" && (containsAnyDenylist(reason)) {
				t.Errorf("event[%d] degraded_reason leaks denylist term: %q", i, reason)
			}
		}
	}
}

// TestAggregator_RecoverOnAdapterPanic confirms a panicking adapter
// translates to ERROR-log + DEGRADED emission + clean goroutine exit
// per ADR D1 § Drawbacks. No PANIC propagates to the caller.
func TestAggregator_RecoverOnAdapterPanic(t *testing.T) {
	store := &memoryStore{}
	agg := New("dep-003", store, scrub.ScrubEventWithCount, []SourceReader{&panickyReader{}})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("aggregator must recover panics from adapters; got %v", r)
		}
	}()
	agg.Run(ctx)

	got := store.snapshot()
	if len(got) == 0 {
		t.Fatalf("expected at least 1 DEGRADED event on panic")
	}
	if got[0].EventType != "deploy_timeline.aggregator.degraded" {
		t.Fatalf("first event = %q; want deploy_timeline.aggregator.degraded", got[0].EventType)
	}
}

// TestAggregator_OutOfOrderTransitionEmitsDegraded confirms an FSM-invalid
// transition from a buggy source emits DEGRADED instead of panicking out.
func TestAggregator_OutOfOrderTransitionEmitsDegraded(t *testing.T) {
	store := &memoryStore{}
	reader := &scriptedReader{
		id: SourceLokiMarkers,
		events: []PhaseEvent{
			// Skip build → jump to sign — FSM panics, aggregator recovers.
			{Phase: "sign", Status: "in_progress", Timestamp: now()},
		},
	}
	agg := New("dep-004", store, scrub.ScrubEventWithCount, []SourceReader{reader})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	agg.Run(ctx)

	got := store.snapshot()
	found := false
	for _, e := range got {
		if e.EventType == "deploy_timeline.aggregator.degraded" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected DEGRADED event from invalid FSM transition; got %d events", len(got))
	}
}

// TestAggregator_SourceFieldStamped confirms the aggregator attaches
// SourceReader.ID() to each emitted event's payload.
func TestAggregator_SourceFieldStamped(t *testing.T) {
	store := &memoryStore{}
	reader := &scriptedReader{
		id: SourceLoopZeroAppInformer,
		events: []PhaseEvent{
			{Phase: "build", Status: "in_progress", Timestamp: now()},
		},
	}
	agg := New("dep-005", store, scrub.ScrubEventWithCount, []SourceReader{reader})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	agg.Run(ctx)

	got := store.snapshot()
	if len(got) == 0 {
		t.Fatalf("expected at least 1 event")
	}
	var payload map[string]any
	if err := json.Unmarshal(got[0].Data, &payload); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if payload["source"] != string(SourceLoopZeroAppInformer) {
		t.Fatalf("payload.source = %v; want %v", payload["source"], SourceLoopZeroAppInformer)
	}
}

// containsAnyDenylist helper used only inside this test file.
func containsAnyDenylist(s string) bool {
	terms := []string{"ArgoCD", "argocd", "argo-cd", "cell-0", "cell-0",
		"cosign", "Kaniko", "BuildKit", "Gitea", "Webpack", "Turbopack"}
	for _, t := range terms {
		if contains(s, t) {
			return true
		}
	}
	return false
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// Compile-time assertion that statemachine package import is wired.
var _ = statemachine.DTPhaseBuild
