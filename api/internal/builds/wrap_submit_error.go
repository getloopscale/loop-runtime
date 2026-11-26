// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"errors"

	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// wrapSubmitBuildError lifts an untyped error returned by a stage of
// BuildOrchestrator.SubmitBuild into a typed *SubmitBuildError so
// routes/builds.go::mapBuildError can dispatch via AsSubmitBuildError to a
// reason-specific user-facing message (Sprint 6 plan qa-sprint6-cleanup-
// 2026-06-25 T2.2 — closes loopzero-cloud#34).
//
// EC-3 absorption (Sprint 5 mirror): typed sentinels that pre-existed Sprint
// 6 MUST pass through unwrapped so the existing handler ladder ordering at
// routes/builds.go:546-571 is preserved verbatim:
//
//   - loopzeroerrors.ErrDeployInProgress — the route handler ladder already
//     short-circuits this at lines 549-557 (409 DEPLOY_IN_PROGRESS).
//   - *loopzeroerrors.LoopZeroError (NewReplicasExceedPlan / ErrScalingAmbiguous /
//     etc.) — the route handler ladder already dispatches typed LoopZeroError
//     at lines 547-559.
//
// Only genuinely-untyped errors (the 6 fmt.Errorf wrap sites in
// build_orchestrator_submit.go lines 58/73/99/114/144/170/194) are
// classified by this function. Per Sprint 6 ADR D2, the discriminator uses
// the STAGE LABEL emitted by the orchestrator at the wrap site — not
// substring matching on err.Error() — because in-process Go orchestration
// errors vary too much across driver / client versions.
func wrapSubmitBuildError(stage SubmitBuildStage, err error) error {
	if err == nil {
		return nil
	}
	// Sprint 6 /review CODE-002 LOW — defense-in-depth no-double-wrap guard.
	// A future call site that accidentally chains wrapSubmitBuildError on an
	// already-wrapped *SubmitBuildError would otherwise corrupt the Detail
	// field. The check costs one type assertion and closes the footgun.
	if _, ok := AsSubmitBuildError(err); ok {
		return err
	}
	// EC-3: preserve typed sentinels so the route handler ladder still
	// short-circuits DEPLOY_IN_PROGRESS + typed LoopZeroError paths.
	if errors.Is(err, loopzeroerrors.ErrDeployInProgress) {
		return err
	}
	var te *loopzeroerrors.LoopZeroError
	if errors.As(err, &te) {
		return err
	}
	// Map stage label to typed reason via Sprint 6 ADR D2 canonical table.
	// Unknown stages (programmer error — adding a new stage without
	// updating the table) fall through to ReasonBuildSubmitUnknown so the
	// operator still sees an actionable verbatim cause, not the misleading
	// generic INTERNAL_ERROR.
	reason, ok := stageToReason[stage]
	if !ok {
		reason = ReasonBuildSubmitUnknown
	}
	return NewSubmitBuildError(reason, stage, err, err.Error())
}
