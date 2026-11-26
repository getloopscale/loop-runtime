// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testSecret = "test-webhook-secret"

func sign(body, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestWebhook_ValidSignature_Returns200(t *testing.T) {
	h := NewHandler(context.Background(), testSecret, slog.Default())
	body := `{"ref":"refs/heads/main","after":"abc123"}`
	sig := sign(body, testSecret)

	req := httptest.NewRequest("POST", "/webhooks/github", strings.NewReader(body))
	req.Header.Set(signatureHeader, sig)
	req.Header.Set(eventHeader, "push")
	req.Header.Set(deliveryHeader, "test-1")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestWebhook_MissingSignature_Returns401(t *testing.T) {
	h := NewHandler(context.Background(), testSecret, slog.Default())

	req := httptest.NewRequest("POST", "/webhooks/github", strings.NewReader("{}"))
	req.Header.Set(eventHeader, "push")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestWebhook_InvalidSignature_Returns403(t *testing.T) {
	h := NewHandler(context.Background(), testSecret, slog.Default())

	req := httptest.NewRequest("POST", "/webhooks/github", strings.NewReader("{}"))
	req.Header.Set(signatureHeader, "sha256=deadbeef")
	req.Header.Set(eventHeader, "push")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestWebhook_WrongMethod_Returns405(t *testing.T) {
	h := NewHandler(context.Background(), testSecret, slog.Default())

	req := httptest.NewRequest("GET", "/webhooks/github", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 405 {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestWebhook_UnknownEvent_Returns200(t *testing.T) {
	h := NewHandler(context.Background(), testSecret, slog.Default())
	body := `{}`
	sig := sign(body, testSecret)

	req := httptest.NewRequest("POST", "/webhooks/github", strings.NewReader(body))
	req.Header.Set(signatureHeader, sig)
	req.Header.Set(eventHeader, "star")
	req.Header.Set(deliveryHeader, "test-2")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200 for unknown event, got %d", w.Code)
	}
}

func TestWebhook_PushEvent_DispatchesToHandler(t *testing.T) {
	var mu sync.Mutex
	var received *PushEvent

	h := NewHandler(context.Background(), testSecret, slog.Default()).OnPush(func(_ context.Context, event PushEvent) error {
		mu.Lock()
		defer mu.Unlock()
		received = &event
		return nil
	})

	body := `{
		"ref": "refs/heads/main",
		"after": "abc123def456",
		"before": "000000",
		"repository": {
			"id": 12345,
			"full_name": "getloopscale/loopzerokit-example",
			"default_branch": "main"
		},
		"sender": {"login": "paulohenriquevn"},
		"head_commit": {"id": "abc123def456", "message": "test commit"},
		"installation": {"id": 122482567}
	}`
	sig := sign(body, testSecret)

	req := httptest.NewRequest("POST", "/webhooks/github", strings.NewReader(body))
	req.Header.Set(signatureHeader, sig)
	req.Header.Set(eventHeader, "push")
	req.Header.Set(deliveryHeader, "test-3")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Poll for async dispatch completion instead of sleeping.
	deadline := time.After(5 * time.Second)
	for {
		mu.Lock()
		done := received != nil
		mu.Unlock()
		if done {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for push handler to be called")
		default:
			time.Sleep(1 * time.Millisecond) // intentional: tight poll for async goroutine completion
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if received.After != "abc123def456" {
		t.Errorf("commit SHA = %q, want abc123def456", received.After)
	}
	if received.Repository.FullName != "getloopscale/loopzerokit-example" {
		t.Errorf("repo = %q, want getloopscale/loopzerokit-example", received.Repository.FullName)
	}
	if received.Installation.ID != 122482567 {
		t.Errorf("installation ID = %d, want 122482567", received.Installation.ID)
	}
	if received.Branch() != "main" {
		t.Errorf("branch = %q, want main", received.Branch())
	}
	if !received.IsDefaultBranch() {
		t.Error("expected IsDefaultBranch() = true")
	}
}

func TestWebhook_PullRequestEvent_DispatchesToHandler(t *testing.T) {
	var mu sync.Mutex
	var received *PullRequestEvent

	h := NewHandler(context.Background(), testSecret, slog.Default()).OnPullRequest(func(_ context.Context, event PullRequestEvent) error {
		mu.Lock()
		defer mu.Unlock()
		received = &event
		return nil
	})

	body := `{
		"action": "opened",
		"number": 42,
		"pull_request": {
			"number": 42,
			"title": "Add feature X",
			"head": {"ref": "feature-x", "sha": "def456"},
			"base": {"ref": "main"},
			"user": {"login": "dev"}
		},
		"repository": {"full_name": "getloopscale/loopzerokit-example"},
		"installation": {"id": 122482567}
	}`
	sig := sign(body, testSecret)

	req := httptest.NewRequest("POST", "/webhooks/github", strings.NewReader(body))
	req.Header.Set(signatureHeader, sig)
	req.Header.Set(eventHeader, "pull_request")
	req.Header.Set(deliveryHeader, "test-4")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	// Poll for async dispatch completion instead of sleeping.
	deadline := time.After(5 * time.Second)
	for {
		mu.Lock()
		done := received != nil
		mu.Unlock()
		if done {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for pull_request handler to be called")
		default:
			time.Sleep(1 * time.Millisecond) // intentional: tight poll for async goroutine completion
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if received.Action != "opened" {
		t.Errorf("action = %q, want opened", received.Action)
	}
	if received.Number != 42 {
		t.Errorf("PR number = %d, want 42", received.Number)
	}
}

func TestPushEvent_BranchHelpers(t *testing.T) {
	e := PushEvent{
		Ref:        "refs/heads/feature-x",
		Repository: Repository{DefaultBranch: "main"},
	}
	if e.Branch() != "feature-x" {
		t.Errorf("Branch() = %q, want feature-x", e.Branch())
	}
	if e.IsDefaultBranch() {
		t.Error("IsDefaultBranch() should be false for feature-x")
	}
	if e.IsTag() {
		t.Error("IsTag() should be false for branch ref")
	}

	tag := PushEvent{Ref: "refs/tags/v1.0.0"}
	if !tag.IsTag() {
		t.Error("IsTag() should be true for tag ref")
	}
}

// --- Plan J-09 T4.2 (counter) + T4.1 (correlation_id) ---

func TestWebhook_CounterIncremented_AcceptedPath(t *testing.T) {
	resetGitHubWebhookCounter()
	h := NewHandler(context.Background(), testSecret, slog.Default())
	body := `{"ref":"refs/heads/main","after":"abc123"}`
	sig := sign(body, testSecret)

	req := httptest.NewRequest("POST", "/webhooks/github", strings.NewReader(body))
	req.Header.Set(signatureHeader, sig)
	req.Header.Set(eventHeader, "push")
	req.Header.Set(deliveryHeader, "delivery-counter-1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	got := getCounter("push", "accepted")
	if got != 1 {
		t.Fatalf("counter push,accepted = %.0f, want 1", got)
	}
}

func TestWebhook_CounterIncremented_InvalidSignature(t *testing.T) {
	resetGitHubWebhookCounter()
	h := NewHandler(context.Background(), testSecret, slog.Default())
	req := httptest.NewRequest("POST", "/webhooks/github", strings.NewReader("{}"))
	req.Header.Set(signatureHeader, "sha256=deadbeef")
	req.Header.Set(eventHeader, "push")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if got := getCounter("push", "invalid_signature"); got != 1 {
		t.Fatalf("counter push,invalid_signature = %.0f, want 1", got)
	}
	if got := getCounter("push", "accepted"); got != 0 {
		t.Fatalf("counter push,accepted = %.0f, want 0 (must NOT increment on invalid signature)", got)
	}
}

func TestWebhook_CounterIncremented_MissingSignature(t *testing.T) {
	resetGitHubWebhookCounter()
	h := NewHandler(context.Background(), testSecret, slog.Default())
	req := httptest.NewRequest("POST", "/webhooks/github", strings.NewReader("{}"))
	req.Header.Set(eventHeader, "push")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if got := getCounter("push", "invalid_signature"); got != 1 {
		t.Fatalf("missing-sig counter push,invalid_signature = %.0f, want 1", got)
	}
}

func TestWebhook_PropagatesGitHubDeliveryAsCorrelationID(t *testing.T) {
	// Plan J-09 T4.1 — the handler's slog.Logger must carry
	// correlation_id == X-GitHub-Delivery so downstream handlers
	// inherit the same id. We capture log output and assert the
	// attribute is present.
	var buf strings.Builder
	captureLogger := slog.New(slog.NewJSONHandler(&logWriter{w: &buf}, &slog.HandlerOptions{Level: slog.LevelInfo}))
	h := NewHandler(context.Background(), testSecret, captureLogger)

	body := `{"ref":"refs/heads/main","after":"abc123"}`
	sig := sign(body, testSecret)
	req := httptest.NewRequest("POST", "/webhooks/github", strings.NewReader(body))
	req.Header.Set(signatureHeader, sig)
	req.Header.Set(eventHeader, "push")
	req.Header.Set(deliveryHeader, "uuid-abc-123")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	got := buf.String()
	if !strings.Contains(got, `"correlation_id":"uuid-abc-123"`) {
		t.Fatalf("expected correlation_id=uuid-abc-123 in log output, got: %s", got)
	}
}

func TestWebhook_FallsBackToXCorrelationIDHeader_WhenDeliveryMissing(t *testing.T) {
	// If GitHub did NOT send X-GitHub-Delivery (impossible in prod, but
	// useful for replay tooling), use the X-Correlation-Id header so a
	// front-proxy can still thread the id through.
	var buf strings.Builder
	captureLogger := slog.New(slog.NewJSONHandler(&logWriter{w: &buf}, &slog.HandlerOptions{Level: slog.LevelInfo}))
	h := NewHandler(context.Background(), testSecret, captureLogger)

	body := `{"ref":"refs/heads/main","after":"abc123"}`
	sig := sign(body, testSecret)
	req := httptest.NewRequest("POST", "/webhooks/github", strings.NewReader(body))
	req.Header.Set(signatureHeader, sig)
	req.Header.Set(eventHeader, "push")
	req.Header.Set("X-Correlation-Id", "proxy-correlation-9")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	got := buf.String()
	if !strings.Contains(got, `"correlation_id":"proxy-correlation-9"`) {
		t.Fatalf("expected fallback correlation_id=proxy-correlation-9 in log output, got: %s", got)
	}
}

// resetGitHubWebhookCounter clears the counter between tests. Counters
// only support Add (not Reset) -- the cleanest hermetic strategy is to
// re-register a fresh CounterVec. Since the metric uses promauto we
// can't unregister cleanly; instead we record the delta vs baseline.
var counterBaseline = map[string]float64{}

func getCounter(eventType, status string) float64 {
	key := eventType + "|" + status
	val := readCounterValue(eventType, status)
	return val - counterBaseline[key]
}

func resetGitHubWebhookCounter() {
	// Snapshot current values so getCounter returns the delta for this
	// test. Works under parallel tests too because t.Parallel is not
	// used in this suite (each test gets fresh state by snapshotting).
	for _, st := range []string{"accepted", "invalid_signature", "duplicate", "read_error"} {
		for _, ev := range []string{"push", "pull_request", "ping", "installation"} {
			counterBaseline[ev+"|"+st] = readCounterValue(ev, st)
		}
	}
}

// readCounterValue uses the prometheus testutil helper to extract the
// current value from a labelled counter for a given (event_type, status).
// We declare a thin wrapper so the test file does not need to depend on
// testutil directly when the package is excluded from test builds.
func readCounterValue(eventType, status string) float64 {
	// metrics.GitHubWebhookDeliveriesTotal is a *CounterVec; obtain the
	// concrete Counter for these labels and read it via promtest.
	return promCounterValue(eventType, status)
}

// logWriter is a slog.Writer indirection -- writes to a strings.Builder
// without violating the io.Writer contract (Builder's Write needs []byte).
type logWriter struct{ w *strings.Builder }

func (l *logWriter) Write(p []byte) (int, error) { return l.w.Write(p) }
