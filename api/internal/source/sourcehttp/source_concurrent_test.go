// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package sourcehttp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/routestest"

	"github.com/getloopscale/loopzero/api/internal/source"
)

// fakeRaceySourceStore simulates the concurrent-upload race observed in
// production: when two clients push to the same project's Gitea repo at
// the same time, the second push gets one of the canonical race errors
// the handler at source.go:285-292 maps to DEPLOY_IN_PROGRESS.
//
// One goroutine succeeds (the first to win the compare-and-swap on
// firstUpload); subsequent callers receive the race error. The test then asserts:
//   - No panic / no data race (-race detector)
//   - Exactly 1 goroutine sees 200 OK
//   - The other N-1 see 409 DEPLOY_IN_PROGRESS (NOT a generic 500)
type fakeRaceySourceStore struct {
	firstUpload   atomic.Bool // CAS — first writer wins
	pushCallCount atomic.Int32
	raceErr       error
}

func (f *fakeRaceySourceStore) PushBundle(_ context.Context, _, _ string, bundle io.Reader) (source.PushResult, error) {
	f.pushCallCount.Add(1)
	// Drain the bundle so callers don't deadlock on body close.
	_, _ = io.Copy(io.Discard, bundle)
	// CAS: only the first call wins. All subsequent calls hit the race
	// path and return the canonical Gitea race error.
	if !f.firstUpload.CompareAndSwap(false, true) {
		return source.PushResult{}, f.raceErr
	}
	return source.PushResult{SourceID: "deadbeef"}, nil
}

// TestSourceHandler_Upload_ConcurrentRaceResolvesAsDeployInProgress —
// T3.3 of plan 2026-06-11-code-review-remediation v1.2 (canonical
// closure of #126 CRITICAL — SourceHandler.Upload had zero concurrent
// coverage, masking the race-path branch at source.go:285-292 from
// regression detection).
//
// 10 goroutines POST identical upload payloads to the same project.
// Per the production race contract:
//   - Exactly one wins (200 OK)
//   - All others see DEPLOY_IN_PROGRESS (409 with code, NOT generic 500)
//   - No data race detected (-race flag stays clean)
//
// Run via: go test -race ./internal/source/sourcehttp/ -run "TestSourceHandler_Upload_ConcurrentRaceResolvesAsDeployInProgress"
func TestSourceHandler_Upload_ConcurrentRaceResolvesAsDeployInProgress(t *testing.T) {
	const N = 10

	store := &fakeRaceySourceStore{
		// Mirror the production error string the handler greps for at
		// source.go:285-289 (Gitea's `git push` ref-lock collision).
		raceErr: errors.New("git push: cannot lock ref 'refs/heads/main'"),
	}
	handler := NewSourceHandler(store)

	// Minimal canonical services.json v2 payload (per services.schema.json
	// — every required Service property surfaced verbatim).
	servicesJSON := `{"version":2,"project":"my-api","services":[{` +
		`"name":"web","runtime":"node","port":3000,` +
		`"proxy":"/api","dev":"npm run dev","start":"node server.js",` +
		`"healthcheck":"/health","cors":false,"passSetCookie":false}]}`
	buildMultipart := func() (*bytes.Buffer, string) {
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		// IMPORTANT: services_json MUST be first per the EC-5 wire-
		// protocol contract enforced at source.go:117-126. Reusing the
		// same order across all N goroutines keeps the test focused on
		// the race-path, not on order-of-fields validation.
		_ = w.WriteField("services_json", servicesJSON)
		bundlePart, _ := w.CreateFormFile("bundle", "bundle.tar.gz")
		// Minimal gzip header so source.PushBundle's format detector
		// doesn't reject upfront — the fake store drains the body and
		// returns either success or race error regardless.
		_, _ = bundlePart.Write([]byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
		_ = w.Close()
		return &body, w.FormDataContentType()
	}

	type result struct {
		status int
		body   string
	}
	results := make([]result, N)

	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			body, contentType := buildMultipart()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/source/upload", body)
			req.Header.Set("Content-Type", contentType)
			req = routestest.WithAuth(req, routestest.DefaultUserContext())
			rec := httptest.NewRecorder()
			handler.Upload(rec, req)
			results[i] = result{status: rec.Code, body: rec.Body.String()}
		}()
	}
	wg.Wait()

	// Aggregate outcomes
	var ok, deployInProgress, other int
	for i, r := range results {
		switch r.status {
		case http.StatusOK:
			ok++
		case http.StatusConflict: // ErrDeployInProgress maps to 409
			deployInProgress++
			// Cheap contract assertion: the body MUST carry the typed
			// DEPLOY_IN_PROGRESS code, NOT a generic SOURCE_UPLOAD_FAILED
			// (which would mean the race branch at source.go:285-292
			// regressed to the generic-error path).
			if !bytesContains(r.body, "DEPLOY_IN_PROGRESS") {
				t.Errorf("goroutine %d: 409 missing DEPLOY_IN_PROGRESS code: %s",
					i, truncate(r.body, 200))
			}
		default:
			other++
			t.Errorf("goroutine %d: unexpected status %d body=%s",
				i, r.status, truncate(r.body, 200))
		}
	}

	if ok != 1 {
		t.Errorf("race contract: want exactly 1 winner, got %d (DEPLOY_IN_PROGRESS=%d other=%d)",
			ok, deployInProgress, other)
	}
	if deployInProgress != N-1 {
		t.Errorf("race contract: want %d losers as DEPLOY_IN_PROGRESS, got %d (ok=%d other=%d)",
			N-1, deployInProgress, ok, other)
	}
	// Sanity: the fake store recorded N PushBundle invocations
	if got := store.pushCallCount.Load(); got != N {
		t.Errorf("expected %d PushBundle calls, got %d", N, got)
	}
}

// bytesContains is a tiny helper used so the test file doesn't need to
// import strings just for one assertion.
func bytesContains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	if len(haystack) < len(needle) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return fmt.Sprintf("%s...(%d bytes)", s[:n], len(s))
}
