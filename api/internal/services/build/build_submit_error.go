// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package build

import (
	"errors"
	"fmt"
	"strings"

	"github.com/getloopscale/loopzero/api/internal/scrub"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// SubmitBuildErrorReason discriminates failure modes returned by
// BuildOrchestrator.SubmitBuild. Sprint 6 plan qa-sprint6-cleanup-2026-06-25
// T1.1 (closes loopzero-cloud#34) — mirrors the Sprint 5 canonical typed-error
// pattern (api/internal/source/pushbundle_error.go) applied to the build
// trigger surface. Before Sprint 6 every untyped error fell through
// routes/builds.go::mapBuildError to the generic ErrInternalError catchall;
// the new discriminator names the actual stage that failed + composes the
// verbatim cause into the user-facing message.
//
// EC-6 absorption (from Sprint 5): values are explicitly assigned (NOT iota)
// so future insertions do not shift persisted reason ints or corrupt
// downstream telemetry / audit consumers pinned on the string label.
type SubmitBuildErrorReason int

const (
	// ReasonBuildSubmitWorkflowFailed: the Argo Workflow submit call failed
	// (HTTP 5xx, network, RBAC, etc.). The verbatim cause is preserved.
	ReasonBuildSubmitWorkflowFailed SubmitBuildErrorReason = 1
	// ReasonBuildSubmitProjectEnsureFailed: EnsureProject failed (tenant
	// lookup race, projects-table write, etc.).
	ReasonBuildSubmitProjectEnsureFailed SubmitBuildErrorReason = 2
	// ReasonBuildSubmitDBWriteFailed: DB write inside the env lock failed
	// (build INSERT, apps_spec SET, active-build check).
	ReasonBuildSubmitDBWriteFailed SubmitBuildErrorReason = 3
	// ReasonBuildSubmitAppNormalizeFailed: NormalizeApp failed for one of
	// the apps in the request.
	ReasonBuildSubmitAppNormalizeFailed SubmitBuildErrorReason = 4
	// ReasonBuildSubmitUnknown is the catchall (NEW — Sprint 6 ADR D2). Per
	// Sprint 5 ADR D2 (closes loopzero-cloud#33): the canonical anti-pattern is
	// defaulting to a misleading specific reason. Operators see the verbatim
	// cause composed into the user-facing message.
	ReasonBuildSubmitUnknown SubmitBuildErrorReason = 5
)

// SubmitBuildStage is the canonical label emitted by the orchestrator at each
// fmt.Errorf wrap site. Per Sprint 6 ADR D2: stage-label discrimination is
// more reliable than substring matching for in-process Go orchestration
// errors that vary across DB driver versions, Argo client versions, etc.
type SubmitBuildStage string

const (
	StageCheckActiveBuilds SubmitBuildStage = "check_active_builds"
	StageResolveScaling    SubmitBuildStage = "resolve_scaling"
	StageEnsureProject     SubmitBuildStage = "ensure_project"
	StageCreateBuild       SubmitBuildStage = "create_build"
	StageSetAppsSpec       SubmitBuildStage = "set_apps_spec"
	StageNormalizeApp      SubmitBuildStage = "normalize_app"
	StageSubmitWorkflow    SubmitBuildStage = "submit_workflow"
)

// stageToReason maps canonical stage labels to the typed reason consumed by
// SubmitBuildError.UserError. Per Sprint 6 ADR D2.
var stageToReason = map[SubmitBuildStage]SubmitBuildErrorReason{
	StageCheckActiveBuilds: ReasonBuildSubmitDBWriteFailed,
	StageResolveScaling:    ReasonBuildSubmitDBWriteFailed,
	StageEnsureProject:     ReasonBuildSubmitProjectEnsureFailed,
	StageCreateBuild:       ReasonBuildSubmitDBWriteFailed,
	StageSetAppsSpec:       ReasonBuildSubmitDBWriteFailed,
	StageNormalizeApp:      ReasonBuildSubmitAppNormalizeFailed,
	StageSubmitWorkflow:    ReasonBuildSubmitWorkflowFailed,
}

// reasonStrings is the canonical wire-form lookup table. EC-6 absorption:
// downstream consumers (logs, audit_log, telemetry, tests) pin on the
// String() output, not the int.
var reasonStrings = map[SubmitBuildErrorReason]string{
	ReasonBuildSubmitWorkflowFailed:      "BuildSubmitWorkflowFailed",
	ReasonBuildSubmitProjectEnsureFailed: "BuildSubmitProjectEnsureFailed",
	ReasonBuildSubmitDBWriteFailed:       "BuildSubmitDBWriteFailed",
	ReasonBuildSubmitAppNormalizeFailed:  "BuildSubmitAppNormalizeFailed",
	ReasonBuildSubmitUnknown:             "BuildSubmitUnknown",
}

// String implements fmt.Stringer. Out-of-range values return "Unknown"
// defensively (EC-6 absorption from Sprint 5).
func (r SubmitBuildErrorReason) String() string {
	if s, ok := reasonStrings[r]; ok {
		return s
	}
	return "Unknown"
}

// reasonSummaries is the actionable summary of each reason, composed with the
// verbatim cause by loopzeroerrors.NewComposed. It lives beside the enum so a new
// reason and its copy change together. Product language only (Rule 3).
var reasonSummaries = map[SubmitBuildErrorReason]string{
	ReasonBuildSubmitWorkflowFailed:      "Build trigger failed at workflow submit",
	ReasonBuildSubmitProjectEnsureFailed: "Build trigger failed while ensuring the project exists",
	ReasonBuildSubmitDBWriteFailed:       "Build trigger failed at a database write step",
	ReasonBuildSubmitAppNormalizeFailed:  "Build trigger failed validating one of the apps",
	ReasonBuildSubmitUnknown:             "Build trigger failed (cause not classified)",
}

// Summary returns the user-facing summary of the reason, or "" for a value
// outside the enum (the caller then falls back to the neutral message).
func (r SubmitBuildErrorReason) Summary() string {
	return reasonSummaries[r]
}

// UserError is the BUILD_SUBMIT_FAILED error shown to the user: the reason's
// summary composed with the scrubbed detail, or a trace reference when the
// detail says nothing useful.
func (e *SubmitBuildError) UserError(traceID string) *loopzeroerrors.LoopZeroError {
	return loopzeroerrors.NewComposed(loopzeroerrors.ErrBuildSubmitFailed, e.Reason.Summary(), e.Detail, traceID)
}

// SubmitBuildError is the discriminated typed error returned by
// BuildOrchestrator.SubmitBuild after Sprint 6. Mirrors the Sprint 5
// canonical *PushBundleError so routes/builds.go::mapBuildError can dispatch
// via AsSubmitBuildError to SubmitBuildError.UserError.
type SubmitBuildError struct {
	Reason SubmitBuildErrorReason
	Stage  SubmitBuildStage
	Cause  error  // wrapped underlying error for errors.Is/As walks
	Detail string // human-readable cause (scrubbed for internal terms — Rule 3)
}

// Error returns a structured representation including reason + stage labels.
func (e *SubmitBuildError) Error() string {
	parts := []string{fmt.Sprintf("submit build failed: reason=%s stage=%s", e.Reason.String(), e.Stage)}
	if e.Detail != "" {
		parts = append(parts, "detail="+e.Detail)
	}
	if e.Cause != nil {
		parts = append(parts, "cause="+e.Cause.Error())
	}
	return strings.Join(parts, " ")
}

// Unwrap returns the wrapped cause so errors.Is/As walks through
// fmt.Errorf("...: %w", ...) middleware.
func (e *SubmitBuildError) Unwrap() error { return e.Cause }

// NewSubmitBuildError constructs a typed SubmitBuildError. Detail is scrubbed
// for internal terms (Rule 3) by scrub.Text, the one table every
// user-facing path shares. Cause is preserved unscrubbed so internal
// observability tools (slog, audit_log) see the raw error.
func NewSubmitBuildError(reason SubmitBuildErrorReason, stage SubmitBuildStage, cause error, detail string) *SubmitBuildError {
	return &SubmitBuildError{
		Reason: reason,
		Stage:  stage,
		Cause:  cause,
		Detail: scrub.Text(detail),
	}
}

// AsSubmitBuildError is the canonical extractor for the route handler's
// mapBuildError ladder. Returns nil + ok=false when the error chain doesn't
// carry a *SubmitBuildError.
func AsSubmitBuildError(err error) (*SubmitBuildError, bool) {
	var se *SubmitBuildError
	if errors.As(err, &se) {
		return se, true
	}
	return nil, false
}
