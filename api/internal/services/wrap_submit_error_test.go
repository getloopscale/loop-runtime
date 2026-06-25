// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"errors"
	"strings"
	"testing"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// Sprint 6 plan T2.1 — pure-function unit tests for wrapSubmitBuildError
// mirroring Sprint 5 Vercel CLI canonical pattern (positive direction +
// negative direction + sentinel preservation). The negative direction
// asserts unknown-stage inputs route to ReasonBuildSubmitUnknown, NOT to a
// misleading discriminated reason — the structural guard against the
// loopzero-cloud#34 catchall anti-pattern this Sprint closes.

func TestWrapSubmitBuildError_NewDiscriminators(t *testing.T) {
	cases := []struct {
		name          string
		stage         SubmitBuildStage
		in            error
		wantReason    SubmitBuildErrorReason
		wantDetailHas string
	}{
		// Positive direction — each canonical stage routes to its reason.
		{
			name:          "P1-workflow-submit-failed",
			stage:         StageSubmitWorkflow,
			in:            errors.New("argo client: 503 Service Unavailable"),
			wantReason:    ReasonBuildSubmitWorkflowFailed,
			wantDetailHas: "503",
		},
		{
			name:          "P2-project-ensure-failed",
			stage:         StageEnsureProject,
			in:            errors.New("project: tenant lookup failed: no rows"),
			wantReason:    ReasonBuildSubmitProjectEnsureFailed,
			wantDetailHas: "tenant lookup failed",
		},
		{
			name:          "P3-db-write-build-create-failed",
			stage:         StageCreateBuild,
			in:            errors.New("pq: duplicate key value violates unique constraint"),
			wantReason:    ReasonBuildSubmitDBWriteFailed,
			wantDetailHas: "duplicate key",
		},
		{
			name:          "P4-db-write-active-check-failed",
			stage:         StageCheckActiveBuilds,
			in:            errors.New("pq: connection reset by peer"),
			wantReason:    ReasonBuildSubmitDBWriteFailed,
			wantDetailHas: "connection reset",
		},
		{
			name:          "P5-app-normalize-failed",
			stage:         StageNormalizeApp,
			in:            errors.New("app shape: invalid runtime declared"),
			wantReason:    ReasonBuildSubmitAppNormalizeFailed,
			wantDetailHas: "invalid runtime",
		},
		// Negative direction — unknown stage label routes to UnknownReason.
		{
			name:          "N1-unknown-stage-label",
			stage:         SubmitBuildStage("not-a-real-stage"),
			in:            errors.New("some downstream failure"),
			wantReason:    ReasonBuildSubmitUnknown,
			wantDetailHas: "some downstream failure",
		},
		{
			name:          "N2-empty-stage-label",
			stage:         SubmitBuildStage(""),
			in:            errors.New("anonymous failure"),
			wantReason:    ReasonBuildSubmitUnknown,
			wantDetailHas: "anonymous failure",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := wrapSubmitBuildError(tc.stage, tc.in)
			se, ok := AsSubmitBuildError(out)
			if !ok {
				t.Fatalf("expected *SubmitBuildError, got %T (%v)", out, out)
			}
			if se.Reason != tc.wantReason {
				t.Errorf("Reason = %v, want %v", se.Reason, tc.wantReason)
			}
			if se.Stage != tc.stage {
				t.Errorf("Stage = %q, want %q", se.Stage, tc.stage)
			}
			if tc.wantDetailHas != "" && !strings.Contains(se.Detail, tc.wantDetailHas) {
				t.Errorf("Detail = %q, want substring %q", se.Detail, tc.wantDetailHas)
			}
			if se.Cause == nil {
				t.Error("Cause must be preserved for errors.As/Unwrap chain")
			}
		})
	}
}

// Sprint 6 plan T2.1 EC-3 — typed sentinel ErrDeployInProgress passes
// through unwrapped (Sprint 5-style preservation). Routes/builds.go ladder
// at lines 549-557 already short-circuits this case.
func TestWrapSubmitBuildError_PreservesErrDeployInProgress(t *testing.T) {
	in := loopzeroerrors.ErrDeployInProgress
	out := wrapSubmitBuildError(StageCheckActiveBuilds, in)
	if !errors.Is(out, loopzeroerrors.ErrDeployInProgress) {
		t.Fatal("ErrDeployInProgress sentinel must pass through wrap")
	}
	if _, ok := AsSubmitBuildError(out); ok {
		t.Fatal("ErrDeployInProgress must NOT be wrapped in SubmitBuildError (EC-3)")
	}
}

// Sprint 6 plan T2.1 EC-3 — typed *LoopZeroError instances (ScalingAmbiguous /
// NewReplicasExceedPlan / etc.) pass through unwrapped so the route handler
// ladder at lines 547-559 still dispatches them with their original code.
func TestWrapSubmitBuildError_PreservesTypedLoopZeroError(t *testing.T) {
	in := loopzeroerrors.ErrScalingAmbiguous
	out := wrapSubmitBuildError(StageResolveScaling, in)
	var te *loopzeroerrors.LoopZeroError
	if !errors.As(out, &te) {
		t.Fatal("typed *LoopZeroError must pass through wrap")
	}
	if te.Code != loopzeroerrors.ErrScalingAmbiguous.Code {
		t.Errorf("Code = %q, want %q", te.Code, loopzeroerrors.ErrScalingAmbiguous.Code)
	}
	if _, ok := AsSubmitBuildError(out); ok {
		t.Fatal("typed *LoopZeroError must NOT be re-wrapped in SubmitBuildError (EC-3)")
	}
}

// Sprint 6 plan T2.1 — nil input contract.
func TestWrapSubmitBuildError_NilInputReturnsNil(t *testing.T) {
	if got := wrapSubmitBuildError(StageSubmitWorkflow, nil); got != nil {
		t.Errorf("wrapSubmitBuildError(_, nil) = %v, want nil", got)
	}
}
