// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPProber_HealthyServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("expected path /health, got %s", r.URL.Path)
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	prober := &HTTPProber{Client: srv.Client(), Backoffs: []time.Duration{0, 0, 0}}
	err := prober.Probe(context.Background(), srv.URL, "/health")
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
}

func TestHTTPProber_ServerReturns500ThreeTimes(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(500)
	}))
	defer srv.Close()

	prober := &HTTPProber{Client: srv.Client(), Backoffs: []time.Duration{0, 0, 0}}
	err := prober.Probe(context.Background(), srv.URL, "/health")
	if err == nil {
		t.Fatal("expected error after 3 failed attempts")
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", calls.Load())
	}
}

func TestHTTPProber_FailThenRecover(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	prober := &HTTPProber{Client: srv.Client(), Backoffs: []time.Duration{0, 0, 0}}
	err := prober.Probe(context.Background(), srv.URL, "/health")
	if err != nil {
		t.Fatalf("expected success on 3rd attempt, got: %v", err)
	}
	if calls.Load() != 3 {
		t.Errorf("expected 3 calls, got %d", calls.Load())
	}
}

func TestHTTPProber_UnreachableServer(t *testing.T) {
	prober := &HTTPProber{Client: &http.Client{}, Backoffs: []time.Duration{0, 0, 0}}
	// Port 1 is almost certainly not listening
	err := prober.Probe(context.Background(), "http://127.0.0.1:1", "/health")
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

func TestHTTPProber_Redirect301IsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(301)
	}))
	defer srv.Close()

	// Disable follow redirects so we get the raw 301
	client := srv.Client()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	prober := &HTTPProber{Client: client}
	err := prober.Probe(context.Background(), srv.URL, "/health")
	if err != nil {
		t.Fatalf("expected 301 to be success (200-399), got: %v", err)
	}
}

func TestHTTPProber_Status400IsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
	}))
	defer srv.Close()

	prober := &HTTPProber{Client: srv.Client(), Backoffs: []time.Duration{0, 0, 0}}
	err := prober.Probe(context.Background(), srv.URL, "/health")
	if err == nil {
		t.Fatal("expected 400 to be failure")
	}
}

func TestHTTPProber_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	prober := &HTTPProber{Client: srv.Client(), Backoffs: []time.Duration{0, 0, 0}}
	err := prober.Probe(ctx, srv.URL, "/health")
	if err == nil {
		t.Fatal("expected error when context is cancelled")
	}
}
