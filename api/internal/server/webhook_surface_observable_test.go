// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/api/internal/server"
	"github.com/getloopscale/loopzero/api/internal/surfaces"
)

// TestWebhookSurface_MissingSecretIsObservableWithoutReadingTheLog closes the third DoD
// bullet of B-104: the absence of the webhook secret must be detectable WITHOUT
// reading the startup log.
//
// Measured on the droplet on 2026-08-14: with the secret absent,
// `POST /api/v1/webhooks/github` answered 404 while `POST /api/v1/health`
// answered 405 — and nothing else anywhere reported that the surface was off.
// In production that shape means a customer's `git push` builds nothing, silently.
func TestWebhookSurface_MissingSecretIsObservableWithoutReadingTheLog(t *testing.T) {
	reg := surfaces.NewRegistry()
	_ = server.NewRouter(server.Config{Surfaces: reg, GitHubWebhookHandler: nil})
	metrics.PublishSurfaces(reg.All())

	if got := testutil.ToFloat64(metrics.SurfaceEnabled.WithLabelValues("github_webhook")); got != 0 {
		t.Errorf("with the secret absent the metric is %v, want 0 — the disabled "+
			"surface must be visible without opening the log", got)
	}

	off := reg.Disabled()
	var found bool
	for _, s := range off {
		if s.Name == "github_webhook" {
			found = true
			if s.Reason == "" {
				t.Error("the surface is recorded as disabled with no reason — " +
					"whoever sees the zero needs to know what to provision")
			}
		}
	}
	if !found {
		t.Fatal("the webhook does not appear among the disabled surfaces")
	}
}

// TestWebhookSurface_EnabledWhenTheSecretExists is the other half. A metric that
// only ever reported zero would be as useless as the silence it replaced, and a
// metric that disagreed with the routing table would be worse — so the test pairs
// the gauge with the actual reachability of the route.
func TestWebhookSurface_EnabledWhenTheSecretExists(t *testing.T) {
	reg := surfaces.NewRegistry()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	router := server.NewRouter(server.Config{Surfaces: reg, GitHubWebhookHandler: handler})
	metrics.PublishSurfaces(reg.All())

	if got := testutil.ToFloat64(metrics.SurfaceEnabled.WithLabelValues("github_webhook")); got != 1 {
		t.Errorf("with the handler present the metric is %v, want 1", got)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", nil))
	if rec.Code == http.StatusNotFound {
		t.Error("the webhook route returned 404 with the handler present — the metric " +
			"would be lying about a surface that does not exist")
	}
	if rec.Code != http.StatusTeapot {
		t.Errorf("the request did not reach the handler: code %d, want %d",
			rec.Code, http.StatusTeapot)
	}
}

// TestRouter_NoRegistryDoesNotPanic protects the hundred-odd existing call
// sites that build a Config without a Registry. A nil registry means "nobody is
// observing", which is exactly what a focused unit test wants; forcing every one
// of them to wire a registry would be churn with no reader.
func TestRouter_NoRegistryDoesNotPanic(t *testing.T) {
	router := server.NewRouter(server.Config{GitHubWebhookHandler: nil})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/health", nil))
	if rec.Code == 0 {
		t.Fatal("the router did not respond")
	}
}
