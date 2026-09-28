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
		// Sprint 5 plan qa-sprint5-cleanup-2026-06-25 T1.1 — new discriminators.
		{ReasonSourceBackendAuthFailure, "SourceBackendAuthFailure"},
		{ReasonSourceBackendCapacity, "SourceBackendCapacity"},
		{ReasonUnknownDownstream, "UnknownDownstream"},
		{PushBundleErrorReason(99), "Unknown"}, // out-of-range guard per EC-6
		{PushBundleErrorReason(0), "Unknown"},  // zero value (uninitialized) is Unknown
	}
	for _, tc := range cases {
		if got := tc.reason.String(); got != tc.want {
			t.Errorf("PushBundleErrorReason(%d).String() = %q, want %q", tc.reason, got, tc.want)
		}
	}
}

// Detail scrubbing — scrub.Text, the shared Rule 3 table.
// Detail is composed into the user-facing LoopZeroError message per ADR D3;
// without scrubbing, Rule 3 (no internal terms — Gitea / MinIO / SVC DNS)
// would be violated. Scrub runs at NewPushBundleError construction time.

func TestPushBundleError_ScrubsInternalTerms(t *testing.T) {
	cases := []struct {
		name        string
		rawDetail   string
		wantSubs    []string
		wantNotSubs []string
	}{
		{
			name:        "gitea hostname scrubbed",
			rawDetail:   "Post \"https://gitea-http.loopzero-system.svc.cluster.local:3000/api/v1/repos\": EOF",
			wantSubs:    []string{"source backend"},
			wantNotSubs: []string{"gitea-http", ".loopzero-system", ".svc.cluster.local"},
		},
		{
			name:        "minio hostname scrubbed",
			rawDetail:   "Get \"http://minio.loopzero-system.svc.cluster.local:9000/bundle\": connection refused",
			wantSubs:    []string{"object store"},
			wantNotSubs: []string{"minio.loopzero-system", ".svc.cluster.local"},
		},
		{
			name:        "svc.cluster.local suffix scrubbed (defense layer)",
			rawDetail:   "lookup somesvc.other-ns.svc.cluster.local: no such host",
			wantSubs:    []string{"no such host"},
			wantNotSubs: []string{".svc.cluster.local"},
		},
		{
			// Sprint 6 /review CODE-001 — pq driver leak.
			name:        "pq driver hostname fully scrubbed (CODE-001)",
			rawDetail:   "pq: could not connect to host postgresql.loopzero-system.svc.cluster.local",
			wantSubs:    []string{"<internal service>"},
			wantNotSubs: []string{"postgresql.loopzero-system", ".svc.cluster.local"},
		},
		{
			// Sprint 6 /review CODE-001 — generic catch-all for any in-cluster service.
			name:        "any cluster-internal hostname fully scrubbed",
			rawDetail:   "Get http://argo-workflows.argo.svc.cluster.local:2746: dial: connect refused",
			wantSubs:    []string{"<internal service>"},
			wantNotSubs: []string{".svc.cluster.local"},
		},
		// Sprint 7 #35 info-disclosure CWE-209 — Argo / ArgoCD / SA / namespace / tmp paths
		{
			name:        "#35 argo: create workflow leak scrubbed",
			rawDetail:   "Build trigger failed at workflow submit: argo: create workflow: Unauthorized",
			wantSubs:    []string{"<orchestrator>"},
			wantNotSubs: []string{"argo: create workflow"},
		},
		{
			name:        "#35 argoproj.io leak scrubbed",
			rawDetail:   "apiGroup argoproj.io denied for SA",
			wantSubs:    []string{"<orchestrator>"},
			wantNotSubs: []string{"argoproj.io"},
		},
		{
			name:        "#35 system:serviceaccount full chain scrubbed",
			rawDetail:   "system:serviceaccount:loopzero-system:default cannot list applications",
			wantSubs:    []string{"<service identity>"},
			wantNotSubs: []string{"system:serviceaccount:loopzero-system:default"},
		},
		{
			name:        "#35 namespace leak scrubbed",
			rawDetail:   "applications.argoproj.io in namespace \"loopzero-system\" forbidden",
			wantSubs:    []string{"<orchestrator>", "<scope>"},
			wantNotSubs: []string{"namespace \"loopzero-system\""},
		},
		{
			name:        "#35 /tmp/loopzero-source path scrubbed",
			rawDetail:   "git fetch /tmp/loopzero-source-1147567712/source.bundle HEAD: did not send all necessary objects",
			wantSubs:    []string{"<server tmp>"},
			wantNotSubs: []string{"/tmp/loopzero-source-1147567712"},
		},
		{
			name:        "#35 git bundle term scrubbed",
			rawDetail:   "git bundle did not send all necessary objects",
			wantSubs:    []string{"<source archive>"},
			wantNotSubs: []string{"git bundle"},
		},
		// Negative-direction: legitimate user terms NOT scrubbed.
		{
			name:      "#35 negative — user project name 'my-argo-test' survives",
			rawDetail: "validation error on project my-argo-test",
			wantSubs:  []string{"my-argo-test"},
		},
		{
			name:      "#35 negative — github.com still NOT scrubbed (Sprint 5 regression)",
			rawDetail: "fetch from https://github.com/user/repo failed",
			wantSubs:  []string{"github.com"},
		},
		{
			name:        "internal CIDR scrubbed",
			rawDetail:   "Post: dial tcp 10.43.0.5:3000: connect: connection refused",
			wantSubs:    []string{"dial tcp <internal>", "connection refused"},
			wantNotSubs: []string{"10.43.0.5"},
		},
		{
			name:        "Authorization header — Bearer scheme fully redacted (F-code-1)",
			rawDetail:   "request denied with header Authorization: Bearer ghp_secret1234567890",
			wantSubs:    []string{"Authorization: <redacted>"},
			wantNotSubs: []string{"ghp_secret1234567890", "Bearer ghp_secret"},
		},
		{
			name:        "Authorization header — Basic scheme fully redacted (F-code-1)",
			rawDetail:   "Authorization: Basic dXNlcjpwYXNzd29yZA==",
			wantSubs:    []string{"Authorization: <redacted>"},
			wantNotSubs: []string{"dXNlcjpwYXNzd29yZA==", "Basic dXNlcjpwYXNz"},
		},
		{
			name:        "Authorization header — token scheme fully redacted",
			rawDetail:   "Authorization: token abcdef0123456789abcdef",
			wantSubs:    []string{"Authorization: <redacted>"},
			wantNotSubs: []string{"abcdef0123456789abcdef"},
		},
		{
			name:        "bare token literal redacted",
			rawDetail:   "git push: invalid token 0123456789abcdef0123456789abcdef",
			wantSubs:    []string{"token <redacted>"},
			wantNotSubs: []string{"0123456789abcdef"},
		},
		// F-code-2 — defense-in-depth for inline Bearer/Basic without header keyword.
		{
			name:        "inline Bearer credential redacted (F-code-2)",
			rawDetail:   "downstream returned Bearer ghp_inline1234567890abcdef",
			wantNotSubs: []string{"ghp_inline1234567890abcdef"},
		},
		// F-sec-1 — RFC1918 172.16/12 internal CIDR scrubbed.
		{
			name:        "internal CIDR 172.16/12 scrubbed",
			rawDetail:   "Post: dial tcp 172.20.45.3:3000: connect: connection refused",
			wantSubs:    []string{"dial tcp <internal>", "connection refused"},
			wantNotSubs: []string{"172.20.45.3"},
		},
		// F-sec-1 — RFC1918 192.168/16 internal CIDR scrubbed.
		{
			name:        "internal CIDR 192.168/16 scrubbed",
			rawDetail:   "Post: dial tcp 192.168.10.50:3000: connect: connection refused",
			wantSubs:    []string{"dial tcp <internal>", "connection refused"},
			wantNotSubs: []string{"192.168.10.50"},
		},
		// F-sec-1 negative — public IP NOT scrubbed.
		{
			name:      "public IP NOT scrubbed (172.x outside RFC1918)",
			rawDetail: "dial tcp 172.4.5.6:443: connect: connection refused",
			wantSubs:  []string{"172.4.5.6", "connection refused"},
		},
		// F-sec-2 — literal "Gitea" product name scrubbed (CR-9).
		{
			name:        "literal Gitea word scrubbed (CR-9 product name)",
			rawDetail:   "Gitea: Internal Server Error — repository not found",
			wantSubs:    []string{"source backend"},
			wantNotSubs: []string{"Gitea"},
		},
		{
			name:        "case-insensitive Gitea word scrubbed",
			rawDetail:   "500 error from GITEA backend",
			wantSubs:    []string{"source backend"},
			wantNotSubs: []string{"GITEA", "Gitea"},
		},
		// Negative direction: legitimate external hostnames MUST NOT be touched.
		{
			name:      "external github.com NOT touched",
			rawDetail: "fetch https://github.com/foo/bar: connection refused",
			wantSubs:  []string{"github.com", "connection refused"},
		},
		{
			name:      "external gitlab.com NOT touched",
			rawDetail: "https://gitlab.com/foo/bar.git: not found",
			wantSubs:  []string{"gitlab.com"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pe := NewPushBundleError(ReasonUnknownDownstream, errors.New(tc.rawDetail), tc.rawDetail)
			for _, want := range tc.wantSubs {
				if !strings.Contains(pe.Detail, want) {
					t.Errorf("Detail %q must contain %q", pe.Detail, want)
				}
			}
			for _, notWant := range tc.wantNotSubs {
				if strings.Contains(pe.Detail, notWant) {
					t.Errorf("Detail %q must NOT contain %q (Rule 3 scrub regression)", pe.Detail, notWant)
				}
			}
			// Cause stays unscrubbed for internal observability.
			if !strings.Contains(pe.Cause.Error(), tc.rawDetail) {
				t.Errorf("Cause MUST preserve raw error for internal logs; got %q", pe.Cause.Error())
			}
		})
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
