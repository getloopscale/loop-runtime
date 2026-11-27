// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package timelinehttp

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// memDeployTimelineStore is a hermetic in-memory store satisfying
// DeployTimelineEventStore + DeployLookup. Provides controllable event
// flow for SSE handler testing.
type memDeployTimelineStore struct {
	mu       sync.Mutex
	events   []DeployTimelineEvent
	tenantID string // expected tenant for GetByDeployID
	deployID string
}

func (m *memDeployTimelineStore) DeployTenantID(ctx context.Context, deployID string) (string, error) {
	if deployID != m.deployID {
		return "", ErrDeployNotFound
	}
	return m.tenantID, nil
}

func (m *memDeployTimelineStore) GetAfterForDeploy(ctx context.Context, tenantID, deployID string, afterID int64) ([]DeployTimelineEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if tenantID != m.tenantID || deployID != m.deployID {
		return nil, ErrDeployNotFound
	}
	out := make([]DeployTimelineEvent, 0)
	for _, e := range m.events {
		if e.ID > afterID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (m *memDeployTimelineStore) appendEvent(eventType, payload string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, DeployTimelineEvent{
		ID:        int64(len(m.events) + 1),
		EventType: eventType,
		Data:      json.RawMessage(payload),
		CreatedAt: time.Now(),
	})
}

// TestDeployTimelineSSE_StreamsCanonicalEvents drives the handler with
// 3 prepopulated events and confirms they arrive in order with id: lines.
func TestDeployTimelineSSE_StreamsCanonicalEvents(t *testing.T) {
	store := &memDeployTimelineStore{
		tenantID: "tn-test",
		deployID: "dep-001",
	}
	store.appendEvent("deploy_timeline.build.in_progress", `{"deploy_id":"dep-001","phase":"build","status":"in_progress"}`)
	store.appendEvent("deploy_timeline.build.completed", `{"deploy_id":"dep-001","phase":"build","status":"completed"}`)
	store.appendEvent("deploy_timeline.live.completed", `{"deploy_id":"dep-001","phase":"live","status":"completed"}`)

	handler := NewDeployTimelineHandler(store,
		WithDTPollInterval(10*time.Millisecond),
		WithDTMaxDuration(2*time.Second),
	)

	r := chi.NewRouter()
	r.Get("/api/v1/deploys/{deployID}/timeline", func(w http.ResponseWriter, req *http.Request) {
		// Inject tenant via context for hermetic testing (real handler reads from middleware).
		ctx := withTestTenant(req.Context(), "tn-test")
		handler.Stream(w, req.WithContext(ctx))
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/deploys/dep-001/timeline")
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q; want text/event-stream", got)
	}

	scanner := bufio.NewScanner(resp.Body)
	var lines []string
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	done := make(chan struct{})
	go func() {
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		close(done)
	}()
	select {
	case <-done:
	case <-deadline.C:
		t.Fatalf("stream did not close in time; got %d lines", len(lines))
	}

	// Expect exactly 3 `id:` lines + 3 `event:` lines.
	idCount, eventCount := 0, 0
	terminal := false
	for _, l := range lines {
		if strings.HasPrefix(l, "id:") {
			idCount++
		}
		if strings.HasPrefix(l, "event:") {
			eventCount++
		}
		if strings.Contains(l, "deploy_timeline.live.completed") {
			terminal = true
		}
	}
	if idCount < 3 || eventCount < 3 {
		t.Errorf("expected ≥3 id:+event: lines; got id=%d event=%d (lines=%v)", idCount, eventCount, lines)
	}
	if !terminal {
		t.Errorf("expected terminal event deploy_timeline.live.completed in stream")
	}
}

// TestDeployTimelineSSE_LastEventID_Dedup confirms the handler honors
// the Last-Event-ID header per EC-1 of the plan: events with id ≤ header
// are NOT re-sent (idempotent reconnect).
func TestDeployTimelineSSE_LastEventID_Dedup(t *testing.T) {
	store := &memDeployTimelineStore{
		tenantID: "tn-test",
		deployID: "dep-002",
	}
	store.appendEvent("deploy_timeline.build.in_progress", `{}`)
	store.appendEvent("deploy_timeline.build.completed", `{}`)
	store.appendEvent("deploy_timeline.live.completed", `{}`)

	handler := NewDeployTimelineHandler(store,
		WithDTPollInterval(10*time.Millisecond),
		WithDTMaxDuration(2*time.Second),
	)

	r := chi.NewRouter()
	r.Get("/api/v1/deploys/{deployID}/timeline", func(w http.ResponseWriter, req *http.Request) {
		ctx := withTestTenant(req.Context(), "tn-test")
		handler.Stream(w, req.WithContext(ctx))
	})
	srv := httptest.NewServer(r)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/deploys/dep-002/timeline", nil)
	req.Header.Set("Last-Event-ID", "2")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	idLines := []string{}
	done := make(chan struct{})
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "id:") {
				idLines = append(idLines, line)
			}
		}
		close(done)
	}()
	<-done

	if len(idLines) != 1 {
		t.Fatalf("expected exactly 1 id: line (event 3); got %d (%v)", len(idLines), idLines)
	}
	if !strings.Contains(idLines[0], "3") {
		t.Errorf("expected id:3 line; got %q", idLines[0])
	}
}

// TestDeployTimelineSSE_TenantMismatch_Returns404 confirms tenant filter
// (EC-3 / ADR D8): GET with wrong tenant context returns 404, never 403
// (avoid leaking deploy_id existence).
func TestDeployTimelineSSE_TenantMismatch_Returns404(t *testing.T) {
	store := &memDeployTimelineStore{tenantID: "tn-owner", deployID: "dep-003"}
	handler := NewDeployTimelineHandler(store, WithDTMaxDuration(time.Second))

	r := chi.NewRouter()
	r.Get("/api/v1/deploys/{deployID}/timeline", func(w http.ResponseWriter, req *http.Request) {
		ctx := withTestTenant(req.Context(), "tn-attacker")
		handler.Stream(w, req.WithContext(ctx))
	})
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/deploys/dep-003/timeline")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d; want 404", resp.StatusCode)
	}
}

// TestDeployTimelineSSE_FiltersNonTimelineEvents confirms LIVE-caught
// 2026-06-18 fix: the sse_events table is shared with the J-17
// build.step.* stream; the handler MUST emit only events whose type
// begins with "deploy_timeline.".
func TestDeployTimelineSSE_FiltersNonTimelineEvents(t *testing.T) {
	store := &memDeployTimelineStore{
		tenantID: "tn-test",
		deployID: "dep-filter",
	}
	store.appendEvent("build.started", `{"build_id":"dep-filter"}`)
	store.appendEvent("build.step.succeeded", `{"step_name":"clone-repo"}`)
	store.appendEvent("deploy_timeline.build.in_progress", `{"phase":"build"}`)
	store.appendEvent("deploy_timeline.live.completed", `{"phase":"live"}`)

	handler := NewDeployTimelineHandler(store,
		WithDTPollInterval(10*time.Millisecond),
		WithDTMaxDuration(2*time.Second),
	)
	r := chi.NewRouter()
	r.Get("/api/v1/deploys/{deployID}/timeline", func(w http.ResponseWriter, req *http.Request) {
		ctx := withTestTenant(req.Context(), "tn-test")
		handler.Stream(w, req.WithContext(ctx))
	})
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/deploys/dep-filter/timeline")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	body := []string{}
	done := make(chan struct{})
	go func() {
		for scanner.Scan() {
			body = append(body, scanner.Text())
		}
		close(done)
	}()
	<-done

	full := strings.Join(body, "\n")
	if strings.Contains(full, "event: build.started") {
		t.Errorf("non-timeline event leaked: build.started")
	}
	if strings.Contains(full, "event: build.step.succeeded") {
		t.Errorf("non-timeline event leaked: build.step.succeeded")
	}
	if !strings.Contains(full, "event: deploy_timeline.build.in_progress") {
		t.Errorf("expected deploy_timeline.build.in_progress in stream; got:\n%s", full)
	}
	if !strings.Contains(full, "event: deploy_timeline.live.completed") {
		t.Errorf("expected deploy_timeline.live.completed in stream; got:\n%s", full)
	}
}

// TestDeployTimelineSSE_UnknownDeploy_Returns404 confirms unknown deploy_id
// returns 404 even when tenant matches nothing.
func TestDeployTimelineSSE_UnknownDeploy_Returns404(t *testing.T) {
	store := &memDeployTimelineStore{tenantID: "tn-test", deployID: "dep-real"}
	handler := NewDeployTimelineHandler(store, WithDTMaxDuration(time.Second))

	r := chi.NewRouter()
	r.Get("/api/v1/deploys/{deployID}/timeline", func(w http.ResponseWriter, req *http.Request) {
		ctx := withTestTenant(req.Context(), "tn-test")
		handler.Stream(w, req.WithContext(ctx))
	})
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/deploys/dep-fake/timeline")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d; want 404", resp.StatusCode)
	}
}

// withTestTenant is a hermetic helper — the production handler reads
// tenant from middleware context (same key).
func withTestTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, deployTimelineTenantCtxKey, tenantID)
}
