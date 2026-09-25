// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	healthyBody   = `{"status":"healthy","components":[{"name":"registry","status":"healthy"}]}`
	unhealthyBody = `{"status":"unhealthy","components":[{"name":"registry","status":"unhealthy"}]}`
)

// healthServer answers /api/v2.0/health with responses[i] on the i-th call and
// repeats the last one afterwards. A negative status truncates the body
// mid-stream, so reading it fails.
type healthReply struct {
	status int
	body   string
}

func healthServer(t *testing.T, replies ...healthReply) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2.0/health" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		n := int(calls.Add(1)) - 1
		if n >= len(replies) {
			n = len(replies) - 1
		}
		reply := replies[n]
		if reply.status < 0 {
			w.Header().Set("Content-Length", "1000")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"status":`)
			return // the server closes the connection short of Content-Length
		}
		w.WriteHeader(reply.status)
		_, _ = io.WriteString(w, reply.body)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func newTestHarbor(srv *httptest.Server, timeout time.Duration) *harborInit {
	return &harborInit{
		client:       srv.Client(),
		baseURL:      srv.URL,
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		timeout:      timeout,
		pollInterval: time.Millisecond,
	}
}

func TestWaitHealthy_ReturnsOnceHarborReportsHealthy(t *testing.T) {
	srv, calls := healthServer(t, healthReply{http.StatusOK, healthyBody})

	err := newTestHarbor(srv, time.Second).waitHealthy(context.Background())

	if err != nil || calls.Load() != 1 {
		t.Fatalf("err = %v after %d calls, want nil after 1", err, calls.Load())
	}
}

func TestWaitHealthy_RetriesANon200Status(t *testing.T) {
	srv, calls := healthServer(t, healthReply{http.StatusServiceUnavailable, ""}, healthReply{http.StatusOK, healthyBody})

	if err := newTestHarbor(srv, time.Second).waitHealthy(context.Background()); err != nil || calls.Load() != 2 {
		t.Fatalf("err = %v after %d calls, want nil after 2", err, calls.Load())
	}
}

func TestWaitHealthy_RetriesAnUnhealthyReport(t *testing.T) {
	srv, calls := healthServer(t, healthReply{http.StatusOK, unhealthyBody}, healthReply{http.StatusOK, healthyBody})

	if err := newTestHarbor(srv, time.Second).waitHealthy(context.Background()); err != nil || calls.Load() != 2 {
		t.Fatalf("err = %v after %d calls, want nil after 2", err, calls.Load())
	}
}

func TestWaitHealthy_RetriesAnUnparseableBody(t *testing.T) {
	srv, calls := healthServer(t, healthReply{http.StatusOK, "<html>starting</html>"}, healthReply{http.StatusOK, healthyBody})

	if err := newTestHarbor(srv, time.Second).waitHealthy(context.Background()); err != nil || calls.Load() != 2 {
		t.Fatalf("err = %v after %d calls, want nil after 2", err, calls.Load())
	}
}

// A body that cannot be read is not a health report. It used to be read as
// whatever bytes arrived, with the read error discarded.
func TestWaitHealthy_RetriesABodyThatCannotBeRead(t *testing.T) {
	srv, calls := healthServer(t, healthReply{-1, ""}, healthReply{http.StatusOK, healthyBody})

	if err := newTestHarbor(srv, time.Second).waitHealthy(context.Background()); err != nil || calls.Load() != 2 {
		t.Fatalf("err = %v after %d calls, want nil after 2", err, calls.Load())
	}
}

func TestWaitHealthy_RetriesAConnectionFailure(t *testing.T) {
	srv, _ := healthServer(t, healthReply{http.StatusOK, healthyBody})
	h := newTestHarbor(srv, 50*time.Millisecond)
	h.baseURL = "http://127.0.0.1:1" // nothing listens here

	err := h.waitHealthy(context.Background())

	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("err = %v, want a timeout after retrying the refused connection", err)
	}
}

func TestWaitHealthy_TimesOutWhileHarborStaysUnhealthy(t *testing.T) {
	srv, calls := healthServer(t, healthReply{http.StatusOK, unhealthyBody})

	err := newTestHarbor(srv, 30*time.Millisecond).waitHealthy(context.Background())

	if err == nil || !strings.Contains(err.Error(), "timeout after 30ms") {
		t.Fatalf("err = %v, want the timeout error", err)
	}
	if calls.Load() < 2 {
		t.Fatalf("polled %d times, want it to keep polling until the deadline", calls.Load())
	}
}

func TestWaitHealthy_StopsWhenTheContextIsCancelled(t *testing.T) {
	srv, _ := healthServer(t, healthReply{http.StatusOK, unhealthyBody})
	h := newTestHarbor(srv, time.Minute)
	h.pollInterval = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)

	if err := h.waitHealthy(ctx); err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestVerifyStorage_RefusesAnUnhealthyRegistry(t *testing.T) {
	srv, _ := healthServer(t, healthReply{http.StatusOK, unhealthyBody})

	err := newTestHarbor(srv, time.Second).verifyStorage(context.Background())

	if err == nil || !strings.Contains(err.Error(), `"registry" is unhealthy`) {
		t.Fatalf("err = %v, want the unhealthy registry named", err)
	}
}

func TestVerifyStorage_AcceptsAHealthyRegistry(t *testing.T) {
	srv, _ := healthServer(t, healthReply{http.StatusOK, healthyBody})

	if err := newTestHarbor(srv, time.Second).verifyStorage(context.Background()); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestVerifyStorage_FailsOnABodyThatCannotBeRead(t *testing.T) {
	srv, _ := healthServer(t, healthReply{-1, ""})

	if err := newTestHarbor(srv, time.Second).verifyStorage(context.Background()); err == nil {
		t.Fatal("a truncated health body must be an error, not a parse of whatever arrived")
	}
}
