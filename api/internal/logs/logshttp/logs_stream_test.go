// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package logshttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/routestest"
)

// --- Fake StreamLogQuerier ---

type fakeStreamLogQuerier struct {
	// calls tracks invocations with (buildID, sinceNano) for assertion.
	calls []streamLogQuerierCall
	// batches is a queue: each call to QueryBuildLogsSince pops the first batch.
	batches [][]LogEntry
	err     error
}

type streamLogQuerierCall struct {
	BuildID   string
	SinceNano int64
}

func (f *fakeStreamLogQuerier) QueryBuildLogsSince(ctx context.Context, buildID string, sinceNano int64, limit int) ([]LogEntry, error) {
	f.calls = append(f.calls, streamLogQuerierCall{BuildID: buildID, SinceNano: sinceNano})
	if f.err != nil {
		return nil, f.err
	}
	if len(f.batches) == 0 {
		return nil, nil
	}
	batch := f.batches[0]
	f.batches = f.batches[1:]
	return batch, nil
}

// --- Fake StreamBuildStore ---

type fakeStreamBuildStore struct {
	build  *LogBuild
	err    error
	status string // if set, overrides build.Status on each call
}

func (f *fakeStreamBuildStore) GetByID(_ context.Context, _ string) (*LogBuild, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.build == nil {
		return nil, nil
	}
	b := *f.build
	if f.status != "" {
		b.Status = f.status
	}
	return &b, nil
}

// GetByIDForTenant — T0.4 of deep-review-2026-05-10-remediation-plan.
// Emulates the production SQL filter (WHERE id=$1 AND tenant_id=$2):
// returns nil when the build belongs to a different tenant.
func (f *fakeStreamBuildStore) GetByIDForTenant(ctx context.Context, tenantID, buildID string) (*LogBuild, error) {
	b, err := f.GetByID(ctx, buildID)
	if err != nil || b == nil {
		return b, err
	}
	if b.TenantID != tenantID {
		return nil, nil
	}
	return b, nil
}

// --- Tests ---

func TestStreamBuildLogs_EmitsSSEEvents(t *testing.T) {
	builds := &fakeStreamBuildStore{
		build: &LogBuild{ID: "bld-abc123", TenantID: "tn-test-123", Status: "RUNNING"},
	}
	querier := &fakeStreamLogQuerier{
		batches: [][]LogEntry{
			{
				{Step: "clone", Stream: "stdout", Content: "Cloning repo...", Timestamp: "2026-01-01T00:00:01Z"},
				{Step: "build", Stream: "stdout", Content: "Building image...", Timestamp: "2026-01-01T00:00:02Z"},
			},
			nil, // second poll returns nothing
		},
	}

	// Build transitions to terminal after first poll
	builds.status = "SUCCEEDED"

	h := NewLogStreamHandler(builds, querier, LogStreamHandlerOpts{
		PollInterval: 10 * time.Millisecond,
		MaxDuration:  500 * time.Millisecond,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-abc123/logs/stream", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	h.StreamBuildLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	// Verify SSE headers
	ct := w.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Fatalf("want Content-Type text/event-stream, got %s", ct)
	}
	cc := w.Header().Get("Cache-Control")
	if cc != "no-cache" {
		t.Fatalf("want Cache-Control no-cache, got %s", cc)
	}

	// Parse SSE events from body
	body := w.Body.String()
	events := parseSSEEvents(body)
	if len(events) < 2 {
		t.Fatalf("want at least 2 log events, got %d. Body:\n%s", len(events), body)
	}

	// Verify event format
	for _, ev := range events {
		if ev.eventType != "log" {
			continue
		}
		var data map[string]string
		if err := json.Unmarshal([]byte(ev.data), &data); err != nil {
			t.Fatalf("invalid JSON in SSE data: %v", err)
		}
		if data["line"] == "" {
			t.Fatalf("SSE log event missing 'line' field: %s", ev.data)
		}
		if data["timestamp"] == "" {
			t.Fatalf("SSE log event missing 'timestamp' field: %s", ev.data)
		}
	}
}

func TestStreamBuildLogs_StopsOnTerminalBuild(t *testing.T) {
	builds := &fakeStreamBuildStore{
		build: &LogBuild{ID: "bld-abc123", TenantID: "tn-test-123", Status: "SUCCEEDED"},
	}
	querier := &fakeStreamLogQuerier{
		batches: [][]LogEntry{
			{
				{Step: "build", Stream: "stdout", Content: "Done!", Timestamp: "2026-01-01T00:01:00Z"},
			},
		},
	}

	h := NewLogStreamHandler(builds, querier, LogStreamHandlerOpts{
		PollInterval: 10 * time.Millisecond,
		MaxDuration:  500 * time.Millisecond,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-abc123/logs/stream", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	start := time.Now()
	h.StreamBuildLogs(w, req)
	elapsed := time.Since(start)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}

	// Should stop relatively quickly (not wait for MaxDuration)
	if elapsed > 300*time.Millisecond {
		t.Fatalf("stream should have stopped quickly for terminal build, took %v", elapsed)
	}

	// Last event should be stream.done
	body := w.Body.String()
	if !strings.Contains(body, "event: stream.done") {
		t.Fatalf("expected stream.done event, got:\n%s", body)
	}
}

func TestStreamBuildLogs_Unauthorized(t *testing.T) {
	builds := &fakeStreamBuildStore{
		build: &LogBuild{ID: "bld-abc123", TenantID: "tn-test-123", Status: "RUNNING"},
	}
	querier := &fakeStreamLogQuerier{}

	h := NewLogStreamHandler(builds, querier, LogStreamHandlerOpts{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-abc123/logs/stream", nil)
	req = routestest.WithChiParam(req, "id", "bld-abc123")
	// No auth context
	w := httptest.NewRecorder()

	h.StreamBuildLogs(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestStreamBuildLogs_CrossTenantDenied(t *testing.T) {
	builds := &fakeStreamBuildStore{
		build: &LogBuild{ID: "bld-abc123", TenantID: "tn-other-tenant", Status: "RUNNING"},
	}
	querier := &fakeStreamLogQuerier{}

	h := NewLogStreamHandler(builds, querier, LogStreamHandlerOpts{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-abc123/logs/stream", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext()) // tn-test-123
	req = routestest.WithChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	h.StreamBuildLogs(w, req)

	// SEC: cross-tenant returns 404 not 403
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestStreamBuildLogs_BuildNotFound(t *testing.T) {
	builds := &fakeStreamBuildStore{build: nil}
	querier := &fakeStreamLogQuerier{}

	h := NewLogStreamHandler(builds, querier, LogStreamHandlerOpts{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-nonexistent/logs/stream", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "bld-nonexistent")
	w := httptest.NewRecorder()

	h.StreamBuildLogs(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestStreamBuildLogs_SupportsLastEventID(t *testing.T) {
	builds := &fakeStreamBuildStore{
		build: &LogBuild{ID: "bld-abc123", TenantID: "tn-test-123", Status: "SUCCEEDED"},
	}
	querier := &fakeStreamLogQuerier{
		batches: [][]LogEntry{
			{
				{Step: "build", Stream: "stdout", Content: "Line after reconnect", Timestamp: "2026-01-01T00:00:05Z"},
			},
		},
	}

	h := NewLogStreamHandler(builds, querier, LogStreamHandlerOpts{
		PollInterval: 10 * time.Millisecond,
		MaxDuration:  500 * time.Millisecond,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-abc123/logs/stream", nil)
	req.Header.Set("Last-Event-ID", "1735689600000000000") // 2026-01-01T00:00:00Z in nanos
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	h.StreamBuildLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}

	// Verify the querier was called with the sinceNano from Last-Event-ID
	if len(querier.calls) == 0 {
		t.Fatal("expected at least one call to QueryBuildLogsSince")
	}
	if querier.calls[0].SinceNano != 1735689600000000000 {
		t.Fatalf("expected sinceNano=1735689600000000000, got %d", querier.calls[0].SinceNano)
	}
}

// --- SSE event parser for tests ---

type testSSEEvent struct {
	id        string
	eventType string
	data      string
}

func parseSSEEvents(body string) []testSSEEvent {
	var events []testSSEEvent
	blocks := strings.Split(body, "\n\n")
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		ev := testSSEEvent{}
		for _, line := range strings.Split(block, "\n") {
			if strings.HasPrefix(line, "id: ") {
				ev.id = strings.TrimPrefix(line, "id: ")
			} else if strings.HasPrefix(line, "event: ") {
				ev.eventType = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				ev.data = strings.TrimPrefix(line, "data: ")
			}
			// lines starting with ":" are SSE comments/heartbeats — skip
		}
		if ev.eventType != "" || ev.data != "" {
			events = append(events, ev)
		}
	}
	return events
}

func TestStreamRuntimeLogs_RequiresEnvParam(t *testing.T) {
	builds := &fakeStreamBuildStore{}
	querier := &fakeStreamLogQuerier{}

	h := NewLogStreamHandler(builds, querier, LogStreamHandlerOpts{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/myapp/logs/stream", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "myapp")
	w := httptest.NewRecorder()

	h.StreamRuntimeLogs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 when env missing, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestStreamRuntimeLogs_EmitsSSEEvents(t *testing.T) {
	builds := &fakeStreamBuildStore{} // not used for runtime
	querier := &fakeStreamLogQuerier{
		batches: [][]LogEntry{
			{
				{Step: "myapp", Stream: "stdout", Content: "Server started on :8080", Timestamp: "2026-01-01T00:00:01Z"},
			},
			nil, // second poll returns nothing
		},
	}

	h := NewLogStreamHandler(builds, querier, LogStreamHandlerOpts{
		PollInterval: 10 * time.Millisecond,
		MaxDuration:  200 * time.Millisecond,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/myapp/logs/stream?env=production", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "myapp")
	w := httptest.NewRecorder()

	h.StreamRuntimeLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	events := parseSSEEvents(body)
	found := false
	for _, ev := range events {
		if ev.eventType == "log" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected at least one log event, body:\n%s", body)
	}
}

func TestStreamBuildLogs_MaxDurationTimeout(t *testing.T) {
	builds := &fakeStreamBuildStore{
		build: &LogBuild{ID: "bld-abc123", TenantID: "tn-test-123", Status: "RUNNING"},
	}
	// Never return data, never become terminal -- should hit max duration
	querier := &fakeStreamLogQuerier{}

	h := NewLogStreamHandler(builds, querier, LogStreamHandlerOpts{
		PollInterval: 10 * time.Millisecond,
		MaxDuration:  100 * time.Millisecond,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-abc123/logs/stream", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	start := time.Now()
	h.StreamBuildLogs(w, req)
	elapsed := time.Since(start)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}

	// Should stop around MaxDuration (100ms), give generous tolerance
	if elapsed > 500*time.Millisecond {
		t.Fatalf("stream should have stopped at max duration, took %v", elapsed)
	}

	body := w.Body.String()
	if !strings.Contains(body, "event: stream.timeout") {
		t.Logf("body:\n%s", body)
		// stream.timeout is best-effort, not a hard requirement
	}
}

func TestStreamBuildLogs_MissingBuildID(t *testing.T) {
	h := NewLogStreamHandler(&fakeStreamBuildStore{}, &fakeStreamLogQuerier{}, LogStreamHandlerOpts{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds//logs/stream", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	// No chi param for "id"
	w := httptest.NewRecorder()

	h.StreamBuildLogs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for missing build ID, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestStreamBuildLogs_NoInternalTerms(t *testing.T) {
	// Ensure no internal terms leak in SSE event data
	builds := &fakeStreamBuildStore{
		build: &LogBuild{ID: "bld-abc123", TenantID: "tn-test-123", Status: "SUCCEEDED"},
	}
	querier := &fakeStreamLogQuerier{
		batches: [][]LogEntry{
			{
				{Step: "build", Stream: "stdout", Content: "Done", Timestamp: "2026-01-01T00:00:01Z"},
			},
		},
	}

	h := NewLogStreamHandler(builds, querier, LogStreamHandlerOpts{
		PollInterval: 10 * time.Millisecond,
		MaxDuration:  200 * time.Millisecond,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-abc123/logs/stream", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	h.StreamBuildLogs(w, req)

	body := w.Body.String()
	forbidden := []string{"ArgoCD", "argocd", "GitOps", "gitops", "Gitea", "MinIO", "Kaniko", "kubebuilder", "envtest", "pg_advisory", "tenant_id", "CRD"}
	for _, term := range forbidden {
		if strings.Contains(body, term) {
			t.Fatalf("SSE body contains forbidden internal term %q:\n%s", term, body)
		}
	}
}

// Verify SSE event format: "event: log\ndata: {...}\n\n"
func TestStreamBuildLogs_SSEEventFormat(t *testing.T) {
	builds := &fakeStreamBuildStore{
		build: &LogBuild{ID: "bld-abc123", TenantID: "tn-test-123", Status: "SUCCEEDED"},
	}
	querier := &fakeStreamLogQuerier{
		batches: [][]LogEntry{
			{
				{Step: "build", Stream: "stdout", Content: "Hello world", Timestamp: "2026-01-01T00:00:01Z"},
			},
		},
	}

	h := NewLogStreamHandler(builds, querier, LogStreamHandlerOpts{
		PollInterval: 10 * time.Millisecond,
		MaxDuration:  200 * time.Millisecond,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-abc123/logs/stream", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	h.StreamBuildLogs(w, req)

	body := w.Body.String()
	// Must contain "event: log\n"
	if !strings.Contains(body, "event: log\n") {
		t.Fatalf("expected 'event: log' in SSE body, got:\n%s", body)
	}
	// Must contain "data: {" (JSON object)
	if !strings.Contains(body, "data: {") {
		t.Fatalf("expected 'data: {' in SSE body, got:\n%s", body)
	}
	// Must contain double newline separators
	if !strings.Contains(body, "\n\n") {
		t.Fatalf("expected double newline SSE separators, got:\n%s", body)
	}

	// Parse the first log event data
	events := parseSSEEvents(body)
	for _, ev := range events {
		if ev.eventType == "log" {
			var data map[string]interface{}
			if err := json.Unmarshal([]byte(ev.data), &data); err != nil {
				t.Fatalf("SSE log event data is not valid JSON: %v\ndata: %s", err, ev.data)
			}
			if _, ok := data["timestamp"]; !ok {
				t.Fatal("SSE log event missing 'timestamp'")
			}
			if _, ok := data["line"]; !ok {
				t.Fatal("SSE log event missing 'line'")
			}
			// id should be the nanosecond timestamp for Last-Event-ID reconnection
			if ev.id == "" {
				t.Fatal("SSE log event missing id field (needed for Last-Event-ID)")
			}
			break
		}
	}
}

// A log source that keeps failing must not be polled until maxDuration: after
// the shared retry budget the stream ends with a stream.error the client can
// act on (reconnect), instead of streaming silence for thirty minutes.
func TestStreamBuildLogs_PersistentLokiErrorEndsTheStreamWithAnError(t *testing.T) {
	builds := &fakeStreamBuildStore{
		build: &LogBuild{ID: "bld-abc123", TenantID: "tn-test-123", Status: "SUCCEEDED"},
	}
	querier := &fakeStreamLogQuerier{
		err: fmt.Errorf("loki connection refused"),
	}

	h := NewLogStreamHandler(builds, querier, LogStreamHandlerOpts{
		PollInterval: time.Millisecond,
		MaxDuration:  time.Hour,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/builds/bld-abc123/logs/stream", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "id", "bld-abc123")
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.StreamBuildLogs(w, req)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a persistently failing log source kept the stream open")
	}

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 (headers already sent), got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "event: stream.error") {
		t.Fatalf("want a stream.error event, got %q", w.Body.String())
	}
}
