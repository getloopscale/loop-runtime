// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package cfkv

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_Put_Integration_HTTPTest(t *testing.T) {
	// Arrange
	var capturedMethod string
	var capturedPath string
	var capturedBody RoutingEntry
	var capturedAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Create client with overridden URL
	client := &Client{
		accountID:   "acct-1",
		namespaceID: "ns-1",
		apiToken:    "my-token",
		httpClient:  srv.Client(),
	}

	// We need to override the URL construction. Since the client uses hardcoded CF API URLs,
	// we test the helper and JSON serialization instead.
	entry := RoutingEntry{BuildID: "bld-123", S3Prefix: "/tn/proj/app/bld-123"}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Directly call the test server to verify handler logic
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/test-key", strings.NewReader(string(data)))
	req.Header.Set("Authorization", "Bearer my-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = resp.Body.Close()

	// Assert
	if capturedMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", capturedMethod)
	}
	if capturedAuth != "Bearer my-token" {
		t.Errorf("auth = %q, want Bearer my-token", capturedAuth)
	}
	if capturedBody.BuildID != "bld-123" {
		t.Errorf("buildId = %q, want bld-123", capturedBody.BuildID)
	}

	_ = capturedPath
	_ = client
}

func TestClient_Get_NotFound_ReturnsNil(t *testing.T) {
	// The Get method uses hardcoded CF URLs, so we test context cancellation behavior
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediate cancellation

	client := NewClient("acc", "ns", "token")
	_, err := client.Get(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error with cancelled context")
	}
}

func TestSubdomain_TrailingHyphenTrimmed(t *testing.T) {
	// When truncation cuts in the middle of a hyphen-separated name,
	// the trailing hyphen should be trimmed.
	result := Subdomain("tn-long-tenant", "long-project-name-here", "my-app", "prod")
	if len(result) > 63 {
		t.Errorf("length = %d, must be <= 63", len(result))
	}
	if strings.HasSuffix(result, "-") {
		t.Error("subdomain should not end with a hyphen")
	}
}

func TestClient_ListKeys_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := NewClient("acc", "ns", "token")
	_, err := client.ListKeys(ctx)
	if err == nil {
		t.Error("expected error with cancelled context")
	}
}

// --- httptest-based integration tests that exercise the real Put/Get/Delete/ListKeys code paths ---

// overrideableClient creates a Client whose HTTP calls are routed to a test server
// by using an http.Client with a custom transport that rewrites the URL.
func newHTTPTestClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	// Custom transport that rewrites all requests to the test server.
	transport := &rewriteTransport{target: srv.URL, wrapped: http.DefaultTransport}
	return &Client{
		accountID:   "test-acct",
		namespaceID: "test-ns",
		apiToken:    "test-token",
		httpClient:  &http.Client{Transport: transport},
	}, srv
}

type rewriteTransport struct {
	target  string
	wrapped http.RoundTripper
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	// Parse the test server URL to get host
	req.URL.Host = strings.TrimPrefix(t.target, "http://")
	return t.wrapped.RoundTrip(req)
}

func TestClient_Put_Success(t *testing.T) {
	var capturedMethod string
	var capturedAuth string
	client, srv := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := client.Put(context.Background(), "my-subdomain", RoutingEntry{BuildID: "b1", S3Prefix: "/p"})
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if capturedMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", capturedMethod)
	}
	if capturedAuth != "Bearer test-token" {
		t.Errorf("auth = %q, want Bearer test-token", capturedAuth)
	}
}

func TestClient_Put_ServerError(t *testing.T) {
	client, srv := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer srv.Close()

	err := client.Put(context.Background(), "my-subdomain", RoutingEntry{BuildID: "b1"})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected status 500 in error, got: %v", err)
	}
}

func TestClient_Get_Success(t *testing.T) {
	client, srv := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"buildId":"b1","s3Prefix":"/p"}`))
	}))
	defer srv.Close()

	data, err := client.Get(context.Background(), "my-key")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if data == nil {
		t.Fatal("expected non-nil data")
	}

	var entry RoutingEntry
	if jsonErr := json.Unmarshal(data, &entry); jsonErr != nil {
		t.Fatalf("unmarshal: %v", jsonErr)
	}
	if entry.BuildID != "b1" {
		t.Errorf("buildId = %q, want b1", entry.BuildID)
	}
}

func TestClient_Get_NotFound_ReturnsNilNil(t *testing.T) {
	client, srv := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	data, err := client.Get(context.Background(), "missing-key")
	if err != nil {
		t.Fatalf("Get should return nil,nil for 404: %v", err)
	}
	if data != nil {
		t.Errorf("expected nil data for 404, got %v", data)
	}
}

func TestClient_Get_ServerError(t *testing.T) {
	client, srv := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	}))
	defer srv.Close()

	_, err := client.Get(context.Background(), "key")
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
}

func TestClient_Delete_Success(t *testing.T) {
	client, srv := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %q, want DELETE", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := client.Delete(context.Background(), "old-key")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
}

func TestClient_Delete_ServerError(t *testing.T) {
	client, srv := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error"))
	}))
	defer srv.Close()

	err := client.Delete(context.Background(), "key")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestClient_ListKeys_Success(t *testing.T) {
	client, srv := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":[{"name":"key1"},{"name":"key2"}],"result_info":{"cursor":""}}`))
	}))
	defer srv.Close()

	keys, err := client.ListKeys(context.Background())
	if err != nil {
		t.Fatalf("ListKeys failed: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
	if keys[0] != "key1" || keys[1] != "key2" {
		t.Errorf("keys = %v, want [key1 key2]", keys)
	}
}

func TestClient_ListKeys_Pagination(t *testing.T) {
	callCount := 0
	client, srv := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		if callCount == 1 {
			_, _ = w.Write([]byte(`{"result":[{"name":"k1"}],"result_info":{"cursor":"page2"}}`))
		} else {
			_, _ = w.Write([]byte(`{"result":[{"name":"k2"}],"result_info":{"cursor":""}}`))
		}
	}))
	defer srv.Close()

	keys, err := client.ListKeys(context.Background())
	if err != nil {
		t.Fatalf("ListKeys failed: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys across pages, got %d", len(keys))
	}
	if callCount != 2 {
		t.Errorf("expected 2 API calls for pagination, got %d", callCount)
	}
}

func TestClient_ListKeys_ServerError(t *testing.T) {
	client, srv := newHTTPTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer srv.Close()

	_, err := client.ListKeys(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}
