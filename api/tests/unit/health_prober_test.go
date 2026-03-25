// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/loopzero/loopzero/api/internal/health"
)

func TestHTTPProber_DirectProbe(t *testing.T) {
	// Arrange — server that returns 200 on /health
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	prober := &health.HTTPProber{
		Client: srv.Client(),
	}

	// Act
	err := prober.Probe(context.Background(), srv.URL, "/health")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestHTTPProber_DirectProbe_Unhealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	prober := &health.HTTPProber{
		Client:   srv.Client(),
		Backoffs: []time.Duration{0}, // Single attempt, no wait — default 10 retries over 120s is for production
	}

	err := prober.Probe(context.Background(), srv.URL, "/health")

	if err == nil {
		t.Fatal("expected error for unhealthy service, got nil")
	}
	if !strings.Contains(err.Error(), "status 503") {
		t.Errorf("expected error about status 503, got %v", err)
	}
}

func TestHTTPProber_IngressOverride_SendsHostHeader(t *testing.T) {
	// Arrange — server that checks Host header
	var receivedHost string
	var receivedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHost = r.Host
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	prober := &health.HTTPProber{
		Client:     srv.Client(),
		IngressURL: srv.URL, // Use test server as the "Ingress"
	}

	// Act — probe with an app URL that has a different hostname
	err := prober.Probe(context.Background(), "http://myapp.acme.loopzero.local", "/health")

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if receivedHost != "myapp.acme.loopzero.local" {
		t.Errorf("expected Host header 'myapp.acme.loopzero.local', got %q", receivedHost)
	}
	if receivedPath != "/health" {
		t.Errorf("expected path '/health', got %q", receivedPath)
	}
}

func TestHTTPProber_IngressOverride_UnhealthyApp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	prober := &health.HTTPProber{
		Client:     srv.Client(),
		IngressURL: srv.URL,
		Backoffs:   []time.Duration{0}, // Single attempt — see comment on DirectProbe_Unhealthy
	}

	err := prober.Probe(context.Background(), "http://myapp.acme.loopzero.local", "/health")

	if err == nil {
		t.Fatal("expected error for unhealthy app via Ingress, got nil")
	}
}

func TestHTTPProber_NoIngressURL_ProbesDirectly(t *testing.T) {
	// Verify that without IngressURL, the prober goes to the app URL directly
	var receivedHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHost = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	prober := &health.HTTPProber{
		Client: srv.Client(),
		// IngressURL not set
	}

	err := prober.Probe(context.Background(), srv.URL, "/health")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// Host should be the test server's host (not overridden)
	if !strings.Contains(receivedHost, "127.0.0.1") {
		t.Errorf("expected direct host (127.0.0.1:...), got %q", receivedHost)
	}
}
