// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package build

import (
	"errors"
	"strings"
	"testing"
)

// Sprint 6 plan qa-sprint6-cleanup-2026-06-25 T1.1 — Stringer round-trip
// + EC-6 absorption (out-of-range guard) mirroring Sprint 5 canonical.
func TestSubmitBuildErrorReason_StringerRoundTrip(t *testing.T) {
	cases := []struct {
		reason SubmitBuildErrorReason
		want   string
	}{
		{ReasonBuildSubmitWorkflowFailed, "BuildSubmitWorkflowFailed"},
		{ReasonBuildSubmitProjectEnsureFailed, "BuildSubmitProjectEnsureFailed"},
		{ReasonBuildSubmitDBWriteFailed, "BuildSubmitDBWriteFailed"},
		{ReasonBuildSubmitAppNormalizeFailed, "BuildSubmitAppNormalizeFailed"},
		{ReasonBuildSubmitUnknown, "BuildSubmitUnknown"},
		{SubmitBuildErrorReason(99), "Unknown"}, // out-of-range guard per EC-6
		{SubmitBuildErrorReason(0), "Unknown"},  // zero value (uninitialized) is Unknown
	}
	for _, tc := range cases {
		if got := tc.reason.String(); got != tc.want {
			t.Errorf("SubmitBuildErrorReason(%d).String() = %q, want %q", tc.reason, got, tc.want)
		}
	}
}

// Sprint 6 plan T1.1 — Unwrap chain works through fmt.Errorf wraps.
func TestSubmitBuildError_UnwrapsThroughFmtErrorf(t *testing.T) {
	cause := errors.New("argo client: 503 Service Unavailable")
	se := NewSubmitBuildError(ReasonBuildSubmitWorkflowFailed, StageSubmitWorkflow, cause, cause.Error())
	wrapped := errors.Join(errors.New("outer wrap"), se)

	got, ok := AsSubmitBuildError(wrapped)
	if !ok {
		t.Fatalf("AsSubmitBuildError on errors.Join-wrapped err returned ok=false")
	}
	if got.Reason != ReasonBuildSubmitWorkflowFailed {
		t.Errorf("Reason = %v, want %v", got.Reason, ReasonBuildSubmitWorkflowFailed)
	}
	if !errors.Is(wrapped, cause) {
		t.Errorf("errors.Is(wrapped, cause) = false, want true (Unwrap chain broken)")
	}
}

// Sprint 6 plan T1.1 — Detail is scrubbed via the canonical
// scrub.Text table, shared with every user-facing path.
// Sprint 6 /review S6-4 added explicit assertions for the Sprint 5
// hardened patterns (literal Gitea word, 172.16/12 + 192.168/16 RFC1918,
// Sprint 6 CODE-001 catch-all pq driver scrub) so Sprint 6 has its own
// regression coverage rather than relying on Sprint 5's tests alone.
func TestSubmitBuildError_ScrubsInternalTermsInDetail(t *testing.T) {
	cases := []struct {
		name        string
		raw         string
		wantSubs    []string
		wantNotSubs []string
	}{
		{
			name:        "gitea + credential + 10/8 CIDR",
			raw:         "Post \"https://gitea-http.loopzero-system.svc.cluster.local:3000\": Authorization: Bearer ghp_secret1234567890 dial tcp 10.43.0.5:3000",
			wantNotSubs: []string{"ghp_secret1234567890", "gitea-http", "10.43.0.5"},
		},
		{
			name:        "literal Gitea word scrubbed (CR-9 — Sprint 5 F-sec-2)",
			raw:         "Gitea: Internal Server Error",
			wantSubs:    []string{"source backend"},
			wantNotSubs: []string{"Gitea"},
		},
		{
			name:        "RFC1918 172.16/12 scrubbed (Sprint 5 F-sec-1)",
			raw:         "dial tcp 172.20.45.3:3000: connect: connection refused",
			wantSubs:    []string{"dial tcp <internal>"},
			wantNotSubs: []string{"172.20.45.3"},
		},
		{
			name:        "RFC1918 192.168/16 scrubbed (Sprint 5 F-sec-1)",
			raw:         "dial tcp 192.168.10.50:5432: connection refused",
			wantSubs:    []string{"dial tcp <internal>"},
			wantNotSubs: []string{"192.168.10.50"},
		},
		{
			// Sprint 6 /review CODE-001 — pq driver leak coverage applied to the
			// SubmitBuildError surface (not just PushBundleError).
			name:        "pq driver internal hostname scrubbed (Sprint 6 CODE-001)",
			raw:         "pq: could not connect to host postgresql.loopzero-system.svc.cluster.local",
			wantSubs:    []string{"<internal service>"},
			wantNotSubs: []string{"postgresql.loopzero-system", ".svc.cluster.local"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cause := errors.New(tc.raw)
			se := NewSubmitBuildError(ReasonBuildSubmitWorkflowFailed, StageSubmitWorkflow, cause, cause.Error())
			for _, want := range tc.wantSubs {
				if !strings.Contains(se.Detail, want) {
					t.Errorf("Detail %q must contain %q", se.Detail, want)
				}
			}
			for _, notWant := range tc.wantNotSubs {
				if strings.Contains(se.Detail, notWant) {
					t.Errorf("Detail leaked %q: %q", notWant, se.Detail)
				}
			}
			// Cause stays unscrubbed for internal observability (slog / audit_log).
			if !strings.Contains(se.Cause.Error(), tc.raw) {
				t.Errorf("Cause MUST preserve raw error for internal logs; got %q", se.Cause.Error())
			}
		})
	}
}
