// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

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
// source.ScrubInternalTerms helper (Sprint 5 T2.3 hardened version).
func TestSubmitBuildError_ScrubsInternalTermsInDetail(t *testing.T) {
	cause := errors.New("Post \"https://gitea-http.loopzero-system.svc.cluster.local:3000\": Authorization: Bearer ghp_secret1234567890 dial tcp 10.43.0.5:3000")
	se := NewSubmitBuildError(ReasonBuildSubmitWorkflowFailed, StageSubmitWorkflow, cause, cause.Error())

	// Detail must NOT contain credentials / internal hostnames / internal CIDR.
	if strings.Contains(se.Detail, "ghp_secret1234567890") {
		t.Errorf("Detail leaked credential: %q", se.Detail)
	}
	if strings.Contains(se.Detail, "gitea-http") {
		t.Errorf("Detail leaked literal gitea hostname: %q", se.Detail)
	}
	if strings.Contains(se.Detail, "10.43.0.5") {
		t.Errorf("Detail leaked internal CIDR: %q", se.Detail)
	}
	// Cause stays unscrubbed for internal observability (slog / audit_log).
	if !strings.Contains(se.Cause.Error(), "ghp_secret1234567890") {
		t.Errorf("Cause MUST preserve raw error for internal logs; got %q", se.Cause.Error())
	}
}
