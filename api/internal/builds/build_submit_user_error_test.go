// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package builds

import (
	"strings"
	"testing"
)

// The build-trigger message pairs the stage that failed with the verbatim
// cause; the "Something went wrong" catchall (loopruntime-cloud#34) never returns.
func TestSubmitBuildError_UserErrorComposesTheReasonSummaryWithTheCause(t *testing.T) {
	cases := []struct {
		reason   SubmitBuildErrorReason
		detail   string
		wantSubs []string
	}{
		{ReasonBuildSubmitWorkflowFailed, "workflow client: 503 Service Unavailable", []string{"Build trigger failed at workflow submit", "503"}},
		{ReasonBuildSubmitProjectEnsureFailed, "tenant lookup failed: no rows", []string{"ensuring the project exists", "tenant lookup failed"}},
		{ReasonBuildSubmitDBWriteFailed, "duplicate key value violates unique constraint", []string{"database write step", "duplicate key"}},
		{ReasonBuildSubmitAppNormalizeFailed, "invalid runtime declared for app foo", []string{"validating one of the apps", "invalid runtime"}},
		{ReasonBuildSubmitUnknown, "some genuinely-unknown downstream failure", []string{"cause not classified", "genuinely-unknown"}},
	}
	for _, tc := range cases {
		t.Run(tc.reason.String(), func(t *testing.T) {
			err := (&SubmitBuildError{Reason: tc.reason, Detail: tc.detail}).UserError("")

			if err.Code != "BUILD_SUBMIT_FAILED" {
				t.Errorf("Code = %q, want BUILD_SUBMIT_FAILED (Rule 4)", err.Code)
			}
			for _, want := range tc.wantSubs {
				if !strings.Contains(err.Message, want) {
					t.Errorf("Message %q must contain %q", err.Message, want)
				}
			}
			if strings.Contains(err.Message, "Something went wrong") {
				t.Errorf("Message %q reintroduced the #34 catchall", err.Message)
			}
		})
	}
}

func TestSubmitBuildErrorReason_EveryReasonHasASummary(t *testing.T) {
	for reason := range reasonStrings {
		if reason.Summary() == "" {
			t.Errorf("reason %s has no user-facing summary", reason)
		}
	}
}
