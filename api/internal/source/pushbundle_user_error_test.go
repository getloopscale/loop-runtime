// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package source

import (
	"strings"
	"testing"
)

// Every reason has actionable copy, and the user message keeps the verbatim
// cause (loopruntime-cloud#33): replacing it with "Verify your project name is
// unused" misled operators whose names were fine.
func TestPushBundleError_UserErrorComposesTheReasonSummaryWithTheCause(t *testing.T) {
	cases := []struct {
		reason   PushBundleErrorReason
		detail   string
		wantSubs []string
	}{
		{ReasonRemoteRejectedPush, "pre-receive hook declined", []string{"The remote rejected the push", "pre-receive hook declined"}},
		{ReasonUnknownDownstream, "repo locked by another process", []string{"Source upload failed", "repo locked by another process"}},
		{ReasonSourceBackendAuthFailure, "server returned 401 Unauthorized", []string{"Source backend rejected authentication", "401"}},
		{ReasonSourceBackendCapacity, "quota exceeded on remote", []string{"Source backend is out of capacity", "quota exceeded"}},
		{ReasonSourceBackendUnreachable, "dial tcp: connection refused", []string{"Source backend is unreachable", "connection refused"}},
		{ReasonBundleMalformed, "", []string{"source bundle is malformed"}},
		{ReasonBundleTooLarge, "", []string{"exceeds the size limit"}},
		{ReasonArchiveReadError, "", []string{"Could not read the source archive"}},
	}
	for _, tc := range cases {
		t.Run(tc.reason.String(), func(t *testing.T) {
			err := (&PushBundleError{Reason: tc.reason, Detail: tc.detail}).UserError("")

			if err.Code != "SOURCE_UPLOAD_FAILED" {
				t.Errorf("Code = %q, want SOURCE_UPLOAD_FAILED (Rule 4)", err.Code)
			}
			for _, want := range tc.wantSubs {
				if !strings.Contains(err.Message, want) {
					t.Errorf("Message %q must contain %q", err.Message, want)
				}
			}
			if strings.Contains(err.Message, "Verify your project name is unused") {
				t.Errorf("Message %q reintroduced the #33 misleading text", err.Message)
			}
		})
	}
}

func TestPushBundleErrorReason_EveryReasonHasASummary(t *testing.T) {
	for reason := range reasonStrings {
		if reason.Summary() == "" {
			t.Errorf("reason %s has no user-facing summary", reason)
		}
	}
}

func TestPushBundleError_UnknownReasonFallsBackToTheNeutralMessage(t *testing.T) {
	err := (&PushBundleError{Reason: PushBundleErrorReason(99), Detail: "ignored when the reason is unknown"}).UserError("tid-unknown")

	if !strings.Contains(err.Message, "Reference: tid-unknown") || strings.Contains(err.Message, "ignored when") {
		t.Errorf("Message = %q, want the neutral message with the trace reference and no raw detail", err.Message)
	}
}
