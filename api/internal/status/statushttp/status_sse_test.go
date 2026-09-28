// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statushttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/middleware/auth/authtest"
	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/api/internal/status/statushttp"

	"github.com/go-chi/chi/v5"

	"github.com/getloopscale/loopzero/api/internal/sse"
)

// T2.1 of plan 2026-06-15-j18-t1.3.b-sse-pg-notify-backend v1.1.
//
// Hermetic tests for StatusSSEHandler. Covers 7 named cases asserting
// the EC absorptions:
//  - EC-7 cross-tenant returns 404 NOT 403 (existence-leak-prevention)
//  - EC-5 Flusher comma-ok form (500 not panic when Flusher missing)
//  - EC-2 (signal.changed of one env only) is the store query's job and is
//    tested on PostgresEventStore.GetSignalsAfter in internal/db/store
//  - EC-9 Last-Event-ID dedup by id
//  - validation: env query required (per memory feedback_j19)
//  - happy path: SSE wire format named event + Flush per event
//  - auth: 401 without session ctx

// fakeStatusSSEEventStore captures invocations + emits scripted events.
type fakeStatusSSEEventStore struct {
	events   []sse.SSEEvent
	calls    atomic.Int32
	err      error
	emitOnce atomic.Bool // emit events on first call only (avoid infinite loop in tests)
}

func (f *fakeStatusSSEEventStore) GetSignalsAfter(_ context.Context, _, _, _ string, afterID int64) ([]sse.SSEEvent, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	if !f.emitOnce.CompareAndSwap(false, true) {
		return nil, nil
	}
	out := make([]sse.SSEEvent, 0, len(f.events))
	for _, ev := range f.events {
		if ev.ID > afterID {
			out = append(out, ev)
		}
	}
	return out, nil
}

// nonFlushingResponseWriter wraps httptest.ResponseRecorder but does NOT
// implement http.Flusher. Used by EC-5 test to assert handler returns 500
// gracefully instead of panicking.
type nonFlushingResponseWriter struct {
	header http.Header
	code   int
	body   strings.Builder
}

func (w *nonFlushingResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *nonFlushingResponseWriter) Write(b []byte) (int, error) { return w.body.Write(b) }
func (w *nonFlushingResponseWriter) WriteHeader(code int)        { w.code = code }

// newSSERequest creates a GET request for the status_sse endpoint with
// chi URL params + optional auth ctx.
func newSSERequest(t *testing.T, projectName, env string, authenticated bool) *http.Request {
	t.Helper()
	url := "/api/v1/projects/" + projectName + "/signals/stream"
	if env != "" {
		url += "?env=" + env
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", projectName)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	if authenticated {
		req = req.WithContext(authtest.Context("github-lucas", "acme", "owner"))
		rctx2 := chi.NewRouteContext()
		rctx2.URLParams.Add("name", projectName)
		ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx2)
		req = req.WithContext(ctx)
	}
	return req
}

func TestStatusSSE_Unauthorized(t *testing.T) {
	t.Parallel()

	projects := &fakeStatusProjectStore{}
	store := &fakeStatusSSEEventStore{}
	handler := statushttp.NewStatusSSEHandler(projects, store, statushttp.StatusSSEHandlerOpts{
		PollInterval: 1 * time.Millisecond,
		MaxDuration:  10 * time.Millisecond,
	})

	req := newSSERequest(t, "my-app", "production", false)
	rec := httptest.NewRecorder()
	handler.Get(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStatusSSE_CrossTenantReturns404(t *testing.T) {
	t.Parallel()

	// EC-7 absorption: cross-tenant project lookup returns nil → 404 NOT 403
	// (existence-leak-prevention per T1.2 EC-2). Exact status code assertion.
	projects := &fakeStatusProjectStore{project: nil}
	store := &fakeStatusSSEEventStore{}
	handler := statushttp.NewStatusSSEHandler(projects, store, statushttp.StatusSSEHandlerOpts{
		PollInterval: 1 * time.Millisecond,
		MaxDuration:  10 * time.Millisecond,
	})

	req := newSSERequest(t, "other-tenant-project", "production", true)
	rec := httptest.NewRecorder()
	handler.Get(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 NOT_FOUND on cross-tenant, got %d: %s", rec.Code, rec.Body.String())
	}
	// Defense: event store must NOT be queried on cross-tenant 404.
	if store.calls.Load() != 0 {
		t.Errorf("EventStore.GetAfterForProject must NOT be called on cross-tenant 404; got %d calls", store.calls.Load())
	}
}

func TestStatusSSE_MissingEnvReturns400(t *testing.T) {
	t.Parallel()

	// Per memory feedback_j19_runtime_logs_stream_missing_env_param: env
	// query parameter is mandatory; missing → 400 invalid_field.
	projects := &fakeStatusProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-app", TeamID: "acme"},
	}
	store := &fakeStatusSSEEventStore{}
	handler := statushttp.NewStatusSSEHandler(projects, store, statushttp.StatusSSEHandlerOpts{
		PollInterval: 1 * time.Millisecond,
		MaxDuration:  10 * time.Millisecond,
	})

	req := newSSERequest(t, "my-app", "", true) // env intentionally empty
	rec := httptest.NewRecorder()
	handler.Get(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on missing env, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStatusSSE_500WhenFlusherUnavailable(t *testing.T) {
	t.Parallel()

	// EC-5 absorption: ResponseWriter without Flusher → 500 graceful, NOT panic.
	projects := &fakeStatusProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-app", TeamID: "acme"},
	}
	store := &fakeStatusSSEEventStore{}
	handler := statushttp.NewStatusSSEHandler(projects, store, statushttp.StatusSSEHandlerOpts{
		PollInterval: 1 * time.Millisecond,
		MaxDuration:  10 * time.Millisecond,
	})

	req := newSSERequest(t, "my-app", "production", true)
	rec := &nonFlushingResponseWriter{}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("handler must NOT panic on missing Flusher, got panic: %v", r)
		}
	}()
	handler.Get(rec, req)

	if rec.code != http.StatusInternalServerError {
		t.Errorf("expected 500 when Flusher unavailable, got %d", rec.code)
	}
	if !strings.Contains(rec.body.String(), "sse_flusher_unavailable") {
		t.Errorf("expected sse_flusher_unavailable error message, got: %s", rec.body.String())
	}
}

func TestStatusSSE_EmitsSignalChangedEvents(t *testing.T) {
	t.Parallel()

	// Happy path: 3 signal.changed events flow through handler with proper
	// SSE wire format per memory feedback_eventsource_named_events_vs_onmessage.
	projects := &fakeStatusProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-app", TeamID: "acme"},
	}
	store := &fakeStatusSSEEventStore{
		events: []sse.SSEEvent{
			{ID: 1, BuildID: "rel-1", EventType: "signal.changed", Data: json.RawMessage(`{"status":"Healthy"}`)},
			{ID: 2, BuildID: "rel-1", EventType: "signal.changed", Data: json.RawMessage(`{"replicas":3}`)},
			{ID: 3, BuildID: "rel-1", EventType: "signal.changed", Data: json.RawMessage(`{"rps":42}`)},
		},
	}
	handler := statushttp.NewStatusSSEHandler(projects, store, statushttp.StatusSSEHandlerOpts{
		PollInterval: 100 * time.Hour, // effectively no further polls
		MaxDuration:  50 * time.Millisecond,
	})

	req := newSSERequest(t, "my-app", "production", true)
	rec := httptest.NewRecorder()
	handler.Get(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "event: signal.changed") {
		t.Errorf("expected named SSE event 'signal.changed' in body, got:\n%s", body)
	}
	if !strings.Contains(body, `"status":"Healthy"`) {
		t.Errorf("expected first event data in body, got:\n%s", body)
	}
	if !strings.Contains(body, `"rps":42`) {
		t.Errorf("expected third event data in body, got:\n%s", body)
	}
	// Defense: id: lines must appear per event for Last-Event-ID reconnect.
	if !strings.Contains(body, "id: 1") || !strings.Contains(body, "id: 3") {
		t.Errorf("expected id: lines for each event, got:\n%s", body)
	}
	// Content-Type must be set to text/event-stream.
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected Content-Type=text/event-stream, got %q", rec.Header().Get("Content-Type"))
	}
}

func TestStatusSSE_LastEventIDDedup(t *testing.T) {
	t.Parallel()

	// EC-9 absorption: Last-Event-ID header skips events with id <= lastID.
	// Client sends Last-Event-ID=10; fake store returns events at ids
	// [10, 11, 12]; handler must skip id=10 (already delivered) + emit
	// id=11, id=12 only.
	projects := &fakeStatusProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-app", TeamID: "acme"},
	}
	store := &fakeStatusSSEEventStore{
		events: []sse.SSEEvent{
			{ID: 10, EventType: "signal.changed", Data: json.RawMessage(`{"old":true}`)},
			{ID: 11, EventType: "signal.changed", Data: json.RawMessage(`{"new":1}`)},
			{ID: 12, EventType: "signal.changed", Data: json.RawMessage(`{"new":2}`)},
		},
	}
	handler := statushttp.NewStatusSSEHandler(projects, store, statushttp.StatusSSEHandlerOpts{
		PollInterval: 100 * time.Hour,
		MaxDuration:  50 * time.Millisecond,
	})

	req := newSSERequest(t, "my-app", "production", true)
	req.Header.Set("Last-Event-ID", "10")
	rec := httptest.NewRecorder()
	handler.Get(rec, req)

	body := rec.Body.String()
	// id=10 must be skipped (already delivered to client)
	if strings.Contains(body, `"old":true`) {
		t.Errorf("event with id<=Last-Event-ID must be skipped, got body:\n%s", body)
	}
	// id=11 + id=12 must be emitted
	if !strings.Contains(body, `"new":1`) {
		t.Errorf("expected id=11 event after Last-Event-ID=10, got body:\n%s", body)
	}
	if !strings.Contains(body, `"new":2`) {
		t.Errorf("expected id=12 event after Last-Event-ID=10, got body:\n%s", body)
	}
}
