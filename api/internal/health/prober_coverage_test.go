// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// =============================================================================
// resolveEndpoint tests (previously 25% coverage)
// =============================================================================

func TestResolveEndpoint_DirectProbe(t *testing.T) {
	p := &HTTPProber{}

	endpoint, host, err := p.resolveEndpoint("https://app.example.com", "/health")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if endpoint != "https://app.example.com/health" {
		t.Errorf("endpoint = %q, want https://app.example.com/health", endpoint)
	}
	if host != "" {
		t.Errorf("host should be empty for direct probe, got %q", host)
	}
}

func TestResolveEndpoint_IngressProbe(t *testing.T) {
	p := &HTTPProber{
		IngressURL: "http://localhost:8090",
	}

	endpoint, host, err := p.resolveEndpoint("https://app.tenant.loopzero.local", "/ready")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if endpoint != "http://localhost:8090/ready" {
		t.Errorf("endpoint = %q, want http://localhost:8090/ready", endpoint)
	}
	if host != "app.tenant.loopzero.local" {
		t.Errorf("host = %q, want app.tenant.loopzero.local", host)
	}
}

func TestResolveEndpoint_InvalidAppURL(t *testing.T) {
	p := &HTTPProber{
		IngressURL: "http://localhost:8090",
	}

	_, _, err := p.resolveEndpoint("://invalid-url", "/health")
	if err == nil {
		t.Fatal("expected error for invalid app URL")
	}
}

// =============================================================================
// Probe tests — additional edge cases
// =============================================================================

func TestProbe_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	p := &HTTPProber{
		Client:   &http.Client{Timeout: 1 * time.Second},
		Backoffs: []time.Duration{0},
	}

	err := p.Probe(ctx, "http://localhost:1", "/health")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestProbe_ServerReturns500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	p := &HTTPProber{
		Client:   server.Client(),
		Backoffs: []time.Duration{0}, // single attempt, no retry
	}

	err := p.Probe(context.Background(), server.URL, "/health")
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
}

func TestProbe_ServerReturns200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := &HTTPProber{
		Client:   server.Client(),
		Backoffs: []time.Duration{0},
	}

	err := p.Probe(context.Background(), server.URL, "/health")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProbe_ServerReturns301(t *testing.T) {
	// 301 is in 200-399 range, should be considered healthy
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMovedPermanently)
	}))
	defer server.Close()

	p := &HTTPProber{
		Client:   &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		Backoffs: []time.Duration{0},
	}

	err := p.Probe(context.Background(), server.URL, "/")
	if err != nil {
		t.Fatalf("301 should be considered healthy: %v", err)
	}
}

func TestProbe_WithIngressURL_SetsHostHeader(t *testing.T) {
	var receivedHost string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHost = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := &HTTPProber{
		Client:     server.Client(),
		IngressURL: server.URL,
		Backoffs:   []time.Duration{0},
	}

	err := p.Probe(context.Background(), "https://myapp.tenant.loopzero.local", "/health")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedHost != "myapp.tenant.loopzero.local" {
		t.Errorf("Host header = %q, want myapp.tenant.loopzero.local", receivedHost)
	}
}

func TestProbe_RetrySucceedsOnSecondAttempt(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := &HTTPProber{
		Client:   server.Client(),
		Backoffs: []time.Duration{0, 0}, // two immediate attempts
	}

	err := p.Probe(context.Background(), server.URL, "/health")
	if err != nil {
		t.Fatalf("should succeed on second attempt: %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}
