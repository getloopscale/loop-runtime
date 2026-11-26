// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argocd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
)

// These tests run against real ArgoCD.
// Set ARGOCD_TEST_URL and ARGOCD_TEST_TOKEN to enable.
// Without them, tests are skipped — not faked.

func skipIfNoArgoCD(t *testing.T) (string, string) {
	t.Helper()
	url := os.Getenv("ARGOCD_TEST_URL")
	if url == "" {
		t.Skip("ARGOCD_TEST_URL not set — skipping ArgoCD integration test")
	}
	token := os.Getenv("ARGOCD_TEST_TOKEN")
	if token == "" {
		t.Skip("ARGOCD_TEST_TOKEN not set — skipping ArgoCD integration test")
	}
	return url, token
}

func newTestSyncClient(t *testing.T) *SyncClient {
	t.Helper()
	url, token := skipIfNoArgoCD(t)

	return mustNewSyncClient(t, SyncClientConfig{
		BaseURL:  url,
		Token:    token,
		Insecure: true, // k3d uses self-signed certs
	})
}

func TestSyncClient_TriggerSyncNonExistentApp(t *testing.T) {
	client := newTestSyncClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := client.TriggerSync(ctx, "nonexistent-app-12345")
	if err == nil {
		t.Fatal("expected error for non-existent application")
	}
	t.Logf("expected error: %v", err)
}

func TestSyncClient_WaitForSyncTimeout(t *testing.T) {
	client := newTestSyncClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := client.WaitForSync(ctx, "nonexistent-app-12345", 2*time.Second, nil)
	if err == nil {
		t.Fatal("expected error for non-existent application")
	}
	t.Logf("expected error: %v", err)
}

// CHG-2026-05-05-DOGFOOD-B32: TriggerSync must hard-refresh the Application
// before POST /sync. ArgoCD repo-server caches manifest errors for ~5min;
// without hard-refresh, the first sync after an empty bootstrap commit fails
// with the misleading "InvalidSpecError: project does not exist".
//
// Verifies the request sequence: GET ?refresh=hard → POST /sync.
func TestSyncClient_TriggerSync_HardRefreshesBeforeSync(t *testing.T) {
	type call struct {
		method string
		path   string
		query  string
	}
	var calls []call
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, call{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.RawQuery,
		})
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()

	client := mustNewSyncClient(t, SyncClientConfig{
		BaseURL: server.URL,
		Token:   "test-token",
	})
	if err := client.TriggerSync(context.Background(), "tn-foo-production"); err != nil {
		t.Fatalf("TriggerSync: %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 HTTP calls (refresh + sync), got %d: %+v", len(calls), calls)
	}
	// Call 1: GET /api/v1/applications/{name}?refresh=hard
	if calls[0].method != http.MethodGet {
		t.Errorf("call[0] method: got %q, want GET", calls[0].method)
	}
	if calls[0].path != "/api/v1/applications/tn-foo-production" {
		t.Errorf("call[0] path: got %q", calls[0].path)
	}
	if calls[0].query != "refresh=hard" {
		t.Errorf("call[0] query: got %q, want refresh=hard", calls[0].query)
	}
	// Call 2: POST /api/v1/applications/{name}/sync
	if calls[1].method != http.MethodPost {
		t.Errorf("call[1] method: got %q, want POST", calls[1].method)
	}
	if calls[1].path != "/api/v1/applications/tn-foo-production/sync" {
		t.Errorf("call[1] path: got %q", calls[1].path)
	}
}

// CHG-2026-05-05-DOGFOOD-B32: hard-refresh failure must NOT block sync.
// If repo-server is slow or the refresh endpoint is broken, the sync POST
// should still proceed — the controller's own refresh interval may have
// already invalidated the cache.
func TestSyncClient_TriggerSync_HardRefreshFailureDoesNotBlockSync(t *testing.T) {
	var syncCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GET refresh=hard returns 500; POST sync returns 200.
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("{}"))
			return
		}
		if r.Method == http.MethodPost {
			syncCalled = true
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()

	client := mustNewSyncClient(t, SyncClientConfig{
		BaseURL: server.URL,
		Token:   "test-token",
	})
	if err := client.TriggerSync(context.Background(), "tn-foo-production"); err != nil {
		t.Fatalf("TriggerSync should not return error when refresh fails: %v", err)
	}
	if !syncCalled {
		t.Error("expected POST /sync to be called even when hard-refresh fails")
	}
}

// B11 (CHG-2026-04-23): TerminateOperation must send Content-Type: application/json.
// ArgoCD rejects DELETE /applications/{name}/operation with HTTP 415
// "Invalid content type" when the header is missing. Without this, stuck
// sync operations cannot be terminated and release convergence blocks.
func TestSyncClient_TerminateOperation_SendsContentType(t *testing.T) {
	var capturedContentType string
	var capturedMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedContentType = r.Header.Get("Content-Type")
		capturedMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := mustNewSyncClient(t, SyncClientConfig{
		BaseURL: server.URL,
		Token:   "test-token",
	})
	if err := client.TerminateOperation(context.Background(), "some-app"); err != nil {
		t.Fatalf("TerminateOperation: %v", err)
	}

	if capturedMethod != http.MethodDelete {
		t.Errorf("method: got %q, want DELETE", capturedMethod)
	}
	if capturedContentType != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", capturedContentType, "application/json")
	}
}

// A 404 on POST /sync is the one outcome delivery may treat as "the
// Application does not exist yet, retry later". It is carried by a sentinel,
// not by the message text, so callers never have to match "not found".
func TestSyncClient_TriggerSync_404WrapsApplicationNotFoundSentinel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	client := mustNewSyncClient(t, SyncClientConfig{BaseURL: server.URL, Token: "test-token"})

	err := client.TriggerSync(context.Background(), "tn-foo-production")

	if !errors.Is(err, deploycontracts.ErrSyncApplicationNotFound) {
		t.Fatalf("expected ErrSyncApplicationNotFound, got %v", err)
	}
}
