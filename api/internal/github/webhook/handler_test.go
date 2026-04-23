// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package webhook

import (
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
	h := NewHandler(testSecret, slog.Default())
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
	h := NewHandler(testSecret, slog.Default())

	req := httptest.NewRequest("POST", "/webhooks/github", strings.NewReader("{}"))
	req.Header.Set(eventHeader, "push")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestWebhook_InvalidSignature_Returns403(t *testing.T) {
	h := NewHandler(testSecret, slog.Default())

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
	h := NewHandler(testSecret, slog.Default())

	req := httptest.NewRequest("GET", "/webhooks/github", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 405 {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestWebhook_UnknownEvent_Returns200(t *testing.T) {
	h := NewHandler(testSecret, slog.Default())
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

	h := NewHandler(testSecret, slog.Default()).OnPush(func(event PushEvent) error {
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
			"full_name": "getloopscale/loopzero-repotest",
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
	if received.Repository.FullName != "getloopscale/loopzero-repotest" {
		t.Errorf("repo = %q, want getloopscale/loopzero-repotest", received.Repository.FullName)
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

	h := NewHandler(testSecret, slog.Default()).OnPullRequest(func(event PullRequestEvent) error {
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
		"repository": {"full_name": "getloopscale/loopzero-repotest"},
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
		Ref: "refs/heads/feature-x",
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
