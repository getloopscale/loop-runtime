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

	"github.com/go-chi/chi/v5"
	"github.com/loopzero/loopzero/api/internal/sse"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// --- Fakes for SSE stream tests ---

// inMemoryEventStore implements sse.EventStore in-memory for testing.
type inMemoryEventStore struct {
	mu     sync.Mutex
	events []sse.SSEEvent
	nextID int64
}

func newInMemoryEventStore() *inMemoryEventStore {
	return &inMemoryEventStore{nextID: 1}
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

func (s *inMemoryEventStore) Prune(_ context.Context, _ time.Duration) (int64, error) {
	return 0, nil
}

// fakeBuildReader implements sse.BuildStoreReader for testing.
type fakeBuildReader struct {
	builds map[string]*sse.BuildInfo
}

func newFakeBuildReader() *fakeBuildReader {
	return &fakeBuildReader{builds: make(map[string]*sse.BuildInfo)}
}

func (f *fakeBuildReader) GetByID(_ context.Context, id string) (*sse.BuildInfo, error) {
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

// serveWithChi wraps the handler in a chi router to support URL params.
func serveWithChi(handler http.HandlerFunc) *httptest.Server {
	r := chi.NewRouter()
	r.Get("/api/v1/builds/{id}/stream", handler)
	return httptest.NewServer(r)
}

// --- Tests ---

func TestStreamHappyPath(t *testing.T) {
	// Arrange
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()

	buildReader.builds["build-1"] = &sse.BuildInfo{
		ID:     "build-1",
		Status: "RUNNING",
	}

	// Pre-populate events
	ctx := context.Background()
	eventStore.Append(ctx, "build-1", "build.started", json.RawMessage(`{"build_id":"build-1","status":"running"}`))
	eventStore.Append(ctx, "build-1", "build.succeeded", json.RawMessage(`{"build_id":"build-1","status":"succeeded"}`))
	eventStore.Append(ctx, "build-1", "deploy.completed", json.RawMessage(`{"deploy_id":"d-1234","status":"success"}`))

	// Mark build as terminal so the stream closes immediately after replaying
	buildReader.builds["build-1"].Status = "SUCCEEDED"

	handler := sse.NewStreamHandler(eventStore, buildReader)
	srv := serveWithChi(handler.ServeHTTP)
	defer srv.Close()

	// Act
	resp, err := http.Get(srv.URL + "/api/v1/builds/build-1/stream")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

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

	// Read full body (stream should close since build is terminal)
	buf := make([]byte, 8192)
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

	buildReader.builds["build-1"] = &sse.BuildInfo{
		ID:     "build-1",
		Status: "SUCCEEDED",
	}

	ctx := context.Background()
	id1, _ := eventStore.Append(ctx, "build-1", "build.started", json.RawMessage(`{"status":"running"}`))
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
	defer resp.Body.Close()

	buf := make([]byte, 8192)
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
	// Arrange - event store with no events for the build (simulates expired journal)
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()

	buildReader.builds["build-1"] = &sse.BuildInfo{
		ID:     "build-1",
		Status: "SUCCEEDED",
	}

	handler := sse.NewStreamHandler(eventStore, buildReader)
	srv := serveWithChi(handler.ServeHTTP)
	defer srv.Close()

	// Act - request with Last-Event-ID but no events exist
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/builds/build-1/stream", nil)
	req.Header.Set("Last-Event-ID", "999")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	buf := make([]byte, 8192)
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

func TestStreamHeartbeat(t *testing.T) {
	// Arrange - build is RUNNING, so stream stays open
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()

	buildReader.builds["build-1"] = &sse.BuildInfo{
		ID:     "build-1",
		Status: "RUNNING",
	}

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
	defer resp.Body.Close()

	// Read until context deadline
	buf := make([]byte, 8192)
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

	// Assert - should contain heartbeat
	if !hasHeartbeat(body) {
		t.Errorf("expected heartbeat comment in stream; body: %s", body)
	}
}

func TestStreamConnectionAfterTerminalState(t *testing.T) {
	// Arrange - build already SUCCEEDED with events in journal
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()

	buildReader.builds["build-1"] = &sse.BuildInfo{
		ID:     "build-1",
		Status: "SUCCEEDED",
	}

	ctx := context.Background()
	eventStore.Append(ctx, "build-1", "build.started", json.RawMessage(`{"status":"running"}`))
	eventStore.Append(ctx, "build-1", "build.succeeded", json.RawMessage(`{"status":"succeeded"}`))
	eventStore.Append(ctx, "build-1", "deploy.completed", json.RawMessage(`{"deploy_id":"d-abcd","status":"success"}`))

	handler := sse.NewStreamHandler(eventStore, buildReader)
	srv := serveWithChi(handler.ServeHTTP)
	defer srv.Close()

	// Act
	resp, err := http.Get(srv.URL + "/api/v1/builds/build-1/stream")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	// Read full response - stream should close immediately after replaying all events
	buf := make([]byte, 8192)
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

	buildReader.builds["build-1"] = &sse.BuildInfo{
		ID:     "build-1",
		Status: "RUNNING",
	}

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
	resp.Body.Read(buf)

	// Cancel the context to simulate client disconnect
	cancel()
	resp.Body.Close()

	// Give the handler time to clean up
	time.Sleep(100 * time.Millisecond)

	// Assert - if we got here without hanging, the goroutine was cleaned up.
	// A leaked goroutine would cause the test to hang or the server to leak resources.
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
	defer resp.Body.Close()

	// Assert - should return 404
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestStreamDeployCompletedEventFormat(t *testing.T) {
	// Arrange
	eventStore := newInMemoryEventStore()
	buildReader := newFakeBuildReader()

	buildReader.builds["build-1"] = &sse.BuildInfo{
		ID:     "build-1",
		Status: "SUCCEEDED",
	}

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
	eventStore.Append(ctx, "build-1", "build.started", json.RawMessage(`{}`))
	eventStore.Append(ctx, "build-1", "deploy.completed", deployData)

	handler := sse.NewStreamHandler(eventStore, buildReader)
	srv := serveWithChi(handler.ServeHTTP)
	defer srv.Close()

	// Act
	resp, err := http.Get(srv.URL + "/api/v1/builds/build-1/stream")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()

	buf := make([]byte, 8192)
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
	defer resp.Body.Close()

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
