// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package timelinehttp

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// TestRegisterDeployTimelineRoute_MountsPath confirms the helper wires
// the canonical path AND honors the injected TenantResolver.
func TestRegisterDeployTimelineRoute_MountsPath(t *testing.T) {
	store := &memDeployTimelineStore{
		tenantID: "tn-test",
		deployID: "dep-mount",
	}
	store.appendEvent("deploy_timeline.live.completed", `{}`)

	resolver := func(ctx context.Context) (string, bool) {
		return "tn-test", true
	}
	r := chi.NewRouter()
	RegisterDeployTimelineRoute(r, store, resolver,
		WithDTPollInterval(10*time.Millisecond),
		WithDTMaxDuration(time.Second),
	)

	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/deploys/dep-mount/timeline")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q; want text/event-stream", got)
	}

	scanner := bufio.NewScanner(resp.Body)
	terminal := false
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "deploy_timeline.live.completed") {
			terminal = true
		}
	}
	if !terminal {
		t.Errorf("expected terminal event in stream")
	}
}

// TestRegisterDeployTimelineRoute_UnauthorizedWhenResolverEmpty
// confirms a resolver returning false yields 401 (defense vs.
// accidental missing-middleware wiring).
func TestRegisterDeployTimelineRoute_UnauthorizedWhenResolverEmpty(t *testing.T) {
	store := &memDeployTimelineStore{tenantID: "tn-test", deployID: "dep-mount"}
	resolver := func(ctx context.Context) (string, bool) {
		return "", false
	}
	r := chi.NewRouter()
	RegisterDeployTimelineRoute(r, store, resolver, WithDTMaxDuration(time.Second))
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/deploys/dep-mount/timeline")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d; want 401", resp.StatusCode)
	}
}
