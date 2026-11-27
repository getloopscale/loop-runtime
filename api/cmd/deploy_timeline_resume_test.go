// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package main

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

// memoryTimelineStore is the sse_events journal of the tests: one slice, with
// the two write paths and the listing the resumer reads.
type memoryTimelineStore struct {
	mu     sync.Mutex
	events []ports.SSEEvent
}

func (m *memoryTimelineStore) add(buildID, eventType string, createdAt time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, ports.SSEEvent{
		ID: int64(len(m.events) + 1), BuildID: buildID, EventType: eventType,
		Data: json.RawMessage(`{}`), CreatedAt: createdAt,
	})
}

func (m *memoryTimelineStore) Append(_ context.Context, buildID, eventType string, _ json.RawMessage) (int64, error) {
	m.add(buildID, eventType, time.Now().UTC())
	return int64(len(m.events)), nil
}

func (m *memoryTimelineStore) AppendOnce(ctx context.Context, buildID, eventType string, data json.RawMessage) (int64, bool, error) {
	m.mu.Lock()
	for _, ev := range m.events {
		if ev.BuildID == buildID && ev.EventType == eventType {
			m.mu.Unlock()
			return 0, false, nil
		}
	}
	m.mu.Unlock()
	id, err := m.Append(ctx, buildID, eventType, data)
	return id, true, err
}

func (m *memoryTimelineStore) GetAfter(_ context.Context, buildID string, afterID int64) ([]ports.SSEEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []ports.SSEEvent
	for _, ev := range m.events {
		if ev.BuildID == buildID && ev.ID > afterID {
			out = append(out, ev)
		}
	}
	return out, nil
}

func (m *memoryTimelineStore) ListBuildsWithoutLiveTimeline(_ context.Context, _ time.Time) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	live := map[string]bool{}
	var started []string
	for _, ev := range m.events {
		switch ev.EventType {
		case "deploy_timeline.live.completed":
			live[ev.BuildID] = true
		case "build.started":
			started = append(started, ev.BuildID)
		}
	}
	var out []string
	for _, id := range started {
		if !live[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

func (m *memoryTimelineStore) typesOf(buildID string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, ev := range m.events {
		if ev.BuildID == buildID {
			out = append(out, ev.EventType)
		}
	}
	return out
}

// interruptedDeploy writes the stream of a deploy whose aggregator stopped
// after `live` began: the orchestrator finished, and the timeline never said so.
func interruptedDeploy(m *memoryTimelineStore, buildID string, at time.Time) {
	for _, eventType := range []string{
		"build.started",
		"deploy_timeline.build.in_progress",
		"build.succeeded",
		"deploy_timeline.build.completed",
		"deploy_timeline.sign.in_progress",
		"deploy_timeline.sign.completed",
		"deploy.delivering",
		"deploy_timeline.release.in_progress",
		"deploy.verifying",
		"deploy_timeline.release.completed",
		"deploy_timeline.deploy.in_progress",
		"deploy.live",
		"deploy.completed",
		"deploy_timeline.deploy.completed",
		"deploy_timeline.live.in_progress",
	} {
		m.add(buildID, eventType, at)
	}
}

func TestUnwrittenTimelineStates_FindsTheStateARestartLeftOut(t *testing.T) {
	m := &memoryTimelineStore{}
	now := time.Now().UTC()
	interruptedDeploy(m, "build-1", now.Add(-10*time.Minute))

	got := unwrittenTimelineStates(m.events, now.Add(-30*time.Second))

	if want := []string{"LIVE_COMPLETED"}; !reflect.DeepEqual(got, want) {
		t.Errorf("unwritten states = %v; want %v", got, want)
	}
}

// An aggregator that is alive writes a state a few seconds after the event
// behind it. Inside that window nothing is missing yet.
func TestUnwrittenTimelineStates_IgnoresEventsYoungerThanTheCutoff(t *testing.T) {
	m := &memoryTimelineStore{}
	now := time.Now().UTC()
	interruptedDeploy(m, "build-1", now.Add(-5*time.Second))

	if got := unwrittenTimelineStates(m.events, now.Add(-30*time.Second)); len(got) != 0 {
		t.Errorf("unwritten states = %v; want none", got)
	}
}

func newTestResumer(m *memoryTimelineStore) *timelineResumer {
	r := newTimelineResumer(m, nil)
	r.pollInterval = 2 * time.Millisecond
	return r
}

func waitForEvent(t *testing.T, m *memoryTimelineStore, buildID, eventType string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, got := range m.typesOf(buildID) {
			if got == eventType {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("stream of %s never got %s; it holds %v", buildID, eventType, m.typesOf(buildID))
}

func TestTimelineResumer_FinishesATimelineARestartLeftOpen(t *testing.T) {
	m := &memoryTimelineStore{}
	interruptedDeploy(m, "build-1", time.Now().UTC().Add(-10*time.Minute))
	before := len(m.typesOf("build-1"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	newTestResumer(m).tick(ctx)
	waitForEvent(t, m, "build-1", "deploy_timeline.live.completed")

	after := m.typesOf("build-1")
	if len(after) != before+1 {
		t.Errorf("the resume wrote %d events; want 1 (the stream holds %v)", len(after)-before, after)
	}
}

func TestTimelineResumer_LeavesADeployInFlightAlone(t *testing.T) {
	m := &memoryTimelineStore{}
	interruptedDeploy(m, "build-1", time.Now().UTC().Add(-5*time.Second))
	r := newTestResumer(m)

	r.tick(context.Background())

	if r.attempted["build-1"] {
		t.Error("the resumer took a deploy whose last events are 5 s old")
	}
}

// A timeline the state machine refuses to finish stays unfinished. Taking it
// again on every pass would add a degraded event each time.
func TestTimelineResumer_TakesADeployOnlyOnce(t *testing.T) {
	m := &memoryTimelineStore{}
	interruptedDeploy(m, "build-1", time.Now().UTC().Add(-10*time.Minute))
	started := make(chan string, 4)
	r := newTestResumer(m)
	r.run = func(_ context.Context, buildID string) { started <- buildID }

	r.tick(context.Background())
	r.tick(context.Background())

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the resumer never started an aggregator for the deploy")
	}
	select {
	case <-started:
		t.Error("the resumer started a second aggregator for the same deploy")
	case <-time.After(50 * time.Millisecond):
	}
}
