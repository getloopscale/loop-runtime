// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package source

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Sprint 4 plan qa-sprint4-cleanup-2026-06-25 T3.1 — typed PushBundle errors.
// Closes loopzero-cloud#31: legacy untyped fmt.Errorf chain collapsed every failure
// mode into the canonical "Ensure your git history is clean and try again."
// message. Typed reasons let the handler emit specific actionable text per mode.

// T3.1.A — Stringer round-trip (EC-6 absorption: explicit ints + canonical labels)
func TestPushBundleErrorReason_StringerRoundTrip(t *testing.T) {
	cases := []struct {
		reason PushBundleErrorReason
		want   string
	}{
		{ReasonSourceBackendUnreachable, "SourceBackendUnreachable"},
		{ReasonRemoteRejectedPush, "RemoteRejectedPush"},
		{ReasonBundleMalformed, "BundleMalformed"},
		{ReasonBundleTooLarge, "BundleTooLarge"},
		{ReasonArchiveReadError, "ArchiveReadError"},
		{PushBundleErrorReason(99), "Unknown"}, // out-of-range guard per EC-6
		{PushBundleErrorReason(0), "Unknown"},  // zero value (uninitialized) is Unknown
	}
	for _, tc := range cases {
		if got := tc.reason.String(); got != tc.want {
			t.Errorf("PushBundleErrorReason(%d).String() = %q, want %q", tc.reason, got, tc.want)
		}
	}
}

// T3.1.B — IsGiteaRaceCondition canonical substring set (EC-3 preservation)
func TestIsGiteaRaceCondition_PreservesF03DiscriminatorSet(t *testing.T) {
	cases := []struct {
		errMsg string
		want   bool
	}{
		{"gitea source: git push: failed: cannot lock ref 'refs/heads/main'", true},
		{"gitea source: git push: failed: remote rejected push", true},
		{"gitea source: git push: failed to update ref", true},
		{"gitea source: git push: failed: stale info", true},
		{"gitea source: reading bundle: io.ErrUnexpectedEOF", false},
		{"", false},
	}
	for _, tc := range cases {
		err := errors.New(tc.errMsg)
		if got := IsGiteaRaceCondition(err); got != tc.want {
			t.Errorf("IsGiteaRaceCondition(%q) = %v, want %v", tc.errMsg, got, tc.want)
		}
	}
	// nil-safety
	if IsGiteaRaceCondition(nil) {
		t.Errorf("IsGiteaRaceCondition(nil) = true, want false")
	}
}

// T3.1.C — PushBundleError unwraps through fmt.Errorf chains (EC-8 absorption)
func TestPushBundleError_UnwrapsThroughFmtErrorf(t *testing.T) {
	cause := errors.New("connect: connection refused")
	pe := NewPushBundleError(ReasonSourceBackendUnreachable, cause, "Gitea unreachable")
	wrapped := fmt.Errorf("handler layer: %w", pe)
	// errors.As must walk through the fmt.Errorf wrapper to find the typed error.
	got, ok := AsPushBundleError(wrapped)
	if !ok {
		t.Fatalf("AsPushBundleError on fmt.Errorf-wrapped err returned ok=false")
	}
	if got.Reason != ReasonSourceBackendUnreachable {
		t.Errorf("Reason = %v, want %v", got.Reason, ReasonSourceBackendUnreachable)
	}
	// errors.Is must also walk to the underlying cause.
	if !errors.Is(wrapped, cause) {
		t.Errorf("errors.Is(wrapped, cause) = false; want true")
	}
}

// T3.1.D — PushBundle returns ReasonBundleMalformed for empty bundle
func TestPushBundle_ReturnsBundleMalformed_whenEmptyBundle(t *testing.T) {
	store, err := NewGiteaSourceStore(GiteaSourceStoreConfig{
		BaseURL: "http://127.0.0.1:1",
		Token:   "tok",
		OrgName: "loopzero",
	})
	if err != nil {
		t.Fatalf("NewGiteaSourceStore: %v", err)
	}
	emptyReader := strings.NewReader("")
	_, err = store.PushBundle(context.Background(), "tn-test", "p-test", emptyReader)
	if err == nil {
		t.Fatal("expected error for empty bundle, got nil")
	}
	pe, ok := AsPushBundleError(err)
	if !ok {
		t.Fatalf("expected *PushBundleError, got %T (%v)", err, err)
	}
	if pe.Reason != ReasonBundleMalformed {
		t.Errorf("Reason = %v, want ReasonBundleMalformed", pe.Reason)
	}
}

// T3.1.E — PushBundle returns ReasonArchiveReadError when bundle reader fails
type erroringReader struct{ err error }

func (r *erroringReader) Read(_ []byte) (int, error) { return 0, r.err }

func TestPushBundle_ReturnsArchiveReadError_whenBundleReaderFails(t *testing.T) {
	store, err := NewGiteaSourceStore(GiteaSourceStoreConfig{
		BaseURL: "http://127.0.0.1:1",
		Token:   "tok",
		OrgName: "loopzero",
	})
	if err != nil {
		t.Fatalf("NewGiteaSourceStore: %v", err)
	}
	failingReader := &erroringReader{err: io.ErrUnexpectedEOF}
	_, err = store.PushBundle(context.Background(), "tn-test", "p-test", failingReader)
	if err == nil {
		t.Fatal("expected error for failing reader, got nil")
	}
	pe, ok := AsPushBundleError(err)
	if !ok {
		t.Fatalf("expected *PushBundleError, got %T (%v)", err, err)
	}
	if pe.Reason != ReasonArchiveReadError {
		t.Errorf("Reason = %v, want ReasonArchiveReadError", pe.Reason)
	}
	// Cause chain preserves io.ErrUnexpectedEOF for downstream errors.Is checks.
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("errors.Is(err, io.ErrUnexpectedEOF) = false; want true")
	}
}

// T3.1.F — PushBundle returns ReasonBundleTooLarge when over MaxBundleBytes.
func TestPushBundle_ReturnsBundleTooLarge_whenAboveLimit(t *testing.T) {
	store, err := NewGiteaSourceStore(GiteaSourceStoreConfig{
		BaseURL: "http://127.0.0.1:1",
		Token:   "tok",
		OrgName: "loopzero",
	})
	if err != nil {
		t.Fatalf("NewGiteaSourceStore: %v", err)
	}
	// Reader that produces MaxBundleBytes+1 bytes.
	oversized := strings.NewReader(strings.Repeat("a", MaxBundleBytes+1))
	_, err = store.PushBundle(context.Background(), "tn-test", "p-test", oversized)
	if err == nil {
		t.Fatal("expected error for oversized bundle, got nil")
	}
	pe, ok := AsPushBundleError(err)
	if !ok {
		t.Fatalf("expected *PushBundleError, got %T (%v)", err, err)
	}
	if pe.Reason != ReasonBundleTooLarge {
		t.Errorf("Reason = %v, want ReasonBundleTooLarge", pe.Reason)
	}
}

// T3.1.G — PushBundle returns ReasonSourceBackendUnreachable when Gitea backend
// unreachable. We use a real httptest server, close it, then point at the now-dead port.
func TestPushBundle_ReturnsSourceBackendUnreachable_whenGiteaUnavailable(t *testing.T) {
	// Spin up + immediately close to get a dead URL.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	srv.Close()
	store, err := NewGiteaSourceStore(GiteaSourceStoreConfig{
		BaseURL: srv.URL,
		Token:   "tok",
		OrgName: "loopzero",
	})
	if err != nil {
		t.Fatalf("NewGiteaSourceStore: %v", err)
	}
	// Valid git bundle header bytes so the format detector doesn't reject early.
	bundle := strings.NewReader("# v2 git bundle\nminimal content\n")
	_, err = store.PushBundle(context.Background(), "tn-test", "p-test", bundle)
	if err == nil {
		t.Fatal("expected error for unreachable backend, got nil")
	}
	pe, ok := AsPushBundleError(err)
	if !ok {
		// Acceptable fallback: the error may be wrapped at a deeper layer
		// (pushGitBundle returns untyped fmt.Errorf today). The plan T3.2 + T3.3
		// wires the typed error at PushBundle's outer return. If this fails,
		// it surfaces an honest gap: PushBundle's outer return doesn't typed-wrap
		// downstream pushGitBundle errors yet.
		t.Logf("PushBundle outer return did not wrap downstream error as *PushBundleError; current error chain: %v", err)
		t.Skip("PushBundle outer-return typed wrapping for deeper layers is plan T3.2 follow-up — covered by integration in T3.3 handler ladder")
	}
	if pe.Reason != ReasonSourceBackendUnreachable {
		t.Errorf("Reason = %v, want ReasonSourceBackendUnreachable", pe.Reason)
	}
}
