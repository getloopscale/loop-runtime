// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package sse_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	auth "github.com/getloopscale/loop-runtime/api/internal/middleware/auth"
	"github.com/getloopscale/loop-runtime/api/internal/sse"
	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
	"github.com/go-chi/chi/v5"
)

// --- Fakes for SSE stream tests ---

// inMemoryEventStore implements sse.EventStore in-memory for testing.
type inMemoryEventStore struct {
	mu     sync.Mutex
	events []sse.SSEEvent
	nextID int64
	// Plan J-17 T2.3 — project membership for GetAfterForProject.
	// Map buildID -> projectName; tenant by projectName.
	projectMembership map[string]string
	projectTenant     map[string]string
}

func newInMemoryEventStore() *inMemoryEventStore {
	return &inMemoryEventStore{
		nextID:            1,
		projectMembership: map[string]string{},
		projectTenant:     map[string]string{},
	}
}

func (s *inMemoryEventStore) Append(_ context.Context, buildID string, eventType string, data json.RawMessage) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := s.nextID
	s.nextID++
	s.events = append(s.events, sse.SSEEvent{
		ID:        id,
		BuildID:   buildID,
		EventType: eventType,
		Data:      data,
		CreatedAt: time.Now(),
	})
	return id, nil
}

func (s *inMemoryEventStore) GetAfter(_ context.Context, buildID string, afterID int64) ([]sse.SSEEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var result []sse.SSEEvent
	for _, ev := range s.events {
		if ev.BuildID == buildID && ev.ID > afterID {
			result = append(result, ev)
		}
	}
	if result == nil {
		result = []sse.SSEEvent{}
	}
	return result, nil
}

// GetAfterForProject — Plan J-17 T2.3 stub for in-memory store.
// Filters events whose buildID is in the membership map below. The
// memberships are seeded by the test (s.projectMembership[buildID] = projectName).
// tenantID is matched against s.projectTenant[projectName] if set; otherwise
// any tenantID matches (compat for legacy build-level tests).
func (s *inMemoryEventStore) GetAfterForProject(_ context.Context, tenantID string, projectName string, afterID int64) ([]sse.SSEEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.projectTenant != nil {
		if want, ok := s.projectTenant[projectName]; ok && want != tenantID {
			return []sse.SSEEvent{}, nil
		}
	}

	var result []sse.SSEEvent
	for _, ev := range s.events {
		if s.projectMembership[ev.BuildID] != projectName {
			continue
		}
		if ev.ID > afterID {
			result = append(result, ev)
		}
	}
	if result == nil {
		result = []sse.SSEEvent{}
	}
	return result, nil
}

func (s *inMemoryEventStore) Prune(_ context.Context, _ time.Duration) (int64, error) {
	return 0, nil
}

// fakeBuildReader implements sse.BuildStoreReader for testing.
// Thread-safe: status can be updated while the stream handler is reading.
type fakeBuildReader struct {
	mu     sync.Mutex
	builds map[string]*sse.BuildInfo
}

func newFakeBuildReader() *fakeBuildReader {
	return &fakeBuildReader{builds: make(map[string]*sse.BuildInfo)}
}

func (f *fakeBuildReader) SetBuild(id, tenantID, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.builds[id] = &sse.BuildInfo{ID: id, TenantID: tenantID, Status: status}
}

func (f *fakeBuildReader) GetByID(_ context.Context, id string) (*sse.BuildInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.builds[id]
	if !ok {
		return nil, loopzeroerrors.ErrBuildNotFound
	}
	cp := *b
	return &cp, nil
}

// errorBuildReader always returns a specified error from GetByID.
type errorBuildReader struct {
	err error
}

func (f *errorBuildReader) GetByID(_ context.Context, _ string) (*sse.BuildInfo, error) {
	return nil, f.err
}

// --- Helpers ---

const testTenantID = "tenant-test"

// sseField represents a parsed SSE event from the stream.
type sseField struct {
	ID    string
	Event string
	Data  string
}

// parseSSEEvents reads SSE events from response body text.
func parseSSEEvents(body string) []sseField {
	var events []sseField
	scanner := bufio.NewScanner(strings.NewReader(body))

	var current sseField
	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// Empty line = event boundary
			if current.Event != "" || current.Data != "" || current.ID != "" {
				events = append(events, current)
				current = sseField{}
			}
			continue
		}

		if strings.HasPrefix(line, "id: ") {
			current.ID = strings.TrimPrefix(line, "id: ")
		} else if strings.HasPrefix(line, "event: ") {
			current.Event = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			current.Data = strings.TrimPrefix(line, "data: ")
		}
		// Ignore comment lines (heartbeats starting with ":")
	}

	// Handle last event if body doesn't end with double newline
	if current.Event != "" || current.Data != "" || current.ID != "" {
		events = append(events, current)
	}

	return events
}

// hasHeartbeat checks if the body contains a heartbeat comment.
func hasHeartbeat(body string) bool {
	return strings.Contains(body, ":heartbeat")
}

// injectUserContext creates a middleware that injects a test UserContext.
func injectUserContext(tenantID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uc := &auth.UserContext{
				UserID:   "user-test",
				Email:    "test@example.com",
				TenantID: tenantID,
				Role:     "owner",
			}
			ctx := context.WithValue(r.Context(), auth.UserContextKeyPublic(), uc)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// serveWithChi wraps the handler in a chi router with auth injection.
func serveWithChi(handler http.HandlerFunc) *httptest.Server {
	r := chi.NewRouter()
	r.Use(injectUserContext(testTenantID))
	r.Get("/api/v1/builds/{id}/stream", handler)
	return httptest.NewServer(r)
}

// serveWithChiNoAuth wraps the handler without auth injection (for auth tests).
func serveWithChiNoAuth(handler http.HandlerFunc) *httptest.Server {
	r := chi.NewRouter()
	r.Get("/api/v1/builds/{id}/stream", handler)
	return httptest.NewServer(r)
}

// readFullBody reads the full response body until EOF or error.
func readFullBody(resp *http.Response) string {
	buf := make([]byte, 16384)
	var body string
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			body += string(buf[:n])
		}
		if readErr != nil {
			break
		}
	}
	return body
}

// --- Tests ---

func TestStreamHappyPath(t *testing.T) {
	// Arrange
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()

	buildReader.SetBuild("build-1", testTenantID, "RUNNING")

	// Pre-populate events
	ctx := context.Background()
	_, _ = eventStore.Append(ctx, "build-1", "build.started", json.RawMessage(`{"build_id":"build-1","status":"running","apps":["api"]}`))
	_, _ = eventStore.Append(ctx, "build-1", "build.succeeded", json.RawMessage(`{"build_id":"build-1","status":"succeeded"}`))
	_, _ = eventStore.Append(ctx, "build-1", "deploy.completed", json.RawMessage(`{"deploy_id":"d-1234","status":"success"}`))

	// Mark build as terminal so the stream closes immediately after replaying
	buildReader.SetBuild("build-1", testTenantID, "SUCCEEDED")

	handler := sse.NewStreamHandler(eventStore, buildReader)
	srv := serveWithChi(handler.ServeHTTP)
	defer srv.Close()

	// Act
	resp, err := http.Get(srv.URL + "/api/v1/builds/build-1/stream")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Assert headers
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("expected Cache-Control no-cache, got %q", cc)
	}
	if conn := resp.Header.Get("Connection"); conn != "keep-alive" {
		t.Errorf("expected Connection keep-alive, got %q", conn)
	}
	if xab := resp.Header.Get("X-Accel-Buffering"); xab != "no" {
		t.Errorf("expected X-Accel-Buffering no, got %q", xab)
	}

	body := readFullBody(resp)
	events := parseSSEEvents(body)
	if len(events) < 3 {
		t.Fatalf("expected at least 3 events, got %d; body: %s", len(events), body)
	}

	// Verify SSE format: id, event, data fields present
	for i, ev := range events {
		if ev.ID == "" {
			t.Errorf("event %d: missing id field", i)
		}
		if ev.Event == "" {
			t.Errorf("event %d: missing event field", i)
		}
		if ev.Data == "" {
			t.Errorf("event %d: missing data field", i)
		}
	}

	// Verify event types in order
	expectedTypes := []string{"build.started", "build.succeeded", "deploy.completed"}
	for i, expected := range expectedTypes {
		if i >= len(events) {
			break
		}
		if events[i].Event != expected {
			t.Errorf("event %d: expected type %q, got %q", i, expected, events[i].Event)
		}
	}
}

func TestStreamLastEventIDReplay(t *testing.T) {
	// Arrange
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()

	buildReader.SetBuild("build-1", testTenantID, "SUCCEEDED")

	ctx := context.Background()
	id1, _ := eventStore.Append(ctx, "build-1", "build.started", json.RawMessage(`{"status":"running","apps":["api"]}`))
	_, _ = eventStore.Append(ctx, "build-1", "build.succeeded", json.RawMessage(`{"status":"succeeded"}`))
	_, _ = eventStore.Append(ctx, "build-1", "deploy.completed", json.RawMessage(`{"status":"success"}`))

	handler := sse.NewStreamHandler(eventStore, buildReader)
	srv := serveWithChi(handler.ServeHTTP)
	defer srv.Close()

	// Act - request with Last-Event-ID = first event ID
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/builds/build-1/stream", nil)
	req.Header.Set("Last-Event-ID", fmt.Sprintf("%d", id1))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body := readFullBody(resp)
	events := parseSSEEvents(body)

	// Assert - should only get events after id1 (2 events: build.succeeded, deploy.completed)
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events after replay, got %d; body: %s", len(events), body)
	}

	if events[0].Event != "build.succeeded" {
		t.Errorf("first replayed event: expected build.succeeded, got %q", events[0].Event)
	}
}

func TestStreamJournalExpired(t *testing.T) {
	// Arrange - event store has events for build but with IDs below lastEventID
	// (simulates pruned journal)
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()

	buildReader.SetBuild("build-1", testTenantID, "SUCCEEDED")

	// Add an event with low ID (will be below Last-Event-ID: 999)
	ctx := context.Background()
	_, _ = eventStore.Append(ctx, "build-1", "build.started", json.RawMessage(`{"status":"running"}`))

	handler := sse.NewStreamHandler(eventStore, buildReader)
	srv := serveWithChi(handler.ServeHTTP)
	defer srv.Close()

	// Act - request with Last-Event-ID higher than any event
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/builds/build-1/stream", nil)
	req.Header.Set("Last-Event-ID", "999")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body := readFullBody(resp)
	events := parseSSEEvents(body)

	// Assert - should contain JOURNAL_EXPIRED event
	found := false
	for _, ev := range events {
		if ev.Event == "JOURNAL_EXPIRED" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected JOURNAL_EXPIRED event; body: %s", body)
	}
}

func TestStreamJournalExpiredFalsePositiveFixed(t *testing.T) {
	// MOD-3: When reconnecting with Last-Event-ID but no events exist AT ALL
	// for the build (fresh build), we should NOT emit JOURNAL_EXPIRED.
	// The stream should enter the poll loop and wait for events.

	// Arrange - empty event store, build is RUNNING
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()

	buildReader.SetBuild("build-1", testTenantID, "RUNNING")
	// No events at all — fresh build

	handler := sse.NewStreamHandler(eventStore, buildReader,
		sse.WithPollInterval(10*time.Millisecond),
		sse.WithHeartbeatInterval(30*time.Millisecond),
		sse.WithSafetyTimeout(2*time.Second),
	)
	srv := serveWithChi(handler.ServeHTTP)
	defer srv.Close()

	// intentional: inject event after short delay to simulate async event arrival during SSE stream
	go func() {
		time.Sleep(80 * time.Millisecond)
		_, _ = eventStore.Append(context.Background(), "build-1", "deploy.completed", json.RawMessage(`{"status":"success"}`))
	}()

	// Act - reconnect with Last-Event-ID but no events exist
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/builds/build-1/stream", nil)
	req.Header.Set("Last-Event-ID", "999")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body := readFullBody(resp)
	events := parseSSEEvents(body)

	// Assert - should NOT have JOURNAL_EXPIRED (no events exist at all, not pruned)
	for _, ev := range events {
		if ev.Event == "JOURNAL_EXPIRED" {
			t.Errorf("JOURNAL_EXPIRED should not be emitted for builds with no events; body: %s", body)
		}
	}
	// Should have received deploy.completed from poll loop
	foundDeploy := false
	for _, ev := range events {
		if ev.Event == "deploy.completed" {
			foundDeploy = true
			break
		}
	}
	if !foundDeploy {
		t.Errorf("expected deploy.completed event from poll loop; body: %s", body)
	}
}

func TestStreamHeartbeat(t *testing.T) {
	// Arrange - build is RUNNING, so stream stays open
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()

	buildReader.SetBuild("build-1", testTenantID, "RUNNING")

	// Use a short heartbeat interval for testing
	handler := sse.NewStreamHandler(eventStore, buildReader,
		sse.WithHeartbeatInterval(50*time.Millisecond),
		sse.WithPollInterval(20*time.Millisecond),
	)
	srv := serveWithChi(handler.ServeHTTP)
	defer srv.Close()

	// Act
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/v1/builds/build-1/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body := readFullBody(resp)

	// Assert - should contain heartbeat
	if !hasHeartbeat(body) {
		t.Errorf("expected heartbeat comment in stream; body: %s", body)
	}
}

func TestStreamConnectionAfterTerminalState(t *testing.T) {
	// Arrange - build already SUCCEEDED with events in journal
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()

	buildReader.SetBuild("build-1", testTenantID, "SUCCEEDED")

	ctx := context.Background()
	_, _ = eventStore.Append(ctx, "build-1", "build.started", json.RawMessage(`{"status":"running","apps":["api"]}`))
	_, _ = eventStore.Append(ctx, "build-1", "build.succeeded", json.RawMessage(`{"status":"succeeded"}`))
	_, _ = eventStore.Append(ctx, "build-1", "deploy.completed", json.RawMessage(`{"deploy_id":"d-abcd","status":"success"}`))

	handler := sse.NewStreamHandler(eventStore, buildReader)
	srv := serveWithChi(handler.ServeHTTP)
	defer srv.Close()

	// Act
	resp, err := http.Get(srv.URL + "/api/v1/builds/build-1/stream")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body := readFullBody(resp)
	events := parseSSEEvents(body)

	// Assert - all 3 events should be present
	if len(events) != 3 {
		t.Fatalf("expected 3 events (replay + close), got %d; body: %s", len(events), body)
	}

	// Last event should be deploy.completed
	if events[2].Event != "deploy.completed" {
		t.Errorf("last event: expected deploy.completed, got %q", events[2].Event)
	}
}

func TestStreamClientDisconnectNoLeak(t *testing.T) {
	// Arrange - build is RUNNING, so stream stays open
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()

	buildReader.SetBuild("build-1", testTenantID, "RUNNING")

	handler := sse.NewStreamHandler(eventStore, buildReader,
		sse.WithPollInterval(10*time.Millisecond),
		sse.WithHeartbeatInterval(50*time.Millisecond),
	)
	srv := serveWithChi(handler.ServeHTTP)
	defer srv.Close()

	// Act - connect then cancel context (simulate client disconnect)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/v1/builds/build-1/stream", nil)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}

	// Read a bit to ensure connection is established
	buf := make([]byte, 1024)
	_, _ = resp.Body.Read(buf)

	// Cancel the context to simulate client disconnect
	cancel()
	_ = resp.Body.Close()

	// Poll until the handler cleans up (active streams return to 0).
	deadline := time.After(5 * time.Second)
	for handler.ActiveStreams() != 0 {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for active streams to drop to 0, got %d", handler.ActiveStreams())
		default:
			time.Sleep(5 * time.Millisecond) // intentional: tight poll for cleanup detection
		}
	}
}

func TestStreamBuildNotFound(t *testing.T) {
	// Arrange
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()

	handler := sse.NewStreamHandler(eventStore, buildReader)
	srv := serveWithChi(handler.ServeHTTP)
	defer srv.Close()

	// Act
	resp, err := http.Get(srv.URL + "/api/v1/builds/nonexistent/stream")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Assert - should return 404
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestStreamDeployCompletedEventFormat(t *testing.T) {
	// Arrange
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()

	buildReader.SetBuild("build-1", testTenantID, "SUCCEEDED")

	deployPayload := map[string]interface{}{
		"deploy_id":        "d-abcd",
		"status":           "success",
		"duration_seconds": 45,
		"apps": []map[string]interface{}{
			{
				"name":                   "api",
				"status":                 "deployed",
				"url":                    "https://api.example.com",
				"build_duration_seconds": 30,
			},
		},
	}
	deployData, _ := json.Marshal(deployPayload)

	ctx := context.Background()
	_, _ = eventStore.Append(ctx, "build-1", "build.started", json.RawMessage(`{"apps":["api"]}`))
	_, _ = eventStore.Append(ctx, "build-1", "deploy.completed", deployData)

	handler := sse.NewStreamHandler(eventStore, buildReader)
	srv := serveWithChi(handler.ServeHTTP)
	defer srv.Close()

	// Act
	resp, err := http.Get(srv.URL + "/api/v1/builds/build-1/stream")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body := readFullBody(resp)
	events := parseSSEEvents(body)

	// Find deploy.completed event
	var deployEvent *sseField
	for _, ev := range events {
		if ev.Event == "deploy.completed" {
			deployEvent = &ev
			break
		}
	}

	if deployEvent == nil {
		t.Fatalf("deploy.completed event not found; body: %s", body)
	}

	// Assert - data should be valid JSON with expected fields
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(deployEvent.Data), &result); err != nil {
		t.Fatalf("deploy.completed data is not valid JSON: %v", err)
	}

	if result["deploy_id"] != "d-abcd" {
		t.Errorf("expected deploy_id d-abcd, got %v", result["deploy_id"])
	}
	if result["status"] != "success" {
		t.Errorf("expected status success, got %v", result["status"])
	}
	if _, ok := result["duration_seconds"]; !ok {
		t.Error("expected duration_seconds field in deploy.completed")
	}
	if _, ok := result["apps"]; !ok {
		t.Error("expected apps field in deploy.completed")
	}
}

func TestStreamStaysOpenAfterBuildSucceededUntilDeployEvent(t *testing.T) {
	// Arrange
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()

	buildReader.SetBuild("build-1", testTenantID, "SUCCEEDED")

	ctx := context.Background()
	_, _ = eventStore.Append(ctx, "build-1", "build.started", json.RawMessage(`{"status":"running"}`))
	_, _ = eventStore.Append(ctx, "build-1", "build.succeeded", json.RawMessage(`{"status":"succeeded"}`))

	handler := sse.NewStreamHandler(eventStore, buildReader,
		sse.WithPollInterval(20*time.Millisecond),
		sse.WithHeartbeatInterval(500*time.Millisecond),
		sse.WithSafetyTimeout(2*time.Second),
	)
	srv := serveWithChi(handler.ServeHTTP)
	defer srv.Close()

	// intentional: simulate Phase 2 writing deploy.completed after a short delay during SSE stream
	go func() {
		time.Sleep(100 * time.Millisecond)
		_, _ = eventStore.Append(ctx, "build-1", "deploy.completed", json.RawMessage(`{"deploy_id":"d-5678","status":"success"}`))
	}()

	// Act
	resp, err := http.Get(srv.URL + "/api/v1/builds/build-1/stream")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body := readFullBody(resp)
	events := parseSSEEvents(body)

	// Assert - must have all 3 events including deploy.completed
	eventTypes := make([]string, 0, len(events))
	for _, ev := range events {
		eventTypes = append(eventTypes, ev.Event)
	}

	if len(events) < 3 {
		t.Fatalf("expected at least 3 events (build.started, build.succeeded, deploy.completed), got %d: %v; body: %s",
			len(events), eventTypes, body)
	}

	// The critical assertion: deploy.completed must be present
	foundDeploy := false
	for _, ev := range events {
		if ev.Event == "deploy.completed" {
			foundDeploy = true
			break
		}
	}
	if !foundDeploy {
		t.Errorf("deploy.completed event missing — stream closed too early; events: %v", eventTypes)
	}
}

func TestStreamClosesOnBuildFailedWithoutWaitingForDeploy(t *testing.T) {
	// Arrange
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()

	buildReader.SetBuild("build-1", testTenantID, "FAILED")

	ctx := context.Background()
	_, _ = eventStore.Append(ctx, "build-1", "build.started", json.RawMessage(`{"status":"running"}`))
	_, _ = eventStore.Append(ctx, "build-1", "build.failed", json.RawMessage(`{"status":"failed","reason":"compilation error"}`))

	handler := sse.NewStreamHandler(eventStore, buildReader)
	srv := serveWithChi(handler.ServeHTTP)
	defer srv.Close()

	// Act
	resp, err := http.Get(srv.URL + "/api/v1/builds/build-1/stream")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body := readFullBody(resp)
	events := parseSSEEvents(body)

	// Assert - stream closes after build.failed
	if len(events) != 2 {
		t.Fatalf("expected 2 events (build.started, build.failed), got %d; body: %s", len(events), body)
	}
	if events[1].Event != "build.failed" {
		t.Errorf("last event: expected build.failed, got %q", events[1].Event)
	}
}

func TestStreamClosesOnBuildCancelledWithoutWaitingForDeploy(t *testing.T) {
	// Arrange
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()

	buildReader.SetBuild("build-1", testTenantID, "CANCELLED")

	ctx := context.Background()
	_, _ = eventStore.Append(ctx, "build-1", "build.started", json.RawMessage(`{"status":"running"}`))
	_, _ = eventStore.Append(ctx, "build-1", "build.cancelled", json.RawMessage(`{"status":"cancelled"}`))

	handler := sse.NewStreamHandler(eventStore, buildReader)
	srv := serveWithChi(handler.ServeHTTP)
	defer srv.Close()

	// Act
	resp, err := http.Get(srv.URL + "/api/v1/builds/build-1/stream")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body := readFullBody(resp)
	events := parseSSEEvents(body)

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d; body: %s", len(events), body)
	}
	if events[1].Event != "build.cancelled" {
		t.Errorf("last event: expected build.cancelled, got %q", events[1].Event)
	}
}

func TestStreamClosesOnDeploySyncFailed(t *testing.T) {
	// Arrange
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()

	buildReader.SetBuild("build-1", testTenantID, "SUCCEEDED")

	ctx := context.Background()
	_, _ = eventStore.Append(ctx, "build-1", "build.started", json.RawMessage(`{"status":"running"}`))
	_, _ = eventStore.Append(ctx, "build-1", "build.succeeded", json.RawMessage(`{"status":"succeeded"}`))
	_, _ = eventStore.Append(ctx, "build-1", "deploy.sync_failed", json.RawMessage(`{"reason":"ArgoCD sync timeout"}`))

	handler := sse.NewStreamHandler(eventStore, buildReader)
	srv := serveWithChi(handler.ServeHTTP)
	defer srv.Close()

	// Act
	resp, err := http.Get(srv.URL + "/api/v1/builds/build-1/stream")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body := readFullBody(resp)
	events := parseSSEEvents(body)

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d; body: %s", len(events), body)
	}
	if events[2].Event != "deploy.sync_failed" {
		t.Errorf("last event: expected deploy.sync_failed, got %q", events[2].Event)
	}
}

func TestStreamSafetyTimeoutWhenNoDeployEventsArrive(t *testing.T) {
	// Arrange
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()

	buildReader.SetBuild("build-1", testTenantID, "SUCCEEDED")

	ctx := context.Background()
	_, _ = eventStore.Append(ctx, "build-1", "build.started", json.RawMessage(`{"status":"running"}`))
	_, _ = eventStore.Append(ctx, "build-1", "build.succeeded", json.RawMessage(`{"status":"succeeded"}`))
	// No deploy events — Phase 2 never starts

	handler := sse.NewStreamHandler(eventStore, buildReader,
		sse.WithPollInterval(10*time.Millisecond),
		sse.WithHeartbeatInterval(500*time.Millisecond),
		sse.WithSafetyTimeout(100*time.Millisecond), // Short timeout for testing
	)
	srv := serveWithChi(handler.ServeHTTP)
	defer srv.Close()

	// Act
	resp, err := http.Get(srv.URL + "/api/v1/builds/build-1/stream")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body := readFullBody(resp)
	events := parseSSEEvents(body)

	// Assert - should have build.started, build.succeeded (replay), then stream.timeout
	foundTimeout := false
	for _, ev := range events {
		if ev.Event == "stream.timeout" {
			foundTimeout = true
			var data map[string]interface{}
			if err := json.Unmarshal([]byte(ev.Data), &data); err != nil {
				t.Errorf("stream.timeout data is not valid JSON: %v", err)
			}
			if _, ok := data["reason"]; !ok {
				t.Error("stream.timeout data missing 'reason' field")
			}
			if _, ok := data["action"]; !ok {
				t.Error("stream.timeout data missing 'action' field")
			}
			break
		}
	}
	if !foundTimeout {
		eventTypes := make([]string, 0, len(events))
		for _, ev := range events {
			eventTypes = append(eventTypes, ev.Event)
		}
		t.Errorf("expected stream.timeout event after safety timeout; events: %v; body: %s", eventTypes, body)
	}
}

func TestStreamDoesNotCloseOnBuildSucceededAlone(t *testing.T) {
	// Arrange
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()

	buildReader.SetBuild("build-1", testTenantID, "SUCCEEDED")

	ctx := context.Background()
	_, _ = eventStore.Append(ctx, "build-1", "build.started", json.RawMessage(`{"status":"running"}`))
	_, _ = eventStore.Append(ctx, "build-1", "build.succeeded", json.RawMessage(`{"status":"succeeded"}`))

	handler := sse.NewStreamHandler(eventStore, buildReader,
		sse.WithPollInterval(10*time.Millisecond),
		sse.WithHeartbeatInterval(30*time.Millisecond),
		sse.WithSafetyTimeout(2*time.Second),
	)
	srv := serveWithChi(handler.ServeHTTP)
	defer srv.Close()

	// Act - connect with a short timeout to prove the stream stays open
	reqCtx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequestWithContext(reqCtx, "GET", srv.URL+"/api/v1/builds/build-1/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body := readFullBody(resp)
	events := parseSSEEvents(body)

	for _, ev := range events {
		if ev.Event == "deploy.completed" || ev.Event == "stream.timeout" {
			t.Errorf("stream should not have closed with %q — client timeout should have disconnected it", ev.Event)
		}
	}

	if !hasHeartbeat(body) {
		t.Errorf("expected heartbeat in stream body (proving stream stayed open); body: %s", body)
	}
}

func TestStreamLastEventIDReplayWithBuildSucceededButNoDeployEvent(t *testing.T) {
	// Arrange
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()

	buildReader.SetBuild("build-1", testTenantID, "SUCCEEDED")

	ctx := context.Background()
	id1, _ := eventStore.Append(ctx, "build-1", "build.started", json.RawMessage(`{"status":"running"}`))
	_, _ = eventStore.Append(ctx, "build-1", "build.succeeded", json.RawMessage(`{"status":"succeeded"}`))

	handler := sse.NewStreamHandler(eventStore, buildReader,
		sse.WithPollInterval(10*time.Millisecond),
		sse.WithHeartbeatInterval(30*time.Millisecond),
		sse.WithSafetyTimeout(2*time.Second),
	)
	srv := serveWithChi(handler.ServeHTTP)
	defer srv.Close()

	// intentional: simulate Phase 2 writing deploy.completed after a short delay during SSE stream
	go func() {
		time.Sleep(80 * time.Millisecond)
		_, _ = eventStore.Append(ctx, "build-1", "deploy.completed", json.RawMessage(`{"deploy_id":"d-9999","status":"success"}`))
	}()

	// Act - reconnect with Last-Event-ID = build.started
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/builds/build-1/stream", nil)
	req.Header.Set("Last-Event-ID", fmt.Sprintf("%d", id1))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body := readFullBody(resp)
	events := parseSSEEvents(body)

	// Assert - should have received build.succeeded (replay) + deploy.completed (polled)
	foundDeploy := false
	for _, ev := range events {
		if ev.Event == "deploy.completed" {
			foundDeploy = true
			break
		}
	}
	if !foundDeploy {
		eventTypes := make([]string, 0, len(events))
		for _, ev := range events {
			eventTypes = append(eventTypes, ev.Event)
		}
		t.Errorf("deploy.completed not found after reconnect — stream closed too early; events: %v; body: %s",
			eventTypes, body)
	}
}

func TestStreamGetByIDInternalErrorReturns500AndLogs(t *testing.T) {
	// Arrange - buildReader returns a database error (not ErrBuildNotFound)
	eventStore := newInMemoryEventStore()
	dbErr := fmt.Errorf("build store: scan failed: cannot convert OID 23 to *int")
	buildReader := &errorBuildReader{err: dbErr}

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	handler := sse.NewStreamHandler(eventStore, buildReader, sse.WithLogger(logger))
	srv := serveWithChi(handler.ServeHTTP)
	defer srv.Close()

	// Act
	resp, err := http.Get(srv.URL + "/api/v1/builds/build-error-test/stream")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	// Assert - HTTP 500
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}

	// Assert - response body contains INTERNAL_ERROR code
	if !strings.Contains(string(body), "INTERNAL_ERROR") {
		t.Errorf("expected INTERNAL_ERROR in body, got %s", string(body))
	}

	// Assert - error was logged with build_id and error details
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "build-error-test") {
		t.Errorf("expected build_id in log output, got %s", logOutput)
	}
	if !strings.Contains(logOutput, "scan failed") {
		t.Errorf("expected error details in log output, got %s", logOutput)
	}
}

// --- SEC-1: Tenant isolation tests ---

func TestStreamReturns401WhenNotAuthenticated(t *testing.T) {
	// Arrange
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()
	buildReader.SetBuild("build-1", testTenantID, "RUNNING")

	handler := sse.NewStreamHandler(eventStore, buildReader)
	// Use router WITHOUT auth injection
	srv := serveWithChiNoAuth(handler.ServeHTTP)
	defer srv.Close()

	// Act
	resp, err := http.Get(srv.URL + "/api/v1/builds/build-1/stream")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Assert
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestStreamReturns404ForCrossTenantAccess(t *testing.T) {
	// SEC-1: A user from tenant-A must NOT be able to stream events of tenant-B's build.
	// Returns 404 (not 403) to avoid leaking build existence.

	// Arrange
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()
	buildReader.SetBuild("build-1", "other-tenant", "RUNNING") // Different tenant

	handler := sse.NewStreamHandler(eventStore, buildReader)
	// User is authenticated as testTenantID but build belongs to other-tenant
	srv := serveWithChi(handler.ServeHTTP)
	defer srv.Close()

	// Act
	resp, err := http.Get(srv.URL + "/api/v1/builds/build-1/stream")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Assert - 404 (not 403)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for cross-tenant access, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "BUILD_NOT_FOUND") {
		t.Errorf("expected BUILD_NOT_FOUND error code, got %s", string(body))
	}
}

// --- BLOCKER-2: Max concurrent streams test ---

func TestStreamRejectsWhenMaxConcurrentStreamsReached(t *testing.T) {
	// Arrange
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()
	buildReader.SetBuild("build-1", testTenantID, "RUNNING")

	handler := sse.NewStreamHandler(eventStore, buildReader,
		sse.WithMaxConcurrentStreams(1), // Only allow 1 stream
		sse.WithPollInterval(10*time.Millisecond),
		sse.WithHeartbeatInterval(50*time.Millisecond),
	)
	srv := serveWithChi(handler.ServeHTTP)
	defer srv.Close()

	// Act - open first stream (should succeed)
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	req1, _ := http.NewRequestWithContext(ctx1, "GET", srv.URL+"/api/v1/builds/build-1/stream", nil)
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatalf("first stream failed: %v", err)
	}

	// Poll until the stream is established (active streams = 1).
	deadline := time.After(5 * time.Second)
	for handler.ActiveStreams() < 1 {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for first stream to be established")
		default:
			time.Sleep(1 * time.Millisecond) // intentional: tight poll for stream establishment
		}
	}

	// Act - try to open second stream (should be rejected)
	resp2, err := http.Get(srv.URL + "/api/v1/builds/build-1/stream")
	if err != nil {
		t.Fatalf("second stream request failed: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()

	// Assert - second stream rejected with 503
	if resp2.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when max streams reached, got %d", resp2.StatusCode)
	}

	body, _ := io.ReadAll(resp2.Body)
	if !strings.Contains(string(body), "TOO_MANY_STREAMS") {
		t.Errorf("expected TOO_MANY_STREAMS error, got %s", string(body))
	}

	// Cleanup first stream
	cancel1()
	_ = resp1.Body.Close()

	// Poll until the first stream is cleaned up.
	deadline2 := time.After(5 * time.Second)
	for handler.ActiveStreams() != 0 {
		select {
		case <-deadline2:
			t.Fatalf("timeout waiting for first stream cleanup, active=%d", handler.ActiveStreams())
		default:
			time.Sleep(1 * time.Millisecond) // intentional: tight poll for cleanup detection
		}
	}

	// After cleanup, a new stream should succeed
	resp3, err := http.Get(srv.URL + "/api/v1/builds/build-1/stream")
	if err != nil {
		t.Fatalf("third stream request failed: %v", err)
	}
	defer func() { _ = resp3.Body.Close() }()

	if resp3.StatusCode == http.StatusServiceUnavailable {
		t.Error("expected stream to succeed after previous one closed, but still got 503")
	}
}

// --- BLOCKER-1: Metrics tracking test ---

func TestStreamActiveStreamCountTracking(t *testing.T) {
	// Arrange
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()
	buildReader.SetBuild("build-1", testTenantID, "RUNNING")

	handler := sse.NewStreamHandler(eventStore, buildReader,
		sse.WithPollInterval(10*time.Millisecond),
		sse.WithHeartbeatInterval(50*time.Millisecond),
	)

	if handler.ActiveStreams() != 0 {
		t.Fatalf("expected 0 active streams initially, got %d", handler.ActiveStreams())
	}

	srv := serveWithChi(handler.ServeHTTP)
	defer srv.Close()

	// Act - open a stream
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/v1/builds/build-1/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}

	// Poll until stream is established.
	deadline := time.After(5 * time.Second)
	for handler.ActiveStreams() < 1 {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for stream to be established")
		default:
			time.Sleep(1 * time.Millisecond) // intentional: tight poll for stream establishment
		}
	}

	if handler.ActiveStreams() != 1 {
		t.Errorf("expected 1 active stream, got %d", handler.ActiveStreams())
	}

	// Disconnect
	cancel()
	_ = resp.Body.Close()

	// Poll until cleanup completes.
	deadline2 := time.After(5 * time.Second)
	for handler.ActiveStreams() != 0 {
		select {
		case <-deadline2:
			t.Fatalf("timeout waiting for active streams to drop to 0, got %d", handler.ActiveStreams())
		default:
			time.Sleep(1 * time.Millisecond) // intentional: tight poll for cleanup detection
		}
	}
}

// =============================================================================
// ProjectStreamHandler tests — Plan J-17 T2.3
// =============================================================================

// serveProjectStreamWithChi wraps ProjectStreamHandler in a chi router with
// auth injection at /api/v1/projects/{name}/builds/stream.
func serveProjectStreamWithChi(handler http.HandlerFunc) *httptest.Server {
	r := chi.NewRouter()
	r.Use(injectUserContext(testTenantID))
	r.Get("/api/v1/projects/{name}/builds/stream", handler)
	return httptest.NewServer(r)
}

// serveProjectStreamWithChiNoAuth wraps without auth injection.
func serveProjectStreamWithChiNoAuth(handler http.HandlerFunc) *httptest.Server {
	r := chi.NewRouter()
	r.Get("/api/v1/projects/{name}/builds/stream", handler)
	return httptest.NewServer(r)
}

func TestStreamProjectBuilds_AuthRequired(t *testing.T) {
	store := newInMemoryEventStore()
	handler := sse.NewProjectStreamHandler(store)
	srv := serveProjectStreamWithChiNoAuth(handler.ServeHTTP)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/projects/foo/builds/stream")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestStreamProjectBuilds_FiltersToTenant(t *testing.T) {
	store := newInMemoryEventStore()
	// Project "foo" belongs to a DIFFERENT tenant; we should see no events.
	store.projectTenant["foo"] = "OTHER_TENANT"
	store.projectMembership["b-foreign"] = "foo"
	ctx := context.Background()
	_, _ = store.Append(ctx, "b-foreign", "build.started", json.RawMessage(`{"build_id":"b-foreign"}`))

	handler := sse.NewProjectStreamHandler(
		store,
		sse.WithProjectHeartbeatInterval(50*time.Millisecond),
		sse.WithProjectPollInterval(10*time.Millisecond),
	)
	srv := serveProjectStreamWithChi(handler.ServeHTTP)
	defer srv.Close()

	cctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(cctx, http.MethodGet, srv.URL+"/api/v1/projects/foo/builds/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body := readFullBody(resp)
	events := parseSSEEvents(body)
	for _, ev := range events {
		if ev.Event == "build.started" {
			t.Fatalf("foreign-tenant event leaked into project stream: %+v", ev)
		}
	}
}

func TestStreamProjectBuilds_FiltersToProject(t *testing.T) {
	store := newInMemoryEventStore()
	store.projectMembership["b-foo-1"] = "foo"
	store.projectMembership["b-bar-1"] = "bar"
	ctx := context.Background()
	_, _ = store.Append(ctx, "b-foo-1", "build.started", json.RawMessage(`{"build_id":"b-foo-1"}`))
	_, _ = store.Append(ctx, "b-bar-1", "build.started", json.RawMessage(`{"build_id":"b-bar-1"}`))

	handler := sse.NewProjectStreamHandler(
		store,
		sse.WithProjectHeartbeatInterval(50*time.Millisecond),
		sse.WithProjectPollInterval(10*time.Millisecond),
	)
	srv := serveProjectStreamWithChi(handler.ServeHTTP)
	defer srv.Close()

	cctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(cctx, http.MethodGet, srv.URL+"/api/v1/projects/foo/builds/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body := readFullBody(resp)
	events := parseSSEEvents(body)

	var sawFoo, sawBar bool
	for _, ev := range events {
		if strings.Contains(ev.Data, "b-foo-1") {
			sawFoo = true
		}
		if strings.Contains(ev.Data, "b-bar-1") {
			sawBar = true
		}
	}
	if !sawFoo {
		t.Fatalf("expected to see event for project foo, body=%q", body)
	}
	if sawBar {
		t.Fatalf("event for project bar leaked into project foo stream, body=%q", body)
	}
}

func TestStreamProjectBuilds_ResumeLastEventID(t *testing.T) {
	store := newInMemoryEventStore()
	store.projectMembership["b-1"] = "foo"
	store.projectMembership["b-2"] = "foo"
	ctx := context.Background()
	id1, _ := store.Append(ctx, "b-1", "build.started", json.RawMessage(`{"build_id":"b-1"}`))
	id2, _ := store.Append(ctx, "b-2", "build.started", json.RawMessage(`{"build_id":"b-2"}`))
	_ = id1
	_ = id2

	handler := sse.NewProjectStreamHandler(
		store,
		sse.WithProjectHeartbeatInterval(50*time.Millisecond),
		sse.WithProjectPollInterval(10*time.Millisecond),
	)
	srv := serveProjectStreamWithChi(handler.ServeHTTP)
	defer srv.Close()

	// Resume from id1: we expect to receive ONLY id2.
	cctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(cctx, http.MethodGet, srv.URL+"/api/v1/projects/foo/builds/stream", nil)
	req.Header.Set("Last-Event-ID", fmt.Sprintf("%d", id1))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body := readFullBody(resp)
	if strings.Contains(body, `"b-1"`) {
		t.Fatalf("Last-Event-ID resume should have skipped id1 (b-1), body=%q", body)
	}
	if !strings.Contains(body, `"b-2"`) {
		t.Fatalf("Last-Event-ID resume must still deliver id2 (b-2), body=%q", body)
	}
}
