// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package argocd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
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

	return NewSyncClient(SyncClientConfig{
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

	client := NewSyncClient(SyncClientConfig{
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
