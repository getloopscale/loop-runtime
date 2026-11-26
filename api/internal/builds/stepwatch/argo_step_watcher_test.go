// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package stepwatch

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/sse"
)

// fakeSource emits a scripted sequence of ArgoNodeEvents synchronously.
type fakeSource struct {
	events []ArgoNodeEvent
}

func (f *fakeSource) Run(ctx context.Context, handler func(ArgoNodeEvent)) error {
	for _, ev := range f.events {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			handler(ev)
		}
	}
	return nil
}

type fakeStore struct {
	mu       sync.Mutex
	appended []fakeStoreCall
}

type fakeStoreCall struct {
	BuildID   string
	EventType string
	Data      json.RawMessage
}

func (f *fakeStore) Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appended = append(f.appended, fakeStoreCall{BuildID: buildID, EventType: eventType, Data: data})
	return int64(len(f.appended)), nil
}

func (f *fakeStore) GetAfter(ctx context.Context, buildID string, afterID int64) ([]sse.SSEEvent, error) {
	return nil, nil
}

// Plan J-17 T2.3 stub — argo_step_watcher only Appends, never reads
// project-level events. Returning empty is correct for these tests.
func (f *fakeStore) GetAfterForProject(_ context.Context, _ string, _ string, _ int64) ([]sse.SSEEvent, error) {
	return nil, nil
}

func (f *fakeStore) Prune(ctx context.Context, olderThan time.Duration) (int64, error) {
	return 0, nil
}

type fakeTerminalStore struct {
	ids map[string]struct{}
}

func (f *fakeTerminalStore) TerminalIDs(ctx context.Context) (map[string]struct{}, error) {
	return f.ids, nil
}

func newTestWatcher(source WorkflowNodeSource, store sse.EventStore, terminal *fakeTerminalStore) *ArgoStepWatcher {
	if terminal == nil {
		terminal = &fakeTerminalStore{ids: map[string]struct{}{}}
	}
	return NewArgoStepWatcher(source, store, terminal, slog.Default())
}

func TestArgoStepWatcher_PodPhaseChange_EmitsStarted(t *testing.T) {
	source := &fakeSource{events: []ArgoNodeEvent{
		{BuildID: "build-abc", NodeName: "wf-abc.clone", DisplayName: "clone-repo", NodeType: "Pod", Phase: ArgoPhaseRunning, StartedAt: time.Now()},
	}}
	store := &fakeStore{}
	w := newTestWatcher(source, store, nil)
	_ = w.Start(context.Background())
	if len(store.appended) != 1 {
		t.Fatalf("expected 1 event, got %d", len(store.appended))
	}
	if store.appended[0].EventType != "build.step.started" {
		t.Fatalf("expected build.step.started, got %s", store.appended[0].EventType)
	}
}

func TestArgoStepWatcher_PodPhaseChange_EmitsSucceeded(t *testing.T) {
	start := time.Now()
	end := start.Add(30 * time.Second)
	source := &fakeSource{events: []ArgoNodeEvent{
		{BuildID: "build-abc", NodeName: "wf-abc.clone", DisplayName: "clone-repo", NodeType: "Pod", Phase: ArgoPhaseSucceeded, StartedAt: start, FinishedAt: end},
	}}
	store := &fakeStore{}
	w := newTestWatcher(source, store, nil)
	_ = w.Start(context.Background())
	// EC-9 (LIVE 2026-06-06): when the watcher sees Succeeded without
	// having observed Running first (poll missed the transition), it
	// synthesizes a `started` event so the UI gets a consistent
	// started→succeeded lifecycle. Expect 2 events: started then succeeded.
	if len(store.appended) != 2 {
		t.Fatalf("expected 2 events (synthetic started + succeeded), got %d", len(store.appended))
	}
	if store.appended[0].EventType != "build.step.started" {
		t.Fatalf("expected build.step.started first, got %s", store.appended[0].EventType)
	}
	if store.appended[1].EventType != "build.step.succeeded" {
		t.Fatalf("expected build.step.succeeded second, got %s", store.appended[1].EventType)
	}
	// Verify duration is computed on the succeeded payload.
	var payload map[string]any
	_ = json.Unmarshal(store.appended[1].Data, &payload)
	if d, _ := payload["duration_seconds"].(float64); d < 29 || d > 31 {
		t.Fatalf("expected duration ~30s, got %v", payload["duration_seconds"])
	}
}

func TestArgoStepWatcher_PodPhaseChange_EmitsFailed(t *testing.T) {
	source := &fakeSource{events: []ArgoNodeEvent{
		{BuildID: "build-abc", NodeName: "wf-abc.build", DisplayName: "build-image-api", NodeType: "Pod", Phase: ArgoPhaseFailed, StartedAt: time.Now(), FinishedAt: time.Now(), Message: "OOMKilled"},
	}}
	store := &fakeStore{}
	w := newTestWatcher(source, store, nil)
	_ = w.Start(context.Background())
	// EC-9: synthetic started precedes a failed event when Running was
	// never observed (poll missed the transition).
	if len(store.appended) != 2 {
		t.Fatalf("expected 2 events (synthetic started + failed), got %d", len(store.appended))
	}
	if store.appended[0].EventType != "build.step.started" {
		t.Fatalf("expected build.step.started first, got %s", store.appended[0].EventType)
	}
	if store.appended[1].EventType != "build.step.failed" {
		t.Fatalf("expected build.step.failed second, got %s", store.appended[1].EventType)
	}
	var payload map[string]any
	_ = json.Unmarshal(store.appended[1].Data, &payload)
	if payload["error_class"] != "OOM" {
		t.Fatalf("expected error_class=OOM, got %v", payload["error_class"])
	}
}

func TestArgoStepWatcher_DedupSamePhase(t *testing.T) {
	now := time.Now()
	source := &fakeSource{events: []ArgoNodeEvent{
		{BuildID: "build-abc", NodeName: "wf-abc.clone", DisplayName: "clone-repo", NodeType: "Pod", Phase: ArgoPhaseRunning, StartedAt: now},
		{BuildID: "build-abc", NodeName: "wf-abc.clone", DisplayName: "clone-repo", NodeType: "Pod", Phase: ArgoPhaseRunning, StartedAt: now}, // duplicate
	}}
	store := &fakeStore{}
	w := newTestWatcher(source, store, nil)
	_ = w.Start(context.Background())
	if len(store.appended) != 1 {
		t.Fatalf("expected 1 event after dedup, got %d", len(store.appended))
	}
}

func TestArgoStepWatcher_RetryPodIgnored(t *testing.T) {
	source := &fakeSource{events: []ArgoNodeEvent{
		{BuildID: "build-abc", NodeName: "wf-abc.clone", DisplayName: "clone-repo", NodeType: "Retry", Phase: ArgoPhaseRunning, StartedAt: time.Now()},
	}}
	store := &fakeStore{}
	w := newTestWatcher(source, store, nil)
	_ = w.Start(context.Background())
	if len(store.appended) != 0 {
		t.Fatalf("expected 0 events for Retry node, got %d", len(store.appended))
	}
}

func TestArgoStepWatcher_DAGPodIgnored(t *testing.T) {
	source := &fakeSource{events: []ArgoNodeEvent{
		{BuildID: "build-abc", NodeName: "wf-abc.dag", DisplayName: "dag", NodeType: "DAG", Phase: ArgoPhaseRunning, StartedAt: time.Now()},
	}}
	store := &fakeStore{}
	w := newTestWatcher(source, store, nil)
	_ = w.Start(context.Background())
	if len(store.appended) != 0 {
		t.Fatalf("expected 0 events for DAG node, got %d", len(store.appended))
	}
}

// EC-4: initial list event storm prevention.
func TestArgoStepWatcher_InitialList_SkipsTerminalBuilds(t *testing.T) {
	source := &fakeSource{events: []ArgoNodeEvent{
		{BuildID: "build-old", NodeName: "wf.x", DisplayName: "clone-repo", NodeType: "Pod", Phase: ArgoPhaseSucceeded, StartedAt: time.Now(), FinishedAt: time.Now()},
		{BuildID: "build-new", NodeName: "wf.y", DisplayName: "clone-repo", NodeType: "Pod", Phase: ArgoPhaseRunning, StartedAt: time.Now()},
	}}
	store := &fakeStore{}
	terminal := &fakeTerminalStore{ids: map[string]struct{}{"build-old": {}}}
	w := newTestWatcher(source, store, terminal)
	_ = w.Start(context.Background())
	if len(store.appended) != 1 {
		t.Fatalf("expected 1 event (build-old skipped), got %d", len(store.appended))
	}
	if store.appended[0].BuildID != "build-new" {
		t.Fatalf("expected build-new event, got %s", store.appended[0].BuildID)
	}
}

// EC-7: cancelled-by-user class detection.
func TestArgoStepWatcher_BuildCancelledMidStep_EmitsCancelledClass(t *testing.T) {
	source := &fakeSource{events: []ArgoNodeEvent{
		{BuildID: "build-abc", NodeName: "wf-abc.build", DisplayName: "build-image-api", NodeType: "Pod", Phase: ArgoPhaseFailed, StartedAt: time.Now(), FinishedAt: time.Now(), Message: "argo: terminated"},
	}}
	store := &fakeStore{}
	w := newTestWatcher(source, store, nil)
	_ = w.Start(context.Background())
	// EC-9: synthetic started precedes the failed event.
	if len(store.appended) != 2 {
		t.Fatalf("expected 2 events (synthetic started + failed), got %d", len(store.appended))
	}
	if store.appended[0].EventType != "build.step.started" {
		t.Fatalf("expected build.step.started first, got %s", store.appended[0].EventType)
	}
	if store.appended[1].EventType != "build.step.failed" {
		t.Fatalf("expected build.step.failed second, got %s", store.appended[1].EventType)
	}
	var payload map[string]any
	_ = json.Unmarshal(store.appended[1].Data, &payload)
	if payload["error_class"] != "CANCELLED_BY_USER" {
		t.Fatalf("expected CANCELLED_BY_USER, got %v", payload["error_class"])
	}
}

// EC-8: retry after failure emits second started.
func TestArgoStepWatcher_RetryAfterFailure_EmitsSecondStarted(t *testing.T) {
	now := time.Now()
	source := &fakeSource{events: []ArgoNodeEvent{
		{BuildID: "build-abc", NodeName: "wf-abc.build", DisplayName: "build-image-api", NodeType: "Pod", Phase: ArgoPhaseRunning, StartedAt: now},
		{BuildID: "build-abc", NodeName: "wf-abc.build", DisplayName: "build-image-api", NodeType: "Pod", Phase: ArgoPhaseFailed, StartedAt: now, FinishedAt: now.Add(2 * time.Second), Message: "transient"},
		{BuildID: "build-abc", NodeName: "wf-abc.build", DisplayName: "build-image-api", NodeType: "Pod", Phase: ArgoPhasePending, StartedAt: now.Add(3 * time.Second)},
		{BuildID: "build-abc", NodeName: "wf-abc.build", DisplayName: "build-image-api", NodeType: "Pod", Phase: ArgoPhaseRunning, StartedAt: now.Add(4 * time.Second)},
	}}
	store := &fakeStore{}
	w := newTestWatcher(source, store, nil)
	_ = w.Start(context.Background())
	// Expect: started, failed, started (retry). Pending is ignored as event but resets dedup.
	if len(store.appended) != 3 {
		t.Fatalf("expected 3 events, got %d", len(store.appended))
	}
	types := []string{store.appended[0].EventType, store.appended[1].EventType, store.appended[2].EventType}
	expected := []string{"build.step.started", "build.step.failed", "build.step.started"}
	for i, e := range expected {
		if types[i] != e {
			t.Fatalf("event[%d] expected %s, got %s", i, e, types[i])
		}
	}
}

func TestArgoStepWatcher_PendingResetsDedup_NoEventEmitted(t *testing.T) {
	source := &fakeSource{events: []ArgoNodeEvent{
		{BuildID: "build-abc", NodeName: "wf.x", DisplayName: "clone-repo", NodeType: "Pod", Phase: ArgoPhasePending, StartedAt: time.Now()},
	}}
	store := &fakeStore{}
	w := newTestWatcher(source, store, nil)
	_ = w.Start(context.Background())
	if len(store.appended) != 0 {
		t.Fatalf("expected 0 events for Pending, got %d", len(store.appended))
	}
}

func TestArgoStepWatcher_SkippedPhaseIgnored(t *testing.T) {
	source := &fakeSource{events: []ArgoNodeEvent{
		{BuildID: "build-abc", NodeName: "wf.x", DisplayName: "save-cache-node", NodeType: "Pod", Phase: ArgoPhaseSkipped, StartedAt: time.Now()},
	}}
	store := &fakeStore{}
	w := newTestWatcher(source, store, nil)
	_ = w.Start(context.Background())
	if len(store.appended) != 0 {
		t.Fatalf("expected 0 events for Skipped, got %d", len(store.appended))
	}
}

func TestArgoStepWatcher_HandleDelete_EmitsWorkflowDeleted(t *testing.T) {
	source := &fakeSource{}
	store := &fakeStore{}
	w := newTestWatcher(source, store, nil)
	// HandleDelete is invoked directly by the adapter.
	w.HandleDelete(context.Background(), "build-abc", []ArgoNodeEvent{
		{BuildID: "build-abc", NodeName: "wf.x", DisplayName: "build-image-api", NodeType: "Pod", Phase: ArgoPhaseRunning, StartedAt: time.Now()},
	})
	if len(store.appended) != 1 {
		t.Fatalf("expected 1 failed event, got %d", len(store.appended))
	}
	var payload map[string]any
	_ = json.Unmarshal(store.appended[0].Data, &payload)
	if payload["error_class"] != "WORKFLOW_DELETED" {
		t.Fatalf("expected WORKFLOW_DELETED, got %v", payload["error_class"])
	}
}

func TestArgoStepWatcher_BuildIDExtractedFromWorkflowName(t *testing.T) {
	cases := map[string]string{
		"loopzero-build-build-3c02294c80a43599-gxlg7": "build-3c02294c80a43599",
		"unrelated-name": "",
		"":               "",
	}
	for input, want := range cases {
		got := BuildIDFromWorkflowName(input)
		if got != want {
			t.Errorf("BuildIDFromWorkflowName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestArgoStepWatcher_NonCanonicalStepUsesOtherLabel(t *testing.T) {
	source := &fakeSource{events: []ArgoNodeEvent{
		{BuildID: "build-abc", NodeName: "wf.x", DisplayName: "custom-prebuild-hook", NodeType: "Pod", Phase: ArgoPhaseRunning, StartedAt: time.Now()},
	}}
	store := &fakeStore{}
	w := newTestWatcher(source, store, nil)
	_ = w.Start(context.Background())
	if len(store.appended) != 1 {
		t.Fatalf("expected 1 event, got %d", len(store.appended))
	}
	var payload map[string]any
	_ = json.Unmarshal(store.appended[0].Data, &payload)
	if payload["phase_group"] != "other" {
		t.Fatalf("expected phase_group=other for custom step, got %v", payload["phase_group"])
	}
}

// EC-1 — T1.1.bis: watch error handler.
func TestArgoStepWatcher_WatchError_401_IncrementsCounter(t *testing.T) {
	w := newTestWatcher(&fakeSource{}, &fakeStore{}, nil)
	beforeStub := readArgoWatcherCounter()
	w.ReportWatchError(401, "Unauthorized")
	after := readArgoWatcherCounter()
	if after-beforeStub < 1 {
		t.Fatalf("expected counter increment, got delta=%f", after-beforeStub)
	}
}

func TestArgoStepWatcher_WatchError_403_IncrementsCounter(t *testing.T) {
	w := newTestWatcher(&fakeSource{}, &fakeStore{}, nil)
	before := readArgoWatcherCounter()
	w.ReportWatchError(403, "Forbidden")
	after := readArgoWatcherCounter()
	if after-before < 1 {
		t.Fatalf("expected counter increment on 403, got delta=%f", after-before)
	}
}

func TestArgoStepWatcher_WatchError_NetworkBlip_NoCounter(t *testing.T) {
	w := newTestWatcher(&fakeSource{}, &fakeStore{}, nil)
	before := readArgoWatcherCounter()
	w.ReportWatchError(500, "internal server")
	after := readArgoWatcherCounter()
	if after != before {
		t.Fatalf("expected counter unchanged on transient err, got delta=%f", after-before)
	}
}

func TestArgoStepWatcher_StripsRetrySuffix(t *testing.T) {
	source := &fakeSource{events: []ArgoNodeEvent{
		{BuildID: "build-abc", NodeName: "wf.x", DisplayName: "build-image-api(0)", NodeType: "Pod", Phase: ArgoPhaseRunning, StartedAt: time.Now()},
	}}
	store := &fakeStore{}
	w := newTestWatcher(source, store, nil)
	_ = w.Start(context.Background())
	if len(store.appended) != 1 {
		t.Fatalf("expected 1 event, got %d", len(store.appended))
	}
	var payload map[string]any
	_ = json.Unmarshal(store.appended[0].Data, &payload)
	if payload["step_name"] != "build-image-api" {
		t.Fatalf("expected step_name=build-image-api (suffix stripped), got %v", payload["step_name"])
	}
}
