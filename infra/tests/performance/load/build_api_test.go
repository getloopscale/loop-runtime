// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package load

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// These tests pin the load clients to the API's real build contract
// (api/internal/routes/builds: SubmitBuildResult{build_id} on 202,
// BuildStatusResponse{status}). DeploySimulator used to read `id` and poll
// `state`, so it got an empty build id and could never observe a finished
// build — every scenario built on it reported a timeout.

func newTestBuildAPI(srv *httptest.Server) buildAPI {
	return buildAPI{
		apiURL:       srv.URL,
		client:       srv.Client(),
		auth:         func(r *http.Request) { r.Header.Set("X-Service-Auth", "s") },
		pollInterval: time.Millisecond,
		retryBackoff: []time.Duration{time.Millisecond, time.Millisecond},
	}
}

func TestSubmitBuildReadsTheBuildIDTheAPIReturns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/builds" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Idempotency-Key") == "" {
			t.Error("submit carried no Idempotency-Key")
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"build_id":"bld-123","stream_url":"/api/v1/builds/bld-123/stream"}`)
	}))
	defer srv.Close()

	id, err := newTestBuildAPI(srv).submitBuild(context.Background(), map[string]string{"project_id": "p"}, "key-1")

	if err != nil || id != "bld-123" {
		t.Fatalf("submitBuild = (%q, %v), want (bld-123, nil)", id, err)
	}
}

func TestSubmitBuildRefusesAnAcceptedResponseWithoutABuildID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"id":"bld-123"}`)
	}))
	defer srv.Close()

	_, err := newTestBuildAPI(srv).submitBuild(context.Background(), map[string]string{}, "k")

	if err == nil || !strings.Contains(err.Error(), "build_id") {
		t.Fatalf("err = %v, want a refusal naming the missing build_id", err)
	}
}

func TestSubmitBuildRetriesCapacityBackpressure(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"build_id":"bld-2"}`)
	}))
	defer srv.Close()

	id, err := newTestBuildAPI(srv).submitBuild(context.Background(), map[string]string{}, "k")

	if err != nil || id != "bld-2" || calls.Load() != 2 {
		t.Fatalf("submitBuild = (%q, %v) after %d calls, want bld-2 after 2", id, err, calls.Load())
	}
}

func TestSubmitBuildDoesNotRetryAClientError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer srv.Close()

	_, err := newTestBuildAPI(srv).submitBuild(context.Background(), map[string]string{}, "k")

	if err == nil || calls.Load() != 1 {
		t.Fatalf("err=%v after %d calls, want one call and an error", err, calls.Load())
	}
}

func TestWaitForBuildObservesTheStatusField(t *testing.T) {
	var polls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/builds/bld-1" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		status := "RUNNING"
		if polls.Add(1) >= 2 {
			status = "SUCCEEDED"
		}
		_, _ = io.WriteString(w, `{"build_id":"bld-1","status":"`+status+`"}`)
	}))
	defer srv.Close()

	state, _, err := newTestBuildAPI(srv).waitForBuild(context.Background(), "bld-1")

	if err != nil || state != "SUCCEEDED" {
		t.Fatalf("waitForBuild = (%q, %v), want (SUCCEEDED, nil)", state, err)
	}
}

func TestWaitForBuildReportsAFailedBuildAsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"status":"FAILED"}`)
	}))
	defer srv.Close()

	state, _, err := newTestBuildAPI(srv).waitForBuild(context.Background(), "bld-1")

	if err == nil || state != "FAILED" {
		t.Fatalf("waitForBuild = (%q, %v), want (FAILED, error)", state, err)
	}
}

func TestWaitForBuildStopsAtTheDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"status":"QUEUED"}`)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	state, _, err := newTestBuildAPI(srv).waitForBuild(ctx, "bld-1")

	if err == nil || state != "TIMEOUT" {
		t.Fatalf("waitForBuild = (%q, %v), want (TIMEOUT, error)", state, err)
	}
}

// The regression itself: the simulator must see the build the API created and
// the moment it finishes.
func TestDeploySimulatorFollowsTheAPIBuildContract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"build_id":"bld-9"}`)
			return
		}
		_, _ = io.WriteString(w, `{"build_id":"bld-9","status":"SUCCEEDED"}`)
	}))
	defer srv.Close()
	sim := NewServiceAuthSimulator(srv.URL, "secret", "u", "tn", "e@x")
	sim.builds.pollInterval = time.Millisecond

	id, err := sim.CreateBuild(context.Background(), "proj", "production", "sha")
	if err != nil || id != "bld-9" {
		t.Fatalf("CreateBuild = (%q, %v), want (bld-9, nil)", id, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	state, _, err := sim.WaitForBuild(ctx, id)
	if err != nil || state != "SUCCEEDED" {
		t.Fatalf("WaitForBuild = (%q, %v), want (SUCCEEDED, nil)", state, err)
	}
}
