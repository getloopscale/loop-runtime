// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package source

import (
	"errors"
	"strings"
	"testing"
)

// Sprint 5 plan qa-sprint5-cleanup-2026-06-25 T2.1 — RED tests for the new
// wrap-site discriminators (auth / capacity / genuine-remote-rejected /
// unknown-downstream). Pure-function unit tests mirror the canonical
// vercel/cli pattern (knowledge-base/references/vercel/packages/cli/test/unit/
// util/deploy/process-deployment.test.ts:48-80): positive direction asserts
// known-shape causes route to the correct typed reason; negative direction
// asserts unknown-shape causes route to ReasonUnknownDownstream and NOT to
// a misleading bucket — the structural guard against the Sprint 4 catchall.
//
// EC-3 absorption: sentinel ErrInvalidTenantOrProject MUST pass through
// unwrapped (preserved Sprint 4 behavior). Regression-protector subtests
// for the network bucket are included so reordering does not silently
// regress Sprint 4 closures.

// T2.1 — positive + negative cases for wrapDownstreamPushError discriminators.
func TestWrapDownstreamPushError_NewDiscriminators(t *testing.T) {
	cases := []struct {
		name          string
		in            error
		wantReason    PushBundleErrorReason
		wantDetailHas string
	}{
		// Positive direction — known-shape causes.
		{
			name:          "P1-auth-401",
			in:            errors.New("git push failed: server returned 401 Unauthorized"),
			wantReason:    ReasonSourceBackendAuthFailure,
			wantDetailHas: "401",
		},
		{
			name:          "P1b-auth-permission-denied",
			in:            errors.New("Authorization failed: permission denied"),
			wantReason:    ReasonSourceBackendAuthFailure,
			wantDetailHas: "permission denied",
		},
		{
			name:          "P2-capacity-507",
			in:            errors.New("git push failed: server returned 507 Insufficient Storage"),
			wantReason:    ReasonSourceBackendCapacity,
			wantDetailHas: "507",
		},
		{
			name:          "P2b-capacity-quota-exceeded",
			in:            errors.New("quota exceeded on remote"),
			wantReason:    ReasonSourceBackendCapacity,
			wantDetailHas: "quota exceeded",
		},
		{
			name:          "P3-remote-rejected-pre-receive",
			in:            errors.New("! [pre-receive hook declined] main -> main"),
			wantReason:    ReasonRemoteRejectedPush,
			wantDetailHas: "pre-receive hook declined",
		},
		// Negative direction — unknown-shape causes MUST route to UnknownDownstream.
		{
			name:          "N1-unknown-repo-locked",
			in:            errors.New("repo locked by another process"),
			wantReason:    ReasonUnknownDownstream,
			wantDetailHas: "repo locked",
		},
		{
			name:          "N2-unknown-bundle-invalid",
			in:            errors.New("bundle: invalid magic header"),
			wantReason:    ReasonUnknownDownstream,
			wantDetailHas: "invalid magic",
		},
		{
			name:          "N3-unknown-generic-failure",
			in:            errors.New("operation failed with unspecified error"),
			wantReason:    ReasonUnknownDownstream,
			wantDetailHas: "unspecified",
		},
		// Sprint 4 regression-protectors.
		{
			name:          "REG-network-connection-refused",
			in:            errors.New("Post \"https://source-backend:3000\": dial tcp: connection refused"),
			wantReason:    ReasonSourceBackendUnreachable,
			wantDetailHas: "source backend is unreachable",
		},
		{
			name:          "REG-network-i-o-timeout",
			in:            errors.New("Get \"https://source-backend\": i/o timeout"),
			wantReason:    ReasonSourceBackendUnreachable,
			wantDetailHas: "source backend is unreachable",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := wrapDownstreamPushError(tc.in)
			pe, ok := AsPushBundleError(out)
			if !ok {
				t.Fatalf("expected *PushBundleError, got %T (%v)", out, out)
			}
			if pe.Reason != tc.wantReason {
				t.Errorf("Reason = %v, want %v", pe.Reason, tc.wantReason)
			}
			if tc.wantDetailHas != "" && !strings.Contains(pe.Detail, tc.wantDetailHas) {
				t.Errorf("Detail = %q, want substring %q", pe.Detail, tc.wantDetailHas)
			}
			if pe.Cause == nil {
				t.Error("Cause must be preserved for errors.As/Unwrap chain")
			}
		})
	}
}

// T2.1 EC-3 regression — sentinel passes through unwrapped (not wrapped in
// PushBundleError). Sprint 4 closure preserved.
func TestWrapDownstreamPushError_PreservesErrInvalidTenantOrProject(t *testing.T) {
	in := ErrInvalidTenantOrProject
	out := wrapDownstreamPushError(in)
	if !errors.Is(out, ErrInvalidTenantOrProject) {
		t.Fatal("ErrInvalidTenantOrProject sentinel must pass through wrap")
	}
	if _, ok := AsPushBundleError(out); ok {
		t.Fatal("ErrInvalidTenantOrProject must NOT be wrapped in PushBundleError (EC-3)")
	}
}

// T2.1 nil input — wrapping nil returns nil (defensive contract).
func TestWrapDownstreamPushError_NilInputReturnsNil(t *testing.T) {
	if got := wrapDownstreamPushError(nil); got != nil {
		t.Errorf("wrapDownstreamPushError(nil) = %v, want nil", got)
	}
}
